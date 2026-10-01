package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"

	"translator-server/internal/parserhost"
)

// The chrysanthemumgarden helper is the one piece of the parser pipeline that
// must never fail silently: an undecodable protection font has to fail the
// fetch, and a decodable one has to hand the script real letters. These tests
// build a minimal WOFF2 font shaped exactly like the cg-scrape-protection
// fonts (cmap format 4, raw hmtx, transformed glyf with explicit bboxes) so
// the whole decode pipeline runs end to end without a network fixture.

// appendBase128 encodes a WOFF2 UIntBase128 value.
func appendBase128(b []byte, v uint32) []byte {
	var groups [5]byte
	n := 0
	groups[n] = byte(v & 0x7F)
	v >>= 7
	n++
	for v > 0 {
		groups[n] = byte(0x80 | (v & 0x7F))
		v >>= 7
		n++
	}
	for i := n - 1; i >= 0; i-- {
		b = append(b, groups[i])
	}
	return b
}

// cgReferenceKey reverse-looks-up the (bbox, advance) a reference letter is
// identified by.
func cgReferenceKey(t *testing.T, letter rune) cgGlyphKey {
	t.Helper()
	for key, l := range cgReferenceGlyphs {
		if l == letter {
			return key
		}
	}
	t.Fatalf("letter %q not in the reference table", letter)
	return cgGlyphKey{}
}

