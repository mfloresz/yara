package noveldownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// chrysanthemumgardenParser downloads novels from chrysanthemumgarden.com,
// a WordPress site whose chapter bodies are guarded by the
// "cg-scrape-protection" plugin:
//
//   - Random junk spans / paragraphs / headings are hidden with inline
//     "height:1px … overflow:hidden" styles. They carry random alphanumeric
//     noise ("p9U6dX") or a watermark ("Story translated by Chrysanthemum
//     Garden.") and must be dropped.
//   - Real prose paragraphs are wrapped in spans rendered with a per-page
//     obfuscation font: a 52-letter subset of Open Sans with permuted glyph
//     assignments, served from …/cg-scrape-protection/resources/fonts/used/.
//     The span text is decoded via decodeCGFontMapping (see cgfont.go),
//     which matches each glyph's bounding box + advance width against the
//     embedded Open Sans reference table.
type chrysanthemumgardenParser struct{}

func NewChrysanthemumGardenParser() *chrysanthemumgardenParser {
	return &chrysanthemumgardenParser{}
}

func (p *chrysanthemumgardenParser) Name() string { return "chrysanthemumgarden" }

// RequiresBrowser: plain HTTP fetches return 200 with the full HTML; no
// Cloudflare challenge or JS rendering stands in the way.
func (p *chrysanthemumgardenParser) RequiresBrowser() bool { return false }

func (p *chrysanthemumgardenParser) CanHandle(urlStr string) bool {
	return strings.Contains(urlStr, "chrysanthemumgarden.com/novel-tl/")
}

var (
	// cgHiddenStyle matches the inline styles used for scrape-protection
	// noise: junk spans, watermark paragraphs and the hidden heading all
	// hide 1px-high overflow content.
	cgHiddenStyleRe = regexp.MustCompile(`height\s*:\s*1px`)
	// cgFontFaceRe extracts family → woff2 URL from the @font-face style
	// block the protection plugin injects into every chapter page.
	cgFontFaceRe = regexp.MustCompile(`@font-face\s*\{[^}]*?font-family\s*:\s*'([^']+)'[^}]*?url\('([^']+?\.woff2)'\)[^}]*?\}`)
	// cgFontFamilyRe pulls the family name out of an element's style attr.
	cgFontFamilyRe = regexp.MustCompile(`font-family\s*:\s*['"]?([A-Za-z0-9_-]+)`)
	// cgCreditRe parses the "Author: …" / "Translators: …" credit lines.
	cgCreditRe = regexp.MustCompile(`(?i)(Author|Translators?)\s*:\s*([^<]+)`)
)

func cgIsHiddenNoise(style string) bool {
	return cgHiddenStyleRe.MatchString(strings.ToLower(style))
}

func (p *chrysanthemumgardenParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
	doc, err := client.FetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{SourceURL: url}

	// Title: h1.novel-title minus the raw-title child span.
	if h := doc.Find("h1.novel-title").First(); h.Length() > 0 {
		info.Title = strings.TrimSpace(h.Clone().Children().Remove().End().Text())
	}
	if info.Title == "" {
		info.Title = strings.TrimSpace(doc.Find("h1.entry-title").First().Text())
	}
	if info.Title == "" {
		info.Title = strings.TrimSuffix(metaContent(doc, "meta[property='og:title']"), " - Chrysanthemum Garden")
		info.Title = strings.TrimSpace(info.Title)
	}

	// Credits: prefer the original author, fall back to the translator.
	if articleHTML, err := doc.Find("article").First().Html(); err == nil {
		author, translator := "", ""
		for _, m := range cgCreditRe.FindAllStringSubmatch(articleHTML, -1) {
			value := strings.TrimSpace(m[2])
			if strings.HasPrefix(strings.ToLower(m[1]), "author") {
				if author == "" {
					author = value
				}
			} else if translator == "" {
				translator = value
			}
		}
		info.Author = author
		if info.Author == "" {
			info.Author = translator
		}
	}

	// Synopsis: direct-child paragraphs of .entry-content. The chapter list
	// lives in a nested div, so "> p" excludes it.
	var descParts []string
	doc.Find(".entry-content > p").Each(func(_ int, s *goquery.Selection) {
		if text := strings.TrimSpace(s.Text()); text != "" {
			descParts = append(descParts, text)
		}
	})
	info.Description = strings.Join(descParts, "\n\n")
	if info.Description == "" {
		info.Description = metaContent(doc, "meta[property='og:description']")
	}

	info.CoverURL = metaContent(doc, "meta[property='og:image']")

	chapters, err := p.extractChapters(doc, url)
	if err != nil {
		return nil, err
	}
	info.Chapters = chapters

	return info, nil
}

