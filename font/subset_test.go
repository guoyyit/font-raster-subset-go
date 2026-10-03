package font

import (
	"bytes"
	"testing"
)

func TestSubsetBasic(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	sub, err := f.Subset([]rune{'A', 'B'})
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Parse(sub)
	if err != nil {
		t.Fatalf("subset parse: %v", err)
	}
	// Should contain .notdef (0), A (1), B (2)
	if f2.NumGlyphs() != 3 {
		t.Errorf("subset glyphs = %d, want 3", f2.NumGlyphs())
	}
	// Runes map correctly
	if g := f2.GlyphIndex('A'); g != 1 {
		t.Errorf("subset GlyphIndex('A') = %d, want 1", g)
	}
	if g := f2.GlyphIndex('B'); g != 2 {
		t.Errorf("subset GlyphIndex('B') = %d, want 2", g)
	}
	// Missing rune maps to 0
	if g := f2.GlyphIndex('C'); g != 0 {
		t.Errorf("subset GlyphIndex('C') = %d, want 0", g)
	}
}

func TestSubsetCompositeClosure(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Request only 'C' (composite glyph 3). Dependencies (glyphs 1 and 2)
	// must be retained automatically.
	sub, err := f.Subset([]rune{'C'})
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Parse(sub)
	if err != nil {
		t.Fatalf("subset parse: %v", err)
	}
	// .notdef + composite + 2 dependencies = 4 glyphs
	if f2.NumGlyphs() != 4 {
		t.Errorf("subset glyphs = %d, want 4 (closure)", f2.NumGlyphs())
	}
	// 'C' maps to a valid glyph
	if g := f2.GlyphIndex('C'); g == 0 {
		t.Error("subset GlyphIndex('C') = 0, want non-zero")
	}
	// Composite glyph renders
	g := f2.GlyphIndex('C')
	bmp, err := f2.Rasterize(g, 48)
	if err != nil {
		t.Fatal(err)
	}
	if bmp.Width == 0 || bmp.Height == 0 {
		t.Error("composite glyph bitmap is empty")
	}
}

func TestSubsetDeterministic(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	runes := []rune{'A', 'B', 'C', 'D'}
	sub1, err := f.Subset(runes)
	if err != nil {
		t.Fatal(err)
	}
	sub2, err := f.Subset(runes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sub1, sub2) {
		t.Error("subset is not deterministic")
	}
}

func TestSubsetRasterMatch(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	sub, err := f.Subset([]rune{'A', 'B', 'C'})
	if err != nil {
		t.Fatal(err)
	}
	f2, _ := Parse(sub)
	for _, ch := range []rune{'A', 'B', 'C'} {
		g1 := f.GlyphIndex(ch)
		g2 := f2.GlyphIndex(ch)
		b1, err := f.Rasterize(g1, 48)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := f2.Rasterize(g2, 48)
		if err != nil {
			t.Fatal(err)
		}
		if b1.Width != b2.Width || b1.Height != b2.Height {
			t.Errorf("%q bitmap size mismatch: %dx%d vs %dx%d", ch, b1.Width, b1.Height, b2.Width, b2.Height)
			continue
		}
		if !bytes.Equal(b1.Pixels, b2.Pixels) {
			t.Errorf("%q bitmap pixels do not match", ch)
		}
	}
}

func TestSubsetKerning(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Subset with both kerning glyphs
	sub, err := f.Subset([]rune{'A', 'B'})
	if err != nil {
		t.Fatal(err)
	}
	f2, _ := Parse(sub)
	// Kerning should be retained
	if k := f2.KerningRunes('A', 'B'); k != -100 {
		t.Errorf("subset kerning(A,B) = %d, want -100", k)
	}
	if k := f2.KerningRunes('B', 'A'); k != -50 {
		t.Errorf("subset kerning(B,A) = %d, want -50", k)
	}
}

func TestSubsetEmpty(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Subset with no runes (only .notdef)
	sub, err := f.Subset(nil)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Parse(sub)
	if err != nil {
		t.Fatalf("subset parse: %v", err)
	}
	if f2.NumGlyphs() != 1 {
		t.Errorf("subset glyphs = %d, want 1", f2.NumGlyphs())
	}
}

func TestSubsetMissingRunes(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	// Request only missing runes
	sub, err := f.Subset([]rune{'X', 'Y', 'Z'})
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Parse(sub)
	if err != nil {
		t.Fatalf("subset parse: %v", err)
	}
	// Only .notdef
	if f2.NumGlyphs() != 1 {
		t.Errorf("subset glyphs = %d, want 1", f2.NumGlyphs())
	}
	if g := f2.GlyphIndex('X'); g != 0 {
		t.Errorf("subset GlyphIndex('X') = %d, want 0", g)
	}
}