// buildTestWOFF2 compiles a protection-style font: every slot rune maps
// (through its glyph's geometry) to the given true letter.
func buildTestWOFF2(t *testing.T, slots map[rune]rune) []byte {
	t.Helper()

	type glyph struct {
		slot rune
		gid  uint16
		key  cgGlyphKey
	}
	// Glyph IDs are assigned from the sorted slot runes so the cmap segments
	// come out ascending, as format 4 requires.
	letters := make([]rune, 0, len(slots))
	for slot := range slots {
		letters = append(letters, slot)
	}
	for i := 0; i < len(letters); i++ {
		for j := i + 1; j < len(letters); j++ {
			if letters[j] < letters[i] {
				letters[i], letters[j] = letters[j], letters[i]
			}
		}
	}
	glyphs := make([]glyph, 0, len(letters))
	for i, slot := range letters {
		glyphs = append(glyphs, glyph{slot: slot, gid: uint16(i + 1), key: cgReferenceKey(t, slots[slot])})
	}
	numGlyphs := len(glyphs) + 1 // gid 0 is .notdef

	// cmap: one segment per slot, delta-encoded to its glyph id, plus the
	// 0xFFFF terminator segment. Every array carries segCount entries.
	segCount := len(glyphs) + 1
	sub := []byte{}
	sub = binary.BigEndian.AppendUint16(sub, 4)  // format
	sub = binary.BigEndian.AppendUint16(sub, 0)  // length, patched below
	sub = binary.BigEndian.AppendUint16(sub, 0)  // language
	sub = binary.BigEndian.AppendUint16(sub, uint16(segCount*2))
	sub = binary.BigEndian.AppendUint16(sub, 0) // searchRange
	sub = binary.BigEndian.AppendUint16(sub, 0) // entrySelector
	sub = binary.BigEndian.AppendUint16(sub, 0) // rangeShift
	for _, g := range glyphs {
		sub = binary.BigEndian.AppendUint16(sub, uint16(g.slot)) // endCode
	}
	sub = binary.BigEndian.AppendUint16(sub, 0xFFFF) // terminator endCode
	sub = binary.BigEndian.AppendUint16(sub, 0)      // reservedPad
	for _, g := range glyphs {
		sub = binary.BigEndian.AppendUint16(sub, uint16(g.slot)) // startCode
	}
	sub = binary.BigEndian.AppendUint16(sub, 0xFFFF) // terminator startCode
	for _, g := range glyphs {
		sub = binary.BigEndian.AppendUint16(sub, uint16(int(g.gid)-int(g.slot))) // idDelta
	}
	sub = binary.BigEndian.AppendUint16(sub, 1) // terminator idDelta
	for i := 0; i < segCount; i++ {
		sub = binary.BigEndian.AppendUint16(sub, 0) // idRangeOffset
	}
	binary.BigEndian.PutUint16(sub[2:4], uint16(len(sub)))

	cmap := []byte{}
	cmap = binary.BigEndian.AppendUint16(cmap, 0) // version
	cmap = binary.BigEndian.AppendUint16(cmap, 1) // numTables
	cmap = binary.BigEndian.AppendUint16(cmap, 3) // platformID
	cmap = binary.BigEndian.AppendUint16(cmap, 1) // encodingID
	cmap = binary.BigEndian.AppendUint32(cmap, 12)
	cmap = append(cmap, sub...)

	// hhea: 36 bytes, numHMetrics at [34:36].
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], uint16(numGlyphs))

	// hmtx raw: one (advance, lsb) pair per glyph.
	hmtx := []byte{}
	hmtx = binary.BigEndian.AppendUint16(hmtx, 0) // .notdef
	hmtx = binary.BigEndian.AppendUint16(hmtx, 0)
	for _, g := range glyphs {
		hmtx = binary.BigEndian.AppendUint16(hmtx, g.key.Advance)
		hmtx = binary.BigEndian.AppendUint16(hmtx, 0)
	}

	// maxp version 0.5: version + numGlyphs.
	maxp := []byte{}
	maxp = binary.BigEndian.AppendUint32(maxp, 0x00005000)
	maxp = binary.BigEndian.AppendUint16(maxp, uint16(numGlyphs))

	// glyf, transformed layout: every real glyph is a single one-point contour
	// whose true bbox is carried by the explicit-bbox bitmap (points contribute
	// nothing; only the reference geometry matters).
	nReal := len(glyphs)
	bboxBitmapSize := ((numGlyphs + 31) >> 5) << 2
	bboxBitmap := make([]byte, bboxBitmapSize)
	for _, g := range glyphs {
		bboxBitmap[g.gid>>3] |= 0x80 >> (g.gid & 7)
	}
	bboxes := []byte{}
	for _, g := range glyphs {
		bboxes = binary.BigEndian.AppendUint16(bboxes, uint16(g.key.XMin))
		bboxes = binary.BigEndian.AppendUint16(bboxes, uint16(g.key.YMin))
		bboxes = binary.BigEndian.AppendUint16(bboxes, uint16(g.key.XMax))
		bboxes = binary.BigEndian.AppendUint16(bboxes, uint16(g.key.YMax))
	}
	s0 := []byte{} // nContours: int16 per glyph
	s0 = binary.BigEndian.AppendUint16(s0, 0)
	for i := 0; i < nReal; i++ {
		s0 = binary.BigEndian.AppendUint16(s0, 1)
	}
	s1 := bytes.Repeat([]byte{0x01}, nReal)               // nPoints: one per contour
	s2 := bytes.Repeat([]byte{0x0B}, nReal)               // flags: flag 11 → 1 coord byte
	s3 := bytes.Repeat([]byte{0x00, 0x00}, nReal)         // coords (0,0) + empty instructions
	s4 := []byte{}                                        // composites: none
	s5 := append(bboxBitmap, bboxes...)                   // explicit bboxes
	s6 := bytes.Repeat([]byte{0x00}, nReal)               // instruction bytes
	glyf := []byte{}
	glyf = binary.BigEndian.AppendUint16(glyf, 0) // transformed version
	glyf = binary.BigEndian.AppendUint16(glyf, 0) // optionFlags
	glyf = binary.BigEndian.AppendUint16(glyf, uint16(numGlyphs))
	glyf = binary.BigEndian.AppendUint16(glyf, 0) // indexFormat
	for _, sz := range []int{len(s0), len(s1), len(s2), len(s3), len(s4), len(s5), len(s6)} {
		glyf = binary.BigEndian.AppendUint32(glyf, uint32(sz))
	}
	glyf = append(glyf, s0...)
	glyf = append(glyf, s1...)
	glyf = append(glyf, s2...)
	glyf = append(glyf, s3...)
	glyf = append(glyf, s4...)
	glyf = append(glyf, s5...)
	glyf = append(glyf, s6...)

	// Table directory + brotli payload, in matching order.
	type dirEntry struct {
		flags       byte
		origLength  int
		transformed bool
		data        []byte
	}
	entries := []dirEntry{
		{flags: 0x00, data: cmap}, // cmap, untransformed
		{flags: 0x02, data: hhea}, // hhea
		{flags: 0x03, data: hmtx}, // hmtx
		{flags: 0x04, data: maxp}, // maxp
		{flags: 0x40 | 0x0A, origLength: len(glyf), transformed: true, data: glyf}, // glyf, transformed
	}
	for i := range entries {
		entries[i].origLength = len(entries[i].data)
	}

	dir := []byte{}
	for _, e := range entries {
		dir = append(dir, e.flags)
		dir = appendBase128(dir, uint32(e.origLength))
		if e.transformed {
			dir = appendBase128(dir, uint32(e.origLength))
		}
	}
	payload := []byte{}
	for _, e := range entries {
		payload = append(payload, e.data...)
	}
	var compressed bytes.Buffer
	w := brotli.NewWriter(&compressed)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("brotli write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("brotli close: %v", err)
	}

	font := []byte("wOF2")
	font = binary.BigEndian.AppendUint32(font, 0x00010000)               // flavor
	font = binary.BigEndian.AppendUint32(font, 0)                        // length (unused here)
	font = binary.BigEndian.AppendUint16(font, uint16(len(entries)))     // numTables
	font = binary.BigEndian.AppendUint16(font, 0)                        // reserved
	font = binary.BigEndian.AppendUint32(font, 0)                        // totalSfntSize
	font = binary.BigEndian.AppendUint32(font, uint32(compressed.Len())) // totalCompressedSize
	font = binary.BigEndian.AppendUint16(font, 1)                        // majorVersion
	font = binary.BigEndian.AppendUint16(font, 0)                        // minorVersion
	font = append(font, bytes.Repeat([]byte{0}, 20)...)                  // meta + priv blocks
	font = append(font, dir...)
	font = append(font, compressed.Bytes()...)
	return font
}

