// Package grbl drives laser controllers running GRBL 1.1 or firmware that
// speaks as it does, such as grblHAL and FluidNC (docs/printing.md#grbl):
// it writes jobs as G-code for laser mode, streams them with GRBL's
// character-counting flow control, and reads the controller's status from
// its ? reports.
package grbl

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
)

// G-code for GRBL's laser mode ($32=1): millimeters (G21), absolute
// coordinates (G90), the feed rate in millimeters a minute, and power as S
// from 0 to the controller's $30, with the laser in dynamic mode (M4), which
// scales it with the head's speed and keeps it off while the head stands
// still. Every move with the laser off carries S0, and the job ends with
// M5. Nothing else is sent: no homing, no $ setting, no move back to the
// origin.

// Setting is how the laser burns: power as a percentage, speed in
// millimeters a second, and how many passes.
type Setting struct {
	Power  float64
	Speed  float64
	Passes int
}

// Raster is a 1-bit image to engrave, black burned, covering a rectangle
// of the bed in millimeters.
type Raster struct {
	X, Y, Width, Height float64
	Dots                *image.Gray
}

// Point is a point on the bed, in millimeters.
type Point struct{ X, Y float64 }

// Path is a path to cut, closed back to its first point when Closed.
type Path struct {
	Points []Point
	Closed bool
}

// Bed is a machine's work area, and where its origin is: points are given
// from the work area's top left corner, as the person faces the machine,
// and FlipX and FlipY turn them around for a machine whose X runs right to
// left or whose Y runs from the front up.
type Bed struct {
	WidthMM, HeightMM float64
	FlipX, FlipY      bool
	// SMax is the S value of full power: the controller's $30.
	SMax int
}

// Program is a job: a raster engraved, then paths cut, on a machine's bed.
type Program struct {
	Bed
	Raster  *Raster
	Engrave Setting
	Paths   []Path
	Cut     Setting
}

// writer writes G-code, checking every point is on the bed.
type writer struct {
	buf      bytes.Buffer
	bed      Bed
	err      error
	feed     float64
	haveFeed bool
}

func (w *writer) line(s string) {
	w.buf.WriteString(s)
	w.buf.WriteByte('\n')
}

