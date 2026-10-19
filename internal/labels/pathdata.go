package labels

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Path segments, as SVG path data's commands reduce to: every command,
// relative or absolute, shorthand or arc, becomes one of these, with
// absolute points.
const (
	SegMove  = 'M'
	SegLine  = 'L'
	SegQuad  = 'Q'
	SegCubic = 'C'
	SegClose = 'Z'
)

// MaxPathSegments bounds one path, and MaxTemplatePathSegments all the
// paths of a template.
const (
	MaxPathSegments         = 2000
	MaxTemplatePathSegments = 20000
)

// PathSeg is one segment: a move or line to Pts[0], a quadratic curve
// through Pts[0] to Pts[1], or a cubic through Pts[0] and Pts[1] to Pts[2].
type PathSeg struct {
	Op  byte
	Pts [3][2]float64
}

// End is where the segment ends; a close has no point of its own.
func (s PathSeg) End() [2]float64 {
	switch s.Op {
	case SegQuad:
		return s.Pts[1]
	case SegCubic:
		return s.Pts[2]
	}
	return s.Pts[0]
}

// Points are the segment's points, its control points among them.
func (s PathSeg) Points() [][2]float64 {
	switch s.Op {
	case SegClose:
		return nil
	case SegQuad:
		return s.Pts[:2]
	case SegCubic:
		return s.Pts[:3]
	}
	return s.Pts[:1]
}

// ParsePathData reads SVG path data (SVG 1.1 §8.3): moves, lines,
// horizontal and vertical lines, cubic and quadratic curves with their
// shorthands, elliptical arcs, which become cubic curves, and closes, each
// absolute or relative. It fails on anything else, or more than
// MaxPathSegments segments.
func ParsePathData(d string) ([]PathSeg, error) {
	p := pathParser{s: d}
	var (
		segs             []PathSeg
		cur, start, ctrl [2]float64
		prev             byte
		cmd              byte
		haveMove         bool
	)
	add := func(s PathSeg) error {
		if len(segs) >= MaxPathSegments {
			return fmt.Errorf("more than %d segments", MaxPathSegments)
		}
		segs = append(segs, s)
		return nil
	}
	for {
		p.skipSpace()
		if p.done() {
			break
		}
		if c := p.s[p.i]; isCommand(c) {
			cmd = c
			p.i++
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' {
			return nil, fmt.Errorf("expected a command at %d", p.i)
		} else {
			// Numbers after a command's own repeat it; after a move, as lines.
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		if !haveMove && up != 'M' {
			return nil, fmt.Errorf("path data must start with a move")
		}
		abs := func(x, y float64) [2]float64 {
			if rel {
				return [2]float64{cur[0] + x, cur[1] + y}
			}
			return [2]float64{x, y}
		}
		var err error
		switch up {
		case 'M':
			var x, y float64
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			cur = abs(x, y)
			start, haveMove = cur, true
			err = add(PathSeg{Op: SegMove, Pts: [3][2]float64{cur}})
		case 'L':
			var x, y float64
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			cur = abs(x, y)
			err = add(PathSeg{Op: SegLine, Pts: [3][2]float64{cur}})
		case 'H', 'V':
			var v float64
			if v, err = p.number(); err != nil {
				return nil, err
			}
			switch {
			case up == 'H' && rel:
				cur[0] += v
			case up == 'H':
				cur[0] = v
			case rel:
				cur[1] += v
			default:
				cur[1] = v
			}
			err = add(PathSeg{Op: SegLine, Pts: [3][2]float64{cur}})
		case 'C', 'S':
			var c1, c2, to [2]float64
			if up == 'C' {
				x1, y1, e1 := p.pair()
				if e1 != nil {
					return nil, e1
				}
				c1 = abs(x1, y1)
			} else {
				// The reflection of the last cubic's second control point.
				c1 = cur
				if prev == 'C' || prev == 'S' {
					c1 = [2]float64{2*cur[0] - ctrl[0], 2*cur[1] - ctrl[1]}
				}
			}
			x2, y2, e2 := p.pair()
			if e2 != nil {
				return nil, e2
			}
			x, y, e3 := p.pair()
			if e3 != nil {
				return nil, e3
			}
			c2, to = abs(x2, y2), abs(x, y)
			ctrl, cur = c2, to
			err = add(PathSeg{Op: SegCubic, Pts: [3][2]float64{c1, c2, to}})
		case 'Q', 'T':
			var c [2]float64
			if up == 'Q' {
				x1, y1, e1 := p.pair()
				if e1 != nil {
					return nil, e1
				}
				c = abs(x1, y1)
			} else {
				c = cur
				if prev == 'Q' || prev == 'T' {
					c = [2]float64{2*cur[0] - ctrl[0], 2*cur[1] - ctrl[1]}
				}
			}
			x, y, e2 := p.pair()
			if e2 != nil {
				return nil, e2
			}
			to := abs(x, y)
			ctrl, cur = c, to
			err = add(PathSeg{Op: SegQuad, Pts: [3][2]float64{c, to}})
		case 'A':
			var rx, ry, angle, large, sweep, x, y float64
			for _, v := range []*float64{&rx, &ry, &angle} {
				if *v, err = p.number(); err != nil {
					return nil, err
				}
			}
			for _, v := range []*float64{&large, &sweep} {
				if *v, err = p.flag(); err != nil {
					return nil, err
				}
			}
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			to := abs(x, y)
			for _, c := range arcToCubics(cur, to, rx, ry, angle, large != 0, sweep != 0) {
				if err = add(c); err != nil {
					return nil, err
				}
			}
			cur = to
		case 'Z':
			cur = start
			err = add(PathSeg{Op: SegClose})
		}
		if err != nil {
			return nil, err
		}
		prev = up
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("path data is empty")
	}
	for _, s := range segs {
		for _, pt := range s.Points() {
			if math.IsNaN(pt[0]) || math.IsInf(pt[0], 0) || math.IsNaN(pt[1]) || math.IsInf(pt[1], 0) {
				return nil, fmt.Errorf("path data has a point that is not a number")
			}
		}
	}
	return segs, nil
}

// FormatPathData writes segments as path data, each number to five
// decimals at most.
func FormatPathData(segs []PathSeg) string {
	var b strings.Builder
	n := func(v float64) {
		s := strconv.FormatFloat(math.Round(v*1e5)/1e5, 'f', -1, 64)
		if s == "-0" {
			s = "0"
		}
		b.WriteString(s)
	}
	for i, s := range segs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(s.Op)
		for k, pt := range s.Points() {
			if k > 0 {
				b.WriteByte(' ')
			}
			n(pt[0])
			b.WriteByte(' ')
			n(pt[1])
		}
	}
	return b.String()
}

