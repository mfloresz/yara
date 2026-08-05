package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const literoticaSeriesHTML = `<!doctype html><html><head>
<meta name="description" content="A 8-part Story Series by TestAuthor."/>
<meta name="keywords" content="incest,gay,brothers"/>
<meta property="og:title" content="Test Series - Gay Male - Literotica.com"/>
<meta property="og:description" content="A 8-part Story Series by TestAuthor."/>
</head><body>
<h1 class="_title_ebp5m_26">Test Series</h1>
<div class="panel _author_1wp51_1 article">
  <a class="_author__title_1wp51_48" href="/authors/TestAuthor/works/stories">TestAuthor</a>
</div>
<div class="_date_container_1y595_1422">
  <div class="_files__date_1y595_672">Started: 01/01/2026</div>
  <div class="_files__date_1y595_672">Updated: 02/01/2026</div>
</div>
<section class="_wrapper_qr6sx_1">
  <h2 class="_title_qr6sx_18">Table of Contents</h2>
  <ul class="_list_qr6sx_43">
    <li class="_item_qr6sx_49"><a href="/s/test-series-ch-01" class="_link_qr6sx_55">Test Series Ch. 01</a></li>
    <li class="_item_qr6sx_49"><a href="/s/test-series-ch-02" class="_link_qr6sx_55">Test Series Ch. 02</a></li>
  </ul>
</section>
</body></html>`

const literoticaStoryHTML = `<!doctype html><html><head>
<meta name="description" content="Test story description."/>
<meta property="og:title" content="Test Series Ch. 01 - Gay Male - Literotica.com"/>
</head><body>
<h1 class="_title_ebp5m_26">Test Series Ch. 01</h1>
<div class="panel article _article_138fn_1 ">
  <div class="_article__content_138fn_99" itemprop="articleBody">
    <div class="_introduction-wrap_jeax0_1 _open_jeax0_27">
      <p>First paragraph of the story.</p>
      <p>Second paragraph with <b>bold</b> text.</p>
    </div>
  </div>
</div>
</body></html>`

const literoticaStoryPage1HTML = `<!doctype html><html><body>
<h1 class="_title_ebp5m_26">Test Series Ch. 01</h1>
<div class="panel article _article_138fn_1 ">
  <div class="_article__content_138fn_99" itemprop="articleBody">
    <div class="_introduction-wrap_jeax0_1 _open_jeax0_27">
      <p>Content from page one.</p>
    </div>
  </div>
</div>
<nav aria-label="Pagination">
  <a href="?page=2" class="_pagination__item_nqq7s_13 _pagination__item--next_nqq7s_40" title="Next Page" aria-label="Next Page">Next</a>
</nav>
</body></html>`

const literoticaStoryPage2HTML = `<!doctype html><html><body>
<h1 class="_title_ebp5m_26">Test Series Ch. 01</h1>
<div class="panel article _article_138fn_1 ">
  <div class="_article__content_138fn_99" itemprop="articleBody">
    <div class="_introduction-wrap_jeax0_1 _open_jeax0_27">
      <p>Content from page two.</p>
    </div>
  </div>
</div>
<nav aria-label="Pagination">
  <span class="_pagination__item_nqq7s_13 _pagination__item--next_nqq7s_40" title="Next Page" aria-label="Next Page">Next</span>
</nav>
</body></html>`

func TestLiteroticaCanHandle(t *testing.T) {
	p := &literoticaParser{}
	cases := map[string]bool{
		"https://www.literotica.com/series/se/495277611":      true,
		"https://www.literotica.com/s/boyfriend-s-vacation-01": true,
		"https://literotica.com/s/some-story?page=2":           true,
		"https://example.com/novel/foo":                        false,
		"https://www.notliterotica.com/s/foo":                  false,
	}
	for urlStr, want := range cases {
		if got := p.CanHandle(urlStr); got != want {
			t.Errorf("CanHandle(%q) = %v, want %v", urlStr, got, want)
		}
	}
}

