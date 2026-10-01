package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"translator-server/internal/config"
	"translator-server/internal/parserhost"
	"translator-server/internal/store"
)

// Tests for the review fixes: tolerant script loading, per-host throttling,
// manifest fetch coalescing, probe caching, title-fallback backfill, read-only
// previews, chapterKey fallback, scoped refresh, manifest signatures, the SSRF
// guard, and chapter-page title preference.

// testManifestKey is the throwaway ed25519 keypair manifests are signed with
// in tests. Verification reads PARSERS_MANIFEST_PUBKEY first, so pointing it
// at this key keeps the embedded release key out of tests.
var (
	testManifestKeyPub  string
	testManifestKeyPriv ed25519.PrivateKey
)

func init() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("generate test manifest key: %v", err))
	}
	testManifestKeyPub = hex.EncodeToString(pub)
	testManifestKeyPriv = priv
}

// useTestManifestKey points signature verification at the test keypair.
func useTestManifestKey(t *testing.T) {
	t.Helper()
	t.Setenv("PARSERS_MANIFEST_PUBKEY", testManifestKeyPub)
}

// signTestManifest signs a manifest's canonical payload with the test key.
func signTestManifest(t *testing.T, m *parserManifest) {
	t.Helper()
	payload, err := manifestPayloadBytes(m)
	if err != nil {
		t.Fatalf("manifest payload: %v", err)
	}
	m.Signature = hex.EncodeToString(ed25519.Sign(testManifestKeyPriv, payload))
}

// neverFetch is the Fetcher for load-only engine tests: loading must never
// fetch (module init is forbidden from networking).
type neverFetch struct{}

func (neverFetch) Fetch(context.Context, string) (*parserhost.FetchResult, error) {
	return nil, fmt.Errorf("parser scripts must not fetch at load")
}

// An invalid livewireCatalogPattern is still a load error at LoadFile time —
// tolerant directory loading skips the file, but the error itself must keep
// naming the bad field (publish-time validation relies on it).
func TestInvalidLivewirePatternIsALoadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badpattern.js")
	body := `module.exports = {
  name: 'badpattern', apiVersion: 1, requiresBrowser: true,
  livewireCatalogPattern: '[',
  probe: function () { return true; },
  toc: function () { return { novel: { title: 'T' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	engine := parserhost.NewEngine(neverFetch{}, parserhost.Options{})
	if _, err := engine.LoadFile(path); err == nil ||
		!strings.Contains(err.Error(), "livewireCatalogPattern is not a valid regular expression") {
		t.Fatalf("expected a load error naming livewireCatalogPattern, got %v", err)
	}
}

// A broken script is skipped with a warning; the scripts around it keep
// working. One bad file must never take down every site.
func TestLoadParserScriptsSkipsBroken(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "zz-good.js", `module.exports = {
  name: 'zz-good', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('zz-good.example') >= 0; },
  toc: function () { return { novel: { title: 'G' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	writeParserScript(t, env, "zz-broken.js", `module.exports = { this is not javascript (`)

	scripts, err := env.server.loadParserScripts("user-1")
	if err != nil {
		t.Fatalf("loadParserScripts: %v", err)
	}
	for _, s := range scripts {
		if s.name() == "zz-broken" {
			t.Fatal("broken script was loaded")
		}
	}
	entry, err := selectParserScript(scripts, "https://zz-good.example/n/1")
	if err != nil {
		t.Fatalf("good script did not survive the broken neighbor: %v", err)
	}
	if entry.name() != "zz-good" {
		t.Fatalf("selected %q, want zz-good", entry.name())
	}
}

// The throttle is per host: repeats to the same host wait, while a different
// host is never blocked — concurrent downloads from different sites run in
// parallel.
func TestParseThrottleIsPerHost(t *testing.T) {
	th := newParseThrottle(150, 150)
	ctx := context.Background()
	a := "https://site-a.example/ch/1"
	b := "https://other.example/ch/1"

	start := time.Now()
	if err := th.wait(ctx, a); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("first fetch to a host must not wait, waited %v", elapsed)
	}
	start = time.Now()
	if err := th.wait(ctx, a); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("repeat fetch to the same host must wait, waited only %v", elapsed)
	}
	start = time.Now()
	if err := th.wait(ctx, b); err != nil {
		t.Fatalf("other-host wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("first fetch to another host must not wait, waited %v", elapsed)
	}
}

// Concurrent resolves share one in-flight manifest fetch instead of
// stampeding the manifest host.
func TestCurrentManifestCoalescesConcurrentFetches(t *testing.T) {
	useTestManifestKey(t)
	release := make(chan struct{})
	var hits atomic.Int64
	m := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{}}
	signTestManifest(t, m)
	manifestBody, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/parsers/index.json", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		<-release // hold every fetcher until all callers are in flight
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(manifestBody)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	s := newParserUpdateServer(t.TempDir(), ts.URL+"/parsers/index.json", true)
	const callers = 8
	done := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			_, err := s.currentManifest(context.Background())
			done <- err
		}()
	}
	time.Sleep(200 * time.Millisecond) // let every goroutine block on the fetch
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-done; err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("expected 1 manifest fetch for %d concurrent callers, got %d", callers, n)
	}
}

