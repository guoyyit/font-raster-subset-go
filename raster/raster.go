// Package raster renders glyph outlines into anti-aliased grayscale bitmaps.
//
// Rendering is deterministic: the same font, glyph and parameters always
// produce pixel-identical output, regardless of concurrency or platform.
// Filling uses the non-zero winding rule; scaling is linear in
// pixels-per-em and outlines are flattened with a fixed flatness
// tolerance, so results depend only on the inputs.
package raster

import (
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/guoyyit/font-raster-subset-go/font"
)

// Params configures rasterization of a single glyph.
type Params struct {
	// Size is the em size in pixels (pixels per em). Must be > 0.
	Size float64
	// MaxDim bounds the output bitmap dimension in pixels; 0 uses DefaultMaxDim.
	MaxDim int
}

// DefaultMaxDim is the default upper bound for bitmap width/height. It caps
// memory usage: a bitmap needs roughly Width*Height bytes.
const DefaultMaxDim = 4096

// ss is the supersampling factor: each pixel is an ss*ss subpixel grid.
const ss = 8

// ErrTooLarge indicates the requested bitmap exceeds the dimension bound.
var ErrTooLarge = errors.New("raster: bitmap dimension exceeds limit")

// Glyph renders g at p.Size pixels-per-em into an 8-bit grayscale image
// using non-zero winding fill. The returned image's Rect.Min is at
// (floor(xMin*scale), floor(yMin*scale)) in a y-down pixel space whose
// origin matches the font's baseline origin, so glyphs of the same font
// and size share a coordinate system. Empty glyphs produce an empty image.
func Glyph(f *font.Font, g font.GlyphID, p Params) (*image.Gray, error) {
	if p.Size <= 0 || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
		return nil, fmt.Errorf("raster: invalid size %v", p.Size)
	}
	if p.MaxDim <= 0 {
		p.MaxDim = DefaultMaxDim
	}
	o, err := f.Outline(g)
	if err != nil {
		return nil, err
	}
	upem := float64(f.Metrics().UnitsPerEm)
	scale := p.Size / upem
	if len(o.Contours) == 0 {
		return image.NewGray(image.Rectangle{}), nil
	}
	// Pixel-space bounds: y flips (font y-up -> image y-down).
	x0 := math.Floor(float64(o.Bounds.XMin) * scale)
	y0 := math.Floor(float64(-o.Bounds.YMax) * scale)
	x1 := math.Ceil(float64(o.Bounds.XMax) * scale)
	y1 := math.Ceil(float64(-o.Bounds.YMin) * scale)
	w, h := int(x1-x0), int(y1-y0)
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rectangle{}), nil
	}
	if w > p.MaxDim || h > p.MaxDim {
		return nil, fmt.Errorf("%w: %dx%d > %d", ErrTooLarge, w, h, p.MaxDim)
	}
	// Flatten all contours into polygons in pixel space.
	var polys [][]segment
	for _, contour := range o.Contours {
		pts := flattenContour(contour, scale)
		for i := 0; i < len(pts); i++ {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if a.y != b.y {
				polys = append(polys, []segment{{a, b}})
			}
		}
	}
	img := image.NewGray(image.Rect(int(x0), int(y0), int(x1), int(y1)))
	fill(img, polys)
	return img, nil
}

type fpoint struct{ x, y float64 }

type segment struct{ a, b fpoint }

// flattenContour converts a TrueType contour (on/off-curve quadratic
// points, in font units) to a closed polygon in pixel space.
func flattenContour(contour []font.Point, scale float64) []fpoint {
	if len(contour) == 0 {
		return nil
	}
	// Insert implicit on-curve points between consecutive off-curve points.
	type qp struct{ x, y float64 }
	pts := make([]qp, 0, len(contour)*2)
	var onFlags []bool
	n := len(contour)
	get := func(i int) font.Point { return contour[((i%n)+n)%n] }
	for i := 0; i < n; i++ {
		p := get(i)
		pts = append(pts, qp{float64(p.X), float64(p.Y)})
		onFlags = append(onFlags, p.OnCurve)
		next := get(i + 1)
		if !p.OnCurve && !next.OnCurve {
			pts = append(pts, qp{
				float64(p.X+next.X) / 2,
				float64(p.Y+next.Y) / 2,
			})
			onFlags = append(onFlags, true) // implicit midpoint is on-curve
		}
	}
	// Rotate so we start at an on-curve point (guaranteed to exist after
	// implicit midpoint insertion).
	s := 0
	for !onFlags[s] {
		s++
	}
	var out []fpoint
	toPix := func(p qp) fpoint { return fpoint{p.x * scale, -p.y * scale} }
	i := 0
	m := len(pts)
	cur := pts[(s)%m]
	out = append(out, toPix(cur))
	for i < m {
		next := pts[(s+i+1)%m]
		if onFlags[(s+i+1)%m] {
			out = append(out, toPix(next))
			cur = next
			i++
		} else {
			// Quadratic from cur, control next, to the following on-curve point.
			end := pts[(s+i+2)%m]
			out = appendQuadratic(out, toPix(cur), toPix(next), toPix(end))
			cur = end
			i += 2
		}
	}
	return out
}

