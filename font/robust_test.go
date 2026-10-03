package font_test

import (
	"math/rand"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
)

// TestMutationNeverPanics flips random bytes in a valid font and requires
// Parse (and subsequent access) to return classified errors, never panic.
func TestMutationNeverPanics(t *testing.T) {
	good := sampleFont()
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 2000; i++ {
		b := append([]byte(nil), good...)
		n := 1 + rng.Intn(4)
		for j := 0; j < n; j++ {
			b[rng.Intn(len(b))] ^= byte(1 << rng.Intn(8))
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on mutated font: %v", r)
				}
			}()
			f, err := font.Parse(b)
			if err != nil {
				return
			}
			// Exercise accessors; all must be safe.
			for _, r := range f.MappedRunes() {
				g, _ := f.GlyphIndex(r)
				_, _ = f.Outline(g)
				_ = f.AdvanceWidth(g)
				_ = f.GlyphBounds(g)
			}
		}()
	}
}

// TestDanglingComposite: a composite referencing a non-existent glyph must
// produce an error on access, not a panic or silent wrong result.
func TestDanglingComposite(t *testing.T) {
	data := testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			{Advance: 600, Components: []testfont.Component{{Glyph: 9, DX: 0, DY: 0}}},
		},
		CMap: map[rune]uint16{'A': 1},
	})
	f, err := font.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := f.Outline(1); err == nil {
		t.Fatal("expected error for dangling composite reference")
	}
	if _, err := f.ComponentGlyphs(1); err == nil {
		t.Fatal("expected error for dangling composite dependency")
	}
}
