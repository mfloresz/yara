package parserhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type stubFetcher struct {
	pages map[string]string
	calls int
}

func (f *stubFetcher) Fetch(_ context.Context, rawURL string) (*FetchResult, error) {
	f.calls++
	body, ok := f.pages[rawURL]
	if !ok {
		return nil, fmt.Errorf("network unreachable: %s", rawURL)
	}
	return &FetchResult{FinalURL: rawURL, Status: 200, Body: []byte(body)}, nil
}

func loadScript(t *testing.T, src string, opts Options) (*Script, *stubFetcher) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parser.js")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	fetcher := &stubFetcher{pages: map[string]string{}}
	script, err := NewEngine(fetcher, opts).LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	return script, fetcher
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", want)
	}
	var scriptErr *ScriptError
	if !errors.As(err, &scriptErr) {
		t.Fatalf("expected *ScriptError with code %s, got %T: %v", want, err, err)
	}
	if scriptErr.Code != want {
		t.Fatalf("expected code %s, got %s (%s)", want, scriptErr.Code, scriptErr.Message)
	}
}

const happyScript = `
module.exports = {
  name: "testsite",
  apiVersion: 1,
  requiresBrowser: true,
  probe: (url) => url.indexOf("testsite.example") >= 0,
  toc: (ctx, url) => {
    const doc = ctx.get(url);
    ctx.log("fetched " + doc.url);
    if (doc.status !== 200) ctx.fail("blocked", "status " + doc.status);
    const title = ctx.css1(doc, "h1.title");
    if (!title) ctx.fail("site_layout_changed", "no h1.title");
    const author = ctx.css1(doc, "span.author");
    const cover = ctx.css1(doc, "meta.cover");
    const blurb = ctx.css1(doc, "p.blurb");
    const chapters = [];
    const links = ctx.css(doc, "ul.chapters a");
    for (let i = 0; i < links.length; i++) {
      chapters.push({ title: links[i].text, url: links[i].href });
    }
    return {
      novel: {
        title: title.text,
        author: author.text,
        coverUrl: cover.attr("content"),
        description: blurb.text,
        language: "en",
        tags: ["fantasy", "adventure"]
      },
      chapters: chapters
    };
  },
  chapter: (ctx, url) => {
    const doc = ctx.get(url);
    const title = ctx.css1(doc, "h2.chapter-title");
    if (!title) ctx.fail("site_layout_changed", "no chapter title");
    const content = ctx.css1(doc, "div.content");
    if (!content) ctx.fail("site_layout_changed", "no div.content");
    const junk = ctx.css(content, "script, style, noscript, .ad, .hidden");
    for (let i = 0; i < junk.length; i++) {
      junk[i].remove();
    }
    return { title: title.text, contentHtml: content.html };
  },
  chapterKey: (ch) => ch.url.replace(/\/$/, "")
};
`