// Signature verification: the published bytes verify, anything else is
// refused and the server runs what is installed.
func TestVerifyParserManifest(t *testing.T) {
	useTestManifestKey(t)
	s := &Server{}
	good := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{
		{Name: "x", File: "x.js", SHA256: strings.Repeat("a", 64)},
	}}
	signTestManifest(t, good)
	if err := s.verifyParserManifest(good); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	tampered := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{
		{Name: "x", File: "x.js", SHA256: strings.Repeat("b", 64)},
	}, Signature: good.Signature}
	if err := s.verifyParserManifest(tampered); err == nil {
		t.Fatal("tampered manifest accepted")
	}

	unsigned := &parserManifest{APIVersion: 1}
	if err := s.verifyParserManifest(unsigned); err == nil {
		t.Fatal("unsigned manifest accepted")
	}

	badSig := &parserManifest{APIVersion: 1, Signature: "zzzz"}
	if err := s.verifyParserManifest(badSig); err == nil {
		t.Fatal("malformed signature accepted")
	}
}

// The SSRF guard refuses private targets and non-http schemes without any
// network access (IP literals never hit the resolver).
func TestValidateSiteFetchURL(t *testing.T) {
	s := &Server{Cfg: &config.Config{ParsersAllowPrivateNets: false}}
	for _, raw := range []string{
		"http://127.0.0.1/x",
		"http://10.1.2.3/x",
		"http://192.168.1.1/x",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/x",
		"http://[fd00::1]/x",
		"https://8.8.8.8/../x", // path games still resolve to a public host: allowed
		"ftp://8.8.8.8/x",
		"file:///etc/passwd",
		"https://",
	} {
		err := s.validateSiteFetchURL(context.Background(), raw)
		if strings.HasPrefix(raw, "https://8.8.8.8") {
			if err != nil {
				t.Errorf("%s: public literal refused: %v", raw, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected refusal, got allow", raw)
		}
	}
	if err := s.validateSiteFetchURL(context.Background(), "https://93.184.216.34/x"); err != nil {
		t.Errorf("public literal refused: %v", err)
	}

	// The dev/test bypass allows private nets again.
	open := &Server{Cfg: &config.Config{ParsersAllowPrivateNets: true}}
	if err := open.validateSiteFetchURL(context.Background(), "http://127.0.0.1/x"); err != nil {
		t.Errorf("bypass enabled but loopback refused: %v", err)
	}
}

// Read-only probe outcomes are cached per host, so the novel list does not
// recompile every script per row; removing a script keeps serving the cached
// outcome until the short TTL expires.
func TestProbeCacheCutsRepeatResolves(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "cacheme.js", `module.exports = {
  name: 'cacheme', apiVersion: 1, requiresBrowser: true,
  probe: function (u) { return String(u).indexOf('cacheme.example') >= 0; },
  toc: function () { return { novel: { title: 'C' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`)
	rawURL := "https://cacheme.example/n/1"
	if !env.server.parserSupportsURL("user-1", rawURL) {
		t.Fatal("supported URL reported unsupported")
	}
	if !env.server.parserURLRequiresBrowser("user-1", rawURL) {
		t.Fatal("requiresBrowser not reported")
	}
	env.server.probeCacheMu.Lock()
	first, cached := env.server.probeCache["cacheme.example"]
	env.server.probeCacheMu.Unlock()
	if !cached || !first.supported {
		t.Fatal("positive probe outcome was not cached per host")
	}

	// Deleting the script changes the directory fingerprint, so the cached
	// positive is re-resolved immediately instead of served stale.
	if err := os.Remove(filepath.Join(env.server.Cfg.ParsersDir, "cacheme.js")); err != nil {
		t.Fatalf("remove script: %v", err)
	}
	if env.server.parserSupportsURL("user-1", rawURL) {
		t.Fatal("removed script still reported supported (stale cache)")
	}
	env.server.probeCacheMu.Lock()
	second, ok := env.server.probeCache["cacheme.example"]
	env.server.probeCacheMu.Unlock()
	if !ok || second.supported {
		t.Fatal("negative probe outcome was not cached after the removal")
	}
	if second.dirFP == first.dirFP {
		t.Fatal("directory fingerprint did not change after removing a script")
	}

	// Negatives are cached too (short TTL): an unknown host resolves once.
	unknown := "https://no-such-parser.example/n/1"
	if env.server.parserSupportsURL("user-1", unknown) {
		t.Fatal("unknown host reported supported")
	}
	env.server.probeCacheMu.Lock()
	entry, ok := env.server.probeCache["no-such-parser.example"]
	env.server.probeCacheMu.Unlock()
	if !ok || entry.supported {
		t.Fatal("negative probe outcome was not cached")
	}
}

// Title-unique pairing migrates novels whose stored orders have gaps the
// snapshot positions don't share; ambiguity still aborts the whole plan.
func TestBackfillTitlePairingFallback(t *testing.T) {
	chapters := []sourceChapter{
		{Title: "Prologue", URL: "https://s/1", Key: "k1", Order: 1},
		{Title: "Growth", URL: "https://s/2", Key: "k2", Order: 2},
		{Title: "The Gate Opens", URL: "https://s/3", Key: "k3", Order: 3},
	}
	t.Run("gapped orders pair by unique title", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "Prologue"},
			{ID: "c2", ChapterOrder: 2, Title: "Growth"},
			// Old parser reported real episode numbers; the snapshot is
			// position-stamped, so order 5 matches no position — but the
			// title is unambiguous.
			{ID: "c3", ChapterOrder: 5, Title: "The Gate Opens"},
		}
		plan, ok := backfillSourceKeyPlan(chapters, stored)
		if !ok {
			t.Fatal("expected the title fallback to pair the gapped novel")
		}
		want := map[string]string{"c1": "k1", "c2": "k2", "c3": "k3"}
		if len(plan) != len(want) {
			t.Fatalf("plan = %v, want %v", plan, want)
		}
		for id, key := range want {
			if plan[id] != key {
				t.Errorf("plan[%s] = %q, want %q", id, plan[id], key)
			}
		}
	})

	t.Run("whitespace and case do not block title pairing", func(t *testing.T) {
		single := []sourceChapter{{Title: "Chapter  5:  Growth", URL: "https://s/5", Key: "k5", Order: 1}}
		stored := []store.ChapterSyncMeta{{ID: "c5", ChapterOrder: 9, Title: "chapter 5: growth"}}
		plan, ok := backfillSourceKeyPlan(single, stored)
		if !ok || plan["c5"] != "k5" {
			t.Errorf("plan = %v ok = %v, want c5 keyed", plan, ok)
		}
	})

	t.Run("ambiguous titles abort", func(t *testing.T) {
		dupes := []sourceChapter{
			{Title: "Growth", URL: "https://s/2", Key: "k2", Order: 1},
			{Title: "Growth", URL: "https://s/5", Key: "k5", Order: 2},
		}
		stored := []store.ChapterSyncMeta{{ID: "c2", ChapterOrder: 7, Title: "Growth"}}
		plan, ok := backfillSourceKeyPlan(dupes, stored)
		if ok || plan != nil {
			t.Errorf("plan = %v ok = %v, want nil/false", plan, ok)
		}
	})
}

