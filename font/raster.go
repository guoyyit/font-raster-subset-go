package font

import (
	"math"
)

// rasterEdge is a flattened line segment in pixel space (y-down).
type rasterEdge struct {
	x0, y0 float64
	x1, y1 float64
	dir    float64 // +1 downward, -1 upward
}

const (
	ppemMax     = 1024
	flatness26p = 0.25 // pixels
)

// Rasterize renders glyph g at the given pixel size (ppem = pixels per em)
// into an anti-aliased grayscale bitmap.
func (f *Font) Rasterize(g GlyphIndex, ppem int) (*Bitmap, error) {
	if ppem <= 0 || ppem > ppemMax {
		return nil, ErrBadArgument
	}
	if int(g) >= len(f.hmtx) {
		return nil, ErrBadGlyph
	}
	scale := float64(ppem) / float64(f.head.unitsPerEm)
	adv := int(math.Round(float64(f.hmtx[g].advanceWidth) * scale))

	metrics, err := f.GlyphMetrics(g)
	if err != nil {
		return nil, err
	}
	b := metrics.Bounds
	if b.Empty() {
		return &Bitmap{Advance: adv, Pixels: []byte{}}, nil
	}
	fx0 := math.Floor(float64(b.Min.X) * scale)
	fx1 := math.Ceil(float64(b.Max.X) * scale)
	w := int(fx1 - fx0)
	yTop := int(math.Round(float64(b.Max.Y) * scale))
	sy1 := float64(b.Max.Y-b.Min.Y) * scale
	h := int(math.Ceil(sy1))
	if w <= 0 || h <= 0 {
		return &Bitmap{Advance: adv, Pixels: []byte{}}, nil
	}

	outline, err := f.GlyphOutline(g)
	if err != nil {
		return nil, err
	}
	edges := make([]rasterEdge, 0, len(outline.Segments))
	var cx, cy float64
	var sx, sy float64
	flush := func(x0, y0, x1, y1 float64) {
		if y0 == y1 {
			return
		}
		dir := 1.0
		ax, ay, bx, by := x0, y0, x1, y1
		if ay > by {
			dir = -1
			ax, ay, bx, by = x1, y1, x0, y0
		}
		edges = append(edges, rasterEdge{x0: ax, y0: ay, x1: bx, y1: by, dir: dir})
	}
	trX := func(v int) float64 { return float64(v)*scale - fx0 }
	trY := func(v int) float64 { return float64(b.Max.Y-v) * scale }
	for _, seg := range outline.Segments {
		switch seg.Op {
		case SegMoveTo:
			cx, cy = trX(seg.X0), trY(seg.Y0)
			sx, sy = cx, cy
		case SegLineTo:
			nx, ny := trX(seg.X1), trY(seg.Y1)
			flush(cx, cy, nx, ny)
			cx, cy = nx, ny
		case SegQuadTo:
			x1, y1 := trX(seg.X1), trY(seg.Y1)
			x2, y2 := trX(seg.X2), trY(seg.Y2)
			flattenQuad(cx, cy, x1, y1, x2, y2, flush)
			cx, cy = x2, y2
		case SegCubeTo:
			x1, y1 := trX(seg.X1), trY(seg.Y1)
			x2, y2 := trX(seg.X2), trY(seg.Y2)
			x3, y3 := trX(seg.X3), trY(seg.Y3)
			flattenCube(cx, cy, x1, y1, x2, y2, x3, y3, flush)
			cx, cy = x3, y3
		case SegClose:
			if cx != sx || cy != sy {
				flush(cx, cy, sx, sy)
			}
			cx, cy = sx, sy
		}
	}

	buf := make([]float64, w*h)
	rasterizeBands(edges, w, h, buf)

	// Coverage is the cumulative sum across each row (winding number).
	pix := make([]byte, w*h)
	for y := 0; y < h; y++ {
		acc := 0.0
		for x := 0; x < w; x++ {
			acc += buf[y*w+x]
			v := acc
			if v < 0 {
				v = -v
			}
			if v > 1 {
				v = 1
			}
			pix[y*w+x] = byte(math.Round(v * 255))
		}
	}
	return &Bitmap{
		Width:   w,
		Height:  h,
		Left:    int(math.Round(fx0)),
		Top:     yTop,
		Advance: adv,
		Pixels:  pix,
	}, nil
}

