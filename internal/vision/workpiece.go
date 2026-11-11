package vision

import (
	"errors"
	"fmt"
	"image"
	"math"
)

// Workpiece is a rectangular workpiece as measured on the bed, in
// millimeters: its center, its size, and how far it is turned clockwise
// (as the bed is drawn, Y down), in degrees from -45 to 45, its width
// along the turned X.
type Workpiece struct {
	Center   Point   `json:"center"`
	Width    float64 `json:"width_mm"`
	Height   float64 `json:"height_mm"`
	AngleDeg float64 `json:"angle_deg"`
}

// ErrNoWorkpiece wraps every failure to find a workpiece.
var ErrNoWorkpiece = errors.New("no workpiece was found")

// minWorkpieceShare is the least of the work area's photo a workpiece
// fills to be taken for one.
const minWorkpieceShare = 0.002

// FindWorkpiece finds the light workpiece on a darker bed in a photo, and
// measures it on the bed: h maps the photo's pixels to millimeters, and
// only what lies within the work area, workW × workH mm, is looked at. The
// largest light patch is taken for the workpiece, and the smallest
// rectangle around it for its outline. Errors wrap ErrNoWorkpiece.
func FindWorkpiece(img image.Image, h Homography, workW, workH float64) (Workpiece, error) {
	g := grayOf(img).blurred(2)
	// Each gray pixel's center, as the photo's pixels, onto the bed.
	inside := make([]bool, len(g.pix))
	n := 0
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			p := h.Apply(g.toPhoto(float64(x)+0.5, float64(y)+0.5))
			if p.X >= 0 && p.Y >= 0 && p.X <= workW && p.Y <= workH {
				inside[y*g.w+x] = true
				n++
			}
		}
	}
	if n == 0 {
		return Workpiece{}, fmt.Errorf("%w: the photo does not show the work area", ErrNoWorkpiece)
	}
	t := g.otsu(inside)
	light := make([]bool, len(g.pix))
	for i, v := range g.pix {
		light[i] = inside[i] && v >= t
	}
	var piece *blob
	for _, b := range blobs(g.w, g.h, light, int(minWorkpieceShare*float64(n))) {
		if b.pixels != nil && (piece == nil || b.area > piece.area) {
			piece = b
		}
	}
	if piece == nil {
		return Workpiece{}, fmt.Errorf("%w: nothing light enough stands out from the bed", ErrNoWorkpiece)
	}
	if float64(piece.area) > 0.9*float64(n) {
		return Workpiece{}, fmt.Errorf("%w: the whole bed looks alike; is the light on, and the bed darker than the piece?", ErrNoWorkpiece)
	}

	// Its edge: the outer corners of each pixel on it, on the bed.
	in := make([]bool, len(g.pix))
	for _, i := range piece.pixels {
		in[i] = true
	}
	var edge []Point
	for _, i := range piece.pixels {
		x, y := i%g.w, i/g.w
		if x > 0 && x < g.w-1 && y > 0 && y < g.h-1 && in[i-1] && in[i+1] && in[i-g.w] && in[i+g.w] {
			continue
		}
		for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
			edge = append(edge, h.Apply(g.toPhoto(float64(x)+c[0], float64(y)+c[1])))
		}
	}
	return minAreaRect(convexHull(edge))
}

// minAreaRect is the smallest rectangle around a convex polygon, which has
// a side along one of the polygon's (rotating calipers).
func minAreaRect(hull []Point) (Workpiece, error) {
	if len(hull) < 3 {
		return Workpiece{}, fmt.Errorf("%w: too small to measure", ErrNoWorkpiece)
	}
	best := math.Inf(1)
	var out Workpiece
	for i := range hull {
		a, b := hull[i], hull[(i+1)%len(hull)]
		l := dist(a, b)
		if l == 0 {
			continue
		}
		ux, uy := (b.X-a.X)/l, (b.Y-a.Y)/l
		minU, maxU, minV, maxV := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for _, p := range hull {
			u := (p.X-a.X)*ux + (p.Y-a.Y)*uy
			v := -(p.X-a.X)*uy + (p.Y-a.Y)*ux
			minU, maxU = math.Min(minU, u), math.Max(maxU, u)
			minV, maxV = math.Min(minV, v), math.Max(maxV, v)
		}
		if area := (maxU - minU) * (maxV - minV); area < best {
			best = area
			cu, cv := (minU+maxU)/2, (minV+maxV)/2
			out = Workpiece{
				Center: Point{X: a.X + cu*ux - cv*uy, Y: a.Y + cu*uy + cv*ux},
				Width:  maxU - minU, Height: maxV - minV,
				AngleDeg: math.Atan2(uy, ux) * 180 / math.Pi,
			}
		}
	}
	// The same rectangle, turned no more than 45° either way.
	for out.AngleDeg > 45 {
		out.AngleDeg -= 90
		out.Width, out.Height = out.Height, out.Width
	}
	for out.AngleDeg <= -45 {
		out.AngleDeg += 90
		out.Width, out.Height = out.Height, out.Width
	}
	out.Center = Point{X: round(out.Center.X), Y: round(out.Center.Y)}
	out.Width, out.Height, out.AngleDeg = round(out.Width), round(out.Height), round(out.AngleDeg)
	return out, nil
}
