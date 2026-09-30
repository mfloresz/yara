package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"translator-server/internal/parserhost"
)

// parseThrottle spaces consecutive parser fetches per site (URL host). It
// lives on the Server, not on a fetcher: every script reload builds a fresh
// fetcher, so per-fetcher state would reset on each chapter and no wait would
// ever be applied.
//
// The wait is per host, not global: two concurrent downloads from different
// sites never block each other, while chapters from the same site stay spaced
// out. Delays are set once at Server construction (see New) and never mutated
// afterwards, so concurrent fetchers can share the throttle race-free.
type parseThrottle struct {
	mu              sync.Mutex
	lastFetchByHost map[string]time.Time
	minDelay        time.Duration
	maxDelay        time.Duration
}

// newParseThrottle builds the shared throttle from the configured inter-fetch
// bounds. Non-positive bounds fall back to the downloader defaults (5-10s),
// matching the historical behavior: the delay was never disabling.
func newParseThrottle(minDelayMs, maxDelayMs int) *parseThrottle {
	if minDelayMs <= 0 {
		minDelayMs = 5000
	}
	if maxDelayMs <= 0 {
		maxDelayMs = 10000
	}
	minDelay := time.Duration(minDelayMs) * time.Millisecond
	maxDelay := time.Duration(maxDelayMs) * time.Millisecond
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return &parseThrottle{
		lastFetchByHost: make(map[string]time.Time),
		minDelay:        minDelay,
		maxDelay:        maxDelay,
	}
}

// setDelays overrides both bounds. Tests use it to shrink the crawl delay;
// production never calls it after New.
func (t *parseThrottle) setDelays(minDelayMs, maxDelayMs int) {
	minDelay := time.Duration(minDelayMs) * time.Millisecond
	maxDelay := time.Duration(maxDelayMs) * time.Millisecond
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	t.mu.Lock()
	t.minDelay = minDelay
	t.maxDelay = maxDelay
	t.mu.Unlock()
}

// throttleHost extracts the throttle key for a URL: the lowercased hostname.
// An unparseable URL falls back to itself as the key, which still spaces
// repeats of the same bad URL without colliding with any real site.
func throttleHost(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	return rawURL
}

// wait blocks until the configured inter-fetch gap has elapsed since the last
// fetch to the same host. The very first fetch to a host is never delayed: a
// single-page TOC scrape should not pay the inter-chapter penalty.
//
// The timestamp records when the fetch is actually issued (after the wait),
// not when the wait started: stamping the start would let a fetch that just
// waited out the full gap be followed immediately by another one, collapsing
// the spacing the throttle exists to enforce.
func (t *parseThrottle) wait(ctx context.Context, rawURL string) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	minDelay, maxDelay := t.minDelay, t.maxDelay
	host := throttleHost(rawURL)
	prev := t.lastFetchByHost[host]
	t.mu.Unlock()
	if prev.IsZero() {
		t.mu.Lock()
		// First fetch to this host: stamp and go without delay. A lost race
		// between two simultaneous first fetches merely delays one of them.
		if _, exists := t.lastFetchByHost[host]; !exists {
			t.lastFetchByHost[host] = time.Now()
		}
		t.mu.Unlock()
		return nil
	}
	wait := minDelay - time.Since(prev)
	if wait < 0 {
		wait = 0
	}
	if maxDelay > minDelay {
		wait += time.Duration(rand.Int63n(int64(maxDelay - minDelay)))
	}
	if wait <= 0 {
		t.mu.Lock()
		t.lastFetchByHost[host] = time.Now()
		t.mu.Unlock()
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		t.mu.Lock()
		t.lastFetchByHost[host] = time.Now()
		t.mu.Unlock()
		return nil
	}
}

// parserFetcher adapts Yara's HTTP and browser-worker paths to the interface
// the parser engine calls back into. It is the single place where all site
// traffic for a parser script is issued, so throttling, proxy routing and
// charset handling stay in Yara rather than in the scripts.
//
// Routing rule: fetch directly over HTTP; when the script declared
// requiresBrowser, or the direct attempt fails in a way a real browser would
// fix (transport error, 4xx/5xx, or a Cloudflare challenge page), relay through
// the user's connected browser worker if one is attached. The relay already
// exists as callable backend logic (EnqueueBrowserJob), so no new WebSocket
// client is introduced here.
type parserFetcher struct {
	server          *Server
	userID          string
	requiresBrowser bool
	// livewirePattern is the compiled livewireCatalogPattern the script
	// declared. On the worker path, a matching URL is fetched through the
	// worker's Livewire operation (the only way some sites expose their chapter
	// catalog to the DOM the extension returns); nil means no routing.
	livewirePattern *regexp.Regexp

	// client is the direct HTTP path. Tests swap it for a transport that
	// rewrites site hosts to an httptest server.
	client *http.Client
	// throttle is shared across every fetcher in the process.
	throttle *parseThrottle
}

