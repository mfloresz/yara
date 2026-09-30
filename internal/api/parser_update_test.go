package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"translator-server/internal/config"
	"translator-server/internal/parserhost"
)

// The parser auto-update suite runs against a hand-built Server: no PocketBase,
// no store, and an httptest server standing in for the release manifest. The
// scripts under testdata/parserupdates differ only in what their probe claims,
// which is observable without touching the network.

const updateTestScriptFile = "update-test.js"

func readUpdateTestScript(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "parserupdates", name))
	if err != nil {
		t.Fatalf("reading testdata parser script %s: %v", name, err)
	}
	return string(data)
}

func sha256OfString(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// buildManifest assembles a release manifest whose entries carry the correct
// digest of each given file body, signed with the test key (production
// manifests are signed — see verifyParserManifest — and unsigned ones are
// refused, so tests that need an unsigned manifest build the JSON by hand).
func buildManifest(t *testing.T, apiVersion int, files map[string]string) string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entries = append(entries, map[string]any{
			"name":            "update-test",
			"file":            name,
			"sha256":          sha256OfString(files[name]),
			"requiresBrowser": false,
		})
	}
	data, err := json.Marshal(map[string]any{"apiVersion": apiVersion, "parsers": entries})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var m parserManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	signTestManifest(t, &m)
	signed, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal signed manifest: %v", err)
	}
	return string(signed)
}

// serveParserManifest hosts the manifest and its script files. Every hit is
// counted so tests can assert how many network round trips an execution made.
func serveParserManifest(t *testing.T, manifest string, files map[string]string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/parsers/index.json", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, manifest)
	})
	for name, body := range files {
		mux.HandleFunc("/parsers/"+name, func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			fmt.Fprint(w, body)
		})
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &hits
}

func newParserUpdateServer(parsersDir, manifestURL string, autoUpdate bool) *Server {
	return &Server{Cfg: &config.Config{
		ParsersDir:         parsersDir,
		ParsersManifestURL: manifestURL,
		ParsersAutoUpdate:  autoUpdate,
	}}
}

func writeInstalledScript(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, updateTestScriptFile), []byte(body), 0o644); err != nil {
		t.Fatalf("writing installed script: %v", err)
	}
}

func readInstalledScript(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, updateTestScriptFile))
	if err != nil {
		t.Fatalf("reading installed script: %v", err)
	}
	return string(data)
}

func assertNotMySite(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected not_my_site, got success")
	}
	var scriptErr *parserhost.ScriptError
	if !errors.As(err, &scriptErr) || scriptErr.Code != parserhost.CodeNotMySite {
		t.Fatalf("expected not_my_site, got %v", err)
	}
}

func TestParserAutoUpdateReplacesStaleScriptBeforeExecution(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	// The installed v1 claims /book/ but not /magazine/. Resolving the novel
	// URL selects it, notices the digest difference and swaps v2 in first.
	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("resolve after update: %v", err)
	}
	if got := readInstalledScript(t, dir); got != v2 {
		t.Fatal("installed script was not replaced with the manifest version")
	}
	// The returned selection is the updated script: it also claims /magazine/.
	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/magazine/"); err != nil {
		t.Fatalf("updated script not used for the re-resolve: %v", err)
	}
	if n := hits.Load(); n != 2 { // manifest fetch + one script download
		t.Fatalf("expected 2 hits (manifest + script), got %d", n)
	}
}

func TestParserAutoUpdateSkipsDownloadWhenDigestMatches(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1 := readUpdateTestScript(t, "v1.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v1}),
		map[string]string{updateTestScriptFile: v1})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("up-to-date script was rewritten")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("expected only the manifest fetch, got %d hits", n)
	}
}

func TestParserAutoUpdateLeavesUnrelatedScriptsAloneOnNotMySite(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	// Nothing claims /magazine/: parsers are per host, so the miss installs
	// only manifest scripts missing locally (none here — update-test.js is
	// installed) and reports not_my_site instead of bulk-refreshing
	// unrelated sites. The installed script is untouched.
	_, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/magazine/")
	assertNotMySite(t, err)
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("an unrelated script was rewritten on a selection miss")
	}
	if n := hits.Load(); n != 1 { // only the manifest fetch for the missing-files check
		t.Fatalf("expected 1 hit (manifest only), got %d", n)
	}
}

func TestParserAutoUpdateInstallsMissingParserFromManifest(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v2 := readUpdateTestScript(t, "v2.js")
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	// No script is installed at all: the not_my_site selection installs the
	// published parser and the retry claims the URL without any other step.
	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("fresh install did not auto-install the published parser: %v", err)
	}
	if got := readInstalledScript(t, dir); got != v2 {
		t.Fatal("manifest parser was not installed into the empty parsers directory")
	}
	if n := hits.Load(); n != 2 { // manifest + one script download
		t.Fatalf("expected 2 hits (manifest + script), got %d", n)
	}
}

