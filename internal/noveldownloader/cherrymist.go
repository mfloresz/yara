package noveldownloader

import (
	"context"
	"encoding/base64"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	cherryMistChapterRe = regexp.MustCompile(`cherrymist\.cafe/story/[a-z0-9-]+/[a-z0-9-]+/?$`)
	cherryMistBaseURL   = "https://cherrymist.cafe"
)

type cherrymistParser struct{}

func NewCherryMistParser() *cherrymistParser {
	return &cherrymistParser{}
}

func (p *cherrymistParser) Name() string { return "cherrymist" }

func (p *cherrymistParser) CanHandle(urlStr string) bool {
	return strings.Contains(urlStr, "cherrymist.cafe")
}

func (p *cherrymistParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
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

	// Author: extract from the identity meta or JSON-LD
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		if info.Author != "" {
			return
		}
		text := s.Text()
		// Look for "author":{"name":"..."}
		if idx := strings.Index(text, `"author"`); idx != -1 {
			if nameIdx := strings.Index(text[idx:], `"name":"`); nameIdx != -1 {
				start := idx + nameIdx + 8
				if end := strings.Index(text[start:], `"`); end != -1 {
					info.Author = text[start : start+end]
				}
			}
		}
	})
	if info.Author == "" {
		// Fallback: chapter__author element
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

	// Cover image
	info.CoverURL = metaContent(doc, "meta[property='og:image']")
	if info.CoverURL == "" {
		if coverImg := doc.Find("figure.story__thumbnail a[data-lightbox]"); coverImg.Length() > 0 {
			if href, exists := coverImg.Attr("href"); exists && href != "" {
				info.CoverURL = href
			}
		}
	}

	// Chapters from the story page
	info.Chapters = fictioneerExtractChapters(doc, cherryMistBaseURL, cherryMistChapterRe, true)

	// Fallback: RSS feed if no chapters found on page
	if len(info.Chapters) == 0 {
		if storyID := fictioneerStoryID(doc); storyID != "" {
			chapters, err := fictioneerFetchChaptersFromRSS(ctx, client, cherryMistBaseURL, storyID)
			if err == nil && len(chapters) > 0 {
				info.Chapters = chapters
			}
		}
	}

	return info, nil
}

func (p *cherrymistParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	chapters := fictioneerExtractChapters(doc, cherryMistBaseURL, cherryMistChapterRe, true)
	if len(chapters) > 0 {
		return chapters, nil
	}
	if storyID := fictioneerStoryID(doc); storyID != "" {
		return fictioneerFetchChaptersFromRSS(ctx, client, cherryMistBaseURL, storyID)
	}
	return nil, nil
}

func (p *cherrymistParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
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

	// Content: try to decrypt the obfuscated content blob first
	content := p.decryptContent(doc)

	// Fallback: plain HTML content (in case the site stops encrypting)
	if content == "" {
		contentSel := doc.Find("#chapter-content")
		if contentSel.Length() == 0 {
			contentSel = doc.Find(".chapter__content")
		}
		if contentSel.Length() == 0 {
			contentSel = doc.Find("[data-fictioneer-chapter-target='content']")
		}

		contentSel.Find("script, style, noscript, iframe, nav, header, footer").Remove()
		contentSel.Find("[style*='display:none'], [style*='display: none']").Remove()

		content = strings.Join(extractParagraphs(contentSel), "\n")
	}

	return &Chapter{
		Title:     title,
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// decryptContent decodes the ROT13 + Base64 + URI-encoded content blob
// that cherrymist.cafe uses to obfuscate chapter text.
func (p *cherrymistParser) decryptContent(doc *goquery.Document) string {
	// Find the script tag with data-poly and data-total attributes
	var scriptSel *goquery.Selection
	doc.Find("script[type='application/json']").Each(func(_ int, s *goquery.Selection) {
		if scriptSel != nil {
			return
		}
		if _, exists := s.Attr("data-poly"); exists {
			scriptSel = s
		}
	})
	if scriptSel == nil {
		return ""
	}

	poly, _ := scriptSel.Attr("data-poly")
	totalStr, _ := scriptSel.Attr("data-total")
	if poly == "" || totalStr == "" {
		return ""
	}

	total := 0
	for _, c := range totalStr {
		if c >= '0' && c <= '9' {
			total = total*10 + int(c-'0')
		}
	}
	if total == 0 {
		return ""
	}

	// Concatenate data-{poly}-{i} chunks
	var concatenated strings.Builder
	for i := 0; i < total; i++ {
		chunk, _ := scriptSel.Attr("data-" + poly + "-" + strconv.Itoa(i))
		concatenated.WriteString(chunk)
	}
	encoded := concatenated.String()
	if encoded == "" {
		return ""
	}

	// ROT13 decode
	rot13 := rot13Decode(encoded)

	// Base64 decode
	decoded, err := base64.StdEncoding.DecodeString(rot13)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.RawStdEncoding.DecodeString(rot13)
		if err != nil {
			return ""
		}
	}

	// URI decode
	decodedStr, err := url.QueryUnescape(string(decoded))
	if err != nil {
		decodedStr = string(decoded)
	}

	// Keep the HTML entities escaped. The decoded blob is already valid
	// HTML where in-text angle brackets are stored as &lt;…&gt; while
	// structural tags remain real. Feeding it escaped to the downstream
	// html-to-markdown converter lets its HTML parser treat &lt;…&gt; as
	// literal text instead of stripping it as an unknown tag.
	return decodedStr
}

// rot13Decode applies ROT13 to alphabetic characters.
func rot13Decode(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			sb.WriteRune((r-'A'+13)%26 + 'A')
		case r >= 'a' && r <= 'z':
			sb.WriteRune((r-'a'+13)%26 + 'a')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
