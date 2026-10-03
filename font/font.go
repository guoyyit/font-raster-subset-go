// Package font implements an offline TrueType font subsetting and grayscale
// rasterization engine.
//
// The engine parses common outline font files (TrueType, sfnt version
// 0x00010000 and OpenType-TT with a glyf table), exposes per-glyph metrics
// and outlines, produces structurally complete subset fonts, rasterizes glyphs
// to anti-aliased grayscale bitmaps, and applies kerning data when present.
//
// All results are deterministic: given the same input and configuration,
// subset bytes and bitmap output are byte-for-byte identical. A Font value is
// immutable after parsing and safe for concurrent use.
package font

import (
	"errors"
	"image"
)

// GlyphIndex is a glyph identifier within a font. Glyph index 0 is the
// special ".notdef" glyph.
type GlyphIndex uint16

// Errors returned by the engine. Errors are distinguishable via errors.Is.
var (
	// ErrTruncated is returned when the input ends before a required field.
	ErrTruncated = errors.New("font: truncated font data")
	// ErrBadMagic is returned when the sfnt magic number is not recognized.
	ErrBadMagic = errors.New("font: bad sfnt magic number")
	// ErrUnsupportedVersion is returned for unsupported table/font versions.
	ErrUnsupportedVersion = errors.New("font: unsupported font version")
	// ErrMissingTable is returned when a required table is absent.
	ErrMissingTable = errors.New("font: missing required table")
	// ErrBadTable is returned when a table's structure is invalid.
	ErrBadTable = errors.New("font: corrupt table")
	// ErrBadChecksum is returned when a table checksum does not match.
	ErrBadChecksum = errors.New("font: table checksum mismatch")
	// ErrBadGlyph is returned when a glyph index is out of range.
	ErrBadGlyph = errors.New("font: glyph index out of range")
	// ErrBadOutline is returned when a glyph outline is malformed.
	ErrBadOutline = errors.New("font: malformed glyph outline")
	// ErrUnsupportedFormat is returned for font formats the engine cannot
	// process (e.g. CFF/OpenType outlines or TrueType collections).
	ErrUnsupportedFormat = errors.New("font: unsupported font format")
	// ErrBadArgument is returned for invalid call arguments.
	ErrBadArgument = errors.New("font: invalid argument")
)

// Metrics describes a glyph's metrics in font units.
type Metrics struct {
	// Advance is the horizontal pen advance.
	Advance int
	// Bounds is the glyph's bounding box (xMin, yMin, xMax, yMax).
	Bounds image.Rectangle
}

// SegmentOp identifies an outline segment operation.
type SegmentOp uint8

const (
	// SegMoveTo starts a new contour at (X0, Y0).
	SegMoveTo SegmentOp = iota
	// SegLineTo draws a line to (X1, Y1).
	SegLineTo
	// SegQuadTo draws a quadratic Bézier to (X2, Y2) with control (X1, Y1).
	SegQuadTo
	// SegCubeTo draws a cubic Bézier to (X3, Y3) with controls (X1, Y1),
	// (X2, Y2). (Reserved for future CFF support; glyf outlines use quads.)
	SegCubeTo
	// SegClose closes the current contour.
	SegClose
)

// Segment is a single outline segment. Coordinates are in font units with a
// y-up axis (as stored in the font).
type Segment struct {
	Op     SegmentOp
	X0, Y0 int
	X1, Y1 int
	X2, Y2 int
	X3, Y3 int
}

// Outline is a glyph's resolved outline in font units. Composite glyphs are
// fully resolved into a single contour list.
type Outline struct {
	Segments []Segment
	Bounds   image.Rectangle
}

// Bitmap is a grayscale rasterized glyph. Pixels are 8-bit coverage values
// (0 = empty, 255 = fully covered). The bitmap covers the glyph's pixel
// bounding box; Left and Top are pen-relative bearings in pixels.
type Bitmap struct {
	Width, Height int
	Left, Top     int
	Advance       int
	Pixels        []byte
}

// Font is a parsed, immutable font. It is safe for concurrent use.
type Font struct {
	data   []byte
	tables map[string]tableRecord

	head headTable
	hhea hheaTable
	maxp maxpTable
	hmtx []longHorMetric
	cmap cmapTable
	loca []uint32
	kern kernTable
}

