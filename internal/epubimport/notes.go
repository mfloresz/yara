package epubimport

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	html2md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// NotesChapterTitle is the title of the single chapter that collects every
// note extracted from Calibre-style notes files.
const NotesChapterTitle = "Notas"

// Note is one extracted footnote: its number (from the note id, the marker
// sup or the source file name) and its markdown text.
type Note struct {
	Num  int
	Text string
}

var (
	// reNotaBlock matches Calibre's note containers: div/p whose class names
	// the content as a note ("nota", "notas", ...).
	reNotaBlock = regexp.MustCompile(`(?is)<(?:div|p)\b[^>]*class="[^"]*\bnota\b[^"]*"[^>]*>.*?</(?:div|p)>`)
	reHeadBlock = regexp.MustCompile(`(?is)<head\b[^>]*>.*?</head>`)

	noteIDNumRe   = regexp.MustCompile(`id="nt(\d+)"`)
	noteFileNumRe = regexp.MustCompile(`notas[_-]split[_-]?(\d+)`)
	noteSupNumRe  = regexp.MustCompile(`<sup[^>]*>\s*\[?(\d+)\]?\s*</sup>`)

	// leadingNoteMarker strips the "[N]" / "[*]" marker sup the conversion
	// leaves at the start of each note (the number is kept separately).
	leadingNoteMarker = regexp.MustCompile(`^\\\[[^\]]*\\\]?`)
	// noteBacklink strips the dead "<<" backlinks Calibre appends to each
	// note pointing back at the chapter file.
	noteBacklink = regexp.MustCompile(`\s*\[[<>]+\]\([^)]*\)`)
	// noteRefLink rewrites in-text dead note links such as
	// [\[241\]](notas_split_241.xhtml#nt241) into a plain escaped marker.
	noteRefLink = regexp.MustCompile(`\[(\\\[)?(\d+)(\\\])?\]\([^)]*notas[^)]*\)`)
)

// extractNoteFile returns the notes contained in a spine file and reports
// whether the file is notes-only, i.e. carries no prose beyond the note
// containers (a chapter with inline notes is NOT notes-only and is imported
// as a regular chapter).
func extractNoteFile(htmlString, htmlPath string) ([]Note, bool) {
	if !strings.Contains(strings.ToLower(htmlString), "nota") {
		return nil, false
	}
	blocks := reNotaBlock.FindAllString(htmlString, -1)
	if len(blocks) == 0 {
		return nil, false
	}
	rest := reNotaBlock.ReplaceAllString(htmlString, "")
	rest = reHeadBlock.ReplaceAllString(rest, "")
	restText := strings.TrimSpace(reTagStrip.ReplaceAllString(rest, ""))
	if len(restText) > 40 {
		return nil, false
	}

	fileNum := 0
	if m := noteFileNumRe.FindStringSubmatch(htmlPath); m != nil {
		fileNum, _ = strconv.Atoi(m[1])
	}
	var notes []Note
	for _, block := range blocks {
		text, err := html2md.ConvertString(block)
		if err != nil {
			continue
		}
		text = leadingNoteMarker.ReplaceAllString(text, "")
		text = noteBacklink.ReplaceAllString(text, "")
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		num := 0
		if m := noteIDNumRe.FindStringSubmatch(block); m != nil {
			num, _ = strconv.Atoi(m[1])
		}
		if num == 0 {
			if m := noteSupNumRe.FindStringSubmatch(block); m != nil {
				num, _ = strconv.Atoi(m[1])
			}
		}
		if num == 0 {
			num = fileNum
		}
		notes = append(notes, Note{Num: num, Text: text})
	}
	return notes, len(notes) > 0
}

// buildNotesChapter folds the collected notes into one end-of-book chapter,
// ordered by note number.
func buildNotesChapter(notes []Note) Chapter {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Num < notes[j].Num })
	var b strings.Builder
	for _, n := range notes {
		b.WriteString("- \\[" + strconv.Itoa(n.Num) + "\\] " + n.Text + "\n")
	}
	return Chapter{Title: NotesChapterTitle, Content: normalizeMarkdown(b.String())}
}

// cleanNoteRefLinks replaces the dead note links the markdown conversion
// produces for in-text markers (e.g. [\[241\]](notas_split_241.xhtml#nt241))
// with the plain escaped marker [241], so nothing in the text points at a
// file that no longer exists.
func cleanNoteRefLinks(md string) string {
	return noteRefLink.ReplaceAllString(md, "\\[${2}\\]")
}
