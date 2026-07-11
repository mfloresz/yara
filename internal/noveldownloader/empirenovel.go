package noveldownloader

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var empireNovelBaseURL = "https://www.empirenovel.com"

// chapterDateRe matches a publication date that some chapter listings append
// directly to the chapter title (e.g. "Chapter 7Jun 24, 2026" — the date has
// no separator from the title). It is stripped so the stored chapter title is
// just "Chapter 7".
var chapterDateRe = regexp.MustCompile(`(?i)(jan(uary)?|feb(ruary)?|mar(ch)?|apr(il)?|may|jun(e)?|jul(y)?|aug(ust)?|sep(t(ember)?)?|oct(ober)?|nov(ember)?|dec(ember)?)[a-z]*\.?\s+\d{1,2}(st|nd|rd|th)?,?\s+\d{4}`)

type empirenovelParser struct{}

func NewEmpireNovelParser() *empirenovelParser {
	return &empirenovelParser{}
}

func (p *empirenovelParser) Name() string { return "empirenovel" }

func (p *empirenovelParser) CanHandle(urlStr string) bool {
	return strings.Contains(urlStr, "empirenovel.com")
}

func (p *empirenovelParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
	doc, err := client.FetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL: url,
	}

	// Title: h1.show_title
	if t := doc.Find("h1.show_title").Text(); t != "" {
		info.Title = strings.TrimSpace(t)
	}
	if info.Title == "" {
		info.Title = metaContent(doc, "meta[property='og:title']")
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Author: a[href*="author"]
	doc.Find("a[href*='author']").Each(func(_ int, a *goquery.Selection) {
		text := strings.TrimSpace(a.Text())
		if text != "" && info.Author == "" {
			info.Author = text
		}
	})

	// Description: meta[name="description"]
	info.Description = metaContent(doc, "meta[name='description']")
	if info.Description == "" {
		info.Description = metaContent(doc, "meta[property='og:description']")
	}

	// Cover: .cover img
	if coverImg := doc.Find(".cover img"); coverImg.Length() > 0 {
		if src, exists := coverImg.Attr("src"); exists && src != "" {
			if !strings.HasPrefix(src, "http") {
				src = empireNovelBaseURL + src
			}
			info.CoverURL = src
		}
	}
	if info.CoverURL == "" {
		info.CoverURL = metaContent(doc, "meta[property='og:image']")
	}

	// Chapters: paginated list across multiple pages
	info.Chapters = p.fetchAllChapterURLs(ctx, client, doc, url)

	return info, nil
}

func (p *empirenovelParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	return p.fetchAllChapterURLs(ctx, client, doc, url), nil
}

