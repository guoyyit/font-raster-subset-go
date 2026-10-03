package font

import (
	"fmt"
	"sort"
)

// cmap maps runes to glyph IDs. It is built once at parse time and is
// read-only afterwards.
type cmap struct {
	m map[rune]GlyphID
}

func (c cmap) lookup(r rune) (GlyphID, bool) {
	g, ok := c.m[r]
	return g, ok
}

// runes returns the sorted set of mapped runes (deterministic order).
func (c cmap) runes() []rune {
	out := make([]rune, 0, len(c.m))
	for r := range c.m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// parseCmap selects the best supported subtable and builds the mapping.
// Preference: format 12 (full Unicode) over format 4 (BMP).
func parseCmap(b []byte, numGlyphs int) (cmap, error) {
	r := newReader(b)
	r.u16() // version
	numTables := int(r.u16())
	if r.err != nil {
		return cmap{}, r.err
	}
	if numTables > 256 {
		return cmap{}, fmt.Errorf("%w: implausible cmap subtable count %d", ErrMalformed, numTables)
	}
	type rec struct {
		platform, encoding uint16
		off                uint32
	}
	recs := make([]rec, 0, numTables)
	for i := 0; i < numTables; i++ {
		p := r.u16()
		e := r.u16()
		o := r.u32()
		if r.err != nil {
			return cmap{}, r.err
		}
		if uint64(o) >= uint64(len(b)) {
			return cmap{}, fmt.Errorf("%w: cmap subtable offset %d out of range", ErrMalformed, o)
		}
		recs = append(recs, rec{p, e, o})
	}
	// Rank subtables: prefer Unicode platform / full repertoire.
	bestScore := -1
	var best []byte
	for _, rc := range recs {
		sub := b[rc.off:]
		sr := newReader(sub)
		format := sr.u16()
		if sr.err != nil {
			continue
		}
		score := -1
		switch format {
		case 12:
			score = 2
		case 4:
			score = 1
		}
		if score < 0 {
			continue
		}
		// Prefer Unicode platforms; among equal formats prefer platform 3.
		if rc.platform == 3 {
			score += 4
		} else if rc.platform == 0 {
			score += 2
		}
		if score > bestScore {
			bestScore = score
			best = sub
		}
	}
	if best == nil {
		return cmap{}, fmt.Errorf("%w: no supported cmap subtable (need format 4 or 12)", ErrUnsupported)
	}
	m := make(map[rune]GlyphID)
	var err error
	sr := newReader(best)
	switch sr.u16() {
	case 4:
		err = parseCmap4(sr, m, numGlyphs)
	case 12:
		err = parseCmap12(sr, m, numGlyphs)
	}
	if err != nil {
		return cmap{}, err
	}
	return cmap{m: m}, nil
}

func parseCmap4(r *reader, m map[rune]GlyphID, numGlyphs int) error {
	length := int(r.u16())
	r.u16() // language
	segCount := int(r.u16() / 2)
	r.u16() // searchRange
	r.u16() // entrySelector
	r.u16() // rangeShift
	if r.err != nil {
		return r.err
	}
	if segCount == 0 || segCount > 4096 {
		return fmt.Errorf("%w: bad cmap4 segCount %d", ErrMalformed, segCount)
	}
	// The subtable must be long enough for all arrays.
	minLen := 16 + 8*segCount
	if length < minLen || length > len(r.b) {
		return fmt.Errorf("%w: cmap4 length %d inconsistent", ErrMalformed, length)
	}
	endCode := r.slice(2 * segCount)
	r.u16() // reservedPad
	startCode := r.slice(2 * segCount)
	idDelta := r.slice(2 * segCount)
	idRangeOffBase := r.off
	idRangeOffset := r.slice(2 * segCount)
	if r.err != nil {
		return r.err
	}
	u16at := func(buf []byte, i int) uint16 {
		return uint16(buf[2*i])<<8 | uint16(buf[2*i+1])
	}
	for i := 0; i < segCount; i++ {
		start := u16at(startCode, i)
		end := u16at(endCode, i)
		delta := int16(u16at(idDelta, i))
		ro := u16at(idRangeOffset, i)
		if end < start {
			return fmt.Errorf("%w: cmap4 segment %d has end < start", ErrMalformed, i)
		}
		// Cap runaway segments: BMP only.
		for c := uint32(start); c <= uint32(end); c++ {
			if c == 0xFFFF {
				continue // .notdef sentinel
			}
			var gid uint16
			if ro == 0 {
				gid = uint16(int32(c) + int32(delta))
			} else {
				// Address of this idRangeOffset word, plus ro, plus 2*(c-start).
				pos := idRangeOffBase + 2*i + int(ro) + 2*int(c-uint32(start))
				if pos+2 > len(r.b) {
					return fmt.Errorf("%w: cmap4 glyphIdArray out of range", ErrMalformed)
				}
				gid = uint16(r.b[pos])<<8 | uint16(r.b[pos+1])
				if gid != 0 {
					gid = uint16(int32(gid) + int32(delta))
				}
			}
			if int(gid) >= numGlyphs {
				return fmt.Errorf("%w: cmap4 maps U+%04X to glyph %d >= numGlyphs %d",
					ErrMalformed, c, gid, numGlyphs)
			}
			m[rune(c)] = GlyphID(gid)
		}
	}
	return nil
}

func parseCmap12(r *reader, m map[rune]GlyphID, numGlyphs int) error {
	r.u16() // reserved
	length := r.u32()
	r.u32() // language
	nGroups := r.u32()
	if r.err != nil {
		return r.err
	}
	if uint64(length) > uint64(len(r.b)) || uint64(nGroups)*12+16 > uint64(length) {
		return fmt.Errorf("%w: cmap12 length %d groups %d inconsistent", ErrMalformed, length, nGroups)
	}
	if nGroups > 1<<20 {
		return fmt.Errorf("%w: cmap12 too many groups %d", ErrMalformed, nGroups)
	}
	for i := uint32(0); i < nGroups; i++ {
		start := r.u32()
		end := r.u32()
		startGID := r.u32()
		if r.err != nil {
			return r.err
		}
		if end < start || end > 0x10FFFF {
			return fmt.Errorf("%w: cmap12 bad group range %d..%d", ErrMalformed, start, end)
		}
		if uint64(startGID)+uint64(end-start) >= uint64(numGlyphs) {
			return fmt.Errorf("%w: cmap12 group maps beyond numGlyphs", ErrMalformed)
		}
		for c := start; c <= end; c++ {
			m[rune(c)] = GlyphID(startGID + (c - start))
		}
	}
	return nil
}
