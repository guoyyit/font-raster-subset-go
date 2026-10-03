// Package testfont builds minimal but fully valid TrueType fonts in memory
// for use in tests. It supports simple glyphs, composite glyphs, cmap
// format 4 and a kern format 0 table.
package testfont

import (
	"sort"

	"github.com/guoyyit/font-raster-subset-go/internal/sfntwrite"
)

// Glyph describes one glyph to encode.
type Glyph struct {
	// Contours: each contour is a flat slice [x0,y0, x1,y1, ...]; all
	// points are on-curve (lines). Ignored for composite glyphs.
	Contours [][]int32
	// Components, if non-empty, makes this a composite glyph.
	Components []Component
	Advance    uint16
}

// Component is one composite reference.
type Component struct {
	Glyph  uint16
	DX, DY int16
}

// Spec describes the font to build.
type Spec struct {
	UnitsPerEm uint16
	Ascender   int16
	Descender  int16
	Glyphs     []Glyph
	// CMap maps runes to glyph indices.
	CMap map[rune]uint16
	// Kern pairs.
	Kern []KernPair
}

// KernPair is one kerning entry.
type KernPair struct {
	Left, Right uint16
	Value       int16
}

// Build encodes the font and returns the TTF bytes.
func Build(s Spec) []byte {
	if s.UnitsPerEm == 0 {
		s.UnitsPerEm = 1000
	}
	glyf, loca := buildGlyf(s.Glyphs)
	tables := map[string][]byte{
		"head": buildHead(s.UnitsPerEm),
		"hhea": buildHhea(s),
		"maxp": buildMaxp(len(s.Glyphs)),
		"hmtx": buildHmtx(s.Glyphs),
		"glyf": glyf,
		"loca": loca,
		"cmap": buildCmap(s.CMap),
		"post": buildPost(),
		"name": buildName(),
	}
	if len(s.Kern) > 0 {
		tables["kern"] = buildKern(s.Kern)
	}
	return sfntwrite.Assemble(tables)
}

func u16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func i16(b []byte, v int16) []byte  { return u16(b, uint16(v)) }
func u32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func buildHead(upem uint16) []byte {
	var b []byte
	b = u32(b, 0x00010000) // version
	b = u32(b, 0x00010000) // fontRevision
	b = u32(b, 0)          // checkSumAdjustment (patched by assemble)
	b = u32(b, 0x5F0F3CF5) // magic
	b = u16(b, 0)          // flags
	b = u16(b, upem)
	b = append(b, make([]byte, 16)...) // created+modified
	b = i16(b, 0)
	b = i16(b, 0)
	b = i16(b, 1000)
	b = i16(b, 1000)
	b = u16(b, 0) // macStyle
	b = u16(b, 8) // lowestRecPPEM
	b = i16(b, 2) // fontDirectionHint
	b = i16(b, 1) // indexToLocFormat: long
	b = i16(b, 0) // glyphDataFormat
	return b
}

func buildHhea(s Spec) []byte {
	var b []byte
	b = u32(b, 0x00010000)
	b = i16(b, s.Ascender)
	b = i16(b, s.Descender)
	b = i16(b, 0) // lineGap
	var maxAdv uint16
	for _, g := range s.Glyphs {
		if g.Advance > maxAdv {
			maxAdv = g.Advance
		}
	}
	b = u16(b, maxAdv)
	b = append(b, make([]byte, 20)...) // minLSB..reserved
	b = i16(b, 0)                      // metricDataFormat
	b = u16(b, uint16(len(s.Glyphs)))
	return b
}

func buildMaxp(numGlyphs int) []byte {
	var b []byte
	b = u32(b, 0x00010000)
	b = u16(b, uint16(numGlyphs))
	b = append(b, make([]byte, 26)...) // v1.0 extra fields, zeroed
	return b
}

func buildHmtx(glyphs []Glyph) []byte {
	var b []byte
	for _, g := range glyphs {
		b = u16(b, g.Advance)
		b = i16(b, 0)
	}
	return b
}

func buildGlyf(glyphs []Glyph) (glyf, loca []byte) {
	var offsets []uint32
	for _, g := range glyphs {
		offsets = append(offsets, uint32(len(glyf)))
		var body []byte
		if len(g.Components) > 0 {
			body = compositeBody(g)
		} else if len(g.Contours) > 0 {
			body = simpleBody(g)
		}
		glyf = append(glyf, body...)
		for len(glyf)%4 != 0 {
			glyf = append(glyf, 0)
		}
	}
	offsets = append(offsets, uint32(len(glyf)))
	for _, o := range offsets {
		loca = u32(loca, o)
	}
	return glyf, loca
}

func glyphBounds(g Glyph) (xMin, yMin, xMax, yMax int16) {
	first := true
	put := func(x, y int32) {
		if first || int16(x) < xMin {
			xMin = int16(x)
		}
		if first || int16(y) < yMin {
			yMin = int16(y)
		}
		if first || int16(x) > xMax {
			xMax = int16(x)
		}
		if first || int16(y) > yMax {
			yMax = int16(y)
		}
		first = false
	}
	for _, c := range g.Contours {
		for i := 0; i+1 < len(c); i += 2 {
			put(c[i], c[i+1])
		}
	}
	return
}

