package labels

import "math"

// SinCos is the sine and cosine of an angle in degrees, exact at multiples
// of 90°.
func SinCos(degrees float64) (sin, cos float64) {
	switch math.Mod(degrees, 360) {
	case 0:
		return 0, 1
	case 90:
		return 1, 0
	case 180:
		return 0, -1
	case 270:
		return -1, 0
	}
	return math.Sincos(degrees * math.Pi / 180)
}

// RightAngle reports whether a rotation is a whole number of quarter
// turns, and how many.
func RightAngle(degrees float64) (int, bool) {
	q := degrees / 90
	if q != math.Trunc(q) {
		return 0, false
	}
	return int(math.Mod(q, 4)), true
}

// Bounds is the box an element takes up once turned, in the template's
// units: its own box when it is not.
func (e *Element) Bounds() (x, y, width, height float64) {
	if e.Rotation == 0 {
		return e.X, e.Y, e.Width, e.Height
	}
	sin, cos := SinCos(e.Rotation)
	width = math.Abs(e.Width*cos) + math.Abs(e.Height*sin)
	height = math.Abs(e.Width*sin) + math.Abs(e.Height*cos)
	return e.X + (e.Width-width)/2, e.Y + (e.Height-height)/2, width, height
}
