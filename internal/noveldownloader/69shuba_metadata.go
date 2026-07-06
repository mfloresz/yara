package noveldownloader

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// minimumChapters is the minimum number of chapters we need from the catalog
// before we consider a direct-HTTP fetch successful. If we get fewer than
// this (e.g. only the 5 recent chapters shown on the info page), we return
// an error so the caller can fall back to the browser proxy (which has login
// cookies and can access the full catalog at /book/{id}/).
const minimumChapters = 20

var (
	sixtyNineShubaTitleRe        = regexp.MustCompile(`<title>([^<]+)</title>`)
	sixtyNineShubaBookInfoJSRe   = regexp.MustCompile(`articlename:\s*'([^']+)'`)
	sixtyNineShubaAuthorJSRe     = regexp.MustCompile(`author:\s*'([^']+)'`)
	sixtyNineShubaWordCountRe    = regexp.MustCompile(`(\d+\.?\d*)万字`)
	sixtyNineShubaDescriptionRe  = regexp.MustCompile(`og:description.*?content="([^"]+)"`)
	sixtyNineShubaArticleIDRe    = regexp.MustCompile(`articleid:\s*'(\d+)'`)
)

func (s *sixtyNineShuba) getInfoFromInfoPage(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	raw, err := client.Fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	// Decode GBK to UTF-8 (the site serves <meta charset="gbk">)
	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	info := &NovelInfo{
		SourceURL: u,
	}

	htmlStr := string(raw)

	// Extract metadata — after GBK→UTF-8 decode these will be readable
	if m := sixtyNineShubaBookInfoJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Title = m[1]
	}
	if m := sixtyNineShubaAuthorJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Author = m[1]
	}

	if info.Title == "" {
		if content, exists := doc.Find("meta[property='og:novel:book_name']").Attr("content"); exists {
			info.Title = content
		}
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find(".booknav2 h1").Text())
	}
	if info.Author == "" {
		if content, exists := doc.Find("meta[property='og:novel:author']").Attr("content"); exists {
			info.Author = content
		}
	}
	if info.Author == "" {
		authorText := strings.TrimSpace(doc.Find(".booknav2 p").First().Text())
		info.Author = strings.TrimPrefix(authorText, "作者：")
	}

	if content, exists := doc.Find("meta[property='og:description']").Attr("content"); exists {
		info.Description = content
	}
	if info.Description == "" {
		info.Description = strings.TrimSpace(doc.Find("meta[name='description']").AttrOr("content", ""))
	}

	if content, exists := doc.Find("meta[property='og:image']").Attr("content"); exists {
		info.CoverURL = content
	}

	// Build catalog URL directly from the book ID — the catalog page at
	// /book/{id}/ contains the full chapter list but requires a logged-in
	// session (or browser-proxy access to bypass Cloudflare).
	bookID := extract69ShubaBookID(u)
	if bookID == "" {
		if m := sixtyNineShubaArticleIDRe.FindStringSubmatch(htmlStr); len(m) > 1 {
			bookID = m[1]
		}
	}

	if bookID != "" {
		catalogURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
		slog.Info("69shuba: trying catalog", "url", catalogURL)

		chapters, err := s.fetchChapterList(ctx, client, catalogURL)
		if err == nil && len(chapters) >= minimumChapters {
			info.Chapters = chapters
			slog.Info("69shuba: catalog fetch succeeded", "chapters", len(chapters))
			return info, nil
		}
		if err != nil {
			slog.Info("69shuba: catalog fetch failed (will try fallback)", "error", err)
		} else {
			slog.Info("69shuba: catalog returned too few chapters", "got", len(chapters), "need", minimumChapters)
		}
	}

	// Fallback: extract chapters from the info page (only shows ~5 recent ones)
	info.Chapters = s.extractChaptersFromInfoPage(doc)

	// If the fallback also gave us too few, return an error so the caller
	// can switch to the browser proxy (which has login cookies).
	if len(info.Chapters) < minimumChapters {
		return nil, fmt.Errorf("69shuba: only got %d/%d chapters via direct HTTP (needs browser proxy for full catalog)",
			len(info.Chapters), minimumChapters)
	}

	return info, nil
}

