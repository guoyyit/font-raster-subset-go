package font

import (
	"sort"
	"testing"
)

// testFont builds a minimal valid TrueType font for testing.
//
// Glyph layout:
//
//	glyph 0: .notdef  - filled rectangle (0,0)-(500,700)
//	glyph 1: 'A'      - filled triangle (0,0)-(500,0)-(250,700)
//	glyph 2: 'B'      - filled rectangle (0,0)-(400,600) with a triangular hole
//	glyph 3: composite of glyphs 1 and 2 side by side
//
// cmap: 'A'->1, 'B'->2, 'C'->3, 'D'->0 (missing)
// kern: (1,2)->-100, (2,1)->-50
type testFont struct {
	t *testing.T

	// glyph data
	glyfData []byte
	loca     []uint32
	numGlyph int

	// metrics
	advances []uint16
	lsbs     []int16

	// cmap
	runeToGlyph map[rune]GlyphIndex

	// kern
	kernPairs []kernPair
}

func newTestFont(t *testing.T) *testFont {
	t.Helper()
	tf := &testFont{t: t, numGlyph: 4}
	tf.build()
	return tf
}

func (tf *testFont) build() {
	// Glyph 0: .notdef rectangle (0,0)-(500,700)
	g0 := simpleGlyph(0, 0, 500, 700, [][2]int{
		{0, 0}, {500, 0}, {500, 700}, {0, 700},
	})
	// Glyph 1: 'A' triangle (0,0)-(500,0)-(250,700)
	g1 := simpleGlyph(0, 0, 500, 700, [][2]int{
		{0, 0}, {500, 0}, {250, 700},
	})
	// Glyph 2: 'B' rectangle (0,0)-(400,600) with triangular hole
	// Outer rectangle, inner triangle (opposite winding)
	g2 := simpleGlyph(0, 0, 400, 600, [][2]int{
		{0, 0}, {400, 0}, {400, 600}, {0, 600},
		{100, 100}, {300, 100}, {200, 500}, // inner triangle (reverse winding)
	})
	// Glyph 3: composite of glyphs 1 and 2
	g3 := compositeGlyph(0, 0, 900, 700, []component{
		{flags: flagArg1And2AreWords | flagArgsAreXYValues | flagMoreComponents, glyphIndex: 1, arg1: 0, arg2: 0, a: 1, d: 1},
		{flags: flagArg1And2AreWords | flagArgsAreXYValues, glyphIndex: 2, arg1: 500, arg2: 0, a: 1, d: 1},
	})

	tf.glyfData = append(tf.glyfData, g0...)
	tf.loca = append(tf.loca, 0)
	tf.glyfData = append(tf.glyfData, g1...)
	tf.loca = append(tf.loca, uint32(len(tf.glyfData)-len(g1)))
	tf.glyfData = append(tf.glyfData, g2...)
	tf.loca = append(tf.loca, uint32(len(tf.glyfData)-len(g2)))
	tf.glyfData = append(tf.glyfData, g3...)
	tf.loca = append(tf.loca, uint32(len(tf.glyfData)-len(g3)))
	tf.loca = append(tf.loca, uint32(len(tf.glyfData)))

	tf.advances = []uint16{500, 500, 400, 900}
	tf.lsbs = []int16{0, 0, 0, 0}

	tf.runeToGlyph = map[rune]GlyphIndex{'A': 1, 'B': 2, 'C': 3}
	tf.kernPairs = []kernPair{
		{left: 1, right: 2, value: -100},
		{left: 2, right: 1, value: -50},
	}
}

// simpleGlyph builds a simple glyph from contour points.
// Each contour is a list of (x,y) points. All points are on-curve.
func simpleGlyph(xMin, yMin, xMax, yMax int, contours ...[][2]int) []byte {
	var b []byte
	// header
	b = appendI16(b, int16(len(contours))) // numberOfContours
	b = appendI16(b, int16(xMin))
	b = appendI16(b, int16(yMin))
	b = appendI16(b, int16(xMax))
	b = appendI16(b, int16(yMax))
	// endPtsOfContours
	end := 0
	var allPts [][2]int
	for _, c := range contours {
		end += len(c)
		b = appendU16(b, uint16(end-1))
		allPts = append(allPts, c...)
	}
	// instructionLength
	b = appendU16(b, 0)
	// flags: all on-curve, int16 deltas
	for range allPts {
		b = append(b, 0x01)
	}
	// x deltas
	prev := 0
	for _, p := range allPts {
		b = appendI16(b, int16(p[0]-prev))
		prev = p[0]
	}
	// y deltas
	prev = 0
	for _, p := range allPts {
		b = appendI16(b, int16(p[1]-prev))
		prev = p[1]
	}
	return b
}

