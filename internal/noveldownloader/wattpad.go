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

// WattpadParser downloads from wattpad.com using the site's public JSON APIs
// (the same endpoints the WattpadDownloader reference project uses):
//
//   - GET /api/v3/stories/{story_id}       -> metadata + full part list
//   - GET /api/v3/story_parts/{part_id}    -> story resolution from a part URL
//   - GET /apiv2/storytext?id={part_id}    -> raw HTML of a single part
//
// Chapter HTML is a sequence of <p data-p-id="…"> elements, which the
// downloader's html->markdown conversion handles directly.
type WattpadParser struct{}

func NewWattpadParser() *WattpadParser {
	return &WattpadParser{}
}

func (p *WattpadParser) Name() string { return "wattpad" }

// RequiresBrowser reports whether fetching needs the browser worker proxy.
// The metadata and content endpoints are plain JSON/file APIs that answer to
// simple HTTP requests (verified with curl, no challenge), so this is false.
func (p *WattpadParser) RequiresBrowser() bool { return false }

func (p *WattpadParser) CanHandle(urlStr string) bool {
	_, _, ok := wattpadParseURL(urlStr)
	return ok
}

// wattpadStoryPathRe matches /story/{id} with an optional -slug suffix, e.g.
// /story/207670289-mine or /story/207670289.
var wattpadStoryPathRe = regexp.MustCompile(`^/story/(\d+)(?:-.*)?/?$`)

// wattpadPartPathRe matches a reading page /{part_id} with an optional -slug
// suffix, e.g. /811991190-chapter-1.
var wattpadPartPathRe = regexp.MustCompile(`^/(\d+)(?:-.*)?/?$`)

// wattpadURLKind classifies a wattpad.com URL as a story page or a part page.
type wattpadURLKind int

const (
	wattpadURLInvalid wattpadURLKind = iota
	wattpadURLStory
	wattpadURLPart
)

// wattpadParseURL parses a wattpad.com URL, returning the numeric id, its kind
// (story or part page), and whether the URL belongs to the site at all.
func wattpadParseURL(rawURL string) (int64, wattpadURLKind, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, wattpadURLInvalid, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "wattpad.com" {
		return 0, wattpadURLInvalid, false
	}
	if m := wattpadStoryPathRe.FindStringSubmatch(u.Path); m != nil {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, wattpadURLInvalid, false
		}
		return id, wattpadURLStory, true
	}
	if m := wattpadPartPathRe.FindStringSubmatch(u.Path); m != nil {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, wattpadURLInvalid, false
		}
		return id, wattpadURLPart, true
	}
	return 0, wattpadURLInvalid, false
}

// wattpadStory maps GET /api/v3/stories/{id}. Only the fields the parser needs
// are declared.
type wattpadStory struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Cover       string `json:"cover"`
	Completed   bool   `json:"completed"`
	URL         string `json:"url"`
	User        struct {
		Username string `json:"username"`
	} `json:"user"`
	Parts []wattpadPart `json:"parts"`
}

type wattpadPart struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// wattpadStoryPart maps GET /api/v3/story_parts/{part_id}; it resolves a part
// page URL back to its parent story.
type wattpadStoryPart struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	GroupID string `json:"groupId"`
}

// wattpadStoryFields is the sparse fieldset requested from the stories API,
// mirroring the reference project.
const wattpadStoryFields = "tags,id,title,createDate,modifyDate,language(name),description,completed,mature,url,isPaywalled,user(username,avatar,description),parts(id,title),cover,copyright"

func (p *WattpadParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	story, err := p.fetchStory(ctx, client, pageURL)
	if err != nil {
		return nil, err
	}

	chapters := make([]ChapterURL, 0, len(story.Parts))
	for i, part := range story.Parts {
		chapters = append(chapters, ChapterURL{
			URL:   wattpadPartURL(pageURL, part.ID, part.Title),
			Title: CleanTitle(part.Title),
			Order: i + 1,
		})
	}

	sourceURL := pageURL
	if story.URL != "" {
		sourceURL = story.URL
	}
	return &NovelInfo{
		Title:       CleanTitle(story.Title),
		Author:      strings.TrimSpace(story.User.Username),
		Description: strings.TrimSpace(story.Description),
		CoverURL:    wattpadCoverURL(story.Cover),
		SourceURL:   sourceURL,
		Chapters:    chapters,
	}, nil
}

func (p *WattpadParser) GetChapterURLs(ctx context.Context, client HTTPClient, _ *goquery.Document, pageURL string) ([]ChapterURL, error) {
	story, err := p.fetchStory(ctx, client, pageURL)
	if err != nil {
		return nil, err
	}

	chapters := make([]ChapterURL, 0, len(story.Parts))
	for i, part := range story.Parts {
		chapters = append(chapters, ChapterURL{
			URL:   wattpadPartURL(pageURL, part.ID, part.Title),
			Title: CleanTitle(part.Title),
			Order: i + 1,
		})
	}
	return chapters, nil
}