// flattenQuad subdivides a quadratic Bézier into line segments until flat.
func flattenQuad(x0, y0, x1, y1, x2, y2 float64, emit func(x0, y0, x1, y1 float64)) {
	dx := x2 - x0
	dy := y2 - y0
	d := math.Hypot(dx, dy)
	if d > 0 {
		cross := math.Abs(dx*(y1-y0) - dy*(x1-x0))
		if cross/d > flatness26p {
			mx0 := (x0 + x1) / 2
			my0 := (y0 + y1) / 2
			mx1 := (x1 + x2) / 2
			my1 := (y1 + y2) / 2
			ax := (mx0 + mx1) / 2
			ay := (my0 + my1) / 2
			flattenQuad(x0, y0, mx0, my0, ax, ay, emit)
			flattenQuad(ax, ay, mx1, my1, x2, y2, emit)
			return
		}
	}
	emit(x0, y0, x2, y2)
}

// flattenCube subdivides a cubic Bézier into line segments.
func flattenCube(x0, y0, x1, y1, x2, y2, x3, y3 float64, emit func(x0, y0, x1, y1 float64)) {
	dx := x3 - x0
	dy := y3 - y0
	d := math.Hypot(dx, dy)
	if d > 0 {
		c1 := math.Abs(dx*(y1-y0)-dy*(x1-x0)) / d
		c2 := math.Abs(dx*(y2-y0)-dy*(x2-x0)) / d
		if math.Max(c1, c2) > flatness26p {
			x01 := (x0 + x1) / 2
			y01 := (y0 + y1) / 2
			x12 := (x1 + x2) / 2
			y12 := (y1 + y2) / 2
			x23 := (x2 + x3) / 2
			y23 := (y2 + y3) / 2
			x012 := (x01 + x12) / 2
			y012 := (y01 + y12) / 2
			x123 := (x12 + x23) / 2
			y123 := (y12 + y23) / 2
			ax := (x012 + x123) / 2
			ay := (y012 + y123) / 2
			flattenCube(x0, y0, x01, y01, x012, y012, ax, ay, emit)
			flattenCube(ax, ay, x123, y123, x23, y23, x3, y3, emit)
			return
		}
	}
	emit(x0, y0, x3, y3)
}

// rasterizeBands accumulates per-edge coverage derivatives into buf (w*h).
// The final coverage is the cumulative sum across each row (winding number).
func rasterizeBands(edges []rasterEdge, w, h int, buf []float64) {
	for band := 0; band < h; band++ {
		bandTop := float64(band)
		bandBottom := bandTop + 1
		for _, e := range edges {
			if e.y1 <= bandTop || e.y0 >= bandBottom {
				continue
			}
			ya := math.Max(e.y0, bandTop)
			yb := math.Min(e.y1, bandBottom)
			if yb <= ya {
				continue
			}
			t0 := (ya - e.y0) / (e.y1 - e.y0)
			t1 := (yb - e.y0) / (e.y1 - e.y0)
			xa := e.x0 + (e.x1-e.x0)*t0
			xb := e.x0 + (e.x1-e.x0)*t1
			d := (yb - ya) * e.dir // signed band height (-1..1)
			addEdge(buf, w, band, xa, xb, d)
		}
	}
}

// addEdge adds one edge's coverage derivative to buf for a single band.
// Port of the x/image/vector trapezoid rasterizer (floating point).
func addEdge(buf []float64, w, band int, xa, xb, d float64) {
	x0, x1 := xa, xb
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	x0i := int(math.Floor(x0))
	x1i := int(math.Ceil(x1))
	if x1i <= x0i+1 {
		// Edge within one column: split by midpoint.
		xmf := 0.5*(xa+xb) - float64(x0i)
		if i := x0i; i >= 0 && i < w {
			buf[band*w+i] += d * (1 - xmf)
		}
		if i := x0i + 1; i >= 0 && i < w {
			buf[band*w+i] += d * xmf
		}
		return
	}
	s := 1 / (x1 - x0)
	x0f := x0 - float64(x0i)
	x1f := x1 - float64(x1i) + 1
	a0 := 0.5 * s * (1 - x0f) * (1 - x0f)
	am := 0.5 * s * x1f * x1f
	if i := x0i; i >= 0 && i < w {
		buf[band*w+i] += d * a0
	}
	if x1i == x0i+2 {
		if i := x0i + 1; i >= 0 && i < w {
			buf[band*w+i] += d * (1 - a0 - am)
		}
	} else {
		a1 := s * (1.5 - x0f)
		if i := x0i + 1; i >= 0 && i < w {
			buf[band*w+i] += d * (a1 - a0)
		}
		dTimesS := d * s
		for xi := x0i + 2; xi < x1i-1; xi++ {
			if i := xi; i >= 0 && i < w {
				buf[band*w+i] += dTimesS
			}
		}
		a2 := a1 + s*float64(x1i-x0i-3)
		if i := x1i - 1; i >= 0 && i < w {
			buf[band*w+i] += d * (1 - a2 - am)
		}
	}
	if i := x1i; i >= 0 && i < w {
		buf[band*w+i] += d * am
	}
}
