package api

import (
	"context"
	"errors"
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

// parseThrottle spaces consecutive chapter downloads per site (URL host). It
// lives on the Server, not on a fetcher: every script reload builds a fresh
// fetcher, so per-fetcher state would reset on each chapter and no wait would
// ever be applied. It is deliberately NOT applied inside parserFetcher.Fetch —
// see the comment there — so the callers that do pace themselves (the download
// job's chapter loop, batch-check's per-novel loop) are the only ones waiting.
//
// The wait is per host, not global: two concurrent downloads from different
// sites never block each other, while chapters from the same site stay spaced
// out. Delays are set once at Server construction (see New) and never mutated
// afterwards, so concurrent callers can share the throttle race-free.
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

// wait blocks until the configured inter-fetch gap has elapsed since the slot
// this URL's host last reserved. Concurrent callers for the same host
// serialize: each reserves its slot (last fetch + minDelay, plus jitter)
// under the lock before sleeping, so N simultaneous fetches wake spaced
// minDelay..maxDelay apart instead of all waking at the same moment. The
// very first fetch to a host is never delayed: a single-page TOC scrape
// should not pay the inter-chapter penalty.
//
// The slot is reserved before sleeping rather than stamped after waking: a
// waiter that reads a shared timestamp and sleeps independently would fire
// its fetch at the same moment as every other waiter that read the same
// value. A caller whose context is canceled mid-wait still burns its slot,
// which can only make the next fetch wait longer, never shorter.
func (t *parseThrottle) wait(ctx context.Context, rawURL string) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	minDelay, maxDelay := t.minDelay, t.maxDelay
	host := throttleHost(rawURL)
	prev := t.lastFetchByHost[host]
	target := time.Now()
	if !prev.IsZero() {
		if earliest := prev.Add(minDelay); earliest.After(target) {
			target = earliest
		}
		if maxDelay > minDelay {
			target = target.Add(time.Duration(rand.Int63n(int64(maxDelay - minDelay))))
		}
	}
	t.lastFetchByHost[host] = target
	t.mu.Unlock()
	delay := time.Until(target)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// markFetched records that a request to this host actually went out. Reserving
// a slot is not the same as issuing a request: without this stamp a caller that
// fetches without waiting (every parser fetch, since the throttle no longer
// lives inside Fetch) leaves the host looking idle, and the next waiter's gap
// is computed from a stale or zero timestamp. The stamp never moves the
// recorded time backwards, so it cannot shorten a gap a concurrent waiter
// already reserved.
func (t *parseThrottle) markFetched(rawURL string) {
	if t == nil {
		return
	}
	host := throttleHost(rawURL)
	now := time.Now()
	t.mu.Lock()
	if t.lastFetchByHost[host].Before(now) {
		t.lastFetchByHost[host] = now
	}
	t.mu.Unlock()
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

// siteFetchRefusedError marks a URL the SSRF guard refused on purpose. It is
// distinguishable from a transport failure so callers never retry a refused
// URL from anywhere else — least of all through the user's browser worker,
// whose machine typically has more internal reach than the server.
type siteFetchRefusedError struct{ err error }

func (e *siteFetchRefusedError) Error() string { return e.err.Error() }
func (e *siteFetchRefusedError) Unwrap() error { return e.err }

func isSiteFetchRefused(err error) bool {
	var refused *siteFetchRefusedError
	return errors.As(err, &refused)
}

// Fetch implements parserhost.Fetcher. Errors returned here are network
// failures and deliberately stay plain Go errors: the engine passes them
// Fetch performs one script-requested HTTP fetch.
//
// Errors returned here are network failures and deliberately stay plain Go
// errors: the engine passes them through untouched so a broken site is
// distinguishable from a broken script. The one exception is a
// siteFetchRefusedError from the SSRF guard, which is a deliberate refusal and
// is never retried via the browser worker.
//
// It deliberately does NOT wait between fetches. A script's toc() is two or
// three catalog pages plus their metadata; spacing those by the download
// throttle turned every check into a 15-20s wall and made the engine's
// toc timeout unreachably low for novels whose catalog paginates (the walk
// needs one fetch per page). The throttle belongs where the aggressive
// request pattern actually is — between chapter downloads — so it is applied
// by the download job, not here. Every fetch still stamps the host's last-fetch
// time so a later wait computes its gap from a real request, not a zero value.
func (f *parserFetcher) Fetch(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	// Every script-requested URL is checked before anything is fetched. The
	// direct path additionally resolves the host; the worker path gets the
	// literal checks only, because the server's DNS says nothing about what the
	// user's machine can reach and a site only resolvable in the user's network
	// must still be relayable. A refusal here is terminal — it is never
	// re-routed through the worker.
	useWorker := f.requiresBrowser && f.hasWorker()
	if err := f.validateURL(ctx, rawURL, useWorker); err != nil {
		return nil, err
	}
	if !useWorker {
		result, err := f.fetchDirect(ctx, rawURL)
		// The request is on the wire either way (it failed after being sent),
		// so stamp it before deciding whether a worker retry follows: that
		// second attempt is another request to the same host.
		f.throttle.markFetched(rawURL)
		if err == nil && !looksBlocked(result) {
			if herr := applySiteHelpers(ctx, result.FinalURL, result, f); herr != nil {
				return nil, herr
			}
			return result, nil
		}
		// The direct attempt failed. Retry through the worker only when a real
		// browser could plausibly fix it — a transport error, a challenge page,
		// or a blocked/transient status. A definitive 404/410 is the site saying
		// the page is gone: relaying it would burn a browser slot and hand the
		// error page to the script as if it were content.
		if !f.hasWorker() || !retryableThroughWorker(result, err) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("fetching %s: %s", rawURL, cloudflareHint)
		}
		slog.Info("direct parser fetch failed, retrying via browser worker", "url", rawURL, "status", statusOf(result, err), "error", err)
	}
	result, err := f.fetchViaWorker(ctx, rawURL)
	f.throttle.markFetched(rawURL)
	if err != nil {
		return nil, err
	}
	if herr := applySiteHelpers(ctx, result.FinalURL, result, f); herr != nil {
		return nil, herr
	}
	return result, nil
}

// validateURL runs the literal SSRF checks on every path, and the DNS-resolving
// guard when the fetch will actually leave from this machine. It types the
// refusal so callers can tell a deliberate one from a transport failure. A nil
// server (tests that build a bare fetcher) skips the guard.
func (f *parserFetcher) validateURL(ctx context.Context, rawURL string, viaWorker bool) error {
	if f.server == nil {
		return nil
	}
	if viaWorker {
		if err := f.server.validateSiteFetchTarget(rawURL); err != nil {
			return &siteFetchRefusedError{err}
		}
		return nil
	}
	if err := f.server.validateSiteFetchURL(ctx, rawURL); err != nil {
		return &siteFetchRefusedError{err}
	}
	return nil
}

func (f *parserFetcher) hasWorker() bool {
	return f.server != nil && f.server.HasBrowserWorkerForUser(f.userID)
}

// maxDirectBodyBytes caps what the host will buffer from a direct site fetch.
// The engine's MaxBodyBytes is checked by the script binding, but only after
// the whole response is already in memory — too late to protect a single-binary
// server from a site that streams for the full client timeout. This is the
// limit that applies on the wire; it matches the binding's default so the two
// never disagree about what a script can see.
const maxDirectBodyBytes = int64(5 << 20)

func (f *parserFetcher) fetchDirect(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	// The SSRF guard already ran in Fetch, covering this path and the worker's.
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
	// Read one byte past the cap so an over-limit body is detectable without
	// buffering the whole stream.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDirectBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	if int64(len(body)) > maxDirectBodyBytes {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", rawURL, maxDirectBodyBytes)
	}
	// Several Chinese sites still serve GBK-family bytes. Without this they
	// reach the script as U+FFFD replacement characters, which the parser
	// cannot recover from.
	body = decodeChineseCharset(body, resp.Header.Get("Content-Type"))
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	result := &parserhost.FetchResult{FinalURL: finalURL, Status: resp.StatusCode, Body: body}
	// An error page is not a page. Handing it to the script makes its selectors
	// fail and reports site_layout_changed — "your parser is broken" for a
	// chapter that was deleted — or, worse, stores the error page as chapter
	// content. The blocked statuses are exempt: looksBlocked routes those
	// through the browser worker, which is the actual fix for them.
	if resp.StatusCode >= 400 && !blockedRetryStatus(resp.StatusCode) {
		return result, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}
	return result, nil
}

// retryableThroughWorker decides whether a failed direct attempt should be
// relayed to the browser worker. No response at all is a transport failure a
// browser can fix; a response is only worth relaying when the site blocked the
// request rather than answered it.
func retryableThroughWorker(result *parserhost.FetchResult, err error) bool {
	if result == nil {
		return err != nil
	}
	return looksBlocked(result)
}

// blockedRetryStatus reports whether a direct-fetch status is one a real
// browser can change: bot protection and transient overload. These match the
// set the deleted Go scraper treated as retryable. Anything else (404, 410,
// 500, 502) is the site's final answer and is not worth a browser slot.
func blockedRetryStatus(status int) bool {
	switch status {
	case http.StatusForbidden, http.StatusNotAcceptable, http.StatusTooManyRequests,
		http.StatusServiceUnavailable:
		return true
	}
	return false
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
// challenge page or a blocked/transient answer. These come back 403/406/429/503
// or as a 200 challenge body, so status alone is not enough to tell a blocked
// fetch from a real one.
func looksBlocked(res *parserhost.FetchResult) bool {
	if res == nil {
		return false
	}
	if blockedRetryStatus(res.Status) {
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
