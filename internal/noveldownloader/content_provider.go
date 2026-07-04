package noveldownloader

import (
	"context"
)

type ContentProvider interface {
	Name() string
	CanHandle(url string) bool
	GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error)
	GetChapterURLs(ctx context.Context, url string) ([]ChapterURL, error)
	ParseChapter(ctx context.Context, url string) (*Chapter, error)
}

type ContentDownloader interface {
	DownloadChapter(ctx context.Context, chapterURL string) (*Chapter, error)
	DownloadChapters(ctx context.Context, chapters []ChapterURL, start, end int) ([]Chapter, error)
	GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error)
	IsSupportedURL(url string) bool
	FindParser(url string) Parser
}
