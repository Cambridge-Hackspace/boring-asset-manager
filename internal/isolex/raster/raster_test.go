package raster

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
)

var black = svg.Palette(map[string]string{"ink": "#000000", "half": "#00000080", "red": "#f00"})

func dash(s string) *string { return &s }

// icon is a 10 by 10 icon of these shapes.
func icon(shapes ...ir.Shape) *ir.Icon {
	return &ir.Icon{Name: "t", Viewbox: [4]float64{0, 0, 10, 10}, Shapes: shapes}
}

func square(x0, y0, x1, y1 float64) [][2]float64 {
	return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

func paint(t *testing.T, i *ir.Icon, height int) *image.NRGBA {
	t.Helper()
	img, err := Draw(i, black, height)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// alpha is each pixel's alpha, row by row.
func alpha(img *image.NRGBA) [][]uint8 {
	b := img.Bounds()
	rows := make([][]uint8, b.Dy())
	for y := range rows {
		rows[y] = make([]uint8, b.Dx())
		for x := range rows[y] {
			rows[y][x] = img.NRGBAAt(x, y).A
		}
	}
	return rows
}

func TestAFillCoversWhatItEncloses(t *testing.T) {
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindPolygon, Points: square(2, 2, 8, 8), Fill: "ink", FillOpacity: 1, Stroke: "none"}), 10))
	n := 0
	for y, row := range a {
		for x, v := range row {
			inside := x >= 2 && x < 8 && y >= 2 && y < 8
			if inside && v != 255 || !inside && v != 0 {
				t.Errorf("(%d,%d) = %d", x, y, v)
			}
			if v > 0 {
				n++
			}
		}
	}
	if n != 36 {
		t.Errorf("%d pixels", n)
	}
}

func TestOpacityAndColors(t *testing.T) {
	img := paint(t, icon(ir.Shape{Kind: ir.KindRect, X: 0, Y: 0, W: 10, H: 10, Fill: "red", FillOpacity: 0.5, Stroke: "none"}), 10)
	if c := img.NRGBAAt(5, 5); c.R != 255 || c.G != 0 || c.A != 128 {
		t.Errorf("%+v", c)
	}
	// a color with its own alpha, at an opacity
	img = paint(t, icon(ir.Shape{Kind: ir.KindRect, W: 10, H: 10, Fill: "half", FillOpacity: 0.5, Stroke: "none"}), 10)
	if c := img.NRGBAAt(5, 5); c.A != 64 {
		t.Errorf("%+v", c)
	}
	// a token with no color, and none, paint nothing
	img = paint(t, icon(ir.Shape{Kind: ir.KindRect, W: 10, H: 10, Fill: "nope", FillOpacity: 1, Stroke: "none"}), 10)
	if c := img.NRGBAAt(5, 5); c.A != 0 {
		t.Errorf("%+v", c)
	}
}

func TestAStrokeIsItsWidthAboutTheLine(t *testing.T) {
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindPath, D: "M0,5 L10,5", Fill: "none", Stroke: "ink", StrokeWidth: 2, FillOpacity: 1}), 10))
	for y, row := range a {
		for x, v := range row {
			on := y == 4 || y == 5
			if on && v != 255 || !on && v != 0 {
				t.Errorf("(%d,%d) = %d", x, y, v)
			}
		}
	}
}

// A closed outline's corners are mitred; a sharp one past the limit is
// bevelled.
func TestJoins(t *testing.T) {
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindPolygon, Points: square(2, 2, 8, 8), Fill: "none", Stroke: "ink", StrokeWidth: 2, FillOpacity: 1}), 10))
	for _, p := range [][2]int{{1, 1}, {8, 1}, {8, 8}, {1, 8}} {
		if v := a[p[1]][p[0]]; v != 255 {
			t.Errorf("corner (%d,%d) = %d, want mitred", p[0], p[1], v)
		}
	}
	if v := a[5][5]; v != 0 {
		t.Errorf("the middle is filled: %d", v)
	}

	// a spike: its miter would reach far beyond the limit, so it stops
	// at the bevel, a stroke's width from the tip
	spike := icon(ir.Shape{Kind: ir.KindPath, D: "M0,4 L100,5 L0,6", Fill: "none", Stroke: "ink", StrokeWidth: 1, FillOpacity: 1})
	spike.Viewbox = [4]float64{0, 0, 110, 10}
	a = alpha(paint(t, spike, 10))
	if v := a[5][103]; v != 0 {
		t.Errorf("past the tip: %d, want bevelled", v)
	}
	if v := a[5][98]; v == 0 {
		t.Error("the spike's end is not drawn")
	}
}

func TestDashes(t *testing.T) {
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindPath, D: "M0,5 L10,5", Fill: "none", Stroke: "ink", StrokeWidth: 2, FillOpacity: 1, Dash: dash("2 2")}), 10))
	for x, v := range a[4] {
		on := x%4 < 2
		if on && v != 255 || !on && v != 0 {
			t.Errorf("x=%d: %d", x, v)
		}
	}
	// an odd array repeats, as SVG's does
	if got := dashes("1 2 3", 1); len(got) != 6 {
		t.Errorf("%v", got)
	}
	for _, bad := range []string{"", "0 0", "1 x", "-1 2"} {
		if dashes(bad, 1) != nil {
			t.Errorf("%q drew dashes", bad)
		}
	}
}

