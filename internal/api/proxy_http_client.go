package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ProxyHTTPClient implements noveldownloader.HTTPClient by fetching pages
// through the Browser Worker extension. This allows the Go parsers to work
// on Cloudflare-protected sites without any site-specific JS in the extension.
type ProxyHTTPClient struct {
	server *Server
}

func NewProxyHTTPClient(s *Server) *ProxyHTTPClient {
	return &ProxyHTTPClient{server: s}
}

func (c *ProxyHTTPClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	result, err := c.server.fetchViaBrowserWorker(url, 120)
	if err != nil {
		return nil, fmt.Errorf("proxy fetch: %w", err)
	}
	if result.HTML == "" {
		return nil, fmt.Errorf("proxy returned empty HTML for %s", url)
	}
	return []byte(result.HTML), nil
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