func (s *sixtyNineShuba) getInfoFromChapter(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	raw, err := client.Fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	raw = DecodeHTMLBody(raw)
	htmlStr := string(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	info := &NovelInfo{
		SourceURL: u,
	}

	if m := sixtyNineShubaBookInfoJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Title = m[1]
	}
	if m := sixtyNineShubaAuthorJSRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		info.Author = m[1]
	}

	bookID := extract69ShubaBookID(u)
	if bookID != "" {
		infoURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
		chapters, err := s.fetchChapterList(ctx, client, infoURL)
		if err == nil && len(chapters) >= minimumChapters {
			info.Chapters = chapters
			return info, nil
		}
	}

	// Fallback from the chapter page itself
	info.Chapters = s.extractChaptersFromInfoPage(doc)

	if len(info.Chapters) < minimumChapters {
		return nil, fmt.Errorf("69shuba: only got %d/%d chapters via direct HTTP (needs browser proxy)",
			len(info.Chapters), minimumChapters)
	}

	return info, nil
}

func (s *sixtyNineShuba) extractChaptersFromInfoPage(doc *goquery.Document) []ChapterURL {
	var chapters []ChapterURL

	doc.Find(".qustime ul li a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		title := strings.TrimSpace(sel.Find("span").Text())
		if title == "" {
			title = strings.TrimSpace(sel.Text())
		}

		if date := sel.Find("small").Text(); date != "" {
			title = strings.Replace(title, date, "", 1)
			title = strings.TrimSpace(title)
		}

		if !strings.HasPrefix(href, "http") {
			href = sixtyNineShubaBaseURL + href
		}

		href = ensureChapterExtension(href)

		chapters = append(chapters, ChapterURL{
			Title: title,
			URL:   href,
		})
	})

	return chapters
}

func (s *sixtyNineShuba) fetchChapterList(ctx context.Context, client HTTPClient, infoURL string) ([]ChapterURL, error) {
	raw, err := client.Fetch(ctx, infoURL)
	if err != nil {
		slog.Info("69shuba: fetchChapterList fetch failed", "url", infoURL, "error", err)
		return nil, err
	}

	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse catalog: %w", err)
	}

	slog.Info("69shuba: fetchChapterList got document", "url", infoURL)

	// Check for 404-like page (site returns 200 but shows "页面目不存在或删除" title)
	pageText := strings.TrimSpace(doc.Find("title").Text())
	if strings.Contains(pageText, "404") || strings.Contains(pageText, "页面目不存在") || strings.Contains(pageText, "页面不存在") {
		return nil, fmt.Errorf("69shuba: catalog page not found (may need login)")
	}

	var chapters []ChapterURL

	// Try multiple selectors for chapter list
	selectors := []string{
		"#catalog ul li a",
		"div.catalog ul li a",
		"ul.chapter-list li a",
		".listmain li a",
		"#list li a",
		".booklist li a",
		".volume li a",
		".qustime li a",
	}

	for _, sel := range selectors {
		doc.Find(sel).Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}

			title := strings.TrimSpace(s.Text())
			if !strings.HasPrefix(href, "http") {
				href = sixtyNineShubaBaseURL + href
			}

			href = ensureChapterExtension(href)

			chapters = append(chapters, ChapterURL{
				Title: title,
				URL:   href,
			})
		})
		if len(chapters) > 0 {
			slog.Info("69shuba: found chapters with selector", "selector", sel, "count", len(chapters))
			break
		}
	}

	// Fallback: find all links that look like chapter URLs
	if len(chapters) == 0 {
		slog.Info("69shuba: no chapters found with selectors, trying fallback")
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}
			title := strings.TrimSpace(s.Text())
			if title == "" {
				return
			}
			if strings.Contains(href, "/txt/") || strings.Contains(href, "/chapter/") || strings.Contains(href, "/read/") {
				if !strings.HasPrefix(href, "http") {
					href = sixtyNineShubaBaseURL + href
				}
				href = ensureChapterExtension(href)
				chapters = append(chapters, ChapterURL{
					Title: title,
					URL:   href,
				})
			}
		})
		if len(chapters) > 0 {
			slog.Info("69shuba: found chapters with fallback", "count", len(chapters))
		}
	}

	// 69shuba lists chapters in reverse order (newest first).
	// Reverse to chronological order (oldest first).
	slices.Reverse(chapters)

	return chapters, nil
}

func extract69ShubaBookID(u string) string {
	if m := sixtyNineShubaChapsRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	if m := sixtyNineShubaInfoRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func (s *sixtyNineShuba) getWordCount(html string) int {
	if m := sixtyNineShubaWordCountRe.FindStringSubmatch(html); len(m) > 1 {
		if wc, err := strconv.ParseFloat(m[1], 64); err == nil {
			return int(wc * 10000)
		}
	}
	return 0
}

func ensureChapterExtension(url string) string {
	// 69shuba recently removed the .html extension from /txt/ chapter URLs.
	// Strip it if present — the extensionless URL is the canonical form.
	if strings.Contains(url, "/txt/") {
		return strings.TrimSuffix(url, ".html")
	}
	return url
}
