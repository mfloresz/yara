package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"translator-server/internal/parserhost"
)

func TestDiffNovelSnapshotMatchesOnSourceKey(t *testing.T) {
	snapshot := &sourceSnapshot{Chapters: []sourceChapter{
		{Title: "Chapter 1", URL: "https://site/a", Key: "https://site/a", Order: 1},
		{Title: "Chapter 2", URL: "https://site/b", Key: "https://site/b", Order: 2},
		{Title: "Chapter 3", URL: "https://site/c", Key: "https://site/c", Order: 3},
	}}

	// The novel is in keyed mode (one key is stored), so identity is
	// authoritative. "Chapter 3" collides with a stored *title*, but that must
	// not hide it: its key is new, so it is a new chapter.
	newChapters, missing := diffNovelSnapshot(snapshot,
		map[string]bool{"https://site/a": true},
		map[int]bool{1: true},
		map[string]bool{"Chapter 3": true},
	)
	if len(newChapters) != 2 {
		t.Fatalf("expected 2 new chapters, got %d (%+v)", len(newChapters), newChapters)
	}
	if newChapters[0].Key != "https://site/b" || newChapters[1].Key != "https://site/c" {
		t.Errorf("unexpected new chapters: %+v", newChapters)
	}
	// Nothing stored is missing from this snapshot.
	if len(missing) != 0 {
		t.Errorf("expected no missing keys, got %v", missing)
	}
}

func TestDiffNovelSnapshotFallsBackToTitleAndOrder(t *testing.T) {
	// No keys stored at all: the novel predates source_key, so the historical
	// title/order heuristic decides, and a fully-downloaded novel reports
	// nothing new instead of its whole library.
	snapshot := &sourceSnapshot{Chapters: []sourceChapter{
		{Title: "Chapter 1", URL: "https://site/a", Order: 1},
		{Title: "Chapter 2", URL: "https://site/b", Order: 2},
		{Title: "Chapter 3", URL: "https://site/c", Order: 3},
	}}

	newChapters, _ := diffNovelSnapshot(snapshot,
		map[string]bool{},              // no keys stored at all
		map[int]bool{1: true, 2: true}, // orders 1-2 already downloaded
		map[string]bool{},              // and no stored titles to match
	)
	if len(newChapters) != 1 || newChapters[0].Title != "Chapter 3" {
		t.Fatalf("expected only Chapter 3 to be new, got %+v", newChapters)
	}
}

func TestDiffNovelSnapshotReportsMissingWithoutDeleting(t *testing.T) {
	snapshot := &sourceSnapshot{Chapters: []sourceChapter{
		{Title: "Kept", URL: "https://site/a", Key: "k-a", Order: 1},
	}}
	// "k-gone" is stored but no longer listed upstream. The diff must report it
	// as missing, and the flow must still leave it alone: no flow deletes.
	newChapters, missing := diffNovelSnapshot(snapshot,
		map[string]bool{"k-a": true, "k-gone": true},
		map[int]bool{1: true, 2: true},
		map[string]bool{},
	)
	if len(newChapters) != 0 {
		t.Errorf("expected no new chapters, got %+v", newChapters)
	}
	if len(missing) != 1 || missing[0] != "k-gone" {
		t.Errorf("expected missing=[k-gone], got %v", missing)
	}
}

func TestDiffNovelSnapshotDeduplicatesRepeatedKey(t *testing.T) {
	// A site listing the same chapter twice must not schedule it twice.
	snapshot := &sourceSnapshot{Chapters: []sourceChapter{
		{Title: "Chapter 1", URL: "https://site/a", Key: "k-1", Order: 1},
		{Title: "Chapter 1 (dup)", URL: "https://site/a-dup", Key: "k-1", Order: 2},
	}}
	newChapters, _ := diffNovelSnapshot(snapshot, map[string]bool{}, map[int]bool{}, map[string]bool{})
	if len(newChapters) != 1 {
		t.Fatalf("expected the duplicate key to collapse, got %+v", newChapters)
	}
}

func TestParserErrorMessageTaxonomy(t *testing.T) {
	cases := []struct {
		code     string
		contains string
	}{
		{parserhost.CodeNotMySite, "no installed parser script"},
		{parserhost.CodeSiteLayoutChanged, "--check-parser"},
		{parserhost.CodeBlocked, "browser worker"},
		{parserhost.CodeParserTimeout, "--check-parser"},
		{parserhost.CodeScriptError, "Parser script error"},
	}
	for _, tc := range cases {
		msg := parserErrorMessage(&parserhost.ScriptError{Code: tc.code, Message: "boom"})
		if !strings.Contains(msg, tc.contains) {
			t.Errorf("code %s: got %q, want it to mention %q", tc.code, msg, tc.contains)
		}
	}
	// A network failure is not a ScriptError and must read as a reach problem.
	netMsg := parserErrorMessage(fmt.Errorf("fetching https://x: dial tcp: refused"))
	if !strings.Contains(netMsg, "Could not reach the site") {
		t.Errorf("network error should be reported as a reach failure, got %q", netMsg)
	}
}

