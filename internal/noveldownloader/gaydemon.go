package noveldownloader

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type gaydemonParser struct{}

func NewGaydemonParser() *gaydemonParser {
	return &gaydemonParser{}
}

func (p *gaydemonParser) Name() string { return "gaydemon" }

// RequiresBrowser reports false: story pages are server-rendered and fetch
// fine with a plain HTTP GET (no Cloudflare challenge observed).
func (p *gaydemonParser) RequiresBrowser() bool { return false }

func (p *gaydemonParser) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return strings.Contains(urlStr, "gaydemon.com")
	}
	host := strings.ToLower(u.Hostname())
	if host != "gaydemon.com" && !strings.HasSuffix(host, ".gaydemon.com") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(u.Path), "/stories/")
}

func (p *gaydemonParser) GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error) {
	doc, err := client.FetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL:   url,
		Title:       p.novelTitle(doc),
		Author:      p.authorName(doc),
		Description: p.novelDescription(doc),
	}
	info.Chapters = p.extractChapters(doc, url)

	// A standalone story page without a chapter nav is a single chapter.
	if len(info.Chapters) == 0 && p.storyContent(doc) != "" {
		title := p.chapterLabel(doc)
		if title == "" {
			title = info.Title
		}
		info.Chapters = []ChapterURL{{URL: url, Title: title, Order: 1}}
	}

	return info, nil
}

func (p *gaydemonParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	return p.extractChapters(doc, url), nil
}

func (p *gaydemonParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	title := p.chapterLabel(doc)
	if title == "" {
		title = p.novelTitle(doc)
	}

	return &Chapter{
		Title:     title,
		Content:   p.storyContent(doc),
		SourceURL: chapterURL,
	}, nil
}

// novelTitle extracts the story title from the h1 heading.
func (p *gaydemonParser) novelTitle(doc *goquery.Document) string {
	if title := strings.TrimSpace(doc.Find("article#story h1[itemprop='name']").First().Text()); title != "" {
		return CleanTitle(title)
	}
	if title := strings.TrimSpace(doc.Find("h1").First().Text()); title != "" {
		return CleanTitle(title)
	}
	return CleanTitle(metaContent(doc, "meta[property='og:title']"))
}

// chapterLabel extracts the per-page chapter label ("Chapter N") shown in
// the chapter-nav toggle button. The h1 holds the story title, identical on
// every chapter page, so it cannot be used as the chapter title. The button
// embeds an <svg><title>Show all chapters</title></svg> whose text is
// stripped on a clone so it does not leak into the label.
func (p *gaydemonParser) chapterLabel(doc *goquery.Document) string {
	btn := doc.Find("#chapters button.story-page-chapt").First()
	if btn.Length() == 0 {
		return ""
	}
	label := btn.Clone()
	label.Find("svg").Remove()
	return CleanTitle(label.Text())
}

// authorName extracts the author's display name.
func (p *gaydemonParser) authorName(doc *goquery.Document) string {
	if author := strings.TrimSpace(doc.Find("a.story-page-author").First().Text()); author != "" {
		return author
	}
	if author := metaContent(doc, "meta[name='author']"); author != "" {
		return author
	}
	return metaContent(doc, "meta[property='article:author']")
}

// novelDescription builds the description from the header standfirst
// (outside the story body), enriched with the story tags.
func (p *gaydemonParser) novelDescription(doc *goquery.Document) string {
	desc := strings.TrimSpace(doc.Find("header.story-page p.textify").First().Text())
	if desc == "" {
		desc = metaContent(doc, "meta[name='description']")
	}
	if desc == "" {
		desc = metaContent(doc, "meta[property='og:description']")
	}

	var tags []string
	doc.Find("section.tags a.story-page-tag").Each(func(_ int, s *goquery.Selection) {
		if tag := strings.TrimSpace(s.Text()); tag != "" {
			tags = append(tags, tag)
		}
	})
	if len(tags) > 0 {
		desc += "\n\nTags: " + strings.Join(tags, ", ")
	}

	return strings.TrimSpace(desc)
}

