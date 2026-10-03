package font

import (
	"sort"
)

// subsetTables are the tables retained in a subset font, in the order they
// appear in the sfnt directory (tags are sorted by the writer anyway).
var subsetTables = []string{
	"head", "hhea", "maxp", "hmtx", "cmap", "loca", "glyf",
	"name", "post", "kern", "OS/2",
}

// Subset produces a new sfnt font containing only the glyphs needed to render
// the given runes. Glyph 0 (.notdef) is always retained. Composite glyph
// dependencies are retained automatically. Runes absent from the source
// character map map to glyph 0 in the subset.
func (f *Font) Subset(runes []rune) ([]byte, error) {
	// 1. Collect requested glyphs.
	kept := map[GlyphIndex]bool{0: true}
	for _, r := range runes {
		if r < 0 || r > 0x10FFFF {
			continue
		}
		g := f.GlyphIndex(r)
		if g != 0 {
			kept[g] = true
		}
	}
	// 2. Closure over composite glyph dependencies.
	if err := f.addCompositeDeps(kept); err != nil {
		return nil, err
	}
	// 3. Deterministic ordering: sort by old glyph index; new indices are
	// assigned in this order (glyph 0 stays 0).
	oldIDs := make([]GlyphIndex, 0, len(kept))
	for g := range kept {
		oldIDs = append(oldIDs, g)
	}
	sort.Slice(oldIDs, func(i, j int) bool { return oldIDs[i] < oldIDs[j] })
	remap := make(map[GlyphIndex]GlyphIndex, len(oldIDs))
	for newID, oldID := range oldIDs {
		remap[oldID] = GlyphIndex(newID)
	}
	numGlyphs := len(oldIDs)

	// 4. Build glyf + loca.
	glyfData := make([]byte, 0)
	loca := make([]uint32, numGlyphs+1)
	for i, oldID := range oldIDs {
		loca[i] = uint32(len(glyfData))
		raw := f.glyphRaw(oldID)
		if gl, err := f.loadGlyph(oldID); err == nil && gl.numberOfContours == -1 {
			raw = patchCompositeComponents(raw, gl, remap)
		}
		glyfData = append(glyfData, raw...)
	}
	loca[numGlyphs] = uint32(len(glyfData))

	// 5. Build hmtx.
	hmtxData := make([]byte, 4*numGlyphs)
	for i, oldID := range oldIDs {
		m := f.hmtx[oldID]
		putU16(hmtxData, 4*i, m.advanceWidth)
		putU16(hmtxData, 4*i+2, uint16(m.lsb))
	}

	// 6. Build cmap.
	runeNew := make(map[rune]GlyphIndex, len(runes))
	for _, r := range runes {
		if r < 0 || r > 0x10FFFF {
			continue
		}
		src := f.GlyphIndex(r)
		runeNew[r] = remap[src]
	}
	cmapData, err := buildSubsetCmap(runeNew)
	if err != nil {
		return nil, err
	}

	// 7. Assemble retained tables.
	tables := map[string][]byte{}
	for _, tag := range subsetTables {
		if tag == "kern" || tag == "OS/2" {
			if f.table(tag) == nil {
				continue
			}
		}
		switch tag {
		case "head":
			b := append([]byte(nil), f.table("head")...)
			putU32(b, 8, 0)  // checksumAdjustment (filled in later)
			putU16(b, 50, 1) // indexToLocFormat = long
			tables[tag] = b
		case "hhea":
			b := append([]byte(nil), f.table("hhea")...)
			putU16(b, 34, uint16(numGlyphs)) // numberOfHMetrics
			tables[tag] = b
		case "maxp":
			b := append([]byte(nil), f.table("maxp")...)
			putU16(b, 4, uint16(numGlyphs)) // numGlyphs
			tables[tag] = b
		case "hmtx":
			tables[tag] = hmtxData
		case "cmap":
			tables[tag] = cmapData
		case "loca":
			tables[tag] = buildLocaLong(loca)
		case "glyf":
			tables[tag] = glyfData
		case "name":
			tables[tag] = append([]byte(nil), f.table("name")...)
		case "post":
			tables[tag] = buildPost3()
		case "kern":
			if kb := buildSubsetKern(f.kern, remap); kb != nil {
				tables[tag] = kb
			}
		case "OS/2":
			tables[tag] = append([]byte(nil), f.table("OS/2")...)
		}
	}

	// 8. Assemble sfnt.
	return assembleSFNT(tables)
}

