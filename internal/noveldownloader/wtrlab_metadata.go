package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// wtrLabNextData maps the __NEXT_DATA__ JSON embedded in a wtr-lab.com novel
// page. Only the fields the parser needs are declared.
type wtrLabNextData struct {
	Props struct {
		PageProps struct {
			Serie struct {
				SerieData wtrLabSerieData `json:"serie_data"`
			} `json:"serie"`
		} `json:"pageProps"`
	} `json:"props"`
}

type wtrLabSerieData struct {
	Slug  string           `json:"slug"`
	RawID int              `json:"raw_id"`
	Data  wtrLabSerieInner `json:"data"`
}

// wtrLabSerieInner is the "data" object of serie_data; it holds the human
// readable metadata of the series.
type wtrLabSerieInner struct {
	Title       string `json:"title"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

// wtrLabChaptersResponse maps GET /api/chapters/{raw_id}.
type wtrLabChaptersResponse struct {
	Chapters []wtrLabChapterItem `json:"chapters"`
}

type wtrLabChapterItem struct {
	ID    int    `json:"id"`
	Order int    `json:"order"`
	Title string `json:"title"`
	Name  string `json:"name"`
}

func (p *WTRLabParser) GetNovelInfo(ctx context.Context, client HTTPClient, pageURL string) (*NovelInfo, error) {
	parts, ok := wtrLabParsePath(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid wtr-lab URL: %s", pageURL)
	}

	// 1. Fetch the novel page and read metadata from its __NEXT_DATA__ JSON.
	body, err := client.Fetch(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetching novel page: %w", err)
	}
	serie, err := wtrLabParseNextData(body)
	if err != nil {
		return nil, fmt.Errorf("parsing novel metadata: %w", err)
	}

	// 2. Fetch the full chapter list.
	chapters, err := p.fetchChapters(ctx, client, pageURL, parts)
	if err != nil {
		return nil, err
	}

	return &NovelInfo{
		Title:       serie.Data.Title,
		Author:      serie.Data.Author,
		Description: strings.TrimSpace(serie.Data.Description),
		CoverURL:    serie.Data.Image,
		SourceURL:   pageURL,
		Chapters:    chapters,
	}, nil
}

func (p *WTRLabParser) GetChapterURLs(ctx context.Context, client HTTPClient, _ *goquery.Document, pageURL string) ([]ChapterURL, error) {
	parts, ok := wtrLabParsePath(pageURL)
	if !ok {
		return nil, fmt.Errorf("invalid wtr-lab URL: %s", pageURL)
	}
	return p.fetchChapters(ctx, client, pageURL, parts)
}

// fetchChapters loads the full chapter list from GET /api/chapters/{raw_id}
// and converts it into download-ready chapter URLs.
func (p *WTRLabParser) fetchChapters(ctx context.Context, client HTTPClient, pageURL string, parts wtrLabPathParts) ([]ChapterURL, error) {
	apiURL := fmt.Sprintf("%s/api/chapters/%d", extractBaseURL(pageURL), parts.RawID)
	body, err := client.Fetch(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter list: %w", err)
	}

	var resp wtrLabChaptersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing chapter list: %w", err)
	}

	chapters := make([]ChapterURL, 0, len(resp.Chapters))
	for _, ch := range resp.Chapters {
		title := ch.Title
		if title == "" {
			title = ch.Name
		}
		chapters = append(chapters, ChapterURL{
			URL:   wtrLabChapterURL(pageURL, parts, ch.Order),
			Title: CleanTitle(title),
		})
	}
	return chapters, nil
}

// wtrLabParseNextData extracts the serie metadata from the __NEXT_DATA__ JSON
// embedded in a wtr-lab.com novel page.
func wtrLabParseNextData(pageHTML []byte) (*wtrLabSerieData, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(pageHTML)))
	if err != nil {
		return nil, fmt.Errorf("parsing page HTML: %w", err)
	}
	raw := strings.TrimSpace(doc.Find(`script#__NEXT_DATA__`).First().Text())
	if raw == "" {
		return nil, fmt.Errorf("__NEXT_DATA__ script not found")
	}

	var nextData wtrLabNextData
	if err := json.Unmarshal([]byte(raw), &nextData); err != nil {
		return nil, fmt.Errorf("parsing __NEXT_DATA__: %w", err)
	}
	serie := nextData.Props.PageProps.Serie.SerieData
	if serie.Data.Title == "" {
		return nil, fmt.Errorf("no serie metadata in __NEXT_DATA__")
	}
	return &serie, nil
}
