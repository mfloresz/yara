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
	doc, err := client.FetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL: url,
	}

	// Title: prefer og:title, fall back to h1.story__identity-title
	info.Title = metaContent(doc, "meta[property='og:title']")
	if info.Title == "" {
		if t := doc.Find("h1.story__identity-title").Text(); t != "" {
			info.Title = strings.TrimSpace(t)
		}
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	// Author: extract from article:author meta tag or chapter__author
	if authorURL := metaContent(doc, "meta[property='article:author']"); authorURL != "" {
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
		info.Description = metaContent(doc, "meta[property='og:description']")
	}
	if info.Description == "" {
		info.Description = metaContent(doc, "meta[name='description']")
	}

	// Cover image: try multiple sources
	info.CoverURL = metaContent(doc, "meta[property='og:image']")
	if info.CoverURL == "" {
		info.CoverURL = metaContent(doc, "meta[name='twitter:image']")
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
	info.Chapters = fictioneerExtractChapters(doc, floraeGardenBaseURL, floraeGardenChapterRe, false)

	// Fallback: RSS feed if no chapters found on page
	if len(info.Chapters) == 0 {
		if storyID := fictioneerStoryID(doc); storyID != "" {
			chapters, err := fictioneerFetchChaptersFromRSS(ctx, client, floraeGardenBaseURL, storyID)
			if err == nil && len(chapters) > 0 {
				info.Chapters = chapters
			}
		}
	}

	return info, nil
}

func (p *floraegardenParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	chapters := fictioneerExtractChapters(doc, floraeGardenBaseURL, floraeGardenChapterRe, false)
	if len(chapters) > 0 {
		return chapters, nil
	}
	if storyID := fictioneerStoryID(doc); storyID != "" {
		return fictioneerFetchChaptersFromRSS(ctx, client, floraeGardenBaseURL, storyID)
	}
	return nil, nil
}

func (p *floraegardenParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	// Title
	title := strings.TrimSpace(doc.Find("h1.chapter__title").Text())
	if title == "" {
		title = metaContent(doc, "meta[property='og:title']")
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
	contentParts := extractParagraphs(contentSel)

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

// titleCase converts a slug like "maskedreplicant8922" to "Maskedreplicant8922"
func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