// compositeGlyph builds a composite glyph.
func compositeGlyph(xMin, yMin, xMax, yMax int, comps []component) []byte {
	var b []byte
	b = appendI16(b, -1) // numberOfContours = -1 (composite)
	b = appendI16(b, int16(xMin))
	b = appendI16(b, int16(yMin))
	b = appendI16(b, int16(xMax))
	b = appendI16(b, int16(yMax))
	for _, c := range comps {
		b = appendU16(b, c.flags)
		b = appendU16(b, uint16(c.glyphIndex))
		if c.flags&flagArg1And2AreWords != 0 {
			b = appendI16(b, c.arg1)
			b = appendI16(b, c.arg2)
		} else {
			b = append(b, byte(c.arg1), byte(c.arg2))
		}
		if c.flags&flagWeHaveScale != 0 {
			b = appendI16(b, int16(c.a*16384))
		} else if c.flags&flagWeHaveXYScale != 0 {
			b = appendI16(b, int16(c.a*16384))
			b = appendI16(b, int16(c.d*16384))
		} else if c.flags&flagWeHave2x2 != 0 {
			b = appendI16(b, int16(c.a*16384))
			b = appendI16(b, int16(c.b*16384))
			b = appendI16(b, int16(c.c*16384))
			b = appendI16(b, int16(c.d*16384))
		}
	}
	return b
}

func appendU16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func appendI16(b []byte, v int16) []byte {
	return append(b, byte(v>>8), byte(v))
}

// fontBytes assembles the test font into sfnt bytes.
func (tf *testFont) fontBytes() []byte {
	tables := map[string][]byte{}

	// head (54 bytes)
	head := make([]byte, 54)
	putU32(head, 0, 0x00010000)  // version
	putU32(head, 4, 0x00010000)  // fontRevision
	putU32(head, 8, 0)           // checksumAdjustment
	putU32(head, 12, 0x5F0F3CF5) // magicNumber
	putU16(head, 16, 0x000B)     // flags
	putU16(head, 18, 1000)       // unitsPerEm
	// created/modified (8 bytes each)
	putU16(head, 36, 0)   // xMin
	putU16(head, 38, 0)   // yMin
	putU16(head, 40, 900) // xMax
	putU16(head, 42, 700) // yMax
	putU16(head, 44, 0)   // macStyle
	putU16(head, 46, 8)   // lowestRecPPEM
	putI16(head, 48, 2)   // fontDirectionHint
	putI16(head, 50, 1)   // indexToLocFormat = long
	putI16(head, 52, 0)   // glyphDataFormat
	tables["head"] = head

	// hhea (36 bytes)
	hhea := make([]byte, 36)
	putU32(hhea, 0, 0x00010000)
	putI16(hhea, 4, 800)                 // ascent
	putI16(hhea, 6, -200)                // descent
	putI16(hhea, 8, 0)                   // lineGap
	putU16(hhea, 10, 900)                // advanceWidthMax
	putI16(hhea, 12, 0)                  // minLeftSideBearing
	putI16(hhea, 14, 0)                  // minRightSideBearing
	putI16(hhea, 16, 900)                // xMaxExtent
	putI16(hhea, 34, int16(tf.numGlyph)) // numberOfHMetrics
	tables["hhea"] = hhea

	// maxp (32 bytes, version 1.0)
	maxp := make([]byte, 32)
	putU32(maxp, 0, 0x00010000)
	putU16(maxp, 4, uint16(tf.numGlyph))
	tables["maxp"] = maxp

	// hmtx
	hmtx := make([]byte, 4*tf.numGlyph)
	for i := 0; i < tf.numGlyph; i++ {
		putU16(hmtx, 4*i, tf.advances[i])
		putI16(hmtx, 4*i+2, tf.lsbs[i])
	}
	tables["hmtx"] = hmtx

	// cmap (format 4)
	tables["cmap"] = tf.buildCmap()

	// loca (long format)
	loca := make([]byte, 4*(tf.numGlyph+1))
	for i, v := range tf.loca {
		putU32(loca, 4*i, v)
	}
	tables["loca"] = loca

	// glyf
	tables["glyf"] = tf.glyfData

	// name (minimal, just format + count + offset)
	name := make([]byte, 6)
	putU16(name, 0, 0) // format
	putU16(name, 2, 0) // count
	putU16(name, 4, 6) // stringOffset
	tables["name"] = name

	// post (format 3.0)
	post := make([]byte, 32)
	putU32(post, 0, 0x00030000)
	tables["post"] = post

	// kern (format 0)
	tables["kern"] = tf.buildKern()

	out, err := assembleSFNT(tables)
	if err != nil {
		tf.t.Fatalf("assembleSFNT: %v", err)
	}
	return out
}

