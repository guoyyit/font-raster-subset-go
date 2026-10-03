package font

import "fmt"

// tableDir maps 4-byte table tags to their raw (unparsed) contents.
type tableDir map[string][]byte

// requiredTables lists tables every TrueType-outline font must have.
var requiredTables = []string{"head", "hhea", "maxp", "hmtx", "loca", "glyf", "cmap"}

// parseDirectory reads the sfnt offset table and table records.
func parseDirectory(b []byte) (tableDir, error) {
	r := newReader(b)
	version := r.u32()
	switch version {
	case 0x00010000, 0x74727565: // 1.0 or 'true'
	case 0x4F54544F: // 'OTTO' -> CFF outlines
		return nil, fmt.Errorf("%w: CFF (OTTO) outlines are not supported", ErrUnsupported)
	case 0x74746366: // 'ttcf' collection
		return nil, fmt.Errorf("%w: TrueType collections are not supported", ErrUnsupported)
	default:
		return nil, fmt.Errorf("%w: bad sfnt version 0x%08x", ErrNotFont, version)
	}
	numTables := int(r.u16())
	r.u16() // searchRange
	r.u16() // entrySelector
	r.u16() // rangeShift
	if r.err != nil {
		return nil, r.err
	}
	// Bound the table count to avoid huge allocations on corrupt input.
	if numTables > 512 {
		return nil, fmt.Errorf("%w: implausible table count %d", ErrMalformed, numTables)
	}
	dir := make(tableDir, numTables)
	for i := 0; i < numTables; i++ {
		tag := r.tag()
		r.u32() // checksum, verified lazily
		off := r.u32()
		length := r.u32()
		if r.err != nil {
			return nil, r.err
		}
		end := uint64(off) + uint64(length)
		if end > uint64(len(b)) {
			return nil, fmt.Errorf("%w: table %q at %d len %d exceeds file length %d",
				ErrMalformed, tag, off, length, len(b))
		}
		if _, dup := dir[tag]; dup {
			return nil, fmt.Errorf("%w: duplicate table %q", ErrMalformed, tag)
		}
		dir[tag] = b[off:end]
	}
	for _, tag := range requiredTables {
		if _, ok := dir[tag]; !ok {
			return nil, fmt.Errorf("%w: required table %q missing", ErrMalformed, tag)
		}
	}
	return dir, nil
}

// headTable holds parsed 'head' fields we need.
type headTable struct {
	unitsPerEm       uint16
	indexToLocFormat int16 // 0 = short, 1 = long
}

func parseHead(b []byte) (headTable, error) {
	r := newReader(b)
	var h headTable
	r.u32()          // version
	r.u32()          // fontRevision
	r.u32()          // checkSumAdjustment
	magic := r.u32() // magicNumber
	if magic != 0x5F0F3CF5 {
		return h, fmt.Errorf("%w: bad head magic 0x%08x", ErrMalformed, magic)
	}
	r.u16() // flags
	h.unitsPerEm = r.u16()
	r.off += 16 // created + modified
	r.off += 8  // xMin..yMax
	r.u16()     // macStyle
	r.u16()     // lowestRecPPEM
	r.i16()     // fontDirectionHint
	h.indexToLocFormat = r.i16()
	if r.err != nil {
		return h, r.err
	}
	if h.unitsPerEm == 0 {
		return h, fmt.Errorf("%w: unitsPerEm is zero", ErrMalformed)
	}
	if h.indexToLocFormat != 0 && h.indexToLocFormat != 1 {
		return h, fmt.Errorf("%w: bad indexToLocFormat %d", ErrMalformed, h.indexToLocFormat)
	}
	return h, nil
}

// hheaTable holds parsed 'hhea' fields.
type hheaTable struct {
	ascender  int16
	descender int16
	lineGap   int16
	numHmtx   uint16
}

func parseHhea(b []byte) (hheaTable, error) {
	r := newReader(b)
	var h hheaTable
	r.u32() // version
	h.ascender = r.i16()
	h.descender = r.i16()
	h.lineGap = r.i16()
	r.u16()    // advanceWidthMax
	r.off += 6 // minLSB, minRSB, xMaxExtent
	r.off += 6 // caretSlopeRise, caretSlopeRun, caretOffset
	r.off += 8 // reserved
	r.i16()    // metricDataFormat
	h.numHmtx = r.u16()
	if r.err != nil {
		return h, r.err
	}
	if h.numHmtx == 0 {
		return h, fmt.Errorf("%w: numberOfHMetrics is zero", ErrMalformed)
	}
	return h, nil
}

// parseMaxp returns numGlyphs.
func parseMaxp(b []byte) (uint16, error) {
	r := newReader(b)
	r.u32() // version
	n := r.u16()
	if r.err != nil {
		return 0, r.err
	}
	return n, nil
}

// hmtx holds horizontal metrics: full (advance, lsb) pairs plus a run of
// lsb-only entries for glyphs beyond numHmtx.
type hmtx struct {
	advances []uint16
}

func parseHmtx(b []byte, numHmtx, numGlyphs int) (hmtx, error) {
	r := newReader(b)
	adv := make([]uint16, numGlyphs)
	var last uint16
	for i := 0; i < numGlyphs; i++ {
		if i < numHmtx {
			last = r.u16()
			r.i16() // lsb
		}
		adv[i] = last
	}
	if r.err != nil {
		return hmtx{}, r.err
	}
	return hmtx{advances: adv}, nil
}