func TestGroupsAndSizes(t *testing.T) {
	// a badge: the shapes moved to (5,5) and halved
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindGroup, TX: 5, TY: 5, Scale: 0.5, Shapes: []ir.Shape{
		{Kind: ir.KindPolygon, Points: square(0, 0, 10, 10), Fill: "ink", FillOpacity: 1, Stroke: "none"},
	}}), 10))
	if a[4][4] != 0 || a[5][5] != 255 || a[9][9] != 255 {
		t.Errorf("%v", a)
	}

	// the height asked for, the width in proportion, the viewbox's origin
	// where the image's is
	wide := &ir.Icon{Name: "w", Viewbox: [4]float64{-10, -5, 20, 10}, Shapes: []ir.Shape{
		{Kind: ir.KindPolygon, Points: square(-10, -5, -9, -4), Fill: "ink", FillOpacity: 1, Stroke: "none"},
	}}
	img := paint(t, wide, 30)
	if b := img.Bounds(); b.Dx() != 60 || b.Dy() != 30 {
		t.Errorf("%v", b)
	}
	if img.NRGBAAt(1, 1).A != 255 || img.NRGBAAt(4, 4).A != 0 {
		t.Error("the viewbox's origin is not the image's")
	}
	for _, h := range []int{0, MaxHeight + 1} {
		if _, err := Draw(wide, black, h); err == nil {
			t.Errorf("drew %d high", h)
		}
	}
}

func matrix(m ...float64) *[6]float64 {
	var out [6]float64
	copy(out[:], m)
	return &out
}

