package noveldownloader

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

// novelArrowFlightPrefix opens every RSC flight chunk emitted by the Next.js
// App Router payloads that novelarrow.com serves.
const novelArrowFlightPrefix = `self.__next_f.push([1,`

func (p *NovelArrowParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	parts, ok := novelArrowParsePath(chapterURL)
	if !ok {
		return nil, fmt.Errorf("invalid novelarrow URL: %s", chapterURL)
	}
	if parts.Chapter == "" {
		return nil, fmt.Errorf("not a chapter URL: %s", chapterURL)
	}

	body, err := client.Fetch(ctx, chapterURL)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter page: %w", err)
	}

	content, err := novelArrowExtractContent(body)
	if err != nil {
		return nil, err
	}

	// The chapter title is the leading heading of the content fragment; fall
	// back to the og:novel:chapter_name meta when the site omits it.
	title := novelArrowHeadingTitle(content)
	if title == "" {
		title = novelArrowMetaChapterName(body)
	}
	if title == "" {
		return nil, fmt.Errorf("no chapter title found")
	}

	return &Chapter{
		Title:     CleanTitle(title),
		Content:   strings.TrimSpace(content),
		SourceURL: chapterURL,
	}, nil
}

// novelArrowExtractContent pulls the decoded chapter HTML out of the RSC flight
// stream of a chapter page. The site serializes the rendered reading pane as a
// single flight string whose body begins with "<" — encoded as \u003c —
// directly followed by the chapter heading. Other flight chunks carry JSON
// bookkeeping and never start with "<", so the first match is the content.
func novelArrowExtractContent(pageHTML []byte) (string, error) {
	page := string(pageHTML)
	for {
		idx := strings.Index(page, novelArrowFlightPrefix)
		if idx < 0 {
			return "", fmt.Errorf("no chapter content found in flight stream")
		}
		rest := page[idx+len(novelArrowFlightPrefix):]
		if !strings.HasPrefix(rest, `"`) {
			page = rest
			continue
		}
		body, _, ok := novelArrowFlightBody(rest)
		if !ok {
			return "", fmt.Errorf("malformed flight chunk")
		}
		if !strings.HasPrefix(body, `\u003c`) {
			page = rest
			continue
		}
		decoded, err := novelArrowDecodeJSString(body)
		if err != nil {
			return "", fmt.Errorf("decoding chapter content: %w", err)
		}
		return decoded, nil
	}
}

// novelArrowFlightBody scans the payload following the opening quote of a
// single self.__next_f.push([1,"...")] chunk and returns the raw JS string
// literal body (with escapes intact). The chunk terminator is a single
// unescaped quote followed by "])".
func novelArrowFlightBody(rest string) (body string, consumed int, ok bool) {
	var sb strings.Builder
	i := 1 // skip the opening quote
	for i < len(rest) {
		switch c := rest[i]; c {
		case '\\':
			if i+1 >= len(rest) {
				return "", 0, false
			}
			sb.WriteByte(c)
			sb.WriteByte(rest[i+1])
			i += 2
		case '"':
			if strings.HasPrefix(rest[i:], `"])`) {
				return sb.String(), i + 3, true
			}
			sb.WriteByte(c)
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return "", 0, false
}

// novelArrowDecodeJSString decodes the string literal body of a Next.js flight
// chunk. The site escapes the chapter HTML as a JS string: < > & are written as
// \u003c \u003e \u0026, apostrophes as \' (which JSON rejects, so a plain JSON
// decode is not applicable), and newlines as \n.
func novelArrowDecodeJSString(raw string) (string, error) {
	var b strings.Builder
	n := len(raw)
	i := 0
	for i < n {
		c := raw[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= n {
			return "", fmt.Errorf("trailing backslash in flight string")
		}
		i++
		e := raw[i]
		i++
		switch e {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0':
			b.WriteByte(0)
		case 'x':
			if i+2 > n {
				return "", fmt.Errorf("truncated \\x escape")
			}
			v, err := strconv.ParseUint(raw[i:i+2], 16, 8)
			if err != nil {
				return "", fmt.Errorf("invalid \\x escape: %w", err)
			}
			b.WriteByte(byte(v))
			i += 2
		case 'u':
			// \u{...} code point escape.
			if i < n && raw[i] == '{' {
				end := strings.IndexByte(raw[i:], '}')
				if end < 0 {
					return "", fmt.Errorf("unterminated \\u{...} escape")
				}
				v, err := strconv.ParseUint(raw[i+1:i+end], 16, 32)
				if err != nil {
					return "", fmt.Errorf("invalid \\u{...} escape: %w", err)
				}
				b.WriteRune(rune(v))
				i += end + 1
				continue
			}
			// \uXXXX (two consecutive escapes can form a surrogate pair).
			if i+4 > n {
				return "", fmt.Errorf("truncated \\u escape")
			}
			hi, err := strconv.ParseUint(raw[i:i+4], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid \\u escape: %w", err)
			}
			r := rune(hi)
			if utf16.IsSurrogate(r) && i+10 <= n && strings.HasPrefix(raw[i+4:], `\u`) {
				if lo, err := strconv.ParseUint(raw[i+6:i+10], 16, 32); err == nil {
					if combined := utf16.DecodeRune(r, rune(lo)); combined != utf8.RuneError {
						b.WriteRune(combined)
						i += 10
						continue
					}
				}
			}
			b.WriteRune(r)
			i += 4
		default:
			// \' \" \\ \/ and any other escaped char map to itself.
			b.WriteByte(e)
		}
	}
	return b.String(), nil
}

// novelArrowHeadingTitle returns the text of the first heading at the top of a
// decoded chapter fragment, or "" if the fragment has no heading.
func novelArrowHeadingTitle(content string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Find("h1,h2,h3,h4,h5,h6").First().Text())
}

// novelArrowMetaChapterName reads the og:novel:chapter_name meta tag that
// novelarrow emits in the <head> of chapter pages.
func novelArrowMetaChapterName(pageHTML []byte) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(pageHTML)))
	if err != nil {
		return ""
	}
	return metaContent(doc, `meta[name="og:novel:chapter_name"]`)
}