// addCompositeDeps expands the glyph set with all component glyphs
// referenced by composite glyphs.
func (f *Font) addCompositeDeps(kept map[GlyphIndex]bool) error {
	visited := map[GlyphIndex]bool{}
	var visit func(g GlyphIndex, depth int) error
	visit = func(g GlyphIndex, depth int) error {
		if depth > 16 {
			return ErrBadOutline
		}
		if visited[g] {
			return nil
		}
		visited[g] = true
		gl, err := f.loadGlyph(g)
		if err != nil {
			return err
		}
		for _, c := range gl.components {
			if int(c.glyphIndex) >= len(f.hmtx) {
				return ErrBadGlyph
			}
			kept[c.glyphIndex] = true
			if err := visit(c.glyphIndex, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for g := range kept {
		if err := visit(g, 0); err != nil {
			return err
		}
	}
	return nil
}

// patchCompositeComponents rewrites component glyph indices in a composite
// glyph's raw bytes using the remap table.
func patchCompositeComponents(raw []byte, gl *glyfGlyph, remap map[GlyphIndex]GlyphIndex) []byte {
	out := append([]byte(nil), raw...)
	p := 10
	for _, c := range gl.components {
		if p+4 > len(out) {
			return out
		}
		newID, ok := remap[c.glyphIndex]
		if !ok {
			newID = 0
		}
		putU16(out, p+2, uint16(newID))
		p += 4
		if c.flags&flagArg1And2AreWords != 0 {
			p += 4
		} else {
			p += 2
		}
		switch {
		case c.flags&flagWeHaveScale != 0:
			p += 2
		case c.flags&flagWeHaveXYScale != 0:
			p += 4
		case c.flags&flagWeHave2x2 != 0:
			p += 8
		}
	}
	return out
}

func buildLocaLong(loca []uint32) []byte {
	b := make([]byte, 4*len(loca))
	for i, v := range loca {
		putU32(b, 4*i, v)
	}
	return b
}

// buildPost3 returns a minimal post table (format 3.0, no glyph names).
func buildPost3() []byte {
	b := make([]byte, 32)
	putU32(b, 0, 0x00030000) // format 3.0
	// italicAngle (4), underlinePosition (2), underlineThickness (2),
	// isFixedPitch (4), minMemType42.. (4x4) are zero.
	return b
}

// buildSubsetKern rebuilds a format-0 kern table with remapped glyph ids,
// dropping pairs that reference removed glyphs.
func buildSubsetKern(k kernTable, remap map[GlyphIndex]GlyphIndex) []byte {
	type pair struct{ l, r, v int }
	var pairs []pair
	for _, p := range k.pairs {
		nl, ok1 := remap[p.left]
		nr, ok2 := remap[p.right]
		if !ok1 || !ok2 {
			continue
		}
		pairs = append(pairs, pair{int(nl), int(nr), int(p.value)})
	}
	if len(pairs) == 0 {
		return nil
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].l != pairs[j].l {
			return pairs[i].l < pairs[j].l
		}
		return pairs[i].r < pairs[j].r
	})
	n := len(pairs)
	subtableLen := 14 + 6*n
	b := make([]byte, 4+subtableLen)
	putU16(b, 0, 0x0000) // version
	putU16(b, 2, 1)      // nTables
	putU16(b, 4, 0)      // sub version
	putU16(b, 6, uint16(subtableLen))
	putU16(b, 8, 0x0001) // coverage: horizontal
	putU16(b, 10, uint16(n))
	// searchRange etc. (can be zero; parsers tolerate it)
	for i, p := range pairs {
		putU16(b, 18+6*i, uint16(p.l))
		putU16(b, 18+6*i+2, uint16(p.r))
		putU16(b, 18+6*i+4, uint16(int16(p.v)))
	}
	return b
}

// buildSubsetCmap builds a cmap mapping runes to new glyph ids.
func buildSubsetCmap(runeNew map[rune]GlyphIndex) ([]byte, error) {
	// Deduplicate and sort runes.
	rs := make([]rune, 0, len(runeNew))
	for r := range runeNew {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })

	useFmt4 := true
	for _, r := range rs {
		if r > 0xFFFF {
			useFmt4 = false
			break
		}
	}
	var subtable []byte
	var platformID, encodingID uint16
	var err error
	if useFmt4 {
		subtable, err = buildCmap4(rs, runeNew)
		platformID, encodingID = 3, 1 // Windows BMP
	} else {
		subtable, err = buildCmap12(rs, runeNew)
		platformID, encodingID = 3, 10 // Windows UCS-4
	}
	if err != nil {
		return nil, err
	}
	// Wrap in a cmap header with one encoding record.
	out := make([]byte, 12+len(subtable))
	putU16(out, 0, 0) // version
	putU16(out, 2, 1) // numTables
	putU16(out, 4, platformID)
	putU16(out, 6, encodingID)
	putU32(out, 8, 12) // offset to subtable
	copy(out[12:], subtable)
	return out, nil
}

