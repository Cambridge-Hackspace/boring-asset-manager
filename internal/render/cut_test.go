package render

import (
	"math"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// tag is a 50 × 20 mm tag: its name, its outline with rounded corners and a
// hole to hang it by, both cut.
const tag = `{"name":"Tag","units":"mm","width":50,"height":20,"dpi":300,"elements":[
  {"type":"text","x":12,"y":4,"width":34,"height":12,"font_size":14,"content_binding":"{{asset.name}}"},
  {"type":"shape","shape":"rectangle","x":0,"y":0,"width":50,"height":20,"corner_radius":3,"stroke":"black","stroke_width":0.2,"fill":"none","cut":true},
  {"type":"shape","shape":"ellipse","x":3,"y":7,"width":6,"height":6,"stroke":"black","stroke_width":0.2,"fill":"none","cut":true}]}`

func TestCutOutlinesBecomeVectorPaths(t *testing.T) {
	tpl, err := labels.Parse([]byte(tag))
	if err != nil {
		t.Fatal(err)
	}
	paths := CutPaths(tpl)
	if len(paths) != 2 || !paths[0].Closed || !paths[1].Closed {
		t.Fatalf("paths: %d", len(paths))
	}
	// The outline keeps to its box, its corners rounded within tolerance.
	for _, p := range paths[0].Points {
		if p[0] < -1e-9 || p[0] > 50+1e-9 || p[1] < -1e-9 || p[1] > 20+1e-9 {
			t.Errorf("outline point %v outside the tag", p)
		}
	}
	// The hole is a circle of radius 3 about (6, 10), followed within the
	// tolerance: smooth, not a polygon of a few sides.
	hole := paths[1].Points
	if len(hole) < 24 {
		t.Errorf("the hole is %d straight pieces", len(hole))
	}
	for i, p := range hole {
		if r := math.Hypot(p[0]-6, p[1]-10); math.Abs(r-3) > 1e-9 {
			t.Errorf("hole point %v is %v from the center", p, r)
		}
		q := hole[(i+1)%len(hole)]
		mid := math.Hypot((p[0]+q[0])/2-6, (p[1]+q[1])/2-10)
		if 3-mid > cutTolerance+1e-9 {
			t.Errorf("piece %d strays %v mm from the circle", i, 3-mid)
		}
	}
}

func TestCutPathsAreTurnedMirroredAndInMillimeters(t *testing.T) {
	tpl, err := labels.Parse([]byte(`{"name":"T","units":"in","width":2,"height":1,"dpi":300,"print_mirror":"across","elements":[
  {"type":"shape","shape":"line","x":0,"y":0.5,"width":1,"height":0,"stroke":"black","stroke_width":0.01,"rotation":90,"cut":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	paths := CutPaths(tpl)
	if len(paths) != 1 || paths[0].Closed {
		t.Fatalf("paths %+v", paths)
	}
	// A level inch-long line turned upright about its middle, (0.5, 0.5),
	// then mirrored across the 2-inch label: x = 1.5 in, from 0 to 1 in.
	got := paths[0].Points
	want := [][2]float64{{1.5 * 25.4, 0}, {1.5 * 25.4, 25.4}}
	for i := range want {
		if math.Abs(got[i][0]-want[i][0]) > 1e-9 || math.Abs(got[i][1]-want[i][1]) > 1e-9 {
			t.Errorf("point %d: %v, want %v", i, got[i], want[i])
		}
	}
}

func TestATemplateRendersToAnEngravingAtAMaterialsResolution(t *testing.T) {
	tpl, err := labels.Parse([]byte(tag))
	if err != nil {
		t.Fatal(err)
	}
	engraved := *WithoutCuts(tpl)
	if len(engraved.Elements) != 1 || len(tpl.Elements) != 3 {
		t.Fatalf("WithoutCuts kept %d of %d elements, and left the template %d", len(engraved.Elements), 3, len(tpl.Elements))
	}
	engraved.DPI = 254
	scene, err := Layout(&engraved, labels.Data{AssetName: "Drill"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := scene.Raster(true)
	// 50 × 20 mm at 254 lines an inch is 500 × 200 dots, every one black or
	// white, and the outline is not among them: the corners stay white.
	if b := img.Bounds(); b.Dx() != 500 || b.Dy() != 200 {
		t.Fatalf("engraving is %v", b)
	}
	black := 0
	for _, v := range img.Pix {
		if v != 0 && v != 0xFF {
			t.Fatalf("a dot of gray %d", v)
		}
		if v == 0 {
			black++
		}
	}
	if black == 0 || img.Pix[0] != 0xFF || img.Pix[len(img.Pix)-1] != 0xFF {
		t.Errorf("black dots %d, corners %d %d", black, img.Pix[0], img.Pix[len(img.Pix)-1])
	}
	// On paper, the outline prints as designed.
	full, err := Layout(tpl, labels.Data{AssetName: "Drill"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if paper := full.Raster(true); paper.Pix[paper.Stride*paper.Rect.Dy()/2] != 0 {
		t.Error("the cut outline is not drawn on a printer")
	}
}

func TestCutIsOnlyForShapes(t *testing.T) {
	if _, err := labels.Parse([]byte(`{"name":"T","units":"mm","width":50,"height":20,"dpi":300,"elements":[
  {"type":"text","x":1,"y":1,"width":30,"height":10,"font_size":10,"content_binding":"x","cut":true}]}`)); err == nil {
		t.Error("a text was marked cut")
	}
}
