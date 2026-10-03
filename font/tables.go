package font

import "sort"

// tableRecord is a parsed sfnt table directory entry.
type tableRecord struct {
	tag      string
	checksum uint32
	offset   uint32
	length   uint32
}

type headTable struct {
	version            uint32
	fontRevision       uint32
	checksumAdjustment uint32
	magicNumber        uint32
	flags              uint16
	unitsPerEm         uint16
	created            int64
	modified           int64
	xMin, yMin         int16
	xMax, yMax         int16
	macStyle           uint16
	lowestRecPPEM      uint16
	fontDirectionHint  int16
	indexToLocFormat   int16
	glyphDataFormat    int16
}

type hheaTable struct {
	ascent, descent, lineGap      int16
	advanceWidthMax               uint16
	minLeftSideBearing            int16
	minRightSideBearing           int16
	xMaxExtent                    int16
	caretSlopeRise, caretSlopeRun int16
	caretOffset                   int16
	metricDataFormat              int16
	numberOfHMetrics              uint16
}

type maxpTable struct {
	version               uint32
	numGlyphs             uint16
	maxPoints             uint16
	maxContours           uint16
	maxCompositePoints    uint16
	maxCompositeContours  uint16
	maxZones              uint16
	maxTwilightPoints     uint16
	maxStorage            uint16
	maxFunctionDefs       uint16
	maxInstructionDefs    uint16
	maxStackElements      uint16
	maxSizeOfInstructions uint16
	maxComponentElements  uint16
	maxComponentDepth     uint16
}

type longHorMetric struct {
	advanceWidth uint16
	lsb          int16
}

type cmapGroup struct {
	start, end uint32
	startGlyph uint32
}

type cmapTable struct {
	format uint16
	// format 4
	segCount     uint16
	endCodes     []uint16
	startCodes   []uint16
	idDeltas     []int16
	idRangeOff   []uint16
	glyphIdArray []byte
	// format 12
	groups []cmapGroup
}

type kernPair struct {
	left, right GlyphIndex
	value       int16
}

type kernTable struct {
	pairs []kernPair
}

// ---- binary reading primitives ----

func u16(b []byte, o int) uint16 {
	return uint16(b[o])<<8 | uint16(b[o+1])
}

func i16(b []byte, o int) int16 { return int16(u16(b, o)) }

func u32(b []byte, o int) uint32 {
	return uint32(b[o])<<24 | uint32(b[o+1])<<16 | uint32(b[o+2])<<8 | uint32(b[o+3])
}

func i32(b []byte, o int) int32 { return int32(u32(b, o)) }

func u8(b []byte, o int) uint8 { return b[o] }

// table returns the bytes of the named table, or nil if absent.
func (f *Font) table(tag string) []byte {
	rec, ok := f.tables[tag]
	if !ok {
		return nil
	}
	o := int(rec.offset)
	l := int(rec.length)
	if o < 0 || l < 0 || o > len(f.data) || l > len(f.data)-o {
		return nil
	}
	return f.data[o : o+l]
}

// ---- table parsers ----

func parseHead(b []byte) (headTable, error) {
	if len(b) < 54 {
		return headTable{}, ErrTruncated
	}
	h := headTable{
		version:            u32(b, 0),
		fontRevision:       u32(b, 4),
		checksumAdjustment: u32(b, 8),
		magicNumber:        u32(b, 12),
		flags:              u16(b, 16),
		unitsPerEm:         u16(b, 18),
		created:            int64(u32(b, 20))<<32 | int64(u32(b, 24)),
		modified:           int64(u32(b, 28))<<32 | int64(u32(b, 32)),
		xMin:               i16(b, 36),
		yMin:               i16(b, 38),
		xMax:               i16(b, 40),
		yMax:               i16(b, 42),
		macStyle:           u16(b, 44),
		lowestRecPPEM:      u16(b, 46),
		fontDirectionHint:  i16(b, 48),
		indexToLocFormat:   i16(b, 50),
		glyphDataFormat:    i16(b, 52),
	}
	if h.magicNumber != 0x5F0F3CF5 {
		return headTable{}, ErrBadTable
	}
	if h.unitsPerEm == 0 {
		return headTable{}, ErrBadTable
	}
	if h.indexToLocFormat != 0 && h.indexToLocFormat != 1 {
		return headTable{}, ErrBadTable
	}
	return h, nil
}

