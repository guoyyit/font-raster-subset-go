// Package font parses TrueType/OpenType (sfnt) font files and exposes
// metrics, character mapping, kerning and glyph outlines.
//
// A *Font is immutable after Parse returns and is safe for concurrent use.
package font

import "errors"

// Sentinel errors. Use errors.Is to distinguish failure classes.
var (
	// ErrNotFont indicates the input is not a recognized sfnt container.
	ErrNotFont = errors.New("font: not a recognized font file")
	// ErrMalformed indicates structurally invalid or truncated data.
	ErrMalformed = errors.New("font: malformed font data")
	// ErrUnsupported indicates a valid font using features we do not support
	// (e.g. CFF outlines, bitmap-only fonts).
	ErrUnsupported = errors.New("font: unsupported font feature")
)

// GlyphID is an index into the font's glyph table.
type GlyphID uint16

// Metrics holds font-wide horizontal metrics in font units.
type Metrics struct {
	UnitsPerEm uint16
	Ascender   int16
	Descender  int16
	LineGap    int16
}

// Bounds is a glyph bounding box in font units.
type Bounds struct {
	XMin, YMin, XMax, YMax int16
}

// Point is a single outline point in font units.
type Point struct {
	X, Y    int32
	OnCurve bool
}

// Outline is a glyph outline: a set of contours, each a closed list of
// points. Quadratic on/off-curve conventions follow the TrueType spec.
type Outline struct {
	Contours [][]Point
	Bounds   Bounds
}

// Font is a parsed sfnt font. It is safe for concurrent use.
type Font struct {
	metrics   Metrics
	head      headTable
	hmtx      hmtx
	cmap      cmap
	kern      kernTable
	glyf      glyfTable
	tables    tableDir // raw tables, for subsetting
	numGlyphs int
}

// Parse parses a TrueType font from b. It never panics on corrupt input;
// failures are reported as errors wrapping ErrNotFont, ErrMalformed or
// ErrUnsupported.
func Parse(b []byte) (f *Font, err error) {
	defer func() {
		if r := recover(); r != nil {
			f = nil
			err = ErrMalformed
		}
	}()
	dir, err := parseDirectory(b)
	if err != nil {
		return nil, err
	}
	head, err := parseHead(dir["head"])
	if err != nil {
		return nil, err
	}
	numGlyphs, err := parseMaxp(dir["maxp"])
	if err != nil {
		return nil, err
	}
	if numGlyphs == 0 {
		return nil, ErrMalformed
	}
	hhea, err := parseHhea(dir["hhea"])
	if err != nil {
		return nil, err
	}
	if int(hhea.numHmtx) > int(numGlyphs) {
		return nil, ErrMalformed
	}
	hm, err := parseHmtx(dir["hmtx"], int(hhea.numHmtx), int(numGlyphs))
	if err != nil {
		return nil, err
	}
	loc, err := parseLoca(dir["loca"], int(numGlyphs), head.indexToLocFormat == 1)
	if err != nil {
		return nil, err
	}
	cm, err := parseCmap(dir["cmap"], int(numGlyphs))
	if err != nil {
		return nil, err
	}
	kt, err := parseKern(dir["kern"], int(numGlyphs))
	if err != nil {
		return nil, err
	}
	return &Font{
		metrics: Metrics{
			UnitsPerEm: head.unitsPerEm,
			Ascender:   hhea.ascender,
			Descender:  hhea.descender,
			LineGap:    hhea.lineGap,
		},
		head:      head,
		hmtx:      hm,
		cmap:      cm,
		kern:      kt,
		glyf:      glyfTable{data: dir["glyf"], loc: loc, num: int(numGlyphs)},
		tables:    dir,
		numGlyphs: int(numGlyphs),
	}, nil
}

// Metrics returns font-wide metrics.
func (f *Font) Metrics() Metrics { return f.metrics }

// NumGlyphs returns the number of glyphs in the font.
func (f *Font) NumGlyphs() int { return f.numGlyphs }

// GlyphIndex maps r to a glyph ID. ok is false if the font has no mapping.
func (f *Font) GlyphIndex(r rune) (GlyphID, bool) { return f.cmap.lookup(r) }

// MappedRunes returns the sorted set of runes with a glyph mapping.
func (f *Font) MappedRunes() []rune { return f.cmap.runes() }

// AdvanceWidth returns the horizontal advance of g in font units.
// Out-of-range IDs return 0.
func (f *Font) AdvanceWidth(g GlyphID) uint16 {
	if int(g) >= len(f.hmtx.advances) {
		return 0
	}
	return f.hmtx.advances[g]
}

// GlyphBounds returns the bounding box of g in font units. Empty glyphs
// return a zero Bounds.
func (f *Font) GlyphBounds(g GlyphID) Bounds {
	raw, err := f.glyf.rawGlyph(g)
	if err != nil || len(raw) == 0 {
		return Bounds{}
	}
	h, err := parseGlyphHeader(raw)
	if err != nil {
		return Bounds{}
	}
	return h.bounds
}

// Outline returns the resolved outline of g (composite glyphs expanded).
func (f *Font) Outline(g GlyphID) (Outline, error) { return f.outline(g, 0) }

// Kern returns the kerning adjustment between left and right in font units.
// It returns 0 when no kerning data applies (missing table, unknown pair,
// or dropped out-of-range entries).
func (f *Font) Kern(left, right GlyphID) int16 { return f.kern.get(left, right) }

// --- accessors used by the subsetter ---

// Table returns the raw contents of a table, or nil if absent.
func (f *Font) Table(tag string) []byte { return f.tables[tag] }

// RawGlyph returns the raw glyf data for g (nil for empty glyphs).
func (f *Font) RawGlyph(g GlyphID) ([]byte, error) { return f.glyf.rawGlyph(g) }

// ComponentGlyphs returns the transitive closure of composite component
// glyph IDs referenced by g (not including g itself).
func (f *Font) ComponentGlyphs(g GlyphID) ([]GlyphID, error) {
	seen := map[GlyphID]bool{}
	var out []GlyphID
	var walk func(GlyphID, int) error
	walk = func(id GlyphID, depth int) error {
		if depth > maxCompositeDepth {
			return ErrMalformed
		}
		comps, err := f.components(id)
		if err != nil {
			return err
		}
		for _, c := range comps {
			if int(c.gid) >= f.numGlyphs {
				return ErrMalformed
			}
			if !seen[c.gid] {
				seen[c.gid] = true
				out = append(out, c.gid)
				if err := walk(c.gid, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(g, 0); err != nil {
		return nil, err
	}
	return out, nil
}

// KernPairs returns all kerning pairs in deterministic (sorted) order.
func (f *Font) KernPairs() []KernPair {
	out := make([]KernPair, 0, len(f.kern.pairs))
	for k, v := range f.kern.pairs {
		out = append(out, KernPair{Left: GlyphID(k >> 16), Right: GlyphID(k & 0xFFFF), Value: v})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessKern(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func lessKern(a, b KernPair) bool {
	if a.Left != b.Left {
		return a.Left < b.Left
	}
	return a.Right < b.Right
}

// KernPair is one kerning entry.
type KernPair struct {
	Left, Right GlyphID
	Value       int16
}
