package font

import (
	"bytes"
	"os"
	"testing"
)

func TestRealFontSubsetAndRaster(t *testing.T) {
	fonts := []string{
		"/usr/share/fonts/ubuntu/UbuntuMono-R.ttf",
		"/usr/share/fonts/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
	}
	for _, path := range fonts {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("font not found: %s", path)
		}
		f, err := Parse(data)
		if err != nil {
			t.Errorf("Parse(%s): %v", path, err)
			continue
		}
		// Subset
		runes := []rune("Hello, World! 0123456789")
		sub, err := f.Subset(runes)
		if err != nil {
			t.Errorf("Subset(%s): %v", path, err)
			continue
		}
		f2, err := Parse(sub)
		if err != nil {
			t.Errorf("Subset parse(%s): %v", path, err)
			continue
		}
		// Verify all runes map correctly
		for _, r := range runes {
			g1 := f.GlyphIndex(r)
			g2 := f2.GlyphIndex(r)
			if g2 == 0 && g1 != 0 {
				t.Errorf("%s: rune %q not in subset", path, r)
			}
		}
		// Verify rendering matches
		for _, r := range []rune{'H', 'e', 'l', 'o', '0', '9'} {
			g1 := f.GlyphIndex(r)
			g2 := f2.GlyphIndex(r)
			b1, _ := f.Rasterize(g1, 48)
			b2, _ := f2.Rasterize(g2, 48)
			if !bytes.Equal(b1.Pixels, b2.Pixels) {
				t.Errorf("%s: %q raster mismatch", path, r)
			}
		}
		// Determinism
		sub2, _ := f.Subset(runes)
		if !bytes.Equal(sub, sub2) {
			t.Errorf("%s: subset not deterministic", path)
		}
	}
}

func TestRealFontKerning(t *testing.T) {
	path := "/usr/share/fonts/dejavu/DejaVuSans.ttf"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("font not found")
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	// These pairs should have non-zero kerning
	pairs := [][2]rune{{'A', 'V'}, {'T', 'o'}, {'L', 'T'}}
	for _, p := range pairs {
		k := f.KerningRunes(p[0], p[1])
		if k == 0 {
			t.Errorf("kerning(%q,%q) = 0, want non-zero", p[0], p[1])
		}
	}
	// Missing pair should return 0 (use a pair unlikely to be kerned)
	if k := f.KerningRunes('Q', 'Q'); k != 0 {
		t.Errorf("kerning(Q,Q) = %d, want 0", k)
	}
}

func TestRealFontLarge(t *testing.T) {
	// Test with a large CJK font if available
	path := "/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc"
	if _, err := os.Stat(path); err != nil {
		t.Skip("CJK font not found")
	}
	// TTC is unsupported, should return ErrUnsupportedFormat
	data, _ := os.ReadFile(path)
	_, err := Parse(data)
	if err != ErrUnsupportedFormat {
		t.Errorf("Parse(TTC) error = %v, want ErrUnsupportedFormat", err)
	}
}