// num is a number as G-code takes it: at most three decimals, no trailing
// zeros.
func num(v float64) string {
	s := strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// at is a point as the machine takes it, or an error when it is off the bed.
func (w *writer) at(x, y float64) string {
	const tolerance = 0.01
	if x < -tolerance || y < -tolerance || x > w.bed.WidthMM+tolerance || y > w.bed.HeightMM+tolerance || math.IsNaN(x) || math.IsNaN(y) {
		if w.err == nil {
			w.err = fmt.Errorf("grbl: the point %s, %s mm is outside the %s × %s mm work area", num(x), num(y), num(w.bed.WidthMM), num(w.bed.HeightMM))
		}
	}
	x, y = min(max(x, 0), w.bed.WidthMM), min(max(y, 0), w.bed.HeightMM)
	if w.bed.FlipX {
		x = w.bed.WidthMM - x
	}
	if w.bed.FlipY {
		y = w.bed.HeightMM - y
	}
	return "X" + num(x) + "Y" + num(y)
}

// travel moves with the laser off.
func (w *writer) travel(x, y float64) {
	w.line("G0" + w.at(x, y) + "S0")
}

// setFeed sets the feed rate for burning moves, from millimeters a second.
func (w *writer) setFeed(speed float64) {
	if !w.haveFeed || w.feed != speed {
		w.line("G1F" + num(speed*60) + "S0")
		w.feed, w.haveFeed = speed, true
	}
}

// power is a percentage as S.
func (w *writer) power(percent float64) string {
	return strconv.Itoa(int(math.Round(min(max(percent, 0), 100) / 100 * float64(w.bed.SMax))))
}

func newWriter(bed Bed) (*writer, error) {
	if !(bed.WidthMM > 0 && bed.HeightMM > 0) {
		return nil, fmt.Errorf("grbl: no work area")
	}
	if bed.SMax < 1 {
		return nil, fmt.Errorf("grbl: no S value for full power")
	}
	w := &writer{bed: bed}
	w.line("G21")
	w.line("G90")
	w.line("M4S0")
	return w, nil
}

func (w *writer) end() ([]byte, error) {
	w.line("M5S0")
	if w.err != nil {
		return nil, w.err
	}
	return w.buf.Bytes(), nil
}

// GCode writes a program: the raster engraved line by line, back and forth,
// each pass in turn, and then each path cut, each pass in turn.
func GCode(p Program) ([]byte, error) {
	w, err := newWriter(p.Bed)
	if err != nil {
		return nil, err
	}
	if r := p.Raster; r != nil && r.Dots != nil {
		if err := checkSetting("engraving", p.Engrave); err != nil {
			return nil, err
		}
		for range p.Engrave.Passes {
			w.raster(r, p.Engrave)
		}
	}
	if len(p.Paths) > 0 {
		if err := checkSetting("cutting", p.Cut); err != nil {
			return nil, err
		}
		for range p.Cut.Passes {
			for _, path := range p.Paths {
				w.path(path, p.Cut)
			}
		}
	}
	return w.end()
}

func checkSetting(what string, s Setting) error {
	if !(s.Power > 0 && s.Power <= 100) || !(s.Speed > 0) || s.Passes < 1 {
		return fmt.Errorf("grbl: the %s setting %+v is not one to burn with", what, s)
	}
	return nil
}

// raster engraves an image once: each row with something black on it, from
// left to right and then right to left, burning each run of black dots.
func (w *writer) raster(r *Raster, s Setting) {
	b := r.Dots.Bounds()
	cols, rows := b.Dx(), b.Dy()
	if cols == 0 || rows == 0 {
		return
	}
	dx, dy := r.Width/float64(cols), r.Height/float64(rows)
	on := w.power(s.Power)
	reverse := false
	black := func(x, y int) bool { return r.Dots.Pix[y*r.Dots.Stride+x] < 0x80 }
	for y := range rows {
		// The runs of black dots on this row, as [first, past last).
		var runs [][2]int
		for x := 0; x < cols; {
			if !black(x, y) {
				x++
				continue
			}
			start := x
			for x < cols && black(x, y) {
				x++
			}
			runs = append(runs, [2]int{start, x})
		}
		if len(runs) == 0 {
			continue
		}
		rowY := r.Y + (float64(y)+0.5)*dy
		edge := func(col int) float64 { return r.X + float64(col)*dx }
		if !reverse {
			w.travel(edge(runs[0][0]), rowY)
			w.setFeed(s.Speed)
			for i, run := range runs {
				if i > 0 {
					w.line("G1" + w.at(edge(run[0]), rowY) + "S0")
				}
				w.line("G1" + w.at(edge(run[1]), rowY) + "S" + on)
			}
		} else {
			last := len(runs) - 1
			w.travel(edge(runs[last][1]), rowY)
			w.setFeed(s.Speed)
			for i := last; i >= 0; i-- {
				if i < last {
					w.line("G1" + w.at(edge(runs[i][1]), rowY) + "S0")
				}
				w.line("G1" + w.at(edge(runs[i][0]), rowY) + "S" + on)
			}
		}
		reverse = !reverse
	}
}

// path cuts a path once.
func (w *writer) path(p Path, s Setting) {
	if len(p.Points) < 2 {
		return
	}
	on := w.power(s.Power)
	w.travel(p.Points[0].X, p.Points[0].Y)
	w.setFeed(s.Speed)
	for _, pt := range p.Points[1:] {
		w.line("G1" + w.at(pt.X, pt.Y) + "S" + on)
	}
	if p.Closed {
		w.line("G1" + w.at(p.Points[0].X, p.Points[0].Y) + "S" + on)
	}
}

// Frame traces a rectangle on the bed for the person at the machine to see
// where a job goes: at a low power, in percent, which shows a dot on the
// workpiece without marking it, or with the laser off at 0, when only the
// head shows it. The speed is in millimeters a second.
func Frame(bed Bed, x, y, width, height, power, speed float64) ([]byte, error) {
	w, err := newWriter(bed)
	if err != nil {
		return nil, err
	}
	if power < 0 || power > 100 || !(speed > 0) {
		return nil, fmt.Errorf("grbl: framing at %s%% and %s mm/s", num(power), num(speed))
	}
	on := w.power(power)
	w.travel(x, y)
	w.setFeed(speed)
	for _, pt := range []Point{{x + width, y}, {x + width, y + height}, {x, y + height}, {x, y}} {
		w.line("G1" + w.at(pt.X, pt.Y) + "S" + on)
	}
	return w.end()
}
