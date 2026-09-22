package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// InkspiredParser downloads from getinkspired.com.
//
// The site sits behind Cloudflare (a plain HTTP client gets 403), so pages
// arrive through the browser worker via LazyFallbackClient. Everything the
// parser needs is then server-rendered as JSON-LD plus plain HTML:
//
//	story page    -> {"@type":"Book", name, author.name, description, url,
//	                 hasPart:[{@type:"Chapter", name, url}]}
//	chapter page  -> {"@type":"Chapter", partOf:{...Book}, url}
//	bodies        -> #chapter_block > div[id^="chapter-scroll-content-"] with one
//	                 div.paragraph per paragraph, each wrapping a single <p>
//
// The <p> elements keep their inline markup (<u>, <strong>, <em>), which the
// downloader's html-to-markdown conversion turns into markdown emphasis.
type InkspiredParser struct{}

func NewInkspiredParser() *InkspiredParser {
	return &InkspiredParser{}
}

func (p *InkspiredParser) Name() string { return "inkspired" }

// RequiresBrowser reports whether fetching needs the browser worker proxy.
// getinkspired.com answers 403 to plain HTTP requests (verified with curl),
// so only a real browser session clears the Cloudflare challenge.
func (p *InkspiredParser) RequiresBrowser() bool { return true }

func (p *InkspiredParser) CanHandle(urlStr string) bool {
	_, _, ok := inkspiredParseURL(urlStr)
	return ok
}

// inkspiredChapterPathRe matches a reading page /{lang}/story/{id}/chapter/{slug}/
// where the slug ends in the numeric chapter id, e.g.
// /es/story/746625/chapter/capitulo-1-huracan-2647835/.
var inkspiredChapterPathRe = regexp.MustCompile(`^/(?:[a-z]{2}/)?story/(\d+)/chapter/[^/]+/?$`)

// inkspiredStoryPathRe matches /{lang}/story/{id}/{slug}/. The language
// segment is optional because the site's own share links drop it
// (https://getinkspired.com/story/746625/...).
var inkspiredStoryPathRe = regexp.MustCompile(`^/(?:[a-z]{2}/)?story/(\d+)(?:/[^/]+)?/?$`)

// inkspiredURLKind classifies a getinkspired.com URL as a story or chapter page.
type inkspiredURLKind int

const (
	inkspiredURLInvalid inkspiredURLKind = iota
	inkspiredURLStory
	inkspiredURLChapter
)

// inkspiredParseURL parses a getinkspired.com URL, returning the story id, the
// page kind, and whether the URL belongs to the site at all.
func inkspiredParseURL(rawURL string) (string, inkspiredURLKind, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", inkspiredURLInvalid, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "getinkspired.com" {
		return "", inkspiredURLInvalid, false
	}
	if m := inkspiredChapterPathRe.FindStringSubmatch(u.Path); m != nil {
		return m[1], inkspiredURLChapter, true
	}
	// A /chapter/ segment with no chapter slug is a malformed reading URL, not
	// a story page whose slug happens to be "chapter".
	if strings.Contains(u.Path, "/chapter/") {
		return "", inkspiredURLInvalid, false
	}
	if m := inkspiredStoryPathRe.FindStringSubmatch(u.Path); m != nil {
		return m[1], inkspiredURLStory, true
	}
	return "", inkspiredURLInvalid, false
}

// inkspiredBookLD maps the {"@type":"Book"} block of a story page.
type inkspiredBookLD struct {
	Type        string             `json:"@type"`
	Name        string             `json:"name"`
	URL         string             `json:"url"`
	Description string             `json:"description"`
	Author      *inkspiredAuthorLD `json:"author"`
	HasPart     []inkspiredPartLD  `json:"hasPart"`
}

type inkspiredAuthorLD struct {
	Name string `json:"name"`
}

