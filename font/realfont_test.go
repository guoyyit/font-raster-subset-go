package font_test

import (
	"os"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
)

func TestRealFont(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("no system font")
	}
	f, err := font.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t.Logf("unitsPerEm=%d glyphs=%d", f.Metrics().UnitsPerEm, f.NumGlyphs())
	g, ok := f.GlyphIndex('A')
	if !ok {
		t.Fatal("no 'A'")
	}
	o, err := f.Outline(g)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(o.Contours) == 0 {
		t.Fatal("empty outline for 'A'")
	}
	t.Logf("'A' advance=%d bounds=%+v contours=%d", f.AdvanceWidth(g), f.GlyphBounds(g), len(o.Contours))
	// Composite: 'Ä' is usually composite in DejaVu.
	if g2, ok := f.GlyphIndex('Ä'); ok {
		deps, err := f.ComponentGlyphs(g2)
		if err != nil {
			t.Fatalf("ComponentGlyphs: %v", err)
		}
		t.Logf("'Ä' deps=%v", deps)
		if _, err := f.Outline(g2); err != nil {
			t.Fatalf("Outline(Ä): %v", err)
		}
	}
}
