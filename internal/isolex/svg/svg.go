// Package svg draws isolex icons (the IR in package ir) as SVG, exactly
// as isolex's own reference renderer does (isolex SPEC §7, src/render.rs;
// isolex is Axonibyte Innovations, LLC's, see ../NOTICE): the same
// elements, attributes and order, and every number printed with the same
// two-decimal rule, so that for the same IR and colors the bytes are the
// same. What a token is painted with is the caller's: a palette for an
// image, or a CSS variable for a page that themes it. It uses only the
// standard library, so the label renderer can use it under TinyGo.
package svg

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
)

// Colors says what a theme token is painted with. It is never asked
// about "none", which is no paint.
type Colors func(token string) string

// IsolexPalette is isolex's built-in palette, the placeholder colors its
// renderer uses when given none.
var IsolexPalette = map[string]string{
	"body-top":   "#e4e7eb",
	"body-left":  "#cbd2d9",
	"body-right": "#9aa5b1",
	"outline":    "#1f2933",
	"ink":        "#1f2933",
	"ink-muted":  "#7b8794",
	"shadow":     "#000000",
	"virt":       "#d9e2ec",
	"accent":     "#d64545",
	"accent-dim": "#e8a0a0",
}

// Palette paints each token with its color in palette, and a token it
// has none for with the token itself, as isolex does (Missing finds them
// first).
func Palette(palette map[string]string) Colors {
	return func(token string) string {
		if c, ok := palette[token]; ok {
			return c
		}
		return token
	}
}

