package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"translator-server/internal/parserhost"
	"translator-server/internal/store"
)

// sourceChapter is one entry of a parser TOC snapshot, tagged with the
// script's stable identity for that chapter. It replaces the Go parser's
// ChapterURL as the currency of the source-synchronization diff.
type sourceChapter struct {
	Title string
	URL   string
	// Key is script.ChapterKey(ref) — the chapter URL unless the script
	// exports chapterKey. It is persisted as chapters.source_key.
	Key string
	// Order is the chapter's position in the TOC (1-based). parserhost's
	// ChapterRef carries no order field — the contract is that the chapters
	// array is already in reading order — so the snapshot builder stamps the
	// array position here. That keeps the site listing the single source of
	// truth for chapter order, which the title-derived fallback alone could not
	// guarantee (titles routinely disagree with the site's own numbering).
	Order int
}

// sourceSnapshot is the full picture of what a site currently has, as returned
// by a single script.TOC call. Callers diff it against the store.
type sourceSnapshot struct {
	Title       string
	Author      string
	Description string
	CoverURL    string
	Language    string
	Tags        []string
	Chapters    []sourceChapter
}

// parserScript pairs a compiled script with the file it came from, so the same
// source can be reloaded against an engine whose fetcher is routed for it.
type parserScript struct {
	file   string
	script *parserhost.Script
}

func (p parserScript) name() string { return p.script.Name() }

// parserHTTPClient returns the direct HTTP client used for parser fetches.
// The factory indirection lets tests point site hosts at an httptest server,
// mirroring the old DownloaderFactory override.
func (s *Server) parserHTTPClient(userID string) *http.Client {
	if s.ParserHTTPClientFactory != nil {
		return s.ParserHTTPClientFactory(userID)
	}
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: s.siteRedirectPolicy()}
}

// siteRedirectPolicy is the CheckRedirect policy for direct site clients:
// every hop re-runs the SSRF guard, so a site-controlled redirect cannot hop
// to loopback or private targets after the initial URL was validated. The
// refusal is typed so the caller never retries the chain via the browser
// worker.
func (s *Server) siteRedirectPolicy() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := s.validateSiteFetchURL(req.Context(), req.URL.String()); err != nil {
			return &siteFetchRefusedError{fmt.Errorf("redirect to %s refused: %w", req.URL, err)}
		}
		return nil
	}
}

// newParserEngine builds an engine whose fetcher honours the script's
// requiresBrowser flag and routes its livewireCatalogPattern matches through
// the worker's Livewire operation.
func (s *Server) newParserEngine(userID string, requiresBrowser bool, livewirePattern *regexp.Regexp) *parserhost.Engine {
	return parserhost.NewEngine(newParserFetcher(s, userID, requiresBrowser, livewirePattern), parserhost.Options{})
}

func (s *Server) parsersDir() string {
	if s.Cfg != nil && s.Cfg.ParsersDir != "" {
		return s.Cfg.ParsersDir
	}
	return ""
}

// sleepBetweenSourceFetches applies the server's inter-fetch throttle between
// consecutive source requests. It exists for batch flows that issue one TOC per
// novel outside a single job's fetcher. rawURL keys the wait to the site's
// host, so novels from different sites never block each other.
func (s *Server) sleepBetweenSourceFetches(ctx context.Context, rawURL string) error {
	f := newParserFetcher(s, "", false, nil)
	return f.sleepBetweenFetches(ctx, rawURL)
}

