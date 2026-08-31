package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// novelArrowStubClient returns canned payloads keyed by URL so parser tests run
// without hitting the live site.
type novelArrowStubClient struct {
	responses map[string]string
}

func (c *novelArrowStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *novelArrowStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *novelArrowStubClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*novelArrowStubClient)(nil)

const novelArrowNovelURL = "https://novelarrow.com/novel/qt-the-rescue-of-the-miserable-bigshots"

const novelArrowStubNovelPage = `<!DOCTYPE html><html><head><title>QT novel</title></head><body>
<h1 class="text-[19px] font-extrabold">QT: The Rescue Of The Miserable Bigshots</h1>
<a class="font-medium" href="/author/latteectrie?name=latteectrie">latteectrie</a>
<div class="site-reading-copy space-y-4 pt-1"><p>In the novel, there are always characters who are strong.</p><p>Jiang Jiamian&#x27;s mission is to save them.</p></div>
<div class="site-radius-sm relative h-full w-full overflow-hidden"><span class="novel-cover-frame h-full w-full object-cover"><img src="https://images.novelarrow.com/novel_480_720/qt-the-rescue-of-the-miserable-bigshots.jpg" srcSet="..."/></span></div>
</body></html>`

const novelArrowStubChapters = `{"items":[
  {"chapter_id":"chapter-11-school-prince-x-school-bully","chapter_name":"Chapter 1.1 School Prince x School Bully","premium_content":false},
  {"chapter_id":"chapter-2-12","chapter_name":"Chapter 2 - 1.2","premium_content":false},
  {"chapter_id":"chapter-358-124","chapter_name":"Chapter 358 - 12.4","premium_content":false}
],"pagination":{"page":1,"total":3,"totalPages":1}}`

const novelArrowStubChapterContent = `<h4>Chapter 1: 1.1 School Prince x School Bully</h4><p> Jiang Jiamian couldn't help but raise his hand to rub his throbbing forehead.</p><p> "Leave me alone!" The girl's voice sounded soft, "It only makes me sick when I see you!"</p><p> It looks like his first mission target is a bit dangerous.</p>`

const novelArrowStubChapterURL = "https://novelarrow.com/chapter/qt-the-rescue-of-the-miserable-bigshots/chapter-11-school-prince-x-school-bully"

// novelArrowJSEncode mirrors how the site's flight chunk escapes the chapter
// HTML inside the JS string literal.
func novelArrowJSEncode(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`'`, `\'`,
		"\n", `\n`,
		`<`, `\u003c`,
		`>`, `\u003e`,
		`&`, `\u0026`,
	)
	return r.Replace(s)
}

func novelArrowStubChapterPage() string {
	return `<!DOCTYPE html><html><head><meta name="og:novel:chapter_name" content="Chapter 1.1 School Prince x School Bully"/></head><body>` +
		`<script>self.__next_f.push([1,"` + novelArrowJSEncode(novelArrowStubChapterContent) + `"])</script>` +
		`<script>self.__next_f.push([1,"9:{..."])</script>` +
		`</body></html>`
}

func novelArrowStubURLs() map[string]string {
	return map[string]string{
		novelArrowNovelURL: novelArrowStubNovelPage,
		"https://novelarrow.com/api-web/novels/qt-the-rescue-of-the-miserable-bigshots/chapters?sort=asc": novelArrowStubChapters,
		novelArrowStubChapterURL: novelArrowStubChapterPage(),
	}
}

