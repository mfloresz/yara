package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"translator-server/internal/config"
)

// checkConfig writes a parser script into a temp dir and returns a config
// pointing -check-parser/-check-url at it, for RunParserCheck tests.
func checkConfig(t *testing.T, checkURL, script string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "check.js")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write check script: %v", err)
	}
	return &config.Config{CheckParser: path, CheckURL: checkURL}
}

// testParsersDir copies testdata/parsers into a per-test temporary directory and
// returns its path. Parsers are loaded from disk on every request with no cache,
// so each test gets its own copy: tests can edit a script to exercise hot reload
// without interfering with each other.
func testParsersDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "parsers")
	dst := filepath.Join(t.TempDir(), "parsers")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("create parsers dir: %v", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read testdata parsers: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatalf("read parser script %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), body, 0o644); err != nil {
			t.Fatalf("write parser script %s: %v", entry.Name(), err)
		}
	}
	return dst
}

// useRewritingClient points the server's parser HTTP client at a transport that
// rewrites the listed hosts onto mock servers. It replaces the old
// DownloaderFactory override, which no longer exists now that all site traffic
// goes through the parser engine. Rewritten hosts land on 127.0.0.1 mocks, so
// the SSRF guard (which refuses private IPs) is bypassed for the test server.
func useRewritingClient(env *apiTestEnv, rewrites map[string]string) {
	env.server.ParserHTTPClientFactory = func(string) *http.Client {
		return &http.Client{Transport: &hostRewritingTransport{rewrites: rewrites}}
	}
	if env.server.Cfg != nil {
		env.server.Cfg.ParsersAllowPrivateNets = true
	}
}

// setParserThrottle overrides the inter-fetch delay the parser fetcher applies.
// Tests that need a slow crawl (e.g. to cancel a job mid-download) set this
// instead of the old per-downloader delay fields. It updates both the config
// (source of truth at boot) and the server's shared throttle.
func setParserThrottle(env *apiTestEnv, minDelayMs, maxDelayMs int) {
	if env.server.Cfg == nil {
		return
	}
	env.server.Cfg.DownloadMinDelayMs = minDelayMs
	env.server.Cfg.DownloadMaxDelayMs = maxDelayMs
	if env.server.parserThrottle != nil {
		env.server.parserThrottle.setDelays(minDelayMs, maxDelayMs)
	}
}

// writeParserScript adds or replaces a script in the test's parsers dir. The
// directory is re-read on the next request, so this exercises hot reload.
func writeParserScript(t *testing.T, env *apiTestEnv, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(env.server.Cfg.ParsersDir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write parser script %s: %v", name, err)
	}
}
