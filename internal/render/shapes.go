package render

import (
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// kappa places the control points of a cubic Bézier quarter circle.
const kappa = 0.5522847498

// contour is a closed outline: where it starts, and its segments from there,
// lines and cubic curves only.
type contour struct {
	start Point
	segs  []Segment
}

func (c *contour) lineTo(p Point) { c.segs = append(c.segs, Segment{Op: LineTo, Pts: [3]Point{p}}) }

func (c *contour) cubeTo(c1, c2, p Point) {
	c.segs = append(c.segs, Segment{Op: CubeTo, Pts: [3]Point{c1, c2, p}})
}

// end is where the contour's last segment ends.
func (c contour) end() Point {
	if len(c.segs) == 0 {
		return c.start
	}
	last := c.segs[len(c.segs)-1]
	if last.Op == CubeTo {
		return last.Pts[2]
	}
	return last.Pts[0]
}

// reversed is the same outline run the other way, so that, inside another,
// it cuts a hole under the nonzero rule.
func (c contour) reversed() contour {
	out := contour{start: c.end()}
	for i := len(c.segs) - 1; i >= 0; i-- {
		from := c.start
		if i > 0 {
			from = contour{start: c.start, segs: c.segs[:i]}.end()
		}
		s := c.segs[i]
		if s.Op == CubeTo {
			out.cubeTo(s.Pts[1], s.Pts[0], from)
		} else {
			out.lineTo(from)
		}
	}
	return out
}

// appendTo adds the contour to a path.
func (c contour) appendTo(p Path) Path {
	p = append(p, Segment{Op: MoveTo, Pts: [3]Point{c.start}})
	return append(p, c.segs...)
}

// polygonContour runs through points in order, turned clockwise on the
// page when clockwise is set.
func polygonContour(pts []Point, clockwise bool) contour {
	if clockwise && signedArea(pts) < 0 {
		rev := make([]Point, len(pts))
		for i, p := range pts {
			rev[len(pts)-1-i] = p
		}
		pts = rev
	}
	c := contour{start: pts[0]}
	for _, p := range pts[1:] {
		c.lineTo(p)
	}
	return c
}

// signedArea is positive for points running clockwise on the page, where y
// runs down.
func signedArea(pts []Point) float64 {
	a := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		a += p.X*q.Y - q.X*p.Y
	}
	return a / 2
}

// ellipseContour runs clockwise round an ellipse from its rightmost point.
func ellipseContour(cx, cy, rx, ry float64) contour {
	kx, ky := rx*kappa, ry*kappa
	c := contour{start: Point{cx + rx, cy}}
	c.cubeTo(Point{cx + rx, cy + ky}, Point{cx + kx, cy + ry}, Point{cx, cy + ry})
	c.cubeTo(Point{cx - kx, cy + ry}, Point{cx - rx, cy + ky}, Point{cx - rx, cy})
	c.cubeTo(Point{cx - rx, cy - ky}, Point{cx - kx, cy - ry}, Point{cx, cy - ry})
	c.cubeTo(Point{cx + kx, cy - ry}, Point{cx + rx, cy - ky}, Point{cx + rx, cy})
	return c
}

// roundedRectContour runs clockwise round a rectangle whose corners are
// quarter circles of radius r.
func roundedRectContour(x0, y0, x1, y1, r float64) contour {
	r = math.Max(0, math.Min(r, math.Min(x1-x0, y1-y0)/2))
	k := r * kappa
	c := contour{start: Point{x0 + r, y0}}
	c.lineTo(Point{x1 - r, y0})
	if r > 0 {
		c.cubeTo(Point{x1 - r + k, y0}, Point{x1, y0 + r - k}, Point{x1, y0 + r})
	}
	c.lineTo(Point{x1, y1 - r})
	if r > 0 {
		c.cubeTo(Point{x1, y1 - r + k}, Point{x1 - r + k, y1}, Point{x1 - r, y1})
	}
	c.lineTo(Point{x0 + r, y1})
	if r > 0 {
		c.cubeTo(Point{x0 + r - k, y1}, Point{x0, y1 - r + k}, Point{x0, y1 - r})
	}
	c.lineTo(Point{x0, y0 + r})
	if r > 0 {
		c.cubeTo(Point{x0, y0 + r - k}, Point{x0 + r - k, y0}, Point{x0 + r, y0})
	}
	return c
}

// bar is the outline of a segment drawn width wide, square at its ends,
// clockwise.
func bar(a, b Point, width float64) contour {
	dx, dy := b.X-a.X, b.Y-a.Y
	n := math.Hypot(dx, dy)
	if n == 0 {
		return polygonContour([]Point{a, a, a}, true)
	}
	ox, oy := -dy/n*width/2, dx/n*width/2
	return polygonContour([]Point{{a.X + ox, a.Y + oy}, {b.X + ox, b.Y + oy}, {b.X - ox, b.Y - oy}, {a.X - ox, a.Y - oy}}, true)
}

