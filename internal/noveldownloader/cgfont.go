package noveldownloader

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// This file decodes the per-chapter obfuscation fonts served by
// chrysanthemumgarden.com (WordPress plugin "cg-scrape-protection").
//
// Those fonts are 52-letter subsets (A-Z, a-z) of Open Sans with permuted
// glyph assignments: the cmap maps 'k' to a glyph slot whose outline draws
// some other letter. Browsers render the right shapes, but the HTML source
// holds the wrong letters. Because the outlines come from a known base font,
// matching each glyph's bounding box + advance width against the embedded
// reference table reveals the substitution for that font file.
//
// Only a WOFF2 subset is implemented: header + table directory, brotli
// payload, cmap format 4, hhea/maxp scalars, raw or transformed hmtx, and
// the transformed glyf bbox walk (full triplet coordinate decoding, without
// reconstructing outlines). Composite glyphs are rejected: the protection
// fonts only ever contain simple letter outlines.

// woff2KnownTags is the spec table-tag list (WOFF2 §4), copied from
// fontTools so directory indexes resolve to the same tags.
var woff2KnownTags = []string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post",
	"cvt ", "fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT",
	"EBLC", "gasp", "hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea",
	"vmtx", "BASE", "GDEF", "GPOS", "GSUB", "EBSC", "JSTF", "MATH",
	"CBDT", "CBLC", "COLR", "CPAL", "SVG ", "sbix", "acnt", "avar",
	"bdat", "bloc", "bsln", "cvar", "fdsc", "feat", "fmtx", "fvar",
	"gvar", "hsty", "just", "lcar", "mort", "morx", "opbd", "prop",
	"trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

// cgGlyphKey identifies a letter outline by geometry. Advance widths alone
// are ambiguous (e.g. 'b', 'd', 'p', 'q' share 1253) and bboxes alone
// collide once ('E' vs 'F'); the pair is unique across A-Z a-z.
type cgGlyphKey struct {
	XMin    int16
	YMin    int16
	XMax    int16
	YMax    int16
	Advance uint16
}

type woff2Reader struct {
	data []byte
	pos  int
}

func (r *woff2Reader) byte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("unexpected end of data at offset %d", r.pos)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *woff2Reader) uint16() (uint16, error) {
	if r.pos+2 > len(r.data) {
		return 0, fmt.Errorf("unexpected end of data at offset %d", r.pos)
	}
	v := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *woff2Reader) int16() (int16, error) {
	v, err := r.uint16()
	return int16(v), err
}