// fetchCoverBlob downloads a cover image, falling back to the browser worker
// when a direct fetch fails (covers are often behind the same protection as
// the chapter pages). Content type is sniffed because the image CDN may not
// send one.
func (s *Server) fetchCoverBlob(ctx context.Context, userID, coverURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating cover request: %w", err)
	}
	// Cover URLs come from site data, so they go through the same SSRF guard
	// as script fetches. A refused URL never reaches the browser worker
	// either: a deliberate refusal is not a transport failure to route
	// around.
	if guardErr := s.validateSiteFetchURL(ctx, coverURL); guardErr != nil {
		err = &siteFetchRefusedError{guardErr}
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "image/*,*/*;q=0.8")

		var resp *http.Response
		resp, err = s.parserHTTPClient(userID).Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				err = fmt.Errorf("cover fetch returned status %d", resp.StatusCode)
			} else {
				blob, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					return nil, "", fmt.Errorf("reading cover: %w", readErr)
				}
				return blob, coverContentType(blob, resp.Header.Get("Content-Type")), nil
			}
		}
	}
	if s.HasBrowserWorkerForUser(userID) && !isSiteFetchRefused(err) {
		slog.Info("direct cover download failed, retrying via browser worker", "cover", coverURL, "error", err)
		return s.FetchImageViaWorker(ctx, coverURL, userID, 60)
	}
	return nil, "", err
}

// coverContentType prefers the server-declared type and falls back to the
// magic bytes the old Go downloader matched (JPEG, PNG, WebP, GIF, BMP).
func coverContentType(blob []byte, declared string) string {
	if declared != "" && strings.HasPrefix(declared, "image/") {
		return declared
	}
	switch {
	case len(blob) > 3 && blob[0] == 0xFF && blob[1] == 0xD8 && blob[2] == 0xFF:
		return "image/jpeg"
	case len(blob) > 8 && string(blob[0:4]) == "\x89PNG":
		return "image/png"
	case len(blob) > 12 && string(blob[0:4]) == "RIFF" && string(blob[8:12]) == "WEBP":
		return "image/webp"
	case len(blob) > 3 && string(blob[0:3]) == "GIF":
		return "image/gif"
	case len(blob) > 2 && blob[0] == 'B' && blob[1] == 'M':
		return "image/bmp"
	}
	if declared != "" {
		return declared
	}
	return "application/octet-stream"
}

