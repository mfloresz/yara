package comicdownloader

// ComicInfo contains metadata for a comic/manga/manhwa series.
// It mirrors the old ComicInfo shape so the import flow (metadata + chapter
// list) stays the same; only the chapter payload changes from text to images.
type ComicInfo struct {
	Title       string            `json:"title"`
	Author      string            `json:"author,omitempty"`
	Description string            `json:"description,omitempty"`
	CoverURL    string            `json:"cover_url,omitempty"`
	SourceURL   string            `json:"source_url"`
	Chapters    []ComicChapterURL `json:"chapters"`
}

// ComicChapterURL represents a downloadable comic chapter.
type ComicChapterURL struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
}

// ComicChapter represents a comic chapter and its page image URLs in reading
// order. Index i in Pages is page i+1. The worker downloads each URL to disk;
// URLs themselves are never stored as base64.
type ComicChapter struct {
	Title     string   `json:"title"`
	Pages     []string `json:"pages"`
	SourceURL string   `json:"source_url"`
	Index     int      `json:"index"`
}

type DownloadRange struct {
	Start int
	End   int
}

// ChapterURL and Chapter are short aliases kept for call sites that were
// written against the old novel-downloader naming.
type (
	ChapterURL = ComicChapterURL
	Chapter    = ComicChapter
)
