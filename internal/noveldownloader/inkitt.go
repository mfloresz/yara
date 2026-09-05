package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// InkittParser downloads from inkitt.com using the site's public JSON API
// (the same endpoints the LNReader plugin uses):
//
//   - GET /api/stories/{story_id}          -> metadata + full chapter list
//   - GET /stories/{story_id}              -> author + summary (HTML; the API
//     carries no summary field)
//   - GET /stories/{id}/chapters/{number}  -> chapter HTML (div#chapterText)
//
// All three answer anonymous requests (verified HTTP 200 with plain curl, no
// Cloudflare challenge and no login, even for 18+ stories), so no account,
// session cookie, or browser worker is needed.
type InkittParser struct{}

func NewInkittParser() *InkittParser {
	return &InkittParser{}
}

func (p *InkittParser) Name() string { return "inkitt" }

// RequiresBrowser: story metadata and the first chapters are public, but
// chapters past the free preview are served folded (an empty div#chapterText
// inside a story-page-text_folded wrapper) to anonymous readers. A logged-in
// browser session is required to unfold them, so reliable full-novel fetching
// needs the browser worker extension.
func (p *InkittParser) RequiresBrowser() bool { return true }

func (p *InkittParser) CanHandle(urlStr string) bool {
	_, _, ok := inkittParseURL(urlStr)
	return ok
}

// inkittStoryPathRe matches /stories/{id} with an optional genre segment or
// slug suffix, e.g. /stories/1579934, /stories/erotica/1579934.
var inkittStoryPathRe = regexp.MustCompile(`^/stories/(?:[a-z0-9-]+/)?(\d+)/?$`)

// inkittChapterPathRe matches a reading page /stories/{id}/chapters/{n},
// e.g. /stories/1579934/chapters/2.
var inkittChapterPathRe = regexp.MustCompile(`^/stories/\d+/chapters/(\d+)/?$`)

// inkittURLKind classifies an inkitt.com URL as a story page or a chapter page.
type inkittURLKind int

const (
	inkittURLInvalid inkittURLKind = iota
	inkittURLStory
	inkittURLChapter
)

// inkittParseURL parses an inkitt.com URL, returning the numeric story id,
// its kind (story or chapter page), and whether the URL belongs to the site.
func inkittParseURL(rawURL string) (string, inkittURLKind, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", inkittURLInvalid, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "inkitt.com" {
		return "", inkittURLInvalid, false
	}
	if m := inkittChapterPathRe.FindStringSubmatch(u.Path); m != nil {
		storyID := inkittStoryIDFromPath(u.Path)
		if storyID == "" {
			return "", inkittURLInvalid, false
		}
		return storyID, inkittURLChapter, true
	}
	if m := inkittStoryPathRe.FindStringSubmatch(u.Path); m != nil {
		return m[1], inkittURLStory, true
	}
	return "", inkittURLInvalid, false
}

// inkittStoryIDFromPath extracts the story id from a chapter path
// /stories/{id}/chapters/{n}.
func inkittStoryIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 && parts[0] == "stories" && parts[2] == "chapters" {
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return parts[1]
		}
	}
	return ""
}

// inkittChapterNumber extracts the chapter number from a chapter path,
// or 0 when the URL is not a chapter page.
func inkittChapterNumber(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	if m := inkittChapterPathRe.FindStringSubmatch(u.Path); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// IsInkittChapterURL reports whether rawURL is an inkitt.com reading page
// (/stories/{id}/chapters/{n}). Used by the fallback client to detect the
// folded-chapter gate on direct fetches.
func IsInkittChapterURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	return host == "inkitt.com" && inkittChapterPathRe.MatchString(u.Path)
}

// hasInkittFoldedChapter reports whether a chapter page body carries the
// login gate: anonymous readers past the free preview get an empty
// div#chapterText inside a story-page-text_folded wrapper instead of the
// chapter paragraphs. A logged-in browser session unfolds the content.
func hasInkittFoldedChapter(body []byte) bool {
	return strings.Contains(string(body), "story-page-text_folded")
}

// inkittStory maps GET /api/stories/{id}. Only the fields the parser needs
// are declared.
type inkittStory struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	CoverURL      string `json:"cover_url"`
	VerticalCover struct {
		URL string `json:"url"`
	} `json:"vertical_cover"`
	User struct {
		Name string `json:"name"`
	} `json:"user"`
	Chapters []inkittChapter `json:"chapters"`
}

type inkittChapter struct {
	ChapterNumber int    `json:"chapter_number"`
	Name          string `json:"name"`
}

func (p *InkittParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	story, err := p.fetchStory(ctx, client, pageURL)
	if err != nil {
		return nil, err
	}

	base := extractBaseURL(pageURL)
	storyURL := fmt.Sprintf("%s/stories/%d", base, story.ID)
	chapters := make([]ChapterURL, 0, len(story.Chapters))
	for _, ch := range story.Chapters {
		if ch.ChapterNumber <= 0 {
			continue
		}
		chapters = append(chapters, ChapterURL{
			URL:   fmt.Sprintf("%s/stories/%d/chapters/%d", base, story.ID, ch.ChapterNumber),
			Title: CleanTitle(ch.Name),
			Order: ch.ChapterNumber,
		})
	}
	// The API does not always carry chapters (e.g. drafts); fall back to the
	// chapter links rendered in the story page HTML.
	author, summary := story.User.Name, ""
	if doc, docErr := client.FetchDocument(ctx, storyURL); docErr == nil {
		if len(chapters) == 0 {
			chapters = inkittExtractChapters(doc, storyURL)
		}
		if a := inkittAuthor(doc); a != "" {
			author = a
		}
		summary = inkittSummary(doc)
	} else if len(chapters) == 0 {
		return nil, fmt.Errorf("fetching story page: %w", docErr)
	}

	cover := story.VerticalCover.URL
	if cover == "" {
		cover = story.CoverURL
	}
	return &NovelInfo{
		Title:       CleanTitle(story.Title),
		Author:      strings.TrimSpace(author),
		Description: strings.TrimSpace(summary),
		CoverURL:    cover,
		SourceURL:   storyURL,
		Chapters:    chapters,
	}, nil
}

