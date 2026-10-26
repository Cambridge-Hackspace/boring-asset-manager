package scene

import (
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
)

// Drawn is a placed item with what it is drawn with: its shapes, at the
// origin, as an icon's are, and the color its category token paints in.
type Drawn struct {
	Placed
	Shapes []ir.Shape
	Color  string
}

// Picture composes a scene into one icon, for the SVG writer and the
// rasterizer to draw as they draw icons: the floor and its grid, then each
// item in the order given, back to front, moved to where it stands, and
// last an outline around the one whose ID is show, if any. Each item's
// category token is renamed to one of its own, and colors says what each
// paints in, to add to the palette.
func Picture(name string, floor [2]float64, viewbox [4]float64, items []Drawn, show string) (icon *ir.Icon, colors map[string]string) {
	pt := func(x, y, z float64) [2]float64 {
		px, py := P(x, y, z)
		return [2]float64{px, py}
	}
	w, d := floor[0], floor[1]
	icon = &ir.Icon{Name: name, Viewbox: viewbox, Describe: []string{}, Grants: [][2]string{}}
	icon.Shapes = append(icon.Shapes, ir.Shape{Kind: ir.KindPolygon, Points: [][2]float64{pt(0, 0, 0), pt(w, 0, 0), pt(w, d, 0), pt(0, d, 0)},
		Fill: "virt", FillOpacity: 1, Stroke: "none"})
	line := func(a, b [2]float64) ir.Shape {
		return ir.Shape{Kind: ir.KindPath, D: "M" + svg.Number(a[0]) + " " + svg.Number(a[1]) + " L" + svg.Number(b[0]) + " " + svg.Number(b[1]),
			Fill: "none", FillOpacity: 1, Stroke: "ink-muted", StrokeWidth: 0.6}
	}
	for i := 0.0; i <= w; i++ {
		icon.Shapes = append(icon.Shapes, line(pt(i, 0, 0), pt(i, d, 0)))
	}
	for j := 0.0; j <= d; j++ {
		icon.Shapes = append(icon.Shapes, line(pt(0, j, 0), pt(w, j, 0)))
	}

	colors = map[string]string{}
	var outline *ir.Shape
	for k, it := range items {
		token := "bam-category-" + strconv.Itoa(k)
		if it.Color != "" {
			colors[token] = it.Color
		} else {
			token = "category"
		}
		icon.Shapes = append(icon.Shapes, ir.Shape{Kind: ir.KindGroup, TX: it.TX, TY: it.TY, Scale: 1, Shapes: renamed(it.Shapes, token)})
		if it.ID == show {
			x, y, z := it.X, it.Y, it.Z
			a, b, c := it.Size[0], it.Size[1], it.Size[2]
			outline = &ir.Shape{Kind: ir.KindPolygon, Points: [][2]float64{
				pt(x, y, z+c), pt(x+a, y, z+c), pt(x+a, y, z), pt(x+a, y+b, z), pt(x, y+b, z), pt(x, y+b, z+c),
			}, Fill: "none", FillOpacity: 1, Stroke: "accent", StrokeWidth: 3}
		}
	}
	if outline != nil {
		icon.Shapes = append(icon.Shapes, *outline)
	}
	return icon, colors
}

// renamed copies shapes, painting with token where they paint with the
// category token.
func renamed(shapes []ir.Shape, token string) []ir.Shape {
	out := make([]ir.Shape, len(shapes))
	for i, s := range shapes {
		if s.Fill == "category" {
			s.Fill = token
		}
		if s.Stroke == "category" {
			s.Stroke = token
		}
		if s.Kind == ir.KindGroup {
			s.Shapes = renamed(s.Shapes, token)
		}
		out[i] = s
	}
	return out
}
