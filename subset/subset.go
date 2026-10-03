// Package subset produces a minimized, structurally valid font containing
// only the glyphs needed for a given set of characters.
//
// Semantics:
//   - Glyph 0 (.notdef) is always retained.
//   - Runes without a mapping in the source font are silently skipped
//     (they contribute no glyphs); use font.Font.GlyphIndex to pre-check.
//   - Composite glyphs pull in their transitive component dependencies;
//     component references are remapped, never left dangling.
//   - Layout tables (GSUB/GPOS/GDEF etc.) are dropped: the output renders
//     kept glyphs identically to the source but performs no shaping.
//   - Output is deterministic: identical inputs produce identical bytes.
package subset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/guoyyit/font-raster-subset-go/font"
	"github.com/guoyyit/font-raster-subset-go/internal/sfntwrite"
)

// Options configures subsetting.
type Options struct {
	// Runes is the set of characters to keep. .notdef is always kept.
	Runes []rune
	// KeepKern retains kerning pairs between kept glyphs (default true).
	KeepKern *bool
	// MaxGlyphs bounds the output glyph count; 0 uses DefaultMaxGlyphs.
	// It guards memory usage on hostile inputs.
	MaxGlyphs int
}

// DefaultMaxGlyphs bounds glyphs in a subset output.
const DefaultMaxGlyphs = 65535

// ErrTooManyGlyphs indicates the subset would exceed the glyph bound.
var ErrTooManyGlyphs = errors.New("subset: glyph count exceeds limit")

// Subset returns a complete, valid font file containing only the glyphs
// reachable from opts.Runes (including composite-glyph dependencies).
func Subset(f *font.Font, opts Options) ([]byte, error) {
	if opts.MaxGlyphs <= 0 {
		opts.MaxGlyphs = DefaultMaxGlyphs
	}
	// 1. Collect the glyph closure.
	keep := map[font.GlyphID]bool{0: true}
	runeGid := map[rune]font.GlyphID{}
	for _, r := range opts.Runes {
		g, ok := f.GlyphIndex(r)
		if !ok {
			continue // defined behavior: unmapped runes are skipped
		}
		keep[g] = true
		runeGid[r] = g
	}
	// Transitive composite dependencies, in deterministic order.
	queue := sortedIDs(keep)
	for len(queue) > 0 {
		g := queue[0]
		queue = queue[1:]
		deps, err := f.ComponentGlyphs(g)
		if err != nil {
			return nil, fmt.Errorf("subset: glyph %d: %w", g, err)
		}
		for _, d := range deps {
			if !keep[d] {
				keep[d] = true
				queue = append(queue, d)
			}
		}
	}
	if len(keep) > opts.MaxGlyphs {
		return nil, fmt.Errorf("%w: %d > %d", ErrTooManyGlyphs, len(keep), opts.MaxGlyphs)
	}
	oldIDs := sortedIDs(keep)
	newID := make(map[font.GlyphID]font.GlyphID, len(oldIDs))
	for i, g := range oldIDs {
		newID[g] = font.GlyphID(i)
	}

	// 2. Rebuild glyf + loca (long format) with remapped composite refs.
	var glyf []byte
	loca := make([]uint32, 0, len(oldIDs)+1)
	for _, g := range oldIDs {
		loca = append(loca, uint32(len(glyf)))
		raw, err := f.RawGlyph(g)
		if err != nil {
			return nil, fmt.Errorf("subset: glyph %d: %w", g, err)
		}
		if len(raw) > 0 {
			raw = font.RewriteCompositeIDs(raw, func(id font.GlyphID) font.GlyphID {
				n, ok := newID[id]
				if !ok {
					// Cannot happen: closure includes all dependencies.
					return 0
				}
				return n
			})
			glyf = append(glyf, raw...)
			for len(glyf)%4 != 0 {
				glyf = append(glyf, 0)
			}
		}
	}
	loca = append(loca, uint32(len(glyf)))

	// 3. hmtx: one full record per kept glyph.
	var hmtx []byte
	for _, g := range oldIDs {
		hmtx = appendU16(hmtx, f.AdvanceWidth(g))
		hmtx = appendU16(hmtx, 0) // lsb
	}

	// 4. cmap from kept runes.
	cmap, err := buildCmap(runeGid, newID)
	if err != nil {
		return nil, err
	}

	// 5. Assemble tables.
	tables := map[string][]byte{
		"glyf": glyf,
		"loca": buildLoca(loca),
		"hmtx": hmtx,
		"cmap": cmap,
		"head": patchHead(f.Table("head")),
		"hhea": patchHhea(f.Table("hhea"), len(oldIDs)),
		"maxp": patchMaxp(f.Table("maxp"), len(oldIDs)),
		"post": postTable(),
	}
	if name := f.Table("name"); name != nil {
		tables["name"] = name
	}
	if os2 := f.Table("OS/2"); os2 != nil {
		tables["OS/2"] = os2
	}
	keepKern := opts.KeepKern == nil || *opts.KeepKern
	if keepKern {
		if kern := buildKern(f, newID); kern != nil {
			tables["kern"] = kern
		}
	}
	return sfntwrite.Assemble(tables), nil
}

