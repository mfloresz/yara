package noveldownloader

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// metaContent returns the trimmed value of the content attribute of the first
// element matching selector, or "" if the element or attribute is absent.
// It captures the ubiquitous doc.Find("meta[...]").Attr("content") + TrimSpace
// pattern used across parsers.
func metaContent(doc *goquery.Document, selector string) string {
	if content, exists := doc.Find(selector).Attr("content"); exists {
		return strings.TrimSpace(content)
	}
	return ""
}

// extractParagraphs collects each non-empty <p> descendant of sel, wrapping the
// text in <p>…</p> so the downstream html-to-markdown conversion preserves
// paragraph breaks. Returns nil when no paragraphs are found.
func extractParagraphs(sel *goquery.Selection) []string {
	var parts []string
	sel.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := strings.TrimSpace(p.Text())
		if text != "" {
			parts = append(parts, "<p>"+text+"</p>")
		}
	})
	return parts
}
