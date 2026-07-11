package noveldownloader

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The Fictioneer WordPress theme powers several sites (cherrymist.cafe,
// floraegarden.com). They share identical chapter-list markup, story-id
// discovery, and an RSS chapter feed, so the extraction logic lives here and is
// parameterized by the site's base URL and chapter-URL matcher.

// fictioneerExtractChapters extracts chapter links from a Fictioneer story
// page. Relative hrefs are resolved against baseURL; chapterRe is the fallback
// matcher used when the themed list markup is absent. When skipPremium is true,
// list items flagged with the "_premium" class are skipped.
func fictioneerExtractChapters(doc *goquery.Document, baseURL string, chapterRe *regexp.Regexp, skipPremium bool) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)

	add := func(href, title string) {
		if !strings.HasPrefix(href, "http") {
			href = baseURL + href
		}
		href = strings.TrimSuffix(href, "/")
		if seen[href] {
			return
		}
		seen[href] = true
		chapters = append(chapters, ChapterURL{Title: strings.TrimSpace(title), URL: href})
	}

	doc.Find("li.chapter-group__list-item").Each(func(_ int, li *goquery.Selection) {
		if skipPremium && li.HasClass("_premium") {
			return
		}
		link := li.Find("a.chapter-group__list-item-link")
		if link.Length() == 0 {
			return
		}
		if href, exists := link.Attr("href"); exists {
			add(href, link.Text())
		}
	})

	// Fallback: look for any chapter-style links matching the site pattern.
	if len(chapters) == 0 {
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			href, exists := a.Attr("href")
			if !exists || !chapterRe.MatchString(href) {
				return
			}
			add(href, a.Text())
		})
	}

	return chapters
}

// fictioneerStoryID discovers the numeric story id used by the theme's RSS
// chapter feed, checking the body data attribute, any [data-story-id] element,
// then an RSS feed link's story_id query parameter.
func fictioneerStoryID(doc *goquery.Document) string {
	if storyID, exists := doc.Find("body").Attr("data-story-id"); exists {
		return storyID
	}

	var storyID string
	doc.Find("[data-story-id]").Each(func(_ int, s *goquery.Selection) {
		if storyID != "" {
			return
		}
		if id, exists := s.Attr("data-story-id"); exists {
			storyID = id
		}
	})

	if storyID == "" {
		doc.Find("link[type='application/rss+xml']").Each(func(_ int, s *goquery.Selection) {
			if storyID != "" {
				return
			}
			href, exists := s.Attr("href")
			if !exists {
				return
			}
			if idx := strings.Index(href, "story_id="); idx != -1 {
				storyID = href[idx+len("story_id="):]
				if ampIdx := strings.Index(storyID, "&"); ampIdx != -1 {
					storyID = storyID[:ampIdx]
				}
			}
		})
	}

	return storyID
}

// fictioneerFetchChaptersFromRSS fetches the theme's per-story RSS chapter feed
// and returns its items as chapter URLs.
func fictioneerFetchChaptersFromRSS(ctx context.Context, client HTTPClient, baseURL, storyID string) ([]ChapterURL, error) {
	rssURL := baseURL + "/feed/rss-chapters?story_id=" + storyID
	raw, err := client.Fetch(ctx, rssURL)
	if err != nil {
		return nil, err
	}
	// The RSS feed is XML; parse it with encoding/xml (via parseRSSChapters),
	// not goquery — an HTML parser treats <link> as a void element and drops
	// its text content, which silently yields zero chapters.
	return parseRSSChapters(raw), nil
}
