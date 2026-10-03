package raster_test

import (
	"image/png"
	"os"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/raster"
)

func TestRealFontCurves(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skip("no system font")
	}
	f, err := font.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []rune{'A', 'O', 'g', 'Ä'} {
		g, ok := f.GlyphIndex(r)
		if !ok {
			t.Fatalf("no glyph for %c", r)
		}
		img, err := raster.Glyph(f, g, raster.Params{Size: 64})
		if err != nil {
			t.Fatalf("raster %c: %v", r, err)
		}
		var sum, n uint64
		for _, p := range img.Pix {
			sum += uint64(p)
			n++
		}
		t.Logf("%c: bounds=%v meanCoverage=%.1f", r, img.Bounds(), float64(sum)/float64(n))
		if sum == 0 {
			t.Errorf("%c rendered fully transparent", r)
		}
		out, _ := os.Create("/tmp/glyph_" + string(r) + ".png")
		png.Encode(out, img)
		out.Close()
	}
}
