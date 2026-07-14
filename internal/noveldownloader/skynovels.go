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

var skynovelsIDRe = regexp.MustCompile(`/novelas/(\d+)`)

type skynovelsParser struct{}

func NewSkyNovelsParser() *skynovelsParser {
	return &skynovelsParser{}
}

func (p *skynovelsParser) Name() string { return "skynovels" }

func (p *skynovelsParser) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	return host == "skynovels.net"
}

func (p *skynovelsParser) GetNovelInfo(ctx context.Context, client HTTPClient, novelURL string) (*NovelInfo, error) {
	novelID, err := extractNovelID(novelURL)
	if err != nil {
		return nil, fmt.Errorf("extracting novel ID: %w", err)
	}

	baseURL := skynovelsAPIBase + "/novels/" + strconv.Itoa(novelID) + "/base"
	raw, err := fetchSkyNovelsAPI(ctx, client, baseURL)
	if err != nil {
		return nil, fmt.Errorf("fetching novel metadata: %w", err)
	}

	var resp struct {
		Novel struct {
			Title        string `json:"nvl_title"`
			Writer       string `json:"nvl_writer"`
			Translator   string `json:"nvl_translator"`
			Description  string `json:"nvl_content"`
			Image        string `json:"image"`
			Name         string `json:"nvl_name"`
			IsVip        string `json:"isVip"`
			PointsLimit  int    `json:"nvl_pointslimit"`
		} `json:"novel"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing novel metadata: %w", err)
	}

	n := resp.Novel
	coverURL := ""
	if n.Image != "" {
		coverURL = skynovelsImageBase + n.Image + "/novels/false"
	}

	author := n.Writer
	if n.Translator != "" {
		if author != "" {
			author += " (trad. " + n.Translator + ")"
		} else {
			author = n.Translator
		}
	}

	info := &NovelInfo{
		Title:       n.Title,
		Author:      author,
		Description: n.Description,
		CoverURL:    coverURL,
		SourceURL:   novelURL,
	}

	chapters, err := p.fetchAllChapters(ctx, client, novelID)
	if err != nil {
		return nil, fmt.Errorf("fetching chapters: %w", err)
	}
	info.Chapters = chapters

	return info, nil
}

func (p *skynovelsParser) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error) {
	novelID, err := extractNovelID(url)
	if err != nil {
		return nil, fmt.Errorf("extracting novel ID: %w", err)
	}
	return p.fetchAllChapters(ctx, client, novelID)
}

func extractNovelID(novelURL string) (int, error) {
	m := skynovelsIDRe.FindStringSubmatch(novelURL)
	if m == nil {
		return 0, fmt.Errorf("no novel ID found in URL %q", novelURL)
	}
	return strconv.Atoi(m[1])
}

const (
	skynovelsAPIBase   = "https://api.skynovels.net/api"
	skynovelsImageBase = "https://api.skynovels.net/api/get-image/"
)

// fetchSkyNovelsAPI makes a GET request to the SkyNovels API with the
// required Referer header. The API rejects requests without it.
func fetchSkyNovelsAPI(ctx context.Context, client HTTPClient, apiURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://www.skynovels.net/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Ensure interface compliance at compile time.
var _ Parser = (*skynovelsParser)(nil)
