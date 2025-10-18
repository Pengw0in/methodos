package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const banner = `
 _____ ______   _______  _________  ___  ___  ________  ________  ________  ________      
|\   _ \  _   \|\  ___ \|\___   ___\\  \|\  \|\   __  \|\   ___ \|\   __  \|\   ____\     
\ \  \\\__\ \  \ \   __/\|___ \  \_\ \  \\\  \ \  \|\  \ \  \_|\ \ \  \|\  \ \  \___|_    
 \ \  \\|__| \  \ \  \_|/__  \ \  \ \ \   __  \ \  \\\  \ \  \ \\ \ \  \\\  \ \_____  \   
  \ \  \    \ \  \ \  \_|\ \  \ \  \ \ \  \ \  \ \  \\\  \ \  \_\\ \ \  \\\  \|____|\  \  
   \ \__\    \ \__\ \_______\  \ \__\ \ \__\ \__\ \_______\ \_______\ \_______\____\_\  \ 
    \|__|     \|__|\|_______|   \|__|  \|__|\|__|\|_______|\|_______|\|_______|\_________\
                                                                              \|_________|                                                                               
`

var defaultMethods = []string{
	"CHECKIN", "CHECKOUT", "CONNECT", "COPY", "DELETE", "GET", "HEAD", "INDEX",
	"LINK", "LOCK", "MKCOL", "MOVE", "NOEXISTE", "OPTIONS", "ORDERPATCH",
	"PATCH", "POST", "PROPFIND", "PROPPATCH", "PUT", "REPORT", "SEARCH",
	"SHOWMETHOD", "SPACEJUMP", "TEXTSEARCH", "TRACE", "TRACK", "UNCHECKOUT",
	"UNLINK", "UNLOCK", "VERSIONCONTROL", "BAMBOOZLE",
}

var dangerousMethods = map[string]bool{
	"DELETE": true, "COPY": true, "PUT": true, "PATCH": true, "UNCHECKOUT": true,
}

type Config struct {
	URL        string
	Verbosity  int
	Quiet      bool
	Insecure   bool
	Redirect   bool
	Safe       bool
	Wordlist   string
	Threads    int
	JSONFile   string
	Proxy      string
	Cookies    string
	Headers    headerFlags
	AutoAccept bool
	AutoRefuse bool
}

type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ",")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

type Result struct {
	StatusCode int    `json:"status_code"`
	Length     int    `json:"length"`
	Reason     string `json:"reason"`
}

type Logger struct {
	verbosity int
	quiet     bool
}

func NewLogger(verbosity int, quiet bool) *Logger {
	return &Logger{verbosity: verbosity, quiet: quiet}
}

func (l *Logger) Debug(msg string) {
	if l.verbosity >= 2 {
		fmt.Printf("\033[33m[DEBUG]\033[0m %s\n", msg)
	}
}

func (l *Logger) Verbose(msg string) {
	if l.verbosity >= 1 {
		fmt.Printf("\033[34m[VERBOSE]\033[0m %s\n", msg)
	}
}

func (l *Logger) Info(msg string) {
	if !l.quiet {
		fmt.Printf("\033[1;34m[*]\033[0m %s\n", msg)
	}
}

func (l *Logger) Success(msg string) {
	if !l.quiet {
		fmt.Printf("\033[1;32m[+]\033[0m %s\n", msg)
	}
}

func (l *Logger) Warning(msg string) {
	if !l.quiet {
		fmt.Printf("\033[1;33m[-]\033[0m %s\n", msg)
	}
}

func (l *Logger) Error(msg string) {
	if !l.quiet {
		fmt.Printf("\033[1;31m[!]\033[0m %s\n", msg)
	}
}

func main() {
	fmt.Print(banner)

	config := parseFlags()
	logger := NewLogger(config.Verbosity, config.Quiet)

	if config.AutoAccept && config.AutoRefuse {
		logger.Error("Cannot use both --auto-accept and --auto-refuse")
		os.Exit(1)
	}

	client := createHTTPClient(config)
	headers := parseHeaders(config.Headers)
	cookies := parseCookies(config.Cookies)

	methods := getMethods(config, logger, client, headers, cookies)
	methods = filterDangerousMethods(config, logger, methods)

	results := testMethods(config, logger, client, methods, headers, cookies)
	printResults(logger, results)

	if config.JSONFile != "" {
		saveJSON(logger, results, config.JSONFile)
	}
}

