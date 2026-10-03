package font

import "fmt"

// kernTable maps packed (left<<16 | right) glyph pairs to adjustments.
type kernTable struct {
	pairs map[uint32]int16
}

func (k kernTable) get(l, r GlyphID) int16 {
	if k.pairs == nil {
		return 0
	}
	return k.pairs[uint32(l)<<16|uint32(r)]
}

// parseKern parses the 'kern' table (format 0 subtables). A missing table
// is not an error: it yields an empty table, i.e. all pairs kern to 0.
func parseKern(b []byte, numGlyphs int) (kernTable, error) {
	if b == nil {
		return kernTable{}, nil
	}
	r := newReader(b)
	version := r.u16()
	nSub := int(r.u16())
	off := 4
	if version == 0x0001 && nSub == 0 {
		// Apple new-style: 32-bit version 0x00010000, 32-bit count.
		r.off = 0
		r.u32()
		nSub = int(r.u32())
		off = 8
	} else if version != 0x0000 {
		return kernTable{}, fmt.Errorf("%w: unknown kern version %d.%d", ErrUnsupported, version, nSub)
	}
	if r.err != nil {
		return kernTable{}, r.err
	}
	if nSub > 64 {
		return kernTable{}, fmt.Errorf("%w: too many kern subtables %d", ErrMalformed, nSub)
	}
	kt := kernTable{pairs: make(map[uint32]int16)}
	for i := 0; i < nSub; i++ {
		if off+6 > len(b) {
			return kernTable{}, fmt.Errorf("%w: kern subtable %d header out of range", ErrMalformed, i)
		}
		sr := newReader(b[off:])
		sr.u16() // subtable version
		length := int(sr.u16())
		coverage := sr.u16()
		if sr.err != nil || length < 6 || off+length > len(b) {
			return kernTable{}, fmt.Errorf("%w: kern subtable %d length %d out of range", ErrMalformed, i, length)
		}
		format := coverage >> 8
		horizontal := coverage&0x01 != 0
		if format == 0 && horizontal {
			if err := parseKernFormat0(b[off:off+length], kt.pairs, numGlyphs); err != nil {
				return kernTable{}, err
			}
		}
		off += length
	}
	return kt, nil
}

func parseKernFormat0(b []byte, pairs map[uint32]int16, numGlyphs int) error {
	r := newReader(b)
	r.off = 6 // skip subtable header
	nPairs := int(r.u16())
	r.u16() // searchRange
	r.u16() // entrySelector
	r.u16() // rangeShift
	if r.err != nil {
		return r.err
	}
	if nPairs*6+14 > len(b) {
		return fmt.Errorf("%w: kern pair count %d exceeds subtable", ErrMalformed, nPairs)
	}
	for p := 0; p < nPairs; p++ {
		l := r.u16()
		rr := r.u16()
		v := r.i16()
		if r.err != nil {
			return r.err
		}
		if int(l) >= numGlyphs || int(rr) >= numGlyphs {
			// Tolerate out-of-range pairs by dropping them; some shipped
			// fonts contain stale entries.
			continue
		}
		pairs[uint32(l)<<16|uint32(rr)] = v
	}
	return nil
}
