package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// novelArrowChaptersResponse maps GET /api-web/novels/{slug}/chapters?sort=asc.
type novelArrowChaptersResponse struct {
	Items []novelArrowChapterItem `json:"items"`
}

type novelArrowChapterItem struct {
	ChapterID   string `json:"chapter_id"`
	ChapterName string `json:"chapter_name"`
}

func (p *NovelArrowParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	parts, ok := novelArrowParsePath(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid novelarrow URL: %s", pageURL)
	}
	if parts.Chapter != "" {
		return nil, fmt.Errorf("not a novel page URL: %s", pageURL)
	}

	doc, err := client.FetchDocument(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetching novel page: %w", err)
	}

	title := strings.TrimSpace(doc.Find("h1").First().Text())
	if title == "" {
		return nil, fmt.Errorf("no novel title found on page")
	}

	author := strings.TrimSpace(doc.Find(`a[href*="/author/"]`).First().Text())

	// The synopsis renders inside the .site-reading-copy block, one <p> per
	// paragraph. Join them so the description stays readable as plain text.
	var desc strings.Builder
	doc.Find(".site-reading-copy").First().Find("p").Each(func(_ int, p *goquery.Selection) {
		text := strings.TrimSpace(p.Text())
		if text == "" {
			return
		}
		if desc.Len() > 0 {
			desc.WriteString("\n\n")
		}
		desc.WriteString(text)
	})

	coverURL := ""
	if img := doc.Find(".novel-cover-frame img").First(); len(img.Nodes) > 0 {
		if src, ok := img.Attr("src"); ok {
			coverURL = strings.TrimSpace(src)
		}
	}

	chapters, err := p.fetchChapters(ctx, client, pageURL, parts.Slug)
	if err != nil {
		return nil, err
	}

	return &NovelInfo{
		Title:       CleanTitle(title),
		Author:      CleanTitle(author),
		Description: strings.TrimSpace(desc.String()),
		CoverURL:    coverURL,
		SourceURL:   pageURL,
		Chapters:    chapters,
	}, nil
}

func (p *NovelArrowParser) GetChapterURLs(ctx context.Context, client HTTPClient, _ *goquery.Document, pageURL string) ([]ChapterURL, error) {
	parts, ok := novelArrowParsePath(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid novelarrow URL: %s", pageURL)
	}
	return p.fetchChapters(ctx, client, pageURL, parts.Slug)
}

// fetchChapters loads the full chapter list of a novel from the site's JSON
// API. The novel page only renders the most recent chapters, so the API is the
// only source for the complete, ascending list.
func (p *NovelArrowParser) fetchChapters(ctx context.Context, client HTTPClient, pageURL, slug string) ([]ChapterURL, error) {
	apiURL := fmt.Sprintf("%s/api-web/novels/%s/chapters?sort=asc", extractBaseURL(pageURL), slug)
	body, err := client.Fetch(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter list: %w", err)
	}

	var resp novelArrowChaptersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing chapter list: %w", err)
	}

	chapters := make([]ChapterURL, 0, len(resp.Items))
	for i, ch := range resp.Items {
		if ch.ChapterID == "" {
			continue
		}
		chapters = append(chapters, ChapterURL{
			URL:   novelArrowChapterURL(pageURL, slug, ch.ChapterID),
			Title: CleanTitle(ch.ChapterName),
			Order: i + 1,
		})
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters returned by the novelarrow API")
	}
	return chapters, nil
}