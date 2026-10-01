package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"translator-server/internal/config"
	"translator-server/internal/parserhost"
)

// stdout is the snapshot destination, indirected so tests can capture it.
var stdout io.Writer = os.Stdout

// RunParserCheck exercises a single parser script against a URL and prints the
// canonical snapshot as indented JSON. It is the debugging entry point for
// writing a script: run it against the live site, read the shape, fix the
// script, repeat. It touches no database and no data directory, and exits
// 0 on success / 1 on failure.
//
// Browser-worker routing is NOT available here: the relay needs a live
// authenticated WebSocket from a user's browser extension, which does not exist
// before the server is up. A script declaring requiresBrowser therefore can
// only be partially verified in this mode — the warning is printed for exactly
// that reason.
func RunParserCheck(cfg *config.Config) int {
	if strings.TrimSpace(cfg.CheckURL) == "" {
		fmt.Fprintln(os.Stderr, "check mode requires -check-url <novel url>")
		return 1
	}

	fetcher := &checkFetcher{client: &http.Client{Timeout: 30 * time.Second}}
	engine := parserhost.NewEngine(fetcher, parserhost.Options{})

	script, err := engine.LoadFile(cfg.CheckParser)
	if err != nil {
		printCheckError("load", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "parser: %s\nurl: %s\n\n", script.Name(), cfg.CheckURL)

	if script.RequiresBrowser() {
		fmt.Fprintln(os.Stderr, "WARNING: this script declares requiresBrowser. "+
			"Check mode has no browser worker attached, so fetches go out as plain HTTP "+
			"and may be blocked by the site. Verify it in the running server instead.")
		fmt.Fprintln(os.Stderr)
	}

	matched, err := script.Probe(cfg.CheckURL)
	if err != nil {
		printCheckError("probe", err)
		return 1
	}
	if !matched {
		fmt.Fprintf(os.Stderr, "probe returned false: this script does not handle %s\n", cfg.CheckURL)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	toc, err := script.TOC(ctx, cfg.CheckURL)
	if err != nil {
		printCheckError("toc", err)
		return 1
	}

	chapters := make([]map[string]any, 0, len(toc.Chapters))
	for i, ref := range toc.Chapters {
		key, keyErr := script.ChapterKey(ref)
		if keyErr != nil {
			// Same fallback as the server path: one bad key must not fail
			// the whole check.
			fmt.Fprintf(os.Stderr, "WARNING: chapterKey failed for %s, using URL as identity: %v\n", ref.URL, keyErr)
			key = ref.URL
		}
		chapters = append(chapters, map[string]any{
			"index": i + 1,
			"title": ref.Title,
			"url":   ref.URL,
			"key":   key,
		})
	}

	out := map[string]any{
		"parser": script.Name(),
		"url":    cfg.CheckURL,
		"novel": map[string]any{
			"title":       toc.Novel.Title,
			"author":      toc.Novel.Author,
			"description": toc.Novel.Description,
			"coverUrl":    toc.Novel.CoverURL,
			"language":    toc.Novel.Language,
			"tags":        toc.Novel.Tags,
		},
		"chapters": chapters,
	}

	if len(toc.Chapters) == 0 {
		encoded, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
		fmt.Fprintln(os.Stderr, "\ntoc returned no chapters; the chapter step was skipped")
		return 0
	}

	first := toc.Chapters[0]
	chapter, err := script.Chapter(ctx, first.URL)
	if err != nil {
		printCheckError("chapter", err)
		return 1
	}
	out["chapterPreview"] = map[string]any{
		"url":          first.URL,
		"title":        chapter.Title,
		"contentChars": len(chapter.ContentHTML),
		"contentHtml":  truncateForDisplay(chapter.ContentHTML),
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode snapshot: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(encoded))
	return 0
}

// truncateForDisplay keeps the printed snapshot readable; the full body is
// never useful in a terminal. Cut on a rune boundary so multibyte text is
// never split mid-character.
func truncateForDisplay(s string) string {
	const limit = 800
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "… (truncated)"
}

// printCheckError renders a failure with the taxonomy code when the script
// raised one, so a broken script reports why rather than just that it broke.
func printCheckError(stage string, err error) {
	var scriptErr *parserhost.ScriptError
	if errors.As(err, &scriptErr) {
		fmt.Fprintf(os.Stderr, "%s failed [%s]: %s\n", stage, scriptErr.Code, scriptErr.Message)
		if scriptErr.Stack != "" {
			fmt.Fprintln(os.Stderr, scriptErr.Stack)
		}
		return
	}
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", stage, err)
}

// checkFetcher is a plain HTTP fetcher for check mode. It deliberately has no
// browser-worker path (see RunParserCheck) and no throttling: this is an
// interactive debugging tool, not a crawl.
type checkFetcher struct {
	client *http.Client
}

func (f *checkFetcher) Fetch(ctx context.Context, rawURL string) (*parserhost.FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	// Same site shims as the server path, so check mode sees what production
	// sees (required Referer headers, GBK-family bodies). Deliberately no
	// SSRF guard and no throttling: this is an interactive debugging tool run
	// by the operator against an explicit URL, not a crawl.
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
	body = decodeChineseCharset(body, resp.Header.Get("Content-Type"))
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	result := &parserhost.FetchResult{FinalURL: finalURL, Status: resp.StatusCode, Body: body}
	// Same hybrid helpers as the server path, so check mode sees what
	// production would download (e.g. decoded chrysanthemumgarden fonts).
	if herr := applySiteHelpers(ctx, finalURL, result, f); herr != nil {
		return nil, herr
	}
	return result, nil
}

// fetchSiteAsset backs the hybrid response helpers in check mode. Deliberately
// no SSRF guard and no throttling, like checkFetcher.Fetch: this is an
// interactive debugging tool run by the operator against an explicit URL.
func (f *checkFetcher) fetchSiteAsset(ctx context.Context, pageURL, assetURL string, maxBytes int64) ([]byte, error) {
	abs, err := resolvePageAssetURL(pageURL, assetURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, abs, nil)
	if err != nil {
		return nil, fmt.Errorf("creating asset request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching asset %s: %w", abs, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d fetching asset %s", resp.StatusCode, abs)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading asset %s: %w", abs, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("asset %s exceeds %d bytes", abs, maxBytes)
	}
	return body, nil
}