// appendQuadratic flattens a quadratic Bezier with a fixed flatness
// tolerance, appending the resulting points (excluding the start) to out.
func appendQuadratic(out []fpoint, p0, p1, p2 fpoint) []fpoint {
	const flatness = 0.25 // pixels
	const maxDepth = 16
	var rec func(a, b, c fpoint, depth int)
	rec = func(a, b, c fpoint, depth int) {
		// Distance of control point from the chord.
		d := math.Abs((b.x-a.x)*(a.y-c.y)-(b.y-a.y)*(a.x-c.x)) /
			math.Hypot(c.x-a.x, c.y-a.y)
		if d <= flatness || depth >= maxDepth {
			out = append(out, c)
			return
		}
		ab := fpoint{(a.x + b.x) / 2, (a.y + b.y) / 2}
		bc := fpoint{(b.x + c.x) / 2, (b.y + c.y) / 2}
		mid := fpoint{(ab.x + bc.x) / 2, (ab.y + bc.y) / 2}
		rec(a, ab, mid, depth+1)
		rec(mid, bc, c, depth+1)
	}
	rec(p0, p1, p2, 0)
	return out
}

// fill rasterizes polygons into img with ss*ss supersampling and the
// non-zero winding rule.
func fill(img *image.Gray, polys [][]segment) {
	r := img.Bounds()
	if r.Empty() {
		return
	}
	var edges []segment
	for _, poly := range polys {
		edges = append(edges, poly...)
	}
	// coverage accumulates subpixel hits per pixel row.
	width := r.Dx()
	cover := make([]uint8, width)
	for sy := 0; sy < r.Dy()*ss; sy++ {
		// Subpixel scanline center in pixel space.
		y := float64(r.Min.Y) + (float64(sy)+0.5)/ss
		// Collect crossings.
		var xs []float64
		var winds []int
		for _, e := range edges {
			ay, by := e.a.y, e.b.y
			if ay > by {
				ay, by = by, ay
			}
			if y < ay || y >= by {
				continue
			}
			t := (y - e.a.y) / (e.b.y - e.a.y)
			x := e.a.x + t*(e.b.x-e.a.x)
			w := 1
			if e.a.y > e.b.y {
				w = -1
			}
			xs = append(xs, x)
			winds = append(winds, w)
		}
		if len(xs) == 0 {
			continue
		}
		// Sort crossings by x (insertion sort is fine and deterministic).
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
				winds[j], winds[j-1] = winds[j-1], winds[j]
			}
		}
		// Walk crossings, filling subpixel spans where winding != 0.
		winding := 0
		for i := 0; i < len(xs); i++ {
			prev := winding
			winding += winds[i]
			if prev == 0 && winding != 0 {
				// Span starts at xs[i]; find where it ends.
				start := xs[i]
				end := start
				for j := i + 1; j < len(xs); j++ {
					winding += winds[j]
					if winding == 0 {
						end = xs[j]
						i = j
						break
					}
				}
				markSpan(cover, r.Min.X, start, end)
			}
		}
		// Flush the row into the image every ss subpixel rows.
		if sy%ss == ss-1 {
			py := r.Min.Y + sy/ss
			row := img.Pix[(py-img.Rect.Min.Y)*img.Stride:]
			for x := 0; x < width; x++ {
				row[x] = uint8(uint16(cover[x]) * 255 / (ss * ss))
				cover[x] = 0
			}
		}
	}
}

// markSpan increments coverage for subpixels whose center falls in [x0,x1).
func markSpan(cover []uint8, originX int, x0, x1 float64) {
	// Subpixel column range in absolute pixel space.
	c0 := int(math.Floor(x0 * ss))
	c1 := int(math.Ceil(x1 * ss))
	for c := c0; c < c1; c++ {
		center := (float64(c) + 0.5) / ss
		if center >= x0 && center < x1 {
			px := c/ss - originX
			if px >= 0 && px < len(cover) {
				cover[px]++
			}
		}
	}
}
