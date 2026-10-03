package font

import "fmt"

// loca holds glyph offsets into the glyf table.
type loca struct {
	offsets []uint32 // numGlyphs+1 entries
}

func parseLoca(b []byte, numGlyphs int, longFormat bool) (loca, error) {
	r := newReader(b)
	n := numGlyphs + 1
	offsets := make([]uint32, n)
	if longFormat {
		for i := 0; i < n; i++ {
			offsets[i] = r.u32()
		}
	} else {
		for i := 0; i < n; i++ {
			offsets[i] = uint32(r.u16()) * 2
		}
	}
	if r.err != nil {
		return loca{}, r.err
	}
	for i := 1; i < n; i++ {
		if offsets[i] < offsets[i-1] {
			return loca{}, fmt.Errorf("%w: loca offsets not monotonic at glyph %d", ErrMalformed, i-1)
		}
	}
	return loca{offsets: offsets}, nil
}

// glyfTable provides access to raw glyph data.
type glyfTable struct {
	data []byte
	loc  loca
	num  int
}

// rawGlyph returns the raw glyf data for gid, or nil for an empty glyph.
func (g *glyfTable) rawGlyph(gid GlyphID) ([]byte, error) {
	if int(gid) >= g.num {
		return nil, fmt.Errorf("%w: glyph id %d out of range (%d glyphs)", ErrMalformed, gid, g.num)
	}
	start := g.loc.offsets[gid]
	end := g.loc.offsets[gid+1]
	if end < start || uint64(end) > uint64(len(g.data)) {
		return nil, fmt.Errorf("%w: glyph %d data range %d..%d invalid", ErrMalformed, gid, start, end)
	}
	return g.data[start:end], nil
}

// glyphHeader is the 10-byte header shared by simple and composite glyphs.
type glyphHeader struct {
	numContours int16
	bounds      Bounds
}

func parseGlyphHeader(raw []byte) (glyphHeader, error) {
	r := newReader(raw)
	var h glyphHeader
	h.numContours = r.i16()
	h.bounds = Bounds{r.i16(), r.i16(), r.i16(), r.i16()}
	if r.err != nil {
		return h, fmt.Errorf("%w: truncated glyph header", ErrMalformed)
	}
	return h, nil
}

// composite flags (glyf spec).
const (
	cmpArg1AreWords     = 0x0001
	cmpArgsAreXY        = 0x0002
	cmpHaveScale        = 0x0008
	cmpMoreComponents   = 0x0020
	cmpHaveXYScale      = 0x0040
	cmpHave2x2          = 0x0080
	cmpHaveInstructions = 0x0100
)

// component is one reference inside a composite glyph.
type component struct {
	gid    GlyphID
	dx, dy float64
	// 2x2 transform matrix; identity for simple offsets.
	m00, m01, m10, m11 float64
}

// parseComposite parses a composite glyph body (after the 10-byte header).
func parseComposite(raw []byte) ([]component, error) {
	r := newReader(raw)
	r.off = 10 // skip header
	var comps []component
	for {
		flags := r.u16()
		gid := GlyphID(r.u16())
		if r.err != nil {
			return nil, fmt.Errorf("%w: truncated composite glyph", ErrMalformed)
		}
		var a1, a2 int32
		if flags&cmpArg1AreWords != 0 {
			a1 = int32(int16(r.u16()))
			a2 = int32(int16(r.u16()))
		} else {
			a1 = int32(int8(r.u8()))
			a2 = int32(int8(r.u8()))
		}
		if flags&cmpArgsAreXY == 0 {
			return nil, fmt.Errorf("%w: composite uses point-matching arguments", ErrUnsupported)
		}
		c := component{gid: gid, dx: float64(a1), dy: float64(a2),
			m00: 1, m11: 1}
		f2dot14 := func() float64 { return float64(int16(r.u16())) / 16384.0 }
		switch {
		case flags&cmpHaveScale != 0:
			c.m00 = f2dot14()
			c.m11 = c.m00
		case flags&cmpHaveXYScale != 0:
			c.m00 = f2dot14()
			c.m11 = f2dot14()
		case flags&cmpHave2x2 != 0:
			c.m00 = f2dot14()
			c.m01 = f2dot14()
			c.m10 = f2dot14()
			c.m11 = f2dot14()
		}
		if r.err != nil {
			return nil, fmt.Errorf("%w: truncated composite glyph", ErrMalformed)
		}
		comps = append(comps, c)
		if flags&cmpMoreComponents == 0 {
			break
		}
		if len(comps) > 1<<14 {
			return nil, fmt.Errorf("%w: composite has too many components", ErrMalformed)
		}
	}
	return comps, nil
}

