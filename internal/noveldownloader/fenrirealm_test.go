package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// fenrirStubClient returns canned API payloads keyed by URL so parser tests
// run without hitting the live site.
type fenrirStubClient struct {
	responses map[string]string
}

func (c *fenrirStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *fenrirStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *fenrirStubClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("Do not implemented in stub")
}

const fenrirStubMeta = `{
	"id": 1,
	"title": "Absolute Regression",
	"slug": "absolute-regression",
	"description": "Test description",
	"cover": "/media/cover.jpg",
	"user": {"username": "Fenrirtl"}
}`

// fenrirStubChapters mixes free and premium chapters. Chapter 2 and chapter 4
// are premium (locked.price > 0) and must be excluded from the chapter list.
const fenrirStubChapters = `[
	{"id":1,"slug":"1","name":"Chapter 1","title":"One","number":1,"type":"text","locked":{"price":0,"unlocked_at":"2025-02-24T21:46:04.000000Z","is_read_only":false}},
	{"id":2,"slug":"2","name":"Chapter 2","title":"Two","number":2,"type":"text","locked":{"price":15,"unlocked_at":null,"is_read_only":false}},
	{"id":3,"slug":"3","name":"Chapter 3","title":"Three","number":3,"type":"text","locked":{"price":0,"unlocked_at":"2025-02-24T21:46:04.000000Z","is_read_only":false}},
	{"id":4,"slug":"4","name":"Chapter 4","title":"Four","number":4,"type":"text","locked":{"price":20,"unlocked_at":null,"is_read_only":false}},
	{"id":5,"slug":"5","name":"Chapter 5","title":"Five","number":5,"type":"text","locked":{"price":0,"unlocked_at":"2025-02-24T21:46:04.000000Z","is_read_only":false}}
]`

func fenrirStubURLs() map[string]string {
	return map[string]string{
		"https://fenrirealm.com/api/new/v2/series/absolute-regression":          fenrirStubMeta,
		"https://fenrirealm.com/api/new/v2/series/absolute-regression/chapters": fenrirStubChapters,
	}
}

func TestFenrirRealmGetNovelInfoSkipsPremiumChapters(t *testing.T) {
	p := NewFenrirRealmParser()
	client := &fenrirStubClient{responses: fenrirStubURLs()}

	info, err := p.GetNovelInfo(context.Background(), client, "https://fenrirealm.com/series/absolute-regression")
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 free chapters, got %d", len(info.Chapters))
	}
	for _, ch := range info.Chapters {
		if strings.Contains(ch.Title, "Two") || strings.Contains(ch.Title, "Four") {
			t.Errorf("premium chapter included in list: %q", ch.Title)
		}
	}
	// Order must be preserved.
	if info.Chapters[0].Title != "Chapter 1" || info.Chapters[1].Title != "Chapter 3" || info.Chapters[2].Title != "Chapter 5" {
		t.Errorf("chapter order/selection wrong: %v", info.Chapters)
	}
}

func TestFenrirRealmGetChapterURLsSkipsPremiumChapters(t *testing.T) {
	p := NewFenrirRealmParser()
	client := &fenrirStubClient{responses: fenrirStubURLs()}

	chapters, err := p.GetChapterURLs(context.Background(), client, nil, "https://fenrirealm.com/series/absolute-regression")
	if err != nil {
		t.Fatalf("GetChapterURLs: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected 3 free chapters, got %d", len(chapters))
	}
	for _, ch := range chapters {
		if !strings.HasSuffix(ch.URL, "/1") && !strings.HasSuffix(ch.URL, "/3") && !strings.HasSuffix(ch.URL, "/5") {
			t.Errorf("unexpected chapter URL in list: %s", ch.URL)
		}
	}
}

func TestFenrirRealmParseChapterRejectsPremium(t *testing.T) {
	p := NewFenrirRealmParser()
	// Premium chapter content: the API returns only a short plain-text preview.
	premiumContent := `{"id":2,"slug":"2","name":"Chapter 2","content":"Chapter 2: preview only","type":"text","number":2,"locked":{"price":15,"unlocked_at":null,"is_read_only":false}}`
	client := &fenrirStubClient{responses: map[string]string{
		"https://fenrirealm.com/api/new/v2/series/absolute-regression/chapters/2": premiumContent,
	}}

	_, err := p.ParseChapter(context.Background(), client, "https://fenrirealm.com/series/absolute-regression/2")
	if err == nil {
		t.Fatal("expected error for premium chapter, got nil")
	}
	if !strings.Contains(err.Error(), "premium") && !strings.Contains(err.Error(), "locked") {
		t.Errorf("error should mention premium/locked, got: %v", err)
	}
}

func TestFenrirRealmParseChapterFree(t *testing.T) {
	p := NewFenrirRealmParser()
	freeContent := `{"id":1,"slug":"1","name":"Chapter 1","title":"One","content":"{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"Hello world\"}]}]}","type":"text","number":1,"locked":{"price":0,"unlocked_at":"2025-02-24T21:46:04.000000Z","is_read_only":false}}`
	client := &fenrirStubClient{responses: map[string]string{
		"https://fenrirealm.com/api/new/v2/series/absolute-regression/chapters/1": freeContent,
	}}

	ch, err := p.ParseChapter(context.Background(), client, "https://fenrirealm.com/series/absolute-regression/1")
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	if !strings.Contains(ch.Content, "Hello world") {
		t.Errorf("content missing text: %q", ch.Content)
	}
}