// Previews plan the key migration in memory and never persist it; the update
// flow that enqueues downloads is what writes the keys.
func TestCheckPreviewDoesNotPersistBackfill(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.Path, "/chapter-") {
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
			return
		}
		_, _ = fmt.Fprint(w, testNovelfireHTML)
	}))
	t.Cleanup(mock.Close)

	env := newAPITestEnv(t)
	useRewritingClient(env, map[string]string{"novelfire.net": mock.URL})

	alice := registerUser(t, env, "alice-preview-readonly@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	// One legacy chapter (no source_key): the snapshot lists two.
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 1, "Chapter 1: First Steps")

	previewResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	assertStatus(t, previewResp, http.StatusOK)
	var preview struct {
		NewChapters int `json:"newChapters"`
	}
	decodeData(t, previewResp, &preview)
	if preview.NewChapters != 1 {
		t.Fatalf("newChapters = %d, want 1", preview.NewChapters)
	}
	keys, err := env.store.GetExistingChapterKeys(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("existing keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("check-preview persisted %d source keys; previews must be read-only", len(keys))
	}

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	assertStatus(t, updateResp, http.StatusAccepted)
	keys, err = env.store.GetExistingChapterKeys(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("existing keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("update-from-url did not persist the backfilled source keys")
	}
}

// One throwing chapterKey falls back to the chapter URL instead of failing
// the whole snapshot.
func TestChapterKeyFailureFallsBackToURL(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "keyfail.js", `module.exports = {
  name: 'keyfail', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('keyfail.example') >= 0; },
  toc: function () { return { novel: { title: 'K' }, chapters: [
    { title: 'A', url: 'https://keyfail.example/a' },
  ] }; },
  chapter: function () { return { title: 'A', contentHtml: '<p>x</p>' }; },
  chapterKey: function () { throw new Error('no stable id on this site'); },
};`)
	snapshot, err := env.server.fetchSourceSnapshot(context.Background(), "user-1", "https://keyfail.example/n")
	if err != nil {
		t.Fatalf("fetchSourceSnapshot: %v", err)
	}
	if len(snapshot.Chapters) != 1 {
		t.Fatalf("chapters = %+v, want 1", snapshot.Chapters)
	}
	if snapshot.Chapters[0].Key != "https://keyfail.example/a" {
		t.Errorf("key = %q, want the chapter URL fallback", snapshot.Chapters[0].Key)
	}
}

