package epubimport

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildEPUB assembles a minimal but valid EPUB zip from the given files.
// The mimetype entry is written first and stored (uncompressed) per spec.
func buildEPUB(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	f, err := w.CreateHeader(mh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}

	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const testContainerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const testOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="book-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Test Novel</dc:title>
    <dc:creator>Jane Author</dc:creator>
    <dc:language>en</dc:language>
    <dc:description>A gripping tale.</dc:description>
    <meta name="calibre:series" content="Copper Lake"/>
    <meta name="calibre:series_index" content="3.0"/>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="chap2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
  </spine>
</package>`

func chapterHTML(title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title></head>
<body><h1>` + title + `</h1><p>` + body + `</p></body></html>`
}

func TestParseFullEPUB(t *testing.T) {
	longBody := "This is a sufficiently long paragraph of chapter text so the importer does not discard it as boilerplate content. It keeps going for a while."
	files := map[string]string{
		"META-INF/container.xml": testContainerXML,
		"OEBPS/content.opf":      testOPF,
		"OEBPS/cover.jpg":        "\xFF\xD8\xFFfake-jpeg-bytes",
		"OEBPS/chap1.xhtml":      chapterHTML("Chapter 1", longBody),
		"OEBPS/chap2.xhtml":      chapterHTML("Chapter 2", longBody),
	}
	blob := buildEPUB(t, files)

	result, err := Parse(blob, "test.epub")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if result.Title != "The Test Novel" {
		t.Errorf("Title = %q, want %q", result.Title, "The Test Novel")
	}
	if result.Author != "Jane Author" {
		t.Errorf("Author = %q, want %q", result.Author, "Jane Author")
	}
	if result.Language != "en" {
		t.Errorf("Language = %q, want %q", result.Language, "en")
	}
	if result.Series != "Copper Lake" {
		t.Errorf("Series = %q, want %q", result.Series, "Copper Lake")
	}
	if result.Number != "3.0" {
		t.Errorf("Number = %q, want %q", result.Number, "3.0")
	}
	if result.Description == "" {
		t.Error("expected non-empty description")
	}
	if len(result.CoverBlob) == 0 {
		t.Error("expected cover blob")
	}
	if result.CoverMime != "image/jpeg" {
		t.Errorf("CoverMime = %q, want image/jpeg", result.CoverMime)
	}
	if len(result.Chapters) < 2 {
		t.Fatalf("expected at least 2 chapters, got %d", len(result.Chapters))
	}
	for i, ch := range result.Chapters {
		if ch.Title == "" {
			t.Errorf("chapter %d has empty title", i)
		}
		if ch.Content == "" {
			t.Errorf("chapter %d has empty content", i)
		}
	}
}

func TestParseFallsBackToFilenameTitle(t *testing.T) {
	opf := `<?xml version="1.0"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="ch1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
  </spine>
</package>`
	body := "A long enough body paragraph to survive the boilerplate filter which discards very short chapter fragments outright."
	files := map[string]string{
		"META-INF/container.xml": testContainerXML,
		"OEBPS/content.opf":      opf,
		"OEBPS/chap1.xhtml":      chapterHTML("Chapter 1", body),
	}
	result, err := Parse(buildEPUB(t, files), "MyBook.epub")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.Title != "MyBook" {
		t.Errorf("Title = %q, want fallback %q", result.Title, "MyBook")
	}
}

func TestParseErrors(t *testing.T) {
	t.Run("not a zip", func(t *testing.T) {
		if _, err := Parse([]byte("plain text, not a zip"), "x.epub"); err == nil {
			t.Error("expected error for non-zip input")
		}
	})

	t.Run("missing container", func(t *testing.T) {
		blob := buildEPUB(t, map[string]string{"OEBPS/content.opf": testOPF})
		if _, err := Parse(blob, "x.epub"); err == nil {
			t.Error("expected error for missing container.xml")
		}
	})

	t.Run("missing opf", func(t *testing.T) {
		blob := buildEPUB(t, map[string]string{"META-INF/container.xml": testContainerXML})
		if _, err := Parse(blob, "x.epub"); err == nil {
			t.Error("expected error for missing OPF package")
		}
	})

	t.Run("no readable chapters", func(t *testing.T) {
		files := map[string]string{
			"META-INF/container.xml": testContainerXML,
			"OEBPS/content.opf":      testOPF,
			"OEBPS/chap1.xhtml":      chapterHTML("Chapter 1", "too short"),
			"OEBPS/chap2.xhtml":      chapterHTML("Chapter 2", "also short"),
		}
		if _, err := Parse(buildEPUB(t, files), "x.epub"); err == nil {
			t.Error("expected error when no readable chapters remain")
		}
	})
}

