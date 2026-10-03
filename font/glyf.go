package font

import "image"

// componentFlags (TrueType glyf composite flags).
const (
	flagArg1And2AreWords   = 0x0001
	flagArgsAreXYValues    = 0x0002
	flagRoundXYToGrid      = 0x0004
	flagWeHaveScale        = 0x0008
	flagReserved           = 0x0010
	flagMoreComponents     = 0x0020
	flagWeHaveXYScale      = 0x0040
	flagWeHave2x2          = 0x0080
	flagWeHaveInstructions = 0x0100
	flagUseMyMetrics       = 0x0200
	flagOverlapCompound    = 0x0400
)

type component struct {
	flags      uint16
	glyphIndex GlyphIndex
	arg1, arg2 int16
	// transform: x' = a*x + c*y + dx ; y' = b*x + d*y + dy
	a, b, c, d float64
}

type glyfPoint struct {
	x, y    int
	onCurve bool
}

type glyfGlyph struct {
	numberOfContours int16
	xMin, yMin       int16
	xMax, yMax       int16
	// simple glyph
	endPts []uint16
	points []glyfPoint
	ins    []byte
	// composite
	components []component
	// raw glyph bytes (slice into font data) for subsetting
	raw []byte
}

func (f *Font) glyphRaw(g GlyphIndex) []byte {
	if int(g) >= len(f.loca) {
		return nil
	}
	s := int(f.loca[g])
	e := int(f.loca[g+1])
	glyf := f.table("glyf")
	if s > e || s < 0 || e > len(glyf) {
		return nil
	}
	return glyf[s:e]
}

func (f *Font) loadGlyph(g GlyphIndex) (*glyfGlyph, error) {
	if int(g) >= len(f.hmtx) {
		return nil, ErrBadGlyph
	}
	raw := f.glyphRaw(g)
	if len(raw) == 0 {
		return &glyfGlyph{}, nil
	}
	if len(raw) < 10 {
		return nil, ErrTruncated
	}
	gl := &glyfGlyph{
		numberOfContours: i16(raw, 0),
		xMin:             i16(raw, 2),
		yMin:             i16(raw, 4),
		xMax:             i16(raw, 6),
		yMax:             i16(raw, 8),
		raw:              raw,
	}
	switch {
	case gl.numberOfContours == 0:
		return gl, nil
	case gl.numberOfContours > 0:
		if err := parseSimpleGlyph(gl, raw); err != nil {
			return nil, err
		}
	case gl.numberOfContours == -1:
		if err := parseCompositeGlyph(gl, raw); err != nil {
			return nil, err
		}
	default:
		return nil, ErrBadOutline
	}
	return gl, nil
}

func parseSimpleGlyph(gl *glyfGlyph, b []byte) error {
	n := int(gl.numberOfContours)
	if len(b) < 10+2*n+2 {
		return ErrTruncated
	}
	gl.endPts = make([]uint16, n)
	for i := 0; i < n; i++ {
		gl.endPts[i] = u16(b, 10+2*i)
	}
	numPoints := int(gl.endPts[n-1]) + 1
	p := 10 + 2*n
	insLen := int(u16(b, p))
	p += 2
	if len(b) < p+insLen {
		return ErrTruncated
	}
	gl.ins = b[p : p+insLen]
	p += insLen

	// Flags.
	flags := make([]byte, 0, numPoints)
	for len(flags) < numPoints {
		if p >= len(b) {
			return ErrTruncated
		}
		fl := b[p]
		p++
		flags = append(flags, fl)
		if fl&0x08 != 0 {
			if p >= len(b) {
				return ErrTruncated
			}
			rep := int(b[p])
			p++
			for i := 0; i < rep; i++ {
				flags = append(flags, fl)
			}
		}
	}
	if len(flags) != numPoints {
		return ErrBadOutline
	}
	gl.points = make([]glyfPoint, numPoints)

	// X coordinates.
	x := 0
	for i := 0; i < numPoints; i++ {
		fl := flags[i]
		switch {
		case fl&0x02 != 0: // x-short
			if p >= len(b) {
				return ErrTruncated
			}
			dx := int(b[p])
			p++
			if fl&0x10 == 0 {
				dx = -dx
			}
			x += dx
		case fl&0x10 == 0: // int16 delta
			if p+2 > len(b) {
				return ErrTruncated
			}
			x += int(i16(b, p))
			p += 2
		default: // same as previous
		}
		gl.points[i].x = x
		gl.points[i].onCurve = fl&0x01 != 0
	}
	// Y coordinates.
	y := 0
	for i := 0; i < numPoints; i++ {
		fl := flags[i]
		switch {
		case fl&0x04 != 0: // y-short
			if p >= len(b) {
				return ErrTruncated
			}
			dy := int(b[p])
			p++
			if fl&0x20 == 0 {
				dy = -dy
			}
			y += dy
		case fl&0x20 == 0: // int16 delta
			if p+2 > len(b) {
				return ErrTruncated
			}
			y += int(i16(b, p))
			p += 2
		default:
		}
		gl.points[i].y = y
	}
	return nil
}

