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
			it.Image.Rotation, it.Image.Cx, it.Image.Cy = degrees, cx, cy
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
			*it = Item{Path: p}
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
