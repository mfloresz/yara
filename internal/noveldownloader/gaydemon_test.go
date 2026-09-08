package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const gaydemonStoryHTML = `<!doctype html><html><head>
<title>In need of the family's affection [Story] | GayDemon</title>
<meta name="description" content="Dylan has something for his family."/>
<meta name="author" content="ZeloS"/>
<meta property="og:title" content="In need of the family's affection"/>
</head><body>
<article id="story" itemscope itemtype="http://schema.org/ShortStory">
<header class="story-page">
<h1 itemprop="name">In need of the family's affection</h1>
<div class="sub-title">
<div class="author">By <a href="/stories/authors/sandro852/" class="story-page-author">ZeloS</a></div>
</div>
<p class="textify">Dylan has something for his family. Are his thoughts in his favor?</p>
</header>
<div id="chapters" class="toggle-collapse">
<button class="collapsed h2 toggle-button story-page-chapt" type="button">Chapter 1 <svg><title>Show all chapters</title></svg></button>
<nav class="collapse" id="chapter-nav">
<ul>
<li><span>Chapter 1</span></li>
<li><a href="/stories/In_need_of_the_family_s_affection_2_45686.html">Chapter 2</a></li>
<li><a href="/stories/In_need_of_the_family_s_affection_3_45705.html">Chapter 3</a></li>
</ul>
</nav>
</div>
<div class="textify story-text" itemprop="articleBody">
<p>Since I could remember, I had always lived with my two older brothers.</p>
<p>Second paragraph of chapter one.</p>
<hr>
<p><em>To get in touch with the author, <a href="/cdn-cgi/l/email-protection">send them an email</a>.</em></p>
<hr>
</div>
<section class="section tags">
<ul>
<li><a href="/stories/tag/brothers/" class="story-page-tag">Brothers</a></li>
<li><a href="/stories/tag/incest/" class="story-page-tag">Incest</a></li>
</ul>
</section>
</article>
</body></html>`

const gaydemonChapter2HTML = `<!doctype html><html><head>
<title>In need of the family's affection [Story] | GayDemon</title>
<meta name="author" content="ZeloS"/>
</head><body>
<article id="story">
<header class="story-page">
<h1 itemprop="name">In need of the family's affection</h1>
<p class="textify">Dylan has something for his family. Are his thoughts in his favor?</p>
</header>
<div id="chapters" class="toggle-collapse">
<button class="collapsed h2 toggle-button story-page-chapt" type="button">Chapter 2 <svg><title>Show all chapters</title></svg></button>
<nav class="collapse" id="chapter-nav">
<ul>
<li><a href="/stories/In_need_of_the_family_s_affection_45653.html">Chapter 1</a></li>
<li><span>Chapter 2</span></li>
<li><a href="/stories/In_need_of_the_family_s_affection_3_45705.html">Chapter 3</a></li>
</ul>
</nav>
</div>
<div class="textify story-text" itemprop="articleBody">
<p>Chapter two opens with new tension.</p>
<p>Another paragraph here.</p>
</div>
</article>
</body></html>`

const gaydemonSingleHTML = `<!doctype html><html><head><title>Solo Story | GayDemon</title></head><body>
<article id="story">
<header class="story-page"><h1 itemprop="name">Solo Story</h1></header>
<div class="textify story-text" itemprop="articleBody">
<p>Only chapter content.</p>
</div>
</article>
</body></html>`

func TestGaydemonCanHandle(t *testing.T) {
	p := &gaydemonParser{}
	cases := map[string]bool{
		"https://www.gaydemon.com/stories/In_need_of_the_family_s_affection_45653.html": true,
		"https://gaydemon.com/stories/Some_story_123.html":                               true,
		"http://www.gaydemon.com/stories/foo.html":                                      true,
		"https://www.gaydemon.com/gallery/foo":                                          false,
		"https://example.com/stories/foo.html":                                          false,
	}
	for urlStr, want := range cases {
		if got := p.CanHandle(urlStr); got != want {
			t.Errorf("CanHandle(%q) = %v, want %v", urlStr, got, want)
		}
	}
}

