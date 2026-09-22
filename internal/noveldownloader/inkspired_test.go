package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// inkspiredStubClient serves canned pages keyed by URL so parser tests run
// without hitting the live (Cloudflare-protected) site.
type inkspiredStubClient struct {
	responses map[string]string
}

func (c *inkspiredStubClient) Fetch(_ context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *inkspiredStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *inkspiredStubClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*inkspiredStubClient)(nil)

const (
	inkspiredStubStoryURL    = "https://getinkspired.com/es/story/746625/gravedad-cero-boys-love/"
	inkspiredStubChapterURL  = "https://getinkspired.com/es/story/746625/chapter/fase-uno-variables-de-riesgo-2645438/"
	inkspiredStubChapter2URL = "https://getinkspired.com/es/story/746625/chapter/capitulo-1-huracan-2647835/"
)

// inkspiredStubStoryHTML mirrors the real story page: JSON-LD Book carrying the
// metadata and the ordered chapter list, plus the #chapterModal table of
// contents with its own "1.- " position prefixes.
const inkspiredStubStoryHTML = `<!DOCTYPE html><html><head>
<title>Gravedad Cero (Boys Love) | Inkspired</title>
<meta property="og:title" content="Inkspired - Gravedad Cero (Boys Love)"/>
<meta property="og:description" content="Truncated blurb…"/>
<meta property="og:image" content="https://cdn.getinkspired.com/media/upload/812a0189.png"/>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[]}
</script>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Book",
 "name":"Gravedad Cero (Boys Love)",
 "author":{"@type":"Person","name":"Red Black","url":"https://getinkspired.com/es/u/_boyinred_/"},
 "description":"Full description that the site caps at five hundred characters…",
 "url":"https://getinkspired.com/es/story/746625/gravedad-cero-boys-love/",
 "hasPart":[
  {"@type":"Chapter","name":"Fase Uno: Variables de Riesgo","url":"` + inkspiredStubChapterURL + `","isAccessibleForFree":true},
  {"@type":"Chapter","name":"Capítulo 1: Huracán","url":"` + inkspiredStubChapter2URL + `","isAccessibleForFree":true}
 ]}
</script>
</head><body class="ink-light">
<header class="pt100 pb30 parallax-window-mini">
 <div class="zindex2 jedi crx crx-ondark">
  <h1 class="crx-h2 strong">Gravedad Cero (Boys Love)</h1>
  <a class="crx-authorrow color-light blocker" href="/es/u/_boyinred_/">
   <span class="crx-authorrow-name shadowing color-light">Red Black</span>
  </a>
  <span class="color-light formating-space mt20 mb30 blocker">Full description.

Second paragraph.</span>
 </div>
</header>
<a class="color-black link-chapters" data-toggle="modal" data-target="#chapterModal"><span class="strong">2 </span><span class="title-lowercase">CAPÍTULOS</span></a>
<div id="chapterModal" class="modal fade crx-modal">
 <div class="crx-modal-bd"><ul class="no-listing">
  <li><a class="reader-chapter-link" href="/es/story/746625/chapter/fase-uno-variables-de-riesgo-2645438/">1.- Fase Uno: Variables de Riesgo</a></li>
  <li><a class="reader-chapter-link" href="/es/story/746625/chapter/capitulo-1-huracan-2647835/">2.- Capítulo 1: Huracán</a></li>
 </ul></div>
</div>
<div class="comment-block"><span class="formating-space">Reader comment, not the blurb.</span></div>
</body></html>`

