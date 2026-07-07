package noveldownloader

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

var (
	floraeGardenChapterRe = regexp.MustCompile(`floraegarden\.com/story/[a-z0-9-]+/[a-z0-9-]+/?$`)
	floraeGardenBaseURL   = "https://floraegarden.com"
)

type floraegardenParser struct{}

func NewFloraeGardenParser() *floraegardenParser {
	return &floraegardenParser{}
}

func (p *floraegardenParser) Name() string { return "floraegarden" }

func (p *floraegardenParser) CanHandle(urlStr string) bool {
	return strings.Contains(urlStr, "floraegarden.com")
}

func (p *floraegardenParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
	raw, err := client.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL: url,
	}

	// Title: prefer og:title, fall back to h1.story__identity-title
	if content, exists := doc.Find("meta[property='og:title']").Attr("content"); exists {
		info.Title = strings.TrimSpace(content)
	}
	if info.Title == "" {
		if t := doc.Find("h1.story__identity-title").Text(); t != "" {
			info.Title = strings.TrimSpace(t)
		}
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Author: extract from article:author meta tag or chapter__author
	if authorURL, exists := doc.Find("meta[property='article:author']").Attr("content"); exists {
		// URL format: https://floraegarden.com/author/maskedreplicant8922/
		if idx := strings.LastIndex(authorURL, "/author/"); idx != -1 {
			slug := authorURL[idx+8:]
			slug = strings.TrimSuffix(slug, "/")
			info.Author = titleCase(slug)
		}
	}
	if info.Author == "" {
		// Try chapter__author element
		authorText := doc.Find("em.chapter__author").Text()
		authorText = strings.TrimSpace(authorText)
		authorText = strings.TrimPrefix(authorText, "by ")
		if authorText != "" {
			info.Author = authorText
		}
	}

	// Description: prefer the full story__summary section
	if summarySel := doc.Find("section.story__summary"); summarySel.Length() > 0 {
		var descParts []string
		summarySel.Find("p").Each(func(_ int, p *goquery.Selection) {
			text := strings.TrimSpace(p.Text())
			if text != "" {
				descParts = append(descParts, text)
			}
		})
		info.Description = strings.Join(descParts, "\n\n")
	}
	if info.Description == "" {
		if content, exists := doc.Find("meta[property='og:description']").Attr("content"); exists {
			info.Description = strings.TrimSpace(content)
		}
	}
	if info.Description == "" {
		if content, exists := doc.Find("meta[name='description']").Attr("content"); exists {
			info.Description = strings.TrimSpace(content)
		}
	}

	// Cover image: try multiple sources
	if content, exists := doc.Find("meta[property='og:image']").Attr("content"); exists && content != "" {
		info.CoverURL = content
	}
	if info.CoverURL == "" {
		if content, exists := doc.Find("meta[name='twitter:image']").Attr("content"); exists && content != "" {
			info.CoverURL = content
		}
	}
	if info.CoverURL == "" {
		// Fictioneer theme: .custom-cover-container img
		if coverImg := doc.Find(".custom-cover-container img"); coverImg.Length() > 0 {
			if src, exists := coverImg.Attr("src"); exists && src != "" {
				info.CoverURL = src
			}
		}
	}
	if info.CoverURL == "" {
		// Fallback: look for the story cover image
		doc.Find("img").Each(func(_ int, s *goquery.Selection) {
			if info.CoverURL != "" {
				return
			}
			src, exists := s.Attr("src")
			if !exists {
				src, _ = s.Attr("data-src")
			}
			if src == "" {
				return
			}
			if strings.Contains(src, "wp-content/uploads") &&
				(strings.HasSuffix(src, ".webp") || strings.HasSuffix(src, ".jpg") || strings.HasSuffix(src, ".jpeg") || strings.HasSuffix(src, ".png")) {
				info.CoverURL = src
			}
		})
	}

	// Chapters from the story page
	info.Chapters = p.extractChaptersFromPage(doc)

	// Fallback: RSS feed if no chapters found on page
	if len(info.Chapters) == 0 {
		if storyID := p.extractStoryID(doc); storyID != "" {
			chapters, err := p.fetchChaptersFromRSS(ctx, client, storyID)
			if err == nil && len(chapters) > 0 {
				info.Chapters = chapters
			}
		}
	}

	return info, nil
}

func (p *floraegardenParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	chapters := p.extractChaptersFromPage(doc)
	if len(chapters) > 0 {
		return chapters, nil
	}
	if storyID := p.extractStoryID(doc); storyID != "" {
		return p.fetchChaptersFromRSS(ctx, client, storyID)
	}
	return nil, nil
}