func parseCompositeGlyph(gl *glyfGlyph, b []byte) error {
	p := 10
	for {
		if len(b) < p+4 {
			return ErrTruncated
		}
		flags := u16(b, p)
		glyphIndex := u16(b, p+2)
		p += 4
		c := component{flags: flags, glyphIndex: GlyphIndex(glyphIndex)}
		if flags&flagArg1And2AreWords != 0 {
			if len(b) < p+4 {
				return ErrTruncated
			}
			c.arg1 = i16(b, p)
			c.arg2 = i16(b, p+2)
			p += 4
		} else {
			if len(b) < p+2 {
				return ErrTruncated
			}
			if flags&flagArgsAreXYValues != 0 {
				c.arg1 = int16(int8(b[p]))
				c.arg2 = int16(int8(b[p+1]))
			} else {
				c.arg1 = int16(b[p])
				c.arg2 = int16(b[p+1])
			}
			p += 2
		}
		c.a, c.d = 1, 1
		switch {
		case flags&flagWeHaveScale != 0:
			if len(b) < p+2 {
				return ErrTruncated
			}
			s := float64(i16(b, p)) / 16384.0
			p += 2
			c.a, c.d = s, s
		case flags&flagWeHaveXYScale != 0:
			if len(b) < p+4 {
				return ErrTruncated
			}
			c.a = float64(i16(b, p)) / 16384.0
			c.d = float64(i16(b, p+2)) / 16384.0
			p += 4
		case flags&flagWeHave2x2 != 0:
			if len(b) < p+8 {
				return ErrTruncated
			}
			c.a = float64(i16(b, p)) / 16384.0
			c.b = float64(i16(b, p+2)) / 16384.0
			c.c = float64(i16(b, p+4)) / 16384.0
			c.d = float64(i16(b, p+6)) / 16384.0
			p += 8
		}
		gl.components = append(gl.components, c)
		if flags&flagMoreComponents == 0 {
			break
		}
		if flags&flagReserved != 0 {
			return ErrBadOutline
		}
	}
	return nil
}

// resolvedGlyph is a glyph with its points in final font coordinates.
type resolvedGlyph struct {
	points []glyfPoint
}

// resolveGlyph returns the glyph's points in font coordinates, applying
// composite transforms recursively.
func (f *Font) resolveGlyph(g GlyphIndex, depth int) (*resolvedGlyph, error) {
	if depth > 16 {
		return nil, ErrBadOutline
	}
	gl, err := f.loadGlyph(g)
	if err != nil {
		return nil, err
	}
	if gl.numberOfContours > 0 {
		return &resolvedGlyph{points: gl.points}, nil
	}
	if gl.numberOfContours == 0 {
		return &resolvedGlyph{}, nil
	}
	// Composite: merge components.
	var out []glyfPoint
	for _, c := range gl.components {
		child, err := f.resolveGlyph(c.glyphIndex, depth+1)
		if err != nil {
			return nil, err
		}
		dx, dy := 0.0, 0.0
		if c.flags&flagArgsAreXYValues != 0 {
			dx, dy = float64(c.arg1), float64(c.arg2)
		} else {
			// Point-matching alignment: arg1 is a parent point index,
			// arg2 is a child point index.
			pi, ci := int(c.arg1), int(c.arg2)
			if pi < 0 {
				pi = 0
			}
			if ci < 0 {
				ci = 0
			}
			if pi >= len(out) || ci >= len(child.points) {
				return nil, ErrBadOutline
			}
			dx = float64(out[pi].x) - (c.a*float64(child.points[ci].x) + c.c*float64(child.points[ci].y))
			dy = float64(out[pi].y) - (c.b*float64(child.points[ci].x) + c.d*float64(child.points[ci].y))
		}
		for _, pt := range child.points {
			x := c.a*float64(pt.x) + c.c*float64(pt.y) + dx
			y := c.b*float64(pt.x) + c.d*float64(pt.y) + dy
			out = append(out, glyfPoint{x: int(x), y: int(y), onCurve: pt.onCurve})
		}
	}
	return &resolvedGlyph{points: out}, nil
}