func simpleBody(g Glyph) []byte {
	var b []byte
	b = i16(b, int16(len(g.Contours)))
	xMin, yMin, xMax, yMax := glyphBounds(g)
	b = i16(b, xMin)
	b = i16(b, yMin)
	b = i16(b, xMax)
	b = i16(b, yMax)
	// endPtsOfContours
	total := 0
	for _, c := range g.Contours {
		total += len(c) / 2
		b = u16(b, uint16(total-1))
	}
	b = u16(b, 0) // instructionLength
	// flags: all on-curve, x and y stored as 16-bit deltas (no short, no same)
	n := total
	for i := 0; i < n; i++ {
		b = append(b, 0x01) // ON_CURVE only
	}
	// x deltas
	var px, py int32
	for _, c := range g.Contours {
		for i := 0; i+1 < len(c); i += 2 {
			b = i16(b, int16(c[i]-px))
			px = c[i]
		}
	}
	for _, c := range g.Contours {
		for i := 0; i+1 < len(c); i += 2 {
			b = i16(b, int16(c[i+1]-py))
			py = c[i+1]
		}
	}
	return b
}

func compositeBody(g Glyph) []byte {
	var b []byte
	b = i16(b, -1) // composite
	b = i16(b, 0)
	b = i16(b, 0)
	b = i16(b, 1000)
	b = i16(b, 1000)
	for i, c := range g.Components {
		flags := uint16(0x0001 | 0x0002) // ARGS_ARE_WORDS | ARGS_ARE_XY_VALUES
		if i < len(g.Components)-1 {
			flags |= 0x0020 // MORE_COMPONENTS
		}
		b = u16(b, flags)
		b = u16(b, c.Glyph)
		b = i16(b, c.DX)
		b = i16(b, c.DY)
	}
	return b
}

func buildCmap(m map[rune]uint16) []byte {
	runes := make([]rune, 0, len(m))
	for r := range m {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	// Build format 4 segments: one segment per rune (simple, always valid),
	// plus the mandatory 0xFFFF terminal segment.
	type seg struct{ start, end, gid uint16 }
	var segs []seg
	for _, r := range runes {
		segs = append(segs, seg{uint16(r), uint16(r), m[r]})
	}
	segs = append(segs, seg{0xFFFF, 0xFFFF, 0})
	n := len(segs)
	var sub []byte
	length := 16 + 8*n
	sub = u16(sub, 4)
	sub = u16(sub, uint16(length))
	sub = u16(sub, 0)           // language
	sub = u16(sub, uint16(2*n)) // segCountX2
	sub = u16(sub, 0)           // searchRange (loose)
	sub = u16(sub, 0)           // entrySelector
	sub = u16(sub, 0)           // rangeShift
	for _, s := range segs {
		sub = u16(sub, s.end)
	}
	sub = u16(sub, 0) // reservedPad
	for _, s := range segs {
		sub = u16(sub, s.start)
	}
	for _, s := range segs {
		// idDelta such that gid = (char + delta) mod 65536
		sub = u16(sub, uint16(int32(s.gid)-int32(s.start)))
	}
	for range segs {
		sub = u16(sub, 0) // idRangeOffset: use delta
	}
	// cmap header: one subtable, platform 3 encoding 1
	var b []byte
	b = u16(b, 0) // version
	b = u16(b, 1) // numTables
	b = u16(b, 3)
	b = u16(b, 1)
	b = u32(b, 12)
	b = append(b, sub...)
	return b
}

func buildKern(pairs []KernPair) []byte {
	sorted := append([]KernPair(nil), pairs...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Left != b.Left {
			return a.Left < b.Left
		}
		return a.Right < b.Right
	})
	var sub []byte
	sub = u16(sub, 0) // version
	sub = u16(sub, uint16(14+6*len(sorted)))
	sub = u16(sub, 0x0001) // format 0, horizontal
	sub = u16(sub, uint16(len(sorted)))
	sub = u16(sub, 0)
	sub = u16(sub, 0)
	sub = u16(sub, 0)
	for _, p := range sorted {
		sub = u16(sub, p.Left)
		sub = u16(sub, p.Right)
		sub = i16(sub, p.Value)
	}
	var b []byte
	b = u16(b, 0) // version
	b = u16(b, 1) // nTables
	b = append(b, sub...)
	return b
}

func buildPost() []byte {
	var b []byte
	b = u32(b, 0x00030000) // version 3.0: no glyph names
	b = append(b, make([]byte, 28)...)
	return b
}

func buildName() []byte {
	var b []byte
	b = u16(b, 0) // format
	b = u16(b, 0) // count
	b = u16(b, 6) // stringOffset
	return b
}