func (p *InkittParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, pageURL string) ([]ChapterURL, error) {
	storyID, _, ok := inkittParseURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid inkitt URL: %s", pageURL)
	}
	if doc == nil {
		var err error
		doc, err = client.FetchDocument(ctx, fmt.Sprintf("%s/stories/%s", extractBaseURL(pageURL), storyID))
		if err != nil {
			return nil, err
		}
	}
	return inkittExtractChapters(doc, pageURL), nil
}

// fetchStory resolves any inkitt.com URL (story or chapter page) to the
// parent story metadata via the public JSON API.
func (p *InkittParser) fetchStory(ctx context.Context, client HTTPClient, pageURL string) (*inkittStory, error) {
	storyID, _, ok := inkittParseURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid inkitt URL: %s", pageURL)
	}
	apiURL := fmt.Sprintf("%s/api/stories/%s", extractBaseURL(pageURL), storyID)
	var story inkittStory
	if err := inkittFetchJSON(ctx, client, apiURL, &story); err != nil {
		return nil, fmt.Errorf("fetching story metadata: %w", err)
	}
	if story.Title == "" {
		return nil, fmt.Errorf("story %s not found or has no title", storyID)
	}
	return &story, nil
}

func (p *InkittParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	storyID, kind, ok := inkittParseURL(chapterURL)
	if !ok || kind != inkittURLChapter {
		return nil, fmt.Errorf("not an inkitt chapter URL: %s", chapterURL)
	}
	doc, err := client.FetchDocument(ctx, chapterURL)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(doc.Find("h2.chapter-head-title").First().Text())
	if title == "" {
		if n := inkittChapterNumber(chapterURL); n > 0 {
			title = fmt.Sprintf("Chapter %d", n)
		}
	}
	content := inkittChapterContent(doc)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("story %s chapter page is folded (only the free preview is public): connect the browser worker and log in to inkitt.com in that browser, then retry", storyID)
	}
	return &Chapter{
		Title:     CleanTitle(title),
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// inkittAuthor extracts the author's display name from the story header.
func inkittAuthor(doc *goquery.Document) string {
	if author := strings.TrimSpace(doc.Find("#storyAuthor").First().Text()); author != "" {
		return author
	}
	if author := strings.TrimSpace(doc.Find("a.author-link").First().Text()); author != "" {
		return author
	}
	return strings.TrimSpace(metaContent(doc, "meta[name='author']"))
}

// inkittSummary extracts the story blurb. The JSON API carries no summary
// field, so the HTML paragraph is the only source.
func inkittSummary(doc *goquery.Document) string {
	if summary := strings.TrimSpace(doc.Find("p.story-summary").First().Text()); summary != "" {
		return summary
	}
	return metaContent(doc, "meta[property='og:description']")
}

// inkittExtractChapters collects the chapter links rendered in the story
// page (a.chapter-link with span.chapter-title), resolving relative hrefs.
func inkittExtractChapters(doc *goquery.Document, pageURL string) []ChapterURL {
	var chapters []ChapterURL
	seen := make(map[string]bool)
	doc.Find("a.chapter-link").Each(func(_ int, a *goquery.Selection) {
		href, exists := a.Attr("href")
		if !exists || href == "" {
			return
		}
		chapterURL := resolveURL(pageURL, href)
		if seen[chapterURL] {
			return
		}
		seen[chapterURL] = true
		title := strings.TrimSpace(a.Find("span.chapter-title").Text())
		if title == "" {
			title = strings.TrimSpace(a.Text())
		}
		order := inkittChapterNumber(chapterURL)
		if order <= 0 {
			order = len(chapters) + 1
		}
		chapters = append(chapters, ChapterURL{
			URL:   chapterURL,
			Title: CleanTitle(title),
			Order: order,
		})
	})
	return chapters
}

// inkittChapterContent extracts the chapter body, preserving inline markup
// (<i>, <b>) so the downstream html-to-markdown conversion keeps emphasis.
func inkittChapterContent(doc *goquery.Document) string {
	sel := doc.Find("div#chapterText")
	if sel.Length() == 0 {
		return ""
	}
	sel.Find("script, style, noscript, iframe").Remove()
	var parts []string
	sel.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
		inner, err := paragraph.Html()
		if err != nil {
			return
		}
		if strings.TrimSpace(paragraph.Text()) == "" {
			return
		}
		parts = append(parts, "<p>"+strings.TrimSpace(inner)+"</p>")
	})
	return strings.Join(parts, "\n")
}

// inkittFetchJSON performs a GET with a JSON Accept header and unmarshals the
// response body into target.
func inkittFetchJSON(ctx context.Context, client HTTPClient, apiURL string, target any) error {
	body, err := inkittFetchBytes(ctx, client, apiURL, "application/json, */*;q=0.8")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("parsing %s: %w", apiURL, err)
	}
	return nil
}

func inkittFetchBytes(ctx context.Context, client HTTPClient, apiURL, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", accept)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// 50MB hard cap: the payload size is server-controlled, so bound it
	// the same way DownloadCover bounds untrusted cover bytes.
	const maxBodyBytes int64 = 50 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	if int64(len(raw)) > maxBodyBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxBodyBytes)
	}
	return raw, nil
}

// Ensure interface compliance at compile time.
var _ Parser = (*InkittParser)(nil)
