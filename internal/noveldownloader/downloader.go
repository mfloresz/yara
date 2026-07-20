package noveldownloader

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

const (
	// DefaultMinChapterDelay is the default minimum wait between two
	// consecutive chapter fetches. Used to stay below the rate limits of
	// upstream sites like novelfire.net.
	DefaultMinChapterDelay = 5 * time.Second
	// DefaultMaxChapterDelay is the default maximum wait between two
	// consecutive chapter fetches. A new random value in
	// [min, max] is picked for each gap so the request pattern is less
	// recognizable by upstream defences.
	DefaultMaxChapterDelay = 10 * time.Second
)

type Downloader struct {
	parsers []Parser
	client  HTTPClient
	// MinChapterDelay is the lower bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MinChapterDelay time.Duration
	// MaxChapterDelay is the upper bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MaxChapterDelay time.Duration
}

func NewDownloader() *Downloader {
	return &Downloader{
		parsers: []Parser{
			NewNovelfireParser(),
			NewFenrirRealmParser(),
			NewFloraeGardenParser(),
			NewCherryMistParser(),
			NewEmpireNovelParser(),
			New69ShubaParser(),
			NewSkyNovelsParser(),
			NewSkyDemonOrderParser(),
		},
		client:          NewHTTPClient(),
		MinChapterDelay: DefaultMinChapterDelay,
		MaxChapterDelay: DefaultMaxChapterDelay,
	}
}

// NewDownloaderWithClient returns a Downloader that uses the provided
// HTTPClient. Primarily intended for tests that need to redirect remote
// hosts to local fixtures; the inter-chapter delay is disabled so test
// runs stay fast.
func NewDownloaderWithClient(client HTTPClient) *Downloader {
	return &Downloader{
		parsers: []Parser{
			NewNovelfireParser(),
			NewFenrirRealmParser(),
			NewFloraeGardenParser(),
			NewCherryMistParser(),
			NewEmpireNovelParser(),
			New69ShubaParser(),
			NewSkyNovelsParser(),
			NewSkyDemonOrderParser(),
		},
		client: client,
	}
}

func (d *Downloader) IsSupportedURL(url string) bool {
	return d.FindParser(url) != nil
}

func (d *Downloader) FindParser(url string) Parser {
	for _, p := range d.parsers {
		if p.CanHandle(url) {
			return p
		}
	}
	return nil
}

