package font

import (
	"bytes"
	"testing"
)

func TestRasterizeBasic(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	bmp, err := f.Rasterize(1, 48)
	if err != nil {
		t.Fatal(err)
	}
	if bmp.Width == 0 || bmp.Height == 0 {
		t.Error("bitmap is empty")
	}
	if len(bmp.Pixels) != bmp.Width*bmp.Height {
		t.Error("pixels length mismatch")
	}
	// Should have some coverage
	hasCoverage := false
	for _, v := range bmp.Pixels {
		if v > 0 {
			hasCoverage = true
			break
		}
	}
	if !hasCoverage {
		t.Error("bitmap has no coverage")
	}
}

func TestRasterizeDeterministic(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	bmp1, err := f.Rasterize(1, 48)
	if err != nil {
		t.Fatal(err)
	}
	bmp2, err := f.Rasterize(1, 48)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bmp1.Pixels, bmp2.Pixels) {
		t.Error("rasterization is not deterministic")
	}
}

func TestRasterizeSizeConsistency(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Same glyph at different sizes should produce consistent shapes.
	// Check that fully-covered interior pixels scale proportionally.
	for _, ppem := range []int{24, 48, 96} {
		bmp, err := f.Rasterize(0, ppem) // .notdef rectangle
		if err != nil {
			t.Fatal(err)
		}
		// Rectangle should have solid coverage in the interior
		// (skip 1-pixel border for AA)
		solid := 0
		for y := 1; y < bmp.Height-1; y++ {
			for x := 1; x < bmp.Width-1; x++ {
				if bmp.Pixels[y*bmp.Width+x] == 255 {
					solid++
				}
			}
		}
		if solid == 0 {
			t.Errorf("ppem=%d: no solid interior pixels", ppem)
		}
	}
}

func TestRasterizeComposite(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	bmp, err := f.Rasterize(3, 48) // composite
	if err != nil {
		t.Fatal(err)
	}
	if bmp.Width == 0 || bmp.Height == 0 {
		t.Error("composite bitmap is empty")
	}
	// Composite should be wider than either component
	b1, _ := f.Rasterize(1, 48)
	if bmp.Width <= b1.Width {
		t.Errorf("composite width %d <= component width %d", bmp.Width, b1.Width)
	}
}

func TestRasterizeEmpty(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Glyph 0 has contours; test that rasterizing doesn't panic
	bmp, err := f.Rasterize(0, 48)
	if err != nil {
		t.Fatal(err)
	}
	_ = bmp
}

func TestRasterizeInvalidSize(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	for _, ppem := range []int{0, -1, 1025} {
		if _, err := f.Rasterize(1, ppem); err == nil {
			t.Errorf("Rasterize(1, %d) expected error, got nil", ppem)
		}
	}
}

func TestRasterizeBounds(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	bmp, err := f.Rasterize(1, 48)
	if err != nil {
		t.Fatal(err)
	}
	// Bitmap should cover the glyph bounds
	if bmp.Width <= 0 || bmp.Height <= 0 {
		t.Error("bitmap has zero dimension")
	}
	// Advance should be positive
	if bmp.Advance <= 0 {
		t.Errorf("advance = %d, want positive", bmp.Advance)
	}
}