func parseHhea(b []byte) (hheaTable, error) {
	if len(b) < 36 {
		return hheaTable{}, ErrTruncated
	}
	h := hheaTable{
		ascent:              i16(b, 0),
		descent:             i16(b, 2),
		lineGap:             i16(b, 4),
		advanceWidthMax:     u16(b, 6),
		minLeftSideBearing:  i16(b, 8),
		minRightSideBearing: i16(b, 10),
		xMaxExtent:          i16(b, 12),
		caretSlopeRise:      i16(b, 14),
		caretSlopeRun:       i16(b, 16),
		caretOffset:         i16(b, 18),
		metricDataFormat:    i16(b, 32),
		numberOfHMetrics:    u16(b, 34),
	}
	if h.metricDataFormat != 0 {
		return hheaTable{}, ErrUnsupportedVersion
	}
	return h, nil
}

func parseMaxp(b []byte) (maxpTable, error) {
	if len(b) < 6 {
		return maxpTable{}, ErrTruncated
	}
	m := maxpTable{version: u32(b, 0), numGlyphs: u16(b, 4)}
	switch m.version {
	case 0x00005000: // version 0.5
		if len(b) < 6 {
			return maxpTable{}, ErrTruncated
		}
	case 0x00010000: // version 1.0
		if len(b) < 32 {
			return maxpTable{}, ErrTruncated
		}
		m.maxPoints = u16(b, 6)
		m.maxContours = u16(b, 8)
		m.maxCompositePoints = u16(b, 10)
		m.maxCompositeContours = u16(b, 12)
		m.maxZones = u16(b, 14)
		m.maxTwilightPoints = u16(b, 16)
		m.maxStorage = u16(b, 18)
		m.maxFunctionDefs = u16(b, 20)
		m.maxInstructionDefs = u16(b, 22)
		m.maxStackElements = u16(b, 24)
		m.maxSizeOfInstructions = u16(b, 26)
		m.maxComponentElements = u16(b, 28)
		m.maxComponentDepth = u16(b, 30)
	default:
		return maxpTable{}, ErrUnsupportedVersion
	}
	return m, nil
}

