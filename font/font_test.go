package font

import (
	"errors"
	"testing"
)

func TestParseMetrics(t *testing.T) {
	tf := newTestFont(t)
	f, err := Parse(tf.fontBytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.UnitsPerEm() != 1000 {
		t.Errorf("UnitsPerEm = %d, want 1000", f.UnitsPerEm())
	}
	if f.NumGlyphs() != 4 {
		t.Errorf("NumGlyphs = %d, want 4", f.NumGlyphs())
	}
	m, err := f.GlyphMetrics(0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Advance != 500 {
		t.Errorf("glyph0 advance = %d, want 500", m.Advance)
	}
	if m.Bounds.Dx() != 500 || m.Bounds.Dy() != 700 {
		t.Errorf("glyph0 bounds = %v, want 500x700", m.Bounds)
	}
	m, err = f.GlyphMetrics(1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Advance != 500 {
		t.Errorf("glyph1 advance = %d, want 500", m.Advance)
	}
	m, err = f.GlyphMetrics(3)
	if err != nil {
		t.Fatal(err)
	}
	if m.Advance != 900 {
		t.Errorf("glyph3 advance = %d, want 900", m.Advance)
	}
	if m.Bounds.Dx() != 900 || m.Bounds.Dy() != 700 {
		t.Errorf("glyph3 bounds = %v, want 900x700", m.Bounds)
	}
}

func TestGlyphIndex(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	tests := []struct {
		r    rune
		want GlyphIndex
	}{
		{'A', 1},
		{'B', 2},
		{'C', 3},
		{'D', 0},
		{'z', 0},
	}
	for _, tc := range tests {
		if got := f.GlyphIndex(tc.r); got != tc.want {
			t.Errorf("GlyphIndex(%q) = %d, want %d", tc.r, got, tc.want)
		}
	}
}

func TestKerning(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	tests := []struct {
		l, r rune
		want int
	}{
		{'A', 'B', -100},
		{'B', 'A', -50},
		{'A', 'A', 0},
		{'C', 'B', 0},
	}
	for _, tc := range tests {
		if got := f.KerningRunes(tc.l, tc.r); got != tc.want {
			t.Errorf("Kerning(%q,%q) = %d, want %d", tc.l, tc.r, got, tc.want)
		}
	}
}

func TestGlyphOutline(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	for _, g := range []GlyphIndex{0, 1, 2, 3} {
		o, err := f.GlyphOutline(g)
		if err != nil {
			t.Fatalf("GlyphOutline(%d): %v", g, err)
		}
		if len(o.Segments) == 0 {
			t.Errorf("glyph %d outline has no segments", g)
		}
	}
}

func TestBadGlyph(t *testing.T) {
	tf := newTestFont(t)
	f, _ := Parse(tf.fontBytes())
	for _, fn := range []func() error{
		func() error { _, err := f.GlyphMetrics(999); return err },
		func() error { _, err := f.GlyphOutline(999); return err },
		func() error { _, err := f.Rasterize(999, 48); return err },
	} {
		if err := fn(); !errors.Is(err, ErrBadGlyph) {
			t.Errorf("error = %v, want ErrBadGlyph", err)
		}
	}
}