type inkspiredPartLD struct {
	Type string `json:"@type"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// inkspiredChapterLD maps the {"@type":"Chapter"} block of a reading page;
// partOf points back at the parent story.
type inkspiredChapterLD struct {
	Type   string           `json:"@type"`
	PartOf *inkspiredBookLD `json:"partOf"`
}

// inkspiredFindJSONLD unmarshals the page's application/ld+json blocks into
// target for the first block whose @type matches, returning whether one was
// found. Malformed blocks are skipped rather than failing the parse, so a
// site-side schema change degrades to the HTML fallbacks instead of erroring.
func inkspiredFindJSONLD(doc *goquery.Document, jsonType string, target any) bool {
	found := false
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		if found {
			return
		}
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		var probe struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal([]byte(raw), &probe); err != nil || probe.Type != jsonType {
			return
		}
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			return
		}
		found = true
	})
	return found
}

func (p *InkspiredParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	if _, _, ok := inkspiredParseURL(pageURL); !ok {
		return nil, fmt.Errorf("invalid inkspired URL: %s", pageURL)
	}

	doc, err := client.FetchDocument(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	storyURL := pageURL
	var book inkspiredBookLD
	hasBook := inkspiredFindJSONLD(doc, "Book", &book)
	if !hasBook {
		// A reading page carries no Book block; partOf points at the story.
		var chapter inkspiredChapterLD
		if inkspiredFindJSONLD(doc, "Chapter", &chapter) && chapter.PartOf != nil && chapter.PartOf.URL != "" {
			storyURL = chapter.PartOf.URL
			doc, err = client.FetchDocument(ctx, storyURL)
			if err != nil {
				return nil, err
			}
			hasBook = inkspiredFindJSONLD(doc, "Book", &book)
		}
	}

	info := &NovelInfo{SourceURL: storyURL}
	if hasBook {
		info.SourceURL = strings.TrimSpace(book.URL)
		if info.SourceURL == "" {
			info.SourceURL = storyURL
		}
		info.Title = CleanTitle(book.Name)
		if book.Author != nil {
			info.Author = strings.TrimSpace(book.Author.Name)
		}
		info.Description = strings.TrimSpace(book.Description)
		info.Chapters = inkspiredChaptersFromBook(&book)
	}

	// HTML fallbacks: the JSON-LD block carries no cover, and a schema change
	// could leave the other fields empty even though the page renders them.
	info.CoverURL = metaContent(doc, "meta[property='og:image']")
	if info.Title == "" {
		info.Title = inkspiredHTMLTitle(doc)
	}
	if info.Author == "" {
		info.Author = strings.TrimSpace(doc.Find("a.crx-authorrow .crx-authorrow-name").First().Text())
	}
	// The rendered blurb is the full synopsis; the JSON-LD and og:description
	// copies of it are capped at 500 and ~300 characters respectively.
	// Scoped to the page header because .formating-space is also used for the
	// reader comments further down the page.
	if blurb := strings.TrimSpace(doc.Find("header span.formating-space").First().Text()); blurb != "" {
		info.Description = blurb
	}
	if info.Description == "" {
		info.Description = metaContent(doc, "meta[property='og:description']")
	}
	if len(info.Chapters) == 0 {
		info.Chapters = inkspiredChaptersFromModal(doc, info.SourceURL)
	}

	if info.Title == "" {
		return nil, fmt.Errorf("no story metadata found at %s", pageURL)
	}
	return info, nil
}

func (p *InkspiredParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, pageURL string) ([]ChapterURL, error) {
	if doc == nil {
		var err error
		doc, err = client.FetchDocument(ctx, pageURL)
		if err != nil {
			return nil, err
		}
	}
	var book inkspiredBookLD
	if inkspiredFindJSONLD(doc, "Book", &book) {
		if chapters := inkspiredChaptersFromBook(&book); len(chapters) > 0 {
			return chapters, nil
		}
	}
	return inkspiredChaptersFromModal(doc, pageURL), nil
}

func (p *InkspiredParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	if _, kind, ok := inkspiredParseURL(chapterURL); !ok || kind != inkspiredURLChapter {
		return nil, fmt.Errorf("not an inkspired chapter URL: %s", chapterURL)
	}
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}

	title := CleanTitle(doc.Find("#chapter_block h2.content_chapter_title_reader").First().Text())
	if title == "" {
		title = inkspiredChapterSlugTitle(chapterURL)
	}

	content := inkspiredChapterHTML(doc)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("inkspired chapter page has no content: %s", chapterURL)
	}
	return &Chapter{
		Title:     title,
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// inkspiredChaptersFromBook reads the ordered chapter list out of the Book
// JSON-LD. Titles there are already clean (no "1.- " position prefix).
func inkspiredChaptersFromBook(book *inkspiredBookLD) []ChapterURL {
	var chapters []ChapterURL
	for _, part := range book.HasPart {
		chapterURL := strings.TrimSpace(part.URL)
		if chapterURL == "" {
			continue
		}
		title := CleanTitle(part.Name)
		if title == "" {
			title = fmt.Sprintf("Chapter %d", len(chapters)+1)
		}
		chapters = append(chapters, ChapterURL{
			URL:   chapterURL,
			Title: title,
			Order: len(chapters) + 1,
		})
	}
	return chapters
}

// inkspiredPositionRe matches the table of contents' own "1.- " prefix, which
// duplicates the Order the parser already assigns.
var inkspiredPositionRe = regexp.MustCompile(`^\d+\.\s*-\s*`)

// inkspiredChaptersFromModal reads the story's table of contents, which the
// story page renders inside #chapterModal next to the "N CAPÍTULOS" link.
func inkspiredChaptersFromModal(doc *goquery.Document, pageURL string) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)
	doc.Find("#chapterModal a.reader-chapter-link").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if href == "" {
			return
		}
		chapterURL := resolveURL(pageURL, href)
		if seen[chapterURL] {
			return
		}
		seen[chapterURL] = true

		title := inkspiredPositionRe.ReplaceAllString(CleanTitle(a.Text()), "")
		if title == "" {
			title = fmt.Sprintf("Chapter %d", len(chapters)+1)
		}
		chapters = append(chapters, ChapterURL{
			URL:   chapterURL,
			Title: CleanTitle(title),
			Order: len(chapters) + 1,
		})
	})
	return chapters
}

// inkspiredChapterHTML extracts the chapter body as <p>…</p> fragments.
// Selecting per-paragraph keeps the comment bubbles, scripts and the paginated
// reader out of the result without an explicit removal pass, because those all
// live outside div.paragraph.
func inkspiredChapterHTML(doc *goquery.Document) string {
	sel := doc.Find("#chapter_block div[id^='chapter-scroll-content-']")
	if sel.Length() == 0 {
		sel = doc.Find("div[id^='chapter-scroll-content-']")
	}
	if sel.Length() == 0 {
		return ""
	}

	var parts []string
	appendParagraph := func(html, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		parts = append(parts, "<p>"+strings.TrimSpace(html)+"</p>")
	}
	sel.Find(".paragraph").Each(func(_ int, para *goquery.Selection) {
		if paragraphs := para.Find("p"); paragraphs.Length() > 0 {
			paragraphs.Each(func(_ int, p *goquery.Selection) {
				inner, err := p.Html()
				if err != nil {
					return
				}
				appendParagraph(inner, p.Text())
			})
			return
		}
		// Some chapters separate paragraphs with <br> instead of <p>.
		inner, err := para.Html()
		if err != nil {
			return
		}
		appendParagraph(inner, para.Text())
	})
	return strings.Join(parts, "\n")
}

// inkspiredHTMLTitle pulls the story title from the page header, dropping the
// site's "Inkspired - " og:title prefix and " | Inkspired" <title> suffix.
func inkspiredHTMLTitle(doc *goquery.Document) string {
	if title := CleanTitle(doc.Find("h1.crx-h2").First().Text()); title != "" {
		return title
	}
	title := strings.TrimPrefix(metaContent(doc, "meta[property='og:title']"), "Inkspired - ")
	if title = CleanTitle(title); title != "" {
		return title
	}
	return CleanTitle(strings.TrimSuffix(CleanTitle(doc.Find("title").First().Text()), " | Inkspired"))
}

// inkspiredChapterIDRe matches the trailing "-{chapter id}" of a chapter slug.
var inkspiredChapterIDRe = regexp.MustCompile(`-\d+$`)

// inkspiredChapterSlugTitle derives a readable title from a chapter URL whose
// page rendered no heading, e.g. capitulo-1-huracan-2647835 -> "capitulo 1 huracan".
func inkspiredChapterSlugTitle(chapterURL string) string {
	u, err := url.Parse(chapterURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	slug := inkspiredChapterIDRe.ReplaceAllString(parts[len(parts)-1], "")
	return CleanTitle(strings.ReplaceAll(slug, "-", " "))
}

// Ensure interface compliance at compile time.
var _ Parser = (*InkspiredParser)(nil)