// IR version 2's groups: drawn through a matrix, cut to a clip outside
// it, their strokes scaled with it unless fixed.
func TestVersionTwoGroups(t *testing.T) {
	fill := func(pts [][2]float64) ir.Shape {
		return ir.Shape{Kind: ir.KindPolygon, Points: pts, Fill: "ink", FillOpacity: 1, Stroke: "none"}
	}
	// a quarter-turn, (x, y) to (10 - y, x): the top strip is the right one
	a := alpha(paint(t, icon(ir.Shape{Kind: ir.KindGroup, Matrix: matrix(0, 1, -1, 0, 10, 0), Shapes: []ir.Shape{
		fill(square(0, 0, 10, 2)),
	}}), 10))
	if a[5][9] != 255 || a[5][1] != 0 || a[0][5] != 0 {
		t.Errorf("turned: %v", a)
	}

	// a clip in the coordinates the group stands in, not those inside its
	// matrix: moved 5 right, cut at 8
	a = alpha(paint(t, icon(ir.Shape{Kind: ir.KindGroup, Matrix: matrix(1, 0, 0, 1, 5, 0), Clip: square(0, 0, 8, 10), Shapes: []ir.Shape{
		fill(square(0, 0, 10, 10)),
	}}), 10))
	if a[5][6] != 255 || a[5][9] != 0 || a[5][2] != 0 {
		t.Errorf("clipped: %v", a)
	}
	// a clip inside a clip is cut to both
	a = alpha(paint(t, icon(ir.Shape{Kind: ir.KindGroup, Matrix: matrix(1, 0, 0, 1, 0, 0), Clip: square(0, 0, 6, 10), Shapes: []ir.Shape{
		{Kind: ir.KindGroup, Matrix: matrix(1, 0, 0, 1, 0, 0), Clip: square(4, 0, 10, 10), Shapes: []ir.Shape{fill(square(0, 0, 10, 10))}},
	}}), 10))
	if a[5][5] != 255 || a[5][2] != 0 || a[5][7] != 0 {
		t.Errorf("clipped twice: %v", a)
	}

	// a version 1 badge around a version 2 group: the group's map first
	a = alpha(paint(t, icon(ir.Shape{Kind: ir.KindGroup, TX: 5, TY: 0, Scale: 0.5, Shapes: []ir.Shape{
		{Kind: ir.KindGroup, Matrix: matrix(1, 0, 0, 1, 0, 10), Shapes: []ir.Shape{fill(square(0, 0, 2, 2))}},
	}}), 10))
	if a[5][5] != 255 || a[0][5] != 0 {
		t.Errorf("composed: %v", a)
	}

	// a line 2 wide along y = 2, drawn three times as tall: its stroke too,
	// 6 wide about y = 6, unless fixed, when it is 2 pixels wide
	line := func(fixed string) *ir.Icon {
		return icon(ir.Shape{Kind: ir.KindGroup, Matrix: matrix(1, 0, 0, 3, 0, 0), Strokes: fixed, Shapes: []ir.Shape{
			{Kind: ir.KindPath, D: "M1,2 L9,2", Fill: "none", FillOpacity: 1, Stroke: "ink", StrokeWidth: 2},
		}})
	}
	rows := func(img *image.NRGBA, x int) (covered []int) {
		for y := 0; y < img.Bounds().Dy(); y++ {
			if img.NRGBAAt(x, y).A == 255 {
				covered = append(covered, y)
			}
		}
		return covered
	}
	if got := rows(paint(t, line(""), 10), 5); len(got) != 6 || got[0] != 3 {
		t.Errorf("scaled: rows %v", got)
	}
	if got := rows(paint(t, line("fixed"), 10), 5); len(got) != 2 || got[0] != 5 {
		t.Errorf("fixed: rows %v", got)
	}
	// ten times the size: a fixed stroke is still 2 pixels, as Chromium
	// draws it, or, for a label, ten times its width as written
	if got := rows(paint(t, line("fixed"), 100), 50); len(got) != 2 || got[0] != 59 {
		t.Errorf("fixed at 100: rows %v", got)
	}
	img, err := DrawWith(line("fixed"), black, 100, Options{ScaleFixedStrokes: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(img, 50); len(got) != 20 || got[0] != 50 {
		t.Errorf("fixed, scaled with the icon: rows %v", got)
	}
}

func TestCurvesAndRoundShapes(t *testing.T) {
	for _, s := range []ir.Shape{
		{Kind: ir.KindCircle, CX: 5, CY: 5, R: 4},
		{Kind: ir.KindEllipse, CX: 5, CY: 5, RX: 4, RY: 4},
		{Kind: ir.KindPath, D: "M1,5 A4,4 0 0 0 9,5 A4,4 0 0 0 1,5 Z"},
		{Kind: ir.KindRect, X: 1, Y: 1, W: 8, H: 8, RX: 4},
	} {
		s.Fill, s.FillOpacity, s.Stroke = "ink", 1, "none"
		a := alpha(paint(t, icon(s), 100))
		// a disc of radius 40 about (50,50): its middle and its edge's
		// inside covered, its bounding box's corners not
		if a[50][50] != 255 || a[50][12] != 255 || a[12][12] != 0 || a[88][88] != 0 {
			t.Errorf("%s: middle %d, edge %d, corners %d %d", s.Kind, a[50][50], a[50][12], a[12][12], a[88][88])
		}
	}
	if _, err := Draw(icon(ir.Shape{Kind: ir.KindPath, D: "M0,0 X", Fill: "ink", FillOpacity: 1}), black, 10); err == nil {
		t.Error("drew bad path data")
	}
}

// isolex's sample, every icon of it, drawn and written as a PNG; and
// isolex 0.4's, with its transforms and text, in IR version 2.
func TestTheSampleDraws(t *testing.T) {
	for _, file := range []string{
		"sample.ir.golden.json",
		filepath.Join("isolex-0.5.0", "sample.ir.golden.json"),
		filepath.Join("isolex-0.5.0", "transforms.ir.golden.json"),
		filepath.Join("isolex-0.5.0", "text.ir.golden.json"),
	} {
		drawsAll(t, file)
	}
}

func drawsAll(t *testing.T, file string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ir.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range doc.Icons {
		var b bytes.Buffer
		if err := Encode(&b, &doc.Icons[i], svg.Palette(svg.IsolexPalette), 96); err != nil {
			t.Fatalf("%s: %v", doc.Icons[i].Name, err)
		}
		encoded := b.Bytes()
		img, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		painted := 0
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					painted++
				}
			}
		}
		// a clipped icon may draw little; every other draws a twentieth
		least := bounds.Dx() * bounds.Dy() / 20
		if strings.HasPrefix(doc.Icons[i].Name, "clipped") || doc.IRVersion == 2 && strings.Contains(file, "text") {
			least = 1
		}
		if bounds.Dy() != 96 || painted < least {
			t.Errorf("%s: %v, %d painted", doc.Icons[i].Name, bounds, painted)
		}
		if os.Getenv("RASTER_DUMP") != "" {
			_ = os.WriteFile(filepath.Join(os.Getenv("RASTER_DUMP"), doc.Icons[i].Name+".png"), encoded, 0o644)
		}
	}
}

// An outline draws the strokes alone, every one in the ink, and no fill.
func TestOutline(t *testing.T) {
	i := icon(
		ir.Shape{Kind: ir.KindPolygon, Points: square(2, 2, 8, 8), Fill: "red", FillOpacity: 1, Stroke: "red", StrokeWidth: 2},
		ir.Shape{Kind: ir.KindPolygon, Points: square(0, 0, 1, 1), Fill: "red", FillOpacity: 1, Stroke: "none"},
	)
	img, err := DrawWith(i, black, 10, Options{Outline: true, Ink: color.NRGBA{B: 255, A: 255}})
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(2, 5); c != (color.NRGBA{B: 255, A: 255}) {
		t.Errorf("stroke: %+v", c)
	}
	if c := img.NRGBAAt(5, 5); c.A != 0 {
		t.Errorf("filled: %+v", c)
	}
	if c := img.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("a fill with no stroke: %+v", c)
	}
}