// paintOf is the paint a shape names.
func paintOf(name string) Paint {
	if name == labels.PaintWhite {
		return White
	}
	return Black
}

// placeShape draws a shape: its fill, then its stroke over it, each in its
// paint. The stroke of a rectangle or ellipse lies inside its box; a line's
// or polygon's is centered on it. A stroke is at least one dot wide. A
// rectangle with square corners, and a level or upright line, are drawn on
// whole dots, as code modules are; everything else is outlines, filled as
// text is.
func (s *Scene) placeShape(e *labels.Element, box image.Rectangle) {
	dpu := s.dotsPerUnit()
	x0, y0 := e.X*dpu, e.Y*dpu
	x1, y1 := x0+e.Width*dpu, y0+e.Height*dpu
	stroke := e.Stroke != labels.PaintNone
	width := 0.0
	if stroke {
		width = math.Max(1, e.StrokeWidth*dpu)
	}
	fill := e.Fill != labels.PaintNone
	add := func(p Path, paint Paint) {
		if len(p) > 0 {
			s.Items = append(s.Items, Item{Path: p, Paint: paint})
		}
	}

	switch e.Shape {
	case labels.ShapeRectangle:
		if e.CornerRadius == 0 {
			s.squareRectangle(box, fill, stroke, max(1, round(width)), paintOf(e.Fill), paintOf(e.Stroke))
			return
		}
		r := e.CornerRadius * dpu
		if fill {
			add(roundedRectContour(x0, y0, x1, y1, r).appendTo(nil), paintOf(e.Fill))
		}
		if stroke {
			p := roundedRectContour(x0, y0, x1, y1, r).appendTo(nil)
			if x1-x0 > 2*width && y1-y0 > 2*width {
				p = roundedRectContour(x0+width, y0+width, x1-width, y1-width, math.Max(0, r-width)).reversed().appendTo(p)
			}
			add(p, paintOf(e.Stroke))
		}

	case labels.ShapeEllipse:
		cx, cy, rx, ry := (x0+x1)/2, (y0+y1)/2, (x1-x0)/2, (y1-y0)/2
		if fill {
			add(ellipseContour(cx, cy, rx, ry).appendTo(nil), paintOf(e.Fill))
		}
		if stroke {
			p := ellipseContour(cx, cy, rx, ry).appendTo(nil)
			if rx > width && ry > width {
				p = ellipseContour(cx, cy, rx-width, ry-width).reversed().appendTo(p)
			}
			add(p, paintOf(e.Stroke))
		}

	case labels.ShapeLine:
		if !stroke {
			return
		}
		a, b := Point{x0, y0}, Point{x1, y1}
		if e.Flip {
			a, b = Point{x0, y1}, Point{x1, y0}
		}
		w := max(1, round(width))
		switch {
		case e.Height == 0:
			s.Items = append(s.Items, Item{Rects: []Rect{{X: round(x0), Y: round(y0 - float64(w)/2), W: max(1, round(x1)-round(x0)), H: w}}, Paint: paintOf(e.Stroke)})
		case e.Width == 0:
			s.Items = append(s.Items, Item{Rects: []Rect{{X: round(x0 - float64(w)/2), Y: round(y0), W: w, H: max(1, round(y1)-round(y0))}}, Paint: paintOf(e.Stroke)})
		default:
			add(bar(a, b, width).appendTo(nil), paintOf(e.Stroke))
		}

	case labels.ShapePath:
		s.placePath(e, x0, y0, x1, y1, width)

	case labels.ShapePolygon:
		pts := make([]Point, len(e.Points))
		for i, p := range e.Points {
			pts[i] = Point{x0 + p[0]*(x1-x0), y0 + p[1]*(y1-y0)}
		}
		closed := e.Closed == nil || *e.Closed
		if fill && closed {
			add(polygonContour(pts, false).appendTo(nil), paintOf(e.Fill))
		}
		if stroke {
			// Each side a bar, with a disc at each corner for a round join,
			// all clockwise, so the nonzero rule fills them as one.
			var p Path
			n := len(pts)
			if !closed {
				n--
			}
			for i := 0; i < n; i++ {
				p = bar(pts[i], pts[(i+1)%len(pts)], width).appendTo(p)
			}
			for i, q := range pts {
				if closed || (i > 0 && i < len(pts)-1) {
					p = ellipseContour(q.X, q.Y, width/2, width/2).appendTo(p)
				}
			}
			add(p, paintOf(e.Stroke))
		}
	}
}