func buildCmap4(rs []rune, runeNew map[rune]GlyphIndex) ([]byte, error) {
	// Group consecutive codepoints into segments.
	type seg struct{ start, end rune }
	var segs []seg
	for _, r := range rs {
		if len(segs) > 0 && r == segs[len(segs)-1].end+1 {
			segs[len(segs)-1].end = r
		} else {
			segs = append(segs, seg{r, r})
		}
	}
	// Sentinel segment.
	segs = append(segs, seg{0xFFFF, 0xFFFF})
	if len(segs) > 0xFFFF {
		return nil, ErrBadTable
	}
	segCount := len(segs)

	// glyphIdArray: one entry per codepoint per segment.
	glyphArray := make([]uint16, 0)
	idRangeOff := make([]uint16, segCount)
	for i, s := range segs {
		if s.start == 0xFFFF {
			idRangeOff[i] = 0
			continue
		}
		arrayStart := len(glyphArray)
		for r := s.start; r <= s.end; r++ {
			glyphArray = append(glyphArray, uint16(runeNew[r]))
		}
		// idx = idRangeOff/2 + (c - start) - segCount = arrayStart + (c-start)
		idRangeOff[i] = uint16(2 * (segCount + arrayStart))
	}

	length := 14 + 2*segCount*4 + 2 + 2*len(glyphArray)
	b := make([]byte, length)
	putU16(b, 0, 4) // format
	putU16(b, 2, uint16(length))
	putU16(b, 4, 0) // language
	putU16(b, 6, uint16(2*segCount))
	// searchRange/entrySelector/rangeShift (computed per spec).
	searchRange := uint16(1)
	entrySelector := uint16(0)
	for searchRange*2 <= uint16(segCount) {
		searchRange *= 2
		entrySelector++
	}
	searchRange *= 2
	putU16(b, 8, searchRange)
	putU16(b, 10, entrySelector)
	putU16(b, 12, uint16(2*segCount)-searchRange)
	p := 14
	for _, s := range segs {
		putU16(b, p, uint16(s.end))
		p += 2
	}
	putU16(b, p, 0) // reservedPad
	p += 2
	for _, s := range segs {
		putU16(b, p, uint16(s.start))
		p += 2
	}
	for i := range segs {
		if segs[i].start == 0xFFFF {
			putU16(b, p, 1) // idDelta
		} else {
			putU16(b, p, 0)
		}
		p += 2
	}
	for i := range segs {
		putU16(b, p, idRangeOff[i])
		p += 2
	}
	for _, g := range glyphArray {
		putU16(b, p, g)
		p += 2
	}
	return b, nil
}