func isCommand(c byte) bool {
	return strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0
}

type pathParser struct {
	s string
	i int
}

func (p *pathParser) done() bool { return p.i >= len(p.s) }

func (p *pathParser) skipSpace() {
	for p.i < len(p.s) && strings.IndexByte(" \t\r\n,", p.s[p.i]) >= 0 {
		p.i++
	}
}

// number reads a number: a sign, digits, a point, and an exponent.
func (p *pathParser) number() (float64, error) {
	p.skipSpace()
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	n := digits()
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		n += digits()
	}
	if n == 0 {
		return 0, fmt.Errorf("expected a number at %d", start)
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		save := p.i
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			p.i = save
		}
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}

func (p *pathParser) pair() (float64, float64, error) {
	x, err := p.number()
	if err != nil {
		return 0, 0, err
	}
	y, err := p.number()
	return x, y, err
}

// flag reads an arc's flag, 0 or 1, which may run into what follows.
func (p *pathParser) flag() (float64, error) {
	p.skipSpace()
	if p.i < len(p.s) && (p.s[p.i] == '0' || p.s[p.i] == '1') {
		p.i++
		return float64(p.s[p.i-1] - '0'), nil
	}
	return 0, fmt.Errorf("expected a flag, 0 or 1, at %d", p.i)
}

// arcToCubics turns an elliptical arc (SVG 1.1 §F.6) into cubic curves of
// at most a quarter turn each.
func arcToCubics(from, to [2]float64, rx, ry, angle float64, large, sweep bool) []PathSeg {
	if from == to {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []PathSeg{{Op: SegLine, Pts: [3][2]float64{to}}}
	}
	phi := angle * math.Pi / 180
	sinPhi, cosPhi := math.Sin(phi), math.Cos(phi)
	// The midpoint, in the ellipse's own frame.
	dx, dy := (from[0]-to[0])/2, (from[1]-to[1])/2
	x1 := cosPhi*dx + sinPhi*dy
	y1 := -sinPhi*dx + cosPhi*dy
	// Radii too small to reach are scaled up until they do.
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	k := math.Sqrt(math.Max(0, num/den))
	if large == sweep {
		k = -k
	}
	cxp, cyp := k*rx*y1/ry, -k*ry*x1/rx
	cx := cosPhi*cxp - sinPhi*cyp + (from[0]+to[0])/2
	cy := sinPhi*cxp + cosPhi*cyp + (from[1]+to[1])/2
	angleOf := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	theta := angleOf(1, 0, (x1-cxp)/rx, (y1-cyp)/ry)
	delta := angleOf((x1-cxp)/rx, (y1-cyp)/ry, (-x1-cxp)/rx, (-y1-cyp)/ry)
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	} else if sweep && delta < 0 {
		delta += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(delta) / (math.Pi / 2)))
	step := delta / float64(n)
	kappa := 4.0 / 3 * math.Tan(step/4)
	point := func(t float64) ([2]float64, [2]float64) {
		// A point on the ellipse, and its derivative.
		ct, st := math.Cos(t), math.Sin(t)
		p := [2]float64{cx + rx*ct*cosPhi - ry*st*sinPhi, cy + rx*ct*sinPhi + ry*st*cosPhi}
		d := [2]float64{-rx*st*cosPhi - ry*ct*sinPhi, -rx*st*sinPhi + ry*ct*cosPhi}
		return p, d
	}
	segs := make([]PathSeg, 0, n)
	t := theta
	p0, d0 := point(t)
	for i := 0; i < n; i++ {
		t += step
		p1, d1 := point(t)
		if i == n-1 {
			p1 = to
		}
		segs = append(segs, PathSeg{Op: SegCubic, Pts: [3][2]float64{
			{p0[0] + kappa*d0[0], p0[1] + kappa*d0[1]},
			{p1[0] - kappa*d1[0], p1[1] - kappa*d1[1]},
			p1,
		}})
		p0, d0 = p1, d1
	}
	return segs
}
