// Package svgimport turns SVG artwork into label template shapes: paths in
// black or white, which print sharp at any size (docs/labels.md). It reads
// only geometry and paint, and says what it leaves out.
package svgimport

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Limits on what is read.
const (
	// MaxBytes caps an SVG file.
	MaxBytes = 1 << 20
	// MaxDepth caps how deeply its elements nest.
	MaxDepth = 64
	// MaxElements caps how many elements it has, drawn or not.
	MaxElements = 10000
)

// Result is imported artwork: its size, in its own units, and its shapes,
// placed in that size, with what was left out.
type Result struct {
	Width, Height float64
	Elements      []labels.Element
	// Skipped says what was left out, once for each kind of thing.
	Skipped []string
}

// matrix is an affine transform: (x, y) goes to (a x + c y + e, b x + d y + f).
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// then is m followed by n.
func (m matrix) then(n matrix) matrix {
	return matrix{
		n[0]*m[0] + n[2]*m[1], n[1]*m[0] + n[3]*m[1],
		n[0]*m[2] + n[2]*m[3], n[1]*m[2] + n[3]*m[3],
		n[0]*m[4] + n[2]*m[5] + n[4], n[1]*m[4] + n[3]*m[5] + n[5],
	}
}

func (m matrix) apply(p [2]float64) [2]float64 {
	return [2]float64{m[0]*p[0] + m[2]*p[1] + m[4], m[1]*p[0] + m[3]*p[1] + m[5]}
}

// scale is how much the transform scales lengths, on average.
func (m matrix) scale() float64 { return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2])) }

// style is what an element inherits from those it is in.
type style struct {
	fill, stroke              paint
	strokeWidth               float64
	fillRule                  string
	opacity, fillOp, strokeOp float64
	hidden                    bool
}

// paint is a color reduced to black or white, or none; gradient says it
// was a gradient, drawn as its fallback or black.
type paint struct {
	name     string
	alpha    float64
	gradient bool
}

type importer struct {
	res      Result
	skipped  map[string]bool
	elements int
	segments int
}

func (im *importer) skip(why string) {
	if !im.skipped[why] {
		im.skipped[why] = true
		im.res.Skipped = append(im.res.Skipped, why)
	}
}

// Import reads SVG artwork. It fails on what is not SVG, or past the
// limits; what it does not draw it leaves out and lists.
func Import(data []byte) (*Result, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("the file is %d bytes; at most %d are read", len(data), MaxBytes)
	}
	im := &importer{skipped: map[string]bool{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	// Only the entities XML itself defines; a document type is refused below.
	dec.Entity = nil
	dec.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		return nil, fmt.Errorf("the file is in %s; only UTF-8 is read", label)
	}
	root, err := im.next(dec)
	if err != nil {
		return nil, err
	}
	if root == nil || root.Name.Local != "svg" {
		return nil, errors.New("it is not an SVG file: its first element is not svg")
	}
	base := style{fill: paint{name: labels.PaintBlack, alpha: 1}, stroke: paint{name: labels.PaintNone, alpha: 1}, strokeWidth: 1, fillRule: labels.FillNonzero,
		opacity: 1, fillOp: 1, strokeOp: 1}
	view, width, height, ok := viewOf(root)
	m, s, shown := im.styled(root, view, base)
	if shown {
		if err := im.walk(dec, m, s, 1); err != nil {
			return nil, err
		}
	}
	if !ok {
		// No size: the artwork's own extent.
		width, height = im.extent()
	}
	im.res.Width, im.res.Height = width, height
	sort.Strings(im.res.Skipped)
	if len(im.res.Elements) == 0 {
		return &im.res, errors.New("it draws nothing a label can print")
	}
	return &im.res, nil
}

// next is the next start element, past text, comments and processing
// instructions; nil at the end. A document type is refused.
func (im *importer) next(dec *xml.Decoder) (*xml.StartElement, error) {
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("it is not well-formed XML: %v", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, errors.New("it has a document type declaration, which is not read")
		case xml.StartElement:
			im.elements++
			if im.elements > MaxElements {
				return nil, fmt.Errorf("it has more than %d elements", MaxElements)
			}
			return &t, nil
		case xml.EndElement:
			return nil, nil
		}
	}
}

