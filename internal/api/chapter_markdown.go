package api

import (
	"errors"
	"fmt"
	"html"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// Chapter content conversion. A parser script returns contentHtml; the chapters
// collection stores markdown, so every chapter passes through
// htmlToChapterMarkdown on its way in. This is the single home for that
// conversion and the whitespace/title cleanup it depends on.

// htmlToChapterMarkdown converts a script's contentHtml into the markdown the
// chapters collection stores.
func htmlToChapterMarkdown(contentHTML, chapterTitle string) (string, error) {
	if strings.TrimSpace(contentHTML) == "" {
		return "", errors.New("empty chapter content")
	}
	markdown, err := md.ConvertString(contentHTML)
	if err != nil {
		return "", fmt.Errorf("converting to markdown: %w", err)
	}
	// The converter escapes literal angle brackets in text. Unescape so the
	// stored markdown holds the literal characters; the EPUB pipeline
	// re-escapes them and the translation step sees clean text.
	markdown = cleanMarkdown(html.UnescapeString(markdown))
	return strings.TrimSpace(stripLeadingTitle(markdown, chapterTitle)), nil
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

// stripLeadingTitle drops a first line that merely repeats the chapter title
// (sites commonly inject it as a heading). A heading whose text does not match
// the title — e.g. a "#### Julian" POV marker — is story content and is kept.
func stripLeadingTitle(content, chapterTitle string) string {
	trimmed := strings.TrimLeft(content, "\n\t ")
	if trimmed == "" {
		return ""
	}
	lines := strings.SplitN(trimmed, "\n", 2)
	first := strings.TrimSpace(lines[0])
	rest := ""
	if len(lines) > 1 {
		rest = strings.TrimSpace(lines[1])
	}
	if heading, ok := markdownHeadingText(first); ok {
		if chapterTitle != "" && matchesTitle(heading, chapterTitle) {
			return rest
		}
		return content
	}
	if chapterTitle != "" && matchesTitle(first, chapterTitle) {
		return rest
	}
	return content
}

func markdownHeadingText(line string) (string, bool) {
	for _, prefix := range []string{"#### ", "### ", "## ", "# "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", false
}

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
	return trimmedText != "" && trimmedText == trimmedTitle
}