func TestGaydemonGetNovelInfo(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, gaydemonStoryHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &gaydemonParser{}
	url := mock.URL + "/stories/In_need_of_the_family_s_affection_45653.html"

	info, err := p.GetNovelInfo(context.Background(), client, url)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "In need of the family's affection" {
		t.Errorf("title = %q", info.Title)
	}
	if info.Author != "ZeloS" {
		t.Errorf("author = %q", info.Author)
	}
	if info.SourceURL != url {
		t.Errorf("sourceURL = %q, want %q", info.SourceURL, url)
	}
	if !strings.Contains(info.Description, "Dylan has something for his family.") {
		t.Errorf("description missing standfirst: %q", info.Description)
	}
	if !strings.Contains(info.Description, "Tags: Brothers, Incest") {
		t.Errorf("description missing tags: %q", info.Description)
	}

	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(info.Chapters))
	}
	if info.Chapters[0].Title != "Chapter 1" || info.Chapters[0].Order != 1 {
		t.Errorf("chapter 0 = %+v", info.Chapters[0])
	}
	if info.Chapters[0].URL != url {
		t.Errorf("chapter 0 URL = %q, want current page %q", info.Chapters[0].URL, url)
	}
	if info.Chapters[1].Title != "Chapter 2" || info.Chapters[1].Order != 2 {
		t.Errorf("chapter 1 = %+v", info.Chapters[1])
	}
	if want := mock.URL + "/stories/In_need_of_the_family_s_affection_2_45686.html"; info.Chapters[1].URL != want {
		t.Errorf("chapter 1 URL = %q, want %q", info.Chapters[1].URL, want)
	}
}

func TestGaydemonSingleStoryFallback(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, gaydemonSingleHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &gaydemonParser{}
	url := mock.URL + "/stories/Solo_Story_999.html"

	info, err := p.GetNovelInfo(context.Background(), client, url)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Solo Story" {
		t.Errorf("title = %q", info.Title)
	}
	if len(info.Chapters) != 1 {
		t.Fatalf("expected 1 chapter for a standalone story, got %d", len(info.Chapters))
	}
	if info.Chapters[0].URL != url {
		t.Errorf("chapter URL = %q, want %q", info.Chapters[0].URL, url)
	}
}

func TestGaydemonParseChapter(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, gaydemonChapter2HTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &gaydemonParser{}
	url := mock.URL + "/stories/In_need_of_the_family_s_affection_2_45686.html"

	chapter, err := p.ParseChapter(context.Background(), client, url)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if chapter.Title != "Chapter 2" {
		t.Errorf("title = %q, want %q", chapter.Title, "Chapter 2")
	}
	if !strings.Contains(chapter.Content, "Chapter two opens with new tension.") {
		t.Errorf("content missing first paragraph: %q", chapter.Content)
	}
	if chapter.SourceURL != url {
		t.Errorf("sourceURL = %q, want %q", chapter.SourceURL, url)
	}
}

func TestGaydemonParseChapterSkipsContactBoilerplate(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, gaydemonStoryHTML)
	}))
	defer mock.Close()

	client := NewHTTPClient()
	p := &gaydemonParser{}
	url := mock.URL + "/stories/In_need_of_the_family_s_affection_45653.html"

	chapter, err := p.ParseChapter(context.Background(), client, url)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if chapter.Title != "Chapter 1" {
		t.Errorf("title = %q, want %q", chapter.Title, "Chapter 1")
	}
	if strings.Contains(chapter.Content, "To get in touch with the author") {
		t.Errorf("content includes author-contact boilerplate: %q", chapter.Content)
	}
	if !strings.Contains(chapter.Content, "Since I could remember") {
		t.Errorf("content missing story text: %q", chapter.Content)
	}
}

func TestGaydemonRegisteredInDownloader(t *testing.T) {
	dl := NewDownloader()
	p := dl.FindParser("https://www.gaydemon.com/stories/In_need_of_the_family_s_affection_45653.html")
	if p == nil {
		t.Fatal("no parser found for gaydemon URL")
	}
	if p.Name() != "gaydemon" {
		t.Errorf("parser name = %q, want %q", p.Name(), "gaydemon")
	}
	if dl.RequiresBrowser("https://www.gaydemon.com/stories/In_need_of_the_family_s_affection_45653.html") {
		t.Errorf("gaydemon should not require browser")
	}
}