func buildCmap12(rs []rune, runeNew map[rune]GlyphIndex) ([]byte, error) {
	// Groups: maximal runs of consecutive codepoints whose new glyph ids are
	// also consecutive.
	type group struct{ start, end, startGlyph uint32 }
	var groups []group
	for _, r := range rs {
		ng := uint32(runeNew[r])
		if len(groups) > 0 {
			last := &groups[len(groups)-1]
			if uint32(r) == last.end+1 && ng == last.startGlyph+(last.end-last.start+1) {
				last.end = uint32(r)
				continue
			}
		}
		groups = append(groups, group{uint32(r), uint32(r), ng})
	}
	length := 16 + 12*len(groups)
	b := make([]byte, length)
	putU16(b, 0, 12)
	putU16(b, 2, 0) // reserved
	putU32(b, 4, uint32(length))
	putU32(b, 8, 0) // language
	putU32(b, 12, uint32(len(groups)))
	p := 16
	for _, g := range groups {
		putU32(b, p, g.start)
		putU32(b, p+4, g.end)
		putU32(b, p+8, g.startGlyph)
		p += 12
	}
	return b, nil
}

// assembleSFNT writes the sfnt offset table, table directory and table data,
// computing checksums and the head checksumAdjustment.
func assembleSFNT(tables map[string][]byte) ([]byte, error) {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	numTables := len(tags)
	headerLen := 12 + 16*numTables
	out := make([]byte, headerLen)
	putU32(out, 0, 0x00010000)
	putU16(out, 4, uint16(numTables))
	// searchRange / entrySelector / rangeShift.
	pow := 1
	entrySelector := 0
	for pow*2 <= numTables {
		pow *= 2
		entrySelector++
	}
	searchRange := uint16(pow * 16)
	putU16(out, 6, searchRange)
	putU16(out, 8, uint16(entrySelector))
	putU16(out, 10, uint16(numTables*16)-searchRange)

	// Table records + data.
	dataOff := headerLen
	for _, tag := range tags {
		data := tables[tag]
		rec := 12 + 16*indexOf(tags, tag)
		putTag(out, rec, tag)
		putU32(out, rec+8, uint32(dataOff))
		putU32(out, rec+12, uint32(len(data)))
		out = append(out, data...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		// checksum of padded data
		sum := checksumTable(data)
		putU32(out, rec+4, sum)
		dataOff = len(out)
	}
	// head checksumAdjustment.
	headRec := -1
	for i, tag := range tags {
		if tag == "head" {
			headRec = i
		}
	}
	if headRec < 0 {
		return nil, ErrMissingTable
	}
	headOff := int(u32(out, 12+16*headRec+8))
	var total uint32
	for i := 0; i+4 <= len(out); i += 4 {
		total += u32(out, i)
	}
	adjustment := uint32(0xB1B0AFBA) - total
	putU32(out, headOff+8, adjustment)
	return out, nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func checksumTable(data []byte) uint32 {
	var sum uint32
	n := len(data) &^ 3
	for i := 0; i < n; i += 4 {
		sum += u32(data, i)
	}
	if rem := len(data) & 3; rem != 0 {
		var tail [4]byte
		copy(tail[:], data[n:])
		sum += u32(tail[:], 0)
	}
	return sum
}

func putU16(b []byte, o int, v uint16) {
	b[o] = byte(v >> 8)
	b[o+1] = byte(v)
}

func putU32(b []byte, o int, v uint32) {
	b[o] = byte(v >> 24)
	b[o+1] = byte(v >> 16)
	b[o+2] = byte(v >> 8)
	b[o+3] = byte(v)
}

func putTag(b []byte, o int, tag string) {
	b[o] = tag[0]
	b[o+1] = tag[1]
	b[o+2] = tag[2]
	b[o+3] = tag[3]
}