func (p *chrysanthemumgardenParser) extractChapters(doc *goquery.Document, pageURL string) ([]ChapterURL, error) {
	var chapters []ChapterURL
	doc.Find("a.chapter-item").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || strings.TrimSpace(href) == "" {
			return
		}
		title := strings.TrimSpace(s.Find(".chapter-item-name").Text())
		if title == "" {
			title = strings.TrimSpace(s.Text())
		}
		// The item text also carries "10 months ago • 1,337 words" details;
		// keep only the first line (the chapter name).
		if line, _, _ := strings.Cut(title, "\n"); line != "" {
			title = strings.TrimSpace(line)
		}
		if title == "" {
			return
		}
		chapters = append(chapters, ChapterURL{
			URL:   resolveURL(pageURL, strings.TrimSpace(href)),
			Title: CleanTitle(title),
			Order: len(chapters) + 1,
		})
	})
	return chapters, nil
}

func (p *chrysanthemumgardenParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	return p.extractChapters(doc, url)
}

func (p *chrysanthemumgardenParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find(".chrys-post-title .chapter-title").First().Text())
	if title == "" {
		title = metaContent(doc, "meta[property='og:title']")
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	contentSel := doc.Find("#novel-content").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".entry-content").First()
	}

	// Drop scrape-protection noise: hidden junk spans inside paragraphs,
	// watermark paragraphs and the hidden heading.
	contentSel.Find("span, p, div, h1, h2, h3, h4").Each(func(_ int, s *goquery.Selection) {
		if style, exists := s.Attr("style"); exists && cgIsHiddenNoise(style) {
			s.Remove()
		}
	})

	// Decode obfuscated spans using the page's protection fonts.
	if err := p.decodeProtectedSpans(ctx, client, doc, contentSel); err != nil {
		return nil, err
	}

	content := strings.Join(extractParagraphs(contentSel), "\n")

	return &Chapter{
		Title:     CleanTitle(title),
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// decodeProtectedSpans replaces the text of spans rendered with a
// cg-scrape-protection font by its decoded form. Fonts are downloaded once
// per chapter and cached by family name.
func (p *chrysanthemumgardenParser) decodeProtectedSpans(ctx context.Context, client HTTPClient, doc *goquery.Document, contentSel *goquery.Selection) error {
	// Collect the family → font URL map from the page's style blocks.
	fontURLs := make(map[string]string)
	doc.Find("style").Each(func(_ int, s *goquery.Selection) {
		for _, m := range cgFontFaceRe.FindAllStringSubmatch(s.Text(), -1) {
			if !strings.Contains(m[2], "cg-scrape-protection") {
				continue
			}
			fontURLs[m[1]] = m[2]
		}
	})
	if len(fontURLs) == 0 {
		return nil
	}

	mappings := make(map[string]map[rune]rune, len(fontURLs))
	var firstErr error
	contentSel.Find("span[style]").Each(func(_ int, s *goquery.Selection) {
		if firstErr != nil {
			return
		}
		style, _ := s.Attr("style")
		famMatch := cgFontFamilyRe.FindStringSubmatch(style)
		if famMatch == nil {
			return
		}
		fontURL, ok := fontURLs[famMatch[1]]
		if !ok {
			return
		}
		mapping, ok := mappings[famMatch[1]]
		if !ok {
			var err error
			mapping, err = p.fetchFontMapping(ctx, client, fontURL)
			if err != nil {
				firstErr = err
				return
			}
			mappings[famMatch[1]] = mapping
		}
		s.SetText(decodeCGText(s.Text(), mapping))
	})
	return firstErr
}

// fetchFontMapping downloads a protection font (capped at 1MB; the real
// files are ~4KB) and derives its substitution map.
func (p *chrysanthemumgardenParser) fetchFontMapping(ctx context.Context, client HTTPClient, fontURL string) (map[rune]rune, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fontURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating font request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching font: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d fetching font", resp.StatusCode)
	}
	const maxFontBytes int64 = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFontBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading font body: %w", err)
	}
	if int64(len(body)) > maxFontBytes {
		return nil, fmt.Errorf("font exceeds %d bytes", maxFontBytes)
	}
	mapping, err := decodeCGFontMapping(body)
	if err != nil {
		return nil, fmt.Errorf("decoding font mapping: %w", err)
	}
	return mapping, nil
}
