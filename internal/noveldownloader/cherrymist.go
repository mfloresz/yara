package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// cherrymist.cafe is a client-rendered React SPA: every route returns the same
// empty <div id="root"> shell, so there is nothing to scrape from the page HTML.
// The site is backed by a JSON API under /api, which is what this parser talks
// to. Note that the API withholds the chapter body (HTTP 200, empty content,
// cipher: null) when the request carries an Origin or Sec-Fetch-Dest header, so
// requests go out through the plain HTTPClient.
const (
	cherryMistBaseURL = "https://cherrymist.cafe"
	cherryMistAPIBase = cherryMistBaseURL + "/api"
)

var (
	// The API returns each paragraph wrapped in a presentational span that only
	// carries a font weight. It says nothing about the text, so drop it.
	cherryMistSpanRe = regexp.MustCompile(`(?is)</?span[^>]*>`)
	// Paragraphs in the API response are separated by a blank line.
	cherryMistBlockRe = regexp.MustCompile(`\r?\n[ \t]*\r?\n`)
	// Blocks that already carry block-level markup are passed through as-is.
	cherryMistBlockTagRe = regexp.MustCompile(`(?i)<(p|div|ul|ol|li|h[1-6]|blockquote|table|pre|figure|section|article|hr)[\s>/]`)
)

type cherryMistSeries struct {
	ID             int    `json:"id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	AuthorName     string `json:"author_name"`
	OriginalAuthor string `json:"original_author"`
	ShortSynopsis  string `json:"short_synopsis"`
	Synopsis       string `json:"synopsis"`
	CoverImageURL  string `json:"cover_image_url"`
	Translator     struct {
		Name string `json:"name"`
	} `json:"translator"`
}

type cherryMistChapter struct {
	ID            int    `json:"id"`
	SeriesID      int    `json:"series_id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	ChapterNumber int    `json:"chapter_number"`
	PartNumber    *int   `json:"part_number"`
	Content       string `json:"content"`
	Cipher        *struct {
		Seed int `json:"seed"`
	} `json:"cipher"`
}

// cherryMistChapterRef is the by-slug lookup that maps a public chapter URL to
// the numeric id the detail endpoint expects.
type cherryMistChapterRef struct {
	ID         int    `json:"id"`
	SeriesID   int    `json:"series_id"`
	SeriesSlug string `json:"series_slug"`
	Slug       string `json:"slug"`
}

type cherrymistParser struct{}

func NewCherryMistParser() *cherrymistParser {
	return &cherrymistParser{}
}

func (p *cherrymistParser) Name() string { return "cherrymist" }

// The site is not behind Cloudflare and serves a stable JSON API, so the
// browser worker is not needed.
func (p *cherrymistParser) RequiresBrowser() bool { return false }

func (p *cherrymistParser) CanHandle(urlStr string) bool {
	return strings.Contains(urlStr, "cherrymist.cafe")
}

func (p *cherrymistParser) GetNovelInfo(ctx context.Context, client HTTPClient, rawURL string) (*NovelInfo, error) {
	seriesSlug, chapterSlug, err := cherryMistSlugs(rawURL)
	if err != nil {
		return nil, err
	}

	// A chapter URL identifies the series only indirectly, via the chapter.
	seriesKey := seriesSlug
	if seriesKey == "" {
		ref, err := p.chapterRef(ctx, client, chapterSlug)
		if err != nil {
			return nil, err
		}
		seriesKey = ref.SeriesSlug
		if seriesKey == "" {
			seriesKey = strconv.Itoa(ref.SeriesID)
		}
	}

	series, err := p.series(ctx, client, seriesKey)
	if err != nil {
		return nil, err
	}

	chapters, err := p.chapters(ctx, client, series.ID)
	if err != nil {
		return nil, err
	}

	info := &NovelInfo{
		SourceURL:   rawURL,
		Title:       CleanTitle(series.Title),
		Author:      CleanTitle(cherryMistAuthor(series)),
		Description: cherryMistSynopsis(series),
		CoverURL:    series.CoverImageURL,
		Chapters:    chapters,
	}
	return info, nil
}