func TestParseContainer(t *testing.T) {
	got, err := parseContainer(testContainerXML)
	if err != nil {
		t.Fatal(err)
	}
	if got != "OEBPS/content.opf" {
		t.Errorf("got %q, want OEBPS/content.opf", got)
	}

	if _, err := parseContainer(`<container></container>`); err == nil {
		t.Error("expected error for container without rootfile")
	}
}

func TestParseManifestAndSpine(t *testing.T) {
	items := parseManifest(testOPF)
	if len(items) != 4 {
		t.Fatalf("parseManifest returned %d items, want 4", len(items))
	}
	byID := map[string]manifestItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if byID["cover-img"].MediaType != "image/jpeg" {
		t.Errorf("cover-img media type = %q", byID["cover-img"].MediaType)
	}
	if byID["ch1"].Href != "chap1.xhtml" {
		t.Errorf("ch1 href = %q", byID["ch1"].Href)
	}

	spine := parseSpine(testOPF)
	want := []string{"ch1", "ch2"}
	if len(spine) != len(want) {
		t.Fatalf("parseSpine = %v, want %v", spine, want)
	}
	for i := range want {
		if spine[i] != want[i] {
			t.Errorf("spine[%d] = %q, want %q", i, spine[i], want[i])
		}
	}
}

func TestParseMetadataFields(t *testing.T) {
	md := parseMetadata(testOPF)
	if got := firstNonEmpty(md["title"]...); got != "The Test Novel" {
		t.Errorf("title = %q", got)
	}
	if got := firstNonEmpty(md["creator"]...); got != "Jane Author" {
		t.Errorf("creator = %q", got)
	}
	if got := firstNonEmpty(md["series"]...); got != "Copper Lake" {
		t.Errorf("series = %q", got)
	}
	if got := firstNonEmpty(md["number"]...); got != "3.0" {
		t.Errorf("number = %q", got)
	}
}

func TestParseMetadataCollectionFallback(t *testing.T) {
	opf := `<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>X</dc:title>
  <meta property="belongs-to-collection" id="c">Saga Name</meta>
  <meta refines="#c" property="group-position">2</meta>
</metadata>`
	md := parseMetadata(opf)
	if got := firstNonEmpty(md["series"]...); got != "Saga Name" {
		t.Errorf("series = %q, want Saga Name", got)
	}
	if got := firstNonEmpty(md["number"]...); got != "2" {
		t.Errorf("number = %q, want 2", got)
	}
}

func TestParseCoverID(t *testing.T) {
	if got := parseCoverID(testOPF); got != "cover-img" {
		t.Errorf("parseCoverID = %q, want cover-img", got)
	}
	if got := parseCoverID(`<metadata></metadata>`); got != "" {
		t.Errorf("parseCoverID = %q, want empty", got)
	}
}

func TestFindCover(t *testing.T) {
	items := []manifestItem{
		{ID: "img1", Href: "images/pic.png", MediaType: "image/png"},
		{ID: "the-cover", Href: "cover.jpg", MediaType: "image/jpeg"},
	}

	t.Run("by id", func(t *testing.T) {
		got := findCover(items, "the-cover")
		if got == nil || got.ID != "the-cover" {
			t.Errorf("findCover by id = %+v", got)
		}
	})

	t.Run("by properties", func(t *testing.T) {
		withProp := []manifestItem{
			{ID: "a", Href: "a.png", MediaType: "image/png", Properties: "cover-image"},
		}
		got := findCover(withProp, "")
		if got == nil || got.ID != "a" {
			t.Errorf("findCover by properties = %+v", got)
		}
	})

	t.Run("by filename", func(t *testing.T) {
		got := findCover(items, "")
		if got == nil || got.ID != "the-cover" {
			t.Errorf("findCover by filename = %+v", got)
		}
	})

	t.Run("none", func(t *testing.T) {
		none := []manifestItem{{ID: "t", Href: "text.xhtml", MediaType: "application/xhtml+xml"}}
		if got := findCover(none, ""); got != nil {
			t.Errorf("findCover = %+v, want nil", got)
		}
	})
}

func TestNormalizeMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"line1\r\nline2", "line1\nline2"},
		{"a\r\rb", "a\n\nb"},
		{"nbsp\u00a0here", "nbsp here"},
		{"  trimmed  ", "trimmed"},
		{"a\n\n\n\n\nb", "a\n\nb"},
	}
	for _, c := range cases {
		if got := normalizeMarkdown(c.in); got != c.want {
			t.Errorf("normalizeMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRemoveScriptTags(t *testing.T) {
	in := `<p>keep</p><script>alert(1)</script><script src="x.js"/><p>keep2</p>`
	got := removeScriptTags(in)
	if want := `<p>keep</p><p>keep2</p>`; got != want {
		t.Errorf("removeScriptTags = %q, want %q", got, want)
	}
}

func TestStripLeadingMarkdownHeading(t *testing.T) {
	cases := []struct{ in, want string }{
		{"# Title\nbody text", "body text"},
		{"## Heading\n\nrest", "rest"},
		{"no heading here", "no heading here"},
		{"# Only heading", ""},
	}
	for _, c := range cases {
		if got := stripLeadingMarkdownHeading(c.in); got != c.want {
			t.Errorf("stripLeadingMarkdownHeading(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "found", "later"); got != "found" {
		t.Errorf("firstNonEmpty = %q, want found", got)
	}
	if got := firstNonEmpty("", "   "); got != "" {
		t.Errorf("firstNonEmpty = %q, want empty", got)
	}
	if got := firstNonEmpty("  spaced  "); got != "spaced" {
		t.Errorf("firstNonEmpty trims = %q, want spaced", got)
	}
}

func TestExtractTitle(t *testing.T) {
	t.Run("from heading", func(t *testing.T) {
		html := `<html><body><h2>Real Heading</h2></body></html>`
		if got := extractTitle(html, "c1.xhtml", 1); got != "Real Heading" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("from title tag", func(t *testing.T) {
		html := `<html><head><title>Doc Title</title></head><body>text</body></html>`
		if got := extractTitle(html, "c1.xhtml", 1); got != "Doc Title" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("from filename", func(t *testing.T) {
		if got := extractTitle(`<p>no titles</p>`, "path/my-chapter_two.xhtml", 5); got != "my chapter two" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("index fallback", func(t *testing.T) {
		if got := extractTitle(`<p>x</p>`, ".xhtml", 7); got != "Capítulo 7" {
			t.Errorf("got %q, want Capítulo 7", got)
		}
	})
}

func TestLooksLikeChapter(t *testing.T) {
	trueCases := []string{"Chapter 1", "Capítulo 5", "PROLOGUE", "Epílogo", "Part II", "Book One", "Act 3"}
	for _, c := range trueCases {
		if !looksLikeChapter(c) {
			t.Errorf("looksLikeChapter(%q) = false, want true", c)
		}
	}
	falseCases := []string{"Introduction blurb", "About the author", "Random"}
	for _, c := range falseCases {
		if looksLikeChapter(c) {
			t.Errorf("looksLikeChapter(%q) = true, want false", c)
		}
	}
}

func TestIsHTMLItem(t *testing.T) {
	if !isHTMLItem(manifestItem{MediaType: "application/xhtml+xml"}) {
		t.Error("xhtml should be html item")
	}
	if !isHTMLItem(manifestItem{MediaType: "text/html"}) {
		t.Error("html should be html item")
	}
	if isHTMLItem(manifestItem{MediaType: "image/png"}) {
		t.Error("png should not be html item")
	}
}

func TestShouldSkipManifestItem(t *testing.T) {
	skip := []manifestItem{
		{Href: "toc.xhtml"},
		{Href: "nav.xhtml"},
		{Href: "cover.xhtml"},
		{Href: "contents.xhtml"},
		{Href: "x.xhtml", Properties: "nav"},
	}
	for _, it := range skip {
		if !shouldSkipManifestItem(it) {
			t.Errorf("shouldSkipManifestItem(%+v) = false, want true", it)
		}
	}
	if shouldSkipManifestItem(manifestItem{Href: "chapter1.xhtml"}) {
		t.Error("real chapter should not be skipped")
	}
}

func TestResolveZipPath(t *testing.T) {
	cases := []struct{ opf, href, want string }{
		{"OEBPS/content.opf", "chap1.xhtml", "OEBPS/chap1.xhtml"},
		{"OEBPS/content.opf", "../images/cover.jpg", "images/cover.jpg"},
		{"content.opf", "chap1.xhtml", "chap1.xhtml"},
		{"content.opf", "sub/chap.xhtml", "sub/chap.xhtml"},
	}
	for _, c := range cases {
		if got := resolveZipPath(c.opf, c.href); got != c.want {
			t.Errorf("resolveZipPath(%q, %q) = %q, want %q", c.opf, c.href, got, c.want)
		}
	}
}

func TestExtractAttr(t *testing.T) {
	attrs := `id="ch1" href='chap1.xhtml' media-type="application/xhtml+xml"`
	if got := extractAttr(attrs, "id"); got != "ch1" {
		t.Errorf("id = %q", got)
	}
	if got := extractAttr(attrs, "href"); got != "chap1.xhtml" {
		t.Errorf("href = %q", got)
	}
	if got := extractAttr(attrs, "missing"); got != "" {
		t.Errorf("missing attr = %q, want empty", got)
	}
}

func TestNormalizeInlineText(t *testing.T) {
	in := `<b>Bold</b>   &amp; more   text`
	if got := normalizeInlineText(in); got != "Bold & more text" {
		t.Errorf("normalizeInlineText = %q", got)
	}
}