func (p *empirenovelParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	// Title: h3 inside the content area
	title := strings.TrimSpace(doc.Find(".mx-2 h3, .mx-sm-5 h3").Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h3").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Content: p tags inside the reader content area
	contentSel := doc.Find(".mx-2.mx-sm-5.p-1.p-sm-5")
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".reader-page .mx-2")
	}

	// Remove non-content elements
	contentSel.Find("script, style, noscript, iframe, nav, header, footer, .ads, .ad").Remove()

	contentParts := extractParagraphs(contentSel)

	// Fallback: extract all text if no paragraphs found
	if len(contentParts) == 0 {
		rawText := contentSel.Text()
		lines := strings.Split(rawText, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				contentParts = append(contentParts, "<p>"+trimmed+"</p>")
			}
		}
	}

	content := strings.Join(contentParts, "\n")

	return &Chapter{
		Title:     title,
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

func (p *empirenovelParser) fetchAllChapterURLs(ctx context.Context, client HTTPClient, firstPageDoc *goquery.Document, novelURL string) []ChapterURL {
	seen := make(map[string]bool)
	var allChapters []ChapterURL

	// Extract chapters from first page
	extractChaptersFromDoc(firstPageDoc, novelURL, seen, &allChapters)

	// Discover total pages from pagination
	totalPages := p.findTotalPages(firstPageDoc)

	// Fetch remaining pages
	for page := 2; page <= totalPages; page++ {
		pageURL := novelURL
		if strings.Contains(pageURL, "?") {
			pageURL = pageURL + "&page=" + strconv.Itoa(page)
		} else {
			pageURL = pageURL + "?page=" + strconv.Itoa(page)
		}

		doc, err := client.FetchDocument(ctx, pageURL)
		if err != nil {
			continue
		}

		extractChaptersFromDoc(doc, novelURL, seen, &allChapters)
	}

	// The source lists chapters newest-first (descending). Sort ascending by
	// chapter number so the returned slice order matches the chapter ordering
	// the rest of the app expects (position N ≈ chapter N), which keeps
	// range-based downloads (startChapter..endChapter) correct.
	sort.SliceStable(allChapters, func(i, j int) bool {
		return empireNovelChapterNumber(allChapters[i].URL) < empireNovelChapterNumber(allChapters[j].URL)
	})

	return allChapters
}

func empireNovelChapterNumber(urlStr string) int {
	u := strings.TrimSuffix(urlStr, "/")
	idx := strings.LastIndex(u, "/")
	if idx == -1 {
		return 0
	}
	suffix := u[idx+1:]
	// Parse the leading integer so range-style suffixes (e.g. "420-421")
	// still sort by their starting chapter.
	for i, c := range suffix {
		if c < '0' || c > '9' {
			suffix = suffix[:i]
			break
		}
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return n
}

func (p *empirenovelParser) findTotalPages(doc *goquery.Document) int {
	maxPage := 1
	doc.Find("a[href*='page=']").Each(func(_ int, a *goquery.Selection) {
		href, exists := a.Attr("href")
		if !exists {
			return
		}
		// Extract page number from href like ?page=14 or &page=14
		idx := strings.LastIndex(href, "page=")
		if idx == -1 {
			return
		}
		numStr := href[idx+5:]
		// Cut off at any non-digit character
		for i, c := range numStr {
			if c < '0' || c > '9' {
				numStr = numStr[:i]
				break
			}
		}
		if num, err := strconv.Atoi(numStr); err == nil && num > maxPage {
			maxPage = num
		}
	})
	return maxPage
}

func extractChaptersFromDoc(doc *goquery.Document, novelURL string, seen map[string]bool, allChapters *[]ChapterURL) {
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, exists := a.Attr("href")
		if !exists || href == "" {
			return
		}

		// Normalize URL
		if !strings.HasPrefix(href, "http") {
			href = empireNovelBaseURL + href
		}
		href = strings.TrimSuffix(href, "/")

		// Must be a chapter link: /novel/{slug}/{number}
		if !isEmpireNovelChapterURL(href, novelURL) {
			return
		}

		if seen[href] {
			return
		}
		seen[href] = true

		title := strings.TrimSpace(a.Text())
		// Clean up title: remove dates and extra whitespace
		if idx := strings.Index(title, "\n"); idx != -1 {
			title = strings.TrimSpace(title[:idx])
		}
		// Strip a publication date that may be glued to the title with no
		// separator (e.g. "Chapter 7Jun 24, 2026").
		title = chapterDateRe.ReplaceAllString(title, "")
		// Remove redundant "Chapter N" prefix if it appears twice
		title = strings.Join(strings.Fields(title), " ")

		*allChapters = append(*allChapters, ChapterURL{
			Title: title,
			URL:   href,
		})
	})
}

func isEmpireNovelChapterURL(href, novelURL string) bool {
	// Normalize both URLs for comparison
	novelURL = strings.TrimSuffix(novelURL, "/")
	href = strings.TrimSuffix(href, "/")

	// The chapter URL should start with the novel URL path
	novelPath := novelURL
	if idx := strings.Index(novelURL, "empirenovel.com"); idx != -1 {
		novelPath = novelURL[idx+len("empirenovel.com"):]
	}

	hrefPath := href
	if idx := strings.Index(href, "empirenovel.com"); idx != -1 {
		hrefPath = href[idx+len("empirenovel.com"):]
	}

	if !strings.HasPrefix(hrefPath, novelPath) {
		return false
	}

	// After the novel path, there should be /{number}
	suffix := strings.TrimPrefix(hrefPath, novelPath)
	suffix = strings.TrimPrefix(suffix, "/")

	if suffix == "" {
		return false
	}

	// Check that the suffix is a pure number (chapter number)
	_, err := strconv.Atoi(suffix)
	return err == nil
}

// Ensure interface compliance at compile time.
var _ Parser = (*empirenovelParser)(nil)
