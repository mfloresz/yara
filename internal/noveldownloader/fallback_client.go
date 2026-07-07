package noveldownloader

import (
	"context"
	"fmt"
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

	if c.checker != nil && c.checker.HasBrowserWorker() && isRetryableError(err) {
		proxy := c.checker.NewProxyHTTPClient()
		return proxy.Fetch(ctx, url)
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

	// Close original response if we're falling back
	if resp != nil {
		resp.Body.Close()
	}

	if c.checker != nil && c.checker.HasBrowserWorker() {
		if err != nil || (resp != nil && isRetryableStatusCode(resp.StatusCode)) {
			proxy := c.checker.NewProxyHTTPClient()
			return proxy.Do(req)
		}
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// isRetryableError checks if an error should trigger proxy fallback.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	// HTTP status codes
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

	// Cloudflare indicators
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
	case 403, 406, 429, 503:
		return true
	default:
		return false
	}
}
