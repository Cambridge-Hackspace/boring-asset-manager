// Package raster draws isolex icons (the IR in package ir) as images, for
// where an SVG will not do: a PNG in an export, a notification or a chat.
// It paints as an SVG viewer paints package svg's drawing of the same icon
// (isolex is Axonibyte Innovations, LLC's, see ../NOTICE): each shape's
// fill, then its stroke, in order, fills by the nonzero rule, strokes with
// miter joins (bevelled past SVG's miter limit of 4), butt caps and their
// dashes, at the shape's opacity, on a transparent background.
package raster

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// MaxHeight bounds the height an icon is drawn at, in pixels: enough for
// a label a foot high at 300 DPI.
const MaxHeight = 4096

// miterLimit is SVG's default stroke-miterlimit.
const miterLimit = 4

type point = [2]float64

// polyline is one outline, flattened: closed when it returns to its start.
type polyline struct {
	pts    []point
	closed bool
}

// transform maps a point p of the icon to s·p + t, in pixels.
type transform struct {
	s  float64
	tx float64
	ty float64
}

func (x transform) apply(p point) point { return point{p[0]*x.s + x.tx, p[1]*x.s + x.ty} }

// then is x after a group's translate(tx ty) scale(sc).
func (x transform) then(tx, ty, sc float64) transform {
	t := x.apply(point{tx, ty})
	return transform{s: x.s * sc, tx: t[0], ty: t[1]}
}

// Draw draws an icon height pixels high, its width in proportion, painting
// each token as colors says. A color that is not #rgb, #rrggbb or
// #rrggbbaa paints nothing.
func Draw(icon *ir.Icon, colors svg.Colors, height int) (*image.NRGBA, error) {
	return DrawWith(icon, colors, height, Options{})
}

// Options change how an icon is drawn.
type Options struct {
	// Outline draws strokes only, every one in Ink: no fills.
	Outline bool
	Ink     color.NRGBA
}

// DrawWith draws an icon as Draw does, as opts say.
func DrawWith(icon *ir.Icon, colors svg.Colors, height int, opts Options) (*image.NRGBA, error) {
	vb := icon.Viewbox
	if height < 1 || height > MaxHeight {
		return nil, fmt.Errorf("an icon is drawn 1 to %d pixels high", MaxHeight)
	}
	if !(vb[2] > 0 && vb[3] > 0) {
		return nil, fmt.Errorf("icon %q has an empty viewbox", icon.Name)
	}
	s := float64(height) / vb[3]
	width := max(1, int(math.Round(vb[2]*s)))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	d := drawer{img: img, colors: colors, opts: opts}
	if err := d.shapes(icon.Shapes, transform{s: s, tx: -vb[0] * s, ty: -vb[1] * s}); err != nil {
		return nil, fmt.Errorf("icon %q: %w", icon.Name, err)
	}
	return img, nil
}