// extractChapters collects the chapter links from the chapter nav. The
// current page is a <span> (not a link), so it resolves to the page URL.
func (p *gaydemonParser) extractChapters(doc *goquery.Document, pageURL string) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)
	doc.Find("nav#chapter-nav ul li").Each(func(_ int, li *goquery.Selection) {
		if a := li.Find("a").First(); a.Length() > 0 {
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
				Title: CleanTitle(strings.TrimSpace(a.Text())),
				Order: len(chapters) + 1,
			})
			return
		}
		if span := li.Find("span").First(); span.Length() > 0 {
			if seen[pageURL] {
				return
			}
			seen[pageURL] = true
			chapters = append(chapters, ChapterURL{
				URL:   pageURL,
				Title: CleanTitle(strings.TrimSpace(span.Text())),
				Order: len(chapters) + 1,
			})
		}
	})
	return chapters
}

// storyContent extracts the story body preserving document order. Besides
// <p> paragraphs it keeps <h1>-<h6> POV/scene headings (e.g. the
// "Julian"/"Alexis" <h4> markers gaydemon stories use to switch narrators),
// <blockquote>/<li> text as paragraphs, and <hr> scene breaks. Skips the
// trailing author-contact boilerplate ("To get in touch with the author…").
func (p *gaydemonParser) storyContent(doc *goquery.Document) string {
	contentSel := doc.Find("div.story-text[itemprop='articleBody']")
	if contentSel.Length() == 0 {
		contentSel = doc.Find("div.textify.story-text")
	}
	if contentSel.Length() == 0 {
		return ""
	}
	contentSel.Find("script, style, noscript, iframe, nav, header, footer").Remove()
	var parts []string
	contentSel.Children().Each(func(_ int, s *goquery.Selection) {
		tag := goquery.NodeName(s)
		switch tag {
		case "hr":
			parts = append(parts, "<hr>")
		case "h1", "h2", "h3", "h4", "h5", "h6":
			text := strings.TrimSpace(s.Text())
			if text == "" {
				return
			}
			parts = append(parts, "<"+tag+">"+text+"</"+tag+">")
		case "p", "blockquote", "li", "pre":
			text := strings.TrimSpace(s.Text())
			if text == "" || strings.HasPrefix(text, "To get in touch with the author") {
				return
			}
			parts = append(parts, "<p>"+text+"</p>")
		case "ul", "ol":
			s.Find("li").Each(func(_ int, li *goquery.Selection) {
				text := strings.TrimSpace(li.Text())
				if text != "" {
					parts = append(parts, "<p>"+text+"</p>")
				}
			})
		case "div", "section", "article":
			// Wrapper without its own text: unwrap nested blocks in order.
			if s.Find("p, h1, h2, h3, h4, h5, h6, blockquote, li").Length() > 0 {
				s.Find("p, h1, h2, h3, h4, h5, h6, blockquote, li").Each(func(_ int, inner *goquery.Selection) {
					innerTag := goquery.NodeName(inner)
					text := strings.TrimSpace(inner.Text())
					if text == "" || strings.HasPrefix(text, "To get in touch with the author") {
						return
					}
					switch innerTag {
					case "h1", "h2", "h3", "h4", "h5", "h6":
						parts = append(parts, "<"+innerTag+">"+text+"</"+innerTag+">")
					default:
						parts = append(parts, "<p>"+text+"</p>")
					}
				})
				return
			}
			if text := strings.TrimSpace(s.Text()); text != "" && !strings.HasPrefix(text, "To get in touch with the author") {
				parts = append(parts, "<p>"+text+"</p>")
			}
		}
	})
	if len(parts) == 0 {
		// Fallback for markup without element children (bare text nodes).
		if text := strings.TrimSpace(contentSel.Text()); text != "" {
				for _, line := range strings.Split(text, "\n") {
					if line = strings.TrimSpace(line); line != "" {
						parts = append(parts, "<p>"+line+"</p>")
					}
				}
		}
	}
	return strings.Join(parts, "\n")
}

// Ensure interface compliance at compile time.
var _ Parser = (*gaydemonParser)(nil)
