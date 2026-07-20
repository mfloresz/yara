package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type skydemonorderParser struct{}

func NewSkyDemonOrderParser() *skydemonorderParser {
	return &skydemonorderParser{}
}

func (p *skydemonorderParser) Name() string { return "skydemonorder" }

func (p *skydemonorderParser) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	return host == "skydemonorder.com"
}

// firstChapterURL extracts the chapter-1 link from the project page. The
// "Start Reading" button points at the first chapter; failing that we fall
// back to the canonical project slug + "/1-…" pattern. The site has no
// server-rendered chapter index, so the list is built by walking the
// "Next chapter" link from this starting point.
func (p *skydemonorderParser) firstChapterURL(doc *goquery.Document, pageURL string) (string, error) {
	if href, exists := doc.Find("a[href*='/projects/']").FilterFunction(func(_ int, s *goquery.Selection) bool {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		return strings.Contains(text, "start reading")
	}).First().Attr("href"); exists && href != "" {
		return resolveURL(pageURL, href), nil
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parsing project URL: %w", err)
	}
	return strings.TrimRight(u.String(), "/") + "/1", nil
}

func (p *skydemonorderParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	slog.Info("skydemonorder: fetching novel page", "url", pageURL)
	rawHTML, err := client.Fetch(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetching novel page: %w", err)
	}
	slog.Info("skydemonorder: page fetched", "htmlLen", len(rawHTML), "hasFreeChapters", strings.Contains(string(rawHTML), "freeChapters"))

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(rawHTML)))
	if err != nil {
		return nil, fmt.Errorf("parsing novel page: %w", err)
	}

	title := strings.TrimSpace(doc.Find("h1.font-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	coverURL := extractSkyDemonOrderCover(doc)
	description := extractSkyDemonOrderDescription(doc)

	chapters, err := p.extractChaptersFromHTML(string(rawHTML), pageURL)
	if err != nil {
		return nil, fmt.Errorf("extracting chapters from HTML: %w", err)
	}
	slog.Info("skydemonorder: extractChaptersFromHTML result", "count", len(chapters))

	if len(chapters) == 0 {
		slog.Warn("skydemonorder: no chapters from Livewire extraction, falling back to walkChapters")
		startURL, err2 := p.firstChapterURL(doc, pageURL)
		if err2 != nil {
			return nil, fmt.Errorf("resolving first chapter: %w", err2)
		}
		chapters, err = p.walkChapters(ctx, client, startURL, pageURL)
		if err != nil {
			return nil, fmt.Errorf("walking chapters: %w", err)
		}
	}

	return &NovelInfo{
		Title:       title,
		Author:      "",
		Description: description,
		CoverURL:    coverURL,
		SourceURL:   pageURL,
		Chapters:    chapters,
	}, nil
}

func (p *skydemonorderParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, pageURL string) ([]ChapterURL, error) {
	startURL, err := p.firstChapterURL(doc, pageURL)
	if err != nil {
		return nil, fmt.Errorf("resolving first chapter: %w", err)
	}
	return p.walkChapters(ctx, client, startURL, pageURL)
}

// chapterJSON matches the structure of chapters inside JSON.parse() strings
// embedded in the page's Alpine.js x-data attribute.
type chapterJSON struct {
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	Slug    string `json:"slug"`
}

// reFreeChapters matches only freeChapters: JSON.parse('...') — not paidChapters.
var reFreeChapters = regexp.MustCompile(`freeChapters:\s*JSON\.parse\('([^']+)'\)`)

// extractChaptersFromHTML looks for the Livewire/Alpine.js freeChapters data
// embedded as a JSON.parse() call. Only free chapters are extracted — premium
// chapters are excluded since they require a subscription to read.
func (p *skydemonorderParser) extractChaptersFromHTML(rawHTML, pageURL string) ([]ChapterURL, error) {
	match := reFreeChapters.FindStringSubmatch(rawHTML)
	if match == nil {
		slog.Warn("skydemonorder: freeChapters regex did not match", "htmlLen", len(rawHTML), "containsFreeChapters", strings.Contains(rawHTML, "freeChapters"), "containsPaidChapters", strings.Contains(rawHTML, "paidChapters"))
		return nil, nil
	}
	slog.Info("skydemonorder: freeChapters regex matched", "matchLen", len(match[1]))

	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parsing project URL: %w", err)
	}

	decoded, err := decodeJSONString(match[1])
	if err != nil {
		return nil, fmt.Errorf("decoding chapter JSON: %w", err)
	}

	var items []chapterJSON
	if err := json.Unmarshal(decoded, &items); err != nil {
		return nil, fmt.Errorf("parsing chapter JSON: %w", err)
	}
	slog.Info("skydemonorder: parsed chapter JSON", "count", len(items))

	chapters := make([]ChapterURL, 0, len(items))
	for _, ch := range items {
		if ch.Slug == "" {
			continue
		}
		chURL := strings.TrimRight(u.String(), "/") + "/" + ch.Slug
		chapters = append(chapters, ChapterURL{
			URL:   chURL,
			Title: CleanTitle(ch.Title),
			Order: ch.Episode,
		})
	}

	// Sort ascending by episode number (1, 2, 3, …).
	sort.Slice(chapters, func(i, j int) bool {
		return chapters[i].Order < chapters[j].Order
	})

	return chapters, nil
}

