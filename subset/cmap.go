package subset

import (
	"fmt"
	"sort"

	"github.com/guoyyit/font-raster-subset-go/font"
)

// cmapEntry is one rune -> new glyph ID mapping.
type cmapEntry struct {
	r   rune
	gid font.GlyphID
}

// buildCmap builds a cmap table mapping kept runes to their new glyph IDs.
// It emits a format 4 subtable when all runes fit in the BMP, otherwise a
// format 12 subtable. Entries are coalesced into segments where both the
// rune and the glyph ID increment by one.
func buildCmap(runeGid map[rune]font.GlyphID, newID map[font.GlyphID]font.GlyphID) ([]byte, error) {
	entries := make([]cmapEntry, 0, len(runeGid))
	maxRune := rune(0)
	for r, g := range runeGid {
		n, ok := newID[g]
		if !ok {
			return nil, fmt.Errorf("subset: internal error: rune U+%04X maps to dropped glyph", r)
		}
		entries = append(entries, cmapEntry{r, n})
		if r > maxRune {
			maxRune = r
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].r < entries[j].r })
	if maxRune > 0xFFFF {
		return buildCmap12(entries), nil
	}
	return buildCmap4(entries), nil
}

// segment is a run of consecutive runes whose glyph IDs are consecutive.
type segment struct {
	start, end, gidStart uint32
}

func coalesce(entries []cmapEntry) []segment {
	var segs []segment
	for _, e := range entries {
		r, g := uint32(e.r), uint32(e.gid)
		if n := len(segs); n > 0 {
			last := &segs[n-1]
			if last.end+1 == r && last.gidStart+(last.end-last.start)+1 == g {
				last.end = r
				continue
			}
		}
		segs = append(segs, segment{start: r, end: r, gidStart: g})
	}
	return segs
}

func buildCmap4(entries []cmapEntry) []byte {
	segs := coalesce(entries)
	segs = append(segs, segment{0xFFFF, 0xFFFF, 0}) // required terminator
	n := len(segs)
	var sub []byte
	sub = appendU16(sub, 4)
	sub = appendU16(sub, uint16(16+8*n))
	sub = appendU16(sub, 0) // language
	sub = appendU16(sub, uint16(2*n))
	// searchRange, entrySelector, rangeShift
	maxPow2, es := 1, 0
	for maxPow2*2 <= n {
		maxPow2 *= 2
		es++
	}
	sub = appendU16(sub, uint16(2*maxPow2))
	sub = appendU16(sub, uint16(es))
	sub = appendU16(sub, uint16(2*n-2*maxPow2))
	for _, s := range segs {
		sub = appendU16(sub, uint16(s.end))
	}
	sub = appendU16(sub, 0) // reservedPad
	for _, s := range segs {
		sub = appendU16(sub, uint16(s.start))
	}
	for _, s := range segs {
		// idDelta: gid = (rune + delta) mod 65536
		sub = appendU16(sub, uint16(uint32(s.gidStart)-s.start))
	}
	for range segs {
		sub = appendU16(sub, 0) // idRangeOffset: use delta
	}
	var b []byte
	b = appendU16(b, 0) // version
	b = appendU16(b, 1) // numTables
	b = appendU16(b, 3) // platform: Windows
	b = appendU16(b, 1) // encoding: Unicode BMP
	b = appendU32(b, 12)
	return append(b, sub...)
}

func buildCmap12(entries []cmapEntry) []byte {
	segs := coalesce(entries)
	var sub []byte
	sub = appendU16(sub, 12)
	sub = appendU16(sub, 0) // reserved
	sub = appendU32(sub, uint32(16+12*len(segs)))
	sub = appendU32(sub, 0) // language
	sub = appendU32(sub, uint32(len(segs)))
	for _, s := range segs {
		sub = appendU32(sub, s.start)
		sub = appendU32(sub, s.end)
		sub = appendU32(sub, s.gidStart)
	}
	var b []byte
	b = appendU16(b, 0)  // version
	b = appendU16(b, 1)  // numTables
	b = appendU16(b, 3)  // platform: Windows
	b = appendU16(b, 10) // encoding: full Unicode
	b = appendU32(b, 12)
	return append(b, sub...)
}
