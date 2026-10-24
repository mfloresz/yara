package comicdownloader

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// BrowserWorkerChecker is an interface to check if a browser worker is available.
type BrowserWorkerChecker interface {
	HasBrowserWorker() bool
	NewProxyHTTPClient() HTTPClient
}

// LazyFallbackClient wraps a direct HTTP client and lazily creates a proxy client
// when a retryable error occurs and a browser worker is available.
type LazyFallbackClient struct {
	direct  HTTPClient
	checker BrowserWorkerChecker
}

// NewLazyFallbackClient creates a client that tries direct HTTP first,
// then falls back to the browser worker proxy on retryable errors.
func NewLazyFallbackClient(direct HTTPClient, checker BrowserWorkerChecker) *LazyFallbackClient {
	return &LazyFallbackClient{
		direct:  direct,
		checker: checker,
	}
}

func (c *LazyFallbackClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, err := c.direct.Fetch(ctx, url)
	if err == nil {
		return body, nil
	}

	slog.Info("lazyFallback: direct fetch failed", "url", url, "error", err)
	if c.checker != nil && c.checker.HasBrowserWorker() && isRetryableError(err) {
		slog.Info("lazyFallback: falling back to proxy", "url", url)
		proxy := c.checker.NewProxyHTTPClient()
		result, proxyErr := proxy.Fetch(ctx, url)
		if proxyErr != nil {
			slog.Error("lazyFallback: proxy fetch failed", "url", url, "error", proxyErr)
			return nil, proxyErr
		}
		return result, nil
	}

	return nil, err
}

func (c *LazyFallbackClient) FetchJSON(ctx context.Context, url string) ([]byte, error) {
	body, err := c.direct.FetchJSON(ctx, url)
	if err == nil {
		return body, nil
	}
	slog.Info("lazyFallback: direct JSON fetch failed", "url", url, "error", err)
	if c.checker != nil && c.checker.HasBrowserWorker() && isRetryableError(err) {
		proxy := c.checker.NewProxyHTTPClient()
		return proxy.FetchJSON(ctx, url)
	}
	return nil, err
}

func (c *LazyFallbackClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}
	return doc, nil
}

func (c *LazyFallbackClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.direct.Do(req)
	if err == nil && resp.StatusCode < 400 {
		return resp, nil
	}

	shouldFallback := c.checker != nil && c.checker.HasBrowserWorker() &&
		(err != nil || (resp != nil && isRetryableStatusCode(resp.StatusCode)))

	if !shouldFallback {
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	if resp != nil {
		resp.Body.Close()
	}
	proxy := c.checker.NewProxyHTTPClient()
	return proxy.Do(req)
}

// isRetryableError checks if an error should trigger proxy fallback.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	if strings.Contains(errStr, "http 403") || strings.Contains(errStr, "forbidden") {
		return true
	}
	if strings.Contains(errStr, "http 406") {
		return true
	}
	if strings.Contains(errStr, "http 429") || strings.Contains(errStr, "too many") {
		return true
	}
	if strings.Contains(errStr, "http 503") || strings.Contains(errStr, "unavailable") {
		return true
	}
	if strings.Contains(errStr, "http 500") || strings.Contains(errStr, "http 502") || strings.Contains(errStr, "http 504") {
		return true
	}
	if strings.Contains(errStr, "http 408") {
		return true
	}

	// Transient transport failures (mid-body resets, timeouts): the direct
	// path is flaky right now, so a connected worker may succeed where the
	// datacenter connection keeps dropping.
	for _, s := range []string{
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"connection refused",
		"connection closed",
		"timeout",
		"timed out",
		"temporary",
		"try again",
	} {
		if strings.Contains(errStr, s) {
			return true
		}
	}

	if strings.Contains(errStr, "challenge") || strings.Contains(errStr, "captcha") {
		return true
	}
	if strings.Contains(errStr, "cloudflare") {
		return true
	}
	if strings.Contains(errStr, "just a moment") || strings.Contains(errStr, "checking your browser") {
		return true
	}
	if strings.Contains(errStr, "turnstile") || strings.Contains(errStr, "cf-") {
		return true
	}

	return false
}

// isRetryableStatusCode checks if an HTTP status code should trigger proxy fallback.
func isRetryableStatusCode(code int) bool {
	switch code {
	case 403, 406, 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}