func TestLiteroticaGetNovelInfo(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, literoticaSeriesHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &literoticaParser{}
	url := mock.URL + "/series/se/495277611"

	info, err := p.GetNovelInfo(context.Background(), client, url)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "Test Series" {
		t.Errorf("title = %q, want %q", info.Title, "Test Series")
	}
	if info.Author != "TestAuthor" {
		t.Errorf("author = %q, want %q", info.Author, "TestAuthor")
	}
	if info.SourceURL != url {
		t.Errorf("sourceURL = %q, want %q", info.SourceURL, url)
	}
	if !strings.Contains(info.Description, "A 8-part Story Series by TestAuthor.") {
		t.Errorf("description missing base text: %q", info.Description)
	}
	if !strings.Contains(info.Description, "Tags: incest, gay, brothers") {
		t.Errorf("description missing tags: %q", info.Description)
	}
	if !strings.Contains(info.Description, "Started: 01/01/2026") {
		t.Errorf("description missing started date: %q", info.Description)
	}

	if len(info.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(info.Chapters))
	}
	if info.Chapters[0].Title != "Test Series Ch. 01" || info.Chapters[0].Order != 1 {
		t.Errorf("chapter 0 = %+v", info.Chapters[0])
	}
	if info.Chapters[0].URL != mock.URL+"/s/test-series-ch-01" {
		t.Errorf("chapter 0 URL = %q, want %q", info.Chapters[0].URL, mock.URL+"/s/test-series-ch-01")
	}
	if info.Chapters[1].Title != "Test Series Ch. 02" || info.Chapters[1].Order != 2 {
		t.Errorf("chapter 1 = %+v", info.Chapters[1])
	}
}

func TestLiteroticaSingleStoryFallback(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, literoticaStoryHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &literoticaParser{}
	url := mock.URL + "/s/test-series-ch-01"

	info, err := p.GetNovelInfo(context.Background(), client, url)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if len(info.Chapters) != 1 {
		t.Fatalf("expected 1 chapter for a single story, got %d", len(info.Chapters))
	}
	if info.Chapters[0].URL != url || info.Chapters[0].Title != "Test Series Ch. 01" {
		t.Errorf("chapter = %+v", info.Chapters[0])
	}
}

func TestLiteroticaParseChapter(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, literoticaStoryHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &literoticaParser{}
	url := mock.URL + "/s/test-series-ch-01"

	chapter, err := p.ParseChapter(context.Background(), client, url)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if chapter.Title != "Test Series Ch. 01" {
		t.Errorf("title = %q, want %q", chapter.Title, "Test Series Ch. 01")
	}
	if !strings.Contains(chapter.Content, "First paragraph of the story.") {
		t.Errorf("content missing first paragraph: %q", chapter.Content)
	}
	if !strings.Contains(chapter.Content, "Second paragraph with bold text.") {
		t.Errorf("content missing second paragraph (inline tags flattened): %q", chapter.Content)
	}
	if chapter.SourceURL != url {
		t.Errorf("sourceURL = %q, want %q", chapter.SourceURL, url)
	}
}

func TestLiteroticaParseChapterPaginated(t *testing.T) {
	var pageHits int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		pageHits++
		switch r.URL.Query().Get("page") {
		case "2":
			_, _ = fmt.Fprint(w, literoticaStoryPage2HTML)
		default:
			_, _ = fmt.Fprint(w, literoticaStoryPage1HTML)
		}
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &literoticaParser{}
	url := mock.URL + "/s/test-series-ch-01"

	chapter, err := p.ParseChapter(context.Background(), client, url)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if !strings.Contains(chapter.Content, "Content from page one.") {
		t.Errorf("content missing page one: %q", chapter.Content)
	}
	if !strings.Contains(chapter.Content, "Content from page two.") {
		t.Errorf("content missing page two: %q", chapter.Content)
	}
	if pageHits != 2 {
		t.Errorf("expected 2 page fetches (page 1 + next), got %d", pageHits)
	}
}