func TestNovelArrowCanHandle(t *testing.T) {
	p := NewNovelArrowParser()
	cases := []struct {
		url  string
		want bool
	}{
		{novelArrowNovelURL, true},
		{novelArrowStubChapterURL, true},
		{"https://www.novelarrow.com/novel/some-slug", true},
		{"https://novelarrow.com/novel/some-slug/", true},
		{"https://novelarrow.com/chapter/some-slug/chapter-1-11", true},
		{"https://novelarrow.com/", false},
		{"https://novelarrow.com/ranking/daily", false},
		{"https://example.com/novel/some-slug", false},
		{"https://novelarrow.com.attacker.example/novel/some-slug", false},
	}
	for _, c := range cases {
		if got := p.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestNovelArrowGetNovelInfo(t *testing.T) {
	p := NewNovelArrowParser()
	client := &novelArrowStubClient{responses: novelArrowStubURLs()}

	info, err := p.GetNovelInfo(context.Background(), client, novelArrowNovelURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "QT: The Rescue Of The Miserable Bigshots" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "latteectrie" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	wantDesc := "In the novel, there are always characters who are strong.\n\nJiang Jiamian's mission is to save them."
	if info.Description != wantDesc {
		t.Errorf("unexpected description: %q", info.Description)
	}
	if info.CoverURL != "https://images.novelarrow.com/novel_480_720/qt-the-rescue-of-the-miserable-bigshots.jpg" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(info.Chapters))
	}
	if first := info.Chapters[0]; first.Title != "Chapter 1.1 School Prince x School Bully" || first.URL != novelArrowStubChapterURL {
		t.Errorf("unexpected first chapter: %+v", first)
	}
	if last := info.Chapters[2]; last.URL != "https://novelarrow.com/chapter/qt-the-rescue-of-the-miserable-bigshots/chapter-358-124" {
		t.Errorf("unexpected last chapter URL: %q", last.URL)
	}
}

func TestNovelArrowGetNovelInfoNotNovelPage(t *testing.T) {
	p := NewNovelArrowParser()
	client := &novelArrowStubClient{responses: novelArrowStubURLs()}

	if _, err := p.GetNovelInfo(context.Background(), client, novelArrowStubChapterURL); err == nil {
		t.Error("expected error for chapter URL, got nil")
	}
	if _, err := p.GetNovelInfo(context.Background(), client, "https://example.com/novel/foo"); err == nil {
		t.Error("expected error for non-novelarrow URL, got nil")
	}
}

func TestNovelArrowGetChapterURLs(t *testing.T) {
	p := NewNovelArrowParser()
	client := &novelArrowStubClient{responses: novelArrowStubURLs()}

	chapters, err := p.GetChapterURLs(context.Background(), client, nil, novelArrowNovelURL)
	if err != nil {
		t.Fatalf("GetChapterURLs: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	wantURLs := map[int]string{
		1: "chapter-11-school-prince-x-school-bully",
		2: "chapter-2-12",
		3: "chapter-358-124",
	}
	for i, ch := range chapters {
		if ch.Title == "" {
			t.Errorf("chapter %d has empty title", i+1)
		}
		if !strings.Contains(ch.URL, wantURLs[i+1]) {
			t.Errorf("chapter %d URL = %q, want it to contain %q", i+1, ch.URL, wantURLs[i+1])
		}
	}
}

func TestNovelArrowParseChapter(t *testing.T) {
	p := NewNovelArrowParser()
	client := &novelArrowStubClient{responses: novelArrowStubURLs()}

	ch, err := p.ParseChapter(context.Background(), client, novelArrowStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1: 1.1 School Prince x School Bully" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	// The decoded HTML must keep paragraph breaks and unescaped apostrophes.
	for _, want := range []string{
		"<h4>Chapter 1: 1.1 School Prince x School Bully</h4>",
		"<p> Jiang Jiamian couldn't help but raise his hand to rub his throbbing forehead.</p>",
		`<p> "Leave me alone!" The girl's voice sounded soft, "It only makes me sick when I see you!"</p>`,
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestNovelArrowParseChapterMetaTitleFallback(t *testing.T) {
	// When the fragment has no leading heading, the parser must fall back to
	// the og:novel:chapter_name meta of the page.
	p := NewNovelArrowParser()
	content := `<p>Chapter body without a heading.</p>`
	page := `<!DOCTYPE html><html><head><meta name="og:novel:chapter_name" content="Chapter 5 - Title"/></head><body>` +
		`<script>self.__next_f.push([1,"` + novelArrowJSEncode(content) + `"])</script>` +
		`</body></html>`
	client := &novelArrowStubClient{responses: map[string]string{novelArrowStubChapterURL: page}}

	ch, err := p.ParseChapter(context.Background(), client, novelArrowStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 5 - Title" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	if !strings.Contains(ch.Content, "Chapter body without a heading.") {
		t.Errorf("unexpected content: %q", ch.Content)
	}
}

func TestNovelArrowParseChapterBadURL(t *testing.T) {
	p := NewNovelArrowParser()
	client := &novelArrowStubClient{responses: novelArrowStubURLs()}

	if _, err := p.ParseChapter(context.Background(), client, novelArrowNovelURL); err == nil {
		t.Error("expected error for novel page URL, got nil")
	}
	if _, err := p.ParseChapter(context.Background(), client, "https://example.com/novel/foo/chapter-1"); err == nil {
		t.Error("expected error for non-novelarrow URL, got nil")
	}
}

func TestNovelArrowDecodeJSString(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"unicode escapes", `\u003ch4\u003eHi\u003c/h4\u003e`, "<h4>Hi</h4>"},
		{"ampersand", `a\u0026b`, "a&b"},
		{"apostrophe", `It\'s fine`, "It's fine"},
		{"double quote", `say \"hi\"`, `say "hi"`},
		{"newline", `line1\nline2`, "line1\nline2"},
		{"backslash", `a\\b`, `a\b`},
		{"carriage return", `a\rb`, "a\rb"},
		{"hex escape", `\x41`, "A"},
		{"code point escape", `\u{1F600}`, "\U0001F600"},
		{"void", ``, ``},
		{"plain text", "hello world", "hello world"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := novelArrowDecodeJSString(c.input)
			if err != nil {
				t.Fatalf("DecodeJSString: %v", err)
			}
			if got != c.want {
				t.Errorf("decode(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestNovelArrowDecodeJSSurrogatePair(t *testing.T) {
	// Two \uXXXX escapes forming a surrogate pair decode to a single rune.
	got, err := novelArrowDecodeJSString(`\uD83D\uDE00`)
	if err != nil {
		t.Fatalf("DecodeJSString: %v", err)
	}
	if got != "\U0001F600" {
		t.Errorf("decode = %q, want %q", got, "\U0001F600")
	}
}

func TestNovelArrowExtractContentMissing(t *testing.T) {
	if _, err := novelArrowExtractContent([]byte("<html><body><p>no flight scripts</p></body></html>")); err == nil {
		t.Error("expected error when no flight chunk is present, got nil")
	}
}