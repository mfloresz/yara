package api

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// testdata URL lives on a host no bundled testdata parser probes, so the
// freshly written script below is the only one claiming it.
const livewireRouteURL = "https://lwroute.example/projects/foo"

const livewireRouteScript = `module.exports = {
  name: 'lwroute', apiVersion: 1, requiresBrowser: true,
  livewireCatalogPattern: '^https?://(?:www\\.)?lwroute\\.example/projects/[^/?#]+/?$',
  probe: function (u) { return String(u).indexOf('lwroute.example') >= 0; },
  toc: function (ctx, url) {
    var doc = ctx.get(url);
    var h1 = ctx.css1(doc, 'h1');
    var a = ctx.css1(doc, 'ul.chapter-list a');
    var chapters = [];
    if (a) chapters.push({ title: a.text, url: ctx.resolveUrl(url, a.attr('href')) });
    return { novel: { title: h1 === null ? '' : h1.text }, chapters: chapters };
  },
  chapter: function (ctx, u) {
    var doc = ctx.get(u);
    return { title: 'c', contentHtml: ctx.css1(doc, 'div.chapter-content').html };
  },
};`

// fakeWorker registers an authenticated worker for userID in the global
// registry so the parser fetcher takes its browser-worker path, and returns a
// cleanup that removes it.
func fakeWorker(t *testing.T, userID string) {
	t.Helper()
	browserWorkersMu.Lock()
	browserWorkers["test-worker"] = &BrowserWorker{ID: "test-worker", State: "authenticated", UserID: userID}
	browserWorkersMu.Unlock()
	t.Cleanup(func() {
		browserWorkersMu.Lock()
		delete(browserWorkers, "test-worker")
		browserWorkersMu.Unlock()
	})
}

// recordingEnqueuer stubs the browser worker queue, recording every requested
// operation and answering fetches with the given page bodies (matched by
// substring of the URL). results[i] maps to operations[i].
func recordingEnqueuer(ops *[]string, pages map[string]string, failOps map[string]bool) func(string, string, map[string]interface{}, string) (*BrowserWorkerJobResult, error) {
	return func(operation, url string, _ map[string]interface{}, _ string) (*BrowserWorkerJobResult, error) {
		*ops = append(*ops, operation)
		if failOps[operation] {
			return nil, fmt.Errorf("stub %s failure", operation)
		}
		for suffix, body := range pages {
			if strings.HasSuffix(url, suffix) {
				return &BrowserWorkerJobResult{Status: "ok", Data: map[string]interface{}{"html": body}}, nil
			}
		}
		return nil, fmt.Errorf("stub has no page for %s", url)
	}
}

// A requiresBrowser script declaring livewireCatalogPattern gets its catalog
// page fetched through the worker's Livewire operation, and the resulting
// payload parses into a normal TOC snapshot.
func TestParserFetcherRoutesLivewireCatalog(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "lwroute.js", livewireRouteScript)
	fakeWorker(t, "user-1")

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/projects/foo": "<html><body><h1>Routed Novel</h1><ul class=\"chapter-list\"><li><a href=\"/projects/foo/c1\">One</a></li></ul></body></html>",
	}, nil)

	snapshot, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", livewireRouteURL)
	if err != nil {
		t.Fatalf("fetchSourceSnapshot: %v", err)
	}
	if snapshot.Title != "Routed Novel" {
		t.Errorf("unexpected title %q", snapshot.Title)
	}
	if len(snapshot.Chapters) != 1 || snapshot.Chapters[0].URL != "https://lwroute.example/projects/foo/c1" {
		t.Errorf("unexpected chapters %+v", snapshot.Chapters)
	}
	if len(ops) != 1 || ops[0] != "fetch_livewire" {
		t.Fatalf("expected exactly one fetch_livewire operation, got %v", ops)
	}
}

// When the Livewire request fails the fetcher falls back to the plain page
// fetch instead of failing the whole invocation.
func TestParserFetcherFallsBackToFetchPage(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "lwroute.js", livewireRouteScript)
	fakeWorker(t, "user-1")

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/projects/foo": "<html><body><h1>Fallback Novel</h1><ul class=\"chapter-list\"><li><a href=\"/projects/foo/c1\">One</a></li></ul></body></html>",
	}, map[string]bool{"fetch_livewire": true})

	snapshot, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", livewireRouteURL)
	if err != nil {
		t.Fatalf("fetchSourceSnapshot: %v", err)
	}
	if snapshot.Title != "Fallback Novel" {
		t.Errorf("unexpected title %q", snapshot.Title)
	}
	if len(ops) != 2 || ops[0] != "fetch_livewire" || ops[1] != "fetch_page" {
		t.Fatalf("expected [fetch_livewire fetch_page], got %v", ops)
	}
}

// Chapter URLs have a second path segment, so they must keep using the plain
// page fetch even when the script declares a catalog pattern.
func TestParserFetcherChapterURLKeepsFetchPage(t *testing.T) {
	env := newAPITestEnv(t)
	fakeWorker(t, "user-1")

	pattern := regexp.MustCompile(`^https?://(?:www\.)?lwroute\.example/projects/[^/?#]+/?$`)
	f := newParserFetcher(env.server, "user-1", true, pattern)

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/projects/foo/c1": "<html><body>chapter page</body></html>",
	}, nil)

	res, err := f.Fetch(context.Background(), "https://lwroute.example/projects/foo/c1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(res.Body) != "<html><body>chapter page</body></html>" {
		t.Errorf("unexpected body %q", res.Body)
	}
	if len(ops) != 1 || ops[0] != "fetch_page" {
		t.Fatalf("expected exactly one fetch_page operation, got %v", ops)
	}
}

// A pattern that does not compile is a load error for that script — and since
// loading is tolerant, the broken script is skipped while the rest keep
// working (publish-time validation in tools/gen-parser-manifest is the gate
// that keeps it out of the release).
func TestParserScriptRejectsInvalidLivewirePattern(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "badpattern.js", `module.exports = {
  name: 'badpattern', apiVersion: 1, requiresBrowser: true,
  livewireCatalogPattern: '[',
  probe: function (u) { return String(u).indexOf('badpattern.example') >= 0; },
  toc: function () { return { novel: { title: 'T' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)

	// The broken script never loads, so its URL selects nothing...
	_, err := env.server.resolveScriptForURL("user-1", "https://badpattern.example/novel/1")
	assertNotMySite(t, err)

	// ...while a healthy script in the same directory still resolves.
	writeParserScript(t, env, "stillfine.js", `module.exports = {
  name: 'stillfine', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('stillfine.example') >= 0; },
  toc: function () { return { novel: { title: 'T' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	if _, err := env.server.resolveScriptForURL("user-1", "https://stillfine.example/novel/1"); err != nil {
		t.Fatalf("healthy script did not survive the broken neighbor: %v", err)
	}
}