// Hot reload: the parser dir is re-read per request, so editing a script takes
// effect on the next call without rebuilding the server.
func TestParserScriptsHotReload(t *testing.T) {
	env := newAPITestEnv(t)
	const url = "https://hotreload.example/novel/1"

	if s := env.server.parserSupportsURL("user-1", url); s {
		t.Fatal("expected no parser to handle the url before any script is installed")
	}

	writeParserScript(t, env, "hot.js", `module.exports = {
	  name: 'hot', apiVersion: 1, requiresBrowser: true,
	  probe: function () { return true; },
	  toc: function () { return { novel: { title: 'T' }, chapters: [] }; },
	  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
	};`)

	if !env.server.parserSupportsURL("user-1", url) {
		t.Fatal("expected the freshly written script to be picked up")
	}
	// requiresBrowser is read from the same registry, so it must follow the edit.
	if !env.server.parserURLRequiresBrowser("user-1", url) {
		t.Fatal("expected requiresBrowser=true from the newly written script")
	}

	// Rewrite the same file to probe false; the next call must see it.
	writeParserScript(t, env, "hot.js", `module.exports = {
	  name: 'hot', apiVersion: 1,
	  probe: function () { return false; },
	  toc: function () { return { novel: { title: 'T' }, chapters: [] }; },
	  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
	};`)
	if env.server.parserSupportsURL("user-1", url) {
		t.Fatal("expected the edited script's probe to take effect immediately")
	}
}

// Check mode must print a usable snapshot and exit 0 against a live page.
func TestRunParserCheckPrintsSnapshot(t *testing.T) {
	page := `<!doctype html><html><body>
<h1>Checked Novel</h1>
<ul class="chapter-list">
  <li><a href="/c/1"><span class="chapter-title">One</span></a></li>
  <li><a href="/c/2"><span class="chapter-title">Two</span></a></li>
</ul>
</body></html>`
	chapter := `<html><body><div class="chapter-content"><p>Body text.</p></div></body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.Path, "/c/") {
			_, _ = fmt.Fprint(w, chapter)
			return
		}
		_, _ = fmt.Fprint(w, page)
	}))
	defer mock.Close()

	cfg := checkConfig(t, mock.URL, `module.exports = {
	  name: 'checked', apiVersion: 1,
	  probe: function (u) { return u.indexOf('127.0.0.1') >= 0; },
	  toc: function (ctx, u) {
	    var doc = ctx.get(u);
	    var nodes = ctx.css(doc, 'ul.chapter-list li a');
	    var chapters = [];
	    for (var i = 0; i < nodes.length; i++) {
	      chapters.push({ title: nodes[i].text, url: nodes[i].href });
	    }
	    return { novel: { title: 'Checked Novel', author: 'A' }, chapters: chapters };
	  },
	  chapter: function (ctx, u) {
	    var doc = ctx.get(u);
	    return { title: 'One', contentHtml: ctx.css1(doc, 'div.chapter-content').html };
	  },
	};`)

	if code := RunParserCheck(cfg); code != 0 {
		t.Fatalf("RunParserCheck = %d, want 0", code)
	}
}

// A script that raises a taxonomy code must fail the check and say why.
func TestRunParserCheckFailsWithTaxonomy(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><body>totally different markup</body></html>`)
	}))
	defer mock.Close()

	cfg := checkConfig(t, mock.URL, `module.exports = {
	  name: 'layout-broken', apiVersion: 1,
	  probe: function () { return true; },
	  toc: function (ctx, u) {
	    var doc = ctx.get(u);
	    if (ctx.css1(doc, 'ul.chapter-list') === null) {
	      ctx.fail('site_layout_changed', 'no chapter list found');
	    }
	    return { novel: { title: 'x' }, chapters: [] };
	  },
	  chapter: function () { return { title: '', contentHtml: '<p>x</p>' }; },
	};`)

	if code := RunParserCheck(cfg); code != 1 {
		t.Fatalf("RunParserCheck = %d, want 1 for a layout-changed script", code)
	}
}

// The snapshot RunParserCheck prints must be valid indented JSON, so it can be
// piped into jq or diffed against a golden file.
func TestRunParserCheckOutputIsIndentedJSON(t *testing.T) {
	var out strings.Builder
	saved := stdout
	stdout = &out
	defer func() { stdout = saved }()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.Path, "/c/") {
			_, _ = fmt.Fprint(w, `<div class="chapter-content"><p>Body.</p></div>`)
			return
		}
		_, _ = fmt.Fprint(w, `<h1>N</h1><ul class="chapter-list"><li><a href="/c/1"><span class="chapter-title">One</span></a></li></ul>`)
	}))
	defer mock.Close()

	cfg := checkConfig(t, mock.URL, `module.exports = {
	  name: 'json', apiVersion: 1,
	  probe: function () { return true; },
	  toc: function (ctx, u) {
	    var doc = ctx.get(u);
	    var a = ctx.css1(doc, 'ul.chapter-list a');
	    return { novel: { title: 'N' }, chapters: [{ title: a.text, url: a.href }] };
	  },
	  chapter: function (ctx, u) {
	    var doc = ctx.get(u);
	    return { title: 'One', contentHtml: ctx.css1(doc, 'div.chapter-content').html };
	  },
	};`)

	if code := RunParserCheck(cfg); code != 0 {
		t.Fatalf("RunParserCheck = %d, want 0", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
		t.Fatalf("check output is not valid JSON: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "\n  ") {
		t.Errorf("expected indented output, got %q", out.String())
	}
	chapters, _ := decoded["chapters"].([]any)
	if len(chapters) != 1 {
		t.Errorf("expected 1 chapter in the snapshot, got %d", len(chapters))
	}
}