// decodeJSONString handles the unicode-escaped JSON string from JSON.parse().
// The raw string from the HTML uses \u0022 for quotes, etc.
func decodeJSONString(raw string) ([]byte, error) {
	// Wrap in quotes to make it a valid Go string literal for json.Unmarshal.
	quoted := `"` + raw + `"`
	var s string
	if err := json.Unmarshal([]byte(quoted), &s); err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// walkChapters follows the "Next chapter" link from startURL until no further
// link exists, collecting every chapter URL/title. The site renders no
// chapter index server-side, so this sequential crawl is the only way to
// enumerate the full table of contents.
func (p *skydemonorderParser) walkChapters(ctx context.Context, client HTTPClient, startURL, pageURL string) ([]ChapterURL, error) {
	slog.Info("skydemonorder: walkChapters starting", "startURL", startURL)
	const maxChapters = 10000
	seen := make(map[string]bool)
	chapters := make([]ChapterURL, 0, 64)

	current := startURL
	for len(chapters) < maxChapters {
		if seen[current] {
			break
		}
		seen[current] = true

		doc, err := client.FetchDocument(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("fetching chapter %q: %w", current, err)
		}

		title := strings.TrimSpace(doc.Find("h1.font-title").First().Text())
		if title == "" {
			title = strings.TrimSpace(doc.Find("h1").First().Text())
		}
		chapters = append(chapters, ChapterURL{
			URL:   current,
			Title: CleanTitle(title),
		})

		next := extractSkyDemonOrderNextChapter(doc)
		if next == "" {
			break
		}
		current = resolveURL(pageURL, next)
	}

	slog.Info("skydemonorder: walkChapters done", "count", len(chapters))
	return chapters, nil
}

func extractSkyDemonOrderNextChapter(doc *goquery.Document) string {
	if href, exists := doc.Find("a[aria-label='Next chapter']").First().Attr("href"); exists {
		return strings.TrimSpace(href)
	}
	// Fallback: the reader also wires the right-arrow key to the next URL.
	next := ""
	doc.Find("div[class*='keydown.right']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		html, err := s.Html()
		if err != nil {
			return true
		}
		if idx := strings.Index(html, "window.location.href = '"); idx != -1 {
			rest := html[idx+len("window.location.href = '"):]
			if end := strings.Index(rest, "'"); end != -1 {
				next = rest[:end]
				return false
			}
		}
		return true
	})
	return next
}

func extractSkyDemonOrderCover(doc *goquery.Document) string {
	if el := doc.Find("div.w-full.max-w-72 img").First(); el.Length() > 0 {
		if src, exists := el.Attr("src"); exists {
			if u := strings.TrimSpace(src); u != "" && !strings.HasPrefix(u, "data:") {
				return u
			}
		}
	}
	if el := doc.Find("meta[property='og:image']").First(); el.Length() > 0 {
		if content, exists := el.Attr("content"); exists {
			return strings.TrimSpace(content)
		}
	}
	return ""
}

func extractSkyDemonOrderDescription(doc *goquery.Document) string {
	// The synopsis lives in a div whose class includes "line-clamp-3"; it
	// contains the real <p> paragraphs (the clamped view is just CSS).
	if el := doc.Find("div[class*='line-clamp-3']").First(); el.Length() > 0 {
		text := strings.TrimSpace(el.Text())
		if text != "" {
			return text
		}
	}
	if el := doc.Find("meta[name='description']").First(); el.Length() > 0 {
		if content, exists := el.Attr("content"); exists {
			return strings.TrimSpace(content)
		}
	}
	return ""
}

func (p *skydemonorderParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter: %w", err)
	}

	title := strings.TrimSpace(doc.Find("h1.font-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	contentSel := doc.Find("#chapter-body")
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#chapter-content")
	}
	if contentSel.Length() == 0 {
		return nil, fmt.Errorf("no chapter content found")
	}

	contentSel.Find("script, style, noscript").Remove()

	html, err := contentSel.Html()
	if err != nil {
		return nil, fmt.Errorf("getting chapter HTML: %w", err)
	}

	return &Chapter{
		Title:     title,
		Content:   strings.TrimSpace(html),
		SourceURL: chapterURL,
	}, nil
}

// Ensure interface compliance at compile time.
var _ Parser = (*skydemonorderParser)(nil)