// walk draws an element's children, within it.
func (im *importer) walk(dec *xml.Decoder, m matrix, s style, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("its elements nest more than %d deep", MaxDepth)
	}
	for {
		child, err := im.next(dec)
		if err != nil {
			return err
		}
		if child == nil {
			return nil
		}
		if err := im.element(dec, child, m, s, depth+1); err != nil {
			return err
		}
	}
}

// element draws one element, and what it holds.
func (im *importer) element(dec *xml.Decoder, el *xml.StartElement, m matrix, s style, depth int) error {
	name := el.Name.Local
	for _, a := range el.Attr {
		if a.Name.Local == "href" {
			im.skip("links to other files")
		}
	}
	switch name {
	case "g", "a", "switch":
		inner, cs, ok := im.styled(el, m, s)
		if !ok {
			return dec.Skip()
		}
		return im.walk(dec, inner, cs, depth)
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		inner, cs, ok := im.styled(el, m, s)
		if ok {
			im.shape(el, inner, cs)
		}
		return dec.Skip()
	case "title", "desc", "metadata", "defs":
		// Nothing drawn: what defs holds is drawn only where it is used.
	case "text", "tspan", "textPath":
		im.skip("text, which needs the artwork's fonts: convert it to paths before exporting")
	case "image":
		im.skip("images")
	case "style":
		im.skip("style sheets: only styles on the elements themselves are read")
	case "script":
		im.skip("scripts")
	case "linearGradient", "radialGradient", "pattern":
		// Read where they are used.
	case "filter":
		im.skip("filters")
	case "mask", "clipPath":
		im.skip("masks and clipping")
	case "use", "symbol", "marker", "foreignObject", "svg":
		im.skip(name + " elements")
	default:
		im.skip(name + " elements")
	}
	return dec.Skip()
}

// styled is an element's transform and style, from its attributes and its
// style attribute; not ok when it is hidden.
func (im *importer) styled(el *xml.StartElement, m matrix, s style) (matrix, style, bool) {
	props := map[string]string{}
	for _, a := range el.Attr {
		props[a.Name.Local] = strings.TrimSpace(a.Value)
	}
	if css, ok := props["style"]; ok {
		for _, decl := range strings.Split(css, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if ok {
				props[strings.TrimSpace(k)] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
			}
		}
	}
	if props["display"] == "none" || props["visibility"] == "hidden" {
		return m, s, false
	}
	if t, ok := props["transform"]; ok {
		tm, err := parseTransform(t)
		if err != nil {
			im.skip("transforms it cannot read")
			return m, s, false
		}
		m = tm.then(m)
	}
	if v, ok := props["fill"]; ok {
		s.fill = im.paintOf(v, s.fill)
	}
	if v, ok := props["stroke"]; ok {
		s.stroke = im.paintOf(v, s.stroke)
	}
	if v, ok := props["stroke-width"]; ok {
		if w, err := length(v); err == nil && w >= 0 {
			s.strokeWidth = w
		}
	}
	if v := props["fill-rule"]; v == "evenodd" || v == "nonzero" {
		s.fillRule = v
	}
	for k, dst := range map[string]*float64{"opacity": &s.opacity, "fill-opacity": &s.fillOp, "stroke-opacity": &s.strokeOp} {
		if v, ok := props[k]; ok {
			if o, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
				if strings.HasSuffix(v, "%") {
					o /= 100
				}
				// Opacity multiplies down the tree; fill and stroke opacity are inherited.
				if k == "opacity" {
					*dst *= o
				} else {
					*dst = o
				}
			}
		}
	}
	return m, s, true
}

