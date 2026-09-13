package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// chrysGardenStubClient returns canned payloads keyed by URL so parser tests
// run without hitting the live site.
type chrysGardenStubClient struct {
	responses map[string]string
}

func (c *chrysGardenStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *chrysGardenStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *chrysGardenStubClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*chrysGardenStubClient)(nil)

const chrysGardenStubNovelURL = "https://chrysanthemumgarden.com/novel-tl/arnpc/"

const chrysGardenStubNovel = `<!DOCTYPE html><html><head>
<meta property="og:title" content="After Rebirth, I Became a Permanent NPC in the Infinite World - Chrysanthemum Garden" />
<meta property="og:description" content="Short synopsis." />
<meta property="og:image" content="https://chrysanthemumgarden.com/wp-content/uploads/2025/11/npc.jpg" />
</head><body>
<article><div class="inside-article">
<header class="entry-header"><h1 class="entry-title" itemprop="headline">After Rebirth, I Became a Permanent NPC in the Infinite World</h1>
<h1 class='novel-title'>After Rebirth, I Became a Permanent NPC in the Infinite World <span class='novel-raw-title'>重生后成为了无限世界的常驻NPC</span></h1>
<a href='https://lcread.com/bookpage/963936/index.html'>RAW Source</a><br />Author: Test Author<br />Total Chapters: 2<br />
<div class='separator'>&nbsp;</div>Translators: Yuzu<br />Release Schedule: Monday</header>
<div class="entry-content" itemprop="text">
<p>First synopsis paragraph.</p>
<p>Second synopsis paragraph.</p>
<hr />
<div id='translated-chapters' class='translated-chapters'><h3>Chapters</h3>
<ul class='list'><li class='chapter-item-wrapper'>
<a class='chapter-item' chapter-id='115338' href='https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-1/'>
<span class='chapter-item-info'><span class='chapter-item-name unread'>Chapter 1</span>
<ul class='chapter-item-details'><li><span class="date">10 months ago</span></li></ul></span></a></li>
<li class='chapter-item-wrapper'>
<a class='chapter-item' chapter-id='115339' href='https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-2/'>
<span class='chapter-item-info'><span class='chapter-item-name unread'>Chapter 2</span>
<ul class='chapter-item-details'><li><span class="date">10 months ago</span></li></ul></span></a></li>
</ul></div>
</div>
</div></article>
</body></html>`

const chrysGardenStubChapterURL = "https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-1/"

// No @font-face block: decodeProtectedSpans is a no-op here, exercising the
// junk-span filtering on its own.
const chrysGardenStubChapter = `<!DOCTYPE html><html><head>
<meta property="og:title" content="ARNPC Ch1 - - Chrysanthemum Garden" />
</head><body>
<h1 class="entry-title"><span class='chrys-post-title'><a href='https://chrysanthemumgarden.com/novel-tl/arnpc/' class='novel-title'>After Rebirth</a><span class='chapter-title'>Chapter 1</span></span></h1>
<div class="entry-content" itemprop="text">
<div id='novel-content'><p>Chapter 1</p>
<p>Plain paragraph with junk <span style='height:1px;width:0;overflow:hidden;display:inline-block'>p9U6dX</span> inside.</p>
<p style='height:1px;width:0;overflow:hidden;display:inline-block'>Story translated by Chrysanthemum Garden.</p>
<h3 style='color:transparent;height:1px;margin:0;padding:0;overflow:hidden'>CG Scrape Protection</h2>
<p>Final paragraph.</p>
</div>
</div>
</body></html>`