func TestTOCAndChapter(t *testing.T) {
	script, fetcher := loadScript(t, happyScript, Options{})
	fetcher.pages["https://testsite.example/novel/slug"] = `<html><head>
<meta class="cover" content="/img/cover.jpg">
</head><body>
<h1 class="title">  The Silent Shelf  </h1>
<span class="author">Ada Lovelace</span>
<p class="blurb">A novel about shelves.</p>
<ul class="chapters">
  <li><a href="chapter-1">Chapter One</a></li>
  <li><a href="/novel/slug/chapter-2">Chapter Two</a></li>
</ul>
</body></html>`
	fetcher.pages["https://testsite.example/novel/slug/chapter-1"] = `<html><body>
<h2 class="chapter-title">Chapter One</h2>
<div class="content"><p>First line.</p><script>tracker()</script><style>p{color:red}</style><span class="ad">buy now</span><p class="hidden">gone</p><p>Second line.</p></div>
</body></html>`

	toc, err := script.TOC(context.Background(), "https://testsite.example/novel/slug")
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	wantNovel := Novel{
		Title:       "The Silent Shelf",
		Description: "A novel about shelves.",
		Author:      "Ada Lovelace",
		CoverURL:    "/img/cover.jpg",
		Language:    "en",
		Tags:        []string{"fantasy", "adventure"},
	}
	if !reflect.DeepEqual(toc.Novel, wantNovel) {
		t.Errorf("novel:\n got %+v\nwant %+v", toc.Novel, wantNovel)
	}
	// "chapter-1" resolves against the base directory of the page URL, while
	// "/novel/slug/chapter-2" is already root-absolute.
	wantChapters := []ChapterRef{
		{Title: "Chapter One", URL: "https://testsite.example/novel/chapter-1"},
		{Title: "Chapter Two", URL: "https://testsite.example/novel/slug/chapter-2"},
	}
	if !reflect.DeepEqual(toc.Chapters, wantChapters) {
		t.Errorf("chapters:\n got %+v\nwant %+v", toc.Chapters, wantChapters)
	}

	chapter, err := script.Chapter(context.Background(), "https://testsite.example/novel/slug/chapter-1")
	if err != nil {
		t.Fatalf("Chapter: %v", err)
	}
	if chapter.Title != "Chapter One" {
		t.Errorf("title: got %q", chapter.Title)
	}
	const wantHTML = "<p>First line.</p><p>Second line.</p>"
	if chapter.ContentHTML != wantHTML {
		t.Errorf("contentHtml:\n got %q\nwant %q", chapter.ContentHTML, wantHTML)
	}

	key, err := script.ChapterKey(ChapterRef{Title: "Chapter One", URL: "https://testsite.example/novel/slug/chapter-1/"})
	if err != nil {
		t.Fatalf("ChapterKey: %v", err)
	}
	if key != "https://testsite.example/novel/slug/chapter-1" {
		t.Errorf("chapterKey: got %q", key)
	}
}

func TestChapterKeyDefaultsToURL(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "nokey", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => ({ novel: { title: "t" }, chapters: [] }),
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{})

	key, err := script.ChapterKey(ChapterRef{URL: "https://x.example/c/1"})
	if err != nil {
		t.Fatalf("ChapterKey: %v", err)
	}
	if key != "https://x.example/c/1" {
		t.Errorf("got %q, want the chapter URL", key)
	}
}

