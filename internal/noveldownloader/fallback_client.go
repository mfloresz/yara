package noveldownloader

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
	hasWorker := c.checker != nil && c.checker.HasBrowserWorker()

	// SkyDemonOrder project pages load their chapter catalog through Livewire.
	// A direct HTTP response can be 200 while still containing only the page
	// shell, so prefer the browser when it is available instead of relying on
	// an HTTP status code to decide whether JavaScript was needed.
	if IsSkyDemonOrderProjectURL(url) && hasWorker {
		slog.Info("lazyFallback: using browser for dynamic project page", "url", url)
		proxy := c.checker.NewProxyHTTPClient()
		result, proxyErr := proxy.Fetch(ctx, url)
		if proxyErr == nil && hasSkyDemonOrderChapterCatalog(url, result) {
			slog.Info("lazyFallback: browser fetch succeeded", "url", url, "bodyLen", len(result))
			return result, nil
		}
		if proxyErr == nil {
			proxyErr = fmt.Errorf("rendered project page lacks freeChapters")
		}
		if proxyErr != nil {
			slog.Warn("lazyFallback: browser fetch failed, trying direct fetch", "url", url, "error", proxyErr)
		}
	}

	body, err := c.direct.Fetch(ctx, url)
	if err == nil {
		// Detect the known 200-but-not-rendered response for SkyDemonOrder. If
		// the browser is available, retry the project page through Livewire
		// before the parser falls back to walking every chapter.
		if IsSkyDemonOrderProjectURL(url) && hasWorker && !strings.Contains(string(body), "freeChapters") {
			slog.Info("lazyFallback: direct project page lacks chapter catalog, retrying in browser", "url", url)
			proxy := c.checker.NewProxyHTTPClient()
			result, proxyErr := proxy.Fetch(ctx, url)
			if proxyErr == nil && hasSkyDemonOrderChapterCatalog(url, result) {
				slog.Info("lazyFallback: browser retry succeeded", "url", url, "bodyLen", len(result))
				return result, nil
			}
			if proxyErr == nil {
				proxyErr = fmt.Errorf("rendered project page lacks freeChapters")
			}
			if proxyErr != nil {
				slog.Warn("lazyFallback: browser retry failed, keeping direct response", "url", url, "error", proxyErr)
			}
		}
		slog.Debug("lazyFallback: direct fetch succeeded", "url", url, "bodyLen", len(body))
		return body, nil
	}

	slog.Info("lazyFallback: direct fetch failed", "url", url, "error", err)
	if hasWorker && isRetryableError(err) {
		slog.Info("lazyFallback: falling back to proxy", "url", url)
		proxy := c.checker.NewProxyHTTPClient()
		result, proxyErr := proxy.Fetch(ctx, url)
		if proxyErr != nil {
			slog.Error("lazyFallback: proxy fetch failed", "url", url, "error", proxyErr)
			return nil, proxyErr
		}
		slog.Info("lazyFallback: proxy fetch succeeded", "url", url, "bodyLen", len(result))
		return result, nil
	}

	slog.Warn("lazyFallback: no fallback available", "url", url, "hasWorker", hasWorker, "retryable", isRetryableError(err))
	return nil, err
}

func hasSkyDemonOrderChapterCatalog(url string, body []byte) bool {
	return IsSkyDemonOrderProjectURL(url) && strings.Contains(string(body), "freeChapters: JSON.parse(")
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

	// Decide whether to fall back to the proxy before touching resp.Body, so
	// that when we do NOT fall back we hand the caller an untouched response
	// (with a readable body) instead of one whose body we already closed.
	shouldFallback := c.checker != nil && c.checker.HasBrowserWorker() &&
		(err != nil || (resp != nil && isRetryableStatusCode(resp.StatusCode)))

	if !shouldFallback {
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Falling back: discard the original response and retry via the proxy.
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
