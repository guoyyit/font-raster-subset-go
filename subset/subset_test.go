package subset_test

import (
	"bytes"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
	"github.com/guoyyit/font-raster-subset-go/subset"
)

func sampleFont(t *testing.T) *font.Font {
	t.Helper()
	f, err := font.Parse(testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Ascender:   800,
		Descender:  -200,
		Glyphs: []testfont.Glyph{
			{Advance: 500}, // .notdef
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}}, // A
			{Advance: 550, Contours: [][]int32{{0, 0, 400, 0, 400, 700, 0, 700}}}, // B
			{Advance: 1200, Components: []testfont.Component{ // C = A + B@600
				{Glyph: 1, DX: 0, DY: 0},
				{Glyph: 2, DX: 600, DY: 0},
			}},
		},
		CMap: map[rune]uint16{'A': 1, 'B': 2, 'C': 3},
		Kern: []testfont.KernPair{{Left: 1, Right: 2, Value: -80}},
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestSubsetRoundTrip(t *testing.T) {
	src := sampleFont(t)
	out, err := subset.Subset(src, subset.Options{Runes: []rune{'A', 'C'}})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("subset output does not parse: %v", err)
	}
	// 'C' is composite of A and B: B must be pulled in as a dependency.
	if sub.NumGlyphs() != 4 { // .notdef, A, B, C
		t.Fatalf("NumGlyphs = %d, want 4", sub.NumGlyphs())
	}
	if _, ok := sub.GlyphIndex('A'); !ok {
		t.Error("subset lost 'A'")
	}
	if _, ok := sub.GlyphIndex('C'); !ok {
		t.Error("subset lost 'C'")
	}
	// Metrics must be preserved.
	if sub.Metrics() != src.Metrics() {
		t.Errorf("metrics changed: %+v vs %+v", sub.Metrics(), src.Metrics())
	}
	// Rendering-relevant data must match the source for kept runes.
	for _, r := range []rune{'A', 'C'} {
		gs, _ := src.GlyphIndex(r)
		gn, _ := sub.GlyphIndex(r)
		osrc, err := src.Outline(gs)
		if err != nil {
			t.Fatalf("src outline %c: %v", r, err)
		}
		onew, err := sub.Outline(gn)
		if err != nil {
			t.Fatalf("subset outline %c: %v", r, err)
		}
		if len(osrc.Contours) != len(onew.Contours) {
			t.Fatalf("%c: contour count %d vs %d", r, len(osrc.Contours), len(onew.Contours))
		}
		for i := range osrc.Contours {
			cs, cn := osrc.Contours[i], onew.Contours[i]
			if len(cs) != len(cn) {
				t.Fatalf("%c contour %d: point count differs", r, i)
			}
			for j := range cs {
				if cs[j] != cn[j] {
					t.Fatalf("%c contour %d point %d: %+v vs %+v", r, i, j, cs[j], cn[j])
				}
			}
		}
		if src.AdvanceWidth(gs) != sub.AdvanceWidth(gn) {
			t.Errorf("%c: advance changed", r)
		}
	}
}

func TestSubsetKernPreserved(t *testing.T) {
	src := sampleFont(t)
	out, err := subset.Subset(src, subset.Options{Runes: []rune{'A', 'B'}})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ga, _ := sub.GlyphIndex('A')
	gb, _ := sub.GlyphIndex('B')
	if got := sub.Kern(ga, gb); got != -80 {
		t.Errorf("kern(A,B) = %d, want -80", got)
	}
}

func TestSubsetDeterministic(t *testing.T) {
	src := sampleFont(t)
	a, err := subset.Subset(src, subset.Options{Runes: []rune{'C', 'A', 'B'}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		b, err := subset.Subset(src, subset.Options{Runes: []rune{'B', 'A', 'C'}})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("subset output differs across runs / input orders")
		}
	}
}

func TestSubsetMissingRunes(t *testing.T) {
	src := sampleFont(t)
	// 'Z' has no mapping; it must be skipped, not an error.
	out, err := subset.Subset(src, subset.Options{Runes: []rune{'A', 'Z'}})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := sub.GlyphIndex('Z'); ok {
		t.Error("unmapped rune should not appear in subset")
	}
	if _, ok := sub.GlyphIndex('A'); !ok {
		t.Error("lost 'A'")
	}
}

func TestSubsetEmpty(t *testing.T) {
	src := sampleFont(t)
	out, err := subset.Subset(src, subset.Options{})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sub.NumGlyphs() != 1 { // only .notdef
		t.Errorf("NumGlyphs = %d, want 1", sub.NumGlyphs())
	}
}
