package font_test

import (
	"errors"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
)

// sampleFont builds a small font:
//
//	glyph 0: .notdef (empty)
//	glyph 1: 'A' simple square 0,0 -> 500,700
//	glyph 2: 'B' simple square 0,0 -> 400,700
//	glyph 3: 'C' composite of glyph 1 at (0,0) and glyph 2 at (600,0)
func sampleFont() []byte {
	return testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Ascender:   800,
		Descender:  -200,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}},
			{Advance: 550, Contours: [][]int32{{0, 0, 400, 0, 400, 700, 0, 700}}},
			{Advance: 1200, Components: []testfont.Component{
				{Glyph: 1, DX: 0, DY: 0},
				{Glyph: 2, DX: 600, DY: 0},
			}},
		},
		CMap: map[rune]uint16{'A': 1, 'B': 2, 'C': 3},
		Kern: []testfont.KernPair{{Left: 1, Right: 2, Value: -80}},
	})
}

func TestParseMetrics(t *testing.T) {
	f, err := font.Parse(sampleFont())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := f.Metrics()
	if m.UnitsPerEm != 1000 || m.Ascender != 800 || m.Descender != -200 {
		t.Errorf("bad metrics: %+v", m)
	}
	if f.NumGlyphs() != 4 {
		t.Errorf("NumGlyphs = %d, want 4", f.NumGlyphs())
	}
}

func TestGlyphIndexAndAdvance(t *testing.T) {
	f, err := font.Parse(sampleFont())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g, ok := f.GlyphIndex('A')
	if !ok || g != 1 {
		t.Errorf("GlyphIndex('A') = %d,%v want 1,true", g, ok)
	}
	if _, ok := f.GlyphIndex('Z'); ok {
		t.Errorf("GlyphIndex('Z') should be unmapped")
	}
	if got := f.AdvanceWidth(1); got != 600 {
		t.Errorf("AdvanceWidth(1) = %d, want 600", got)
	}
	if got := f.GlyphBounds(1); got.XMax != 500 || got.YMax != 700 {
		t.Errorf("GlyphBounds(1) = %+v", got)
	}
}

func TestCompositeOutline(t *testing.T) {
	f, err := font.Parse(sampleFont())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	o, err := f.Outline(3)
	if err != nil {
		t.Fatalf("Outline(3): %v", err)
	}
	if len(o.Contours) != 2 {
		t.Fatalf("composite outline has %d contours, want 2", len(o.Contours))
	}
	// Second contour must be shifted by (600,0).
	c := o.Contours[1]
	if c[1].X != 1000 || c[1].Y != 0 {
		t.Errorf("shifted contour point = %+v, want X=1000 Y=0", c[1])
	}
	// Component closure of glyph 3 must be {1,2}.
	deps, err := f.ComponentGlyphs(3)
	if err != nil {
		t.Fatalf("ComponentGlyphs: %v", err)
	}
	if len(deps) != 2 {
		t.Errorf("deps = %v, want 2 entries", deps)
	}
}

func TestKern(t *testing.T) {
	f, err := font.Parse(sampleFont())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := f.Kern(1, 2); got != -80 {
		t.Errorf("Kern(1,2) = %d, want -80", got)
	}
	if got := f.Kern(2, 1); got != 0 {
		t.Errorf("Kern(2,1) = %d, want 0 (fallback)", got)
	}
}

func TestCorruptInputs(t *testing.T) {
	good := sampleFont()
	cases := map[string][]byte{
		"empty":      {},
		"not-a-font": []byte("hello world, this is not a font at all....."),
		"truncated":  good[:len(good)/2],
		"bad-magic":  flip(good, 0),
		"bad-table":  flip(append([]byte(nil), good...), 20),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := font.Parse(data)
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
			if !errors.Is(err, font.ErrNotFont) && !errors.Is(err, font.ErrMalformed) &&
				!errors.Is(err, font.ErrUnsupported) {
				t.Fatalf("error %v is not a classified font error", err)
			}
		})
	}
}

func flip(b []byte, off int) []byte {
	c := append([]byte(nil), b...)
	c[off] ^= 0xFF
	return c
}