// Parse parses a TrueType font from data. The data is retained (not copied)
// and must not be modified by the caller afterwards.
func Parse(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, ErrTruncated
	}
	sfnt := u32(data, 0)
	switch sfnt {
	case 0x00010000, 0x74727565 /* 'true' */, 0x74797031 /* 'typ1' */ :
		// TrueType outlines
	case 0x4F54544F /* 'OTTO' */ :
		return nil, ErrUnsupportedFormat
	case 0x74746366 /* 'ttcf' */ :
		return nil, ErrUnsupportedFormat
	default:
		return nil, ErrBadMagic
	}
	numTables := int(u16(data, 4))
	if numTables == 0 || len(data) < 12+16*numTables {
		return nil, ErrTruncated
	}
	f := &Font{data: data, tables: make(map[string]tableRecord, numTables)}
	for i := 0; i < numTables; i++ {
		o := 12 + 16*i
		tag := string(data[o : o+4])
		rec := tableRecord{
			tag:      tag,
			checksum: u32(data, o+4),
			offset:   u32(data, o+8),
			length:   u32(data, o+12),
		}
		if uint64(rec.offset)+uint64(rec.length) > uint64(len(data)) {
			return nil, ErrBadTable
		}
		if _, dup := f.tables[tag]; dup {
			return nil, ErrBadTable
		}
		f.tables[tag] = rec
	}
	// Required tables.
	for _, tag := range []string{"head", "hhea", "maxp", "hmtx", "cmap", "loca", "glyf"} {
		if _, ok := f.tables[tag]; !ok {
			return nil, ErrMissingTable
		}
	}
	// Verify table checksums. The head table checksum is computed with its
	// checksumAdjustment field zeroed.
	for _, rec := range f.tables {
		if rec.tag == "head" {
			if !verifyHeadChecksum(data, rec) {
				return nil, ErrBadChecksum
			}
			continue
		}
		if !verifyChecksum(data, rec) {
			return nil, ErrBadChecksum
		}
	}
	var err error
	if f.head, err = parseHead(f.table("head")); err != nil {
		return nil, err
	}
	if f.hhea, err = parseHhea(f.table("hhea")); err != nil {
		return nil, err
	}
	if f.maxp, err = parseMaxp(f.table("maxp")); err != nil {
		return nil, err
	}
	if f.hmtx, err = parseHmtx(f.table("hmtx"), f.hhea.numberOfHMetrics, f.maxp.numGlyphs); err != nil {
		return nil, err
	}
	if f.cmap, err = parseCmap(f.table("cmap")); err != nil {
		return nil, err
	}
	if f.loca, err = parseLoca(f.table("loca"), f.maxp.numGlyphs, f.head.indexToLocFormat); err != nil {
		return nil, err
	}
	// Kerning is optional; malformed kerning falls back to no kerning.
	if kb := f.table("kern"); kb != nil {
		if kt, kerr := parseKern(kb); kerr == nil {
			f.kern = kt
		}
	}
	return f, nil
}

// verifyChecksum verifies a table's sfnt checksum (uint32 sum of the table
// data padded with zero bytes to a 4-byte boundary).
func verifyChecksum(data []byte, rec tableRecord) bool {
	o := int(rec.offset)
	l := int(rec.length)
	if o < 0 || l < 0 || o > len(data) || l > len(data)-o {
		return false
	}
	var sum uint32
	n := l &^ 3
	for i := 0; i < n; i += 4 {
		sum += u32(data, o+i)
	}
	if rem := l & 3; rem != 0 {
		var tail [4]byte
		copy(tail[:], data[o+n:o+l])
		sum += u32(tail[:], 0)
	}
	return sum == rec.checksum
}

// verifyHeadChecksum verifies the head table checksum with the
// checksumAdjustment field (bytes 8..12) zeroed.
func verifyHeadChecksum(data []byte, rec tableRecord) bool {
	o := int(rec.offset)
	l := int(rec.length)
	if l < 12 || o < 0 || o > len(data) || l > len(data)-o {
		return false
	}
	buf := make([]byte, l)
	copy(buf, data[o:o+l])
	for i := 8; i < 12; i++ {
		buf[i] = 0
	}
	var sum uint32
	n := len(buf) &^ 3
	for i := 0; i < n; i += 4 {
		sum += u32(buf, i)
	}
	if rem := len(buf) & 3; rem != 0 {
		var tail [4]byte
		copy(tail[:], buf[n:])
		sum += u32(tail[:], 0)
	}
	return sum == rec.checksum
}