func TestDecodeCGFontMapping(t *testing.T) {
	font := buildTestWOFF2(t, map[rune]rune{'W': 'w', 'k': 'l', 'z': 'q'})
	mapping, err := decodeCGFontMapping(font)
	if err != nil {
		t.Fatalf("decodeCGFontMapping: %v", err)
	}
	if mapping['k'] != 'l' || mapping['W'] != 'w' || mapping['z'] != 'q' {
		t.Fatalf("unexpected mapping: %v", mapping)
	}
	if got := decodeCGText("kWz; ok!", mapping); got != "lwq; ol!" {
		t.Errorf("decodeCGText = %q, want %q", got, "lwq; ol!")
	}
}

// cgProtectedPage is a chapter page carrying the plugin's @font-face block and
// one obfuscated span.
func cgProtectedPage() string {
	return `<html><head><style>` +
		`@font-face{font-family:'cgregular';src:url('/wp-content/plugins/cg-scrape-protection/resources/fonts/used/abc.woff2') format('woff2');}` +
		`</style></head><body><p><span style="font-family: 'cgregular'">Wkz</span> end</p></body></html>`
}

type stubAssetFetcher struct {
	font  []byte
	err   error
	calls []string
}

func (f *stubAssetFetcher) fetchSiteAsset(_ context.Context, _, assetURL string, _ int64) ([]byte, error) {
	f.calls = append(f.calls, assetURL)
	if f.err != nil {
		return nil, f.err
	}
	return f.font, nil
}

func TestApplySiteHelpersDecodesChrysanthemumGarden(t *testing.T) {
	res := &parserhost.FetchResult{
		FinalURL: "https://www.chrysanthemumgarden.com/novel-tl/foo/ch-1/",
		Status:   200,
		Body:     []byte(cgProtectedPage()),
	}
	assets := &stubAssetFetcher{font: buildTestWOFF2(t, map[rune]rune{'W': 'w', 'k': 'l', 'z': 'q'})}
	if err := applySiteHelpers(context.Background(), res.FinalURL, res, assets); err != nil {
		t.Fatalf("applySiteHelpers: %v", err)
	}
	if !strings.Contains(string(res.Body), "wlq") {
		t.Fatalf("protected span was not decoded: %s", res.Body)
	}
	if strings.Contains(string(res.Body), "Wkz") {
		t.Fatalf("permuted letters still present: %s", res.Body)
	}
	if len(assets.calls) != 1 {
		t.Errorf("expected exactly one font fetch, got %v", assets.calls)
	}
}

// A protected page whose font cannot be fetched must fail the whole fetch as
// blocked: passing through would store a substitution cipher of the prose.
func TestApplySiteHelpersFailsLoudlyOnUndecodableFont(t *testing.T) {
	res := &parserhost.FetchResult{
		FinalURL: "https://www.chrysanthemumgarden.com/novel-tl/foo/ch-1/",
		Status:   200,
		Body:     []byte(cgProtectedPage()),
	}
	assets := &stubAssetFetcher{err: context.DeadlineExceeded}
	err := applySiteHelpers(context.Background(), res.FinalURL, res, assets)
	if err == nil {
		t.Fatal("undecodable protection passed through silently")
	}
	var scriptErr *parserhost.ScriptError
	if !errors.As(err, &scriptErr) || scriptErr.Code != parserhost.CodeBlocked {
		t.Fatalf("expected blocked ScriptError, got %v", err)
	}
}

// Pages without the protection marker, and non-registered hosts, pass through
// byte-for-byte and never trigger a font fetch.
func TestApplySiteHelpersSkipsUnrelatedResponses(t *testing.T) {
	unprotected := &parserhost.FetchResult{
		FinalURL: "https://www.chrysanthemumgarden.com/novel-tl/foo/ch-1/",
		Status:   200,
		Body:     []byte(`<html><body><p>plain text</p></body></html>`),
	}
	assets := &stubAssetFetcher{}
	if err := applySiteHelpers(context.Background(), unprotected.FinalURL, unprotected, assets); err != nil {
		t.Fatalf("applySiteHelpers: %v", err)
	}
	if len(assets.calls) != 0 {
		t.Errorf("unprotected page fetched a font: %v", assets.calls)
	}
	if string(unprotected.Body) != `<html><body><p>plain text</p></body></html>` {
		t.Errorf("unprotected page was modified: %s", unprotected.Body)
	}

	protected := &parserhost.FetchResult{
		FinalURL: "https://other-site.example/novel/1/",
		Status:   200,
		Body:     []byte(cgProtectedPage()),
	}
	if err := applySiteHelpers(context.Background(), protected.FinalURL, protected, assets); err != nil {
		t.Fatalf("applySiteHelpers: %v", err)
	}
	if len(assets.calls) != 0 {
		t.Errorf("non-registered host fetched a font: %v", assets.calls)
	}
}
