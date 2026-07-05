package noveldownloader

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func (p *NovelfireParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter: %w", err)
	}

	// Check if we got a redirect/loading page
	if isNovelfireRedirectPage(doc) {
		fallbackURL := buildFallbackURL(chapterURL)
		if fallbackURL != "" {
			return p.ParseChapter(ctx, client, fallbackURL)
		}
	}

	title := strings.TrimSpace(doc.Find("span.chapter-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1, h2").First().Text())
	}

	contentSel := doc.Find("div.chapter-content")
	if contentSel.Length() == 0 {
		contentSel = doc.Find("div#content")
	}

	if contentSel.Length() == 0 {
		// Try fallback domain if content not found
		fallbackURL := buildFallbackURL(chapterURL)
		if fallbackURL != "" {
			return p.ParseChapter(ctx, client, fallbackURL)
		}
		return nil, fmt.Errorf("no chapter content found")
	}

	contentSel.Find("script, style, noscript").Remove()
	contentSel.Find("*").Each(func(_ int, s *goquery.Selection) {
		if style, exists := s.Attr("style"); exists {
			if strings.Contains(strings.ToLower(style), "display:none") {
				s.Remove()
			}
		}
	})

	contentSel.Find("p").Each(func(_ int, s *goquery.Selection) {
		if class, exists := s.Attr("class"); exists && class != "" {
			s.Remove()
		}
	})

	contentSel.Find("div dl dt").Each(func(_ int, s *goquery.Selection) {
		s.Remove()
	})

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
