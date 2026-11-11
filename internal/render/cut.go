package render

import (
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Cutting (docs/printing.md#lasers): a laser cuts along the outlines of a
// template's shapes marked cut, after engraving the rest. Those shapes are
// drawn on every other printer as they are designed, and left out of what a
// laser engraves.

// CutPath is one outline to cut, in millimeters from the label's top left
// corner as it prints: its points in order, back to the first when Closed.
type CutPath struct {
	Points [][2]float64
	Closed bool
}

// cutTolerance is how far a cut's straight pieces may stray from a curve,
// in millimeters: well under a laser's kerf, so the edge is smooth.
const cutTolerance = 0.02

// CutPaths are the outlines of a template's shapes marked cut, as vectors,
// in millimeters: each turned and mirrored as the shape is, then as the
// label prints. Curves are followed in straight pieces within cutTolerance
// of them.
func CutPaths(t *labels.Template) []CutPath {
	mm := t.InchesPerUnit() * 25.4
	var out []CutPath
	for i := range t.Elements {
		e := &t.Elements[i]
		if e.Type != labels.ElementShape || !e.Cut {
			continue
		}
		for _, p := range shapeOutlines(e, mm) {
			for k, q := range p.Points {
				q = placeInBox(e, q)
				x, y := t.Printed(q[0], q[1])
				p.Points[k] = [2]float64{x * mm, y * mm}
			}
			if len(p.Points) >= 2 {
				out = append(out, p)
			}
		}
	}
	return out
}

// WithoutCuts is a template without its shapes marked cut, which a laser
// cuts rather than engraves; the template itself when it has none.
func WithoutCuts(t *labels.Template) *labels.Template {
	kept := make([]labels.Element, 0, len(t.Elements))
	for _, e := range t.Elements {
		if !(e.Type == labels.ElementShape && e.Cut) {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(t.Elements) {
		return t
	}
	c := *t
	c.Elements = kept
	return &c
}

// placeInBox mirrors a point of an element as the element is mirrored in
// its box, then turns it clockwise about the box's center, as Layout does.
func placeInBox(e *labels.Element, p [2]float64) [2]float64 {
	if e.Mirror == labels.MirrorAcross || e.Mirror == labels.MirrorBoth {
		p[0] = 2*e.X + e.Width - p[0]
	}
	if e.Mirror == labels.MirrorDown || e.Mirror == labels.MirrorBoth {
		p[1] = 2*e.Y + e.Height - p[1]
	}
	if e.Rotation == 0 {
		return p
	}
	sin, cos := labels.SinCos(e.Rotation)
	cx, cy := e.X+e.Width/2, e.Y+e.Height/2
	dx, dy := p[0]-cx, p[1]-cy
	return [2]float64{cx + dx*cos - dy*sin, cy + dx*sin + dy*cos}
}

// shapeOutlines are a shape's outlines in the template's units, before it
// is mirrored or turned: its edge, not its stroke, which is only how it is
// drawn.
func shapeOutlines(e *labels.Element, mm float64) []CutPath {
	tol := cutTolerance / mm
	x0, y0, x1, y1 := e.X, e.Y, e.X+e.Width, e.Y+e.Height
	switch e.Shape {
	case labels.ShapeRectangle:
		r := math.Min(e.CornerRadius, math.Min(e.Width, e.Height)/2)
		if r <= 0 {
			return []CutPath{{Points: [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}, Closed: true}}
		}
		var pts [][2]float64
		// Each corner's arc, clockwise from the top right.
		corners := [][3]float64{{x1 - r, y0 + r, -90}, {x1 - r, y1 - r, 0}, {x0 + r, y1 - r, 90}, {x0 + r, y0 + r, 180}}
		for _, c := range corners {
			pts = append(pts, arc(c[0], c[1], r, r, c[2], 90, tol)...)
		}
		return []CutPath{{Points: pts, Closed: true}}
	case labels.ShapeEllipse:
		pts := arc((x0+x1)/2, (y0+y1)/2, (x1-x0)/2, (y1-y0)/2, 0, 360, tol)
		return []CutPath{{Points: pts[:len(pts)-1], Closed: true}}
	case labels.ShapeLine:
		if e.Flip {
			return []CutPath{{Points: [][2]float64{{x0, y1}, {x1, y0}}}}
		}
		return []CutPath{{Points: [][2]float64{{x0, y0}, {x1, y1}}}}
	case labels.ShapePolygon:
		pts := make([][2]float64, len(e.Points))
		for i, p := range e.Points {
			pts[i] = [2]float64{x0 + p[0]*(x1-x0), y0 + p[1]*(y1-y0)}
		}
		return []CutPath{{Points: pts, Closed: e.Closed == nil || *e.Closed}}
	case labels.ShapePath:
		return pathOutlines(e, x0, y0, x1, y1, mm)
	}
	return nil
}

// arc is points along an ellipse's arc about (cx, cy), from start degrees
// clockwise through sweep, both ends included, close enough together to
// stay within tol of it.
func arc(cx, cy, rx, ry, start, sweep, tol float64) [][2]float64 {
	n := curvePieces(math.Max(rx, ry), sweep, tol)
	pts := make([][2]float64, 0, n+1)
	for k := 0; k <= n; k++ {
		a := (start + sweep*float64(k)/float64(n)) * math.Pi / 180
		pts = append(pts, [2]float64{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
	}
	return pts
}

// curvePieces is how many straight pieces follow sweep degrees of a circle
// of radius r within tol of it.
func curvePieces(r, sweep, tol float64) int {
	if r <= tol {
		return 4
	}
	step := 2 * math.Acos(1-tol/r) * 180 / math.Pi
	return max(4, min(720, int(math.Ceil(math.Abs(sweep)/step))))
}

// pathOutlines are a path shape's outlines, its data fractions of its box,
// its curves in straight pieces; mm is millimeters to the template's unit.
func pathOutlines(e *labels.Element, x0, y0, x1, y1, mm float64) []CutPath {
	segs, err := labels.ParsePathData(e.D)
	if err != nil {
		return nil // validated already
	}
	at := func(p [2]float64) [2]float64 { return [2]float64{x0 + p[0]*(x1-x0), y0 + p[1]*(y1-y0)} }
	var out []CutPath
	var cur CutPath
	flush := func() {
		if len(cur.Points) >= 2 {
			out = append(out, cur)
		}
		cur = CutPath{}
	}
	var pen [2]float64
	for _, sg := range segs {
		switch sg.Op {
		case labels.SegMove:
			flush()
			pen = at(sg.Pts[0])
			cur.Points = [][2]float64{pen}
		case labels.SegLine:
			pen = at(sg.Pts[0])
			cur.Points = append(cur.Points, pen)
		case labels.SegQuad, labels.SegCubic:
			c1, end := at(sg.Pts[0]), at(sg.End())
			c2 := c1
			if sg.Op == labels.SegCubic {
				c2 = at(sg.Pts[1])
			} else {
				q := c1
				c1 = [2]float64{pen[0] + 2.0/3*(q[0]-pen[0]), pen[1] + 2.0/3*(q[1]-pen[1])}
				c2 = [2]float64{end[0] + 2.0/3*(q[0]-end[0]), end[1] + 2.0/3*(q[1]-end[1])}
			}
			// The control polygon's length bounds the curve's; pieces of
			// at most a quarter of a millimeter stay close to it.
			length := math.Hypot(c1[0]-pen[0], c1[1]-pen[1]) + math.Hypot(c2[0]-c1[0], c2[1]-c1[1]) + math.Hypot(end[0]-c2[0], end[1]-c2[1])
			n := max(4, min(512, int(math.Ceil(length*mm/0.25))))
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				u := 1 - t
				cur.Points = append(cur.Points, [2]float64{
					u*u*u*pen[0] + 3*u*u*t*c1[0] + 3*u*t*t*c2[0] + t*t*t*end[0],
					u*u*u*pen[1] + 3*u*u*t*c1[1] + 3*u*t*t*c2[1] + t*t*t*end[1],
				})
			}
			pen = end
		case labels.SegClose:
			cur.Closed = true
			if len(cur.Points) > 0 {
				pen = cur.Points[0]
			}
			// A closing point the same as the first is the close itself.
			if n := len(cur.Points); n > 2 && cur.Points[n-1] == cur.Points[0] {
				cur.Points = cur.Points[:n-1]
			}
			flush()
		}
	}
	flush()
	return out
}