// GetChapterURLs refreshes the chapter list. The doc argument is unused: the
// story route renders client-side, so the HTML carries no chapter links.
func (p *cherrymistParser) GetChapterURLs(ctx context.Context, client HTTPClient, _ *goquery.Document, rawURL string) ([]ChapterURL, error) {
	seriesSlug, chapterSlug, err := cherryMistSlugs(rawURL)
	if err != nil {
		return nil, err
	}
	if seriesSlug == "" {
		ref, err := p.chapterRef(ctx, client, chapterSlug)
		if err != nil {
			return nil, err
		}
		seriesSlug = ref.SeriesSlug
		if seriesSlug == "" {
			seriesSlug = strconv.Itoa(ref.SeriesID)
		}
	}
	series, err := p.series(ctx, client, seriesSlug)
	if err != nil {
		return nil, err
	}
	return p.chapters(ctx, client, series.ID)
}

func (p *cherrymistParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	_, chapterSlug, err := cherryMistSlugs(chapterURL)
	if err != nil {
		return nil, err
	}
	if chapterSlug == "" {
		return nil, fmt.Errorf("cherrymist: %q is not a chapter URL", chapterURL)
	}

	ref, err := p.chapterRef(ctx, client, chapterSlug)
	if err != nil {
		return nil, err
	}

	var ch cherryMistChapter
	if err := cherryMistGetJSON(ctx, client, fmt.Sprintf("%s/chapters/%d", cherryMistAPIBase, ref.ID), &ch); err != nil {
		return nil, err
	}

	if strings.TrimSpace(ch.Content) == "" {
		return nil, fmt.Errorf("cherrymist: chapter %q returned no body", ch.Slug)
	}

	// The body is stored with its letters remapped onto Private Use Area
	// codepoints; the seed selects which permutation was used.
	body := ch.Content
	if ch.Cipher != nil {
		body = cherryMistDecode(body, ch.Cipher.Seed)
	}

	title := ch.Title
	if title == "" {
		title = ch.Slug
	}

	return &Chapter{
		Title:     CleanTitle(title),
		Content:   cherryMistParagraphs(body),
		SourceURL: chapterURL,
	}, nil
}

func (p *cherrymistParser) series(ctx context.Context, client HTTPClient, key string) (*cherryMistSeries, error) {
	var s cherryMistSeries
	if err := cherryMistGetJSON(ctx, client, cherryMistAPIBase+"/series/"+url.PathEscape(key), &s); err != nil {
		return nil, fmt.Errorf("cherrymist: loading series %q: %w", key, err)
	}
	if s.ID == 0 {
		return nil, fmt.Errorf("cherrymist: series %q not found", key)
	}
	return &s, nil
}

func (p *cherrymistParser) chapterRef(ctx context.Context, client HTTPClient, slug string) (*cherryMistChapterRef, error) {
	var ref cherryMistChapterRef
	endpoint := fmt.Sprintf("%s/chapters/by-slug/%s", cherryMistAPIBase, url.PathEscape(slug))
	if err := cherryMistGetJSON(ctx, client, endpoint, &ref); err != nil {
		return nil, fmt.Errorf("cherrymist: resolving chapter %q: %w", slug, err)
	}
	if ref.ID == 0 {
		return nil, fmt.Errorf("cherrymist: chapter %q not found", slug)
	}
	return &ref, nil
}

