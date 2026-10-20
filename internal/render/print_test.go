package render

import (
	"image"
	"math"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// byHand is a template that prints mirrored or turned, done by hand
// instead: each element moved, mirrored and turned where the print would
// put it, and the template printing as designed.
func byHand(t *labels.Template) *labels.Template {
	out := *t
	out.PrintMirror, out.PrintRotation = "", 0
	across := t.PrintMirror == labels.MirrorAcross || t.PrintMirror == labels.MirrorBoth
	down := t.PrintMirror == labels.MirrorDown || t.PrintMirror == labels.MirrorBoth
	out.Elements = make([]labels.Element, len(t.Elements))
	for i, e := range t.Elements {
		x, y := t.Printed(e.X+e.Width/2, e.Y+e.Height/2)
		e.X, e.Y = x-e.Width/2, y-e.Height/2
		if across != down && e.Rotation != 0 {
			e.Rotation = 360 - e.Rotation
		}
		e.Rotation = math.Mod(e.Rotation+t.PrintRotation, 360)
		ea := e.Mirror == labels.MirrorAcross || e.Mirror == labels.MirrorBoth
		ed := e.Mirror == labels.MirrorDown || e.Mirror == labels.MirrorBoth
		e.Mirror = map[[2]bool]string{{true, false}: labels.MirrorAcross, {false, true}: labels.MirrorDown, {true, true}: labels.MirrorBoth}[[2]bool{ea != across, ed != down}]
		out.Elements[i] = e
	}
	return &out
}

// TestPrintMatchesByHand checks that a label printed mirrored and turned is
// the label with each of its elements mirrored, turned and moved where the
// print puts them: the same dots, but for where boxes round to other dots.
func TestPrintMatchesByHand(t *testing.T) {
	images := memImages{logoID: testLogo(), arrowID: testArrow()}
	for _, name := range []string{"mirrored", "print_turned", "mirrored_turned"} {
		tpl := mustTemplate(t, goldenTemplates[name])
		printed, err := Layout(tpl, testData, images)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		hand, err := Layout(byHand(tpl), testData, images)
		if err != nil {
			t.Fatalf("%s by hand: %v", name, err)
		}
		a, b := printed.Raster(true), hand.Raster(true)
		black, differ := 0, 0
		for i := range a.Pix {
			if a.Pix[i] == 0 || b.Pix[i] == 0 {
				black++
			}
			if a.Pix[i] != b.Pix[i] {
				differ++
			}
		}
		// Edges a dot apart differ; anything mirrored or turned the wrong
		// way differs nearly everywhere.
		if black == 0 || float64(differ) > 0.12*float64(black) {
			t.Errorf("%s: %d of %d dots differ from the label done by hand", name, differ, black)
		}
	}
}

// TestPrintedTurnsImagesOnTheirTurn checks that an image turned on the
// label keeps its turn as the whole label turns: turned 90° and then 330°
// as the label prints, it is the image turned 60°.
func TestPrintedTurnsImagesOnTheirTurn(t *testing.T) {
	pi := PlacedImage{X: 10, Y: 20, W: 40, H: 20, Image: testArrow()}
	sin, cos := labels.SinCos(90)
	pi.turn(90, 30, 30, func(p Point) Point {
		return Point{30 + (p.X-30)*cos - (p.Y-30)*sin, 30 + (p.X-30)*sin + (p.Y-30)*cos}
	})
	sin, cos = labels.SinCos(330)
	pi.turn(330, 50, 50, func(p Point) Point {
		return Point{50 + (p.X-50)*cos - (p.Y-50)*sin, 50 + (p.X-50)*sin + (p.Y-50)*cos}
	})
	if math.Abs(pi.Rotation-60) > 1e-9 {
		t.Errorf("rotation %g, want 60", pi.Rotation)
	}
	// Its center, (30, 30), turned 90° about (30, 30) stays there, then
	// goes 330° about (50, 50).
	wantX, wantY := 50+(-20)*cos-(-20)*sin, 50+(-20)*sin+(-20)*cos
	r := 60 * math.Pi / 180
	bx, by := 30.0, 30.0
	gotX := pi.Cx + (bx-pi.Cx)*math.Cos(r) - (by-pi.Cy)*math.Sin(r)
	gotY := pi.Cy + (bx-pi.Cx)*math.Sin(r) + (by-pi.Cy)*math.Cos(r)
	if math.Abs(gotX-wantX) > 1e-9 || math.Abs(gotY-wantY) > 1e-9 {
		t.Errorf("center lands at (%g, %g), want (%g, %g)", gotX, gotY, wantX, wantY)
	}

	// Turned back level, it is moved instead.
	pi.turn(300, 0, 0, func(p Point) Point {
		s, c := labels.SinCos(300)
		return Point{p.X*c - p.Y*s, p.X*s + p.Y*c}
	})
	if pi.Rotation != 0 {
		t.Errorf("turned back level, rotation %g", pi.Rotation)
	}
}

// TestMirrorKeepsCodesOnWholeDots checks that a mirrored code's modules stay
// rectangles of whole dots, mirrored within the box.
func TestMirrorKeepsCodesOnWholeDots(t *testing.T) {
	items := []Item{{Rects: []Rect{{X: 10, Y: 5, W: 2, H: 8}, {X: 14, Y: 5, W: 4, H: 8}}}}
	mirror(items, image.Rect(10, 5, 30, 13), true, false)
	want := []Rect{{X: 28, Y: 5, W: 2, H: 8}, {X: 22, Y: 5, W: 4, H: 8}}
	for i, r := range items[0].Rects {
		if r != want[i] {
			t.Errorf("rect %d: %+v, want %+v", i, r, want[i])
		}
	}
}