func (p *floraegardenParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	raw, err := client.Fetch(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}

	// Title
	title := strings.TrimSpace(doc.Find("h1.chapter__title").Text())
	if title == "" {
		if content, exists := doc.Find("meta[property='og:title']").Attr("content"); exists {
			title = strings.TrimSpace(content)
		}
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Content: find the chapter content section
	contentSel := doc.Find("#chapter-content")
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".chapter__content")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("section.chapter__content")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("[data-fictioneer-chapter-target='content']")
	}

	// Remove non-content elements
	contentSel.Find("script, style, noscript, iframe, nav, header, footer").Remove()
	contentSel.Find(".chapter-group__list-item-checkmark, .only-logged-in").Remove()
	contentSel.Find("[style*='display:none'], [style*='display: none']").Remove()

	// Extract paragraphs - each <p> tag is a paragraph.
	// Wrap each in <p> tags so md.ConvertString preserves line breaks.
	var contentParts []string
	contentSel.Find("p").Each(func(_ int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if text != "" {
			contentParts = append(contentParts, "<p>"+text+"</p>")
		}
	})

	// If no paragraphs, try extracting from divs with specific classes
	if len(contentParts) == 0 {
		contentSel.Find("div").Each(func(_ int, sel *goquery.Selection) {
			if sel.Parent().Is("section, article, .chapter__content") {
				text := strings.TrimSpace(sel.Text())
				if text != "" {
					contentParts = append(contentParts, "<p>"+text+"</p>")
				}
			}
		})
	}

	// Last resort: get all text and try to split by newlines
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

func (p *floraegardenParser) extractChaptersFromPage(doc *goquery.Document) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)

	// Look for chapter links using the Fictioneer theme's structure
	doc.Find("li.chapter-group__list-item").Each(func(_ int, li *goquery.Selection) {
		link := li.Find("a.chapter-group__list-item-link")
		if link.Length() == 0 {
			return
		}

		href, exists := link.Attr("href")
		if !exists {
			return
		}

		// Normalize URL
		if !strings.HasPrefix(href, "http") {
			href = floraeGardenBaseURL + href
		}
		href = strings.TrimSuffix(href, "/")

		if seen[href] {
			return
		}
		seen[href] = true

		title := strings.TrimSpace(link.Text())

		chapters = append(chapters, ChapterURL{
			Title: title,
			URL:   href,
		})
	})

	// Fallback: look for any chapter-style links
	if len(chapters) == 0 {
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			href, exists := a.Attr("href")
			if !exists {
				return
			}
			if !floraeGardenChapterRe.MatchString(href) {
				return
			}
			if !strings.HasPrefix(href, "http") {
				href = floraeGardenBaseURL + href
			}
			href = strings.TrimSuffix(href, "/")
			if seen[href] {
				return
			}
			seen[href] = true

			title := strings.TrimSpace(a.Text())
			chapters = append(chapters, ChapterURL{
				Title: title,
				URL:   href,
			})
		})
	}

	return chapters
}

func (p *floraegardenParser) extractStoryID(doc *goquery.Document) string {
	// Look in body data-story-id attribute
	if storyID, exists := doc.Find("body").Attr("data-story-id"); exists {
		return storyID
	}

	// Look in RSS feed link
	var storyID string
	doc.Find("link[type='application/rss+xml']").Each(func(_ int, s *goquery.Selection) {
		if storyID != "" {
			return
		}
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		if idx := strings.Index(href, "story_id="); idx != -1 {
			storyID = href[idx+9:]
			// Remove any trailing parameters
			if ampIdx := strings.Index(storyID, "&"); ampIdx != -1 {
				storyID = storyID[:ampIdx]
			}
		}
	})

	// Look in data attributes
	if storyID == "" {
		doc.Find("[data-story-id]").Each(func(_ int, s *goquery.Selection) {
			if storyID != "" {
				return
			}
			if id, exists := s.Attr("data-story-id"); exists {
				storyID = id
			}
		})
	}

	return storyID
}

func (p *floraegardenParser) fetchChaptersFromRSS(ctx context.Context, client HTTPClient, storyID string) ([]ChapterURL, error) {
	rssURL := floraeGardenBaseURL + "/feed/rss-chapters?story_id=" + storyID
	raw, err := client.Fetch(ctx, rssURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}

	var chapters []ChapterURL

	doc.Find("item").Each(func(_ int, item *goquery.Selection) {
		title := strings.TrimSpace(item.Find("title").Text())
		link := strings.TrimSpace(item.Find("link").Text())

		if link != "" {
			chapters = append(chapters, ChapterURL{
				Title: title,
				URL:   link,
			})
		}
	})

	return chapters, nil
}

// titleCase converts a slug like "maskedreplicant8922" to "Maskedreplicant8922"
func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
