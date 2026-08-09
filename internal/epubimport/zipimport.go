package epubimport

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Sentinel errors returned by ParseZipNovel so callers can map them to the
// right HTTP responses.
var (
	// ErrInvalidZip means the blob is not a readable zip archive.
	ErrInvalidZip = errors.New("invalid zip")
	// ErrMissingMetadata means the zip has no metadata.json at its root.
	ErrMissingMetadata = errors.New("missing metadata.json")
	// ErrMissingOriginals means the zip has no originals/ directory.
	ErrMissingOriginals = errors.New("missing originals directory")
)

// ZipChapter is a single chapter extracted from an import zip.
type ZipChapter struct {
	Order             int
	Title             string
	TranslatedTitle   string
	OriginalContent   string
	TranslatedContent string
}

// ZipNovelData is the parsed content of an import zip: metadata, cover and
// ordered chapters.
type ZipNovelData struct {
	MetadataJSON string
	CoverBlob    []byte
	CoverMime    string
	Chapters     []ZipChapter
}

// zipChapterOrderRegex mirrors the chapter-number extraction used by the
// URL-import flows: the first number found in the entry name.
var zipChapterOrderRegex = regexp.MustCompile(`(\d+)`)

func zipChapterOrder(filename string) int {
	matches := zipChapterOrderRegex.FindStringSubmatch(filename)
	if len(matches) >= 2 {
		if n, err := strconv.Atoi(matches[1]); err == nil {
			return n
		}
	}
	return 0
}

// ParseZipNovel reads an import zip (metadata.json plus an originals/
// directory, with optional cover.* and translated/ counterparts), classifies
// its entries, and returns the ordered chapters. It is the pure layout logic
// behind the import-from-zip endpoint; the caller handles HTTP concerns.
func ParseZipNovel(blob []byte) (ZipNovelData, error) {
	reader, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return ZipNovelData{}, fmt.Errorf("%w: %v", ErrInvalidZip, err)
	}
	rawEntries := make([]struct {
		name    string
		content []byte
	}, 0)
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			return ZipNovelData{}, fmt.Errorf("open %s: %w", f.Name, openErr)
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return ZipNovelData{}, fmt.Errorf("read %s: %w", f.Name, readErr)
		}
		name := strings.TrimLeft(filepath.ToSlash(f.Name), "./")
		rawEntries = append(rawEntries, struct {
			name    string
			content []byte
		}{name, data})
	}
	prefix := detectZipRoot(rawEntries)
	var metadataJSON string
	var coverBlob []byte
	var coverMime string
	type zipFile struct {
		name    string
		content string
	}
	originals := map[string]zipFile{}
	translated := map[string]zipFile{}
	for _, entry := range rawEntries {
		normalized := strings.TrimPrefix(entry.name, prefix)
		lower := strings.ToLower(normalized)
		switch {
		case lower == "metadata.json":
			metadataJSON = string(entry.content)
		case strings.HasPrefix(lower, "cover."):
			coverBlob = entry.content
			ext := strings.ToLower(filepath.Ext(normalized))
			switch ext {
			case ".jpg", ".jpeg":
				coverMime = "image/jpeg"
			case ".png":
				coverMime = "image/png"
			case ".gif":
				coverMime = "image/gif"
			case ".webp":
				coverMime = "image/webp"
			default:
				coverMime = "image/jpeg"
			}
		case strings.HasPrefix(lower, "originals/"):
			name := normalized[len("originals/"):]
			if name != "" {
				originals[name] = zipFile{name: name, content: string(entry.content)}
			}
		case strings.HasPrefix(lower, "translated/"):
			name := normalized[len("translated/"):]
			if name != "" {
				translated[name] = zipFile{name: name, content: string(entry.content)}
			}
		}
	}
	if metadataJSON == "" {
		return ZipNovelData{}, ErrMissingMetadata
	}
	if len(originals) == 0 {
		return ZipNovelData{}, ErrMissingOriginals
	}
	type namedFile struct {
		name    string
		content string
		number  int
	}
	sorted := make([]namedFile, 0, len(originals))
	for name, f := range originals {
		sorted = append(sorted, namedFile{name: name, content: f.content, number: zipChapterOrder(name)})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].number < sorted[j].number
	})
	chapters := make([]ZipChapter, 0, len(sorted))
	for idx, entry := range sorted {
		title := extractChapterTitle(entry.content, entry.name)
		origContent := contentAfterTitle(entry.content)
		transContent := ""
		transTitle := ""
		if t, ok := translated[entry.name]; ok {
			transContent = contentAfterTitle(t.content)
			transTitle = extractChapterTitle(t.content, entry.name)
		}
		chapters = append(chapters, ZipChapter{
			Order:             idx + 1,
			Title:             title,
			TranslatedTitle:   transTitle,
			OriginalContent:   origContent,
			TranslatedContent: transContent,
		})
	}
	return ZipNovelData{
		MetadataJSON: metadataJSON,
		CoverBlob:    coverBlob,
		CoverMime:    coverMime,
		Chapters:     chapters,
	}, nil
}

func detectZipRoot(entries []struct {
	name    string
	content []byte
}) string {
	if len(entries) == 0 {
		return ""
	}
	candidate := entries[0].name
	for {
		idx := strings.IndexByte(candidate, '/')
		if idx < 0 {
			return ""
		}
		prefix := candidate[:idx+1]
		allMatch := true
		for _, e := range entries {
			if !strings.HasPrefix(e.name, prefix) {
				allMatch = false
				break
			}
		}
		if allMatch {
			if hasFileAtRoot(strings.TrimSuffix(prefix, "/"), entries) {
				return prefix
			}
			candidate = entries[0].name[idx+1:]
			continue
		}
		return ""
	}
}

func hasFileAtRoot(dir string, entries []struct {
	name    string
	content []byte
}) bool {
	for _, e := range entries {
		rest := strings.TrimPrefix(e.name, dir+"/")
		if rest != "" && strings.IndexByte(rest, '/') < 0 {
			if base := strings.ToLower(filepath.Base(rest)); base == "metadata.json" || strings.HasPrefix(base, "originals") || strings.HasPrefix(base, "translated") {
				return true
			}
		}
	}
	return false
}

func extractChapterTitle(content, filename string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	first = strings.TrimSpace(first)
	first = strings.TrimLeft(first, "# ")
	first = stripMarkdown(first)
	first = strings.TrimSpace(first)
	if first != "" {
		return first
	}
	return filename
}

func stripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "***", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "~~", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func contentAfterTitle(content string) string {
	_, rest, found := strings.Cut(strings.TrimSpace(content), "\n")
	if !found || rest == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest)
}