// A runtime script failure updates only the selected script and retries the
// failed call once against the published version — including after an
// outage: the resolve-time manifest fetch fails, execution proceeds with the
// installed copy, the TOC fails, and the retry re-fetches the manifest.
func TestFailedScriptRetriesPublishedVersion(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1 := `module.exports = {
  name: 'retryme', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('retryme.example') >= 0; },
  toc: function (ctx, url) { ctx.fail('site_layout_changed', 'selectors gone'); },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`
	v2 := `module.exports = {
  name: 'retryme', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('retryme.example') >= 0; },
  toc: function () { return { novel: { title: 'Healed' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`
	if err := os.WriteFile(filepath.Join(dir, "retryme.js"), []byte(v1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	m := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{
		{Name: "retryme", File: "retryme.js", SHA256: sha256OfString(v2)},
	}}
	signTestManifest(t, m)
	manifestBody, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var manifestHits atomic.Int64
	var scriptHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/parsers/index.json", func(w http.ResponseWriter, _ *http.Request) {
		// The first fetch fails (outage during resolve); the retry's
		// re-fetch succeeds. This is the only deterministic way to reach
		// the update-and-retry path: a reachable manifest would already
		// have updated the script before execution.
		if manifestHits.Add(1) == 1 {
			http.Error(w, "outage", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(manifestBody)
	})
	mux.HandleFunc("/parsers/retryme.js", func(w http.ResponseWriter, _ *http.Request) {
		scriptHits.Add(1)
		fmt.Fprint(w, v2)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	snapshot, err := s.fetchSourceSnapshot(context.Background(), "u1", "https://retryme.example/n")
	if err != nil {
		t.Fatalf("fetchSourceSnapshot after published fix: %v", err)
	}
	if snapshot.Title != "Healed" {
		t.Errorf("title = %q, want the retried published script's novel", snapshot.Title)
	}
	installed, err := os.ReadFile(filepath.Join(dir, "retryme.js"))
	if err != nil || string(installed) != v2 {
		t.Fatal("installed script was not replaced by the published fix")
	}
	if n := manifestHits.Load(); n != 2 {
		t.Fatalf("expected 2 manifest fetches (failed resolve + retry), got %d", n)
	}
	if n := scriptHits.Load(); n != 1 {
		t.Fatalf("expected exactly 1 script download, got %d", n)
	}
}

// A fresh install bootstraps only the missing scripts on a selection miss —
// installed scripts are never bulk-refreshed to heal an unknown URL.
func TestMissingScriptBootstrapsFromManifest(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	claimed := `module.exports = {
  name: 'bootstrap', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('bootstrap.example') >= 0; },
  toc: function () { return { novel: { title: 'B' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`
	unrelated := `module.exports = {
  name: 'unrelated', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('unrelated.example') >= 0; },
  toc: function () { return { novel: { title: 'U' }, chapters: [] }; },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
};`
	if err := os.WriteFile(filepath.Join(dir, "unrelated.js"), []byte(unrelated), 0o644); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}
	m := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{
		{Name: "bootstrap", File: "bootstrap.js", SHA256: sha256OfString(claimed)},
		{Name: "unrelated", File: "unrelated.js", SHA256: sha256OfString(unrelated + " ")},
	}}
	signTestManifest(t, m)
	manifestJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	// Only the missing file is served: if the resolver tried to refresh the
	// installed (but digest-different) unrelated script, the download 404s.
	var unrelatedHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/parsers/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(manifestJSON)
	})
	mux.HandleFunc("/parsers/bootstrap.js", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, claimed)
	})
	mux.HandleFunc("/parsers/unrelated.js", func(w http.ResponseWriter, _ *http.Request) {
		unrelatedHits.Add(1)
		http.NotFound(w, nil)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	entry, err := s.resolveScriptForExecution(context.Background(), "u1", "https://bootstrap.example/n")
	if err != nil {
		t.Fatalf("bootstrap resolve: %v", err)
	}
	if entry.name() != "bootstrap" {
		t.Fatalf("selected %q, want bootstrap", entry.name())
	}
	if n := unrelatedHits.Load(); n != 0 {
		t.Fatal("an installed script was re-downloaded to heal an unrelated URL")
	}
}

// The stored chapter title comes from the chapter page, not the TOC: TOC
// entries sometimes prefix every chapter with the novel name.
func TestChapterPageTitlePreferred(t *testing.T) {
	env := newAPITestEnv(t)
	writeParserScript(t, env, "pagetitle.js", `module.exports = {
  name: 'pagetitle', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('pagetitle.example') >= 0; },
  toc: function () { return { novel: { title: 'N' }, chapters: [
    { title: 'Novel Name - Chapter 1', url: 'https://pagetitle.example/c1' },
  ] }; },
  chapter: function () { return { title: 'Chapter 1', contentHtml: '<p>Body.</p>' }; },
};`)
	title, markdown, err := env.server.fetchChapterThroughScript(
		context.Background(), "user-1", "https://pagetitle.example/n",
		"https://pagetitle.example/c1", "Novel Name - Chapter 1")
	if err != nil {
		t.Fatalf("fetchChapterThroughScript: %v", err)
	}
	if title != "Chapter 1" {
		t.Errorf("title = %q, want the chapter page title", title)
	}
	if strings.Contains(markdown, "Novel Name") {
		t.Errorf("markdown carries the redundant TOC prefix: %q", markdown)
	}
}

// BackfillSourceKeys reports only rows it actually claimed: a second run
// over filled keys writes nothing and counts nothing.
func TestBackfillSourceKeysCountsClaimedRows(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-backfill-count@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")

	created, err := env.store.UpsertChapterWithoutStats(alice.User.ID, novel.ID, &store.Chapter{
		ChapterOrder: 1, Title: "Alpha", OriginalContent: "x", Status: "pending",
	})
	if err != nil {
		t.Fatalf("upsert chapter: %v", err)
	}
	updated, err := env.store.BackfillSourceKeys(alice.User.ID, novel.ID, map[string]string{created.ID: "k1"})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	updated, err = env.store.BackfillSourceKeys(alice.User.ID, novel.ID, map[string]string{created.ID: "k1"})
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0 (key already filled)", updated)
	}
}

