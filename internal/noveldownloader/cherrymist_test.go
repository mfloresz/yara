package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

// cherryMistStubClient serves the captured API payloads so the parser is
// exercised without touching the live site.
type cherryMistStubClient struct {
	responses map[string]string
}

func (c *cherryMistStubClient) Fetch(_ context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *cherryMistStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *cherryMistStubClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*cherryMistStubClient)(nil)

const (
	cherryMistStubStoryURL   = "https://cherrymist.cafe/story/reverse-dungeon/"
	cherryMistStubChapterURL = "https://cherrymist.cafe/chapter/rd-chapter-1"
	cherryMistStubListURL    = "https://cherrymist.cafe/api/chapters?series_id=63&published=1&limit=500"
	cherryMistStubBySlugURL  = "https://cherrymist.cafe/api/chapters/by-slug/rd-chapter-1"
	cherryMistStubDetailURL  = "https://cherrymist.cafe/api/chapters/1734"
	cherryMistStubSeriesURL  = "https://cherrymist.cafe/api/series/reverse-dungeon"
)

func newCherryMistStubClient() *cherryMistStubClient {
	return &cherryMistStubClient{responses: map[string]string{
		cherryMistStubSeriesURL:                 cherryMistStubSeriesJSON,
		cherryMistStubListURL:                   cherryMistStubChapterListJSON,
		cherryMistStubBySlugURL:                 cherryMistStubChapterRefJSON,
		cherryMistStubDetailURL:                 cherryMistStubChapterJSON,
		"https://cherrymist.cafe/api/series/63": cherryMistStubSeriesJSON,
	}}
}

func TestCherryMistGetNovelInfo(t *testing.T) {
	p := NewCherryMistParser()

	info, err := p.GetNovelInfo(context.Background(), newCherryMistStubClient(), cherryMistStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Reverse Dungeon" {
		t.Errorf("title = %q, want %q", info.Title, "Reverse Dungeon")
	}
	if info.Author != "Rae" {
		t.Errorf("author = %q, want %q (translator fallback)", info.Author, "Rae")
	}
	if info.Description != "Has the game company lost its mind?" {
		t.Errorf("description = %q, want the short synopsis", info.Description)
	}
	if info.CoverURL != "https://media.cherrymist.cafe/cover.jpg" {
		t.Errorf("coverURL = %q", info.CoverURL)
	}
	if info.SourceURL != cherryMistStubStoryURL {
		t.Errorf("sourceURL = %q, want %q", info.SourceURL, cherryMistStubStoryURL)
	}
	if len(info.Chapters) != 2 {
		t.Fatalf("chapters = %d, want 2", len(info.Chapters))
	}
	// The stub returns chapter 2 before chapter 1; the parser must sort.
	if info.Chapters[0].URL != "https://cherrymist.cafe/chapter/rd-chapter-1" {
		t.Errorf("first chapter URL = %q, want the /chapter/<slug> page URL", info.Chapters[0].URL)
	}
	if info.Chapters[0].Order != 1 || info.Chapters[1].Order != 2 {
		t.Errorf("chapter order = %d,%d, want 1,2", info.Chapters[0].Order, info.Chapters[1].Order)
	}
}

func TestCherryMistGetNovelInfoFromChapterURL(t *testing.T) {
	p := NewCherryMistParser()

	info, err := p.GetNovelInfo(context.Background(), newCherryMistStubClient(), cherryMistStubChapterURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Reverse Dungeon" {
		t.Errorf("title = %q, want %q", info.Title, "Reverse Dungeon")
	}
	if len(info.Chapters) != 2 {
		t.Errorf("chapters = %d, want 2", len(info.Chapters))
	}
}

