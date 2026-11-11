package vision

import (
	"errors"
	"fmt"
	"image"
	"math"
)

// Calibration marks (docs/printing.md#calibrating): dots engraved on light
// scrap in a grid three across and three down, over a span half as wide
// again as it is tall, with the top left one, the origin, larger. The
// larger dot and the longer side tell which way round the photo is, so
// any camera, turned or mirrored, is fitted the same way.
const (
	markColumns = 3
	markRows    = 3
	// markSpan is how much of the work area's width the marks span, and
	// markAspect how much wider than tall they are.
	markSpan   = 0.6
	markAspect = 1.5
	// originScale is how much wider the origin is than the other dots.
	originScale = 1.6
	// scrapMargin is how far scrap reaches beyond the marks, in mm.
	scrapMargin = 5
	// MinMarks is how many marks must be found to calibrate.
	MinMarks = 6
)

// MarkLayout is where the calibration marks are engraved on a work area.
type MarkLayout struct {
	// Marks are their centers on the bed, in millimeters, row by row from
	// the top left; the first is the origin.
	Marks []Point `json:"marks"`
	// Diameter is the dots', OriginDiameter the origin's, in millimeters.
	Diameter       float64 `json:"diameter_mm"`
	OriginDiameter float64 `json:"origin_diameter_mm"`
	// Scrap is the part of the bed the scrap must cover: X, Y, width and
	// height, in millimeters.
	Scrap [4]float64 `json:"scrap"`
}

// LayoutFor lays the marks out on a work area: centered, spanning most of
// its width, and as tall as the span allows.
func LayoutFor(workW, workH float64) MarkLayout {
	w := markSpan * workW
	h := math.Min(w/markAspect, markSpan*workH)
	d := math.Min(math.Max(w/30, 2), 6)
	l := MarkLayout{Diameter: d, OriginDiameter: d * originScale}
	x0, y0 := (workW-w)/2, (workH-h)/2
	for r := 0; r < markRows; r++ {
		for c := 0; c < markColumns; c++ {
			l.Marks = append(l.Marks, Point{X: x0 + w*float64(c)/float64(markColumns-1), Y: y0 + h*float64(r)/float64(markRows-1)})
		}
	}
	m := l.OriginDiameter/2 + scrapMargin
	sx, sy := math.Max(0, x0-m), math.Max(0, y0-m)
	l.Scrap = [4]float64{sx, sy, math.Min(workW, x0+w+m) - sx, math.Min(workH, y0+h+m) - sy}
	return l
}

// corners are the indices of the layout's four corner marks: the origin,
// then along X, the far corner, and along Y.
func (l MarkLayout) corners() [4]int {
	last := len(l.Marks) - 1
	return [4]int{0, markColumns - 1, last, last - (markColumns - 1)}
}

// ErrMarksNotFound wraps every failure to find the marks.
var ErrMarksNotFound = errors.New("the calibration marks were not found")

// Calibration is a camera's photo fitted to the bed.
type Calibration struct {
	// H maps a pixel of a photo Width × Height to millimeters on the bed.
	H             Homography
	Width, Height int
	// Marks is how many marks were found and fitted, of Expected.
	Marks, Expected int
	// RMS and Max are how far the marks found land, mapped, from where
	// they were engraved: the root mean square and the largest, in mm.
	RMS, Max float64
}

// Calibrate finds a layout's marks in a photo and fits the homography from
// its pixels to the bed. Errors wrap ErrMarksNotFound or ErrDegenerate.
func Calibrate(img image.Image, l MarkLayout) (Calibration, error) {
	px, mm, err := FindMarks(img, l)
	if err != nil {
		return Calibration{}, err
	}
	fit, err := FitHomography(px, mm)
	if err != nil {
		return Calibration{}, err
	}
	b := img.Bounds()
	return Calibration{H: fit.H, Width: b.Dx(), Height: b.Dy(), Marks: len(px), Expected: len(l.Marks), RMS: round(fit.RMS*1000) / 1000, Max: round(fit.Max*1000) / 1000}, nil
}

// candidate is a dark dot that may be a mark.
type candidate struct {
	at   Point // in the gray image's pixels
	area int
}