func (r *woff2Reader) uint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, fmt.Errorf("unexpected end of data at offset %d", r.pos)
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *woff2Reader) take(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, fmt.Errorf("unexpected end of data at offset %d (need %d bytes)", r.pos, n)
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

// base128 reads a WOFF2 UIntBase128 value.
func (r *woff2Reader) base128() (uint32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		v = (v << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("malformed base128 value")
}

// uint255 reads a WOFF2 255UInt16 value.
func (r *woff2Reader) uint255() (uint16, error) {
	code, err := r.byte()
	if err != nil {
		return 0, err
	}
	switch code {
	case 253:
		return r.uint16()
	case 255:
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		return 253 + uint16(b), nil
	case 254:
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		return 506 + uint16(b), nil
	default:
		return uint16(code), nil
	}
}

type woff2Table struct {
	data        []byte
	transformed bool
}

// parseWOFF2Tables decompresses the brotli payload and slices it into per
// table blobs following the directory order.
func parseWOFF2Tables(fontData []byte) (map[string]woff2Table, error) {
	r := &woff2Reader{data: fontData}
	sig, err := r.take(4)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(sig, []byte("wOF2")) {
		return nil, fmt.Errorf("not a WOFF2 font (bad signature %q)", sig)
	}
	if _, err := r.take(4); err != nil { // flavor
		return nil, err
	}
	if _, err := r.take(4); err != nil { // length
		return nil, err
	}
	numTables, err := r.uint16()
	if err != nil {
		return nil, err
	}
	if _, err := r.uint16(); err != nil { // reserved
		return nil, err
	}
	if _, err := r.uint32(); err != nil { // totalSfntSize
		return nil, err
	}
	totalCompressedSize, err := r.uint32()
	if err != nil {
		return nil, err
	}
	if _, err := r.take(2 + 2 + 4*5); err != nil { // version … privLength
		return nil, err
	}

	type entry struct {
		tag         string
		length      uint32
		transformed bool
	}
	entries := make([]entry, 0, numTables)
	for i := 0; i < int(numTables); i++ {
		flags, err := r.byte()
		if err != nil {
			return nil, err
		}
		var tag string
		if idx := flags & 0x3F; idx == 0x3F {
			raw, err := r.take(4)
			if err != nil {
				return nil, err
			}
			tag = string(raw)
		} else {
			if int(idx) >= len(woff2KnownTags) {
				return nil, fmt.Errorf("unknown WOFF2 tag index %d", idx)
			}
			tag = woff2KnownTags[idx]
		}
		origLen, err := r.base128()
		if err != nil {
			return nil, fmt.Errorf("table %s origLength: %w", tag, err)
		}
		length := origLen
		ver := flags >> 6
		transformed := ver != 0
		if tag == "glyf" || tag == "loca" {
			transformed = ver != 3
		}
		if transformed {
			length, err = r.base128()
			if err != nil {
				return nil, fmt.Errorf("table %s transformLength: %w", tag, err)
			}
		}
		entries = append(entries, entry{tag: tag, length: length, transformed: transformed})
	}
	if uint32(len(fontData)-r.pos) < totalCompressedSize {
		return nil, fmt.Errorf("truncated WOFF2 payload")
	}
	compressed := fontData[r.pos : r.pos+int(totalCompressedSize)]
	decompressed, err := io.ReadAll(brotli.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		return nil, fmt.Errorf("brotli decompress: %w", err)
	}
	tables := make(map[string]woff2Table, len(entries))
	pos := 0
	for _, e := range entries {
		if pos+int(e.length) > len(decompressed) {
			return nil, fmt.Errorf("table %s overruns decompressed data", e.tag)
		}
		tables[e.tag] = woff2Table{data: decompressed[pos : pos+int(e.length)], transformed: e.transformed}
		pos += int(e.length)
	}
	return tables, nil
}

// parseCmapFormat4 maps Unicode code points to glyph IDs. Only format 4 is
// supported; the protection fonts ship exactly that.
func parseCmapFormat4(data []byte) (map[rune]uint16, error) {
	r := &woff2Reader{data: data}
	if _, err := r.uint16(); err != nil { // version
		return nil, err
	}
	nsubs, err := r.uint16()
	if err != nil {
		return nil, err
	}
	best := -1
	for i := 0; i < int(nsubs); i++ {
		plat, err := r.uint16()
		if err != nil {
			return nil, err
		}
		enc, err := r.uint16()
		if err != nil {
			return nil, err
		}
		off, err := r.uint32()
		if err != nil {
			return nil, err
		}
		if (plat == 3 && enc == 1) || (plat == 0 && enc == 3) {
			best = int(off)
		}
	}
	if best < 0 {
		return nil, fmt.Errorf("no unicode cmap subtable")
	}
	r.pos = best
	format, err := r.uint16()
	if err != nil {
		return nil, err
	}
	if format != 4 {
		return nil, fmt.Errorf("unsupported cmap format %d", format)
	}
	if _, err := r.uint16(); err != nil { // length
		return nil, err
	}
	if _, err := r.uint16(); err != nil { // language
		return nil, err
	}
	segCountX2, err := r.uint16()
	if err != nil {
		return nil, err
	}
	n := int(segCountX2) / 2
	if _, err := r.take(2 + 2 + 2); err != nil { // searchRange, entrySelector, rangeShift
		return nil, err
	}
	endCodes := make([]uint16, n)
	for i := range endCodes {
		endCodes[i], err = r.uint16()
		if err != nil {
			return nil, err
		}
	}
	if _, err := r.uint16(); err != nil { // reservedPad
		return nil, err
	}
	startCodes := make([]uint16, n)
	for i := range startCodes {
		startCodes[i], err = r.uint16()
		if err != nil {
			return nil, err
		}
	}
	idDeltas := make([]int16, n)
	for i := range idDeltas {
		idDeltas[i], err = r.int16()
		if err != nil {
			return nil, err
		}
	}
	rangeOffsetPos := r.pos
	idRangeOffsets := make([]uint16, n)
	for i := range idRangeOffsets {
		idRangeOffsets[i], err = r.uint16()
		if err != nil {
			return nil, err
		}
	}
	out := make(map[rune]uint16)
	for i := 0; i < n; i++ {
		if startCodes[i] == 0xFFFF {
			continue
		}
		for c := uint32(startCodes[i]); c <= uint32(endCodes[i]); c++ {
			var gid uint16
			if idRangeOffsets[i] == 0 {
				gid = uint16((c + uint32(uint16(idDeltas[i]))) & 0xFFFF)
			} else {
				p := rangeOffsetPos + i*2 + int(idRangeOffsets[i]) + 2*int(c-uint32(startCodes[i]))
				if p+2 > len(data) {
					return nil, fmt.Errorf("cmap glyph lookup out of range")
				}
				gid = binary.BigEndian.Uint16(data[p:])
				if gid != 0 {
					gid = uint16((uint32(gid) + uint32(uint16(idDeltas[i]))) & 0xFFFF)
				}
			}
			if gid != 0 {
				out[rune(c)] = gid
			}
		}
	}
	return out, nil
}

// parseHmtxAdvances returns the advance width per glyph ID. Handles both the
// raw table and the WOFF2 transform (lsb arrays are skipped: only advances
// are needed for glyph matching).
func parseHmtxAdvances(t woff2Table, numGlyphs, numHMetrics int) ([]uint16, error) {
	if !t.transformed {
		r := &woff2Reader{data: t.data}
		adv := make([]uint16, 0, numGlyphs)
		for i := 0; i < numHMetrics && i < numGlyphs; i++ {
			a, err := r.uint16()
			if err != nil {
				return nil, err
			}
			if _, err := r.int16(); err != nil { // lsb
				return nil, err
			}
			adv = append(adv, a)
		}
		return adv, nil
	}
	r := &woff2Reader{data: t.data}
	flags, err := r.byte()
	if err != nil {
		return nil, err
	}
	if flags&0b11111100 != 0 {
		return nil, fmt.Errorf("reserved bits set in transformed hmtx flags %#x", flags)
	}
	adv := make([]uint16, 0, numGlyphs)
	for i := 0; i < numHMetrics && i < numGlyphs; i++ {
		a, err := r.uint16()
		if err != nil {
			return nil, err
		}
		adv = append(adv, a)
	}
	return adv, nil
}

// tripletDelta decodes one WOFF2 glyf triplet flag + coordinate bytes into
// x/y deltas, following fontTools' _decodeTriplets exactly.
func tripletDelta(flag byte, coords []byte) (dx, dy int, err error) {
	sign := func(f byte, v int) int {
		if f&1 == 1 {
			return v
		}
		return -v
	}
	switch {
	case flag < 10:
		if len(coords) < 1 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 1 byte", flag)
		}
		return 0, sign(flag, int(flag&14)<<7+int(coords[0])), nil
	case flag < 20:
		if len(coords) < 1 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 1 byte", flag)
		}
		return sign(flag, int((flag-10)&14)<<7+int(coords[0])), 0, nil
	case flag < 84:
		if len(coords) < 1 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 1 byte", flag)
		}
		b0 := flag - 20
		b1 := coords[0]
		return sign(flag, 1+int(b0&0x30)+int(b1>>4)),
			sign(flag>>1, 1+int(b0&0x0C)<<2+int(b1&0x0F)), nil
	case flag < 120:
		if len(coords) < 2 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 2 bytes", flag)
		}
		b0 := flag - 84
		return sign(flag, 1+int(b0/12)<<8+int(coords[0])),
			sign(flag>>1, 1+int((b0%12)>>2)<<8+int(coords[1])), nil
	case flag < 124:
		if len(coords) < 3 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 3 bytes", flag)
		}
		b2 := coords[1]
		return sign(flag, int(coords[0])<<4+int(b2>>4)),
			sign(flag>>1, int(b2&0x0F)<<8+int(coords[2])), nil
	default:
		if len(coords) < 4 {
			return 0, 0, fmt.Errorf("triplet flag %d needs 4 bytes", flag)
		}
		return sign(flag, int(coords[0])<<8+int(coords[1])),
			sign(flag>>1, int(coords[2])<<8+int(coords[3])), nil
	}
}

