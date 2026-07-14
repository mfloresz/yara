package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type skynovelsVolume struct {
	ID             int    `json:"id"`
	Title          string `json:"vlm_title"`
	NovelID        int    `json:"nvl_id"`
	ChaptersCount  int    `json:"chapters_count"`
}

type skynovelsChapterItem struct {
	ID       int    `json:"id"`
	Title    string `json:"chp_title"`
	Number   int    `json:"chp_number"`
	Name     string `json:"chp_name"`
	IsVIP    string `json:"isVip"`
}

type skynovelsChaptersResponse struct {
	Items      []skynovelsChapterItem `json:"items"`
	Pagination struct {
		Page      int `json:"page"`
		Limit     int `json:"limit"`
		Total     int `json:"total"`
		TotalPages int `json:"totalPages"`
		HasMore   bool `json:"hasMore"`
	} `json:"pagination"`
}

func (p *skynovelsParser) fetchAllChapters(ctx context.Context, client HTTPClient, novelID int) ([]ChapterURL, error) {
	volumes, err := p.fetchVolumes(ctx, client, novelID)
	if err != nil {
		return nil, err
	}

	var allChapters []ChapterURL
	for _, vol := range volumes {
		chapters, err := p.fetchVolumeChapters(ctx, client, novelID, vol.ID, vol.ChaptersCount)
		if err != nil {
			continue
		}
		allChapters = append(allChapters, chapters...)
	}

	sort.SliceStable(allChapters, func(i, j int) bool {
		return chapterNumberFromURL(allChapters[i].URL) < chapterNumberFromURL(allChapters[j].URL)
	})

	return allChapters, nil
}

func (p *skynovelsParser) fetchVolumes(ctx context.Context, client HTTPClient, novelID int) ([]skynovelsVolume, error) {
	u := skynovelsAPIBase + "/novels/" + strconv.Itoa(novelID) + "/volumes"
	raw, err := fetchSkyNovelsAPI(ctx, client, u)
	if err != nil {
		return nil, fmt.Errorf("fetching volumes: %w", err)
	}

	var resp struct {
		Volumes []skynovelsVolume `json:"volumes"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing volumes: %w", err)
	}
	return resp.Volumes, nil
}

func (p *skynovelsParser) fetchVolumeChapters(ctx context.Context, client HTTPClient, novelID, volumeID, total int) ([]ChapterURL, error) {
	const pageSize = 100
	var all []ChapterURL

	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/volumes/%d/%d/chapters?page=%d&limit=%d",
			skynovelsAPIBase, novelID, volumeID, page, pageSize)

		raw, err := fetchSkyNovelsAPI(ctx, client, u)
		if err != nil {
			return nil, fmt.Errorf("fetching chapters page %d: %w", page, err)
		}

		var resp skynovelsChaptersResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("parsing chapters page %d: %w", page, err)
		}

		for _, ch := range resp.Items {
			chURL := fmt.Sprintf("https://www.skynovels.net/novelas/%d/chapter/%d", novelID, ch.ID)
			all = append(all, ChapterURL{
				Title: ch.Title,
				URL:   chURL,
			})
		}

		if !resp.Pagination.HasMore || len(resp.Items) == 0 {
			break
		}
	}

	return all, nil
}

func chapterNumberFromURL(u string) int {
	idx := strings.LastIndex(u, "/")
	if idx == -1 {
		return 0
	}
	n, err := strconv.Atoi(u[idx+1:])
	if err != nil {
		return 0
	}
	return n
}
