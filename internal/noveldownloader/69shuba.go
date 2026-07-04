package noveldownloader

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	sixtyNineShubaInfoRe    = regexp.MustCompile(`69shuba\.com/book/(\d+)\.htm`)
	sixtyNineShubaChapsRe   = regexp.MustCompile(`69shuba\.com/book/(\d+)/?$`)
	sixtyNineShubaChapterRe = regexp.MustCompile(`69shuba\.com/txt/(\d+)/(\d+)`)
	sixtyNineShubaBaseURL   = "https://www.69shuba.com"
)

type sixtyNineShuba struct{}

func New69ShubaParser() *sixtyNineShuba {
	return &sixtyNineShuba{}
}

func (s *sixtyNineShuba) Name() string { return "69shuba" }

func (s *sixtyNineShuba) CanHandle(u string) bool {
	return strings.Contains(u, "69shuba.com")
}

func (s *sixtyNineShuba) GetNovelInfo(ctx context.Context, client HTTPClient, u string) (*NovelInfo, error) {
	if sixtyNineShubaChapterRe.MatchString(u) {
		return s.getInfoFromChapter(ctx, client, u)
	}
	return s.getInfoFromInfoPage(ctx, client, u)
}

func (s *sixtyNineShuba) GetChapterURLs(ctx context.Context, client HTTPClient, doc *goquery.Document, u string) ([]ChapterURL, error) {
	return s.fetchChapterList(ctx, client, u)
}

func (s *sixtyNineShuba) ParseChapter(ctx context.Context, client HTTPClient, url string) (*Chapter, error) {
	return s.getChapterContent(ctx, client, url)
}