// Encode draws an icon as Draw does, as a PNG.
func Encode(w io.Writer, icon *ir.Icon, colors svg.Colors, height int) error {
	img, err := Draw(icon, colors, height)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

type drawer struct {
	img    *image.NRGBA
	colors svg.Colors
	opts   Options
}

func (d drawer) shapes(shapes []ir.Shape, x transform) error {
	for i := range shapes {
		s := &shapes[i]
		if s.Kind == ir.KindGroup {
			if err := d.shapes(s.Shapes, x.then(s.TX, s.TY, s.Scale)); err != nil {
				return err
			}
			continue
		}
		lines, err := outline(s, x)
		if err != nil {
			return err
		}
		if c, ok := d.paint(s.Fill, s.FillOpacity); ok && !d.opts.Outline {
			d.fill(lines, c)
		}
		c, ok := d.paint(s.Stroke, s.FillOpacity)
		if d.opts.Outline && s.Stroke != "" && s.Stroke != "none" {
			c, ok = d.opts.Ink, true
		}
		if ok && s.StrokeWidth > 0 {
			if s.Dash != nil {
				lines = dashed(lines, dashes(*s.Dash, x.s))
			}
			d.fill(stroke(lines, s.StrokeWidth*x.s), c)
		}
	}
	return nil
}

// paint is a token's color at an opacity, or false for none.
func (d drawer) paint(token string, opacity float64) (color.NRGBA, bool) {
	if token == "" || token == "none" {
		return color.NRGBA{}, false
	}
	c, ok := parseColor(d.colors(token))
	if !ok {
		return color.NRGBA{}, false
	}
	c.A = uint8(math.Round(float64(c.A) * math.Max(0, math.Min(1, opacity))))
	return c, c.A > 0
}

// fill paints the area the outlines enclose, by the nonzero rule.
func (d drawer) fill(lines []polyline, c color.NRGBA) {
	b := d.img.Bounds()
	z := vector.NewRasterizer(b.Dx(), b.Dy())
	drawn := false
	for _, l := range lines {
		if len(l.pts) < 3 {
			continue
		}
		z.MoveTo(float32(l.pts[0][0]), float32(l.pts[0][1]))
		for _, p := range l.pts[1:] {
			z.LineTo(float32(p[0]), float32(p[1]))
		}
		z.ClosePath()
		drawn = true
	}
	if !drawn {
		return
	}
	mask := image.NewAlpha(b)
	z.Draw(mask, b, image.Opaque, image.Point{})
	draw.DrawMask(d.img, b, &image.Uniform{C: c}, image.Point{}, mask, image.Point{}, draw.Over)
}

// outline flattens a shape into polylines, in pixels.
func outline(s *ir.Shape, x transform) ([]polyline, error) {
	switch s.Kind {
	case ir.KindPolygon:
		pts := make([]point, len(s.Points))
		for i, p := range s.Points {
			pts[i] = x.apply(p)
		}
		return []polyline{{pts: pts, closed: true}}, nil
	case ir.KindEllipse:
		return []polyline{ellipse(s.CX, s.CY, s.RX, s.RY, x)}, nil
	case ir.KindCircle:
		return []polyline{ellipse(s.CX, s.CY, s.R, s.R, x)}, nil
	case ir.KindRect:
		return []polyline{rect(s, x)}, nil
	case ir.KindPath:
		return path(s.D, x)
	}
	return nil, fmt.Errorf("a shape of unknown kind %q", s.Kind)
}

// pieces is how many straight pieces a curve about length pixels long is
// drawn in.
func pieces(length float64) int { return max(4, min(256, int(length/2)+1)) }

func ellipse(cx, cy, rx, ry float64, x transform) polyline {
	if rx <= 0 || ry <= 0 {
		return polyline{}
	}
	n := pieces(2 * math.Pi * math.Max(rx, ry) * x.s)
	pts := make([]point, n)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = x.apply(point{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
	}
	return polyline{pts: pts, closed: true}
}

// rect is a rectangle, its corners rounded by rx as SVG rounds them, ry
// taking rx's value.
func rect(s *ir.Shape, x transform) polyline {
	if s.W <= 0 || s.H <= 0 {
		return polyline{}
	}
	r := math.Min(s.RX, math.Min(s.W, s.H)/2)
	x0, y0, x1, y1 := s.X, s.Y, s.X+s.W, s.Y+s.H
	if r <= 0 {
		return polyline{pts: []point{x.apply(point{x0, y0}), x.apply(point{x1, y0}), x.apply(point{x1, y1}), x.apply(point{x0, y1})}, closed: true}
	}
	var pts []point
	corner := func(cx, cy, from float64) {
		n := pieces(math.Pi / 2 * r * x.s)
		for i := 0; i <= n; i++ {
			a := from + math.Pi/2*float64(i)/float64(n)
			pts = append(pts, x.apply(point{cx + r*math.Cos(a), cy + r*math.Sin(a)}))
		}
	}
	corner(x1-r, y0+r, -math.Pi/2)
	corner(x1-r, y1-r, 0)
	corner(x0+r, y1-r, math.Pi/2)
	corner(x0+r, y0+r, math.Pi)
	return polyline{pts: pts, closed: true}
}

// path flattens SVG path data, each subpath a polyline.
func path(d string, x transform) ([]polyline, error) {
	segs, err := labels.ParsePathData(d)
	if err != nil {
		return nil, err
	}
	var out []polyline
	var cur polyline
	var at point
	flush := func() {
		if len(cur.pts) > 1 {
			out = append(out, cur)
		}
		cur = polyline{}
	}
	for _, sg := range segs {
		switch sg.Op {
		case labels.SegMove:
			flush()
			at = x.apply(sg.Pts[0])
			cur.pts = []point{at}
		case labels.SegLine:
			at = x.apply(sg.Pts[0])
			cur.pts = append(cur.pts, at)
		case labels.SegQuad, labels.SegCubic:
			c1, end := x.apply(sg.Pts[0]), x.apply(sg.End())
			c2 := c1
			if sg.Op == labels.SegCubic {
				c2 = x.apply(sg.Pts[1])
			} else {
				// the quadratic as the cubic it is
				q := c1
				c1 = point{at[0] + 2.0/3*(q[0]-at[0]), at[1] + 2.0/3*(q[1]-at[1])}
				c2 = point{end[0] + 2.0/3*(q[0]-end[0]), end[1] + 2.0/3*(q[1]-end[1])}
			}
			n := pieces(dist(at, c1) + dist(c1, c2) + dist(c2, end))
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				u := 1 - t
				cur.pts = append(cur.pts, point{
					u*u*u*at[0] + 3*u*u*t*c1[0] + 3*u*t*t*c2[0] + t*t*t*end[0],
					u*u*u*at[1] + 3*u*u*t*c1[1] + 3*u*t*t*c2[1] + t*t*t*end[1],
				})
			}
			at = end
		case labels.SegClose:
			if len(cur.pts) > 0 {
				cur.closed = true
				start := cur.pts[0]
				flush()
				// a path goes on from where the closed subpath started
				at = start
				cur.pts = []point{at}
			}
		}
	}
	flush()
	return out, nil
}

func dist(a, b point) float64 { return math.Hypot(b[0]-a[0], b[1]-a[1]) }

// dashes reads a dash array, scaled to pixels; nil draws it solid, as SVG
// does an array of no positive length.
func dashes(spec string, scale float64) []float64 {
	var out []float64
	total := 0.0
	for _, f := range strings.FieldsFunc(spec, func(r rune) bool { return r == ' ' || r == ',' }) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		out = append(out, v*scale)
		total += v
	}
	if total <= 0 {
		return nil
	}
	if len(out)%2 == 1 {
		out = append(out, out...)
	}
	return out
}

