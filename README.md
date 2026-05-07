# FuryFuzzer

![Go](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat-square&logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)
![Concurrency](https://img.shields.io/badge/Concurrent-goroutines-blueviolet?style=flat-square)
![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=flat-square)
![Authorized Pentesting Only](https://img.shields.io/badge/⚠%EF%B8%8F%20Authorized%20Pentesting%20Only-critical?style=flat-square)

A fast, concurrent web directory and endpoint fuzzer written in Go.

---

## Table of Contents

- [Features](#features)
- [How It Works](#how-it-works)
- [Installation](#installation)
- [Usage](#usage)
  - [Examples](#examples)
- [Support](#support)
- [Formatting](#formatting)
  - [Input](#input)
  - [Output](#output)
- [Contributing](#contributing)
- [Attribution](#attribution)
- [Legal & Ethics](#legal--ethics)
- [License](#license)

---

## Features

- **Concurrent**: Configurable goroutine count. Each goroutine owns its own `http.Client` with a tuned transport for HTTP keep-alive and connection reuse.
- **Multi-target**: Supply a file of URLs and every target is fuzzed in the same run.
- **Retry logic**: Transient network failures trigger up to 3 automatic retries per path before moving on.
- **Proxy support**: Route traffic through any HTTP proxy via the `-proxy` flag.
- **Debug mode**: Verbose per-goroutine tracing to stderr and clean results to stdout, easy to pipe or redirect independently.
- **TLS flexibility**: SSL verification is disabled by default for testing self-signed certificates.
- **True parallelism**: `GOMAXPROCS` is set to `NumCPU` so goroutines are distributed across all hardware threads.

## How It Works

```
URLs file          Wordlist file
     │                   │
     ▼                   ▼
 Channel            in-memory slice
     │
     ├── Goroutine 1 ──► Client ──► isAlive? ──► enumerate paths ──► print [HTTP NNN] url
     ├── Goroutine 2 ──► Client ──► isAlive? ──► enumerate paths ──► print [HTTP NNN] url
     └── Goroutine N ──► Client ──► isAlive? ──► enumerate paths ──► print [HTTP NNN] url
```

1. All target URLs are loaded into a buffered channel at startup.
2. Each goroutine pulls a URL, checks if the host is reachable (2 attempts), then iterates the full wordlist, appending each word to the base URL and issuing a `GET` request.
3. Every response is printed as `[HTTP <status>] <url>`, regardless of status code, so you can filter results however you like (e.g. `grep "HTTP 200"`).
4. Goroutines share the wordlist (read-only) and a print mutex. All other state (HTTP client, connection pool) is local to the goroutine.

---

## Installation

**Requires Go 1.21+**

```bash
git clone https://github.com/whoamiamleo/FuryFuzzer.git
cd FuryFuzzer
go build -o furyfuzzer furyfuzzer.go
```

No external dependencies — the standard library only.

> **Optional:** install directly to your `$GOPATH/bin`:
> ```bash
> go install .
> ```

---

## Usage

```
./furyfuzzer -u <urls_file> -w <wordlist_file> [-t <goroutines>] [-d] [-proxy <url>]
```

| Flag | Required | Default | Description |
|---|---|---|---|
| `-u` | Yes | | Path to a file of target base URLs (one per line) |
| `-w` | Yes | | Path to a wordlist file (one path per line) |
| `-t` | No | `1` | Number of concurrent goroutines |
| `-d` | No | | Enable verbose debug output to stderr |
| `-proxy` | No | | Proxy URL (e.g. `http://127.0.0.1:8080`) |

### Examples

```bash
# Single target, single goroutine
./furyfuzzer -u urls.txt -w wordlist.txt

# Multiple targets, 10 goroutines
./furyfuzzer -u urls.txt -w wordlist.txt -t 10

# Filter for 200s only
./furyfuzzer -u urls.txt -w wordlist.txt -t 5 | grep "HTTP 200"

# Save results to file while watching live
./furyfuzzer -u urls.txt -w wordlist.txt -t 5 | tee results.txt

# Debug mode (results to stdout, debug to stderr)
./furyfuzzer -u urls.txt -w wordlist.txt -d 2>debug.log

# Route through Burp Suite
./furyfuzzer -u urls.txt -w wordlist.txt -t 10 -proxy http://127.0.0.1:8080
```

---

## Support

| Requirement | Version |
|---|---|
| Go | 1.21+ |
| macOS | ✅ |
| Linux | ✅ |
| Windows | ✅ |

No external dependencies. The standard library only.

## Formatting

### Input

**urls.txt**: one base URL per line, trailing slash included:

```
http://example.com/
http://staging.example.com/
https://api.example.com/v1/
```

**wordlist.txt**: one path segment per line. Entries with or without a leading `/` are both accepted:

```
index.html
/admin/
admin/login
/api/users
.env
.git/config
config.php
backup.zip
```

Empty lines in either file are ignored automatically.

### Output

All results go to **stdout**. Debug messages go to **stderr**. This separation makes it easy to pipe results into other tools without noise.

```
[HTTP 200] http://example.com/index.html
[HTTP 200] http://example.com/admin/
[HTTP 301] http://example.com/admin/login
[HTTP 403] http://example.com/.git/config
[HTTP 404] http://example.com/backup.zip
```

---

## Contributing

Contributions, issues, and feature requests are welcome. Feel free to check the [issues](https://github.com/whoamiamleo/FuryFuzzer/issues) page or submit a pull request.

## Attribution

If you use FuryFuzzer in a project or research, a mention or link back to this repository is appreciated.

- Author: Leopold von Niebelschuetz-Godlewski
- Repository: [https://github.com/whoamiamleo/FuryFuzzer](https://github.com/whoamiamleo/FuryFuzzer)
- License: MIT

---

## Legal & Ethics

FuryFuzzer is intended solely for authorized security testing and research activities. Any unauthorized use is strictly prohibited. The author assumes no responsibility for misuse or damage resulting from improper or unlawful use.

---

## License

MIT License

Copyright (c) 2026 Leopold von Niebelschuetz-Godlewski

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
