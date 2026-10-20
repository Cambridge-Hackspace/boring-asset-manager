package labels

import (
	"fmt"
	"math"
	"strconv"
)

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

// Printed is where a point of the design lands as the label prints: mirrored
// as PrintMirror says, then turned PrintRotation degrees clockwise, both
// about the label's center.
func (t *Template) Printed(x, y float64) (float64, float64) {
	if t.PrintMirror == MirrorAcross || t.PrintMirror == MirrorBoth {
		x = t.Width - x
	}
	if t.PrintMirror == MirrorDown || t.PrintMirror == MirrorBoth {
		y = t.Height - y
	}
	if t.PrintRotation == 0 {
		return x, y
	}
	sin, cos := SinCos(t.PrintRotation)
	cx, cy := t.Width/2, t.Height/2
	dx, dy := x-cx, y-cy
	return cx + dx*cos - dy*sin, cy + dx*sin + dy*cos
}

// PrintWarnings say where what prints is not what the design shows: the
// elements turned partly off the label as it prints, which are cut off, and
// mirrored codes, which do not scan from the side the label is read from.
func (t *Template) PrintWarnings() []string {
	var out []string
	for i := range t.Elements {
		e := &t.Elements[i]
		if e.Type == ElementGenerator && e.Mirror != "" {
			out = append(out, fmt.Sprintf("%s is a mirrored code: it does not scan from the side the label is read from.", e.called(i)))
		}
		if t.PrintRotation != 0 && t.turnedOff(e) {
			out = append(out, fmt.Sprintf("Turned %s° as it prints, %s goes partly off the label and is cut off.", strconv.FormatFloat(t.PrintRotation, 'f', -1, 64), e.called(i)))
		}
	}
	return out
}

// called is what the editor calls the element at index i: its name, in
// quotes, or what it is and its number, as its layers list it.
func (e *Element) called(i int) string {
	switch {
	case e.Name != "":
		return "“" + e.Name + "”"
	case e.Type == ElementGenerator:
		return fmt.Sprintf("Barcode %d (%s)", i+1, e.GeneratorType)
	case e.Type == ElementStaticImage:
		return fmt.Sprintf("Image %d", i+1)
	case e.Type == ElementShape:
		return fmt.Sprintf("Shape %d (%s)", i+1, e.Shape)
	case e.ContentBinding == "":
		return fmt.Sprintf("Text %d: empty", i+1)
	}
	return fmt.Sprintf("Text %d: %s", i+1, e.ContentBinding)
}

// turnedOff reports whether a corner of the element's box, turned as it is
// and then as the label prints, lies off the label.
func (t *Template) turnedOff(e *Element) bool {
	const tolerance = 1e-6
	sin, cos := SinCos(e.Rotation)
	cx, cy := e.X+e.Width/2, e.Y+e.Height/2
	for _, c := range [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
		dx, dy := c[0]*e.Width/2, c[1]*e.Height/2
		x, y := t.Printed(cx+dx*cos-dy*sin, cy+dx*sin+dy*cos)
		if x < -tolerance || y < -tolerance || x > t.Width+tolerance || y > t.Height+tolerance {
			return true
		}
	}
	return false
}
