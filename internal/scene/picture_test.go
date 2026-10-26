package scene

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
)

// A scene's picture: the floor and a line along each tile edge, each item
// moved to where it stands, painting its category token in its own color,
// and the one shown outlined last.
func TestPicture(t *testing.T) {
	s := Layout(&[3]float64{2, 1, 1}, []Item{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}})
	painted := []ir.Shape{{Kind: ir.KindGroup, Scale: 1, Shapes: []ir.Shape{
		{Kind: ir.KindPolygon, Points: [][2]float64{{0, 0}, {1, 0}, {0, 1}}, Fill: "category", FillOpacity: 1, Stroke: "category", StrokeWidth: 1},
	}}}
	items := []Drawn{
		{Placed: s.Items[0], Shapes: painted, Color: "#123456"},
		{Placed: s.Items[1], Shapes: Box(s.Items[1].Size)},
	}
	icon, colors := Picture("Lab", s.Floor, s.Viewbox, items, "b")
	// floor, 3 + 2 grid lines, 2 items, the outline
	if len(icon.Shapes) != 1+5+2+1 || icon.Viewbox != s.Viewbox {
		t.Fatalf("%d shapes", len(icon.Shapes))
	}
	if g := icon.Shapes[6]; g.Kind != ir.KindGroup || g.TX != s.Items[0].TX || g.Shapes[0].Shapes[0].Fill != "bam-category-0" || g.Shapes[0].Shapes[0].Stroke != "bam-category-0" {
		t.Errorf("first item %+v", g)
	}
	if painted[0].Shapes[0].Fill != "category" {
		t.Error("the item's own shapes were changed")
	}
	if len(colors) != 1 || colors["bam-category-0"] != "#123456" {
		t.Errorf("colors %v", colors)
	}
	if o := icon.Shapes[8]; o.Stroke != "accent" || len(o.Points) != 6 {
		t.Errorf("outline %+v", o)
	}
	if icon, _ := Picture("Lab", s.Floor, s.Viewbox, items, ""); len(icon.Shapes) != 8 {
		t.Errorf("nothing shown: %d shapes", len(icon.Shapes))
	}
	out := svg.Icon(icon, svg.Palette(map[string]string{"bam-category-0": "#123456"}))
	if !strings.Contains(out, `fill="#123456"`) || !strings.Contains(out, "<title>Lab</title>") || !strings.Contains(out, `<path d="M0.00 0.00 L-34.64 20.00"`) {
		t.Errorf("%s", out)
	}
}
