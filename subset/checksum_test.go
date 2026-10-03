package subset_test

import (
	"encoding/binary"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
	"github.com/guoyyit/font-raster-subset-go/subset"
)

// The whole-file checksum of a valid sfnt (with checkSumAdjustment in
// place) must equal 0xB1B0AFBA. Other loaders verify this.
func TestSubsetChecksumAdjustment(t *testing.T) {
	src := sampleFont(t)
	out, err := subset.Subset(src, subset.Options{Runes: []rune{'A', 'C'}})
	if err != nil {
		t.Fatal(err)
	}
	var total uint32
	for i := 0; i+4 <= len(out); i += 4 {
		total += binary.BigEndian.Uint32(out[i:])
	}
	if total != 0xB1B0AFBA {
		t.Errorf("file checksum = 0x%08x, want 0xB1B0AFBA", total)
	}
}

// Runes beyond the BMP must produce a format 12 cmap that round-trips.
func TestSubsetNonBMP(t *testing.T) {
	src, err := font.Parse(testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}},
		},
		CMap: map[rune]uint16{'A': 1},
	}))
	if err != nil {
		t.Fatal(err)
	}
	// Build a source font whose cmap maps a non-BMP rune by hand: reuse the
	// subsetter's own format-12 output as the source for a second round.
	sub1, err := subset.Subset(src, subset.Options{Runes: []rune{'A'}})
	if err != nil {
		t.Fatal(err)
	}
	_ = sub1
	// Directly verify format 12 via a font built with a non-BMP cmap.
	// testfont only emits format 4, so instead verify the subsetter emits
	// format 12 when asked to keep a non-BMP rune that the source maps.
	// We simulate by subsetting a font parsed from a hand-built cmap12.
	f12 := buildCmap12Font(t)
	out, err := subset.Subset(f12, subset.Options{Runes: []rune{0x1F600}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("subset with format 12 cmap does not parse: %v", err)
	}
	if _, ok := sub.GlyphIndex(0x1F600); !ok {
		t.Error("non-BMP rune lost in subset")
	}
}

func buildCmap12Font(t *testing.T) *font.Font {
	t.Helper()
	// Build a normal font, then replace its cmap with a format 12 one
	// mapping U+1F600 -> glyph 1.
	base := testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}},
		},
		CMap: map[rune]uint16{'A': 1},
	})
	f, err := font.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	// Craft cmap12 table: header + one subtable + one group.
	var sub []byte
	u16 := func(v uint16) { sub = append(sub, byte(v>>8), byte(v)) }
	u32 := func(v uint32) { sub = append(sub, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }
	u16(12)
	u16(0)
	u32(16 + 12)
	u32(0)
	u32(1)       // nGroups
	u32(0x1F600) // startCharCode
	u32(0x1F600) // endCharCode
	u32(1)       // startGlyphID
	var cmap []byte
	c16 := func(v uint16) { cmap = append(cmap, byte(v>>8), byte(v)) }
	c32 := func(v uint32) { cmap = append(cmap, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }
	c16(0)
	c16(1)
	c16(3)
	c16(10)
	c32(12)
	cmap = append(cmap, sub...)
	tables := map[string][]byte{"cmap": cmap}
	for _, tag := range []string{"head", "hhea", "maxp", "hmtx", "loca", "glyf", "post", "name"} {
		tables[tag] = f.Table(tag)
	}
	out := assembleForTest(tables)
	f2, err := font.Parse(out)
	if err != nil {
		t.Fatalf("rebuilt font does not parse: %v", err)
	}
	if _, ok := f2.GlyphIndex(0x1F600); !ok {
		t.Fatal("test setup: cmap12 font lacks U+1F600")
	}
	return f2
}
