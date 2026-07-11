package epubexport

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestHashFuncNonNegativeAndNonZero(t *testing.T) {
	for _, in := range []string{"", "a", "The Lord of the Rings", strings.Repeat("z", 1000)} {
		if got := hashFunc(in); got <= 0 {
			t.Errorf("hashFunc(%q) = %d, want positive", in, got)
		}
	}
}

func TestDeterministicBookIDDiffersByMetadata(t *testing.T) {
	a := deterministicBookID(EpubMetadata{Title: "One", Author: "X"})
	b := deterministicBookID(EpubMetadata{Title: "Two", Author: "X"})
	if a == b {
		t.Error("expected different IDs for different titles")
	}
	if !strings.HasPrefix(a, "book-") {
		t.Errorf("id %q missing book- prefix", a)
	}
}

func TestMimeToExt(t *testing.T) {
	cases := map[string]string{
		"image/jpeg":               ".jpg",
		"image/jpg":                ".jpg",
		"image/png":                ".png",
		"image/webp":               ".webp",
		"application/octet-stream": ".jpg",
	}
	for mime, want := range cases {
		if got := mimeToExt(mime); got != want {
			t.Errorf("mimeToExt(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestDetectImageMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
		{"webp", []byte{0x52, 0x49, 0x46, 0x46}, "image/webp"},
		{"too short", []byte{0x01}, "image/jpeg"},
		{"unknown", []byte{0x00, 0x01, 0x02, 0x03}, "image/jpeg"},
	}
	for _, c := range cases {
		if got := DetectImageMime(c.data); got != c.want {
			t.Errorf("DetectImageMime(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDescriptionToEscapedHTML(t *testing.T) {
	got := descriptionToEscapedHTML("Para one line1\nline2\n\nPara two & more")
	if !strings.Contains(got, "&lt;p&gt;") {
		t.Errorf("expected escaped <p>, got %q", got)
	}
	if !strings.Contains(got, "&lt;br/&gt;") {
		t.Errorf("expected escaped <br/>, got %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("expected escaped ampersand, got %q", got)
	}
	if got := descriptionToEscapedHTML("   \n\n   "); got != "" {
		t.Errorf("blank description = %q, want empty", got)
	}
}

func TestReadCloserToBytes(t *testing.T) {
	got, err := ReadCloserToBytes(strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestGenerateEpubFileStructure(t *testing.T) {
	meta := EpubMetadata{
		Title:       "Generated Novel",
		Author:      "Author Name",
		Description: "A short description.",
		Language:    "es",
		Publisher:   "Yara",
		Series:      "My Series",
		Number:      "2",
	}
	chapters := []ChapterData{
		{Title: "Chapter 1", Content: "First chapter body **bold**."},
		{Title: "Chapter 2", Content: "Second chapter body."},
	}
	cover := []byte{0x89, 0x50, 0x4E, 0x47, 0x01, 0x02}

	blob, err := GenerateEpubFile(meta, chapters, cover, "image/png")
	if err != nil {
		t.Fatalf("GenerateEpubFile: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}

	present := map[string]bool{}
	var mimetype *zip.File
	for _, f := range zr.File {
		present[f.Name] = true
		if f.Name == "mimetype" {
			mimetype = f
		}
	}

	required := []string{
		"mimetype",
		"META-INF/container.xml",
		"OEBPS/content.opf",
		"OEBPS/toc.ncx",
		"OEBPS/toc.xhtml",
		"OEBPS/css/styles.css",
		"OEBPS/cover.png",
		"OEBPS/cover.xhtml",
		"OEBPS/chapter1.xhtml",
		"OEBPS/chapter2.xhtml",
	}
	for _, name := range required {
		if !present[name] {
			t.Errorf("epub missing entry %q", name)
		}
	}

	if mimetype == nil {
		t.Fatal("mimetype entry missing")
	}
	if mimetype.Method != zip.Store {
		t.Errorf("mimetype must be stored uncompressed, got method %d", mimetype.Method)
	}

	opf := readZipEntry(t, zr, "OEBPS/content.opf")
	if !strings.Contains(opf, "<dc:title>Generated Novel</dc:title>") {
		t.Error("content.opf missing title")
	}
	if !strings.Contains(opf, "calibre:series") {
		t.Error("content.opf missing series metadata")
	}
	if !strings.Contains(opf, `properties="cover-image"`) {
		t.Error("content.opf missing cover-image manifest entry")
	}

	ch1 := readZipEntry(t, zr, "OEBPS/chapter1.xhtml")
	if !strings.Contains(ch1, "<b>bold</b>") {
		t.Error("chapter1 missing processed markdown")
	}
	if !strings.Contains(ch1, "Chapter 1") {
		t.Error("chapter1 missing title")
	}
}

func TestGenerateEpubFileNoCover(t *testing.T) {
	blob, err := GenerateEpubFile(
		EpubMetadata{Title: "No Cover", Language: "en"},
		[]ChapterData{{Title: "Only", Content: "body"}},
		nil, "",
	)
	if err != nil {
		t.Fatalf("GenerateEpubFile: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "OEBPS/cover") {
			t.Errorf("did not expect cover entry %q when no cover supplied", f.Name)
		}
	}
}

func readZipEntry(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			buf := new(bytes.Buffer)
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			return buf.String()
		}
	}
	t.Fatalf("entry %q not found", name)
	return ""
}
