package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"translator-server/internal/config"
	"translator-server/internal/parserhost"
)

// The redirect policy must re-run the SSRF guard on every hop: a site that
// answers the validated first request with a redirect to loopback must never
// be followed.
func TestSiteRedirectPolicyRevalidatesEachHop(t *testing.T) {
	guarded := &Server{Cfg: &config.Config{ParsersAllowPrivateNets: false}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9/secret", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	err = guarded.siteRedirectPolicy()(req, nil)
	if err == nil {
		t.Fatal("redirect to loopback accepted by the redirect policy")
	}
	if !isSiteFetchRefused(err) {
		t.Fatalf("refusal not typed as siteFetchRefusedError: %v", err)
	}

	open := &Server{Cfg: &config.Config{ParsersAllowPrivateNets: true}}
	if err := open.siteRedirectPolicy()(req, nil); err != nil {
		t.Fatalf("bypass enabled but redirect refused: %v", err)
	}
}

// A URL the SSRF guard refused must fail the fetch outright — never be
// retried through the user's browser worker, whose machine has more internal
// reach than the server.
func TestFetchRefusedURLIsNotRelayedToWorker(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "ssrfme.js", `module.exports = {
  name: 'ssrfme', apiVersion: 1, requiresBrowser: false,
  probe: function (u) { return String(u).indexOf('127.0.0.1') >= 0; },
  toc: function (ctx, url) {
    var doc = ctx.get(url);
    return { novel: { title: 'X' }, chapters: [] };
  },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	fakeWorker(t, "user-1")

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/private": "<html><body>leaked</body></html>",
	}, nil)

	_, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "http://127.0.0.1:9/private")
	if err == nil {
		t.Fatal("SSRF-guarded URL fetched without error")
	}
	if !isSiteFetchRefused(err) {
		t.Fatalf("refusal not surfaced as siteFetchRefusedError: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("refused URL was relayed to the browser worker: %v", ops)
	}
}

// Concurrent waiters for the same host must serialize: each reserves its slot
// under the lock, so the actual fetches stay spaced by the minimum delay
// instead of all firing at once.
func TestParseThrottleSerializesConcurrentWaiters(t *testing.T) {
	const minDelay = 150 * time.Millisecond
	tr := newParseThrottle(int(minDelay.Milliseconds()), int((minDelay + 50*time.Millisecond).Milliseconds()))

	var wg sync.WaitGroup
	var mu sync.Mutex
	var done []time.Time
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tr.wait(context.Background(), "https://concurrent.example/x"); err != nil {
				t.Errorf("wait: %v", err)
				return
			}
			mu.Lock()
			done = append(done, time.Now())
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(done) != 3 {
		t.Fatalf("expected 3 completed waits, got %d", len(done))
	}
	sort.Slice(done, func(i, j int) bool { return done[i].Before(done[j]) })
	// Reservations are spaced minDelay apart; allow generous scheduling slack
	// but nothing close to a collapse (which would leave a ~0 gap).
	for i := 0; i < 2; i++ {
		if gap := done[i+1].Sub(done[i]); gap < minDelay/2 {
			t.Fatalf("concurrent waiters collapsed: gap %d = %v (want >= %v)", i, gap, minDelay/2)
		}
	}
}

// The first fetch to a host is never delayed, even when another host is
// already being waited on.
func TestParseThrottleFirstFetchNotDelayed(t *testing.T) {
	tr := newParseThrottle(150, 200)
	start := time.Now()
	if err := tr.wait(context.Background(), "https://first.example/x"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("first fetch to a host was delayed by %v", elapsed)
	}
}

// Fetches do not wait, but they must still stamp the host's last-fetch time.
// A fetch that went out without stamping leaves the host looking idle, and the
// next waiter's gap would be computed from a zero timestamp — which is how a
// same-site job queue lost its cooldown.
func TestFetchStampsHostWithoutWaiting(t *testing.T) {
	env := newAPITestEnv(t)
	useRewritingClient(env, map[string]string{"novelfire.net": "http://127.0.0.1:1"})
	setParserThrottle(env, 5000, 5000)

	f := newParserFetcher(env.server, "user-1", false, nil)

	// Two fetches in a row: neither may sleep, which is what makes a check
	// fast now that the throttle no longer lives inside Fetch.
	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := f.Fetch(context.Background(), "https://novelfire.net/book/x"); err == nil {
			t.Fatal("expected the unreachable mock to fail the fetch")
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("consecutive fetches to one host waited %v; Fetch must not throttle", elapsed)
	}

	// Both requests are on the record, so a later wait measures its gap from
	// the second fetch instead of seeing an untouched host.
	tr := env.server.parserThrottle
	tr.mu.Lock()
	stamp := tr.lastFetchByHost["novelfire.net"]
	tr.mu.Unlock()
	if stamp.IsZero() {
		t.Fatal("fetch did not stamp the host's last-fetch time")
	}

	start = time.Now()
	if err := tr.wait(context.Background(), "https://novelfire.net/book/y"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 4*time.Second {
		t.Fatalf("wait after a real fetch returned in %v, want at least the 5s gap", elapsed)
	}
}

// A fetch stamp must never move a host's recorded time backwards, or a
// concurrent waiter's reserved slot could be shortened by a late finish.
func TestMarkFetchedNeverMovesTimeBackwards(t *testing.T) {
	tr := newParseThrottle(1000, 1000)
	future := time.Now().Add(time.Hour)

	tr.mu.Lock()
	tr.lastFetchByHost["later.example"] = future
	tr.mu.Unlock()

	tr.markFetched("https://later.example/x")

	tr.mu.Lock()
	got := tr.lastFetchByHost["later.example"]
	tr.mu.Unlock()
	if !got.Equal(future) {
		t.Fatalf("markFetched moved a reserved slot backwards: got %v, want %v", got, future)
	}
}

// The invocation ceiling is not evidence of a stale parser, so a timeout must
// not trigger the auto-update retry: that would spend a manifest refetch, a
// script download and a second full TOC run to reach the same error.
func TestParserTimeoutSkipsAutoUpdateRetry(t *testing.T) {
	env := newAPITestEnv(t)
	env.server.Cfg.ParsersAutoUpdate = true
	env.server.Cfg.ParsersManifestURL = "https://slowtoc-manifest.example/index.json"

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Slow</h1></body></html>"))
	}))
	t.Cleanup(mock.Close)
	useRewritingClient(env, map[string]string{"slowtoc.example": mock.URL})

	writeParserScript(t, env, "slowtoc.js", `module.exports = {
  name: 'slowtoc', apiVersion: 1, requiresBrowser: false,
  probe: function (u) { return String(u).indexOf('slowtoc.example') >= 0; },
  toc: function (ctx, url) {
    var doc = ctx.get(url);
    // Exceed the fetch budget: the binding aborts through the VM interrupt
    // with CodeParserTimeout, the same code the wall clock produces.
    for (var i = 0; i < 400; i++) { ctx.get(url); }
    return { novel: { title: 'X' }, chapters: [] };
  },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)

	// The manifest advertises a different digest for this very script, so the
	// update path would swap it in if it ran. fetchedAt is stamped so the entry
	// counts as live (a zero fetchedAt reads as long expired).
	env.server.parserUpdates.mu.Lock()
	env.server.parserUpdates.manifest = &parserManifest{
		APIVersion: 1,
		Parsers: []parserManifestEntry{
			{Name: "slowtoc", File: "slowtoc.js", SHA256: strings.Repeat("ab", 32)},
		},
	}
	env.server.parserUpdates.fetchedAt = time.Now()
	env.server.parserUpdates.mu.Unlock()

	_, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "https://slowtoc.example/book/x")
	if err == nil {
		t.Fatal("expected the fetch-budget overrun to fail the TOC")
	}
	var scriptErr *parserhost.ScriptError
	if !errors.As(err, &scriptErr) || scriptErr.Code != parserhost.CodeParserTimeout {
		t.Fatalf("expected parser_timeout, got %v", err)
	}
	env.server.parserUpdates.mu.Lock()
	cached := env.server.parserUpdates.manifest
	env.server.parserUpdates.mu.Unlock()
	if cached == nil || len(cached.Parsers) != 1 {
		t.Fatal("manifest cache was invalidated by a parser_timeout")
	}
	// The script file is untouched: a retry would have replaced it.
	onDisk, readErr := os.ReadFile(filepath.Join(env.server.Cfg.ParsersDir, "slowtoc.js"))
	if readErr != nil {
		t.Fatalf("read installed script: %v", readErr)
	}
	if !strings.Contains(string(onDisk), "for (var i = 0; i < 400") {
		t.Fatal("parser_timeout triggered a parser update")
	}
}

