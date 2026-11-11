// Package vision finds what BAM! needs in a laser camera's photo
// (docs/printing.md#camera-alignment): a fixed set of engraved marks, to
// fit a homography from the photo's pixels to millimeters on the bed, and a
// light rectangular workpiece against a darker bed. It is deliberately
// narrow: no lens model, no features, nothing general, and only the
// standard library.
package vision

import (
	"errors"
	"fmt"
	"math"
)

// Point is a point in a photo, in pixels, or on a bed, in millimeters.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Homography maps points on one plane to another: row by row, a 3 × 3
// matrix in homogeneous coordinates, its last entry 1.
type Homography [9]float64

// Identity is the homography that maps every point to itself.
var Identity = Homography{1, 0, 0, 0, 1, 0, 0, 0, 1}

// Apply maps a point.
func (h Homography) Apply(p Point) Point {
	w := h[6]*p.X + h[7]*p.Y + h[8]
	return Point{X: (h[0]*p.X + h[1]*p.Y + h[2]) / w, Y: (h[3]*p.X + h[4]*p.Y + h[5]) / w}
}

// Mul is h after g: (h.Mul(g)).Apply(p) is h.Apply(g.Apply(p)).
func (h Homography) Mul(g Homography) Homography {
	var out Homography
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			out[r*3+c] = h[r*3]*g[c] + h[r*3+1]*g[3+c] + h[r*3+2]*g[6+c]
		}
	}
	return out.normalized()
}

// Inverse maps back again.
func (h Homography) Inverse() (Homography, error) {
	a, b, c, d, e, f, g, k, m := h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8]
	det := a*(e*m-f*k) - b*(d*m-f*g) + c*(d*k-e*g)
	if math.Abs(det) < 1e-12 || math.IsNaN(det) {
		return Homography{}, ErrDegenerate
	}
	inv := Homography{
		(e*m - f*k) / det, (c*k - b*m) / det, (b*f - c*e) / det,
		(f*g - d*m) / det, (a*m - c*g) / det, (c*d - a*f) / det,
		(d*k - e*g) / det, (b*g - a*k) / det, (a*e - b*d) / det,
	}
	return inv.normalized(), nil
}

// Scaled is h for a photo of another size showing the same view: a pixel
// of the new photo is first scaled to the size h was fitted at.
func (h Homography) Scaled(fromW, fromH, toW, toH int) Homography {
	sx, sy := float64(fromW)/float64(toW), float64(fromH)/float64(toH)
	return h.Mul(Homography{sx, 0, 0, 0, sy, 0, 0, 0, 1})
}

func (h Homography) normalized() Homography {
	if math.Abs(h[8]) > 1e-12 {
		k := h[8]
		for i := range h {
			h[i] /= k
		}
	}
	return h
}

// ErrDegenerate is returned for points from which no homography follows:
// fewer than four, or too many of them in a line.
var ErrDegenerate = errors.New("the points do not fix a homography: fewer than four, or in a line")

// Fit is a homography fitted to the pairs from[i] → to[i].
type Fit struct {
	H Homography
	// RMS and Max are how far, after mapping, each from lands from its to:
	// the root mean square and the largest, in to's units.
	RMS, Max float64
}

