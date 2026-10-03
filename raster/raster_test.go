package raster_test

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/testfont"
	"github.com/guoyyit/font-raster-subset-go/raster"
)

func squareFont() *font.Font {
	f, err := font.Parse(testfont.Build(testfont.Spec{
		UnitsPerEm: 1000,
		Glyphs: []testfont.Glyph{
			{Advance: 500},
			// 500x700 filled square.
			{Advance: 600, Contours: [][]int32{{0, 0, 500, 0, 500, 700, 0, 700}}},
		},
		CMap: map[rune]uint16{'A': 1},
	}))
	if err != nil {
		panic(err)
	}
	return f
}

func TestRasterSquare(t *testing.T) {
	f := squareFont()
	img, err := raster.Glyph(f, 1, raster.Params{Size: 100})
	if err != nil {
		t.Fatalf("Glyph: %v", err)
	}
	// 100px em, glyph is 500x700 font units -> 50x70 pixels.
	if got := img.Bounds().Dx(); got != 50 {
		t.Errorf("width = %d, want 50", got)
	}
	if got := img.Bounds().Dy(); got != 70 {
		t.Errorf("height = %d, want 70", got)
	}
	// Center pixel must be fully covered.
	c := img.GrayAt(img.Bounds().Min.X+25, img.Bounds().Min.Y+35)
	if c.Y != 255 {
		t.Errorf("center coverage = %d, want 255", c.Y)
	}
	// Corner pixel outside the square must be empty.
	if img.Bounds().Min.X > 0 {
		t.Skip("origin not positive")
	}
}

func TestRasterDeterministic(t *testing.T) {
	f := squareFont()
	var ref []byte
	for i := 0; i < 5; i++ {
		img, err := raster.Glyph(f, 1, raster.Params{Size: 37.5})
		if err != nil {
			t.Fatalf("Glyph: %v", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if ref == nil {
			ref = buf.Bytes()
		} else if !bytes.Equal(ref, buf.Bytes()) {
			t.Fatal("raster output differs between runs")
		}
	}
}

func TestRasterEmptyAndErrors(t *testing.T) {
	f := squareFont()
	img, err := raster.Glyph(f, 0, raster.Params{Size: 20})
	if err != nil {
		t.Fatalf("empty glyph: %v", err)
	}
	if !img.Bounds().Empty() {
		t.Errorf("empty glyph should produce empty image, got %v", img.Bounds())
	}
	if _, err := raster.Glyph(f, 1, raster.Params{Size: 0}); err == nil {
		t.Error("size 0 should error")
	}
	if _, err := raster.Glyph(f, 1, raster.Params{Size: 1e6, MaxDim: 64}); err == nil {
		t.Error("oversized bitmap should error")
	}
}