// inkspiredStubStoryNoJSONLDHTML is the same page stripped of every JSON-LD
// block, exercising the HTML fallbacks.
const inkspiredStubStoryNoJSONLDHTML = `<!DOCTYPE html><html><head>
<title>Gravedad Cero (Boys Love) | Inkspired</title>
<meta property="og:title" content="Inkspired - Gravedad Cero (Boys Love)"/>
<meta property="og:image" content="https://cdn.getinkspired.com/media/upload/812a0189.png"/>
</head><body>
<header class="pt100 pb30">
 <h1 class="crx-h2 strong">Gravedad Cero (Boys Love)</h1>
 <a class="crx-authorrow color-light blocker" href="/es/u/_boyinred_/">
  <span class="crx-authorrow-name shadowing color-light">Red Black</span>
 </a>
 <span class="color-light formating-space mt20 mb30 blocker">Full description.</span>
</header>
<div id="chapterModal" class="modal fade crx-modal">
 <ul class="no-listing">
  <li><a class="reader-chapter-link" href="/es/story/746625/chapter/fase-uno-variables-de-riesgo-2645438/">1.- Fase Uno: Variables de Riesgo</a></li>
  <li><a class="reader-chapter-link" href="/es/story/746625/chapter/capitulo-1-huracan-2647835/">2.- Capítulo 1: Huracán</a></li>
 </ul>
</div>
</body></html>`

// inkspiredStubChapterHTML mirrors the real reading page: a single
// div.paragraph per paragraph, comment bubbles as siblings of the paragraphs,
// and the paginated reader + chapter navigation kept outside the body.
const inkspiredStubChapterHTML = `<!DOCTYPE html><html><head>
<link rel="canonical" href="` + inkspiredStubChapterURL + `" />
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Chapter",
 "partOf":{"@type":"Book","name":"Gravedad Cero (Boys Love)",
           "author":{"@type":"Person","name":"Red Black"},
           "url":"` + inkspiredStubStoryURL + `"},
 "url":"` + inkspiredStubChapterURL + `"}
</script>
</head><body>
<div id="chapter_block" class="chapter_block mt-50 chapter-2645438">
 <div class="header_controls pt50">
  <h2 class="content_chapter_title_reader z10000" id="scroll-to-2645438">Fase Uno: Variables de Riesgo</h2>
  <span class="sith"><a href="/es/story/746625/chapter/capitulo-1-huracan-2647835/" aria-label="Siguiente capítulo: Capítulo 1: Huracán"></a></span>
 </div>
 <div id="chapter-scroll-content-2645438" class="mt20 jedi">
  <div class="para-wrapper" id="wrapper-b6589fc6">
   <div class="sith sith-right"><a class="comment-bubble">+</a></div>
   <div class="paragraph p5 border10" id="b6589fc6"><p data-paragraph-id="b6589fc6"><u><strong>Variables de Riesgo</strong></u></p></div>
  </div>
  <div class="para-wrapper" id="wrapper-356a192b">
   <div class="sith sith-right"><a class="comment-bubble">+</a></div>
   <div class="paragraph p5 border10" id="356a192b"><p data-paragraph-id="356a192b">—Yo te necesito... con <em>énfasis</em>.</p></div>
  </div>
  <div class="para-wrapper" id="wrapper-da4b9237">
   <div class="sith sith-right"><a class="comment-bubble">+</a></div>
   <div class="paragraph p5 border10" id="da4b9237"><p data-paragraph-id="da4b9237">Segundo párrafo.</p></div>
  </div>
 </div>
 <script id="ink-pages-source-2645438">var pages = [];</script>
 <section class="ink-pages"><section class="ink-pages-stage"></section></section>
</div>
</body></html>`

func newInkspiredStubClient() *inkspiredStubClient {
	return &inkspiredStubClient{
		responses: map[string]string{
			inkspiredStubStoryURL:    inkspiredStubStoryHTML,
			inkspiredStubChapterURL:  inkspiredStubChapterHTML,
			inkspiredStubChapter2URL: inkspiredStubChapterHTML,
		},
	}
}