func newParserFetcher(s *Server, userID string, requiresBrowser bool, livewirePattern *regexp.Regexp) *parserFetcher {
	f := &parserFetcher{
		server:          s,
		userID:          userID,
		requiresBrowser: requiresBrowser,
		livewirePattern: livewirePattern,
		client:          s.parserHTTPClient(userID),
	}
	if s != nil {
		f.throttle = s.parserThrottle
	}
	if f.throttle == nil {
		f.throttle = newParseThrottle(0, 0)
	}
	return f
}

// sleepBetweenFetches exposes the throttle for the trailing cooldown the
// download job runs before releasing its origin keys, and for batch flows that
// issue one TOC per novel. rawURL keys the wait to the site's host.
func (f *parserFetcher) sleepBetweenFetches(ctx context.Context, rawURL string) error {
	return f.throttle.wait(ctx, rawURL)
}

// Fetch implements parserhost.Fetcher. Errors returned here are network
// failures and deliberately stay plain Go errors: the engine passes them
// through untouched so a broken site is distinguishable from a broken script.
func (f *parserFetcher) Fetch(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	if err := f.throttle.wait(ctx, rawURL); err != nil {
		return nil, err
	}
	useWorker := f.requiresBrowser && f.hasWorker()
	if !useWorker {
		result, err := f.fetchDirect(ctx, rawURL)
		if err == nil && !looksBlocked(result) {
			return result, nil
		}
		// Either the transport failed or we got a challenge page. A real
		// browser usually gets past both, so retry through the worker when
		// one is connected before giving up.
		if !f.hasWorker() {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("fetching %s: %s", rawURL, cloudflareHint)
		}
		slog.Info("direct parser fetch failed, retrying via browser worker", "url", rawURL, "status", statusOf(result, err), "error", err)
	}
	return f.fetchViaWorker(ctx, rawURL)
}

func (f *parserFetcher) hasWorker() bool {
	return f.server != nil && f.server.HasBrowserWorkerForUser(f.userID)
}

func (f *parserFetcher) fetchDirect(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	// Script-requested URLs are untrusted: refuse private targets before any
	// socket is opened (SSRF guard). The worker path is exempt — those
	// requests run in the user's own browser.
	if err := f.server.validateSiteFetchURL(ctx, rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	// Same UA/Accept pair the Go parsers used, so sites that answered the old
	// client keep answering this one.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	applySiteHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	// Several Chinese sites still serve GBK-family bytes. Without this they
	// reach the script as U+FFFD replacement characters, which the parser
	// cannot recover from.
	body = decodeChineseCharset(body, resp.Header.Get("Content-Type"))
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return &parserhost.FetchResult{FinalURL: finalURL, Status: resp.StatusCode, Body: body}, nil
}

// fetchViaWorker relays the request through the browser worker extension,
// reusing the same queue-guarded path the old ProxyHTTPClient used.
func (f *parserFetcher) fetchViaWorker(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	operation := "fetch_page"
	if f.livewirePattern != nil && f.livewirePattern.MatchString(rawURL) {
		operation = "fetch_livewire"
	}
	result, err := f.server.enqueueBrowserJob(operation, rawURL, nil, f.userID)
	if err != nil && operation != "fetch_page" {
		// The Livewire request can fail transiently (or an older extension may
		// not support the operation); fall back to the plain page fetch so the
		// invocation still gets the DOM it would have had before this routing
		// existed.
		slog.Info("livewire worker fetch failed, falling back to fetch_page", "url", rawURL, "error", err)
		result, err = f.server.enqueueBrowserJob("fetch_page", rawURL, nil, f.userID)
	}
	if err != nil {
		return nil, fmt.Errorf("browser worker fetch of %s: %w", rawURL, err)
	}
	html := getStringFromData(result.Data, "html")
	if html == "" {
		return nil, fmt.Errorf("browser worker returned empty body for %s", rawURL)
	}
	// The extension does not report a status or the post-redirect URL, so both
	// are synthesized: the worker is only used for pages a direct client could
	// not read at all, and a 200 keeps scripts from treating it as an error.
	return &parserhost.FetchResult{FinalURL: rawURL, Status: http.StatusOK, Body: []byte(html)}, nil
}

const cloudflareHint = "the site returned a challenge page (Cloudflare?) and no browser worker is connected — connect the extension and retry"

// looksBlocked reports whether a successful HTTP response is actually a
// challenge page. These answers come back 200/403, so status alone is not
// enough to tell a blocked fetch from a real one.
func looksBlocked(res *parserhost.FetchResult) bool {
	if res == nil {
		return false
	}
	if res.Status == http.StatusTooManyRequests || res.Status == http.StatusForbidden {
		return true
	}
	if res.Status < 400 {
		body := res.Body
		if len(body) > 4096 {
			body = body[:4096]
		}
		lower := strings.ToLower(string(body))
		return strings.Contains(lower, "just a moment") ||
			strings.Contains(lower, "checking your browser") ||
			strings.Contains(lower, "cf-browser-verification") ||
			strings.Contains(lower, "cf_chl_opt")
	}
	return false
}

func statusOf(res *parserhost.FetchResult, err error) int {
	if res != nil {
		return res.Status
	}
	if err != nil {
		return 0
	}
	return 0
}
