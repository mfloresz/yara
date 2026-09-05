package noveldownloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// wattpadStubClient serves canned GET payloads keyed by exact URL so parser
// tests run without hitting the live site.
type wattpadStubClient struct {
	getResponses map[string][]byte
}

func (c *wattpadStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	return nil, fmt.Errorf("no stub for %s (use Do)", url)
}

func (c *wattpadStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	return nil, fmt.Errorf("no stub for %s", url)
}

func (c *wattpadStubClient) Do(req *http.Request) (*http.Response, error) {
	body, ok := c.getResponses[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("no stub for %s %s", req.Method, req.URL)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

const wattpadStubStoryURL = "https://www.wattpad.com/story/207670289-mine"

// wattpadFailClient wraps wattpadStubClient but answers the part-text endpoint
// with 404, simulating a paywalled or deleted part.
type wattpadFailClient struct {
	getResponses map[string][]byte
}

func (c *wattpadFailClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	return nil, fmt.Errorf("no stub for %s (use Do)", url)
}

func (c *wattpadFailClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	return nil, fmt.Errorf("no stub for %s", url)
}

func (c *wattpadFailClient) Do(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.String(), "https://www.wattpad.com/apiv2/storytext") {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	}
	body, ok := c.getResponses[req.URL.String()]
	if !ok {
		// Unknown part metadata: answer as if the part existed so the test
		// reaches the text endpoint.
		if strings.HasPrefix(req.URL.String(), "https://www.wattpad.com/api/v3/story_parts/") {
			body = []byte(`{"id":999999999,"title":"Paid part","groupId":"207670289"}`)
		} else {
			return nil, fmt.Errorf("no stub for %s %s", req.Method, req.URL)
		}
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

const wattpadStubStoryJSON = `{"id":"207670289","title":"Mine","description":"A test description.",
"cover":"https://img.wattpad.com/cover/207670289-256-k283574.jpg","completed":true,
"url":"https://www.wattpad.com/story/207670289-mine",
"user":{"username":"KatNim"},"parts":[
{"id":811917197,"title":"Cast "},
{"id":811991190,"title":"Chapter 1"},
{"id":812889074,"title":"Chapter 2"}]}`

const wattpadStubPartJSON = `{"id":811991190,"title":"Chapter 1","groupId":"207670289"}`

const wattpadStubPartHTML = `<p data-p-id="b">Chapter one first paragraph.</p><p data-p-id="c">Chapter one second paragraph.</p>`

func wattpadStubResponses(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"https://www.wattpad.com/api/v3/stories/207670289?fields=" + wattpadStoryFields: []byte(wattpadStubStoryJSON),
		"https://www.wattpad.com/api/v3/story_parts/811991190?fields=id,title,groupId": []byte(wattpadStubPartJSON),
		"https://www.wattpad.com/apiv2/storytext?id=811991190":                         []byte(wattpadStubPartHTML),
	}
}

func TestWattpadCanHandle(t *testing.T) {
	p := NewWattpadParser()
	cases := []struct {
		url  string
		want bool
	}{
		{wattpadStubStoryURL, true},
		{"https://www.wattpad.com/story/207670289", true},
		{"https://wattpad.com/story/207670289-mine", true},
		{"https://www.wattpad.com/811991190-chapter-1", true},
		{"https://www.wattpad.com/811991190", true},
		{"https://www.wattpad.com/user/KatNim", false},
		{"https://www.wattpad.com/search/mine", false},
		{"https://example.com/story/207670289-mine", false},
		{"https://wattpad.com.attacker.example/811991190-x", false},
	}
	for _, c := range cases {
		if got := p.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestWattpadGetNovelInfo(t *testing.T) {
	p := NewWattpadParser()
	client := &wattpadStubClient{getResponses: wattpadStubResponses(t)}

	info, err := p.GetNovelInfo(context.Background(), client, wattpadStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "Mine" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "KatNim" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	if info.Description != "A test description." {
		t.Errorf("unexpected description: %q", info.Description)
	}
	// The 256px thumbnail must be upgraded to the 512px variant.
	if info.CoverURL != "https://img.wattpad.com/cover/207670289-512-k283574.jpg" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(info.Chapters))
	}
	first := info.Chapters[0]
	if first.Title != "Cast" {
		t.Errorf("unexpected first title: %q", first.Title)
	}
	if first.URL != "https://www.wattpad.com/811917197-cast" {
		t.Errorf("unexpected first URL: %q", first.URL)
	}
	if info.Chapters[1].Order != 2 || info.Chapters[2].Order != 3 {
		t.Errorf("unexpected chapter orders: %+v", info.Chapters)
	}
}

func TestWattpadGetNovelInfoFromPartURL(t *testing.T) {
	// A reading-page URL must resolve to the parent story via the
	// story_parts endpoint.
	p := NewWattpadParser()
	responses := wattpadStubResponses(t)
	responses["https://www.wattpad.com/api/v3/story_parts/811917197?fields=id,title,groupId"] =
		[]byte(`{"id":811917197,"title":"Cast","groupId":"207670289"}`)
	client := &wattpadStubClient{getResponses: responses}

	info, err := p.GetNovelInfo(context.Background(), client, "https://www.wattpad.com/811917197-cast")
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Mine" || len(info.Chapters) != 3 {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestWattpadGetChapterURLs(t *testing.T) {
	p := NewWattpadParser()
	client := &wattpadStubClient{getResponses: wattpadStubResponses(t)}

	chapters, err := p.GetChapterURLs(context.Background(), client, nil, wattpadStubStoryURL)
	if err != nil {
		t.Fatalf("GetChapterURLs: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	if chapters[1].URL != "https://www.wattpad.com/811991190-chapter-1" {
		t.Errorf("unexpected chapter URL: %q", chapters[1].URL)
	}
}

func TestWattpadParseChapter(t *testing.T) {
	p := NewWattpadParser()
	client := &wattpadStubClient{getResponses: wattpadStubResponses(t)}

	ch, err := p.ParseChapter(context.Background(), client, "https://www.wattpad.com/811991190-chapter-1")
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	for _, want := range []string{
		"Chapter one first paragraph.",
		"Chapter one second paragraph.",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestWattpadParseChapterMissingPart(t *testing.T) {
	// A part whose text endpoint answers 404 (paywalled/deleted) must fail
	// with an actionable error, not an empty chapter.
	p := NewWattpadParser()
	client := &wattpadFailClient{getResponses: wattpadStubResponses(t)}

	_, err := p.ParseChapter(context.Background(), client, "https://www.wattpad.com/999999999-paid-part")
	if err == nil {
		t.Fatal("expected error for missing part text, got nil")
	}
	if !strings.Contains(err.Error(), "paywalled") {
		t.Errorf("error should mention paywalled, got: %v", err)
	}
}

func TestWattpadParseChapterBadURL(t *testing.T) {
	p := NewWattpadParser()
	client := &wattpadStubClient{getResponses: wattpadStubResponses(t)}

	if _, err := p.ParseChapter(context.Background(), client, wattpadStubStoryURL); err == nil {
		t.Error("expected error for story page URL, got nil")
	}
	if _, err := p.ParseChapter(context.Background(), client, "https://example.com/811991190-x"); err == nil {
		t.Error("expected error for non-wattpad URL, got nil")
	}
}

func TestWattpadSlugify(t *testing.T) {
	cases := map[string]string{
		"Chapter 1":   "chapter-1",
		"Cast ":       "cast",
		"¡Hola, qué!": "hola-qu",
		"":            "",
	}
	for in, want := range cases {
		if got := wattpadSlugify(in); got != want {
			t.Errorf("wattpadSlugify(%q) = %q, want %q", in, got, want)
		}
	}
}
