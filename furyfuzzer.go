package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mirror the Python constants exactly.
const (
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
	reqTimeout   = 5 * time.Second
	maxFailures  = 3
	aliveRetries = 2
)

var (
	// Single lock protects both stdout and stderr writes so result lines and
	// debug lines never interleave — same as Python's PRINT_LOCK.
	printMu sync.Mutex

	debugMode bool

	// Atomic counter mirrors Python's queue.qsize() for debug output.
	remaining atomic.Int64
)

// tPrint writes a result line to stdout under the shared lock.
func tPrint(status int, targetURL string) {
	printMu.Lock()
	fmt.Printf("[HTTP %d] %s\n", status, targetURL)
	printMu.Unlock()
}

// tDebug writes a debug line to stderr under the shared lock, no-op when
// debug mode is off (zero-cost fast path avoids format work entirely).
func tDebug(id int, format string, args ...any) {
	if !debugMode {
		return
	}
	printMu.Lock()
	fmt.Fprintf(os.Stderr, "[DEBUG FROM T%03d] ", id)
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	printMu.Unlock()
}

// newClient builds a per-worker *http.Client with a tuned transport:
//   - TLS verification disabled (same as Python requests verify=False)
//   - Redirects NOT followed — 3xx surfaces as-is (same as allow_redirects=False)
//   - Large idle-connection pool so every goroutine can keep a warm connection
//   - Optional proxy routed through http.Transport.Proxy
//
// Creating one client per worker (not one globally) avoids lock contention
// on the shared idle-connection map inside http.Transport.
func newClient(proxyAddr string) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		// Size the pool generously so workers never wait for a free slot.
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 100,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     90 * time.Second,
		// Keep HTTP/1.1 keep-alive — don't disable it.
		DisableKeepAlives: false,
	}
	if proxyAddr != "" {
		if p, err := url.Parse(proxyAddr); err == nil {
			tr.Proxy = http.ProxyURL(p)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   reqTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// Return the redirect response directly without following it.
			// net/http treats ErrUseLastResponse specially: the response is
			// returned with err == nil, so callers see the 3xx status code.
			return http.ErrUseLastResponse
		},
	}
}

// doGET issues a single GET with the shared User-Agent header.
// Allocating a new *http.Request per call is unavoidable in net/http, but
// the struct is small and the GC handles it cheaply.
func doGET(client *http.Client, target string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return client.Do(req)
}

// drainClose fully reads and discards the response body before closing it.
// This is required for net/http to return the underlying TCP connection to
// the idle pool for reuse. Without it every request opens a new connection.
func drainClose(resp *http.Response) {
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()
}

// isAlive probes baseURL up to aliveRetries times and returns true on the
// first successful response (any HTTP status counts — the server is up).
func isAlive(client *http.Client, baseURL string, id int) bool {
	for i := 0; i < aliveRetries; i++ {
		resp, err := doGET(client, baseURL)
		if err == nil {
			drainClose(resp)
			tDebug(id, "Successfully contacted %s!", baseURL)
			return true
		}
		tDebug(id, "%v", err)
	}
	return false
}

// worker is the goroutine body. It drains the URL channel, checks liveness,
// then enumerates every wordlist entry against that base URL.
//
// Multi-threading: N goroutines run concurrently.
// Multi-processing (Go model): runtime.GOMAXPROCS == NumCPU so the Go
// scheduler maps goroutines onto all available hardware threads in parallel,
// providing the same benefit as Python's multiprocessing module.
func worker(id int, urlCh <-chan string, wordlist []string, proxy string, wg *sync.WaitGroup) {
	defer wg.Done()
	// Each worker owns its own http.Client / Transport / connection pool —
	// no contention on idle-connection maps between goroutines.
	client := newClient(proxy)

	for baseURL := range urlCh {
		// Atomically decrement and read — lock-free, faster than a mutex.
		rem := remaining.Add(-1)
		tDebug(id, "Fetched URL from queue. %d remaining...", rem)
		tDebug(id, "Checking if %s is up...", baseURL)

		if !isAlive(client, baseURL, id) {
			continue
		}
		tDebug(id, "%s is up. Beginning enumeration...", baseURL)

		// wordlist is a read-only slice shared across all goroutines —
		// zero synchronisation cost on the hot path.
		for _, word := range wordlist {
			target := baseURL + word // string + is optimal for two operands
			for failures := 0; failures < maxFailures; failures++ {
				resp, err := doGET(client, target)
				if err == nil {
					status := resp.StatusCode
					drainClose(resp) // drain before print so conn is reused immediately
					tPrint(status, target)
					break
				}
				tDebug(id, "Failed to reach %s. Retrying... (%d/%d)", target, failures, maxFailures)
			}
		}
	}
	tDebug(id, "Queue empty.")
}

// readLines reads a file and returns non-empty lines, trimming nothing —
// lines are used verbatim as URL prefixes / suffixes.
// A 1 MB scanner buffer handles long lines without reallocating.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

func main() {
	// GOMAXPROCS = NumCPU ensures the Go scheduler uses all hardware threads,
	// providing true parallelism across CPU cores (multi-processing in
	// Python terms). Goroutines (multi-threading) are then distributed
	// across those OS threads by the M:N scheduler.
	runtime.GOMAXPROCS(runtime.NumCPU())

	urlsFile     := flag.String("u", "", "Target URLs file")
	wordlistFile := flag.String("w", "", "Wordlist file")
	threads      := flag.Int("t", 1, "Number of goroutines")
	proxy        := flag.String("proxy", "", "Proxy URL (e.g. http://127.0.0.1:8080)")
	debugFlag    := flag.Bool("d", false, "Enable debug output to stderr")
	flag.Parse()

	if *urlsFile == "" || *wordlistFile == "" {
		fmt.Fprintf(os.Stderr, "[ERROR] Usage: %s -u /path/to/urls.txt -w /path/to/wordlist.txt [-t N] [-d] [-proxy URL]\n", os.Args[0])
		os.Exit(1)
	}
	debugMode = *debugFlag

	urls, err := readLines(*urlsFile)
	if err != nil {
		if debugMode {
			fmt.Fprintf(os.Stderr, "[DEBUG FROM main] %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "[ERROR] Usage: %s -u /path/to/urls.txt -w /path/to/wordlist.txt\n", os.Args[0])
		os.Exit(1)
	}
	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "[ERROR] URLs file is empty: %s\n", *urlsFile)
		os.Exit(1)
	}

	wordlist, err := readLines(*wordlistFile)
	if err != nil {
		if debugMode {
			fmt.Fprintf(os.Stderr, "[DEBUG FROM main] %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "[ERROR] Usage: %s -u /path/to/urls.txt -w /path/to/wordlist.txt\n", os.Args[0])
		os.Exit(1)
	}

	for i, u := range urls {
		urls[i] = strings.TrimRight(u, "/")
	}
	for i, w := range wordlist {
		if !strings.HasPrefix(w, "/") {
			wordlist[i] = "/" + w
		}
	}

	remaining.Store(int64(len(urls)))

	// Pre-fill a buffered channel so workers never block on the producer.
	urlCh := make(chan string, len(urls))
	for _, u := range urls {
		urlCh <- u
	}
	close(urlCh)

	n := *threads
	if n < 1 {
		fmt.Fprintf(os.Stderr, "[ERROR] -t must be at least 1\n")
		os.Exit(1)
	}

	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		if debugMode {
			fmt.Fprintf(os.Stderr, "[DEBUG FROM main] Starting goroutine %d...\n", i)
		}
		wg.Add(1)
		go worker(i, urlCh, wordlist, *proxy, &wg)
	}

	wg.Wait()
}
