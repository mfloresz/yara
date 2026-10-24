package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"translator-server/internal/comicdownloader"
)

// ProxyHTTPClient implements comicdownloader.HTTPClient by fetching through
// the browser-worker extension, so page images on Cloudflare-protected sites
// still download when a worker is connected. Calls are serialized through the
// browser job queue (enqueueBrowserJob), which guarantees sequential
// execution — critical for Cloudflare challenge handling.
type ProxyHTTPClient struct {
	server *Server
	// userID scopes browser-worker jobs to the requesting user's own connected
	// worker. An empty userID would let the job be dispatched to ANY connected
	// user's browser, so callers must supply the owner's ID.
	userID string
}

func NewProxyHTTPClient(s *Server, userID string) *ProxyHTTPClient {
	return &ProxyHTTPClient{server: s, userID: userID}
}

func (c *ProxyHTTPClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	result, err := c.server.enqueueBrowserJob("fetch_page", url, nil, c.userID)
	if err != nil {
		return nil, fmt.Errorf("proxy fetch: %w", err)
	}
	html := getStringFromData(result.Data, "html")
	if html == "" {
		return nil, fmt.Errorf("proxy returned empty HTML for %s", url)
	}
	return []byte(html), nil
}

// FetchJSON is the worker-relay shape of Fetch; comic image CDNs answer over
// plain HTTP, so this path only runs after the direct fetch already failed.
func (c *ProxyHTTPClient) FetchJSON(ctx context.Context, url string) ([]byte, error) {
	return c.Fetch(ctx, url)
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

// Do serves binary image downloads (covers, chapter pages) through the
// browser worker. It must use the fetch_image operation, which returns the
// real bytes base64-encoded: fetch_page navigates a tab to the URL and only
// yields the viewer's DOM wrapper, which the downloader rejects as "not a
// valid image".
func (c *ProxyHTTPClient) Do(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	result, err := c.server.enqueueBrowserJob("fetch_image", url, map[string]interface{}{"timeout": 60}, c.userID)
	if err != nil {
		return nil, fmt.Errorf("proxy image fetch: %w", err)
	}
	b64 := getStringFromData(result.Data, "dataBase64")
	if b64 == "" {
		return nil, fmt.Errorf("proxy returned no image data for %s", url)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decoding proxied image: %w", err)
	}
	// Same 25MB hard cap the downloader enforces on direct image bodies: the
	// payload arrives base64-encoded over the worker WebSocket, so bound it
	// here before it becomes a response body.
	const maxProxiedImageBytes = int64(25 << 20)
	if int64(len(data)) > maxProxiedImageBytes {
		return nil, fmt.Errorf("proxied image exceeds %d bytes", maxProxiedImageBytes)
	}
	contentType := getStringFromData(result.Data, "contentType")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	slog.Info("proxied comic page image through browser worker", "url", url, "bytes", len(data), "userID", c.userID)
	header := make(http.Header)
	header.Set("Content-Type", contentType)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     header,
	}, nil
}

// BrowserWorkerCheckerImpl implements comicdownloader.BrowserWorkerChecker:
// the lazy fallback client asks it whether a worker is connected before
// relaying a failed direct fetch.
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

func (c *BrowserWorkerCheckerImpl) NewProxyHTTPClient() comicdownloader.HTTPClient {
	return NewProxyHTTPClient(c.server, c.userID)
}

var _ comicdownloader.BrowserWorkerChecker = (*BrowserWorkerCheckerImpl)(nil)
