package xtool

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
)

// G-code in xTool's dialect, as far as it is known (docs/printing.md#xtool):
// absolute millimeters, the feed rate in millimeters a minute, and power as
// S from 0 to 1000, with the laser in dynamic mode (M4). Every move with the
// laser off carries S0, and the job ends with it off. Nothing else is sent:
// none of the machine-specific codes xTool Studio adds, whose meanings are
// not published, and never the machine's own safety settings.

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

// Program is a job: a raster engraved, then paths cut, on a machine's bed.
type Program struct {
	// WidthMM and HeightMM are the work area; nothing reaches outside it.
	WidthMM, HeightMM float64
	// MirrorX turns X around, for a machine whose X runs right to left.
	MirrorX bool
	Raster  *Raster
	Engrave Setting
	Paths   []Path
	Cut     Setting
}

// FrameSpeed is how fast framing traces a job's outline, in millimeters a
// second, with the laser off.
const FrameSpeed = 100

// gcodeWriter writes G-code, checking every point is on the bed.
type gcodeWriter struct {
	buf               bytes.Buffer
	width, height     float64
	mirror            bool
	err               error
	feed              float64
	haveFeed, haveCut bool
}

func (w *gcodeWriter) line(s string) {
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
func (w *gcodeWriter) at(x, y float64) string {
	const tolerance = 0.01
	if x < -tolerance || y < -tolerance || x > w.width+tolerance || y > w.height+tolerance || math.IsNaN(x) || math.IsNaN(y) {
		if w.err == nil {
			w.err = fmt.Errorf("xtool: the point %s, %s mm is outside the %s × %s mm work area", num(x), num(y), num(w.width), num(w.height))
		}
	}
	x, y = min(max(x, 0), w.width), min(max(y, 0), w.height)
	if w.mirror {
		x = w.width - x
	}
	return "X" + num(x) + "Y" + num(y)
}

// travel moves with the laser off.
func (w *gcodeWriter) travel(x, y float64) {
	w.line("G0" + w.at(x, y) + "S0")
}

// setFeed sets the feed rate for burning moves, from millimeters a second.
func (w *gcodeWriter) setFeed(speed float64) {
	if !w.haveFeed || w.feed != speed {
		w.line("G1F" + num(speed*60) + "S0")
		w.feed, w.haveFeed = speed, true
	}
}

// power is a percentage as S.
func power(percent float64) string {
	return strconv.Itoa(int(math.Round(min(max(percent, 0), 100) * 10)))
}

func (w *gcodeWriter) start() {
	w.line("G90")
	w.line("M4S0")
}

func (w *gcodeWriter) end() ([]byte, error) {
	w.line("G0S0")
	if w.err != nil {
		return nil, w.err
	}
	return w.buf.Bytes(), nil
}

// GCode writes a program: the raster engraved line by line, back and forth,
// each pass in turn, and then each path cut, each pass in turn.
func GCode(p Program) ([]byte, error) {
	if !(p.WidthMM > 0 && p.HeightMM > 0) {
		return nil, fmt.Errorf("xtool: no work area")
	}
	w := &gcodeWriter{width: p.WidthMM, height: p.HeightMM, mirror: p.MirrorX}
	w.start()
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
		return fmt.Errorf("xtool: the %s setting %+v is not one to burn with", what, s)
	}
	return nil
}

// raster engraves an image once: each row with something black on it, from
// left to right and then right to left, burning each run of black dots.
func (w *gcodeWriter) raster(r *Raster, s Setting) {
	b := r.Dots.Bounds()
	cols, rows := b.Dx(), b.Dy()
	if cols == 0 || rows == 0 {
		return
	}
	dx, dy := r.Width/float64(cols), r.Height/float64(rows)
	on := power(s.Power)
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
func (w *gcodeWriter) path(p Path, s Setting) {
	if len(p.Points) < 2 {
		return
	}
	on := power(s.Power)
	w.travel(p.Points[0].X, p.Points[0].Y)
	w.setFeed(s.Speed)
	for _, pt := range p.Points[1:] {
		w.line("G1" + w.at(pt.X, pt.Y) + "S" + on)
	}
	if p.Closed {
		w.line("G1" + w.at(p.Points[0].X, p.Points[0].Y) + "S" + on)
	}
}

// FrameGCode traces a rectangle on the bed with the laser off, for the
// person at the machine to see where a job goes by its pointer.
func FrameGCode(widthMM, heightMM float64, mirrorX bool, x, y, width, height float64) ([]byte, error) {
	w := &gcodeWriter{width: widthMM, height: heightMM, mirror: mirrorX}
	w.start()
	w.travel(x, y)
	w.setFeed(FrameSpeed)
	for _, pt := range []Point{{x + width, y}, {x + width, y + height}, {x, y + height}, {x, y}} {
		w.line("G1" + w.at(pt.X, pt.Y) + "S0")
	}
	return w.end()
}