// Missing returns, in order, every token icons paint with that palette
// has no color for.
func Missing(icons []ir.Icon, palette map[string]string) []string {
	seen := map[string]bool{}
	var walk func([]ir.Shape)
	walk = func(shapes []ir.Shape) {
		for _, s := range shapes {
			if s.Kind == ir.KindGroup {
				walk(s.Shapes)
				continue
			}
			for _, token := range []string{s.Fill, s.Stroke} {
				if _, ok := palette[token]; token != "none" && !ok {
					seen[token] = true
				}
			}
		}
	}
	for i := range icons {
		walk(icons[i].Shapes)
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Number is isolex's formatting rule (SPEC §7): two decimal places, ties
// to even on the number's exact binary value, and zero never negative.
func Number(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	if s == "-0.00" {
		return "0.00"
	}
	return s
}

// Icon draws one icon as a standalone SVG document.
func Icon(icon *ir.Icon, colors Colors) string {
	return Sized(icon, colors, icon.Viewbox[3])
}

// Sized draws one icon as Icon does, height units high, its width in
// proportion; at the viewbox's own height it is Icon.
func Sized(icon *ir.Icon, colors Colors, height float64) string {
	var b strings.Builder
	v := icon.Viewbox
	width := v[2]
	if v[3] > 0 {
		width = v[2] * height / v[3]
	}
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="`)
	b.WriteString(Number(v[0]) + " " + Number(v[1]) + " " + Number(v[2]) + " " + Number(v[3]))
	b.WriteString(`" width="` + Number(width) + `" height="` + Number(height) + "\">\n")
	b.WriteString("<title>" + escape(icon.Name) + "</title>\n")
	w := writer{b: &b, colors: colors, icon: icon.Name}
	w.shapes(icon.Shapes, 1, false)
	b.WriteString("</svg>\n")
	return b.String()
}

// writer draws one icon's shapes, counting the clip paths it declares.
type writer struct {
	b      *strings.Builder
	colors Colors
	icon   string
	clips  int
}

// shapes draws ss indented depth deep. A version 2 group is drawn as
// isolex 0.4 draws it: a clip path declared where it stands, named for the
// icon and numbered in order, around a group drawn through its matrix,
// so the clip is in the coordinates the group is drawn in; in a group
// with fixed strokes, every stroke keeps its width. A version 1 group is
// drawn as isolex 0.3 drew a badge.
func (w *writer) shapes(ss []ir.Shape, depth int, fixed bool) {
	b := w.b
	indent := strings.Repeat("  ", depth)
	for i := range ss {
		s := &ss[i]
		b.WriteString(indent)
		switch s.Kind {
		case ir.KindGroup:
			if s.Matrix == nil {
				b.WriteString(`<g transform="translate(` + Number(s.TX) + " " + Number(s.TY) + ") scale(" + Number(s.Scale) + ")\">\n")
				w.shapes(s.Shapes, depth+1, fixed)
				b.WriteString(indent + "</g>\n")
				continue
			}
			inside, deeper := indent, depth+1
			if s.Clip != nil {
				w.clips++
				id := escape("clip-" + w.icon + "-" + strconv.Itoa(w.clips))
				b.WriteString(`<clipPath id="` + id + `"><polygon points="` + points(s.Clip) + "\"/></clipPath>\n")
				b.WriteString(indent + `<g clip-path="url(#` + id + ")\">\n")
				inside += "  "
				deeper++
				b.WriteString(inside)
			}
			m := s.Matrix
			b.WriteString(`<g transform="matrix(` + Number(m[0]) + " " + Number(m[1]) + " " + Number(m[2]) + " " +
				Number(m[3]) + " " + Number(m[4]) + " " + Number(m[5]) + ")\">\n")
			w.shapes(s.Shapes, deeper, s.FixedStrokes())
			b.WriteString(inside + "</g>\n")
			if s.Clip != nil {
				b.WriteString(indent + "</g>\n")
			}
			continue
		case ir.KindPolygon:
			b.WriteString(`<polygon points="` + points(s.Points) + `"`)
		case ir.KindPath:
			b.WriteString(`<path d="` + escape(s.D) + `"`)
		case ir.KindEllipse:
			b.WriteString(`<ellipse cx="` + Number(s.CX) + `" cy="` + Number(s.CY) + `" rx="` + Number(s.RX) + `" ry="` + Number(s.RY) + `"`)
		case ir.KindCircle:
			b.WriteString(`<circle cx="` + Number(s.CX) + `" cy="` + Number(s.CY) + `" r="` + Number(s.R) + `"`)
		case ir.KindRect:
			b.WriteString(`<rect x="` + Number(s.X) + `" y="` + Number(s.Y) + `" width="` + Number(s.W) + `" height="` + Number(s.H) + `"`)
			if s.RX > 0 {
				b.WriteString(` rx="` + Number(s.RX) + `"`)
			}
		}
		paint(b, s, w.colors)
		if fixed && s.Stroke != "none" {
			b.WriteString(` vector-effect="non-scaling-stroke"`)
		}
		b.WriteString("/>\n")
	}
}

func points(ps [][2]float64) string {
	out := make([]string, len(ps))
	for j, p := range ps {
		out[j] = Number(p[0]) + "," + Number(p[1])
	}
	return strings.Join(out, " ")
}

// paint writes fill and stroke, always in this order; an opacity of one,
// and a stroke's width and dash where there is no stroke, are left out.
func paint(b *strings.Builder, s *ir.Shape, colors Colors) {
	color := func(token string) string {
		if token == "none" {
			return "none"
		}
		return escape(colors(token))
	}
	b.WriteString(` fill="` + color(s.Fill) + `"`)
	if s.Fill != "none" && s.FillOpacity != 1 {
		b.WriteString(` fill-opacity="` + Number(s.FillOpacity) + `"`)
	}
	b.WriteString(` stroke="` + color(s.Stroke) + `"`)
	if s.Stroke != "none" {
		b.WriteString(` stroke-width="` + Number(s.StrokeWidth) + `"`)
		if s.FillOpacity != 1 {
			b.WriteString(` stroke-opacity="` + Number(s.FillOpacity) + `"`)
		}
		if s.Dash != nil {
			b.WriteString(` stroke-dasharray="` + escape(*s.Dash) + `"`)
		}
	}
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func escape(s string) string { return escaper.Replace(s) }