// fetchStory resolves any wattpad.com URL (story or part page) to the parent
// story metadata.
func (p *WattpadParser) fetchStory(ctx context.Context, client HTTPClient, pageURL string) (*wattpadStory, error) {
	id, kind, ok := wattpadParseURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid wattpad URL: %s", pageURL)
	}
	storyID := id
	if kind == wattpadURLPart {
		part, err := wattpadFetchStoryPart(ctx, client, pageURL, id)
		if err != nil {
			return nil, err
		}
		storyID, err = strconv.ParseInt(part.GroupID, 10, 64)
		if err != nil || storyID == 0 {
			return nil, fmt.Errorf("part %d has no parent story", id)
		}
	}

	apiURL := fmt.Sprintf("%s/api/v3/stories/%d?fields=%s",
		extractBaseURL(pageURL), storyID, wattpadStoryFields)
	var story wattpadStory
	if err := wattpadFetchJSON(ctx, client, apiURL, &story); err != nil {
		return nil, fmt.Errorf("fetching story metadata: %w", err)
	}
	if story.Title == "" {
		return nil, fmt.Errorf("story %d not found or has no title", storyID)
	}
	return &story, nil
}

func (p *WattpadParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	partID, kind, ok := wattpadParseURL(chapterURL)
	if !ok || kind != wattpadURLPart {
		return nil, fmt.Errorf("not a wattpad chapter URL: %s", chapterURL)
	}

	// 1. Resolve the part to its parent story (and canonical title).
	part, err := wattpadFetchStoryPart(ctx, client, chapterURL, partID)
	if err != nil {
		return nil, err
	}

	// 2. Fetch this part's HTML directly. The endpoint answers one small
	// document per part, so no whole-story download is needed.
	textURL := fmt.Sprintf("%s/apiv2/storytext?id=%d",
		extractBaseURL(chapterURL), partID)
	raw, err := wattpadFetchBytes(ctx, client, textURL)
	if err != nil {
		// Wattpad answers 404 for parts the anonymous API cannot see
		// (paywalled or deleted); surface that instead of a bare status.
		if strings.Contains(err.Error(), "HTTP 404") {
			return nil, fmt.Errorf("part %d not accessible (it may be paywalled or deleted)", partID)
		}
		return nil, fmt.Errorf("fetching part %d contents: %w", partID, err)
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return nil, fmt.Errorf("part %d is empty (it may be paywalled or deleted)", partID)
	}

	return &Chapter{
		Title:     CleanTitle(part.Title),
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// wattpadFetchStoryPart loads the part metadata used to resolve the parent
// story and the canonical chapter title.
func wattpadFetchStoryPart(ctx context.Context, client HTTPClient, pageURL string, partID int64) (*wattpadStoryPart, error) {
	apiURL := fmt.Sprintf("%s/api/v3/story_parts/%d?fields=id,title,groupId",
		extractBaseURL(pageURL), partID)
	var part wattpadStoryPart
	if err := wattpadFetchJSON(ctx, client, apiURL, &part); err != nil {
		return nil, fmt.Errorf("fetching part %d: %w", partID, err)
	}
	if part.GroupID == "" {
		return nil, fmt.Errorf("part %d not found or has no parent story", partID)
	}
	return &part, nil
}

// wattpadFetchJSON performs a GET with a JSON Accept header and unmarshals the
// response body into target. Wattpad answers errors as JSON with an
// error_code; the message is surfaced when the status is not 2xx.
func wattpadFetchJSON(ctx context.Context, client HTTPClient, apiURL string, target any) error {
	body, err := wattpadFetchBytes(ctx, client, apiURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("parsing %s: %w", apiURL, err)
	}
	return nil
}

func wattpadFetchBytes(ctx context.Context, client HTTPClient, apiURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, */*;q=0.8")

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

// wattpadPartURL builds the canonical reading URL of a part, keeping the
// scheme/host of the page the parser was given.
func wattpadPartURL(pageURL string, partID int64, title string) string {
	slug := wattpadSlugify(title)
	if slug == "" {
		return fmt.Sprintf("%s/%d", extractBaseURL(pageURL), partID)
	}
	return fmt.Sprintf("%s/%d-%s", extractBaseURL(pageURL), partID, slug)
}

// wattpadCoverURL upgrades the 256px thumbnail the API returns to the 512px
// variant, mirroring the reference project.
func wattpadCoverURL(cover string) string {
	if cover == "" {
		return ""
	}
	return strings.Replace(cover, "-256-", "-512-", 1)
}

// wattpadSlugify converts a part title to a URL slug ("Chapter 1" ->
// "chapter-1"), mirroring the site's reading-page URLs.
func wattpadSlugify(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var sb strings.Builder
	prevDash := true // trim leading dashes without a special case
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash {
				sb.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}

// Ensure interface compliance at compile time.
var _ Parser = (*WattpadParser)(nil)
