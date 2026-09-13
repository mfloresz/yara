package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// webnovelStubClient returns canned payloads keyed by URL so parser tests run
// without hitting the live site.
type webnovelStubClient struct {
	responses map[string]string
}

func (c *webnovelStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *webnovelStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *webnovelStubClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*webnovelStubClient)(nil)

const webnovelStubBookID = "32842037100534205"

const webnovelStubBookURL = "https://www.webnovel.com/book/" + webnovelStubBookID

const webnovelStubCatalogURL = webnovelStubBookURL + "/catalog"

const webnovelStubChapterURL = "https://www.webnovel.com/es/book/bl-the-northern-grand-duke%E2%80%99s-hamster-(novel-translation)_" + webnovelStubBookID + "/chapter-1_88176479329909946"

const webnovelStubBookPage = `<!DOCTYPE html><html><head>
<meta property="og:title" content="BL: The Northern Grand Duke’s Hamster (Novel Translation)"/>
<meta property="og:author" content="ShonenMoon"/>
<meta property="og:image" content="https://book-pic.webnovel.com/bookcover/32842037100534205?imageMogr2/thumbnail/600x&amp;imageId=1748525987310"/>
<meta property="og:description" content="After an accident, I reincarnated into a novel."/>
</head><body>
<div class="det-info"><h1>BL: The Northern Grand Duke’s Hamster (Novel Translation)</h1></div>
<address><a href="/es/profile/4327762285">ShonenMoon</a></address>
</body></html>`

// webnovelFEFF is the U+FEFF marker the site embeds in some chapter slugs
// and titles (percent-encoded as %EF%BB%BF in hrefs). It cannot appear as a
// literal in Go source (illegal byte order mark), so it is escaped here.
const webnovelFEFF = "\ufeff"

const webnovelStubCatalogPagePrefix = `<!DOCTYPE html><html><body>
<a id="j_read" href="/es/book/slug_32842037100534205/chapter-1_88176479329909946">leer</a>
<div class="j_catalog_list"><ol class="content-list">
<li class="g_col _6" data-cid="88176479329909946"><a href="/es/book/slug_32842037100534205/chapter-1_88176479329909946" title="Chapter 1"><i>1</i><div><strong>Chapter 1</strong></div></a></li>
<li class="g_col _6" data-cid="88200944134702551"><a href="/es/book/slug_32842037100534205/chapter-17%EF%BB%BF_88200944134702551" title="Chapter 17`

const webnovelStubCatalogPageSuffix = `"><i>17</i><div><strong>Chapter 17`

const webnovelStubCatalogPageTail = `</strong></div></a></li>
<li class="g_col _6" data-cid="89497284047331793"><a href="/es/book/slug_32842037100534205/chapter-129_89497284047331793" title="Chapter 129"><i>129</i><div><strong>Chapter 129</strong></div></a></li>
</ol></div>
</body></html>`

var webnovelStubCatalogPage = webnovelStubCatalogPagePrefix + webnovelFEFF + webnovelStubCatalogPageSuffix + webnovelFEFF + webnovelStubCatalogPageTail

const webnovelStubChapterPage = `<!DOCTYPE html><html><body>
<div class="chapter_content" data-islock="0" data-cid="88176479329909946" data-chaptername="Chapter 1">
<div class="cha-tit"><h1>Capítulo 1: Chapter 1</h1></div>
<div class="cha-content"><div class="cha-words">
<div class="cha-paragraph"><div class="dib"><p>It sounds like something out of a dream, but it happened.</p></div></div>
<div class="cha-paragraph"><div class="dib"><p>—<em>Squeak!</em> (Why?!)</p></div></div>
<div class="cha-paragraph"><div class="dib"><p>   </p></div></div>
</div></div>
</div>
</body></html>`

const webnovelStubLockedChapterPage = `<!DOCTYPE html><html><body>
<div class="chapter_content _lock" data-islock="1" data-cid="999" data-chaptername="Chapter 50">
<div class="cha-content _lock"><div class="cha-words"></div></div>
</div>
</body></html>`

func webnovelTestClient() *webnovelStubClient {
	return &webnovelStubClient{responses: map[string]string{
		webnovelStubBookURL:    webnovelStubBookPage,
		webnovelStubCatalogURL: webnovelStubCatalogPage,
		webnovelStubChapterURL: webnovelStubChapterPage,
	}}
}

func TestWebnovelCanHandle(t *testing.T) {
	p := NewWebnovelParser()
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://www.webnovel.com/es/book/bl-the-northern-grand-duke%E2%80%99s-hamster-(novel-translation)_32842037100534205", true},
		{"https://www.webnovel.com/book/32842037100534205", true},
		{"https://www.webnovel.com/book/32842037100534205/catalog", true},
		{"https://www.webnovel.com/es/book/slug_32842037100534205/chapter-1_88176479329909946", true},
		{"https://www.webnovel.com/es/book/slug_32842037100534205/chapter-17%EF%BB%BF_88200944134702551", true},
		{"https://m.webnovel.com/book/32842037100534205", true},
		{"https://www.webnovel.com/es/search?keywords=hamster", false},
		{"https://www.webnovel.com/", false},
		{"https://novelfire.net/book/some-book", false},
		{"not a url at all", false},
	}
	for _, tc := range cases {
		if got := p.CanHandle(tc.url); got != tc.ok {
			t.Errorf("CanHandle(%q) = %v, want %v", tc.url, got, tc.ok)
		}
	}
}

