package noveldownloader

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var skynovelsChapterIDRe = regexp.MustCompile(`/chapter/(\d+)`)

type skynovelsChapterResponse struct {
	Chapter struct {
		ID      int    `json:"id"`
		Title   string `json:"chp_title"`
		Content string `json:"chp_content"`
		Number  int    `json:"chp_number"`
	} `json:"chapter"`
}

func (p *skynovelsParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	m := skynovelsChapterIDRe.FindStringSubmatch(chapterURL)
	if m == nil {
		return nil, fmt.Errorf("no chapter ID found in URL %q", chapterURL)
	}
	chapterID := m[1]

	apiURL := skynovelsAPIBase + "/chapters/" + chapterID
	raw, err := fetchSkyNovelsAPI(ctx, client, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter: %w", err)
	}

	var resp skynovelsChapterResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing chapter JSON: %w", err)
	}

	ch := resp.Chapter
	if ch.Content == "" {
		return nil, fmt.Errorf("empty chapter content")
	}

	content := cleanSkyNovelsContent(ch.Content)

	return &Chapter{
		Title:     ch.Title,
		Content:   content,
		SourceURL: chapterURL,
	}, nil
}

// cleanSkyNovelsContent removes zero-width characters injected by SkyNovels
// for obfuscation and converts plain-text paragraphs (separated by \r\n\r\n
// or \n\n) into HTML <p> tags so the markdown converter preserves breaks.
func cleanSkyNovelsContent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isZeroWidth(r) {
			b.WriteRune(r)
		}
	}
	cleaned := b.String()

	// Normalize line endings
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")

	// Split into paragraphs on double newlines
	paragraphs := strings.Split(cleaned, "\n\n")

	var result strings.Builder
	for i, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Restore single newlines as <br> for dialogue/verse formatting
		p = strings.ReplaceAll(p, "\n", "<br>")
		if i > 0 {
			result.WriteString("\n\n")
		}
		result.WriteString("<p>")
		result.WriteString(p)
		result.WriteString("</p>")
	}

	return result.String()
}

func isZeroWidth(r rune) bool {
	switch r {
	case '\u200B', // ZERO WIDTH SPACE
		'\u200C', // ZERO WIDTH NON-JOINER
		'\u200D', // ZERO WIDTH JOINER
		'\uFEFF', // ZERO WIDTH NO-BREAK SPACE (BOM)
		'\u2060', // WORD JOINER
		'\u2061', // FUNCTION APPLICATION
		'\u2062', // INVISIBLE TIMES
		'\u2063', // INVISIBLE SEPARATOR
		'\u2064': // INVISIBLE PLUS
		return true
	}
	if unicode.Is(unicode.Cf, r) {
		return true
	}
	return false
}