func TestChrysanthemumGardenCanHandle(t *testing.T) {
	p := NewChrysanthemumGardenParser()
	for _, url := range []string{
		"https://chrysanthemumgarden.com/novel-tl/arnpc/",
		"https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-1/",
		"http://chrysanthemumgarden.com/novel-tl/other-novel/other-novel-12/",
	} {
		if !p.CanHandle(url) {
			t.Errorf("CanHandle(%q) = false, want true", url)
		}
	}
	for _, url := range []string{
		"https://chrysanthemumgarden.com/",
		"https://chrysanthemumgarden.com/tag/bl/",
		"https://novelfire.net/book/some-book",
		"",
	} {
		if p.CanHandle(url) {
			t.Errorf("CanHandle(%q) = true, want false", url)
		}
	}
	if p.RequiresBrowser() {
		t.Errorf("RequiresBrowser() = true, want false (plain HTTP works)")
	}
	if p.Name() == "" {
		t.Errorf("empty parser name")
	}
}

func TestChrysanthemumGardenGetNovelInfo(t *testing.T) {
	p := NewChrysanthemumGardenParser()
	client := &chrysGardenStubClient{responses: map[string]string{
		chrysGardenStubNovelURL: chrysGardenStubNovel,
	}}

	info, err := p.GetNovelInfo(context.Background(), client, chrysGardenStubNovelURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Title != "After Rebirth, I Became a Permanent NPC in the Infinite World" {
		t.Errorf("title = %q", info.Title)
	}
	if info.Author != "Test Author" {
		t.Errorf("author = %q, want %q", info.Author, "Test Author")
	}
	if !strings.Contains(info.Description, "First synopsis paragraph.") {
		t.Errorf("description missing first paragraph:\n%s", info.Description)
	}
	if strings.Contains(info.Description, "Chapter 1") {
		t.Errorf("description leaked the chapter list:\n%s", info.Description)
	}
	if info.CoverURL != "https://chrysanthemumgarden.com/wp-content/uploads/2025/11/npc.jpg" {
		t.Errorf("cover = %q", info.CoverURL)
	}
	if len(info.Chapters) != 2 {
		t.Fatalf("chapters = %d, want 2", len(info.Chapters))
	}
	if info.Chapters[0].URL != "https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-1/" ||
		info.Chapters[0].Title != "Chapter 1" || info.Chapters[0].Order != 1 {
		t.Errorf("chapter 0 = %+v", info.Chapters[0])
	}
	if info.Chapters[1].URL != "https://chrysanthemumgarden.com/novel-tl/arnpc/arnpc-2/" ||
		info.Chapters[1].Title != "Chapter 2" || info.Chapters[1].Order != 2 {
		t.Errorf("chapter 1 = %+v", info.Chapters[1])
	}
}

func TestChrysanthemumGardenParseChapterStripsNoise(t *testing.T) {
	p := NewChrysanthemumGardenParser()
	client := &chrysGardenStubClient{responses: map[string]string{
		chrysGardenStubChapterURL: chrysGardenStubChapter,
	}}

	ch, err := p.ParseChapter(context.Background(), client, chrysGardenStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1" {
		t.Errorf("title = %q, want %q", ch.Title, "Chapter 1")
	}
	for _, noise := range []string{"p9U6dX", "Story translated by Chrysanthemum Garden.", "CG Scrape Protection"} {
		if strings.Contains(ch.Content, noise) {
			t.Errorf("content leaked scrape-protection noise %q:\n%s", noise, ch.Content)
		}
	}
	for _, want := range []string{
		"<p>Plain paragraph with junk  inside.</p>",
		"<p>Final paragraph.</p>",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestChrysanthemumGardenCreditFallback(t *testing.T) {
	// No "Author:" line: the translator credit is used instead.
	page := strings.Replace(chrysGardenStubNovel, "Author: Test Author<br />", "", 1)
	p := NewChrysanthemumGardenParser()
	client := &chrysGardenStubClient{responses: map[string]string{
		chrysGardenStubNovelURL: page,
	}}
	info, err := p.GetNovelInfo(context.Background(), client, chrysGardenStubNovelURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}
	if info.Author != "Yuzu" {
		t.Errorf("author = %q, want translator fallback %q", info.Author, "Yuzu")
	}
}

func TestDecodeCGText(t *testing.T) {
	// Synthetic ROT1 map over letters: each letter maps to the next one.
	rot1 := make(map[rune]rune)
	for c := 'A'; c <= 'Z'; c++ {
		rot1[c] = 'A' + (c-'A'+1)%26
	}
	for c := 'a'; c <= 'z'; c++ {
		rot1[c] = 'a' + (c-'a'+1)%26
	}
	got := decodeCGText("Zebra 123! …“quoted”", rot1)
	if got != "Afcsb 123! …“rvpufe”" {
		t.Errorf("decodeCGText = %q", got)
	}
}

func TestCGReferenceTableComplete(t *testing.T) {
	if len(cgReferenceGlyphs) != 52 {
		t.Fatalf("reference table has %d entries, want 52", len(cgReferenceGlyphs))
	}
	seen := make(map[rune]bool)
	for _, letter := range cgReferenceGlyphs {
		if seen[letter] {
			t.Fatalf("duplicate letter %q in reference table", letter)
		}
		seen[letter] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		if !seen[c] {
			t.Errorf("missing %q from reference table", c)
		}
	}
	for c := 'a'; c <= 'z'; c++ {
		if !seen[c] {
			t.Errorf("missing %q from reference table", c)
		}
	}
}

func TestWOFF2ScalarDecoders(t *testing.T) {
	// UIntBase128 vectors.
	for _, tc := range []struct {
		raw  []byte
		want uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x7F}, 127},
		{[]byte{0x81, 0x00}, 128},
		{[]byte{0x8F, 0xFF, 0xFF, 0xFF, 0x7F}, 0xFFFFFFFF},
	} {
		r := &woff2Reader{data: tc.raw}
		got, err := r.base128()
		if err != nil {
			t.Fatalf("base128(%x): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Errorf("base128(%x) = %d, want %d", tc.raw, got, tc.want)
		}
	}
	// 255UInt16 vectors (from the WOFF2 spec, incl. non-unique encodings).
	for _, tc := range []struct {
		raw  []byte
		want uint16
	}{
		{[]byte{252}, 252},
		{[]byte{255, 253}, 506},
		{[]byte{254, 0}, 506},
		{[]byte{253, 1, 250}, 506},
		{[]byte{253, 0x12, 0x34}, 0x1234},
	} {
		r := &woff2Reader{data: tc.raw}
		got, err := r.uint255()
		if err != nil {
			t.Fatalf("uint255(%x): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Errorf("uint255(%x) = %d, want %d", tc.raw, got, tc.want)
		}
	}
	// Triplet decoding vectors (mirror fontTools semantics).
	for _, tc := range []struct {
		flag   byte
		coords []byte
		wantDx int
		wantDy int
	}{
		{0, []byte{10}, 0, -10},      // Y-only, negative
		{1, []byte{10}, 0, 10},       // Y-only, positive
		{10, []byte{10}, -10, 0},     // X-only, negative delta 0
		{11, []byte{10}, 10, 0},      // X-only, positive delta 0
		{20, []byte{0xAB}, -11, -12}, // nibble pair, both negative
		{84, []byte{0x05, 0x07}, -6, -8},
		{120, []byte{0x12, 0x34, 0x56}, -0x123, -0x456},
		{124, []byte{0x12, 0x34, 0x56, 0x78}, -0x1234, -0x5678},
	} {
		dx, dy, err := tripletDelta(tc.flag, tc.coords)
		if err != nil {
			t.Fatalf("tripletDelta(%d): %v", tc.flag, err)
		}
		if dx != tc.wantDx || dy != tc.wantDy {
			t.Errorf("tripletDelta(%d, %x) = (%d,%d), want (%d,%d)",
				tc.flag, tc.coords, dx, dy, tc.wantDx, tc.wantDy)
		}
	}
}

func TestDecodeCGFontMappingRejectsGarbage(t *testing.T) {
	if _, err := decodeCGFontMapping([]byte("not a font")); err == nil {
		t.Errorf("expected error for garbage input")
	}
	if _, err := decodeCGFontMapping(make([]byte, 100)); err == nil {
		t.Errorf("expected error for zero input")
	}
}
