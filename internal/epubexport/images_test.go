package epubexport

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestProcessChapterWithImages(t *testing.T) {
	images := map[string]ImageFile{
		"[[IMG-1]]": {Name: "c1i1.jpg", Alt: `Alt with "quotes" & <tags>`},
		"[[IMG-2]]": {Name: "c1i2.png", Alt: "Second"},
	}
	got := ProcessChapterWithImages("Before.\n\n[[IMG-1]]\n\n[[IMG-9]]\n\nAfter [[IMG-2]] inline.", images)
	if !strings.Contains(got, `<p><img src="images/c1i1.jpg" alt="Alt with &quot;quotes&quot; &amp; &lt;tags&gt;"/></p>`) {
		t.Errorf("token 1 should become an escaped <img> block, got:\n%s", got)
	}
	if !strings.Contains(got, `<img src="images/c1i2.png" alt="Second"/>`) {
		t.Errorf("inline token 2 should become an <img>, got:\n%s", got)
	}
	if strings.Contains(got, "[[IMG-9]]") {
		t.Errorf("unresolved token should be dropped, got:\n%s", got)
	}
	if strings.Contains(got, "[[IMG-1]]") || strings.Contains(got, "[[IMG-2]]") {
		t.Errorf("resolved tokens should not leak as text, got:\n%s", got)
	}

	// nil map keeps the plain conversion (tokens stay as text)
	if plain := ProcessChapter("A [[IMG-1]] B"); !strings.Contains(plain, "[[IMG-1]]") {
		t.Errorf("plain ProcessChapter should keep tokens as text, got %q", plain)
	}
}

func TestGenerateEpubFileEmbedsChapterImages(t *testing.T) {
	meta := EpubMetadata{Title: "Image Book", Language: "es"}
	chapters := []ChapterData{
		{
			Title:   "Chapter 1",
			Content: "Body one.\n\n[[IMG-1]]\n\nBody two [[IMG-2]].",
			Images: []ImageFile{
				{MimeType: "image/jpeg", Alt: "First", Blob: []byte{0xFF, 0xD8, 0xFF, 0xE0}},
				{MimeType: "image/png", Alt: "Second", Blob: []byte{0x89, 0x50, 0x4E, 0x47}},
			},
		},
		{
			Title:   "Chapter 2",
			Content: "No images here.",
		},
	}

	blob, err := GenerateEpubFile(meta, chapters, []byte{0x89, 0x50, 0x4E, 0x47, 0x01}, "image/png")
	if err != nil {
		t.Fatalf("GenerateEpubFile: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, f := range zr.File {
		present[f.Name] = true
	}
	for _, name := range []string{"OEBPS/images/c1i1.jpg", "OEBPS/images/c1i2.png"} {
		if !present[name] {
			t.Errorf("epub missing image entry %q (have %v)", name, present)
		}
	}

	opf := readZipEntry(t, zr, "OEBPS/content.opf")
	if !strings.Contains(opf, `<item id="img-c1i1" href="images/c1i1.jpg" media-type="image/jpeg"/>`) {
		t.Errorf("manifest missing image item, opf:\n%s", opf)
	}
	ch1 := readZipEntry(t, zr, "OEBPS/chapter1.xhtml")
	if !strings.Contains(ch1, `<img src="images/c1i1.jpg" alt="First"/>`) {
		t.Errorf("chapter1 missing embedded <img>, got:\n%s", ch1)
	}
	if strings.Contains(ch1, "[[IMG-") {
		t.Errorf("chapter1 must not leak tokens, got:\n%s", ch1)
	}
}