// placePath draws a path shape, its data fractions of the box from (x0, y0)
// to (x1, y1): its fill by its fill rule, then its stroke, centered on it.
func (s *Scene) placePath(e *labels.Element, x0, y0, x1, y1, width float64) {
	segs, err := labels.ParsePathData(e.D)
	if err != nil {
		return // validated already
	}
	at := func(p [2]float64) Point { return Point{x0 + p[0]*(x1-x0), y0 + p[1]*(y1-y0)} }
	if e.Fill != labels.PaintNone {
		var p Path
		for _, sg := range segs {
			switch sg.Op {
			case labels.SegMove:
				p = append(p, Segment{Op: MoveTo, Pts: [3]Point{at(sg.Pts[0])}})
			case labels.SegLine:
				p = append(p, Segment{Op: LineTo, Pts: [3]Point{at(sg.Pts[0])}})
			case labels.SegQuad:
				p = append(p, Segment{Op: QuadTo, Pts: [3]Point{at(sg.Pts[0]), at(sg.Pts[1])}})
			case labels.SegCubic:
				p = append(p, Segment{Op: CubeTo, Pts: [3]Point{at(sg.Pts[0]), at(sg.Pts[1]), at(sg.Pts[2])}})
			}
		}
		if len(p) > 0 {
			s.Items = append(s.Items, Item{Path: p, Paint: paintOf(e.Fill), EvenOdd: e.FillRule == labels.FillEvenOdd})
		}
	}
	if e.Stroke == labels.PaintNone {
		return
	}
	// Each outline as a polyline, its curves in short straight pieces, each
	// piece a bar, with a disc wherever two pieces meet at an angle.
	var stroke Path
	var line []Point
	var joints []bool
	closed := false
	flush := func() {
		if len(line) < 2 {
			line, joints, closed = nil, nil, false
			return
		}
		n := len(line) - 1
		for i := 0; i < n; i++ {
			stroke = bar(line[i], line[i+1], width).appendTo(stroke)
		}
		if closed && line[0] != line[n] {
			stroke = bar(line[n], line[0], width).appendTo(stroke)
		}
		for i, q := range line {
			if joints[i] && (closed || (i > 0 && i < n)) {
				stroke = ellipseContour(q.X, q.Y, width/2, width/2).appendTo(stroke)
			}
		}
		line, joints, closed = nil, nil, false
	}
	var cur Point
	pieces := func(a, b Point) int { return max(2, min(32, int(math.Hypot(b.X-a.X, b.Y-a.Y)/4)+1)) }
	for _, sg := range segs {
		switch sg.Op {
		case labels.SegMove:
			flush()
			cur = at(sg.Pts[0])
			line, joints = []Point{cur}, []bool{true}
		case labels.SegLine:
			cur = at(sg.Pts[0])
			line, joints = append(line, cur), append(joints, true)
		case labels.SegQuad, labels.SegCubic:
			c1, end := at(sg.Pts[0]), at(sg.End())
			c2 := c1
			if sg.Op == labels.SegCubic {
				c2 = at(sg.Pts[1])
			} else {
				// A quadratic as the cubic it is.
				c1 = Point{cur.X + 2.0/3*(c1.X-cur.X), cur.Y + 2.0/3*(c1.Y-cur.Y)}
				c2 = Point{end.X + 2.0/3*(at(sg.Pts[0]).X-end.X), end.Y + 2.0/3*(at(sg.Pts[0]).Y-end.Y)}
			}
			n := pieces(cur, end) + pieces(c1, c2)
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				u := 1 - t
				line = append(line, Point{
					u*u*u*cur.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*end.X,
					u*u*u*cur.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*end.Y,
				})
				// Within a curve, pieces meet smoothly; discs only where it ends.
				joints = append(joints, k == n)
			}
			cur = end
		case labels.SegClose:
			closed = true
			if len(line) > 0 {
				cur = line[0]
			}
			flush()
		}
	}
	flush()
	if len(stroke) > 0 {
		s.Items = append(s.Items, Item{Path: stroke, Paint: paintOf(e.Stroke)})
	}
}

// squareRectangle draws a rectangle with square corners on whole dots: its
// fill, then its stroke, w dots wide, inside the box.
func (s *Scene) squareRectangle(box image.Rectangle, fill, stroke bool, w int, fillPaint, strokePaint Paint) {
	x, y, bw, bh := box.Min.X, box.Min.Y, box.Dx(), box.Dy()
	if fill {
		s.Items = append(s.Items, Item{Rects: []Rect{{X: x, Y: y, W: bw, H: bh}}, Paint: fillPaint})
	}
	if !stroke {
		return
	}
	if 2*w >= bw || 2*w >= bh {
		s.Items = append(s.Items, Item{Rects: []Rect{{X: x, Y: y, W: bw, H: bh}}, Paint: strokePaint})
		return
	}
	s.Items = append(s.Items, Item{Rects: []Rect{
		{X: x, Y: y, W: bw, H: w},
		{X: x, Y: y + w, W: w, H: bh - 2*w},
		{X: x + bw - w, Y: y + w, W: w, H: bh - 2*w},
		{X: x, Y: y + bh - w, W: bw, H: w},
	}, Paint: strokePaint})
}
