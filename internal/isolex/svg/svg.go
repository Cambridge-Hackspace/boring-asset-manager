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
	var b strings.Builder
	v := icon.Viewbox
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="`)
	b.WriteString(Number(v[0]) + " " + Number(v[1]) + " " + Number(v[2]) + " " + Number(v[3]))
	b.WriteString(`" width="` + Number(v[2]) + `" height="` + Number(v[3]) + "\">\n")
	b.WriteString("<title>" + escape(icon.Name) + "</title>\n")
	shapes(&b, icon.Shapes, colors, 1)
	b.WriteString("</svg>\n")
	return b.String()
}

func shapes(b *strings.Builder, ss []ir.Shape, colors Colors, depth int) {
	indent := strings.Repeat("  ", depth)
	for i := range ss {
		s := &ss[i]
		b.WriteString(indent)
		switch s.Kind {
		case ir.KindGroup:
			b.WriteString(`<g transform="translate(` + Number(s.TX) + " " + Number(s.TY) + ") scale(" + Number(s.Scale) + ")\">\n")
			shapes(b, s.Shapes, colors, depth+1)
			b.WriteString(indent + "</g>\n")
			continue
		case ir.KindPolygon:
			points := make([]string, len(s.Points))
			for j, p := range s.Points {
				points[j] = Number(p[0]) + "," + Number(p[1])
			}
			b.WriteString(`<polygon points="` + strings.Join(points, " ") + `"`)
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
		paint(b, s, colors)
		b.WriteString("/>\n")
	}
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
