package noveldownloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// debugProxyClient talks to the local debug-proxy (cmd/debug-proxy) which
// relays requests through a connected browser-worker-debug extension. It is
// used only by tests that exercise Cloudflare-protected parsers against the
// live site.
type debugProxyClient struct {
	baseURL string
	client  *http.Client
}

func newDebugProxyClient(baseURL string) *debugProxyClient {
	return &debugProxyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *debugProxyClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	endpoint := "/api/proxy/fetch"
	timeout := 120
	// Only use fetch_livewire for novel info pages (not chapter pages).
	if strings.Contains(url, "skydemonorder.com") {
		idx := strings.Index(url, "/projects/")
		if idx != -1 {
			rest := url[idx+len("/projects/"):]
			if !strings.Contains(rest, "/") {
				endpoint = "/api/proxy/livewire"
				timeout = 180
			}
		}
	}
	payload, err := json.Marshal(map[string]any{"url": url, "timeout": timeout})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("proxy returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			HTML string `json:"html"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status != "ok" {
		return nil, fmt.Errorf("proxy returned status %q: %s", envelope.Status, string(body))
	}
	return []byte(envelope.Data.HTML), nil
}

func (c *debugProxyClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *debugProxyClient) Do(req *http.Request) (*http.Response, error) {
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

func TestRealSkyDemonOrderViaProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real URL test in short mode")
	}
	proxyBase := envOr("SDO_DEBUG_PROXY", "http://localhost:5177")
	client := newDebugProxyClient(proxyBase)
	if _, err := client.Fetch(context.Background(), "https://skydemonorder.com/"); err != nil {
		t.Skipf("debug proxy not reachable at %s: %v", proxyBase, err)
	}

	pageURL := "https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect"
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	p := NewSkyDemonOrderParser()
	info, err := p.GetNovelInfo(ctx, client, pageURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	t.Logf("title=%q", info.Title)
	t.Logf("coverURL=%q", info.CoverURL)
	d := info.Description
	if len(d) > 200 {
		d = d[:200] + "..."
	}
	t.Logf("description=%q", d)
	if len(info.Chapters) == 0 {
		t.Fatalf("no chapters found")
	}
	t.Logf("totalChapters=%d", len(info.Chapters))
	for i, ch := range info.Chapters {
		if i < 5 {
			t.Logf("  ch%d: ep=%q url=%s", i+1, ch.Title, ch.URL)
		}
		if i == len(info.Chapters)-1 && len(info.Chapters) > 5 {
			t.Logf("  ...")
			t.Logf("  ch%d: ep=%q url=%s", i+1, ch.Title, ch.URL)
		}
	}
}

func TestRealSkyDemonOrderChapterViaProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real URL test in short mode")
	}
	proxyBase := envOr("SDO_DEBUG_PROXY", "http://localhost:5177")
	client := newDebugProxyClient(proxyBase)
	if _, err := client.Fetch(context.Background(), "https://skydemonorder.com/"); err != nil {
		t.Skipf("debug proxy not reachable at %s: %v", proxyBase, err)
	}

	url := "https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect/1-what-the-hell-is-this-situation-1"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chapter, err := NewSkyDemonOrderParser().ParseChapter(ctx, client, url)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	t.Logf("title=%q contentLen=%d", chapter.Title, len(chapter.Content))
	if chapter.Title == "" {
		t.Errorf("empty title")
	}
	if len(chapter.Content) < 100 {
		t.Errorf("content too short: %d bytes", len(chapter.Content))
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