// parseGlyfBBoxes walks the transformed WOFF2 glyf table and returns the
// bounding box of every glyph. Composite glyphs are rejected.
func parseGlyfBBoxes(data []byte) ([][4]int16, error) {
	r := &woff2Reader{data: data}
	if version, err := r.uint16(); err != nil {
		return nil, err
	} else if version != 0 {
		return nil, fmt.Errorf("unsupported transformed glyf version %d", version)
	}
	if _, err := r.uint16(); err != nil { // optionFlags
		return nil, err
	}
	numGlyphs, err := r.uint16()
	if err != nil {
		return nil, err
	}
	if _, err := r.uint16(); err != nil { // indexFormat
		return nil, err
	}
	sizes := make([]uint32, 7)
	for i := range sizes {
		sizes[i], err = r.uint32()
		if err != nil {
			return nil, err
		}
	}
	streams := make([][]byte, 7)
	for i, sz := range sizes {
		streams[i], err = r.take(int(sz))
		if err != nil {
			return nil, fmt.Errorf("glyf stream %d: %w", i, err)
		}
	}
	nContourR := &woff2Reader{data: streams[0]}
	nPointsR := &woff2Reader{data: streams[1]}
	flagR := &woff2Reader{data: streams[2]}
	glyphR := &woff2Reader{data: streams[3]}
	bboxStream := streams[5]
	instrR := &woff2Reader{data: streams[6]}

	bboxBitmapSize := ((int(numGlyphs) + 31) >> 5) << 2
	if bboxBitmapSize > len(bboxStream) {
		return nil, fmt.Errorf("bbox stream too short")
	}
	bboxBitmap := bboxStream[:bboxBitmapSize]
	bboxR := &woff2Reader{data: bboxStream[bboxBitmapSize:]}

	boxes := make([][4]int16, 0, numGlyphs)
	for gid := 0; gid < int(numGlyphs); gid++ {
		nContour, err := nContourR.int16()
		if err != nil {
			return nil, fmt.Errorf("glyph %d nContour: %w", gid, err)
		}
		if nContour == 0 {
			boxes = append(boxes, [4]int16{})
			continue
		}
		if nContour < 0 {
			return nil, fmt.Errorf("glyph %d is composite (unsupported)", gid)
		}
		nPoints := 0
		for i := 0; i < int(nContour); i++ {
			v, err := nPointsR.uint255()
			if err != nil {
				return nil, fmt.Errorf("glyph %d nPoints: %w", gid, err)
			}
			nPoints += int(v)
		}
		flags, err := flagR.take(nPoints)
		if err != nil {
			return nil, fmt.Errorf("glyph %d flags: %w", gid, err)
		}
		var minX, minY, maxX, maxY int
		first := true
		x, y := 0, 0
		for _, f := range flags {
			flag := f & 0x7F
			var nBytes int
			switch {
			case flag < 84:
				nBytes = 1
			case flag < 120:
				nBytes = 2
			case flag < 124:
				nBytes = 3
			default:
				nBytes = 4
			}
			coords, err := glyphR.take(nBytes)
			if err != nil {
				return nil, fmt.Errorf("glyph %d coords: %w", gid, err)
			}
			dx, dy, err := tripletDelta(flag, coords)
			if err != nil {
				return nil, fmt.Errorf("glyph %d: %w", gid, err)
			}
			x += dx
			y += dy
			if first || x < minX {
				minX = x
			}
			if first || x > maxX {
				maxX = x
			}
			if first || y < minY {
				minY = y
			}
			if first || y > maxY {
				maxY = y
			}
			first = false
		}
		// Consume the instruction length + bytes (usually empty).
		ilen, err := glyphR.uint255()
		if err != nil {
			return nil, fmt.Errorf("glyph %d instructions: %w", gid, err)
		}
		if _, err := instrR.take(int(ilen)); err != nil {
			return nil, fmt.Errorf("glyph %d instructions: %w", gid, err)
		}
		if bboxBitmap[gid>>3]&(0x80>>(gid&7)) != 0 {
			explicit := [4]int16{}
			for i := range explicit {
				explicit[i], err = bboxR.int16()
				if err != nil {
					return nil, fmt.Errorf("glyph %d bbox: %w", gid, err)
				}
			}
			boxes = append(boxes, explicit)
		} else {
			boxes = append(boxes, [4]int16{int16(minX), int16(minY), int16(maxX), int16(maxY)})
		}
	}
	return boxes, nil
}