// GlyphOutline returns the resolved outline of glyph g in font units.
func (f *Font) GlyphOutline(g GlyphIndex) (*Outline, error) {
	if int(g) >= len(f.hmtx) {
		return nil, ErrBadGlyph
	}
	rg, err := f.resolveGlyph(g, 0)
	if err != nil {
		return nil, err
	}
	if len(rg.points) == 0 {
		return &Outline{}, nil
	}
	// Determine contour boundaries from the glyph's endPts (simple glyph) or
	// from the component structure. For composites we need per-component
	// contour boundaries; recompute by re-resolving with contour info.
	segs, err := f.pointsToSegments(g, rg)
	if err != nil {
		return nil, err
	}
	b := image.Rect(rg.points[0].x, rg.points[0].y, rg.points[0].x, rg.points[0].y)
	for _, p := range rg.points {
		b.Min.X = min(b.Min.X, p.x)
		b.Min.Y = min(b.Min.Y, p.y)
		b.Max.X = max(b.Max.X, p.x)
		b.Max.Y = max(b.Max.Y, p.y)
	}
	return &Outline{Segments: segs, Bounds: b}, nil
}

// contourEnds returns the end-point indices for each contour of a glyph,
// resolving composites recursively.
func (f *Font) contourEnds(g GlyphIndex, depth int) ([]int, error) {
	if depth > 16 {
		return nil, ErrBadOutline
	}
	gl, err := f.loadGlyph(g)
	if err != nil {
		return nil, err
	}
	if gl.numberOfContours > 0 {
		ends := make([]int, len(gl.endPts))
		for i, e := range gl.endPts {
			ends[i] = int(e)
		}
		return ends, nil
	}
	if gl.numberOfContours == 0 {
		return nil, nil
	}
	var ends []int
	offset := 0
	for _, c := range gl.components {
		child, err := f.resolveGlyph(c.glyphIndex, depth+1)
		if err != nil {
			return nil, err
		}
		childEnds, err := f.contourEnds(c.glyphIndex, depth+1)
		if err != nil {
			return nil, err
		}
		for _, e := range childEnds {
			ends = append(ends, offset+e)
		}
		offset += len(child.points)
	}
	return ends, nil
}

func (f *Font) pointsToSegments(g GlyphIndex, rg *resolvedGlyph) ([]Segment, error) {
	ends, err := f.contourEnds(g, 0)
	if err != nil {
		return nil, err
	}
	var segs []Segment
	start := 0
	for _, end := range ends {
		if end >= len(rg.points) {
			return nil, ErrBadOutline
		}
		pts := rg.points[start : end+1]
		start = end + 1
		if len(pts) == 0 {
			continue
		}
		// Find first on-curve point so the contour starts deterministically.
		first := 0
		for i, p := range pts {
			if p.onCurve {
				first = i
				break
			}
		}
		// Rotate points so the contour starts on an on-curve point.
		ordered := make([]glyfPoint, 0, len(pts)+1)
		for i := 0; i < len(pts); i++ {
			ordered = append(ordered, pts[(first+i)%len(pts)])
		}
		if !ordered[0].onCurve {
			// No on-curve point at all: insert the implied on-curve midpoint
			// between the last and first off-curve points.
			last := ordered[len(ordered)-1]
			mid := glyfPoint{
				x:       (last.x + ordered[0].x) / 2,
				y:       (last.y + ordered[0].y) / 2,
				onCurve: true,
			}
			ordered = append([]glyfPoint{mid}, ordered...)
		}
		segs = append(segs, Segment{Op: SegMoveTo, X0: ordered[0].x, Y0: ordered[0].y})
		for i := 1; i < len(ordered); i++ {
			p := ordered[i]
			if p.onCurve {
				segs = append(segs, Segment{Op: SegLineTo, X1: p.x, Y1: p.y})
				continue
			}
			// Off-curve control point. If the next point is also off-curve,
			// the on-curve point between them is implied.
			next := ordered[(i+1)%len(ordered)]
			nx, ny := next.x, next.y
			if !next.onCurve {
				nx = (p.x + next.x) / 2
				ny = (p.y + next.y) / 2
			}
			segs = append(segs, Segment{
				Op: SegQuadTo,
				X1: p.x, Y1: p.y,
				X2: nx, Y2: ny,
			})
		}
		segs = append(segs, Segment{Op: SegClose})
	}
	return segs, nil
}
