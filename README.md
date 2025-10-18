# Methodos

High-performance HTTP methods testing tool written in Go. Tests various HTTP methods against a URL to find verb tampering vulnerabilities and dangerous enabled methods.

## Features

- **Fast**: Concurrent testing with configurable thread pool
- **Security**: Tests for dangerous HTTP methods (PUT, DELETE, etc.)
- **Rich output**: Color-coded results table
- **JSON export**: Save results for further analysis
- **Flexible**: Custom headers, cookies, proxy support


## Usage

Basic usage:
```bash
./httpmethods -url https://example.com/admin
```

With options:
```bash
./httpmethods -url https://example.com/admin \
  -t 10 \
  -k \
  -H "Authorization: Bearer token" \
  -b "session=abc123" \
  -j results.json
```

## Options

```
  -url string
        Target URL (required)
  -v int
        Verbosity (1=verbose, 2=debug)
  -q    Quiet mode
  -k    Allow insecure SSL
  -L    Follow redirects
  -s    Safe mode (skip dangerous methods)
  -w string
        Custom wordlist file
  -t int
        Threads (default: 5)
  -j string
        Save to JSON file
  -x string
        Proxy (e.g., http://localhost:8080)
  -b string
        Cookies (e.g., "session=abc;token=xyz")
  -H value
        Headers (can be repeated)
  -y    Auto-accept prompts
  -n    Auto-refuse prompts
```

## Examples

Test with custom methods:
```bash
./httpmethods -url https://api.example.com -w custom-methods.txt
```

Through proxy with debugging:
```bash
./httpmethods -url https://example.com -x http://localhost:8080 -v 2
```

Safe mode (auto-skip dangerous methods):
```bash
./httpmethods -url https://example.com -s -y
```

## License

GNU General Public License v3.0