// Truncation for display never splits a multibyte rune.
func TestTruncateForDisplayKeepsRunesValid(t *testing.T) {
	s := strings.Repeat("汉", 500) // 3 bytes each: the 800-byte cut lands mid-rune
	out := truncateForDisplay(s)
	if !utf8.ValidString(out) {
		t.Fatal("truncated snapshot preview is not valid UTF-8")
	}
	if !strings.HasSuffix(out, "… (truncated)") {
		t.Fatal("truncated output lost its marker")
	}
}

// Two TOC entries sharing one identity collapse to a single new chapter
// instead of double-counting.
func TestDiffDropsDuplicateKeys(t *testing.T) {
	snapshot := &sourceSnapshot{Chapters: []sourceChapter{
		{Title: "A", URL: "https://s/a", Key: "same", Order: 1},
		{Title: "A (mirror)", URL: "https://s/a-mirror", Key: "same", Order: 2},
	}}
	got, _ := diffNovelSnapshot(snapshot, map[string]bool{}, map[int]bool{}, map[string]bool{}, false)
	if len(got) != 1 {
		t.Fatalf("new chapters = %d, want 1", len(got))
	}
}

// When a stale script's TOC fails and the published version heals it, the
// snapshot's chapter keys must come from the published version too. Computing
// them with the stale script would persist identities the published script no
// longer produces, so the next sync would report the whole library as new and
// re-download it as permanent duplicates.
func TestRetriedScriptSuppliesChapterKeys(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1 := `module.exports = {
  name: 'keyswap', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('keyswap.example') >= 0; },
  toc: function (ctx, url) { ctx.fail('site_layout_changed', 'selectors gone'); },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
  chapterKey: function (ch) { return 'stale:' + ch.url; },
};`
	v2 := `module.exports = {
  name: 'keyswap', apiVersion: 1,
  probe: function (u) { return String(u).indexOf('keyswap.example') >= 0; },
  toc: function () {
    return { novel: { title: 'Healed' }, chapters: [{ title: 'One', url: 'https://keyswap.example/c1' }] };
  },
  chapter: function () { return { title: 'c', contentHtml: '<p>x</p>' }; },
  chapterKey: function (ch) { return 'stable:' + ch.url; },
};`
	if err := os.WriteFile(filepath.Join(dir, "keyswap.js"), []byte(v1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	m := &parserManifest{APIVersion: 1, Parsers: []parserManifestEntry{
		{Name: "keyswap", File: "keyswap.js", SHA256: sha256OfString(v2)},
	}}
	signTestManifest(t, m)
	manifestBody, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var manifestHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/parsers/index.json", func(w http.ResponseWriter, _ *http.Request) {
		// First fetch fails so execution starts on the stale copy and only the
		// retry picks up the published script.
		if manifestHits.Add(1) == 1 {
			http.Error(w, "outage", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(manifestBody)
	})
	mux.HandleFunc("/parsers/keyswap.js", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, v2)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	snapshot, err := s.fetchSourceSnapshot(context.Background(), "u1", "https://keyswap.example/n")
	if err != nil {
		t.Fatalf("fetchSourceSnapshot after published fix: %v", err)
	}
	if len(snapshot.Chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(snapshot.Chapters))
	}
	if got := snapshot.Chapters[0].Key; got != "stable:https://keyswap.example/c1" {
		t.Fatalf("chapter key came from the stale script: %q", got)
	}
}