func parseCmap(b []byte) (cmapTable, error) {
	if len(b) < 4 {
		return cmapTable{}, ErrTruncated
	}
	numTables := int(u16(b, 2))
	if numTables == 0 || len(b) < 4+8*numTables {
		return cmapTable{}, ErrBadTable
	}
	type sub struct {
		platform, encoding uint16
		offset             uint32
	}
	subs := make([]sub, 0, numTables)
	for i := 0; i < numTables; i++ {
		o := 4 + 8*i
		subs = append(subs, sub{
			platform: u16(b, o),
			encoding: u16(b, o+2),
			offset:   u32(b, o+4),
		})
	}
	// Prefer a full-Unicode format 12 subtable, then format 4.
	best := -1
	bestScore := -1
	for i, s := range subs {
		if int(s.offset) >= len(b) {
			continue
		}
		format := u16(b, int(s.offset))
		score := 0
		switch format {
		case 12:
			score = 2
		case 4:
			score = 1
		default:
			continue
		}
		// Prefer Unicode platform subtables.
		if s.platform == 0 || s.platform == 3 {
			score += 1
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	if best < 0 {
		return cmapTable{}, ErrUnsupportedVersion
	}
	off := int(subs[best].offset)
	format := u16(b, off)
	switch format {
	case 4:
		return parseCmap4(b, off)
	case 12:
		return parseCmap12(b, off)
	}
	return cmapTable{}, ErrUnsupportedVersion
}

func parseCmap4(b []byte, off int) (cmapTable, error) {
	if len(b) < off+14 {
		return cmapTable{}, ErrTruncated
	}
	segCount := int(u16(b, off+6)) / 2
	if segCount < 1 || len(b) < off+14+2*segCount*4+2 {
		return cmapTable{}, ErrBadTable
	}
	c := cmapTable{format: 4, segCount: uint16(segCount)}
	p := off + 14
	c.endCodes = make([]uint16, segCount)
	for i := range c.endCodes {
		c.endCodes[i] = u16(b, p+2*i)
	}
	p += 2*segCount + 2 // endCodes + reservedPad
	c.startCodes = make([]uint16, segCount)
	for i := range c.startCodes {
		c.startCodes[i] = u16(b, p+2*i)
	}
	p += 2 * segCount
	c.idDeltas = make([]int16, segCount)
	for i := range c.idDeltas {
		c.idDeltas[i] = i16(b, p+2*i)
	}
	p += 2 * segCount
	c.idRangeOff = make([]uint16, segCount)
	for i := range c.idRangeOff {
		c.idRangeOff[i] = u16(b, p+2*i)
	}
	p += 2 * segCount
	if p > len(b) {
		return cmapTable{}, ErrBadTable
	}
	c.glyphIdArray = b[p:]
	return c, nil
}

func parseCmap12(b []byte, off int) (cmapTable, error) {
	if len(b) < off+16 {
		return cmapTable{}, ErrTruncated
	}
	numGroups := u32(b, off+12)
	if uint64(len(b)) < uint64(off)+16+12*uint64(numGroups) {
		return cmapTable{}, ErrBadTable
	}
	c := cmapTable{format: 12, groups: make([]cmapGroup, numGroups)}
	p := off + 16
	for i := range c.groups {
		c.groups[i] = cmapGroup{
			start:      u32(b, p),
			end:        u32(b, p+4),
			startGlyph: u32(b, p+8),
		}
		p += 12
	}
	return c, nil
}

func (c *cmapTable) lookup(r uint32) GlyphIndex {
	switch c.format {
	case 4:
		for i := 0; i < int(c.segCount); i++ {
			if r < uint32(c.startCodes[i]) || r > uint32(c.endCodes[i]) {
				continue
			}
			if c.idRangeOff[i] == 0 {
				return GlyphIndex((r + uint32(int32(c.idDeltas[i]))) & 0xFFFF)
			}
			idx := int(c.idRangeOff[i])/2 + int(r-uint32(c.startCodes[i])) - int(c.segCount)
			if idx < 0 || idx >= len(c.glyphIdArray)/2 {
				return 0
			}
			g := u16(c.glyphIdArray, 2*idx)
			if g == 0 {
				return 0
			}
			return GlyphIndex((uint32(g) + uint32(int32(c.idDeltas[i]))) & 0xFFFF)
		}
		return 0
	case 12:
		i := sort.Search(len(c.groups), func(i int) bool { return c.groups[i].start > r })
		if i == 0 {
			return 0
		}
		g := c.groups[i-1]
		if r > g.end {
			return 0
		}
		return GlyphIndex(g.startGlyph + (r - g.start))
	}
	return 0
}

func parseKern(b []byte) (kernTable, error) {
	if len(b) < 4 {
		return kernTable{}, ErrTruncated
	}
	version := u16(b, 0)
	nTables := int(u16(b, 2))
	if version != 0 || nTables < 1 {
		return kernTable{}, ErrUnsupportedVersion
	}
	p := 4
	for i := 0; i < nTables; i++ {
		if len(b) < p+6 {
			return kernTable{}, ErrTruncated
		}
		subVersion := u16(b, p)
		length := int(u16(b, p+2))
		coverage := u16(b, p+4)
		if subVersion != 0 || length < 6 || len(b) < p+length {
			return kernTable{}, ErrBadTable
		}
		format := coverage >> 8
		horizontal := coverage&0x0001 != 0
		minimum := coverage&0x0002 != 0
		crossStream := coverage&0x0004 != 0
		if format == 0 && horizontal && !minimum && !crossStream {
			if len(b) < p+14 {
				return kernTable{}, ErrTruncated
			}
			nPairs := int(u16(b, p+6))
			if length < 14+6*nPairs {
				return kernTable{}, ErrBadTable
			}
			kt := kernTable{pairs: make([]kernPair, nPairs)}
			q := p + 14
			for j := 0; j < nPairs; j++ {
				kt.pairs[j] = kernPair{
					left:  GlyphIndex(u16(b, q)),
					right: GlyphIndex(u16(b, q+2)),
					value: i16(b, q+4),
				}
				q += 6
			}
			return kt, nil
		}
		p += length
	}
	return kernTable{}, nil
}