// paintOf reads a paint: none, a color reduced to black or white by its
// lightness, or a gradient or pattern, drawn as its fallback, or black.
func (im *importer) paintOf(v string, inherit paint) paint {
	v = strings.TrimSpace(v)
	switch {
	case v == "inherit":
		return inherit
	case v == "none" || v == "transparent":
		return paint{name: labels.PaintNone, alpha: 1}
	case strings.HasPrefix(v, "url("):
		im.skip("gradients and patterns, drawn in their fallback color or black")
		if i := strings.Index(v, ")"); i >= 0 && strings.TrimSpace(v[i+1:]) != "" {
			p := im.paintOf(v[i+1:], inherit)
			p.gradient = true
			return p
		}
		return paint{name: labels.PaintBlack, alpha: 1, gradient: true}
	}
	r, g, b, a, ok := parseColor(v)
	if !ok {
		im.skip("colors it cannot read, drawn black")
		return paint{name: labels.PaintBlack, alpha: 1}
	}
	// Lightness as people see it, from linear light.
	lin := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	y := 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
	if y > 0.18 { // halfway in lightness, L* 50
		return paint{name: labels.PaintWhite, alpha: a}
	}
	return paint{name: labels.PaintBlack, alpha: a}
}

// shape draws a path, rect, circle, ellipse, line, polyline or polygon.
func (im *importer) shape(el *xml.StartElement, m matrix, s style) {
	attr := map[string]string{}
	for _, a := range el.Attr {
		attr[a.Name.Local] = a.Value
	}
	num := func(k string) float64 {
		v, err := length(attr[k])
		if err != nil {
			return 0
		}
		return v
	}
	var d string
	closed := true
	switch el.Name.Local {
	case "path":
		d = attr["d"]
	case "rect":
		x, y, w, h := num("x"), num("y"), num("width"), num("height")
		rx, ry := num("rx"), num("ry")
		if _, ok := attr["rx"]; !ok {
			rx = ry
		}
		if _, ok := attr["ry"]; !ok {
			ry = rx
		}
		rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
		if w <= 0 || h <= 0 {
			return
		}
		if rx > 0 && ry > 0 {
			d = fmt.Sprintf("M%g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %g H%g A%g %g 0 0 1 %g %g V%g A%g %g 0 0 1 %g %gZ",
				x+rx, y, x+w-rx, rx, ry, x+w, y+ry, y+h-ry, rx, ry, x+w-rx, y+h, x+rx, rx, ry, x, y+h-ry, y+ry, rx, ry, x+rx, y)
		} else {
			d = fmt.Sprintf("M%g %g H%g V%g H%gZ", x, y, x+w, y+h, x)
		}
	case "circle", "ellipse":
		cx, cy := num("cx"), num("cy")
		rx, ry := num("r"), num("r")
		if el.Name.Local == "ellipse" {
			rx, ry = num("rx"), num("ry")
		}
		if rx <= 0 || ry <= 0 {
			return
		}
		d = fmt.Sprintf("M%g %g A%g %g 0 1 1 %g %g A%g %g 0 1 1 %g %gZ", cx+rx, cy, rx, ry, cx-rx, cy, rx, ry, cx+rx, cy)
	case "line":
		d = fmt.Sprintf("M%g %g L%g %g", num("x1"), num("y1"), num("x2"), num("y2"))
		closed = false
	case "polyline", "polygon":
		pts := strings.TrimSpace(attr["points"])
		if pts == "" {
			return
		}
		d = "M" + pts
		if el.Name.Local == "polygon" {
			d += "Z"
		} else {
			closed = false
		}
	}
	segs, err := labels.ParsePathData(d)
	if err != nil {
		im.skip("paths it cannot read")
		return
	}
	if im.segments+len(segs) > labels.MaxTemplatePathSegments {
		im.skip(fmt.Sprintf("shapes past %d segments in all", labels.MaxTemplatePathSegments))
		return
	}

	fill := s.fill
	if !closed || fill.alpha*s.fillOp*s.opacity < 0.5 {
		// An open line has no inside; paint mostly see-through is none.
		fill = paint{name: labels.PaintNone}
	}
	stroke := s.stroke
	width := s.strokeWidth * m.scale()
	if stroke.alpha*s.strokeOp*s.opacity < 0.5 || width <= 0 {
		stroke = paint{name: labels.PaintNone}
	}
	if fill.name == labels.PaintNone && stroke.name == labels.PaintNone {
		return
	}

	// The points, transformed, and the box round them all, control points too.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range segs {
		for k := range segs[i].Points() {
			p := m.apply(segs[i].Pts[k])
			segs[i].Pts[k] = p
			minX, minY = math.Min(minX, p[0]), math.Min(minY, p[1])
			maxX, maxY = math.Max(maxX, p[0]), math.Max(maxY, p[1])
		}
	}
	// A level or upright line still has a box: as deep as its stroke.
	w, h := maxX-minX, maxY-minY
	least := math.Max(width, 1e-3*math.Max(w, h))
	if least <= 0 {
		return
	}
	if w < least {
		minX, w = minX-(least-w)/2, least
	}
	if h < least {
		minY, h = minY-(least-h)/2, least
	}
	for i := range segs {
		for k := range segs[i].Points() {
			p := segs[i].Pts[k]
			segs[i].Pts[k] = [2]float64{(p[0] - minX) / w, (p[1] - minY) / h}
		}
	}
	im.segments += len(segs)
	e := labels.Element{Type: labels.ElementShape, Shape: labels.ShapePath, X: minX, Y: minY, Width: w, Height: h,
		D: labels.FormatPathData(segs), Fill: fill.name, Stroke: stroke.name}
	if stroke.name != labels.PaintNone {
		e.StrokeWidth = width
	}
	if s.fillRule == labels.FillEvenOdd && fill.name != labels.PaintNone {
		e.FillRule = labels.FillEvenOdd
	}
	im.res.Elements = append(im.res.Elements, e)
}