func TestParserAutoUpdateDisabledDoesNotFetchOrWrite(t *testing.T) {
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", false)

	_, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/magazine/")
	assertNotMySite(t, err)
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("script changed while auto-update was disabled")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("expected no requests, got %d", n)
	}
}

func TestParserAutoUpdateKeepsInstalledScriptWhenManifestUnreachable(t *testing.T) {
	dir := t.TempDir()
	v1 := readUpdateTestScript(t, "v1.js")
	writeInstalledScript(t, dir, v1)
	ts := httptest.NewServer(http.NewServeMux())
	manifestURL := ts.URL + "/parsers/index.json"
	ts.Close() // nothing is listening anymore

	s := newParserUpdateServer(dir, manifestURL, true)
	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("execution must proceed with the installed script: %v", err)
	}
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("script changed despite the unreachable manifest")
	}
}

func TestParserAutoUpdateRejectsSelectedScriptNotMatchingManifestDigest(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	// The manifest lies about the digest: the download must be rejected even
	// though its content loads fine.
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: "not the published content"}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	// The installed v1 claims /book/, so the digest lie is caught on the
	// selected-script update path: the download is rejected and execution
	// proceeds with v1, on every attempt.
	for i := 0; i < 2; i++ {
		if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
			t.Fatalf("resolve %d: execution must proceed with the installed script: %v", i, err)
		}
	}
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("installed script was replaced by a digest-mismatched download")
	}
	if n := hits.Load(); n != 3 { // manifest (cached after the first) + one rejected download per resolve
		t.Fatalf("expected 3 hits (manifest + 2 rejected downloads), got %d", n)
	}
}

func TestParserAutoUpdateRejectsUnsafeManifestEntry(t *testing.T) {
	dir := t.TempDir()
	v1 := readUpdateTestScript(t, "v1.js")
	writeInstalledScript(t, dir, v1)
	manifest := `{"apiVersion":1,"parsers":[{"name":"evil","file":"../evil.js","sha256":"` +
		sha256OfString(v1) + `","requiresBrowser":false}]}`
	// Hand-built and unsigned: rejected before any entry is downloaded or
	// written (bad entry — and it would also fail the signature step), so
	// nothing is downloaded or written.
	ts, hits := serveParserManifest(t, manifest, nil)
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("resolve with rejected manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.js")); !os.IsNotExist(err) {
		t.Fatal("a manifest entry escaped the parsers directory")
	}
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("script changed despite the rejected manifest")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("expected only the manifest fetch, got %d hits", n)
	}
}

func TestParserAutoUpdateSkipsManifestWithUnsupportedAPIVersion(t *testing.T) {
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion+1, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	// A future manifest is not trusted: no updates run, the installed script
	// keeps claiming only /book/, and the failure is cached (single fetch).
	_, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/magazine/")
	assertNotMySite(t, err)
	if _, err := s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/"); err != nil {
		t.Fatalf("resolve with future manifest: %v", err)
	}
	if got := readInstalledScript(t, dir); got != v1 {
		t.Fatal("script replaced from a manifest this binary cannot trust")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("expected only the manifest fetch (failure cached), got %d hits", n)
	}
}

func TestParserManifestCacheIsUsedThenInvalidated(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1 := readUpdateTestScript(t, "v1.js")
	writeInstalledScript(t, dir, v1)
	ts, hits := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v1}),
		map[string]string{updateTestScriptFile: v1})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	if _, err := s.currentManifest(context.Background()); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := s.currentManifest(context.Background()); err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("expected 1 fetch before invalidation, got %d", n)
	}
	s.invalidateParserUpdates()
	if _, err := s.currentManifest(context.Background()); err != nil {
		t.Fatalf("refetch after invalidation: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("expected 2 fetches after invalidation, got %d", n)
	}
}

func TestParserAutoUpdateConcurrentRefreshIsSafe(t *testing.T) {
	useTestManifestKey(t)
	dir := t.TempDir()
	v1, v2 := readUpdateTestScript(t, "v1.js"), readUpdateTestScript(t, "v2.js")
	writeInstalledScript(t, dir, v1)
	ts, _ := serveParserManifest(t,
		buildManifest(t, parserhost.APIVersion, map[string]string{updateTestScriptFile: v2}),
		map[string]string{updateTestScriptFile: v2})
	s := newParserUpdateServer(dir, ts.URL+"/parsers/index.json", true)

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, errs[slot] = s.resolveScriptForExecution(context.Background(), "u1", "https://site.test/book/")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent resolve %d: %v", i, err)
		}
	}
	if got := readInstalledScript(t, dir); got != v2 {
		t.Fatal("concurrent refreshes left the installed script stale")
	}
}
