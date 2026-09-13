package noveldownloader

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// WebnovelParser downloads from webnovel.com. Book, catalog, and chapter
// pages are server-side rendered and answer anonymous requests (verified
// HTTP 200 with plain curl: no Cloudflare challenge and no login, even for
// 18+ books), so no session cookie or browser worker is needed.
//
// URL shapes (an optional locale prefix like /es/ may precede /book/):
//
//   - Book:    /book/<slug>_<bookId> or /book/<bookId>
//   - Catalog: /book/<bookId>/catalog (the chapter list is NOT embedded in
//     the book page — its #contents pane loads via AJAX — so the parser
//     always fetches the catalog page separately)
//   - Chapter: /book/<slug>_<bookId>/chapter-<n>_<chapterId>
//
// Quirk: some chapter slugs carry a trailing U+FEFF (zero-width no-break
// space), percent-encoded as %EF%BB%BF (e.g. chapter-17%EF%BB%BF_<id>).
// URL parsing and title cleaning strip it explicitly.
type webnovelParser struct{}

func NewWebnovelParser() *webnovelParser {
	return &webnovelParser{}
}

func (p *webnovelParser) Name() string { return "webnovel" }

// RequiresBrowser: book, catalog, and chapter pages are static SSR HTML
// with the full chapter text in div.cha-words (verified with plain curl),
// so reliable fetching does not need the browser worker extension.
func (p *webnovelParser) RequiresBrowser() bool { return false }

func (p *webnovelParser) CanHandle(urlStr string) bool {
	_, _, ok := webnovelParseURL(urlStr)
	return ok
}

// webnovelURLKind classifies a webnovel.com URL as a book page (book or
// catalog) or a chapter page.
type webnovelURLKind int

const (
	webnovelURLInvalid webnovelURLKind = iota
	webnovelURLBook
	webnovelURLChapter
)

// webnovelParseURL parses a webnovel.com URL, returning the numeric book id,
// its kind (book or chapter page), and whether the URL belongs to the site.
func webnovelParseURL(rawURL string) (string, webnovelURLKind, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", webnovelURLInvalid, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	if host != "webnovel.com" {
		return "", webnovelURLInvalid, false
	}
	if !strings.Contains(u.Path, "/book/") {
		return "", webnovelURLInvalid, false
	}
	bookID := webnovelBookID(u.Path)
	if bookID == "" {
		return "", webnovelURLInvalid, false
	}
	// Some catalog hrefs prefix the chapter slug with a U+FEFF marker
	// (%EF%BB%BF), e.g. /%EF%BB%BFchapter-72_<id>, which decodes to
	// /\ufeffchapter-... in u.Path and breaks a literal "/chapter-" match.
	// Strip it before classifying; the marker is zero-width so removal is safe.
	normalizedPath := strings.ReplaceAll(u.Path, "\ufeff", "")
	if strings.Contains(normalizedPath, "/chapter-") {
		return bookID, webnovelURLChapter, true
	}
	return bookID, webnovelURLBook, true
}

// webnovelBookID extracts the numeric book id from a /book/... path: the
// first run of 5+ digits after /book/. Slugs never contain such runs, so
// this skips both the slug text and the short chapter number in chapter
// URLs (chapter-129_<17-digit-id>).
func webnovelBookID(path string) string {
	idx := strings.Index(path, "/book/")
	if idx < 0 {
		return ""
	}
	rest := path[idx + len("/book/"):]
	start := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] >= '0' && rest[i] <= '9' {
			if start < 0 {
				start = i
			}
		} else {
			if start >= 0 && i-start >= 5 {
				return rest[start:i]
			}
			start = -1
		}
	}
	if start >= 0 && len(rest)-start >= 5 {
		return rest[start:]
	}
	return ""
}

// webnovelCatalogURL builds the canonical catalog URL carrying the full
// chapter list (the site's own <link rel="canonical"> on the catalog page
// uses this locale-free form).
func webnovelCatalogURL(pageURL, bookID string) string {
	return fmt.Sprintf("%s/book/%s/catalog", extractBaseURL(pageURL), bookID)
}

// webnovelStripLocale removes a leading locale segment (e.g. /es, /pt) from
// a catalog href path so emitted chapter URLs stay in the canonical
// locale-free form instead of inheriting a UI locale.
func webnovelStripLocale(href string) string {
	if len(href) > 7 && href[0] == '/' && href[3] == '/' {
		locale := href[1:3]
		if isASCIILetters(locale) && strings.HasPrefix(href[3:], "/book/") {
			return href[3:]
		}
	}
	return href
}

func isASCIILetters(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return len(s) > 0
}

func (p *webnovelParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	bookID, _, ok := webnovelParseURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid webnovel URL: %s", pageURL)
	}

	// A chapter URL carries no synopsis, so metadata always comes from the
	// canonical book page.
	bookURL := fmt.Sprintf("%s/book/%s", extractBaseURL(pageURL), bookID)
	doc, err := client.FetchDocument(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("fetching book page: %w", err)
	}

	title := metaContent(doc, "meta[property='og:title']")
	if title == "" {
		title = strings.TrimSpace(doc.Find("div.det-info h1").First().Text())
	}
	if title == "" {
		return nil, fmt.Errorf("book %s not found or has no title", bookID)
	}

	author := metaContent(doc, "meta[property='og:author']")
	if author == "" {
		author = strings.TrimSpace(doc.Find("address a").First().Text())
	}

	description := metaContent(doc, "meta[property='og:description']")
	if description == "" {
		description = metaContent(doc, "meta[name='description']")
	}

	cover := metaContent(doc, "meta[property='og:image']")
	if strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}

	chapters, err := p.fetchChapters(ctx, client, pageURL, bookID)
	if err != nil {
		return nil, err
	}

	return &NovelInfo{
		Title:       CleanTitle(webnovelCleanText(title)),
		Author:      strings.TrimSpace(webnovelCleanText(author)),
		Description: strings.TrimSpace(webnovelCleanText(description)),
		CoverURL:    strings.TrimSpace(cover),
		SourceURL:   bookURL,
		Chapters:    chapters,
	}, nil
}