func TestCherryMistParseChapterDecodesCipher(t *testing.T) {
	p := NewCherryMistParser()

	chapter, err := p.ParseChapter(context.Background(), newCherryMistStubClient(), cherryMistStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if chapter.Title != "RD | Chapter 1" {
		t.Errorf("title = %q, want %q", chapter.Title, "RD | Chapter 1")
	}
	if chapter.SourceURL != cherryMistStubChapterURL {
		t.Errorf("sourceURL = %q, want %q", chapter.SourceURL, cherryMistStubChapterURL)
	}

	// The real seed-9 body must come back as readable prose.
	for _, want := range []string{
		"The mysterious thing about life is that you can never predict what",
		"In that sense, Jeong Yiwon had quite a special day",
		"To be precise, when he woke up, he found himself on a horse.",
	} {
		if !strings.Contains(chapter.Content, want) {
			t.Errorf("decoded content missing %q\ngot: %s", want, chapter.Content)
		}
	}

	// Every Private Use Area codepoint must be resolved.
	for _, r := range chapter.Content {
		if unicode.In(r, unicode.Co) {
			t.Errorf("undecoded Private Use Area rune %U in content", r)
		}
	}

	// The presentational font-weight spans are dropped and each block becomes a
	// paragraph, because the downloader converts Content to markdown.
	if strings.Contains(chapter.Content, "<span") {
		t.Errorf("content still contains the font-weight spans: %s", chapter.Content)
	}
	if n := strings.Count(chapter.Content, "<p>"); n != 3 {
		t.Errorf("paragraph count = %d, want 3\ngot: %s", n, chapter.Content)
	}
	if !strings.HasPrefix(chapter.Content, "<p>The mysterious") {
		t.Errorf("content should start with the epigraph paragraph: %s", chapter.Content)
	}
}

func TestCherryMistDecodeLeavesPlainTextAlone(t *testing.T) {
	const plain = "Nothing to decode here — 12345."
	if got := cherryMistDecode(plain, 9); got != plain {
		t.Errorf("decoding plain text changed it: %q", got)
	}
}

func TestCherryMistDecodeUnknownSeedIsNoop(t *testing.T) {
	const ciphered = "abc"
	// A seed outside the table range must not corrupt the text; the parser
	// surfaces garbled output rather than panicking.
	if got := cherryMistDecode(ciphered, 999); got != ciphered {
		t.Errorf("unknown seed changed the text: %q", got)
	}
}

func TestCherryMistSlugs(t *testing.T) {
	tests := []struct {
		url        string
		wantSeries string
		wantChap   string
	}{
		{"https://cherrymist.cafe/story/reverse-dungeon/", "reverse-dungeon", ""},
		{"https://cherrymist.cafe/story/reverse-dungeon", "reverse-dungeon", ""},
		{"https://cherrymist.cafe/chapter/rd-chapter-1/", "", "rd-chapter-1"},
		// Legacy shape the site still redirects.
		{"https://cherrymist.cafe/story/reverse-dungeon/chapter/1/", "reverse-dungeon", "1"},
	}
	for _, tt := range tests {
		series, chapterSlug, err := cherryMistSlugs(tt.url)
		if err != nil {
			t.Errorf("cherryMistSlugs(%q): %v", tt.url, err)
			continue
		}
		if series != tt.wantSeries || chapterSlug != tt.wantChap {
			t.Errorf("cherryMistSlugs(%q) = (%q, %q), want (%q, %q)",
				tt.url, series, chapterSlug, tt.wantSeries, tt.wantChap)
		}
	}

	if _, _, err := cherryMistSlugs("https://cherrymist.cafe/"); err == nil {
		t.Error("expected an error for the site root URL")
	}
}

func TestCherryMistCanHandle(t *testing.T) {
	p := NewCherryMistParser()
	if !p.CanHandle(cherryMistStubStoryURL) || !p.CanHandle(cherryMistStubChapterURL) {
		t.Error("parser should handle both story and chapter URLs")
	}
	if p.CanHandle("https://example.com/story/reverse-dungeon/") {
		t.Error("parser should not handle other hosts")
	}
	if p.RequiresBrowser() {
		t.Error("the JSON API is reachable without the browser worker")
	}
}