// A parser script must not be able to aim the user's browser at its own LAN.
// The literal checks run on the worker path too, so a page-supplied href to a
// private address is refused before it reaches the worker queue.
func TestWorkerPathRefusesLiteralPrivateTargets(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "ssrfworker.js", `module.exports = {
  name: 'ssrfworker', apiVersion: 1, requiresBrowser: true,
  probe: function (u) { return String(u).indexOf('official.example') >= 0; },
  toc: function (ctx, url) {
    // A chapter href on the page points at the user's own network.
    var doc = ctx.get('http://192.168.7.7/secret');
    return { novel: { title: 'X' }, chapters: [] };
  },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	fakeWorker(t, "user-1")

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/secret": "<html><body>lan</body></html>",
	}, nil)

	_, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "https://official.example/book")
	if err == nil {
		t.Fatal("private target on the worker path was fetched without error")
	}
	if len(ops) != 0 {
		t.Fatalf("private target was relayed to the browser worker: %v", ops)
	}
}

// The literal checks must not require DNS: a site only resolvable inside the
// user's own network still has to be relayable through their worker.
func TestWorkerPathAllowsHostTheServerCannotResolve(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "lwroute2.js", livewireRouteScript)
	fakeWorker(t, "user-1")

	var ops []string
	env.server.BrowserJobEnqueuer = recordingEnqueuer(&ops, map[string]string{
		"/projects/foo": "<html><body><h1>R</h1><ul class=\"chapter-list\"><li><a href=\"/projects/foo/c1\">One</a></li></ul></body></html>",
	}, nil)

	// lwroute.example does not resolve in the test environment, which is the
	// point: the worker path must not consult the server's resolver.
	if _, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", livewireRouteURL); err != nil {
		t.Fatalf("worker path refused an unresolvable public host: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("expected the fetch to be relayed to the worker")
	}
}

// An HTTP error status is not content: a 404 must fail the fetch as a network
// error instead of being handed to the script's selectors (which would report
// site_layout_changed for a page that simply no longer exists).
func TestDirectFetchRejects404AsNetworkFailure(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "gone.js", `module.exports = {
  name: 'gone', apiVersion: 1, requiresBrowser: false,
  probe: function (u) { return String(u).indexOf('gone.example') >= 0; },
  toc: function (ctx, url) {
    var doc = ctx.get(url);
    ctx.fail('site_layout_changed', 'no chapter content found');
  },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	env.server.ParserHTTPClientFactory = alwaysStatusClient(404)
	// The host does not resolve in this environment, so the SSRF guard would
	// stop the fetch before the mock transport ever sees it. This test is about
	// the status handling, not the guard.
	env.server.Cfg.ParsersAllowPrivateNets = true

	_, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "https://gone.example/book")
	if err == nil {
		t.Fatal("404 was accepted as a normal page")
	}
	var scriptErr *parserhost.ScriptError
	if errors.As(err, &scriptErr) {
		t.Fatalf("404 surfaced as a script error (%s) instead of a network failure: %v", scriptErr.Code, err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error does not mention the status: %v", err)
	}
}

// The direct fetch must not buffer an unbounded body. The engine's own limit is
// checked after the response is already resident, so the host has to stop
// reading at the cap: a site that streams for the full client timeout would
// otherwise be OOM-material for a single-binary server. This asserts on bytes
// pulled off the wire, which is what the engine-side check cannot protect.
func TestDirectFetchCapsBodySize(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "flood.js", `module.exports = {
  name: 'flood', apiVersion: 1, requiresBrowser: false,
  probe: function (u) { return String(u).indexOf('flood.example') >= 0; },
  toc: function (ctx, url) { ctx.get(url); return { novel: { title: 't' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	env.server.Cfg.ParsersAllowPrivateNets = true
	// Twice the cap is available, so an uncapped reader would happily pull all
	// of it. The counter records how much actually reached the host.
	source := &countingReader{remaining: maxDirectBodyBytes * 2}
	env.server.ParserHTTPClientFactory = func(string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(source),
				Request:    req,
			}, nil
		})}
	}

	_, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "https://flood.example/book")
	if err == nil {
		t.Fatal("oversized body accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error does not report the size limit: %v", err)
	}
	if served := source.served; served > maxDirectBodyBytes+64*1024 {
		t.Errorf("host buffered %d bytes, want it to stop near the %d cap", served, maxDirectBodyBytes)
	}
}

// countingReader yields zero bytes up to a limit and records how many were
// actually read, so a test can prove the reader stopped rather than the source.
type countingReader struct {
	remaining int64
	served    int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	r.remaining -= int64(n)
	r.served += int64(n)
	return n, nil
}

// A 403/406/429/503 is a blocked or overloaded answer, not chapter content. It
// must be treated the same way by both the status gate and the worker-retry
// decision, so it is neither parsed as a page nor dropped without the browser
// fallback. A 500 has no browser remedy and fails as a network error.
func TestBlockedStatusesAgreeBetweenGateAndWorkerRetry(t *testing.T) {
	for _, status := range []int{403, 406, 429, 503} {
		res := &parserhost.FetchResult{Status: status, Body: []byte("<html>blocked</html>")}
		if !looksBlocked(res) {
			t.Errorf("status %d not treated as blocked, so it would be parsed as content", status)
		}
		if !retryableThroughWorker(res, nil) {
			t.Errorf("status %d is not retried through the worker", status)
		}
	}

	for _, status := range []int{404, 410, 500, 502} {
		res := &parserhost.FetchResult{Status: status, Body: []byte("<html>error</html>")}
		if looksBlocked(res) {
			t.Errorf("status %d treated as blocked; it is a definitive answer", status)
		}
		if retryableThroughWorker(res, nil) {
			t.Errorf("status %d retried through the worker; a browser cannot fix it", status)
		}
	}
}

// A transport failure (no response at all) must still reach the browser, which
// is the whole point of the worker fallback.
func TestTransportFailureStillRetriesThroughWorker(t *testing.T) {
	if !retryableThroughWorker(nil, errors.New("connection reset")) {
		t.Error("transport failure did not request a worker retry")
	}
}