func sortedIDs(m map[font.GlyphID]bool) []font.GlyphID {
	out := make([]font.GlyphID, 0, len(m))
	for g := range m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func appendU16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func appendI16(b []byte, v int16) []byte  { return appendU16(b, uint16(v)) }
func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func buildLoca(offsets []uint32) []byte {
	var b []byte
	for _, o := range offsets {
		b = appendU32(b, o)
	}
	return b
}

// patchHead copies the source head table and forces long loca format.
// checkSumAdjustment is recomputed by the assembler.
func patchHead(src []byte) []byte {
	out := make([]byte, 54)
	copy(out, src)
	binary.BigEndian.PutUint32(out[8:], 0)  // checkSumAdjustment
	binary.BigEndian.PutUint16(out[50:], 1) // indexToLocFormat = long
	return out
}

func patchHhea(src []byte, numGlyphs int) []byte {
	out := make([]byte, 36)
	copy(out, src)
	binary.BigEndian.PutUint16(out[34:], uint16(numGlyphs))
	return out
}

func patchMaxp(src []byte, numGlyphs int) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	binary.BigEndian.PutUint16(out[4:], uint16(numGlyphs))
	return out
}

// postTable emits a format 3.0 post table (no glyph names), which carries
// no glyph-index dependencies.
func postTable() []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint32(out[0:], 0x00030000)
	return out
}

// buildKern emits a format 0 kern table containing only pairs where both
// glyphs survived, with remapped IDs. Returns nil if no pairs remain.
func buildKern(f *font.Font, newID map[font.GlyphID]font.GlyphID) []byte {
	var pairs []font.KernPair
	for _, p := range f.KernPairs() {
		l, ok1 := newID[p.Left]
		r, ok2 := newID[p.Right]
		if ok1 && ok2 {
			pairs = append(pairs, font.KernPair{Left: l, Right: r, Value: p.Value})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	// pairs are already sorted by (old left, old right); re-sort by new IDs.
	sort.Slice(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.Left != b.Left {
			return a.Left < b.Left
		}
		return a.Right < b.Right
	})
	var sub []byte
	sub = appendU16(sub, 0) // version
	sub = appendU16(sub, uint16(14+6*len(pairs)))
	sub = appendU16(sub, 0x0001) // format 0, horizontal
	sub = appendU16(sub, uint16(len(pairs)))
	sub = appendU16(sub, 0)
	sub = appendU16(sub, 0)
	sub = appendU16(sub, 0)
	for _, p := range pairs {
		sub = appendU16(sub, uint16(p.Left))
		sub = appendU16(sub, uint16(p.Right))
		sub = appendI16(sub, p.Value)
	}
	var b []byte
	b = appendU16(b, 0) // version
	b = appendU16(b, 1) // nTables
	return append(b, sub...)
}