func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.URL, "url", "", "Target URL (e.g., https://example.com:port/path)")
	flag.IntVar(&config.Verbosity, "v", 0, "Verbosity level (1=verbose, 2=debug)")
	flag.BoolVar(&config.Quiet, "q", false, "Quiet mode")
	flag.BoolVar(&config.Insecure, "k", false, "Allow insecure SSL connections")
	flag.BoolVar(&config.Redirect, "L", false, "Follow redirects")
	flag.BoolVar(&config.Safe, "s", false, "Use only safe methods")
	flag.StringVar(&config.Wordlist, "w", "", "Custom wordlist file")
	flag.IntVar(&config.Threads, "t", 5, "Number of concurrent threads")
	flag.StringVar(&config.JSONFile, "j", "", "Save results to JSON file")
	flag.StringVar(&config.Proxy, "x", "", "Proxy URL (e.g., http://localhost:8080)")
	flag.StringVar(&config.Cookies, "b", "", "Cookies (e.g., 'cookie1=value1;cookie2=value2')")
	flag.Var(&config.Headers, "H", "Custom headers (can be used multiple times)")
	flag.BoolVar(&config.AutoAccept, "y", false, "Auto accept prompts")
	flag.BoolVar(&config.AutoRefuse, "n", false, "Auto refuse prompts")

	flag.Parse()

	if config.URL == "" {
		fmt.Println("Error: URL is required")
		flag.Usage()
		os.Exit(1)
	}

	return config
}

func createHTTPClient(config *Config) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: config.Insecure,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if config.Proxy != "" {
		proxyURL, err := url.Parse(config.Proxy)
		if err != nil {
			fmt.Printf("Error parsing proxy URL: %v\n", err)
			os.Exit(1)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !config.Redirect {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func parseHeaders(headerFlags []string) map[string]string {
	headers := make(map[string]string)
	for _, h := range headerFlags {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return headers
}

func parseCookies(cookieStr string) map[string]string {
	cookies := make(map[string]string)
	if cookieStr == "" {
		return cookies
	}
	for _, pair := range strings.Split(cookieStr, ";") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			cookies[parts[0]] = parts[1]
		}
	}
	return cookies
}

func getMethods(config *Config, logger *Logger, client *http.Client, headers, cookies map[string]string) []string {
	methods := make([]string, len(defaultMethods))
	copy(methods, defaultMethods)

	if config.Wordlist != "" {
		methods = append(methods, loadWordlist(config.Wordlist, logger)...)
	}

	optionsMethods := getOptionsFromServer(config, logger, client, headers, cookies)
	methods = append(methods, optionsMethods...)

	// Deduplicate and uppercase
	methodSet := make(map[string]bool)
	for _, m := range methods {
		methodSet[strings.ToUpper(m)] = true
	}

	uniqueMethods := make([]string, 0, len(methodSet))
	for m := range methodSet {
		uniqueMethods = append(uniqueMethods, m)
	}
	sort.Strings(uniqueMethods)

	return uniqueMethods
}

func loadWordlist(filename string, logger *Logger) []string {
	logger.Verbose(fmt.Sprintf("Loading wordlist from %s", filename))
	file, err := os.Open(filename)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to open wordlist: %v", err))
		return []string{}
	}
	defer file.Close()

	var methods []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		method := strings.TrimSpace(scanner.Text())
		if method != "" {
			methods = append(methods, method)
		}
	}
	return methods
}

func getOptionsFromServer(config *Config, logger *Logger, client *http.Client, headers, cookies map[string]string) []string {
	logger.Verbose("Checking OPTIONS method on server")

	req, err := http.NewRequest("OPTIONS", config.URL, nil)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create OPTIONS request: %v", err))
		return []string{}
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Verbose(fmt.Sprintf("OPTIONS request failed: %v", err))
		return []string{}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		logger.Verbose("URL accepts OPTIONS")
		if allow := resp.Header.Get("Allow"); allow != "" {
			logger.Info(fmt.Sprintf("Server reports allowed methods: %s", allow))

			includeOptions := askUser(config, logger, "Do you want to add these methods to the test? [Y/n] ")
			if includeOptions {
				methods := strings.Split(strings.ReplaceAll(allow, " ", ""), ",")
				logger.Debug(fmt.Sprintf("Adding %d methods from OPTIONS", len(methods)))
				return methods
			}
		}
	}
	return []string{}
}

func filterDangerousMethods(config *Config, logger *Logger, methods []string) []string {
	if config.Safe {
		var filtered []string
		for _, method := range methods {
			if !dangerousMethods[method] {
				filtered = append(filtered, method)
			}
		}
		logger.Verbose(fmt.Sprintf("Filtered out %d dangerous methods", len(methods)-len(filtered)))
		return filtered
	}

	// Collect all dangerous methods first
	dangerousToTest := []string{}
	safeToTest := []string{}

	for _, method := range methods {
		if dangerousMethods[method] {
			dangerousToTest = append(dangerousToTest, method)
		} else {
			safeToTest = append(safeToTest, method)
		}
	}

	if len(dangerousToTest) == 0 {
		return methods
	}

	// Single prompt for all dangerous methods
	fmt.Printf("\n\033[1;33m[!]\033[0m Found %d dangerous methods: \033[1;31m%s\033[0m\n",
		len(dangerousToTest), strings.Join(dangerousToTest, ", "))
	fmt.Println("    These methods may modify or delete data on the server!")

	msg := "Do you want to test these dangerous methods? [y/N] "
	if askUser(config, logger, msg) {
		return methods
	}

	logger.Warning("Skipping dangerous methods")
	return safeToTest
}

