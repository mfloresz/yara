package noveldownloader

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

type BrowserWorkerClient interface {
	SendJob(operation, url string, params map[string]interface{}) (*BrowserWorkerResult, error)
	IsConnected() bool
}

type BrowserWorkerResult struct {
	JobID  string                 `json:"jobId"`
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data"`
}

type BrowserWorkerProvider struct {
	client BrowserWorkerClient
}

func NewBrowserWorkerProvider(client BrowserWorkerClient) *BrowserWorkerProvider {
	return &BrowserWorkerProvider{client: client}
}

func (p *BrowserWorkerProvider) Name() string {
	return "browser-worker"
}

func (p *BrowserWorkerProvider) CanHandle(url string) bool {
	return p.client != nil && p.client.IsConnected()
}

func (p *BrowserWorkerProvider) GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_novel_info", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	info := &NovelInfo{
		SourceURL: url,
	}

	if title, ok := result.Data["title"].(string); ok {
		info.Title = title
	}
	if author, ok := result.Data["author"].(string); ok {
		info.Author = author
	}
	if desc, ok := result.Data["description"].(string); ok {
		info.Description = desc
	}
	if cover, ok := result.Data["coverURL"].(string); ok {
		info.CoverURL = cover
	}
	if sourceURL, ok := result.Data["sourceURL"].(string); ok {
		info.SourceURL = sourceURL
	}

	if chaptersRaw, ok := result.Data["chapters"].([]interface{}); ok {
		for _, ch := range chaptersRaw {
			chMap, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			chURL := ChapterURL{}
			if u, ok := chMap["url"].(string); ok {
				chURL.URL = u
			}
			if t, ok := chMap["title"].(string); ok {
				chURL.Title = t
			}
			info.Chapters = append(info.Chapters, chURL)
		}
	}

	slog.Info("browser worker got novel info",
		"title", info.Title,
		"chapters", len(info.Chapters))

	return info, nil
}

func (p *BrowserWorkerProvider) GetChapterURLs(ctx context.Context, url string) ([]ChapterURL, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_chapters", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	var chapters []ChapterURL
	if chaptersRaw, ok := result.Data["chapters"].([]interface{}); ok {
		for _, ch := range chaptersRaw {
			chMap, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			chURL := ChapterURL{}
			if u, ok := chMap["url"].(string); ok {
				chURL.URL = u
			}
			if t, ok := chMap["title"].(string); ok {
				chURL.Title = t
			}
			chapters = append(chapters, chURL)
		}
	}

	return chapters, nil
}

func (p *BrowserWorkerProvider) ParseChapter(ctx context.Context, url string) (*Chapter, error) {
	if !p.client.IsConnected() {
		return nil, fmt.Errorf("browser worker not connected")
	}

	result, err := p.client.SendJob("get_chapter", url, nil)
	if err != nil {
		return nil, fmt.Errorf("browser worker job failed: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("browser worker returned status: %s", result.Status)
	}

	chapter := &Chapter{
		SourceURL: url,
	}

	if title, ok := result.Data["title"].(string); ok {
		chapter.Title = title
	}
	if content, ok := result.Data["content"].(string); ok {
		chapter.Content = content
	}
	if markdown, ok := result.Data["markdown"].(string); ok {
		chapter.Markdown = markdown
	}
	if sourceURL, ok := result.Data["sourceURL"].(string); ok {
		chapter.SourceURL = sourceURL
	}

	if chapter.Content != "" && chapter.Markdown == "" {
		markdown, err := md.ConvertString(chapter.Content)
		if err != nil {
			slog.Warn("browser worker failed to convert to markdown", "error", err)
		} else {
			// Mirror DownloadChapter: the converter escapes in-text angle
			// brackets, so unescape to keep literal characters in the markdown.
			chapter.Markdown = strings.TrimSpace(html.UnescapeString(markdown))
		}
	}

	return chapter, nil
}
