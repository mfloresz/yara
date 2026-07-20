package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"translator-server/internal/noveldownloader"
)

// ProxyHTTPClient implements noveldownloader.HTTPClient by fetching pages
// through the Browser Worker extension. This allows the Go parsers to work
// on Cloudflare-protected sites without any site-specific JS in the extension.
//
// All calls are serialized through the browser job queue (EnqueueBrowserJob),
// which guarantees sequential execution — critical for Cloudflare challenge
// handling.
type ProxyHTTPClient struct {
	server *Server
	// userID scopes browser-worker jobs to the requesting user's own
	// connected worker. Passing an empty userID would let the job be
	// dispatched to ANY connected user's browser (routing the request through
	// their IP and site cookies), so callers must supply the owner's ID.
	userID string
}

func NewProxyHTTPClient(s *Server, userID string) *ProxyHTTPClient {
	return &ProxyHTTPClient{server: s, userID: userID}
}

func (c *ProxyHTTPClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	operation := "fetch_page"
	if isLivewireSite(url) {
		operation = "fetch_livewire"
	}
	slog.Info("proxyHTTP: fetching", "url", url, "operation", operation, "userID", c.userID)
	result, err := c.server.EnqueueBrowserJob(operation, url, nil, c.userID)
	if err != nil {
		slog.Error("proxyHTTP: browser job failed", "url", url, "error", err)
		return nil, fmt.Errorf("proxy fetch: %w", err)
	}
	html := getStringFromData(result.Data, "html")
	if html == "" {
		slog.Error("proxyHTTP: empty HTML returned", "url", url, "dataKeys", fmt.Sprintf("%v", result.Data))
		return nil, fmt.Errorf("proxy returned empty HTML for %s", url)
	}
	slog.Info("proxyHTTP: success", "url", url, "htmlLen", len(html), "hasFreeChapters", strings.Contains(html, "freeChapters"))
	return []byte(html), nil
}

// isLivewireSite returns true for novel info pages that load chapter lists
// via Livewire x-intersect lazy loading, requiring scroll+wait before
// extraction. Individual chapter pages do NOT need this — their content
// is server-rendered in the initial HTML.
func isLivewireSite(urlStr string) bool {
	if !strings.Contains(urlStr, "skydemonorder.com") {
		return false
	}
	// Match novel info pages: /projects/<id>-<slug>
	// Exclude chapter pages:   /projects/<id>-<slug>/<chapter>
	idx := strings.Index(urlStr, "/projects/")
	if idx == -1 {
		return false
	}
	rest := urlStr[idx+len("/projects/"):]
	// If there's no '/' after the project slug, it's the novel info page.
	return !strings.Contains(rest, "/")
}

func (c *ProxyHTTPClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
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

func (c *ProxyHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := c.Fetch(req.Context(), req.URL.String())
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
	}, nil
}

// Ensure interface compliance at compile time.
var _ interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
	FetchDocument(ctx context.Context, url string) (*goquery.Document, error)
	Do(req *http.Request) (*http.Response, error)
} = (*ProxyHTTPClient)(nil)

// BrowserWorkerCheckerImpl implements noveldownloader.BrowserWorkerChecker.
type BrowserWorkerCheckerImpl struct {
	server *Server
	userID string
}

func NewBrowserWorkerChecker(s *Server, userID string) *BrowserWorkerCheckerImpl {
	return &BrowserWorkerCheckerImpl{server: s, userID: userID}
}

func (c *BrowserWorkerCheckerImpl) HasBrowserWorker() bool {
	return c.server.HasBrowserWorkerForUser(c.userID)
}

func (c *BrowserWorkerCheckerImpl) NewProxyHTTPClient() noveldownloader.HTTPClient {
	return NewProxyHTTPClient(c.server, c.userID)
}

// Ensure interface compliance at compile time.
var _ noveldownloader.BrowserWorkerChecker = (*BrowserWorkerCheckerImpl)(nil)
