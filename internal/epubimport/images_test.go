package epubimport

import (
	"strings"
	"testing"
)

const imagesOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="book-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Image Novel</dc:title>
    <dc:creator>Jane Author</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="img-a" href="images/a.jpg" media-type="image/jpeg"/>
    <item id="img-b" href="images/b.png" media-type="image/png"/>
    <item id="ch1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="ch1"/>
  </spine>
</package>`

func chapterWithImages(body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter 1</title></head>
<body><h1>Chapter 1</h1>` + body + `</body></html>`
}

func TestParseExtractsImagesWithTokens(t *testing.T) {
	longBody := "This is a sufficiently long paragraph of chapter text so the importer does not discard it as boilerplate content. It keeps going for a while."
	chapterBody := `<p>` + longBody + `</p>` +
		`<p><img src="images/a.jpg" alt="Alt A"/></p>` +
		`<p><img src="images/b.png" alt="Alt B"/></p>` +
		`<p>Between paragraphs.</p>` +
		`<p><img src="images/a.jpg" alt="Alt A"/></p>` +
		`<p><img src="images/missing.jpg" alt="Dead"/></p>` +
		`<p>` + longBody + `</p>`
	files := map[string]string{
		"META-INF/container.xml": testContainerXML,
		"OEBPS/content.opf":      imagesOPF,
		"OEBPS/images/a.jpg":     "jpeg-bytes-a",
		"OEBPS/images/b.png":     "png-bytes-b",
		"OEBPS/chap1.xhtml":      chapterWithImages(chapterBody),
	}

	result, err := Parse(buildEPUB(t, files), "test.epub")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Chapters) != 1 {
		t.Fatalf("chapters = %d, want 1", len(result.Chapters))
	}
	ch := result.Chapters[0]

	if strings.Count(ch.Content, "[[IMG-1]]") != 2 {
		t.Errorf("content should reference [[IMG-1]] twice, got:\n%s", ch.Content)
	}
	if strings.Count(ch.Content, "[[IMG-2]]") != 1 {
		t.Errorf("content should reference [[IMG-2]] once, got:\n%s", ch.Content)
	}
	if strings.Contains(ch.Content, "[[IMG-3]]") {
		t.Errorf("unresolvable image must not get a token:\n%s", ch.Content)
	}
	if strings.Contains(ch.Content, "IMGREF-") {
		t.Errorf("intermediate markers must not survive into content:\n%s", ch.Content)
	}

	if len(ch.Images) != 2 {
		t.Fatalf("images = %d, want 2", len(ch.Images))
	}
	if ch.Images[0].Token != "[[IMG-1]]" || ch.Images[0].MimeType != "image/jpeg" ||
		ch.Images[0].Alt != "Alt A" || string(ch.Images[0].Blob) != "jpeg-bytes-a" {
		t.Errorf("images[0] = %+v, want token [[IMG-1]] jpeg Alt A blob jpeg-bytes-a", ch.Images[0])
	}
	if ch.Images[1].Token != "[[IMG-2]]" || ch.Images[1].MimeType != "image/png" ||
		ch.Images[1].Alt != "Alt B" || string(ch.Images[1].Blob) != "png-bytes-b" {
		t.Errorf("images[1] = %+v, want token [[IMG-2]] png Alt B blob png-bytes-b", ch.Images[1])
	}
}

func TestParseImageOverPerImageLimit(t *testing.T) {
	chapterBody := `<p>This is a sufficiently long paragraph of chapter text so the importer keeps the chapter.</p>` +
		`<p><img src="images/huge.jpg" alt="Huge"/></p>`
	files := map[string]string{
		"META-INF/container.xml": testContainerXML,
		"OEBPS/content.opf":      imagesOPF,
		"OEBPS/images/huge.jpg":  strings.Repeat("x", int(MaxImageBytes)+1),
		"OEBPS/chap1.xhtml":      chapterWithImages(chapterBody),
	}
	// imagesOPF references images/a.jpg; the huge fixture needs its own manifest
	// entry, so patch it in place of a.jpg via a dedicated OPF.
	files["OEBPS/content.opf"] = strings.Replace(imagesOPF, "images/a.jpg", "images/huge.jpg", 1)

	_, err := Parse(buildEPUB(t, files), "test.epub")
	if err == nil {
		t.Fatal("expected per-image limit error, got nil")
	}
	if !strings.Contains(err.Error(), "per-image limit") {
		t.Errorf("unexpected error: %v", err)
	}
}