func TestInkspiredCanHandle(t *testing.T) {
	p := NewInkspiredParser()
	cases := []struct {
		url  string
		want bool
	}{
		{inkspiredStubStoryURL, true},
		{"https://getinkspired.com/story/746625/gravedad-cero-boys-love/", true},
		{"https://www.getinkspired.com/es/story/746625/gravedad-cero-boys-love/", true},
		{"https://getinkspired.com/es/story/746625/", true},
		{inkspiredStubChapterURL, true},
		{"https://getinkspired.com/en/story/746625/chapter/capitulo-1-huracan-2647835/", true},
		{"https://getinkspired.com/es/u/_boyinred_/", false},
		{"https://getinkspired.com/es/discover/romance/", false},
		{"https://getinkspired.com/es/story/746625/chapter/", false},
		{"https://example.com/es/story/746625/gravedad-cero-boys-love/", false},
		{"https://getinkspired.com.attacker.example/es/story/746625/x/", false},
	}
	for _, c := range cases {
		if got := p.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestInkspiredRequiresBrowser(t *testing.T) {
	if !NewInkspiredParser().RequiresBrowser() {
		t.Error("getinkspired.com sits behind Cloudflare; RequiresBrowser must be true")
	}
}

func TestInkspiredGetNovelInfo(t *testing.T) {
	p := NewInkspiredParser()

	info, err := p.GetNovelInfo(context.Background(), newInkspiredStubClient(), inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "Gravedad Cero (Boys Love)" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "Red Black" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	// The rendered blurb wins over the JSON-LD copy, which the site caps at
	// 500 characters, and keeps its paragraph break.
	if info.Description != "Full description.\n\nSecond paragraph." {
		t.Errorf("unexpected description: %q", info.Description)
	}
	if info.CoverURL != "https://cdn.getinkspired.com/media/upload/812a0189.png" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if info.SourceURL != inkspiredStubStoryURL {
		t.Errorf("unexpected source URL: %q", info.SourceURL)
	}
	if len(info.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(info.Chapters))
	}
	// hasPart titles carry no "1.- " position prefix and start at Order 1.
	if info.Chapters[0].Title != "Fase Uno: Variables de Riesgo" {
		t.Errorf("unexpected first chapter title: %q", info.Chapters[0].Title)
	}
	if info.Chapters[0].URL != inkspiredStubChapterURL || info.Chapters[0].Order != 1 {
		t.Errorf("unexpected first chapter: %+v", info.Chapters[0])
	}
	if info.Chapters[1].Title != "Capítulo 1: Huracán" || info.Chapters[1].Order != 2 {
		t.Errorf("unexpected second chapter: %+v", info.Chapters[1])
	}
}

func TestInkspiredGetNovelInfoFromChapterURL(t *testing.T) {
	// A reading-page URL must resolve to the parent story via partOf.
	p := NewInkspiredParser()

	info, err := p.GetNovelInfo(context.Background(), newInkspiredStubClient(), inkspiredStubChapterURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Gravedad Cero (Boys Love)" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.SourceURL != inkspiredStubStoryURL {
		t.Errorf("unexpected source URL: %q", info.SourceURL)
	}
	if len(info.Chapters) != 2 {
		t.Errorf("expected 2 chapters, got %d", len(info.Chapters))
	}
}

func TestInkspiredGetNovelInfoHTMLFallback(t *testing.T) {
	// Without JSON-LD the page header and the modal TOC must still produce a
	// complete NovelInfo, and the modal's "1.- " prefixes must be stripped.
	p := NewInkspiredParser()
	client := &inkspiredStubClient{responses: map[string]string{
		inkspiredStubStoryURL: inkspiredStubStoryNoJSONLDHTML,
	}}

	info, err := p.GetNovelInfo(context.Background(), client, inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "Gravedad Cero (Boys Love)" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "Red Black" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	// header span.formating-space, not the reader comment further down.
	if info.Description != "Full description." {
		t.Errorf("unexpected description: %q", info.Description)
	}
	if info.CoverURL != "https://cdn.getinkspired.com/media/upload/812a0189.png" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if len(info.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(info.Chapters))
	}
	if info.Chapters[0].Title != "Fase Uno: Variables de Riesgo" {
		t.Errorf("modal position prefix not stripped: %q", info.Chapters[0].Title)
	}
	if info.Chapters[0].URL != inkspiredStubChapterURL {
		t.Errorf("relative chapter href not resolved: %q", info.Chapters[0].URL)
	}
}

func TestInkspiredGetNovelInfoBadURL(t *testing.T) {
	p := NewInkspiredParser()
	if _, err := p.GetNovelInfo(context.Background(), newInkspiredStubClient(), "https://example.com/es/story/1/x/"); err == nil {
		t.Error("expected error for non-inkspired URL, got nil")
	}
}

// inkspiredStubStoryNoBlurbHTML carries a Book JSON-LD block but no rendered
// blurb, so the description must fall through to the JSON-LD copy.
const inkspiredStubStoryNoBlurbHTML = `<!DOCTYPE html><html><head>
<meta property="og:image" content="https://cdn.getinkspired.com/media/upload/812a0189.png"/>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Book",
 "name":"Gravedad Cero (Boys Love)",
 "author":{"@type":"Person","name":"Red Black"},
 "description":"JSON-LD only description.",
 "url":"` + inkspiredStubStoryURL + `",
 "hasPart":[{"@type":"Chapter","name":"Capítulo 1: Huracán","url":"` + inkspiredStubChapter2URL + `"}]}
</script>
</head><body></body></html>`

func TestInkspiredGetNovelInfoDescriptionFromJSONLD(t *testing.T) {
	p := NewInkspiredParser()
	client := &inkspiredStubClient{responses: map[string]string{
		inkspiredStubStoryURL: inkspiredStubStoryNoBlurbHTML,
	}}

	info, err := p.GetNovelInfo(context.Background(), client, inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Description != "JSON-LD only description." {
		t.Errorf("unexpected description: %q", info.Description)
	}
	if len(info.Chapters) != 1 {
		t.Errorf("expected 1 chapter, got %d", len(info.Chapters))
	}
}

func TestInkspiredParseChapter(t *testing.T) {
	p := NewInkspiredParser()

	ch, err := p.ParseChapter(context.Background(), newInkspiredStubClient(), inkspiredStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Fase Uno: Variables de Riesgo" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	if ch.SourceURL != inkspiredStubChapterURL {
		t.Errorf("unexpected source URL: %q", ch.SourceURL)
	}
	for _, want := range []string{"Variables de Riesgo", "—Yo te necesito", "Segundo párrafo."} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
	// Inline markup must survive as HTML for the markdown conversion.
	if !strings.Contains(ch.Content, "<u><strong>Variables de Riesgo</strong></u>") {
		t.Errorf("content lost inline markup:\n%s", ch.Content)
	}
	if !strings.Contains(ch.Content, "<em>énfasis</em>") {
		t.Errorf("content lost emphasis:\n%s", ch.Content)
	}
	// One <p> per source paragraph, and nothing from the surrounding chrome.
	if got := strings.Count(ch.Content, "<p>"); got != 3 {
		t.Errorf("expected 3 paragraphs, got %d:\n%s", got, ch.Content)
	}
	for _, unwanted := range []string{"comment-bubble", "ink-pages", "Siguiente capítulo"} {
		if strings.Contains(ch.Content, unwanted) {
			t.Errorf("content leaked page chrome %q:\n%s", unwanted, ch.Content)
		}
	}
}

func TestInkspiredParseChapterBadURL(t *testing.T) {
	p := NewInkspiredParser()
	client := newInkspiredStubClient()

	if _, err := p.ParseChapter(context.Background(), client, inkspiredStubStoryURL); err == nil {
		t.Error("expected error for story page URL, got nil")
	}
	if _, err := p.ParseChapter(context.Background(), client, "https://example.com/es/story/746625/chapter/x-1/"); err == nil {
		t.Error("expected error for non-inkspired URL, got nil")
	}
}

func TestInkspiredGetChapterURLs(t *testing.T) {
	p := NewInkspiredParser()
	client := newInkspiredStubClient()

	doc, err := client.FetchDocument(context.Background(), inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("FetchDocument: %v", err)
	}

	fromDoc, err := p.GetChapterURLs(context.Background(), client, doc, inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("GetChapterURLs(doc): %v", err)
	}
	// A nil doc must trigger the fetch instead of panicking.
	fromFetch, err := p.GetChapterURLs(context.Background(), client, nil, inkspiredStubStoryURL)
	if err != nil {
		t.Fatalf("GetChapterURLs(nil): %v", err)
	}
	if len(fromDoc) != 2 || len(fromFetch) != 2 {
		t.Fatalf("expected 2 chapters from both paths, got %d and %d", len(fromDoc), len(fromFetch))
	}
	if fromDoc[1].Title != "Capítulo 1: Huracán" || fromDoc[1].URL != inkspiredStubChapter2URL {
		t.Errorf("unexpected chapter: %+v", fromDoc[1])
	}
}