func (d *Downloader) GetNovelInfo(ctx context.Context, url string) (*NovelInfo, error) {
	parser := d.FindParser(url)
	if parser == nil {
		return nil, fmt.Errorf("unsupported URL: %s", url)
	}
	info, err := parser.GetNovelInfo(ctx, d.client, url)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (d *Downloader) DownloadChapter(ctx context.Context, chapterURL string) (*Chapter, error) {
	parser := d.FindParser(chapterURL)
	if parser == nil {
		return nil, fmt.Errorf("unsupported URL: %s", chapterURL)
	}
	chapter, err := parser.ParseChapter(ctx, d.client, chapterURL)
	if err != nil {
		return nil, err
	}
	if chapter.Content != "" {
		markdown, err := md.ConvertString(chapter.Content)
		if err != nil {
			return nil, fmt.Errorf("converting to markdown: %w", err)
		}
		// The HTML->Markdown converter escapes in-text angle brackets
		// (&lt;…&gt;). Unescape so the stored markdown holds the literal
		// characters; the EPUB pipeline re-escapes them with escapeXML and
		// the translation step sees clean text instead of entities.
		markdown = html.UnescapeString(markdown)
		chapter.Markdown = stripLeadingTitle(cleanMarkdown(markdown), chapter.Title)
	}
	return chapter, nil
}

func (d *Downloader) DownloadChapters(ctx context.Context, chapters []ChapterURL, start, end int) ([]Chapter, error) {
	if start < 1 {
		start = 1
	}
	if end > len(chapters) || end < start {
		end = len(chapters)
	}

	var selected []ChapterURL
	for i, ch := range chapters {
		idx := i + 1
		if idx >= start && idx <= end {
			selected = append(selected, ch)
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no chapters in range %d-%d (total: %d)", start, end, len(chapters))
	}

	result := make([]Chapter, 0, len(selected))
	for i, ch := range selected {
		if i > 0 {
			if err := d.SleepBetweenChapters(ctx); err != nil {
				return nil, err
			}
		}
		chapter, err := d.DownloadChapter(ctx, ch.URL)
		if err != nil {
			return nil, fmt.Errorf("downloading chapter %q: %w", ch.Title, err)
		}
		result = append(result, *chapter)
	}
	return result, nil
}

// SleepBetweenChapters waits a random duration within
// [MinChapterDelay, MaxChapterDelay] before fetching the next chapter.
// The wait is bounded by the request context so cancellation propagates.
func (d *Downloader) SleepBetweenChapters(ctx context.Context) error {
	min := d.MinChapterDelay
	max := d.MaxChapterDelay
	if min <= 0 && max <= 0 {
		return nil
	}
	if max < min {
		max = min
	}
	var delay time.Duration
	if max == min {
		delay = max
	} else {
		delay = min + time.Duration(rand.Int63n(int64(max-min)))
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// stripLeadingTitle removes a markdown heading from the first line of
// content when it looks like a duplicate of the chapter title.
// Some sites (novelfire) inject the chapter title as a heading inside the
// content body, causing a duplicate title in the stored content.
// It also removes the first line if it matches the chapter title (with or
// without numeric prefixes like "1." or "01.").
func stripLeadingTitle(content, chapterTitle string) string {
	trimmed := strings.TrimLeft(content, "\n\t ")
	if trimmed == "" {
		return ""
	}
	// Re-split from the trimmed content so leading whitespace does not
	// cause us to miss a heading on the first non-empty line.
	lines := strings.SplitN(trimmed, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "# ") ||
		strings.HasPrefix(first, "## ") ||
		strings.HasPrefix(first, "### ") ||
		strings.HasPrefix(first, "#### ") {
		if len(lines) > 1 {
			return strings.TrimSpace(lines[1])
		}
		return ""
	}
	// Check if the first line matches the chapter title (exact or without numeric prefix)
	if chapterTitle != "" && matchesTitle(first, chapterTitle) {
		if len(lines) > 1 {
			return strings.TrimSpace(lines[1])
		}
		return ""
	}
	return content
}

// matchesTitle checks if text matches the chapter title, either exactly
// or after removing common numeric prefixes like "1.", "01.", etc.
func matchesTitle(text, title string) bool {
	if text == title {
		return true
	}
	// Try removing numeric prefix from title (e.g., "1.第1章" -> "第1章")
	trimmedTitle := strings.TrimLeft(title, "0123456789.")
	trimmedTitle = strings.TrimSpace(trimmedTitle)
	if trimmedTitle != "" && text == trimmedTitle {
		return true
	}
	// Try removing numeric prefix from text
	trimmedText := strings.TrimLeft(text, "0123456789.")
	trimmedText = strings.TrimSpace(trimmedText)
	if trimmedText != "" && trimmedText == trimmedTitle {
		return true
	}
	return false
}

func cleanMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")
	markdown = strings.TrimSpace(markdown)
	for strings.Contains(markdown, "\n\n\n") {
		markdown = strings.ReplaceAll(markdown, "\n\n\n", "\n\n")
	}
	return markdown
}

// DownloadCover fetches a remote image and returns its bytes together with
// the content type reported by the server. The downloader routes the
// request through its configured HTTPClient so that test transports and
// host rewrites apply consistently.
func (d *Downloader) DownloadCover(ctx context.Context, coverURL string) ([]byte, string, error) {
	if strings.TrimSpace(coverURL) == "" {
		return nil, "", fmt.Errorf("empty cover url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating cover request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching cover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching cover", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading cover body: %w", err)
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	// The browser-worker proxy always returns page text, so for a
	// hotlink-protected or redirecting cover URL it can hand back a small
	// HTML placeholder/error page instead of the actual image bytes. Storing
	// that as the cover corrupts it and makes thumbnail generation fail.
	// Reject payloads that are not real image data before persisting.
	if !isLikelyImage(body) {
		return nil, "", fmt.Errorf("downloaded cover is not a valid image (content-type %q, %d bytes)", mimeType, len(body))
	}
	return body, mimeType, nil
}

// isLikelyImage reports whether data begins with a known image file signature.
// Used to reject HTML error/placeholder pages that the proxy may return for
// cover URLs instead of the binary image.
func isLikelyImage(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return true
	}
	// GIF: 47 49 46 38 ("GIF8")
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return true
	}
	// WEBP: RIFF....WEBP
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return true
	}
	return false
}