// decodeCGFontMapping builds the slot-letter → true-letter substitution for
// one obfuscation font. Exact (bbox, advance) matches resolve almost every
// glyph; the generator perturbs a few outlines by a unit, so leftovers fall
// back to the unique nearest bbox with the same advance.
func decodeCGFontMapping(fontData []byte) (map[rune]rune, error) {
	tables, err := parseWOFF2Tables(fontData)
	if err != nil {
		return nil, err
	}
	need := []string{"cmap", "glyf", "hmtx", "hhea", "maxp"}
	for _, tag := range need {
		if _, ok := tables[tag]; !ok {
			return nil, fmt.Errorf("font is missing table %s", tag)
		}
	}
	maxp := tables["maxp"].data
	if len(maxp) < 6 {
		return nil, fmt.Errorf("truncated maxp table")
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxp[4:6]))
	hhea := tables["hhea"].data
	if len(hhea) < 36 {
		return nil, fmt.Errorf("truncated hhea table")
	}
	numHMetrics := int(binary.BigEndian.Uint16(hhea[34:36]))

	cmap, err := parseCmapFormat4(tables["cmap"].data)
	if err != nil {
		return nil, fmt.Errorf("cmap: %w", err)
	}
	advances, err := parseHmtxAdvances(tables["hmtx"], numGlyphs, numHMetrics)
	if err != nil {
		return nil, fmt.Errorf("hmtx: %w", err)
	}
	boxes, err := parseGlyfBBoxes(tables["glyf"].data)
	if err != nil {
		return nil, fmt.Errorf("glyf: %w", err)
	}

	mapping := make(map[rune]rune, len(cmap))
	used := make(map[rune]bool, len(cmap))
	type pending struct {
		slot rune
		gid  int
	}
	var rest []pending
	for slot, gid := range cmap {
		if int(gid) >= len(boxes) || int(gid) >= len(advances) {
			return nil, fmt.Errorf("glyph %d out of range", gid)
		}
		key := cgGlyphKey{boxes[gid][0], boxes[gid][1], boxes[gid][2], boxes[gid][3], advances[gid]}
		if trueLetter, ok := cgReferenceGlyphs[key]; ok && !used[trueLetter] {
			mapping[slot] = trueLetter
			used[trueLetter] = true
		} else {
			rest = append(rest, pending{slot: slot, gid: int(gid)})
		}
	}
	const cgBBoxTolerance = 8
	for _, p := range rest {
		box := boxes[p.gid]
		adv := advances[p.gid]
		var best rune
		bestDist := -1
		tied := false
		for key, letter := range cgReferenceGlyphs {
			if used[letter] || key.Advance != adv {
				continue
			}
			dist := absDiff(int(box[0]), int(key.XMin)) + absDiff(int(box[1]), int(key.YMin)) +
				absDiff(int(box[2]), int(key.XMax)) + absDiff(int(box[3]), int(key.YMax))
			if dist > cgBBoxTolerance {
				continue
			}
			if bestDist < 0 || dist < bestDist {
				best, bestDist, tied = letter, dist, false
			} else if dist == bestDist {
				tied = true
			}
		}
		if bestDist < 0 || tied {
			return nil, fmt.Errorf("cannot resolve glyph for %q (bbox %v advance %d)", p.slot, box, adv)
		}
		mapping[p.slot] = best
		used[best] = true
	}
	return mapping, nil
}

func absDiff(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}

// decodeCGText applies a slot → true-letter substitution to text. Only ASCII
// letters are ever substituted (the fonts contain nothing else); every other
// rune passes through untouched.
func decodeCGText(s string, mapping map[rune]rune) string {
	return string(decodeCGTextRunes([]rune(s), mapping))
}

func decodeCGTextRunes(rs []rune, mapping map[rune]rune) []rune {
	for i, c := range rs {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			if real, ok := mapping[c]; ok {
				rs[i] = real
			}
		}
	}
	return rs
}