func parseHmtx(b []byte, numberOfHMetrics uint16, numGlyphs uint16) ([]longHorMetric, error) {
	if numberOfHMetrics == 0 || numGlyphs == 0 {
		return nil, ErrBadTable
	}
	if uint32(numberOfHMetrics) > uint32(numGlyphs) {
		return nil, ErrBadTable
	}
	need := 4*int(numberOfHMetrics) + 2*int(numGlyphs-numberOfHMetrics)
	if len(b) < need {
		return nil, ErrTruncated
	}
	m := make([]longHorMetric, numGlyphs)
	for i := 0; i < int(numberOfHMetrics); i++ {
		m[i] = longHorMetric{advanceWidth: u16(b, 4*i), lsb: i16(b, 4*i+2)}
	}
	last := m[numberOfHMetrics-1]
	for i := int(numberOfHMetrics); i < int(numGlyphs); i++ {
		m[i] = last
		m[i].lsb = i16(b, 4*int(numberOfHMetrics)+2*(i-int(numberOfHMetrics)))
	}
	return m, nil
}

func parseLoca(b []byte, numGlyphs uint16, indexToLocFormat int16) ([]uint32, error) {
	n := int(numGlyphs) + 1
	loca := make([]uint32, n)
	switch indexToLocFormat {
	case 0: // short offsets, stored as uint16/2
		if len(b) < 2*n {
			return nil, ErrTruncated
		}
		for i := 0; i < n; i++ {
			loca[i] = uint32(u16(b, 2*i)) * 2
		}
	case 1: // long offsets
		if len(b) < 4*n {
			return nil, ErrTruncated
		}
		for i := 0; i < n; i++ {
			loca[i] = u32(b, 4*i)
		}
	default:
		return nil, ErrBadTable
	}
	return loca, nil
}

// NumGlyphs returns the number of glyphs in the font.
func (f *Font) NumGlyphs() int { return int(f.maxp.numGlyphs) }

// UnitsPerEm returns the font's units-per-em value.
func (f *Font) UnitsPerEm() int { return int(f.head.unitsPerEm) }

// GlyphIndex returns the glyph index for rune r. Runes not present in the
// font's character map map to glyph 0 (".notdef").
func (f *Font) GlyphIndex(r rune) GlyphIndex {
	if r < 0 || r > 0x10FFFF {
		return 0
	}
	return f.cmap.lookup(uint32(r))
}

// GlyphMetrics returns the advance width and bounding box of glyph g in font
// units.
func (f *Font) GlyphMetrics(g GlyphIndex) (Metrics, error) {
	if int(g) >= len(f.hmtx) {
		return Metrics{}, ErrBadGlyph
	}
	gl, err := f.loadGlyph(g)
	if err != nil {
		return Metrics{}, err
	}
	m := Metrics{Advance: int(f.hmtx[g].advanceWidth)}
	if gl.numberOfContours != 0 {
		m.Bounds = image.Rect(int(gl.xMin), int(gl.yMin), int(gl.xMax), int(gl.yMax))
	}
	return m, nil
}

// Kerning returns the horizontal kerning adjustment in font units for the
// glyph pair (left, right). It returns 0 when the font has no kerning data,
// the pair is absent, or the kerning table is malformed (deterministic
// fallback to no kerning).
func (f *Font) Kerning(left, right GlyphIndex) int {
	if int(left) >= len(f.hmtx) || int(right) >= len(f.hmtx) {
		return 0
	}
	for _, p := range f.kern.pairs {
		if p.left == left && p.right == right {
			return int(p.value)
		}
	}
	return 0
}

// KerningRunes is a convenience wrapper around Kerning that maps runes to
// glyph indices first.
func (f *Font) KerningRunes(left, right rune) int {
	return f.Kerning(f.GlyphIndex(left), f.GlyphIndex(right))
}