// extent is the box round everything drawn, from the origin.
func (im *importer) extent() (float64, float64) {
	w, h := 0.0, 0.0
	for _, e := range im.res.Elements {
		w, h = math.Max(w, e.X+e.Width), math.Max(h, e.Y+e.Height)
	}
	return w, h
}

// viewOf is the transform the root's viewBox sets up, and the artwork's
// size: its viewBox, or its width and height; not ok with neither.
func viewOf(root *xml.StartElement) (matrix, float64, float64, bool) {
	attr := map[string]string{}
	for _, a := range root.Attr {
		attr[a.Name.Local] = a.Value
	}
	if vb := strings.Fields(strings.ReplaceAll(attr["viewBox"], ",", " ")); len(vb) == 4 {
		var v [4]float64
		ok := true
		for i, s := range vb {
			f, err := strconv.ParseFloat(s, 64)
			ok = ok && err == nil
			v[i] = f
		}
		if ok && v[2] > 0 && v[3] > 0 {
			return matrix{1, 0, 0, 1, -v[0], -v[1]}, v[2], v[3], true
		}
	}
	w, errW := length(attr["width"])
	h, errH := length(attr["height"])
	if errW == nil && errH == nil && w > 0 && h > 0 {
		return identity, w, h, true
	}
	return identity, 0, 0, false
}

// length reads a length, without its unit.
func length(v string) (float64, error) {
	v = strings.TrimSpace(v)
	for _, unit := range []string{"px", "pt", "mm", "cm", "in", "em", "%"} {
		v = strings.TrimSuffix(v, unit)
	}
	return strconv.ParseFloat(strings.TrimSpace(v), 64)
}

// Place fits the artwork into a box, keeping its proportions, centered:
// its shapes moved and scaled, strokes with them.
func (r *Result) Place(x, y, width, height float64) []labels.Element {
	s := math.Min(width/r.Width, height/r.Height)
	ox, oy := x+(width-r.Width*s)/2, y+(height-r.Height*s)/2
	tidy := func(v float64) float64 { return math.Round(v*1e4) / 1e4 }
	out := make([]labels.Element, len(r.Elements))
	for i, e := range r.Elements {
		e.X, e.Y = tidy(ox+e.X*s), tidy(oy+e.Y*s)
		e.Width, e.Height = math.Max(tidy(e.Width*s), 1e-4), math.Max(tidy(e.Height*s), 1e-4)
		if e.StrokeWidth > 0 {
			e.StrokeWidth = math.Max(tidy(e.StrokeWidth*s), 1e-4)
		}
		out[i] = e
	}
	return out
}
