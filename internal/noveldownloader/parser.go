package noveldownloader

import (
	"context"

	"github.com/PuerkitoBio/goquery"
)

type Parser interface {
	Name() string
	// RequiresBrowser reports whether fetching from this site only works
	// reliably through the browser worker proxy (Cloudflare challenge or
	// JavaScript-rendered pages). Documentation-only: it must not change
	// fetch behavior.
	RequiresBrowser() bool
	CanHandle(url string) bool
	GetNovelInfo(ctx context.Context, client HTTPClient, url string) (*NovelInfo, error)
	GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, url string) ([]ChapterURL, error)
	ParseChapter(ctx context.Context, client HTTPClient, url string) (*Chapter, error)
}
