package noveldownloader

import (
	"encoding/xml"
	"strings"
)

// rssFeed is the minimal shape of the Fictioneer chapter RSS feed. The feed is
// XML, so it must be parsed with encoding/xml — an HTML parser (goquery) treats
// <link> as a void element and drops its text content, which silently yields
// zero chapters.
type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
}

// parseRSSChapters extracts chapter URLs (and titles) from a Fictioneer
// chapter RSS feed, preserving the feed's item order.
func parseRSSChapters(raw []byte) []ChapterURL {
	var feed rssFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil
	}

	var chapters []ChapterURL
	for _, item := range feed.Items {
		link := strings.TrimSpace(item.Link)
		if link == "" {
			continue
		}
		chapters = append(chapters, ChapterURL{
			Title: strings.TrimSpace(item.Title),
			URL:   link,
		})
	}
	return chapters
}