// dashed cuts outlines into their dashes, each restarting the array.
func dashed(lines []polyline, array []float64) []polyline {
	if array == nil {
		return lines
	}
	var out []polyline
	for _, l := range lines {
		pts := l.pts
		if l.closed && len(pts) > 0 {
			pts = append(append([]point(nil), pts...), pts[0])
		}
		i, left, on := 0, array[0], true
		var cur []point
		if on && len(pts) > 0 {
			cur = []point{pts[0]}
		}
		for k := 1; k < len(pts); k++ {
			a, b := pts[k-1], pts[k]
			seg := dist(a, b)
			done := 0.0
			for seg-done > left {
				done += left
				p := point{a[0] + (b[0]-a[0])*done/seg, a[1] + (b[1]-a[1])*done/seg}
				if on {
					out = append(out, polyline{pts: append(cur, p)})
					cur = nil
				} else {
					cur = []point{p}
				}
				on = !on
				i = (i + 1) % len(array)
				left = array[i]
			}
			left -= seg - done
			if on {
				cur = append(cur, b)
			}
		}
		if on && len(cur) > 1 {
			out = append(out, polyline{pts: cur})
		}
	}
	return out
}

// stroke outlines the outlines width pixels wide: a quadrilateral for each
// piece, butt-ended, and a wedge at each joint for its miter, or its
// bevel past the limit. Every outline turns the same way, so that where
// they overlap they add up, not cancel.
func stroke(lines []polyline, width float64) []polyline {
	h := width / 2
	var out []polyline
	add := func(pts ...point) {
		if area(pts) < 0 {
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
		out = append(out, polyline{pts: pts, closed: true})
	}
	for _, l := range lines {
		pts := dedupe(l.pts)
		closed := l.closed && len(pts) > 2
		if closed && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		n := len(pts)
		if n < 2 {
			continue
		}
		segs := n - 1
		if closed {
			segs = n
		}
		normal := func(k int) point {
			a, b := pts[k%n], pts[(k+1)%n]
			l := dist(a, b)
			return point{-(b[1] - a[1]) / l, (b[0] - a[0]) / l}
		}
		for k := 0; k < segs; k++ {
			a, b, m := pts[k%n], pts[(k+1)%n], normal(k)
			add(point{a[0] + m[0]*h, a[1] + m[1]*h}, point{b[0] + m[0]*h, b[1] + m[1]*h},
				point{b[0] - m[0]*h, b[1] - m[1]*h}, point{a[0] - m[0]*h, a[1] - m[1]*h})
		}
		// joints: between pieces, and where a closed outline meets itself
		joints := n - 2
		if closed {
			joints = n
		}
		for k := 1; k <= joints; k++ {
			p, n1, n2 := pts[k%n], normal(k-1), normal(k)
			turn := n1[0]*n2[1] - n1[1]*n2[0]
			if math.Abs(turn) < 1e-9 {
				continue
			}
			// the outer side of the turn
			o := -math.Copysign(1, turn)
			a := point{p[0] + o*n1[0]*h, p[1] + o*n1[1]*h}
			b := point{p[0] + o*n2[0]*h, p[1] + o*n2[1]*h}
			sum := point{n1[0] + n2[0], n1[1] + n2[1]}
			l := math.Hypot(sum[0], sum[1])
			if l > 1e-9 && 2/l <= miterLimit {
				r := h * 2 / (l * l)
				add(p, a, point{p[0] + o*sum[0]*r, p[1] + o*sum[1]*r}, b)
			} else {
				add(p, a, b)
			}
		}
	}
	return out
}

func dedupe(pts []point) []point {
	out := make([]point, 0, len(pts))
	for _, p := range pts {
		if len(out) == 0 || out[len(out)-1] != p {
			out = append(out, p)
		}
	}
	return out
}

// area is a polygon's signed area.
func area(pts []point) float64 {
	a := 0.0
	for i := range pts {
		p, q := pts[i], pts[(i+1)%len(pts)]
		a += p[0]*q[1] - q[0]*p[1]
	}
	return a / 2
}

// parseColor reads #rgb, #rrggbb or #rrggbbaa.
func parseColor(s string) (color.NRGBA, bool) {
	if len(s) < 4 || s[0] != '#' {
		return color.NRGBA{}, false
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) == 6 {
		h += "ff"
	}
	if len(h) != 8 {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, true
}
