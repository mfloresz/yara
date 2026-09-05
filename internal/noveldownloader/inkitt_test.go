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

// inkittStubClient serves canned API payloads via Do and canned story pages
// via FetchDocument so parser tests run without hitting the live site.
type inkittStubClient struct {
	getResponses map[string][]byte
	documents    map[string]string
}

func (c *inkittStubClient) Fetch(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("no stub (use Do/FetchDocument)")
}

func (c *inkittStubClient) FetchDocument(_ context.Context, pageURL string) (*goquery.Document, error) {
	body, ok := c.documents[pageURL]
	if !ok {
		return nil, fmt.Errorf("no stub document for %s", pageURL)
	}
	return goquery.NewDocumentFromReader(strings.NewReader(body))
}

func (c *inkittStubClient) Do(req *http.Request) (*http.Response, error) {
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

const inkittStubStoryURL = "https://www.inkitt.com/stories/1579934"

const inkittStubStoryJSON = `{"id":1579934,"title":"My Boss Moved In After His Divorce ",
"cover_url":"https://cdn-gcs.inkitt.com/storycovers/c1b1958a41beb05c8c8a600745748af1.jpg",
"vertical_cover":{"url":"https://cdn-gcs.inkitt.com/vertical_storycovers/ipad_f8c4187a1504bab7f28bccc0c786f25d.jpg"},
"user":{"name":"DanXWrites"},
"chapters":[{"chapter_number":1,"id":7877515,"name":"Summary"},
{"chapter_number":2,"id":7877516,"name":"EP 1 : The Offer"},
{"chapter_number":3,"id":7877517,"name":"EP 2: Move In Day"}]}`

const inkittStubStoryHTML = `<html><head>
<meta name="author" content="FallbackAuthor">
<meta property="og:description" content="Fallback summary.">
</head><body>
<h1 class="story-title">My Boss Moved In After His Divorce</h1>
<span id="storyAuthor">DanXWrites</span>
<p class="story-summary">A friendly gesture turns complicated.</p>
<ul class="nav nav-list chapter-list-dropdown">
<li><a class="chapter-link" href="/stories/1579934/chapters/1"><span class="chapter-nr">1</span> <span class="chapter-title">Summary</span></a></li>
<li><a class="chapter-link" href="/stories/1579934/chapters/2"><span class="chapter-nr">2</span> <span class="chapter-title">EP 1 : The Offer</span></a></li>
</ul>
</body></html>`

const inkittStubChapterHTML = `<html><body>
<h2 class="chapter-head-title">EP 1 : The Offer</h2>
<div class="story-page-text" id="chapterText">
<p data-content="1">First paragraph with <i>emphasis</i>.</p>
<p data-content="2">Second paragraph.</p>
</div>
</body></html>`

func newInkittStubClient() *inkittStubClient {
	return &inkittStubClient{
		getResponses: map[string][]byte{
			"https://www.inkitt.com/api/stories/1579934": []byte(inkittStubStoryJSON),
		},
		documents: map[string]string{
			inkittStubStoryURL: inkittStubStoryHTML,
			"https://www.inkitt.com/stories/1579934/chapters/2": inkittStubChapterHTML,
		},
	}
}

func TestInkittCanHandle(t *testing.T) {
	p := NewInkittParser()
	cases := []struct {
		url  string
		want bool
	}{
		{inkittStubStoryURL, true},
		{"https://www.inkitt.com/stories/erotica/1579934", true},
		{"https://inkitt.com/stories/1579934", true},
		{"https://www.inkitt.com/stories/1579934/chapters/2", true},
		{"https://www.inkitt.com/stories/1579934/chapters/30", true},
		{"https://www.inkitt.com/genres/erotica", false},
		{"https://www.inkitt.com/DanXWrites", false},
		{"https://example.com/stories/1579934", false},
		{"https://inkitt.com.attacker.example/stories/1579934", false},
	}
	for _, c := range cases {
		if got := p.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestInkittGetNovelInfo(t *testing.T) {
	p := NewInkittParser()
	client := newInkittStubClient()

	info, err := p.GetNovelInfo(context.Background(), client, inkittStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "My Boss Moved In After His Divorce" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "DanXWrites" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	if info.Description != "A friendly gesture turns complicated." {
		t.Errorf("unexpected description: %q", info.Description)
	}
	// The vertical cover wins over the landscape one.
	if info.CoverURL != "https://cdn-gcs.inkitt.com/vertical_storycovers/ipad_f8c4187a1504bab7f28bccc0c786f25d.jpg" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(info.Chapters))
	}
	second := info.Chapters[1]
	if second.Title != "EP 1 : The Offer" {
		t.Errorf("unexpected chapter title: %q", second.Title)
	}
	if second.URL != "https://www.inkitt.com/stories/1579934/chapters/2" {
		t.Errorf("unexpected chapter URL: %q", second.URL)
	}
	if second.Order != 2 {
		t.Errorf("unexpected chapter order: %d", second.Order)
	}
	if info.SourceURL != inkittStubStoryURL {
		t.Errorf("unexpected source URL: %q", info.SourceURL)
	}
}

func TestInkittGetNovelInfoFromChapterURL(t *testing.T) {
	// A reading-page URL must resolve to the parent story.
	p := NewInkittParser()
	client := newInkittStubClient()

	info, err := p.GetNovelInfo(context.Background(), client, "https://www.inkitt.com/stories/1579934/chapters/2")
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "My Boss Moved In After His Divorce" || len(info.Chapters) != 3 {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestInkittParseChapter(t *testing.T) {
	p := NewInkittParser()
	client := newInkittStubClient()

	ch, err := p.ParseChapter(context.Background(), client, "https://www.inkitt.com/stories/1579934/chapters/2")
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "EP 1 : The Offer" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	for _, want := range []string{
		"First paragraph with",
		"Second paragraph.",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
	// Inline emphasis must survive as HTML for the markdown conversion.
	if !strings.Contains(ch.Content, "<i>emphasis</i>") {
		t.Errorf("content lost inline markup:\n%s", ch.Content)
	}
}

func TestInkittParseChapterBadURL(t *testing.T) {
	p := NewInkittParser()
	client := newInkittStubClient()

	if _, err := p.ParseChapter(context.Background(), client, inkittStubStoryURL); err == nil {
		t.Error("expected error for story page URL, got nil")
	}
	if _, err := p.ParseChapter(context.Background(), client, "https://example.com/stories/1579934/chapters/2"); err == nil {
		t.Error("expected error for non-inkitt URL, got nil")
	}
}

func TestIsInkittChapterURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.inkitt.com/stories/1579934/chapters/2", true},
		{"https://inkitt.com/stories/1579934/chapters/30", true},
		{"https://www.inkitt.com/stories/1579934", false},
		{"https://www.inkitt.com/stories/erotica/1579934", false},
		{"https://example.com/stories/1579934/chapters/2", false},
	}
	for _, c := range cases {
		if got := IsInkittChapterURL(c.url); got != c.want {
			t.Errorf("IsInkittChapterURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestHasInkittFoldedChapter(t *testing.T) {
	folded := `<div class='story-page-text_folded story-page-text_folded--without-height'><div class='story-page-text' id='chapterText'>  </div></div>`
	if !hasInkittFoldedChapter([]byte(folded)) {
		t.Error("folded chapter page not detected")
	}
	unfolded := `<div class='' style='position: relative'><div class='story-page-text' id='chapterText'><p data-content="1">Text.</p></div></div>`
	if hasInkittFoldedChapter([]byte(unfolded)) {
		t.Error("unfolded chapter page misdetected as folded")
	}
}