func TestErrorTaxonomy(t *testing.T) {
	tests := []struct {
		name string
		toc  string
		want string
	}{
		{"not my site", `ctx.fail("not_my_site", "wrong host");`, CodeNotMySite},
		{"layout changed", `ctx.fail("site_layout_changed", "no chapters");`, CodeSiteLayoutChanged},
		{"blocked", `ctx.fail("blocked", "cloudflare");`, CodeBlocked},
		{"invalid code", `ctx.fail("kaboom", "typo");`, CodeScriptError},
		{"uncaught throw", `throw new Error("boom");`, CodeScriptError},
		{"string throw", `throw "boom";`, CodeScriptError},
		{"no novel", `return { chapters: [] };`, CodeScriptError},
		{"no chapters", `return { novel: { title: "t" } };`, CodeScriptError},
		{"blank novel title", `return { novel: { title: "  " }, chapters: [] };`, CodeScriptError},
		{"chapter not an object", `return [];`, CodeScriptError},
		{"chapter missing url", `return { novel: { title: "t" }, chapters: [{ title: "c" }] };`, CodeScriptError},
		{"chapter blank url", `return { novel: { title: "t" }, chapters: [{ title: "c", url: " " }] };`, CodeScriptError},
		{"chapter missing title", `return { novel: { title: "t" }, chapters: [{ url: "/c" }] };`, CodeScriptError},
		{"not a function", `return 42;`, CodeScriptError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script, _ := loadScript(t, fmt.Sprintf(`
module.exports = {
  name: "taxonomy", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => { %s },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, tc.toc), Options{})

			_, err := script.TOC(context.Background(), "https://x.example/")
			assertCode(t, err, tc.want)
		})
	}
}

func TestChapterShapeValidation(t *testing.T) {
	tests := []struct {
		name    string
		chapter string
	}{
		{"missing contentHtml", `return { title: "t" };`},
		{"blank contentHtml", `return { title: "t", contentHtml: "   " };`},
		{"not an object", `return "nope";`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script, _ := loadScript(t, fmt.Sprintf(`
module.exports = {
  name: "shape", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => ({ novel: { title: "t" }, chapters: [] }),
  chapter: (ctx, url) => { %s }
};`, tc.chapter), Options{})

			_, err := script.Chapter(context.Background(), "https://x.example/c/1")
			assertCode(t, err, CodeScriptError)
		})
	}
}

func TestFetcherErrorStaysPlainGoError(t *testing.T) {
	script, _ := loadScript(t, happyScript, Options{})

	_, err := script.TOC(context.Background(), "https://testsite.example/unreachable")
	if err == nil {
		t.Fatal("expected a network error")
	}
	var scriptErr *ScriptError
	if errors.As(err, &scriptErr) {
		t.Fatalf("fetcher error must not be a *ScriptError, got %+v", scriptErr)
	}
	if got := err.Error(); got != "fetching https://testsite.example/unreachable: network unreachable: https://testsite.example/unreachable" {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestFetchBudgetExceeded(t *testing.T) {
	script, fetcher := loadScript(t, `
module.exports = {
  name: "budget", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    ctx.get(url + "1");
    ctx.get(url + "2");
    return { novel: { title: "t" }, chapters: [] };
  },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{MaxFetches: 1})
	fetcher.pages["https://x.example/1"] = "one"
	fetcher.pages["https://x.example/2"] = "two"

	_, err := script.TOC(context.Background(), "https://x.example/")
	assertCode(t, err, CodeParserTimeout)
	if fetcher.calls != 1 {
		t.Errorf("expected the budget to stop the second fetch, got %d fetches", fetcher.calls)
	}
}

func TestTimeoutInterruptsVM(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "spin", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => { while (true) {} },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{Timeout: 200 * time.Millisecond})

	start := time.Now()
	_, err := script.TOC(context.Background(), "https://x.example/")
	assertCode(t, err, CodeParserTimeout)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("interrupt took %s, expected it to fire near the 200ms limit", elapsed)
	}
}

func TestContextCancellationInterruptsVM(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "spin", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => { while (true) {} },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{Timeout: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := script.TOC(ctx, "https://x.example/")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %T: %v", err, err)
	}
}

func TestVMIsolationBetweenInvocations(t *testing.T) {
	script, fetcher := loadScript(t, `
let calls = 0;
module.exports = {
  name: "stateful", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    calls++;
    globalThis.leaked = "set-by-toc";
    return { novel: { title: "run-" + calls }, chapters: [{ title: "c", url: url }] };
  },
  chapter: (ctx, url) => ({ title: globalThis.leaked === undefined ? "clean" : "leaked", contentHtml: "<p>x</p>" })
};`, Options{})
	fetcher.pages["https://x.example/"] = "page"

	for i := 0; i < 3; i++ {
		toc, err := script.TOC(context.Background(), "https://x.example/")
		if err != nil {
			t.Fatalf("TOC run %d: %v", i, err)
		}
		if toc.Novel.Title != "run-1" {
			t.Fatalf("run %d saw module state from a previous invocation: %q", i, toc.Novel.Title)
		}
	}
	chapter, err := script.Chapter(context.Background(), "https://x.example/c/1")
	if err != nil {
		t.Fatalf("Chapter: %v", err)
	}
	if chapter.Title != "clean" {
		t.Errorf("globalThis leaked between invocations: %q", chapter.Title)
	}
}

func TestProbe(t *testing.T) {
	script, _ := loadScript(t, happyScript, Options{})

	tests := []struct {
		url  string
		want bool
	}{
		{"https://testsite.example/novel/slug", true},
		{"https://testsite.example/novel/slug/chapter-1", true},
		{"https://other.example/novel/slug", false},
	}
	for _, tc := range tests {
		got, err := script.Probe(tc.url)
		if err != nil {
			t.Fatalf("Probe(%s): %v", tc.url, err)
		}
		if got != tc.want {
			t.Errorf("Probe(%s) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestProbeMustReturnBoolean(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "sloppy", apiVersion: 1,
  probe: (url) => { },
  toc: (ctx, url) => ({ novel: { title: "t" }, chapters: [] }),
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{})

	_, err := script.Probe("https://x.example/")
	assertCode(t, err, CodeScriptError)
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	beta := filepath.Join(dir, "beta.js")
	alpha := filepath.Join(dir, "alpha.js")
	ignored := filepath.Join(dir, "notes.txt")
	body := `
module.exports = {
  name: %q, apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => ({ novel: { title: "t" }, chapters: [] }),
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`
	for path, name := range map[string]string{alpha: "alpha", beta: "beta", ignored: "ignored"} {
		if err := os.WriteFile(path, []byte(fmt.Sprintf(body, name)), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	scripts, err := NewEngine(&stubFetcher{pages: map[string]string{}}, Options{}).LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scripts) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(scripts))
	}
	if scripts[0].Name() != "alpha" || scripts[1].Name() != "beta" {
		t.Errorf("expected [alpha beta], got [%s %s]", scripts[0].Name(), scripts[1].Name())
	}
}

func TestLoadRejectsBadContracts(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"syntax error", `module.exports = {`, "compiling parser script"},
		{"no exports", `var x = 1;`, "module.exports must be an object"},
		{"missing name", `module.exports = { apiVersion: 1, probe(){}, toc(){}, chapter(){} };`, "name must be a non-empty string"},
		{"wrong api version", `module.exports = { name: "s", apiVersion: 2, probe(){}, toc(){}, chapter(){} };`, "apiVersion must be 1"},
		{"missing probe", `module.exports = { name: "s", apiVersion: 1, toc(){}, chapter(){} };`, "probe must be a function"},
		{"missing toc", `module.exports = { name: "s", apiVersion: 1, probe(){}, chapter(){} };`, "toc must be a function"},
		{"missing chapter", `module.exports = { name: "s", apiVersion: 1, probe(){}, toc(){} };`, "chapter must be a function"},
		{"throwing init", `throw new Error("bad init"); module.exports = {};`, "bad init"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parser.js")
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatalf("writing script: %v", err)
			}
			_, err := NewEngine(&stubFetcher{pages: map[string]string{}}, Options{}).LoadFile(path)
			if err == nil {
				t.Fatalf("expected a load error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestMaxBodyBytes(t *testing.T) {
	script, fetcher := loadScript(t, happyScript, Options{MaxBodyBytes: 16})
	fetcher.pages["https://x.example/big"] = "0123456789abcdefghij"

	_, err := script.TOC(context.Background(), "https://x.example/big")
	assertCode(t, err, CodeScriptError)
	var scriptErr *ScriptError
	errors.As(err, &scriptErr)
	if !strings.Contains(scriptErr.Message, "exceeds limit") {
		t.Errorf("unexpected message: %q", scriptErr.Message)
	}
}

func TestNodeNamePreservesDocumentOrderAcrossTags(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "order", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    const body = "<div id='c'><h4>Julian</h4><p>one</p><hr><p>two</p></div>";
    const tags = ctx.css(body, "#c p, #c h4, #c hr").map((n) => n.nodeName);
    const nested = ctx.css(ctx.css1(body, "#c"), "hr")[0].nodeName;
    return {
      novel: { title: "n", description: tags.join(",") + "|" + nested },
      chapters: [{ title: "t", url: url }]
    };
  },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{})

	toc, err := script.TOC(context.Background(), "https://x.example/page")
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	// ctx.css returns matches in document order, not grouped by selector.
	if want := "h4,p,hr,p|hr"; toc.Novel.Description != want {
		t.Errorf("got %q, want %q", toc.Novel.Description, want)
	}
}

func TestCSSAgainstHTMLStringAndNode(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "targets", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    const nodes = ctx.css("<div id='x'><a href='/a'>A</a><a href='/b'>B</a></div>", "#x a");
    const first = ctx.css1("<div id='y'><a href='/c'>C</a></div>", "#y a");
    const missing = ctx.css1(url, ".nope");
    return {
      novel: {
        title: "n",
        description: nodes.length + "|" + first.text + "|" + first.href + "|" + (missing === null)
      },
      chapters: [{ title: "t", url: url }]
    };
  },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{})

	toc, err := script.TOC(context.Background(), "https://x.example/page")
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	if want := "2|C|/c|true"; toc.Novel.Description != want {
		t.Errorf("got %q, want %q", toc.Novel.Description, want)
	}
}