// FindMarks finds a layout's marks in a photo: each found, in the photo's
// pixels, with where it was engraved on the bed. The marks are dark dots on
// light scrap; the larger dot and the longer side of the grid say which
// corner is which. Errors wrap ErrMarksNotFound.
func FindMarks(img image.Image, l MarkLayout) ([]Point, []Point, error) {
	g := grayOf(img).blurred(1)
	t := g.otsu(nil)
	dark := make([]bool, len(g.pix))
	for i, v := range g.pix {
		dark[i] = v < t
	}
	maxArea := len(g.pix) / 50
	var cands []candidate
	for _, b := range blobs(g.w, g.h, dark, 0) {
		if b.edge || b.area < 6 || b.area > maxArea {
			continue
		}
		bw, bh := b.maxX-b.minX+1, b.maxY-b.minY+1
		fill := float64(b.area) / float64(bw*bh)
		if fill < 0.55 || fill > 0.95 || bw > 2*bh || bh > 2*bw {
			continue
		}
		cx, cy := b.center()
		// A mark is engraved on the scrap: light all around it.
		r := 1.5 * float64(max(bw, bh)) / 2
		ring := true
		for k := 0; k < 8 && ring; k++ {
			a := float64(k) * math.Pi / 4
			x, y := int(math.Round(cx+r*math.Cos(a))), int(math.Round(cy+r*math.Sin(a)))
			ring = x >= 0 && y >= 0 && x < g.w && y < g.h && g.at(x, y) >= t
		}
		if ring {
			cands = append(cands, candidate{at: Point{X: cx, Y: cy}, area: b.area})
		}
	}
	if len(cands) < 4 {
		return nil, nil, fmt.Errorf("%w: found %d dark dots on light scrap, and needs at least 4", ErrMarksNotFound, len(cands))
	}

	// The origin is the largest; the others are smaller by the square of
	// originScale, give or take.
	origin := 0
	for i, c := range cands {
		if c.area > cands[origin].area {
			origin = i
		}
	}
	ratio := originScale * originScale
	kept := []candidate{cands[origin]}
	for i, c := range cands {
		if i == origin {
			continue
		}
		if a := float64(cands[origin].area) / float64(c.area); a > ratio/2 && a < ratio*2.5 {
			kept = append(kept, c)
		}
	}
	if len(kept) < 4 {
		return nil, nil, fmt.Errorf("%w: found %d dots the size of the marks, and needs at least 4", ErrMarksNotFound, len(kept))
	}

	// The grid's corners are the four of its hull enclosing the most.
	pts := make([]Point, len(kept))
	for i, c := range kept {
		pts[i] = c.at
	}
	hull := convexHull(pts)
	if len(hull) < 4 {
		return nil, nil, fmt.Errorf("%w: the dots found lie in a line", ErrMarksNotFound)
	}
	var quad [4]Point
	best := -1.0
	for a := 0; a < len(hull); a++ {
		for b := a + 1; b < len(hull); b++ {
			for c := b + 1; c < len(hull); c++ {
				for d := c + 1; d < len(hull); d++ {
					q := [4]Point{hull[a], hull[b], hull[c], hull[d]}
					if area := quadArea(q); area > best {
						best, quad = area, q
					}
				}
			}
		}
	}
	at := -1
	for i, p := range quad {
		if p == pts[0] {
			at = i
		}
	}
	if at < 0 {
		return nil, nil, fmt.Errorf("%w: the largest dot, the origin, is not at a corner of the others", ErrMarksNotFound)
	}
	o, n1, far, n2 := quad[at], quad[(at+1)%4], quad[(at+2)%4], quad[(at+3)%4]
	alongX, alongY := n1, n2
	if dist(o, n2)+dist(far, n1) > dist(o, n1)+dist(far, n2) {
		alongX, alongY = n2, n1
	}
	corners := l.corners()
	from := []Point{o, alongX, far, alongY}
	to := []Point{l.Marks[corners[0]], l.Marks[corners[1]], l.Marks[corners[2]], l.Marks[corners[3]]}
	rough, err := FitHomography(from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrMarksNotFound, err)
	}
	back, err := rough.H.Inverse()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrMarksNotFound, err)
	}

	// Every mark where the corners put it, matched to the nearest dot.
	predicted := make([]Point, len(l.Marks))
	for i, m := range l.Marks {
		predicted[i] = back.Apply(m)
	}
	spacing := math.Inf(1)
	for i := range predicted {
		if (i+1)%markColumns != 0 {
			spacing = math.Min(spacing, dist(predicted[i], predicted[i+1]))
		}
		if i+markColumns < len(predicted) {
			spacing = math.Min(spacing, dist(predicted[i], predicted[i+markColumns]))
		}
	}
	reach := 0.3 * spacing
	used := make([]bool, len(kept))
	var px, mm []Point
	for i, p := range predicted {
		nearest, d := -1, reach
		for j, c := range kept {
			if !used[j] && dist(c.at, p) < d {
				nearest, d = j, dist(c.at, p)
			}
		}
		if nearest < 0 {
			continue
		}
		used[nearest] = true
		px = append(px, g.toPhoto(kept[nearest].at.X+0.5, kept[nearest].at.Y+0.5))
		mm = append(mm, l.Marks[i])
	}
	if len(px) < MinMarks {
		return nil, nil, fmt.Errorf("%w: found %d of the %d marks, and needs at least %d", ErrMarksNotFound, len(px), len(l.Marks), MinMarks)
	}
	return px, mm, nil
}

func dist(a, b Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

// quadArea is the area of a quadrilateral, its corners in order.
func quadArea(q [4]Point) float64 {
	var s float64
	for i := 0; i < 4; i++ {
		j := (i + 1) % 4
		s += q[i].X*q[j].Y - q[j].X*q[i].Y
	}
	return math.Abs(s) / 2
}
