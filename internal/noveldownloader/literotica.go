package noveldownloader

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// literoticaStoryPathRe matches a single story page path, e.g.
// /s/boyfriend-s-family-vacation-ch-01 (optionally with a ?page=N query).
// Series pages live under /series/ and do not match.
var literoticaStoryPathRe = regexp.MustCompile(`/s/[a-z0-9-]+/?(?:\?.*)?$`)

// literoticaTitleSuffixRe strips the trailing category/site suffix that
// literotica appends to og:title values (e.g. " - Taboo/Incest - Literotica.com").
var literoticaTitleSuffixRe = regexp.MustCompile(`\s+-\s+[^-]+-\s+Literotica\.com\s*$`)

type literoticaParser struct{}

func NewLiteroticaParser() *literoticaParser {
	return &literoticaParser{}
}

func (p *literoticaParser) Name() string { return "literotica" }

func (p *literoticaParser) RequiresBrowser() bool { return false }

func (p *literoticaParser) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return strings.Contains(urlStr, "literotica.com")
	}
	host := strings.ToLower(u.Hostname())
	return host == "literotica.com" || strings.HasSuffix(host, ".literotica.com")
}

func (p *literoticaParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
	doc, err := client.FetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL: url,
		Title:     p.pageTitle(doc),
		Author:    p.authorName(doc),
		// Literotica serves a site-wide generic social image
		// (speedy.literotica.com/so/assets/social-image-*.png) that is not
		// specific to the series, so no cover URL is extracted.
		Description: p.seriesDescription(doc),
	}
	info.Chapters = p.extractChapters(doc, url)

	// A single story page used directly as a novel: treat it as one chapter.
	if len(info.Chapters) == 0 && literoticaStoryPathRe.MatchString(url) {
		info.Chapters = []ChapterURL{{URL: url, Title: info.Title, Order: 1}}
	}

	return info, nil
}

func (p *literoticaParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	return p.extractChapters(doc, url), nil
}

func (p *literoticaParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	title := p.pageTitle(doc)
	content := p.storyContent(doc)

	// Long stories are paginated; follow the "Next Page" links until the
	// last page (where the next control is a disabled span, not a link).
	visited := map[string]bool{chapterURL: true}
	current := chapterURL
	for {
		next := p.nextPageURL(doc, current)
		if next == "" || visited[next] {
			break
		}
		visited[next] = true
		doc, err = client.FetchDocument(ctx, next)
		if err != nil {
			break
		}
		if pageContent := p.storyContent(doc); pageContent != "" {
			content = strings.TrimSpace(content + "\n" + pageContent)
		}
		current = next
	}

	return &Chapter{
		Title:     title,
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// pageTitle extracts the series or story title from the h1 heading.
func (p *literoticaParser) pageTitle(doc *goquery.Document) string {
	title := strings.TrimSpace(doc.Find("h1._title_ebp5m_26").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = literoticaTitleSuffixRe.ReplaceAllString(metaContent(doc, "meta[property='og:title']"), "")
	}
	return CleanTitle(title)
}

// authorName extracts the author's display name from the author panel.
func (p *literoticaParser) authorName(doc *goquery.Document) string {
	if author := strings.TrimSpace(doc.Find("a._author__title_1wp51_48").First().Text()); author != "" {
		return author
	}
	// Fallback: parse "by <Author>" from the meta description
	// ("A 8-part Story Series by RickyWrites.").
	desc := metaContent(doc, "meta[name='description']")
	if idx := strings.LastIndex(desc, " by "); idx != -1 {
		if name := strings.TrimSuffix(strings.TrimSpace(desc[idx+4:]), "."); name != "" {
			return name
		}
	}
	return ""
}

// seriesDescription builds the novel description from the meta description,
// enriched with the series tags and the started/updated dates.
func (p *literoticaParser) seriesDescription(doc *goquery.Document) string {
	desc := metaContent(doc, "meta[name='description']")
	if desc == "" {
		desc = metaContent(doc, "meta[property='og:description']")
	}

	if keywords := metaContent(doc, "meta[name='keywords']"); keywords != "" {
		var tags []string
		for _, tag := range strings.Split(keywords, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			desc += "\n\nTags: " + strings.Join(tags, ", ")
		}
	}

	var dates []string
	doc.Find("div._date_container_1y595_1422 div._files__date_1y595_672").Each(func(_ int, s *goquery.Selection) {
		dates = append(dates, strings.TrimSpace(s.Text()))
	})
	if len(dates) > 0 {
		desc += "\n\n" + strings.Join(dates, "\n")
	}

	return strings.TrimSpace(desc)
}

// extractChapters collects the story links from the series table of contents.
func (p *literoticaParser) extractChapters(doc *goquery.Document, pageURL string) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)
	doc.Find("ul._list_qr6sx_43 li._item_qr6sx_49 a._link_qr6sx_55").Each(func(_ int, a *goquery.Selection) {
		href, exists := a.Attr("href")
		if !exists || href == "" {
			return
		}
		chapterURL := resolveURL(pageURL, href)
		if seen[chapterURL] {
			return
		}
		seen[chapterURL] = true
		chapters = append(chapters, ChapterURL{
			URL:   chapterURL,
			Title: strings.TrimSpace(a.Text()),
			Order: len(chapters) + 1,
		})
	})
	return chapters
}

// storyContent extracts the story body as <p>…</p> paragraphs.
func (p *literoticaParser) storyContent(doc *goquery.Document) string {
	contentSel := doc.Find("div._article__content_138fn_99")
	if contentSel.Length() == 0 {
		return ""
	}
	contentSel.Find("script, style, noscript, iframe, nav, header, footer").Remove()
	return strings.Join(extractParagraphs(contentSel), "\n")
}

// nextPageURL returns the resolved URL of the "Next Page" pagination link,
// or "" when the current page is the last one.
func (p *literoticaParser) nextPageURL(doc *goquery.Document, currentURL string) string {
	sel := doc.Find("a[aria-label='Next Page']")
	if sel.Length() == 0 {
		return ""
	}
	href, exists := sel.Attr("href")
	if !exists || href == "" {
		return ""
	}
	return resolveURL(currentURL, href)
}

// Ensure interface compliance at compile time.
var _ Parser = (*literoticaParser)(nil)