// Runaway recursion must hit the VM's call-stack cap quickly instead of
// growing the heap until the process dies: goja's default cap is MaxInt32
// and the wall-clock interrupt fires far too late to prevent that.
func TestRecursionHitsCallStackCap(t *testing.T) {
	script, _ := loadScript(t, `
module.exports = {
  name: "recurse", apiVersion: 1,
  probe: () => { const f = () => f(); f(); return true; },
  toc: () => ({ novel: { title: "t" }, chapters: [] }),
  chapter: () => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{Timeout: 10 * time.Second})
	start := time.Now()
	_, err := script.Probe("https://x.example/")
	if err == nil {
		t.Fatal("unbounded recursion completed without error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("recursion took %v to fail; the call-stack cap is not armed", elapsed)
	}
	// goja reports the overflow as a plain script error (its message is empty
	// at the top level); what matters is that it fails fast and is not
	// misreported as the wall-clock limit.
	assertCode(t, err, CodeScriptError)
}

// A script must not be able to swallow the host's fetch budget with try/catch.
// The budget aborts through the VM interrupt flag, which always escapes
// try/catch, so a `while (true) { try { ctx.get() } catch {} }` loop still
// stops at the limit instead of spinning for the whole wall-clock budget.
func TestFetchBudgetCannotBeCaught(t *testing.T) {
	script, fetcher := loadScript(t, `
module.exports = {
  name: "greedy", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    for (var i = 0; i < 100; i++) {
      try {
        ctx.get(url + i);
      } catch (e) {
        // Swallowing the budget must not extend it.
      }
    }
    return { novel: { title: "t" }, chapters: [] };
  },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{MaxFetches: 3, Timeout: 5 * time.Second})

	_, err := script.TOC(context.Background(), "https://x.example/")
	assertCode(t, err, CodeParserTimeout)
	if fetcher.calls != 3 {
		t.Errorf("expected the budget to stop the script at 3 fetches, got %d", fetcher.calls)
	}
}

// The body-size limit is equally uncatchable: a script that catches it must not
// be handed an over-limit body to parse.
func TestBodyLimitCannotBeCaught(t *testing.T) {
	script, fetcher := loadScript(t, `
module.exports = {
  name: "greedybody", apiVersion: 1,
  probe: () => true,
  toc: (ctx, url) => {
    try {
      ctx.get(url);
    } catch (e) {
    }
    return { novel: { title: "t" }, chapters: [] };
  },
  chapter: (ctx, url) => ({ title: "t", contentHtml: "<p>x</p>" })
};`, Options{MaxBodyBytes: 16, Timeout: 5 * time.Second})
	fetcher.pages["https://x.example/big"] = "0123456789abcdefghij"

	_, err := script.TOC(context.Background(), "https://x.example/big")
	assertCode(t, err, CodeScriptError)
	var scriptErr *ScriptError
	errors.As(err, &scriptErr)
	if !strings.Contains(scriptErr.Message, "exceeds limit") {
		t.Errorf("unexpected message: %q", scriptErr.Message)
	}
}