// parseSimpleGlyph decodes a simple (non-composite) glyph body into contours.
func parseSimpleGlyph(raw []byte, numContours int) ([][]Point, error) {
	r := newReader(raw)
	r.off = 10
	if numContours <= 0 || numContours > 4096 {
		return nil, fmt.Errorf("%w: bad contour count %d", ErrMalformed, numContours)
	}
	endPts := make([]int, numContours)
	total := 0
	for i := range endPts {
		endPts[i] = int(r.u16())
		if endPts[i] < total && i > 0 {
			return nil, fmt.Errorf("%w: contour end points not increasing", ErrMalformed)
		}
		total = endPts[i] + 1
	}
	if total > 1<<16 {
		return nil, fmt.Errorf("%w: too many points %d", ErrMalformed, total)
	}
	instrLen := int(r.u16())
	r.slice(instrLen) // skip instructions
	if r.err != nil {
		return nil, fmt.Errorf("%w: truncated simple glyph", ErrMalformed)
	}
	// Flags, with run-length repetition.
	flags := make([]uint8, total)
	for i := 0; i < total; {
		f := r.u8()
		rep := 1
		if f&0x08 != 0 { // REPEAT_FLAG
			rep = int(r.u8()) + 1
		}
		if i+rep > total {
			return nil, fmt.Errorf("%w: flag repeat overruns point count", ErrMalformed)
		}
		for j := 0; j < rep; j++ {
			flags[i+j] = f
		}
		i += rep
	}
	if r.err != nil {
		return nil, fmt.Errorf("%w: truncated simple glyph flags", ErrMalformed)
	}
	pts := make([]Point, total)
	// X coordinates: delta-encoded.
	var x int32
	for i := 0; i < total; i++ {
		f := flags[i]
		switch {
		case f&0x02 != 0: // X_SHORT
			v := int32(r.u8())
			if f&0x10 != 0 {
				x += v
			} else {
				x -= v
			}
		case f&0x10 != 0: // X_IS_SAME_OR_POSITIVE (no data, delta 0)
		default:
			x += int32(int16(r.u16()))
		}
		pts[i].X = x
	}
	var y int32
	for i := 0; i < total; i++ {
		f := flags[i]
		switch {
		case f&0x04 != 0: // Y_SHORT
			v := int32(r.u8())
			if f&0x20 != 0 {
				y += v
			} else {
				y -= v
			}
		case f&0x20 != 0:
		default:
			y += int32(int16(r.u16()))
		}
		pts[i].Y = y
		pts[i].OnCurve = flags[i]&0x01 != 0
	}
	if r.err != nil {
		return nil, fmt.Errorf("%w: truncated simple glyph coordinates", ErrMalformed)
	}
	contours := make([][]Point, numContours)
	prev := 0
	for i, e := range endPts {
		contours[i] = pts[prev : e+1]
		prev = e + 1
	}
	return contours, nil
}

// maxCompositeDepth bounds composite nesting to keep memory/time bounded.
const maxCompositeDepth = 16

// outline resolves gid to an outline, expanding composites recursively.
func (f *Font) outline(gid GlyphID, depth int) (Outline, error) {
	if depth > maxCompositeDepth {
		return Outline{}, fmt.Errorf("%w: composite nesting too deep", ErrMalformed)
	}
	raw, err := f.glyf.rawGlyph(gid)
	if err != nil {
		return Outline{}, err
	}
	if len(raw) == 0 {
		return Outline{}, nil // empty glyph (e.g. space)
	}
	h, err := parseGlyphHeader(raw)
	if err != nil {
		return Outline{}, err
	}
	out := Outline{Bounds: h.bounds}
	if h.numContours >= 0 {
		contours, err := parseSimpleGlyph(raw, int(h.numContours))
		if err != nil {
			return Outline{}, err
		}
		out.Contours = contours
		return out, nil
	}
	comps, err := parseComposite(raw)
	if err != nil {
		return Outline{}, err
	}
	for _, c := range comps {
		sub, err := f.outline(c.gid, depth+1)
		if err != nil {
			return Outline{}, fmt.Errorf("composite component %d: %w", c.gid, err)
		}
		for _, contour := range sub.Contours {
			tc := make([]Point, len(contour))
			for i, p := range contour {
				fx, fy := float64(p.X), float64(p.Y)
				tc[i] = Point{
					X:       int32(c.m00*fx + c.m01*fy + c.dx),
					Y:       int32(c.m10*fx + c.m11*fy + c.dy),
					OnCurve: p.OnCurve,
				}
			}
			out.Contours = append(out.Contours, tc)
		}
	}
	return out, nil
}

// components returns the direct component glyph IDs of a composite glyph,
// or nil for simple/empty glyphs.
func (f *Font) components(gid GlyphID) ([]component, error) {
	raw, err := f.glyf.rawGlyph(gid)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	h, err := parseGlyphHeader(raw)
	if err != nil {
		return nil, err
	}
	if h.numContours >= 0 {
		return nil, nil
	}
	return parseComposite(raw)
}

// RewriteCompositeIDs returns a copy of raw composite glyph data with every
// component glyph index replaced by mapID. It is used by the subsetter.
// Non-composite or empty input is returned unchanged.
func RewriteCompositeIDs(raw []byte, mapID func(GlyphID) GlyphID) []byte {
	if len(raw) < 10 {
		return raw
	}
	r := newReader(raw)
	h, err := parseGlyphHeader(raw)
	if err != nil || h.numContours >= 0 {
		return raw
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	r.off = 10
	for {
		flagsOff := r.off
		flags := r.u16()
		gidOff := r.off
		gid := GlyphID(r.u16())
		if r.err != nil {
			return raw // corrupt; leave unchanged, parse-time error will surface
		}
		nid := mapID(gid)
		out[gidOff] = byte(nid >> 8)
		out[gidOff+1] = byte(nid)
		// Skip arguments and optional transform.
		if flags&cmpArg1AreWords != 0 {
			r.off += 4
		} else {
			r.off += 2
		}
		switch {
		case flags&cmpHaveScale != 0:
			r.off += 2
		case flags&cmpHaveXYScale != 0:
			r.off += 4
		case flags&cmpHave2x2 != 0:
			r.off += 8
		}
		if r.err != nil || r.off > len(raw) {
			return raw
		}
		_ = flagsOff
		if flags&cmpMoreComponents == 0 {
			break
		}
	}
	return out
}
