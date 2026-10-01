// Package parserhost runs user-editable JavaScript site parsers inside an
// embedded goja VM. All site-specific logic lives in the scripts; the host owns
// networking, diffing and storage.
package parserhost

import (
	"context"
	"fmt"
)

// Script contract — the canonical shape of a parser script:
//
//	module.exports = {
//	  name: "site",             // required, non-empty
//	  apiVersion: 1,            // required, must equal 1 (else load error)
//	  requiresBrowser: false,   // bool; true routes this script's fetches
//	                             // through the user's browser worker
//	  livewireCatalogPattern: "", // optional Go-regexp source; on the browser-worker
//	                             // path, URLs matching it are fetched through the
//	                             // worker's Livewire operation instead of fetch_page
//	  probe: (url) => boolean,  // can this script handle the URL?
//	  // "what exists": full snapshot; in-site pagination is the SCRIPT's job
//	  // (multiple ctx.get calls allowed within the fetch budget)
//	  toc: (ctx, url) => ({
//	    novel: { title, description, author, coverUrl, language, tags },
//	    chapters: [{ title, url }],   // ordered; host computes the new/missing diff
//	  }),
//	  // "how to obtain one chapter"
//	  chapter: (ctx, url) => ({ title, contentHtml }),
//	  chapterKey: (ch) => string,     // optional; default identity = chapter URL
//	}
//
// Module init runs once per invocation in a throwaway VM, so it must not
// fetch: everything network-bound belongs in toc or chapter.
type Novel struct {
	Title       string
	Description string
	Author      string
	CoverURL    string
	Language    string
	Tags        []string
}

type ChapterRef struct {
	Title string
	URL   string
}

// TOC is a full snapshot of what a site currently has. The host diffs it
// against its store to decide what to download.
type TOC struct {
	Novel    Novel
	Chapters []ChapterRef
}

type Chapter struct {
	Title       string
	ContentHTML string
}

// Error taxonomy. Scripts signal failure with ctx.fail(code, message); every
// other fault (uncaught throw, shape violation, interrupt) is mapped to one of
// these codes by the host. Callers recover the code with errors.As:
//
//	var se *parserhost.ScriptError
//	if errors.As(err, &se) { switch se.Code { ... } }
//
// A network failure from the Fetcher is deliberately NOT a ScriptError: it
// surfaces as a plain wrapped Go error so callers can tell a broken site from
// a broken parser.
const (
	// CodeNotMySite is a probe mismatch or an unexpected URL.
	CodeNotMySite = "not_my_site"
	// CodeSiteLayoutChanged means the expected elements were not found and
	// the script called ctx.fail with this code.
	CodeSiteLayoutChanged = "site_layout_changed"
	// CodeBlocked means a Cloudflare/challenge page was not resolved.
	CodeBlocked = "blocked"
	// CodeParserTimeout means the CPU interrupt fired or the per-invocation
	// fetch budget was exceeded.
	CodeParserTimeout = "parser_timeout"
	// CodeScriptError is any other uncaught script or host error.
	CodeScriptError = "script_error"
)

// ScriptError carries a taxonomy code plus the VM stack when the failure came
// from inside the script.
type ScriptError struct {
	Code    string
	Message string
	Stack   string
}

func (e *ScriptError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func validCode(code string) bool {
	switch code {
	case CodeNotMySite, CodeSiteLayoutChanged, CodeBlocked, CodeParserTimeout, CodeScriptError:
		return true
	}
	return false
}

func shapeError(format string, args ...any) *ScriptError {
	return &ScriptError{Code: CodeScriptError, Message: fmt.Sprintf(format, args...)}
}

// FetchResult is one HTTP response as produced by the host's Fetcher. FinalURL
// is the URL after redirects and doubles as the base for relative link
// resolution inside the document.
type FetchResult struct {
	FinalURL string
	Status   int
	Body     []byte
}

// Fetcher is the inversion that keeps the engine out of the networking
// business: the engine never issues HTTP itself, it only calls back into the
// host-supplied Fetcher (which owns proxies, browser-worker routing, throttling
// and charset handling). An error returned here is a network failure and
// propagates to the caller as a plain Go error, never as a ScriptError.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (*FetchResult, error)
}