func (tf *testFont) buildCmap() []byte {
	// Build a format-4 cmap with the rune->glyph mappings.
	rs := make([]rune, 0, len(tf.runeToGlyph))
	for r := range tf.runeToGlyph {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	segCount := len(rs) + 1 // + sentinel
	glyphArray := make([]uint16, 0)
	idRangeOff := make([]uint16, segCount)
	for i, r := range rs {
		arrayStart := len(glyphArray)
		glyphArray = append(glyphArray, uint16(tf.runeToGlyph[r]))
		idRangeOff[i] = uint16(2 * (segCount + arrayStart))
	}

	subtableLen := 16 + 8*segCount + 2*len(glyphArray)
	b := make([]byte, 12+subtableLen)
	// cmap header
	putU16(b, 0, 0)  // version
	putU16(b, 2, 1)  // numTables
	putU16(b, 4, 3)  // platformID
	putU16(b, 6, 1)  // encodingID
	putU32(b, 8, 12) // offset to subtable
	// format 4 subtable
	p := 12
	putU16(b, p, 4) // format
	putU16(b, p+2, uint16(subtableLen))
	putU16(b, p+4, 0) // language
	putU16(b, p+6, uint16(2*segCount))
	searchRange := uint16(1)
	entrySelector := uint16(0)
	for searchRange*2 <= uint16(segCount) {
		searchRange *= 2
		entrySelector++
	}
	searchRange *= 2
	putU16(b, p+8, searchRange)
	putU16(b, p+10, entrySelector)
	putU16(b, p+12, uint16(2*segCount)-searchRange)
	q := p + 14
	for _, r := range rs {
		putU16(b, q, uint16(r))
		q += 2
	}
	putU16(b, q, 0xFFFF) // sentinel endCode
	q += 2
	putU16(b, q, 0) // reservedPad
	q += 2
	for _, r := range rs {
		putU16(b, q, uint16(r))
		q += 2
	}
	putU16(b, q, 0xFFFF) // sentinel startCode
	q += 2
	for i := 0; i < len(rs); i++ {
		putU16(b, q, 0) // idDelta
		q += 2
	}
	putU16(b, q, 1) // sentinel idDelta
	q += 2
	for i := 0; i < len(rs); i++ {
		putU16(b, q, idRangeOff[i])
		q += 2
	}
	putU16(b, q, 0) // sentinel idRangeOff
	q += 2
	for _, g := range glyphArray {
		putU16(b, q, g)
		q += 2
	}
	return b
}

func (tf *testFont) buildKern() []byte {
	if len(tf.kernPairs) == 0 {
		return nil
	}
	n := len(tf.kernPairs)
	subtableLen := 14 + 6*n
	b := make([]byte, 4+subtableLen)
	putU16(b, 0, 0) // version
	putU16(b, 2, 1) // nTables
	putU16(b, 4, 0) // subVersion
	putU16(b, 6, uint16(subtableLen))
	putU16(b, 8, 0x0001) // coverage: horizontal
	putU16(b, 10, uint16(n))
	for i, p := range tf.kernPairs {
		putU16(b, 18+6*i, uint16(p.left))
		putU16(b, 18+6*i+2, uint16(p.right))
		putU16(b, 18+6*i+4, uint16(int16(p.value)))
	}
	return b
}

func putI16(b []byte, o int, v int16) {
	b[o] = byte(v >> 8)
	b[o+1] = byte(v)
}