// loadParserScripts compiles every *.js in the configured parser dir. It is
// deliberately uncached: reading per job is what makes editing a script take
// effect on the next download without a restart.
//
// A broken script never aborts the load: it is skipped with a warning so one
// bad file cannot take down every other site. Publishing-time validation (see
// tools/gen-parser-manifest) is the gate that keeps broken scripts out of the
// release manifest, not the runtime loader.
func (s *Server) loadParserScripts(userID string) ([]parserScript, error) {
	dir := s.parsersDir()
	if dir == "" {
		return nil, fmt.Errorf("no parsers directory configured")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading parser dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	engine := s.newParserEngine(userID, false, nil)
	loaded := make([]parserScript, 0, len(files))
	for _, name := range files {
		path := filepath.Join(dir, name)
		script, err := engine.LoadFile(path)
		if err != nil {
			slog.Warn("skipping parser script that failed to load; other scripts still active", "file", path, "error", err)
			continue
		}
		loaded = append(loaded, parserScript{file: path, script: script})
	}
	return loaded, nil
}

// selectParserScript runs each script's probe in order and returns the first
// that claims the URL. No match is a taxonomy not_my_site failure so callers
// can word it for users.
func selectParserScript(scripts []parserScript, rawURL string) (parserScript, error) {
	for _, entry := range scripts {
		ok, err := entry.script.Probe(rawURL)
		if err != nil {
			// A broken probe must not abort selection: another script may
			// still handle the URL. Log and keep looking.
			slog.Warn("parser probe failed, skipping script", "parser", entry.name(), "url", rawURL, "error", err)
			continue
		}
		if ok {
			return entry, nil
		}
	}
	return parserScript{}, &parserhost.ScriptError{
		Code:    parserhost.CodeNotMySite,
		Message: fmt.Sprintf("no parser script handles %s", rawURL),
	}
}

// resolveScriptForURL is the per-request entry point: load, probe, select.
func (s *Server) resolveScriptForURL(userID, rawURL string) (parserScript, error) {
	scripts, err := s.loadParserScripts(userID)
	if err != nil {
		return parserScript{}, err
	}
	if len(scripts) == 0 {
		return parserScript{}, &parserhost.ScriptError{
			Code:    parserhost.CodeNotMySite,
			Message: fmt.Sprintf("no parser scripts found in %s", s.parsersDir()),
		}
	}
	return selectParserScript(scripts, rawURL)
}

// routedScript reloads entry against an engine whose fetcher is configured for
// that script, so requiresBrowser is honoured for the whole TOC/chapter call
// rather than only for probe (which never fetches), and the script's catalog
// pattern routes its TOC page through the worker's Livewire operation.
func (s *Server) routedScript(userID string, entry parserScript) (*parserhost.Script, error) {
	return s.newParserEngine(userID, entry.script.RequiresBrowser(), entry.script.LivewireCatalogPattern()).LoadFile(entry.file)
}

// fetchSourceSnapshot resolves the script for rawURL and asks it for the full
// TOC, returning the canonical snapshot the sync diff runs against. Resolution
// goes through the auto-update path: a stale installed script is replaced with
// the manifest's version before it runs.
func (s *Server) fetchSourceSnapshot(ctx context.Context, userID, rawURL string) (*sourceSnapshot, error) {
	entry, err := s.resolveScriptForExecution(ctx, userID, rawURL)
	if err != nil {
		return nil, err
	}
	script, err := s.routedScript(userID, entry)
	if err != nil {
		return nil, err
	}
	toc, err := script.TOC(ctx, rawURL)
	if err != nil {
		var scriptErr *parserhost.ScriptError
		if errors.As(err, &scriptErr) {
			// A repeatable script failure is the signature of a stale
			// parser: drop the manifest cache so the update check below
			// re-fetches immediately (even after an outage), then try the
			// published version once before giving up. Only this script
			// is updated — never the other sites'.
			s.invalidateParserUpdates()
			if fresh, retried := s.updatedScriptForRetry(ctx, userID, entry, rawURL); retried {
				if retryTOC, retryErr := fresh.TOC(ctx, rawURL); retryErr == nil {
					toc, err = retryTOC, nil
				} else {
					// The fresh script fails too; its error is the one to
					// report. The manifest was just refreshed, so the cache
					// stays valid (no second invalidation).
					err = retryErr
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("parser %s: %w", entry.name(), err)
		}
	}
	snapshot := &sourceSnapshot{
		Title:       toc.Novel.Title,
		Author:      toc.Novel.Author,
		Description: toc.Novel.Description,
		CoverURL:    toc.Novel.CoverURL,
		Language:    toc.Novel.Language,
		Tags:        toc.Novel.Tags,
		Chapters:    make([]sourceChapter, 0, len(toc.Chapters)),
	}
	for i, ref := range toc.Chapters {
		key, keyErr := script.ChapterKey(ref)
		if keyErr != nil {
			// A single chapter's key function must not invalidate the whole
			// snapshot: fall back to the chapter URL (the default identity)
			// and keep going. TOC validation above guarantees a non-empty
			// URL, so the fallback is always available.
			slog.Warn("parser chapterKey failed, falling back to chapter URL", "parser", entry.name(), "url", ref.URL, "error", keyErr)
			key = ref.URL
		}
		if key == "" {
			key = ref.URL
		}
		snapshot.Chapters = append(snapshot.Chapters, sourceChapter{
			Title: ref.Title, URL: ref.URL, Key: key, Order: i + 1,
		})
	}
	return snapshot, nil
}

// fetchChapterThroughScript resolves the script for the novel's source URL and
// returns one chapter's title and markdown, routed for requiresBrowser.
// Resolution goes through the auto-update path, like every other parser
// execution.
//
// The title comes from the chapter page first: TOC entries sometimes carry
// redundant prefixes (e.g. the novel name before every chapter title), while
// the page title is what the site shows for the chapter itself. The TOC title
// is only the fallback when the page reports none.
func (s *Server) fetchChapterThroughScript(ctx context.Context, userID, novelURL, chapterURL, chapterTitle string) (title, markdown string, err error) {
	entry, err := s.resolveScriptForExecution(ctx, userID, novelURL)
	if err != nil {
		return "", "", err
	}
	script, err := s.routedScript(userID, entry)
	if err != nil {
		return "", "", err
	}
	chapter, err := script.Chapter(ctx, chapterURL)
	if err != nil {
		var scriptErr *parserhost.ScriptError
		if errors.As(err, &scriptErr) {
			s.invalidateParserUpdates()
			if fresh, retried := s.updatedScriptForRetry(ctx, userID, entry, novelURL); retried {
				if retryChapter, retryErr := fresh.Chapter(ctx, chapterURL); retryErr == nil {
					chapter, err = retryChapter, nil
				} else {
					err = retryErr
				}
			}
		}
		if err != nil {
			return "", "", fmt.Errorf("parser %s: %w", entry.name(), err)
		}
	}
	title = chapter.Title
	if title == "" {
		title = chapterTitle
	}
	markdown, err = htmlToChapterMarkdown(chapter.ContentHTML, title)
	if err != nil {
		return "", "", err
	}
	return title, markdown, nil
}

// parserSupportsURL mirrors the old IsSupportedURL: can any script in the
// registry handle this URL? Backs the canUpdate response field.
//
// Probe selection is cached per URL host (see probeCache): the novel list
// calls this once per row, and recompiling every script per row would make
// the list endpoint pay N×P goja VMs. Execution paths always resolve fresh
// so script edits still take effect immediately.
func (s *Server) parserSupportsURL(userID, rawURL string) bool {
	if strings.TrimSpace(rawURL) == "" {
		return false
	}
	if supported, _, ok := s.cachedProbe(rawURL); ok {
		return supported
	}
	entry, err := s.resolveScriptForURL(userID, rawURL)
	if err != nil {
		s.storeProbeOnNotMySite(rawURL, err, false, "", false)
		return false
	}
	s.storeProbe(rawURL, true, entry.file, entry.script.RequiresBrowser())
	return true
}

// parserURLRequiresBrowser mirrors the old RequiresBrowser. Backs the
// requiresBrowser response field. Cached like parserSupportsURL.
func (s *Server) parserURLRequiresBrowser(userID, rawURL string) bool {
	if strings.TrimSpace(rawURL) == "" {
		return false
	}
	if supported, requiresBrowser, ok := s.cachedProbe(rawURL); ok {
		return supported && requiresBrowser
	}
	entry, err := s.resolveScriptForURL(userID, rawURL)
	if err != nil {
		s.storeProbeOnNotMySite(rawURL, err, false, "", false)
		return false
	}
	requiresBrowser := entry.script != nil && entry.script.RequiresBrowser()
	s.storeProbe(rawURL, true, entry.file, requiresBrowser)
	return requiresBrowser
}

// Probe-result cache for the read-only lookups (canUpdate, requiresBrowser).
//
// Why this exists: the novel list resolves canUpdate/requiresBrowser once per
// row. Without a cache, each row recompiles every parser script and runs every
// probe — N novels × P scripts goja VMs per list request. Probes are pure
// string matching over the URL (they never fetch), so caching the outcome per
// URL host is safe; positives live 5 minutes, negatives (unsupported hosts)
// only 1 minute so a newly added script is picked up quickly. Execution paths
// (TOC/chapter/download) always resolve fresh and are unaffected.
//
// Hot reload still works: every cached entry records a fingerprint of the
// parsers directory (file names, sizes, modtimes), and a hit whose
// fingerprint no longer matches is re-resolved fresh. Steady state costs one
// directory read per row instead of P script compilations; any script
// add/remove/edit takes effect on the very next lookup.

const (
	probeCachePositiveTTL = 5 * time.Minute
	probeCacheNegativeTTL = time.Minute
)

// probeCacheEntry is one cached host outcome. file identifies the winning
// script for observability; supported=false marks an unsupported host. dirFP
// is the parsersDirFingerprint at store time.
type probeCacheEntry struct {
	supported       bool
	file            string
	requiresBrowser bool
	expires         time.Time
	dirFP           string
}

// probeCacheHost extracts the cache key for a URL: the lowercased hostname of
// an http(s) URL. Anything else is not cached — the caller resolves fresh.
func probeCacheHost(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

// parsersDirFingerprint summarizes the parsers directory for cache
// invalidation: sorted file name, size and modtime of every *.js. Any script
// add, removal or edit changes it; a failed read yields "" which never
// matches a stored entry and forces a fresh resolve (safe side).
func (s *Server) parsersDirFingerprint() string {
	dir := s.parsersDir()
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return ""
		}
		b.WriteString(entry.Name())
		b.WriteByte(0)
		b.WriteString(info.ModTime().UTC().Format(time.RFC3339Nano))
		b.WriteByte(0)
		_, _ = fmt.Fprintf(&b, "%d", info.Size())
		b.WriteByte(0)
	}
	return b.String()
}

// cachedProbe returns the cached outcome for rawURL's host, if fresh and the
// parsers directory has not changed since it was stored.
func (s *Server) cachedProbe(rawURL string) (supported, requiresBrowser bool, ok bool) {
	host, cacheable := probeCacheHost(rawURL)
	if !cacheable {
		return false, false, false
	}
	s.probeCacheMu.Lock()
	defer s.probeCacheMu.Unlock()
	entry, found := s.probeCache[host]
	if !found || time.Now().After(entry.expires) {
		return false, false, false
	}
	if fp := s.parsersDirFingerprint(); fp == "" || fp != entry.dirFP {
		return false, false, false
	}
	return entry.supported, entry.requiresBrowser, true
}

// storeProbe records a fresh outcome. Negatives use the short TTL.
func (s *Server) storeProbe(rawURL string, supported bool, file string, requiresBrowser bool) {
	host, cacheable := probeCacheHost(rawURL)
	if !cacheable {
		return
	}
	ttl := probeCachePositiveTTL
	if !supported {
		ttl = probeCacheNegativeTTL
	}
	s.probeCacheMu.Lock()
	if s.probeCache == nil {
		s.probeCache = make(map[string]probeCacheEntry)
	}
	s.probeCache[host] = probeCacheEntry{
		supported:       supported,
		file:            file,
		requiresBrowser: requiresBrowser,
		expires:         time.Now().Add(ttl),
		dirFP:           s.parsersDirFingerprint(),
	}
	s.probeCacheMu.Unlock()
}

// storeProbeOnNotMySite caches a resolution failure only when it is a genuine
// "no script claims this host" outcome. Load errors (unreadable directory)
// are transient and must not be cached.
func (s *Server) storeProbeOnNotMySite(rawURL string, err error, supported bool, file string, requiresBrowser bool) {
	var scriptErr *parserhost.ScriptError
	if errors.As(err, &scriptErr) && scriptErr.Code == parserhost.CodeNotMySite {
		s.storeProbe(rawURL, supported, file, requiresBrowser)
	}
}

// parserErrorMessage turns a parser or network failure into a user-actionable
// string suitable for a job's errorMessage. Taxonomy codes are the contract;
// network failures arrive as plain errors and are reported as such.
func parserErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var scriptErr *parserhost.ScriptError
	if errors.As(err, &scriptErr) {
		switch scriptErr.Code {
		case parserhost.CodeNotMySite:
			return "Unsupported URL: no installed parser script handles this site. Add one to the parsers directory (see docs/parsers.md)."
		case parserhost.CodeSiteLayoutChanged:
			return "The site layout changed and the parser no longer matches it (" + scriptErr.Message +
				"). Update the script and verify it with --check-parser <script.js> -check-url <url>."
		case parserhost.CodeBlocked:
			return "The site blocked the request (" + scriptErr.Message +
				"). Connect the browser worker extension and retry if the site is behind Cloudflare."
		case parserhost.CodeParserTimeout:
			return "The parser took too long or used too many requests (" + scriptErr.Message +
				"). Check the script and verify it with --check-parser <script.js> -check-url <url>."
		default:
			return "Parser script error: " + scriptErr.Message
		}
	}
	// Not a ScriptError: a network/transport failure from the fetcher.
	return "Could not reach the site: " + err.Error()
}

// diffNovelSnapshot splits a TOC snapshot into the chapters missing from the
// store and the stored keys the snapshot no longer lists. It is the single
// new/missing computation every sync flow shares.
//
// Matching runs in one of two modes, chosen per novel:
//
//   - Keyed mode (the novel has at least one stored source_key): identity is
//     authoritative. A chapter whose key is not stored is genuinely new. This
//     is what the URL/ChapterKey diff asks for, and it is immune to the title
//     collisions that used to hide new chapters.
//   - Legacy mode (no stored source_key at all, i.e. every chapter predates the
//     field): identity cannot decide anything, so the historical title/order
//     heuristic runs instead. This is what stops a pre-existing novel from
//     reporting its whole library as new on the first sync after upgrading.
//
// forceLegacy forces legacy mode regardless of the stored keys. Stored keys
// stay authoritative in both modes — the key check below runs first — so
// forcing legacy only changes how the not-yet-keyed remainder is matched.
// That is the escape hatch for a novel stuck mid-migration: partially keyed,
// with a backfill plan that keeps failing (title drift). In keyed mode its
// legacy rows would all be reported as new and re-downloaded as permanent
// duplicates every sync; in legacy mode the title/order heuristic keeps
// matching them until a snapshot pairs cleanly again.
//
// missingKeys is returned for observability only: no flow deletes, matching the
// behavior of the update flows this replaces.
func diffNovelSnapshot(snapshot *sourceSnapshot, existingKeys map[string]bool, existingOrders map[int]bool, existingTitles map[string]bool, forceLegacy bool) ([]sourceChapter, []string) {
	newChapters := make([]sourceChapter, 0, len(snapshot.Chapters))
	seenKeys := make(map[string]bool, len(snapshot.Chapters))
	keyed := len(existingKeys) > 0 && !forceLegacy
	// accounted tracks site numbers whose stored first part was seen in this
	// batch, proving a same-number entry with a new title is another part.
	accounted := make(map[int]bool, len(snapshot.Chapters))
	seenTitles := make(map[string]bool, len(snapshot.Chapters))

	for _, ch := range snapshot.Chapters {
		key := ch.Key
		if key == "" {
			key = ch.URL
		}
		// Authoritative in both modes: this chapter's own identity is stored.
		if existingKeys[key] {
			continue
		}
		if seenKeys[key] {
			// Two TOC entries sharing one identity: the second carries no
			// new information, but it usually means the script's chapterKey
			// (or the site's URLs) collide, so say so once per key.
			slog.Warn("duplicate chapter identity in parser snapshot; entry skipped", "key", key, "title", ch.Title)
			continue
		}
		if !keyed {
			// Legacy mode: the title/order heuristic that backed source sync
			// before source_key existed.
			chNum := chapterOrderOf(ch)
			if ch.Title != "" && (existingTitles[ch.Title] || seenTitles[ch.Title]) {
				if chNum > 0 {
					accounted[chNum] = true
				}
				continue
			}
			if chNum > 0 && existingOrders[chNum] && !accounted[chNum] {
				continue
			}
			if ch.Title != "" {
				seenTitles[ch.Title] = true
			}
		}
		seenKeys[key] = true
		newChapters = append(newChapters, ch)
	}

	inSnapshot := make(map[string]bool, len(snapshot.Chapters))
	for _, ch := range snapshot.Chapters {
		if ch.Key != "" {
			inSnapshot[ch.Key] = true
		} else {
			inSnapshot[ch.URL] = true
		}
	}
	missingKeys := make([]string, 0)
	for key := range existingKeys {
		if !inSnapshot[key] {
			missingKeys = append(missingKeys, key)
		}
	}
	return newChapters, missingKeys
}

// backfillSourceKeyPlan builds the write plan that migrates a novel's legacy
// chapters (empty source_key) to keyed identity: for every stored chapter
// still without a key it takes the TOC entry sitting at the chapter's stored
// order and returns the key that entry would have been persisted with.
//
// No manual migration flag is needed for this: the plan is all-or-nothing and
// gated, so a novel that cannot be paired confidently simply stays on the
// legacy title/order heuristic, unchanged, instead of risking wrong identities.
//
// Two pairing passes run in order, and the first full pairing wins:
//
//  1. Positional — stored order N maps to snapshot position N-1 — accepted
//     only when both sides carry a non-empty title and the titles match
//     case-insensitively after trimming. That gate is what keeps the plan
//     honest: a positional pairing against drifted titles would stamp the
//     wrong identity onto a chapter permanently. This covers the common case
//     where the site listing and the library agree on order.
//  2. Title-unique — each legacy title must match exactly one snapshot entry
//     (normalized: trimmed, whitespace-collapsed, case-folded), and no two
//     rows may claim the same snapshot key. This covers novels whose stored
//     orders have gaps the snapshot positions don't share (e.g. a parser that
//     used to report real episode numbers instead of TOC positions): positions
//     disagree, but titles still identify chapters unambiguously.
//
// Writing keys for only part of a novel would flip the remaining key-less
// chapters into keyed-mode "new" on the next sync — the duplicate flood keyed
// identity exists to prevent — so any legacy row that cannot be paired
// confidently aborts the whole plan (ok=false). A failed plan is not an
// error; the novel simply stays on the legacy heuristic, unchanged.
//
// Rows that already carry a key are ignored without a consistency check; the
// keyed diff already governs them.
func backfillSourceKeyPlan(chapters []sourceChapter, stored []store.ChapterSyncMeta) (map[string]string, bool) {
	if plan, ok := backfillPositionalPlan(chapters, stored); ok {
		return plan, true
	}
	return backfillTitlePlan(chapters, stored)
}

// backfillPositionalPlan pairs stored order N with snapshot position N-1,
// gated on title equality. Like the title plan, it refuses to claim the same
// snapshot key twice: a TOC listing one URL twice would otherwise stamp two
// rows with one identity, and one of those keys would then shadow a real
// chapter on every later sync.
func backfillPositionalPlan(chapters []sourceChapter, stored []store.ChapterSyncMeta) (map[string]string, bool) {
	plan := make(map[string]string)
	claimed := make(map[string]bool)
	for _, meta := range stored {
		if meta.SourceKey != "" {
			continue
		}
		if meta.ChapterOrder < 1 || meta.ChapterOrder > len(chapters) {
			return nil, false
		}
		ch := chapters[meta.ChapterOrder-1]
		key := ch.Key
		if key == "" {
			key = ch.URL
		}
		if key == "" || claimed[key] || ch.Title == "" || meta.Title == "" ||
			!strings.EqualFold(strings.TrimSpace(ch.Title), strings.TrimSpace(meta.Title)) {
			return nil, false
		}
		claimed[key] = true
		plan[meta.ID] = key
	}
	return plan, true
}

// backfillTitlePlan pairs legacy rows to snapshot entries by normalized title,
// requiring each legacy title to match exactly one snapshot entry and each
// snapshot key to be claimed at most once.
func backfillTitlePlan(chapters []sourceChapter, stored []store.ChapterSyncMeta) (map[string]string, bool) {
	byTitle := make(map[string][]sourceChapter)
	for _, ch := range chapters {
		if strings.TrimSpace(ch.Title) == "" {
			continue
		}
		norm := normalizeChapterTitle(ch.Title)
		byTitle[norm] = append(byTitle[norm], ch)
	}
	plan := make(map[string]string)
	claimed := make(map[string]bool)
	for _, meta := range stored {
		if meta.SourceKey != "" {
			continue
		}
		if strings.TrimSpace(meta.Title) == "" {
			return nil, false
		}
		candidates := byTitle[normalizeChapterTitle(meta.Title)]
		if len(candidates) != 1 {
			return nil, false
		}
		key := candidates[0].Key
		if key == "" {
			key = candidates[0].URL
		}
		if key == "" || claimed[key] {
			return nil, false
		}
		claimed[key] = true
		plan[meta.ID] = key
	}
	return plan, true
}

// normalizeChapterTitle folds a chapter title for identity comparison:
// trimmed, internal whitespace collapsed, lowercased.
func normalizeChapterTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// legacyDiffFallback reports whether a failed backfill plan must force the
// legacy diff heuristic. A plan only fails while legacy rows remain, so a
// novel whose rows are all keyed (or all legacy) needs no override: the first
// diffs as keyed, the second naturally diffs as legacy (its key set is
// empty). The dangerous combination is the mid-migration novel — some rows
// keyed, some not — where keyed mode would report every legacy row as new
// and re-download the whole legacy library as permanent duplicates on every
// sync.
func legacyDiffFallback(planOK bool, stored []store.ChapterSyncMeta) bool {
	if planOK {
		return false
	}
	anyKeyed, anyLegacy := false, false
	for _, meta := range stored {
		if meta.SourceKey == "" {
			anyLegacy = true
		} else {
			anyKeyed = true
		}
	}
	return anyKeyed && anyLegacy
}

// legacySourceKeyPlan computes the backfill plan that would migrate a novel's
// legacy chapters (empty source_key) to keyed identity, without writing
// anything. Read-only flows (check-preview, batch-check, check jobs) use this
// so a preview never mutates the database: they diff against the merged key
// set in memory. The second return value forces the legacy diff heuristic
// when the plan failed on a mid-migration novel (see legacyDiffFallback).
// Only the update flow that actually enqueues downloads persists the plan
// (see backfillLegacySourceKeys).
func (s *Server) legacySourceKeyPlan(userID, novelID string, chapters []sourceChapter) (map[string]string, bool, error) {
	stored, err := s.Store.ListChapterSyncMeta(userID, novelID)
	if err != nil {
		return nil, false, err
	}
	plan, ok := backfillSourceKeyPlan(chapters, stored)
	if ok && len(plan) > 0 {
		return plan, false, nil
	}
	return nil, legacyDiffFallback(ok, stored), nil
}

// mergeSourceKeys returns a new set with the plan's keys added to the stored
// ones, so a read-only diff can observe the novel as fully keyed without
// re-querying the store or writing anything. The input set is not mutated.
func mergeSourceKeys(existingKeys map[string]bool, plan map[string]string) map[string]bool {
	if len(plan) == 0 {
		return existingKeys
	}
	merged := make(map[string]bool, len(existingKeys)+len(plan))
	for key := range existingKeys {
		merged[key] = true
	}
	for _, key := range plan {
		merged[key] = true
	}
	return merged
}

// backfillLegacySourceKeys runs the migration for one novel before its sync
// diff: it plans the key writes from the current TOC snapshot, persists them
// and merges the new keys into existingKeys, so the caller's diffNovelSnapshot
// call observes the novel as fully keyed without re-querying the store.
//
// Persisting is reserved for the write endpoint that enqueues downloads
// (update-from-url): previews and checks plan in memory only. Novels that are
// already keyed, or whose legacy rows cannot be confidently paired with the
// snapshot, are left untouched. The boolean return forces the legacy diff
// heuristic when the plan failed on a mid-migration novel (see
// legacyDiffFallback) — that keeps the sync matching by title/order instead
// of re-downloading every legacy row as a duplicate. A write failure is the
// only error path.
func (s *Server) backfillLegacySourceKeys(userID, novelID string, chapters []sourceChapter, existingKeys map[string]bool) (bool, error) {
	stored, err := s.Store.ListChapterSyncMeta(userID, novelID)
	if err != nil {
		return false, err
	}
	plan, ok := backfillSourceKeyPlan(chapters, stored)
	if ok && len(plan) > 0 {
		if _, err := s.Store.BackfillSourceKeys(userID, novelID, plan); err != nil {
			return false, err
		}
		for _, key := range plan {
			existingKeys[key] = true
		}
		slog.Info("backfilled legacy source keys", "novel", novelID, "chapters", len(plan))
		return false, nil
	}
	return legacyDiffFallback(ok, stored), nil
}
