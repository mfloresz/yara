package api

import (
	"regexp"
	"strings"
)

type CleanMode string

const (
	CleanModeRemoveAfter          CleanMode = "remove_after"
	CleanModeRemoveDuplicates     CleanMode = "remove_duplicates"
	CleanModeRemoveLine           CleanMode = "remove_line"
	CleanModeRemoveMultipleBlanks CleanMode = "remove_multiple_blanks"
	CleanModeSearchReplace        CleanMode = "search_replace"
)

type CleanOptions struct {
	Mode          CleanMode `json:"mode"`
	SearchText    string    `json:"searchText"`
	ReplaceText   string    `json:"replaceText"`
	CaseSensitive bool      `json:"caseSensitive"`
	UseRegex      bool      `json:"useRegex"`
}

type CleanResult struct {
	Original     string `json:"original"`
	Cleaned      string `json:"cleaned"`
	Changed      bool   `json:"changed"`
	RemovedLines int    `json:"removedLines"`
}

type CleanPreviewResult struct {
	ChapterTitle string          `json:"chapterTitle"`
	Changes      []CleanDiffHunk `json:"changes"`
	CleanResult
}

type CleanPreviewBulkItem struct {
	ChapterID    string          `json:"chapterId"`
	ChapterOrder int             `json:"chapterOrder"`
	ChapterTitle string          `json:"chapterTitle"`
	Changes      []CleanDiffHunk `json:"changes"`
	CleanResult
}

func ApplyClean(text string, opts CleanOptions) CleanResult {
	original := text
	working := strings.ReplaceAll(text, "\r\n", "\n")
	working = strings.ReplaceAll(working, "\r", "\n")

	lines := strings.Split(working, "\n")
	lines = trimBlankEdges(lines)

	var processed []string
	switch opts.Mode {
	case CleanModeRemoveAfter:
		processed = applyRemoveAfter(lines, opts)
	case CleanModeRemoveDuplicates:
		processed = applyRemoveDuplicates(lines, opts)
	case CleanModeRemoveLine:
		processed = applyRemoveLine(lines, opts)
	case CleanModeRemoveMultipleBlanks:
		processed = normalizeBlankLines(lines)
	case CleanModeSearchReplace:
		processed = applySearchReplace(lines, opts)
	default:
		processed = lines
	}

	processed = trimBlankEdges(processed)
	cleaned := strings.Join(processed, "\n")

	return CleanResult{
		Original:     original,
		Cleaned:      cleaned,
		Changed:      cleaned != original,
		RemovedLines: len(lines) - len(processed),
	}
}

// ---------------------------------------------------------------------------
// Predicate builder: returns a function that tests a line for a match
// ---------------------------------------------------------------------------

func buildPredicate(searchText string, caseSensitive, useRegex bool) func(string) bool {
	if useRegex {
		pattern := searchText
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return func(line string) bool { return strings.Contains(line, searchText) }
		}
		return re.MatchString
	}

	if caseSensitive {
		return func(line string) bool { return strings.HasPrefix(line, searchText) }
	}

	needle := strings.ToLower(searchText)
	return func(line string) bool { return strings.HasPrefix(strings.ToLower(line), needle) }
}

// ---------------------------------------------------------------------------
// Modes
// ---------------------------------------------------------------------------

func applyRemoveAfter(lines []string, opts CleanOptions) []string {
	if opts.SearchText == "" {
		return lines
	}
	pred := buildPredicate(opts.SearchText, opts.CaseSensitive, opts.UseRegex)
	for i, line := range lines {
		if pred(line) {
			return lines[:i]
		}
	}
	return lines
}

func applyRemoveDuplicates(lines []string, opts CleanOptions) []string {
	if opts.SearchText == "" {
		return lines
	}
	pred := buildPredicate(opts.SearchText, opts.CaseSensitive, opts.UseRegex)
	var matches []int
	for i, line := range lines {
		if pred(line) {
			matches = append(matches, i)
		}
	}
	if len(matches) <= 1 {
		return lines
	}
	remove := make(map[int]struct{}, len(matches)-1)
	for _, idx := range matches[:len(matches)-1] {
		remove[idx] = struct{}{}
	}
	out := make([]string, 0, len(lines)-len(remove))
	for i, line := range lines {
		if _, ok := remove[i]; !ok {
			out = append(out, line)
		}
	}
	return out
}

func applyRemoveLine(lines []string, opts CleanOptions) []string {
	if opts.SearchText == "" {
		return lines
	}
	pred := buildPredicate(opts.SearchText, opts.CaseSensitive, opts.UseRegex)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if !pred(line) {
			out = append(out, line)
		}
	}
	return out
}

func normalizeBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank {
			if !prevBlank {
				out = append(out, line)
			}
			prevBlank = true
		} else {
			out = append(out, line)
			prevBlank = false
		}
	}
	return out
}

func applySearchReplace(lines []string, opts CleanOptions) []string {
	if opts.SearchText == "" {
		return lines
	}
	replace := opts.ReplaceText

	if opts.UseRegex {
		pattern := opts.SearchText
		if !opts.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return applySplitReplace(lines, opts.SearchText, replace, opts.CaseSensitive)
		}
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = re.ReplaceAllString(line, replace)
		}
		return out
	}

	return applySplitReplace(lines, opts.SearchText, replace, opts.CaseSensitive)
}

func applySplitReplace(lines []string, search, replace string, caseSensitive bool) []string {
	if !caseSensitive {
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = caseInsensitiveReplace(line, search, replace)
		}
		return out
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.ReplaceAll(line, search, replace)
	}
	return out
}

// ---------------------------------------------------------------------------
// Case-insensitive split/replace (matches TypeScript splitReplace)
// ---------------------------------------------------------------------------

func caseInsensitiveReplace(line, search, replace string) string {
	if search == "" {
		return line
	}
	lower := strings.ToLower(line)
	needle := strings.ToLower(search)

	var buf strings.Builder
	cursor := 0
	for {
		index := strings.Index(lower[cursor:], needle)
		if index == -1 {
			buf.WriteString(line[cursor:])
			break
		}
		buf.WriteString(line[cursor : cursor+index])
		buf.WriteString(replace)
		cursor += index + len(needle)
	}
	return buf.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func trimBlankEdges(lines []string) []string {
	start := 0
	end := len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start == 0 && end == len(lines) {
		return lines
	}
	return lines[start:end]
}

// ---------------------------------------------------------------------------
// Line diff: hunks of removed/added lines for previewing affected lines
// ---------------------------------------------------------------------------

// CleanDiffHunk groups the lines removed by a clean operation (Before) with the
// lines that replaced them (After). An empty After means the lines were simply
// removed; an empty Before means the lines were added.
type CleanDiffHunk struct {
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// diffLines computes a coarse line-level diff between the original and cleaned
// text. Line endings are normalized first so CRLF-only differences are not
// reported as changes. The result is a list of hunks of removed/added lines.
func diffLines(original, cleaned string) []CleanDiffHunk {
	a := splitLinesNormalized(original)
	b := splitLinesNormalized(cleaned)

	var hunks []CleanDiffHunk
	cur := CleanDiffHunk{Before: []string{}, After: []string{}}

	flush := func() {
		if len(cur.Before) > 0 || len(cur.After) > 0 {
			hunks = append(hunks, cur)
			cur = CleanDiffHunk{Before: []string{}, After: []string{}}
		}
	}

	// Look-ahead used to resync after a run of differing lines so adjacent
	// removals/additions group into a single hunk instead of one hunk per line.
	const resyncWindow = 16

	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			flush()
			i++
			j++
			continue
		}

		found := indexWithin(a, b[j], i, resyncWindow)
		if found > i {
			cur.Before = append(cur.Before, a[i:found]...)
			i = found
			continue
		}

		found = indexWithin(b, a[i], j, resyncWindow)
		if found > j {
			cur.After = append(cur.After, b[j:found]...)
			j = found
			continue
		}

		// No resync within the window: treat as a removal plus an addition.
		cur.Before = append(cur.Before, a[i])
		cur.After = append(cur.After, b[j])
		i++
		j++
	}
	if i < len(a) {
		cur.Before = append(cur.Before, a[i:]...)
	}
	if j < len(b) {
		cur.After = append(cur.After, b[j:]...)
	}
	flush()
	return hunks
}

// indexWithin returns the first index k in [start, start+window) where lines[k]
// equals needle, or start if not found. The window is clamped to the slice.
func indexWithin(lines []string, needle string, start, window int) int {
	end := start + window
	if end > len(lines) {
		end = len(lines)
	}
	for k := start; k < end; k++ {
		if lines[k] == needle {
			return k
		}
	}
	return start
}

func splitLinesNormalized(text string) []string {
	if text == "" {
		return nil
	}
	working := strings.ReplaceAll(text, "\r\n", "\n")
	working = strings.ReplaceAll(working, "\r", "\n")
	return strings.Split(working, "\n")
}