// FitHomography fits the homography mapping each from to its to, by the
// direct linear transform with both sets normalized first (Hartley's), in
// the least-squares sense when there are more than four pairs.
func FitHomography(from, to []Point) (Fit, error) {
	n := len(from)
	if n != len(to) {
		return Fit{}, fmt.Errorf("vision: %d points mapped to %d", n, len(to))
	}
	if n < 4 {
		return Fit{}, ErrDegenerate
	}
	tf, nf := normalize(from)
	tt, nt := normalize(to)

	// AᵀA, accumulated two rows of A at a time; its eigenvector with the
	// smallest eigenvalue is the homography, as A's least singular vector.
	var ata [81]float64
	for i := 0; i < n; i++ {
		x, y, u, v := nf[i].X, nf[i].Y, nt[i].X, nt[i].Y
		rows := [2][9]float64{
			{-x, -y, -1, 0, 0, 0, u * x, u * y, u},
			{0, 0, 0, -x, -y, -1, v * x, v * y, v},
		}
		for _, r := range rows {
			for a := 0; a < 9; a++ {
				if r[a] == 0 {
					continue
				}
				for b := 0; b < 9; b++ {
					ata[a*9+b] += r[a] * r[b]
				}
			}
		}
	}
	values, vectors := jacobiEigen(ata)
	least, next, largest := 0, -1, 0
	for i := 1; i < 9; i++ {
		if values[i] < values[least] {
			least = i
		}
		if values[i] > values[largest] {
			largest = i
		}
	}
	for i := 0; i < 9; i++ {
		if i != least && (next < 0 || values[i] < values[next]) {
			next = i
		}
	}
	// A second solution as good as the first: the points do not fix one.
	if values[largest] <= 0 || values[next] < 1e-10*values[largest] {
		return Fit{}, ErrDegenerate
	}
	var hn Homography
	for i := 0; i < 9; i++ {
		hn[i] = vectors[i*9+least]
	}
	ttInv, err := tt.Inverse()
	if err != nil {
		return Fit{}, ErrDegenerate
	}
	h := ttInv.Mul(hn).Mul(tf)
	if math.Abs(h[8]) < 1e-12 {
		return Fit{}, ErrDegenerate
	}
	fit := Fit{H: h}
	var sum float64
	for i := range from {
		p := h.Apply(from[i])
		d := math.Hypot(p.X-to[i].X, p.Y-to[i].Y)
		if math.IsNaN(d) || math.IsInf(d, 0) {
			return Fit{}, ErrDegenerate
		}
		sum += d * d
		fit.Max = math.Max(fit.Max, d)
	}
	fit.RMS = math.Sqrt(sum / float64(n))
	return fit, nil
}

// normalize moves points' centroid to the origin and scales them to a mean
// distance of √2 from it, returning the transform and the points moved.
func normalize(ps []Point) (Homography, []Point) {
	var cx, cy float64
	for _, p := range ps {
		cx += p.X
		cy += p.Y
	}
	cx /= float64(len(ps))
	cy /= float64(len(ps))
	var d float64
	for _, p := range ps {
		d += math.Hypot(p.X-cx, p.Y-cy)
	}
	d /= float64(len(ps))
	s := 1.0
	if d > 0 {
		s = math.Sqrt2 / d
	}
	t := Homography{s, 0, -s * cx, 0, s, -s * cy, 0, 0, 1}
	out := make([]Point, len(ps))
	for i, p := range ps {
		out[i] = t.Apply(p)
	}
	return t, out
}

// jacobiEigen finds a symmetric 9 × 9 matrix's eigenvalues, and its
// eigenvectors as the columns of the second result, by cyclic Jacobi
// rotations.
func jacobiEigen(m [81]float64) ([9]float64, [81]float64) {
	const n = 9
	a := m
	var v [81]float64
	for i := 0; i < n; i++ {
		v[i*n+i] = 1
	}
	for sweep := 0; sweep < 100; sweep++ {
		var off float64
		for p := 0; p < n; p++ {
			for q := p + 1; q < n; q++ {
				off += a[p*n+q] * a[p*n+q]
			}
		}
		if off < 1e-30 {
			break
		}
		for p := 0; p < n; p++ {
			for q := p + 1; q < n; q++ {
				apq := a[p*n+q]
				if math.Abs(apq) < 1e-300 {
					continue
				}
				theta := (a[q*n+q] - a[p*n+p]) / (2 * apq)
				t := math.Copysign(1, theta) / (math.Abs(theta) + math.Sqrt(theta*theta+1))
				c := 1 / math.Sqrt(t*t+1)
				s := t * c
				for k := 0; k < n; k++ {
					akp, akq := a[k*n+p], a[k*n+q]
					a[k*n+p] = c*akp - s*akq
					a[k*n+q] = s*akp + c*akq
				}
				for k := 0; k < n; k++ {
					apk, aqk := a[p*n+k], a[q*n+k]
					a[p*n+k] = c*apk - s*aqk
					a[q*n+k] = s*apk + c*aqk
				}
				for k := 0; k < n; k++ {
					vkp, vkq := v[k*n+p], v[k*n+q]
					v[k*n+p] = c*vkp - s*vkq
					v[k*n+q] = s*vkp + c*vkq
				}
			}
		}
	}
	var values [9]float64
	for i := 0; i < n; i++ {
		values[i] = a[i*n+i]
	}
	return values, v
}
