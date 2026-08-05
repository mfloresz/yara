package noveldownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

type fallbackTestClient struct {
	fetchResults []fallbackFetchResult
	fetchCalls   int
}

type fallbackFetchResult struct {
	body string
	err  error
}

func (c *fallbackTestClient) Fetch(context.Context, string) ([]byte, error) {
	c.fetchCalls++
	if len(c.fetchResults) == 0 {
		return nil, fmt.Errorf("no test fetch result configured")
	}
	result := c.fetchResults[0]
	c.fetchResults = c.fetchResults[1:]
	return []byte(result.body), result.err
}

func (c *fallbackTestClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *fallbackTestClient) Do(req *http.Request) (*http.Response, error) {
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

type fallbackTestChecker struct {
	hasWorker  bool
	proxy      HTTPClient
	proxyCalls int
}

func (c *fallbackTestChecker) HasBrowserWorker() bool { return c.hasWorker }

func (c *fallbackTestChecker) NewProxyHTTPClient() HTTPClient {
	c.proxyCalls++
	return c.proxy
}

func TestLazyFallbackClientUsesBrowserForSkyDemonOrderProject(t *testing.T) {
	direct := &fallbackTestClient{fetchResults: []fallbackFetchResult{{body: "direct"}}}
	proxy := &fallbackTestClient{fetchResults: []fallbackFetchResult{{body: "freeChapters: JSON.parse('[{}]')"}}}
	checker := &fallbackTestChecker{hasWorker: true, proxy: proxy}
	client := NewLazyFallbackClient(direct, checker)

	body, err := client.Fetch(context.Background(), "https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(body) != "freeChapters: JSON.parse('[{}]')" {
		t.Fatalf("Fetch() body = %q, want browser response", body)
	}
	if direct.fetchCalls != 0 {
		t.Fatalf("direct fetch calls = %d, want 0", direct.fetchCalls)
	}
	if checker.proxyCalls != 1 {
		t.Fatalf("proxy client calls = %d, want 1", checker.proxyCalls)
	}
}

func TestLazyFallbackClientRetries200ShellThroughBrowser(t *testing.T) {
	direct := &fallbackTestClient{fetchResults: []fallbackFetchResult{{body: "project shell"}}}
	proxy := &fallbackTestClient{fetchResults: []fallbackFetchResult{
		{err: fmt.Errorf("browser temporarily unavailable")},
		{body: "rendered freeChapters: JSON.parse('[{}]') catalog"},
	}}
	checker := &fallbackTestChecker{hasWorker: true, proxy: proxy}
	client := NewLazyFallbackClient(direct, checker)

	body, err := client.Fetch(context.Background(), "https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(body) != "rendered freeChapters: JSON.parse('[{}]') catalog" {
		t.Fatalf("Fetch() body = %q, want rendered browser retry", body)
	}
	if direct.fetchCalls != 1 {
		t.Fatalf("direct fetch calls = %d, want 1", direct.fetchCalls)
	}
	if checker.proxyCalls != 2 {
		t.Fatalf("proxy client calls = %d, want 2", checker.proxyCalls)
	}
}

func TestLazyFallbackClientKeepsDirectChapterFetch(t *testing.T) {
	direct := &fallbackTestClient{fetchResults: []fallbackFetchResult{{body: "chapter html"}}}
	proxy := &fallbackTestClient{fetchResults: []fallbackFetchResult{{body: "browser chapter html"}}}
	checker := &fallbackTestChecker{hasWorker: true, proxy: proxy}
	client := NewLazyFallbackClient(direct, checker)

	body, err := client.Fetch(context.Background(), "https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect/1-first-chapter")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(body) != "chapter html" {
		t.Fatalf("Fetch() body = %q, want direct chapter response", body)
	}
	if direct.fetchCalls != 1 || checker.proxyCalls != 0 {
		t.Fatalf("direct calls = %d, proxy calls = %d; want 1 and 0", direct.fetchCalls, checker.proxyCalls)
	}
}

func TestIsSkyDemonOrderProjectURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect", true},
		{"https://www.skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect/", true},
		{"https://skydemonorder.com/projects/3801994495-return-of-the-mount-hua-sect/1-first-chapter", false},
		{"https://example.com/projects/3801994495-return-of-the-mount-hua-sect", false},
	}
	for _, tt := range tests {
		if got := IsSkyDemonOrderProjectURL(tt.url); got != tt.want {
			t.Errorf("IsSkyDemonOrderProjectURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
