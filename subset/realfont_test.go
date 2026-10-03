package subset_test

import (
	"os"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/raster"
	"github.com/guoyyit/font-raster-subset-go/subset"
)

func TestRealFontSubset(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("no system font")
	}
	src, err := font.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune("Hello, World! ÄÖÜäöüß 0123")
	out, err := subset.Subset(src, subset.Options{Runes: runes})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	if len(out) >= len(data) {
		t.Errorf("subset (%d bytes) not smaller than source (%d bytes)", len(out), len(data))
	}
	sub, err := font.Parse(out)
	if err != nil {
		t.Fatalf("subset does not parse: %v", err)
	}
	// Rasterize identical glyphs in both fonts; bitmaps must be identical.
	for _, r := range runes {
		gs, ok1 := src.GlyphIndex(r)
		gn, ok2 := sub.GlyphIndex(r)
		if ok1 != ok2 {
			t.Fatalf("%c: mapping presence differs", r)
		}
		if !ok1 {
			continue
		}
		is, err := raster.Glyph(src, gs, raster.Params{Size: 40})
		if err != nil {
			t.Fatalf("src raster %c: %v", r, err)
		}
		in, err := raster.Glyph(sub, gn, raster.Params{Size: 40})
		if err != nil {
			t.Fatalf("sub raster %c: %v", r, err)
		}
		if is.Bounds() != in.Bounds() {
			t.Fatalf("%c: bounds %v vs %v", r, is.Bounds(), in.Bounds())
		}
		for i := range is.Pix {
			if is.Pix[i] != in.Pix[i] {
				t.Fatalf("%c: pixel %d differs (%d vs %d)", r, i, is.Pix[i], in.Pix[i])
			}
		}
	}
	if testing.Verbose() {
		os.WriteFile("/tmp/subset.ttf", out, 0644)
		t.Logf("subset size: %d -> %d bytes, glyphs %d -> %d",
			len(data), len(out), src.NumGlyphs(), sub.NumGlyphs())
	}
}