func TestWebnovelParseURL(t *testing.T) {
	cases := []struct {
		url    string
		bookID string
		kind   webnovelURLKind
	}{
		{"https://www.webnovel.com/es/book/slug_32842037100534205", "32842037100534205", webnovelURLBook},
		{"https://www.webnovel.com/book/32842037100534205", "32842037100534205", webnovelURLBook},
		{"https://www.webnovel.com/book/32842037100534205/catalog", "32842037100534205", webnovelURLBook},
		{"https://www.webnovel.com/es/book/slug_32842037100534205/chapter-1_88176479329909946", "32842037100534205", webnovelURLChapter},
		{"https://www.webnovel.com/es/book/slug_32842037100534205/chapter-17%EF%BB%BF_88200944134702551", "32842037100534205", webnovelURLChapter},
	}
	for _, tc := range cases {
		bookID, kind, ok := webnovelParseURL(tc.url)
		if !ok {
			t.Errorf("webnovelParseURL(%q): not ok", tc.url)
			continue
		}
		if bookID != tc.bookID || kind != tc.kind {
			t.Errorf("webnovelParseURL(%q) = (%q, %v), want (%q, %v)",
				tc.url, bookID, kind, tc.bookID, tc.kind)
		}
	}
}

func TestWebnovelGetNovelInfo(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	info, err := p.GetNovelInfo(ctx, webnovelTestClient(), webnovelStubBookURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "BL: The Northern Grand Duke’s Hamster (Novel Translation)" {
		t.Errorf("title = %q", info.Title)
	}
	if info.Author != "ShonenMoon" {
		t.Errorf("author = %q", info.Author)
	}
	if !strings.Contains(info.Description, "reincarnated into a novel") {
		t.Errorf("description = %q", info.Description)
	}
	if !strings.HasPrefix(info.CoverURL, "https://book-pic.webnovel.com/bookcover/32842037100534205") {
		t.Errorf("coverURL = %q", info.CoverURL)
	}
	if len(info.Chapters) != 3 {
		t.Fatalf("chapters = %d, want 3", len(info.Chapters))
	}
	// The %EF%BB%BF (U+FEFF) marker in the chapter-17 slug/title is stripped.
	if info.Chapters[1].Title != "Chapter 17" {
		t.Errorf("chapters[1].Title = %q, want %q", info.Chapters[1].Title, "Chapter 17")
	}
	for i, ch := range info.Chapters {
		if ch.Order != i+1 {
			t.Errorf("chapters[%d].Order = %d", i, ch.Order)
		}
		// Catalog hrefs carry a UI locale prefix (/es/book/...); emitted
		// chapter URLs use the canonical locale-free form.
		if !strings.HasPrefix(ch.URL, "https://www.webnovel.com/book/") {
			t.Errorf("chapters[%d].URL = %q (want canonical locale-free URL)", i, ch.URL)
		}
	}
}

func TestWebnovelGetNovelInfoFromChapterURL(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	info, err := p.GetNovelInfo(ctx, webnovelTestClient(), webnovelStubChapterURL)
	if err != nil {
		t.Fatalf("GetNovelInfo from chapter URL: %v", err)
	}
	if len(info.Chapters) != 3 {
		t.Errorf("chapters = %d, want 3", len(info.Chapters))
	}
}

func TestWebnovelGetChapterURLs(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	chapters, err := p.GetChapterURLs(ctx, webnovelTestClient(), nil, webnovelStubChapterURL)
	if err != nil {
		t.Fatalf("GetChapterURLs: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("chapters = %d, want 3", len(chapters))
	}
	if chapters[0].Title != "Chapter 1" || chapters[2].Title != "Chapter 129" {
		t.Errorf("unexpected titles: %q, %q", chapters[0].Title, chapters[2].Title)
	}
}

func TestWebnovelParseChapter(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	ch, err := p.ParseChapter(ctx, webnovelTestClient(), webnovelStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1" {
		t.Errorf("title = %q, want %q", ch.Title, "Chapter 1")
	}
	// Empty paragraphs are skipped; inline emphasis is preserved.
	if strings.Count(ch.Content, "<p>") != 2 {
		t.Errorf("expected 2 paragraphs, got content:\n%s", ch.Content)
	}
	if !strings.Contains(ch.Content, "<em>Squeak!</em>") {
		t.Errorf("emphasis lost in content:\n%s", ch.Content)
	}
}

func TestWebnovelParseLockedChapter(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	client := &webnovelStubClient{responses: map[string]string{
		"https://www.webnovel.com/book/slug_32842037100534205/chapter-50_88294385443203592": webnovelStubLockedChapterPage,
	}}
	_, err := p.ParseChapter(ctx, client, "https://www.webnovel.com/book/slug_32842037100534205/chapter-50_88294385443203592")
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Errorf("expected locked-chapter error, got %v", err)
	}
}

func TestWebnovelParseNonChapterURL(t *testing.T) {
	p := NewWebnovelParser()
	ctx := context.Background()
	_, err := p.ParseChapter(ctx, webnovelTestClient(), webnovelStubBookURL)
	if err == nil {
		t.Errorf("expected error for non-chapter URL")
	}
}