func (p *webnovelParser) GetChapterURLs(ctx context.Context, client HTTPClient, _ *goquery.Document, pageURL string) ([]ChapterURL, error) {
	bookID, _, ok := webnovelParseURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid webnovel URL: %s", pageURL)
	}
	return p.fetchChapters(ctx, client, pageURL, bookID)
}

// fetchChapters loads the catalog page (the only source of the full chapter
// list) and extracts its entries in page order.
func (p *webnovelParser) fetchChapters(ctx context.Context, client HTTPClient, pageURL, bookID string) ([]ChapterURL, error) {
	doc, err := client.FetchDocument(ctx, webnovelCatalogURL(pageURL, bookID))
	if err != nil {
		return nil, fmt.Errorf("fetching catalog page: %w", err)
	}
	chapters := webnovelExtractChapters(doc, pageURL)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("book %s has no chapters in its catalog", bookID)
	}
	return chapters, nil
}

// webnovelExtractChapters collects the chapter links from a catalog page.
// Entries live in li[data-cid] (one per chapter, in ascending order); the
// selector stays scoped to those items so the header shortcuts (read-now /
// latest-chapter links, which duplicate real chapters) are never picked up.
func webnovelExtractChapters(doc *goquery.Document, pageURL string) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)
	doc.Find("li[data-cid] a[href]").Each(func(_ int, a *goquery.Selection) {
		href, exists := a.Attr("href")
		if !exists || href == "" || !strings.Contains(href, "chapter-") {
			return
		}
		chapterURL := resolveURL(pageURL, webnovelStripLocale(href))
		if seen[chapterURL] {
			return
		}
		seen[chapterURL] = true
		title := strings.TrimSpace(a.Find("strong").First().Text())
		if title == "" {
			if t, exists := a.Attr("title"); exists {
				title = strings.TrimSpace(t)
			}
		}
		if title == "" {
			title = strings.TrimSpace(a.Text())
		}
		chapters = append(chapters, ChapterURL{
			URL:   chapterURL,
			Title: CleanTitle(webnovelCleanText(title)),
			Order: len(chapters) + 1,
		})
	})
	return chapters
}

func (p *webnovelParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	if _, kind, ok := webnovelParseURL(chapterURL); !ok || kind != webnovelURLChapter {
		return nil, fmt.Errorf("not a webnovel chapter URL: %s", chapterURL)
	}
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	// Locked (VIP/paywalled) chapters render an empty body behind a
	// div.cha-content._lock gate with data-islock="1"; anonymous fetches
	// cannot unfold them.
	if doc.Find(`div.chapter_content[data-islock="1"]`).Length() > 0 ||
		doc.Find("div.cha-content._lock").Length() > 0 {
		return nil, fmt.Errorf("chapter is locked (VIP/paywalled): open it in the WebNovel app or a logged-in browser to read it")
	}

	// data-chaptername is the clean "Chapter N" title; the visible h1
	// carries a locale prefix ("Capítulo 1: Chapter 1").
	title := ""
	if name, exists := doc.Find("div.chapter_content").First().Attr("data-chaptername"); exists {
		title = strings.TrimSpace(name)
	}
	if title == "" {
		if h1 := strings.TrimSpace(doc.Find("div.cha-tit h1").First().Text()); h1 != "" {
			if idx := strings.LastIndex(h1, ": "); idx >= 0 {
				h1 = strings.TrimSpace(h1[idx+2:])
			}
			title = h1
		}
	}

	content := webnovelChapterContent(doc)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("chapter page has no readable content (it may be locked or removed)")
	}

	return &Chapter{
		Title:     CleanTitle(webnovelCleanText(title)),
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// webnovelChapterContent extracts the chapter body, preserving inline markup
// (<i>, <b>) so the downstream html-to-markdown conversion keeps emphasis.
func webnovelChapterContent(doc *goquery.Document) string {
	sel := doc.Find("div.chapter_content div.cha-words")
	if sel.Length() == 0 {
		sel = doc.Find("div.cha-words")
	}
	var parts []string
	sel.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
		inner, err := paragraph.Html()
		if err != nil {
			return
		}
		if strings.TrimSpace(paragraph.Text()) == "" {
			return
		}
		parts = append(parts, "<p>"+strings.TrimSpace(inner)+"</p>")
	})
	return strings.Join(parts, "\n")
}

// webnovelCleanText strips the U+FEFF markers the site embeds in some
// chapter slugs and titles (escaped: a literal U+FEFF is illegal in Go
// source).
func webnovelCleanText(s string) string {
	return strings.ReplaceAll(s, "\ufeff", "")
}

// Ensure interface compliance at compile time.
var _ Parser = (*webnovelParser)(nil)
