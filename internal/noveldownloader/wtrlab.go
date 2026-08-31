package noveldownloader

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// WTRLabParser downloads from wtr-lab.com, a Next.js site that serves novels
// in several reading modes ("web", "web+", "AI"). This parser targets the
// "web" mode — the raw source text (the Chinese original for these novels) —
// which is exactly what the translation pipeline consumes. The data comes from
// two JSON APIs:
//
//   - GET  /api/chapters/{raw_id}        -> full chapter list
//   - POST /api/reader/get               -> chapter content
//
// The novel metadata (title, author, description, cover) is embedded as the
// __NEXT_DATA__ JSON in the SSR HTML of the novel page.
//
// The reader's "web" payload is AES-GCM encrypted (the key ships in the page
// JS); the "AI" translation mode is plaintext but limited by a free-reading
// quota and Cloudflare Turnstile, so it is not used here.
type WTRLabParser struct{}

func NewWTRLabParser() *WTRLabParser {
	return &WTRLabParser{}
}

func (p *WTRLabParser) Name() string {
	return "wtr-lab"
}

func (p *WTRLabParser) RequiresBrowser() bool { return false }

func (p *WTRLabParser) CanHandle(urlStr string) bool {
	_, ok := wtrLabParsePath(urlStr)
	return ok
}

// wtrLabPathParts holds the parsed parts of a wtr-lab.com novel or chapter
// URL. Chapter is 0 for the novel info page.
type wtrLabPathParts struct {
	Locale  string
	RawID   int
	Slug    string
	Chapter int
}

// wtrLabNovelPathRe matches /{locale}/novel/{raw_id}/{slug} and its
// /chapter-{order} variant, e.g.
//
//	/en/novel/88651/im-playing-the-role-of-a-beautiful-powerful-and-tragic-big-shot-in-the-infinite-world
//	/en/novel/88651/im-playing-the-role-of-a-beautiful-powerful-and-tragic-big-shot-in-the-infinite-world/chapter-1
var wtrLabNovelPathRe = regexp.MustCompile(`^/([a-z]{2}(?:-[a-z]{2})?)/novel/(\d+)/([^/]+)(?:/chapter-(\d+))?/?$`)

// wtrLabParsePath parses a wtr-lab.com URL into its path parts. The second
// return value reports whether the URL belongs to the site at all.
func wtrLabParsePath(rawURL string) (wtrLabPathParts, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return wtrLabPathParts{}, false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "wtr-lab.com" {
		return wtrLabPathParts{}, false
	}
	m := wtrLabNovelPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return wtrLabPathParts{}, false
	}
	rawID, err := strconv.Atoi(m[2])
	if err != nil {
		return wtrLabPathParts{}, false
	}
	parts := wtrLabPathParts{
		Locale: m[1],
		RawID:  rawID,
		Slug:   m[3],
	}
	if m[4] != "" {
		parts.Chapter, err = strconv.Atoi(m[4])
		if err != nil {
			return wtrLabPathParts{}, false
		}
	}
	return parts, true
}

// wtrLabChapterURL builds the URL of the order-th chapter of a novel, keeping
// the scheme/host and locale of the page the parser was given.
func wtrLabChapterURL(pageURL string, parts wtrLabPathParts, order int) string {
	return fmt.Sprintf("%s/%s/novel/%d/%s/chapter-%d",
		extractBaseURL(pageURL), parts.Locale, parts.RawID, parts.Slug, order)
}