func askUser(config *Config, logger *Logger, prompt string) bool {
	if config.AutoAccept {
		return true
	}
	if config.AutoRefuse {
		return false
	}

	fmt.Printf("\033[1;33m[?]\033[0m %s", prompt)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes" || response == ""
}

func testMethods(config *Config, logger *Logger, client *http.Client, methods []string, headers, cookies map[string]string) map[string]Result {
	logger.Info(fmt.Sprintf("Starting HTTP method enumeration (%d methods to test)", len(methods)))

	results := make(map[string]Result)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Progress tracking
	var completed int32 = 0
	total := len(methods)

	semaphore := make(chan struct{}, config.Threads)

	for _, method := range methods {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result := testMethod(config, logger, client, m, headers, cookies)
			mu.Lock()
			results[m] = result
			mu.Unlock()

			// Update progress
			current := atomic.AddInt32(&completed, 1)
			if !config.Quiet {
				fmt.Printf("\r\033[K\033[1;34m[*]\033[0m Progress: %d/%d methods tested (%.1f%%)",
					current, total, float64(current)/float64(total)*100)
			}
		}(method)
	}

	wg.Wait()
	if !config.Quiet {
		fmt.Println() // New line after progress
	}
	return results
}

func testMethod(config *Config, logger *Logger, client *http.Client, method string, headers, cookies map[string]string) Result {
	var lastErr error
	maxRetries := 2

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(method, config.URL, nil)
		if err != nil {
			logger.Debug(fmt.Sprintf("Failed to create request for %s: %v", method, err))
			return Result{StatusCode: 0, Length: 0, Reason: "Request creation failed"}
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}
		for k, v := range cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(time.Millisecond * 100 * time.Duration(attempt+1))
				continue
			}
			logger.Debug(fmt.Sprintf("Request failed for %s after %d attempts: %v", method, maxRetries+1, err))
			return Result{StatusCode: 0, Length: 0, Reason: fmt.Sprintf("Failed after %d retries", maxRetries+1)}
		}
		defer resp.Body.Close()

		// Read body to get length but don't store it
		bodyLen := 0
		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			bodyLen += n
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
		}

		reason := resp.Status
		if len(reason) > 100 {
			reason = reason[:100]
		}

		logger.Debug(fmt.Sprintf("%s: %d, %d bytes, %s", method, resp.StatusCode, bodyLen, reason))

		return Result{
			StatusCode: resp.StatusCode,
			Length:     bodyLen,
			Reason:     reason,
		}
	}

	return Result{StatusCode: 0, Length: 0, Reason: lastErr.Error()}
}

func printResults(logger *Logger, results map[string]Result) {
	logger.Verbose("Printing results")

	// Get sorted method names
	methods := make([]string, 0, len(results))
	for method := range results {
		methods = append(methods, method)
	}
	sort.Strings(methods)

	// Print table header
	fmt.Println("\n┌────────────────┬─────────┬─────────────┬──────────────────────────────────────────┐")
	fmt.Printf("│ %-14s │ %-7s │ %-11s │ %-40s │\n", "Method", "Length", "Status Code", "Reason")
	fmt.Println("├────────────────┼─────────┼─────────────┼──────────────────────────────────────────┤")

	for _, method := range methods {
		result := results[method]
		color := getColorForStatus(result.StatusCode)
		reason := result.Reason
		if len(reason) > 40 {
			reason = reason[:37] + "..."
		}
		fmt.Printf("│ %s%-14s\033[0m │ %-7d │ %-11d │ %-40s │\n",
			color, method, result.Length, result.StatusCode, reason)
	}

	fmt.Println("└────────────────┴─────────┴─────────────┴──────────────────────────────────────────┘")
}

func getColorForStatus(status int) string {
	switch {
	case status == 200:
		return "\033[32m" // Green
	case status >= 300 && status < 400:
		return "\033[36m" // Cyan
	case status >= 400 && status < 500:
		return "\033[31m" // Red
	case status >= 500 && status < 600 && status != 502:
		return "\033[33m" // Orange/Yellow
	case status == 502:
		return "\033[93m" // Bright yellow
	default:
		return "\033[0m" // Default
	}
}

func saveJSON(logger *Logger, results map[string]Result, filename string) {
	logger.Verbose(fmt.Sprintf("Saving results to %s", filename))

	file, err := os.Create(filename)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create JSON file: %v", err))
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(results); err != nil {
		logger.Error(fmt.Sprintf("Failed to write JSON: %v", err))
		return
	}

	logger.Success(fmt.Sprintf("Results saved to %s", filename))
}
