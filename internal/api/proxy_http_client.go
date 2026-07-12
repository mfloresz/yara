package api

import (
	"context"
	"fmt"
	"io"
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
	result, err := c.server.EnqueueBrowserJob("fetch_page", url, nil, c.userID)
	if err != nil {
		return nil, fmt.Errorf("proxy fetch: %w", err)
	}
	html := getStringFromData(result.Data, "html")
	if html == "" {
		return nil, fmt.Errorf("proxy returned empty HTML for %s", url)
	}
	return []byte(html), nil
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
