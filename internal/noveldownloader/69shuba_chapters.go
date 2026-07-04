package noveldownloader

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// GetChapters returns chapter URLs by fetching the catalog page at /book/{id}/.
func (s *sixtyNineShuba) GetChapters(ctx context.Context, client HTTPClient, novelURL string) ([]ChapterURL, error) {
	bookID := extract69ShubaBookID(novelURL)
	if bookID == "" {
		return nil, fmt.Errorf("69shuba: cannot extract book ID from %s", novelURL)
	}

	infoURL := fmt.Sprintf("%s/book/%s/", sixtyNineShubaBaseURL, bookID)
	return s.fetchChapterList(ctx, client, infoURL)
}

func (s *sixtyNineShuba) getChapterContent(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	raw, err := client.Fetch(ctx, chapterURL)
	if err != nil {
		return nil, fmt.Errorf("69shuba fetch: %w", err)
	}

	// Decode GBK to UTF-8 (the site serves <meta charset="gbk">)
	raw = DecodeHTMLBody(raw)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("69shuba parse: %w", err)
	}

	// Find the main content container (.txtnav)
	txtNav := doc.Find(".txtnav")
	if txtNav.Length() == 0 {
		txtNav = doc.Find("#content")
	}
	if txtNav.Length() == 0 {
		return nil, fmt.Errorf("69shuba: no content found at %s", chapterURL)
	}

	// Extract chapter title from <h1> inside .txtnav
	title := strings.TrimSpace(txtNav.Find("h1").First().Text())

	// Remove non-content elements that live inside .txtnav
	txtNav.Find("h1").Remove()
	txtNav.Find("div.txtinfo").Remove()
	txtNav.Find("#txtright").Remove()
	txtNav.Find("div.txtright").Remove()

	// Remove scripts, styles, iframes, and ad containers
	txtNav.Find("script, style, noscript, iframe, ins, .ad, .ads, .advert").Remove()

	// Remove elements with display:none
	txtNav.Find("*").Each(func(_ int, s *goquery.Selection) {
		if style, exists := s.Attr("style"); exists {
			if strings.Contains(strings.ToLower(style), "display:none") || strings.Contains(strings.ToLower(style), "display: none") {
				s.Remove()
			}
		}
	})

	// Get the cleaned HTML — let the markdown converter handle paragraph formatting
	html, err := txtNav.Html()
	if err != nil {
		return nil, fmt.Errorf("69shuba: failed to get HTML: %w", err)
	}

	// Clean up artifacts: em-space indentation (U+2003) used by 69shuba for paragraph indents
	html = strings.ReplaceAll(html, "\u2003", " ")

	content := strings.TrimSpace(html)
	if content == "" {
		return nil, fmt.Errorf("69shuba: empty content at %s", chapterURL)
	}

	return &Chapter{
		Title:     title,
		SourceURL: chapterURL,
		Content:   content,
	}, nil
}
