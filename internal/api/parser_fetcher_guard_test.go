package api

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"translator-server/internal/config"
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