func (p *cherrymistParser) chapters(ctx context.Context, client HTTPClient, seriesID int) ([]ChapterURL, error) {
	// published=1 drops scheduled chapters that have no readable body yet.
	// Without it the endpoint also returns ~65 future-dated entries for a
	// completed story, which would fail on the first download.
	endpoint := fmt.Sprintf("%s/chapters?series_id=%d&published=1&limit=500", cherryMistAPIBase, seriesID)
	var list []cherryMistChapter
	if err := cherryMistGetJSON(ctx, client, endpoint, &list); err != nil {
		return nil, fmt.Errorf("cherrymist: loading chapters of series %d: %w", seriesID, err)
	}

	sort.SliceStable(list, func(i, j int) bool {
		if list[i].ChapterNumber != list[j].ChapterNumber {
			return list[i].ChapterNumber < list[j].ChapterNumber
		}
		return cherryMistPart(list[i].PartNumber) < cherryMistPart(list[j].PartNumber)
	})

	chapters := make([]ChapterURL, 0, len(list))
	for _, c := range list {
		if c.Slug == "" {
			continue
		}
		chapters = append(chapters, ChapterURL{
			URL:   cherryMistBaseURL + "/chapter/" + c.Slug,
			Title: CleanTitle(c.Title),
			Order: c.ChapterNumber,
		})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("cherrymist: series %d has no published chapters", seriesID)
	}
	return chapters, nil
}

func cherryMistPart(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func cherryMistAuthor(s *cherryMistSeries) string {
	for _, candidate := range []string{s.OriginalAuthor, s.AuthorName, s.Translator.Name} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

func cherryMistSynopsis(s *cherryMistSeries) string {
	if s.ShortSynopsis != "" {
		return s.ShortSynopsis
	}
	return s.Synopsis
}

// cherryMistSlugs pulls the series and chapter slugs out of a public URL.
// Supported shapes are /story/<series>/, /story/<series>/chapter/<n>/ (the
// legacy link shape, which the site still redirects) and /chapter/<slug>/.
func cherryMistSlugs(rawURL string) (seriesSlug, chapterSlug string, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("cherrymist: parsing %q: %w", rawURL, err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		switch part {
		case "story", "series", "novel":
			if i+1 < len(parts) {
				seriesSlug = parts[i+1]
			}
		case "chapter":
			if i+1 < len(parts) {
				chapterSlug = parts[i+1]
			}
		}
	}
	if seriesSlug == "" && chapterSlug == "" {
		return "", "", fmt.Errorf("cherrymist: %q is not a cherrymist story or chapter URL", rawURL)
	}
	return seriesSlug, chapterSlug, nil
}

func cherryMistGetJSON(ctx context.Context, client HTTPClient, endpoint string, out any) error {
	body, err := client.Fetch(ctx, endpoint)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// cherryMistDecode maps the Private Use Area codepoints in a chapter body back
// to letters using the table for the chapter's cipher seed. Characters outside
// the cipher range (punctuation, spaces, markup) are already in the clear and
// are passed through untouched.
func cherryMistDecode(s string, seed int) string {
	if seed < 0 || seed >= len(cherryMistCipherTables) {
		return s
	}
	table := cherryMistCipherTables[seed]

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0xE000 && r <= 0xF8FF {
			if i := int(r - 0xE000); i < len(table) {
				b.WriteByte(table[i])
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// cherryMistParagraphs turns the decoded body into the paragraph HTML the rest
// of the downloader expects, mirroring how the site itself renders the field:
// unwrap the font-weight spans, then treat each blank-line-separated block as a
// paragraph unless it already carries block-level markup.
func cherryMistParagraphs(content string) string {
	cleaned := strings.ReplaceAll(content, "\r\n", "\n")
	cleaned = cherryMistSpanRe.ReplaceAllString(cleaned, "")

	var blocks []string
	for _, block := range cherryMistBlockRe.Split(cleaned, -1) {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if cherryMistBlockTagRe.MatchString(block) {
			blocks = append(blocks, block)
			continue
		}
		blocks = append(blocks, "<p>"+strings.ReplaceAll(block, "\n", "<br />")+"</p>")
	}
	return strings.Join(blocks, "\n")
}
