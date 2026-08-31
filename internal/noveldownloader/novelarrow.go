package noveldownloader

import (
	"net/url"
	"regexp"
	"strings"
)

// NovelArrowParser downloads from novelarrow.com, a Next.js (App Router) site
// that renders the whole page HTML server-side. The full chapter list is served
// by a small JSON API and the chapter body ships inside the RSC flight stream
// as a JS-string-escaped HTML fragment (see novelarrow_content.go).
type NovelArrowParser struct{}

func NewNovelArrowParser() *NovelArrowParser {
	return &NovelArrowParser{}
}

func (p *NovelArrowParser) Name() string {
	return "novelarrow"
}

func (p *NovelArrowParser) RequiresBrowser() bool { return false }

func (p *NovelArrowParser) CanHandle(urlStr string) bool {
	_, ok := novelArrowParsePath(urlStr)
	return ok
}

// novelArrowPathParts holds the parsed parts of a novelarrow.com URL. Chapter
// is empty for the novel info page.
type novelArrowPathParts struct {
	Slug    string
	Chapter string
}

var (
	// https://novelarrow.com/novel/{slug}
	novelArrowNovelPathRe = regexp.MustCompile(`^/novel/([^/]+)/?$`)
	// https://novelarrow.com/chapter/{slug}/{chapter_id}
	novelArrowChapterPathRe = regexp.MustCompile(`^/chapter/([^/]+)/([^/]+)/?$`)
)

// novelArrowParsePath parses a novelarrow.com URL into its path parts. The
// second return value reports whether the URL belongs to the site at all.
func novelArrowParsePath(rawURL string) (novelArrowPathParts, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return novelArrowPathParts{}, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "novelarrow.com" {
		return novelArrowPathParts{}, false
	}
	if m := novelArrowNovelPathRe.FindStringSubmatch(u.Path); m != nil {
		return novelArrowPathParts{Slug: m[1]}, true
	}
	if m := novelArrowChapterPathRe.FindStringSubmatch(u.Path); m != nil {
		return novelArrowPathParts{Slug: m[1], Chapter: m[2]}, true
	}
	return novelArrowPathParts{}, false
}

// novelArrowChapterURL builds the URL of a chapter, keeping the scheme/host of
// the page the parser was given.
func novelArrowChapterURL(pageURL, slug, chapterID string) string {
	return extractBaseURL(pageURL) + "/chapter/" + slug + "/" + chapterID
}