package render

import (
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// turn rotates what an element drew in box clockwise by degrees about the
// box's center. A whole number of quarter turns keeps everything on whole
// dots: barcode modules stay rectangles of dots and images are turned dot
// for dot, in a box turned with them whose corner is a whole dot. At other
// angles barcode modules become polygons, filled as text is, and images
// are turned as they are drawn.
func (s *Scene) turn(items []Item, box image.Rectangle, degrees float64) {
	if degrees == 0 || len(items) == 0 {
		return
	}
	if quarters, ok := labels.RightAngle(degrees); ok {
		q := quarterTurn{quarters: quarters, from: box}
		q.to = box
		if quarters%2 == 1 {
			w, h := box.Dx(), box.Dy()
			min := box.Min.Add(image.Pt((w-h)/2, (h-w)/2))
			q.to = image.Rectangle{Min: min, Max: min.Add(image.Pt(h, w))}
		}
		for i := range items {
			q.item(&items[i])
		}
		return
	}

	sin, cos := labels.SinCos(degrees)
	cx, cy := float64(box.Min.X)+float64(box.Dx())/2, float64(box.Min.Y)+float64(box.Dy())/2
	at := func(p Point) Point {
		dx, dy := p.X-cx, p.Y-cy
		return Point{cx + dx*cos - dy*sin, cy + dx*sin + dy*cos}
	}
	for i := range items {
		it := &items[i]
		switch {
		case it.Image != nil:
			it.Image.turn(degrees, cx, cy, at)
		case it.Rects != nil:
			// Each run of modules becomes a turned rectangle.
			p := make(Path, 0, 4*len(it.Rects))
			for _, r := range it.Rects {
				x0, y0, x1, y1 := float64(r.X), float64(r.Y), float64(r.X+r.W), float64(r.Y+r.H)
				p = append(p,
					Segment{Op: MoveTo, Pts: [3]Point{at(Point{x0, y0})}},
					Segment{Op: LineTo, Pts: [3]Point{at(Point{x1, y0})}},
					Segment{Op: LineTo, Pts: [3]Point{at(Point{x1, y1})}},
					Segment{Op: LineTo, Pts: [3]Point{at(Point{x0, y1})}})
			}
			*it = Item{Path: p, Paint: it.Paint}
		default:
			it.Path.transform(at)
		}
	}
}

// quarterTurn turns what was drawn in from by a whole number of quarter
// turns clockwise into to, the same box turned, both on whole dots.
type quarterTurn struct {
	quarters int
	from, to image.Rectangle
}

// point turns a point.
func (q quarterTurn) point(p Point) Point {
	u, v := p.X-float64(q.from.Min.X), p.Y-float64(q.from.Min.Y)
	w, h := float64(q.from.Dx()), float64(q.from.Dy())
	switch q.quarters {
	case 1:
		u, v = h-v, u
	case 2:
		u, v = w-u, h-v
	case 3:
		u, v = v, w-u
	}
	return Point{float64(q.to.Min.X) + u, float64(q.to.Min.Y) + v}
}

// rect turns a rectangle of dots.
func (q quarterTurn) rect(x, y, w, h int) (int, int, int, int) {
	a := q.point(Point{float64(x), float64(y)})
	b := q.point(Point{float64(x + w), float64(y + h)})
	x0, x1 := int(math.Min(a.X, b.X)), int(math.Max(a.X, b.X))
	y0, y1 := int(math.Min(a.Y, b.Y)), int(math.Max(a.Y, b.Y))
	return x0, y0, x1 - x0, y1 - y0
}

func (q quarterTurn) item(it *Item) {
	switch {
	case it.Image != nil:
		pi := *it.Image
		pi.X, pi.Y, pi.W, pi.H = q.rect(pi.X, pi.Y, pi.W, pi.H)
		pi.Image = turnImage(pi.Image, q.quarters)
		// An image already turned keeps its turn, about its center moved.
		if pi.Rotation != 0 {
			c := q.point(Point{pi.Cx, pi.Cy})
			pi.Cx, pi.Cy = c.X, c.Y
		}
		it.Image = &pi
	case it.Rects != nil:
		rects := make([]Rect, len(it.Rects))
		for i, r := range it.Rects {
			rects[i].X, rects[i].Y, rects[i].W, rects[i].H = q.rect(r.X, r.Y, r.W, r.H)
		}
		it.Rects = rects
	default:
		it.Path.transform(q.point)
	}
}

// transform moves every point of the path.
func (p Path) transform(f func(Point) Point) {
	for i := range p {
		n := 1
		switch p[i].Op {
		case QuadTo:
			n = 2
		case CubeTo:
			n = 3
		}
		for k := 0; k < n; k++ {
			p[i].Pts[k] = f(p[i].Pts[k])
		}
	}
}

// turnImage turns an image a whole number of quarter turns clockwise, dot
// for dot.
func turnImage(img image.Image, quarters int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if quarters%2 == 1 {
		w, h = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var tx, ty int
			switch quarters {
			case 1:
				tx, ty = b.Dy()-1-y, x
			case 2:
				tx, ty = b.Dx()-1-x, b.Dy()-1-y
			case 3:
				tx, ty = y, b.Dx()-1-x
			default:
				tx, ty = x, y
			}
			out.Set(tx, ty, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

// turn turns the image by degrees about (cx, cy), at being that turn, on
// top of any turn it has: one turn, by both, about the point that does
// both. Turned back level, it is moved instead, to the nearest dot.
func (pi *PlacedImage) turn(degrees, cx, cy float64, at func(Point) Point) {
	if pi.Rotation == 0 {
		pi.Rotation, pi.Cx, pi.Cy = degrees, cx, cy
		return
	}
	// Where the image's center goes, turned as it is and then as asked.
	sin, cos := labels.SinCos(pi.Rotation)
	bx, by := float64(pi.X)+float64(pi.W)/2, float64(pi.Y)+float64(pi.H)/2
	dx, dy := bx-pi.Cx, by-pi.Cy
	to := at(Point{pi.Cx + dx*cos - dy*sin, pi.Cy + dx*sin + dy*cos})
	total := math.Mod(pi.Rotation+degrees, 360)
	if total == 0 {
		pi.X, pi.Y = round(to.X-float64(pi.W)/2), round(to.Y-float64(pi.H)/2)
		pi.Rotation, pi.Cx, pi.Cy = 0, 0, 0
		return
	}
	// The point p about which turning the center b by total lands it on to:
	// (I - R) p = to - R b.
	sin, cos = labels.SinCos(total)
	tx, ty := to.X-(bx*cos-by*sin), to.Y-(bx*sin+by*cos)
	det := (1-cos)*(1-cos) + sin*sin
	pi.Rotation = total
	pi.Cx = ((1-cos)*tx - sin*ty) / det
	pi.Cy = (sin*tx + (1-cos)*ty) / det
}

// mirror flips what was drawn in box, across (left for right), down (top
// for bottom) or both, about the box's center. Barcode modules stay whole
// dots and images are flipped dot for dot.
func mirror(items []Item, box image.Rectangle, across, down bool) {
	if !across && !down {
		return
	}
	sx, sy := box.Min.X+box.Max.X, box.Min.Y+box.Max.Y
	at := func(p Point) Point {
		if across {
			p.X = float64(sx) - p.X
		}
		if down {
			p.Y = float64(sy) - p.Y
		}
		return p
	}
	for i := range items {
		it := &items[i]
		switch {
		case it.Image != nil:
			pi := *it.Image
			pi.Image = mirrored(pi.Image, across, down)
			if across {
				pi.X = sx - pi.X - pi.W
			}
			if down {
				pi.Y = sy - pi.Y - pi.H
			}
			if pi.Rotation != 0 {
				c := at(Point{pi.Cx, pi.Cy})
				pi.Cx, pi.Cy = c.X, c.Y
				// Mirrored one way, a turn goes the other way.
				if across != down {
					pi.Rotation = 360 - pi.Rotation
				}
			}
			it.Image = &pi
		case it.Rects != nil:
			rects := make([]Rect, len(it.Rects))
			for k, r := range it.Rects {
				if across {
					r.X = sx - r.X - r.W
				}
				if down {
					r.Y = sy - r.Y - r.H
				}
				rects[k] = r
			}
			it.Rects = rects
		default:
			it.Path.transform(at)
		}
	}
}

// print mirrors and turns everything on the label as it prints, as the
// template's PrintMirror and PrintRotation say, about the label's center.
func (s *Scene) print(t *labels.Template) {
	canvas := image.Rect(0, 0, s.Width, s.Height)
	m := t.PrintMirror
	mirror(s.Items, canvas, m == labels.MirrorAcross || m == labels.MirrorBoth, m == labels.MirrorDown || m == labels.MirrorBoth)
	s.turn(s.Items, canvas, t.PrintRotation)
}
