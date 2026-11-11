package printers

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/simcamera"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// TestAnchorPutsTheLabelInACorner: centered, or in each corner, inset.
func TestAnchorPutsTheLabelInACorner(t *testing.T) {
	for _, c := range []struct {
		corner string
		want   PointMM
	}{
		{"", PointMM{X: 5, Y: 10}},
		{labels.CornerCenter, PointMM{X: 5, Y: 10}},
		{labels.CornerTopLeft, PointMM{X: 2, Y: 2}},
		{labels.CornerTopRight, PointMM{X: 8, Y: 2}},
		{labels.CornerBottomLeft, PointMM{X: 2, Y: 18}},
		{labels.CornerBottomRight, PointMM{X: 8, Y: 18}},
	} {
		inset := 2.0
		if c.corner == "" || c.corner == labels.CornerCenter {
			inset = 0
		}
		if got := AnchorAt(labels.WorkpieceAnchor{Corner: c.corner, InsetMM: inset}, 60, 40, 50, 20); got != c.want {
			t.Errorf("%q: %+v, want %+v", c.corner, got, c.want)
		}
	}
}

func near(a, b PointMM) bool { return math.Hypot(a.X-b.X, a.Y-b.Y) < 1e-6 }

// TestMakeJobTurnsTheLabelWithTheWorkpiece: a workpiece turned on the bed
// carries its label, anchored in a corner, and its cut outline, turned with
// it; the engraving is turned within the square around them.
func TestMakeJobTurnsTheLabelWithTheWorkpiece(t *testing.T) {
	model := testModel(t, TestLaserPanel)
	birch := model.Materials[0]
	in := JobInput{Material: birch, Workpiece: RectMM{X: 60, Y: 50, Width: 60, Height: 40}, AngleDeg: 90,
		Anchor:       labels.WorkpieceAnchor{Corner: labels.CornerTopLeft, InsetMM: 2},
		LabelWidthMM: 50, LabelHeightMM: 20, Engrave: labelImage(500, 200),
		Cuts: []Polyline{{Points: []PointMM{{0, 0}, {50, 0}, {50, 20}, {0, 20}}, Closed: true}}}
	job, _, err := MakeJob(model, in)
	if err != nil {
		t.Fatal(err)
	}
	if job.Placed != (RectMM{X: 62, Y: 52, Width: 50, Height: 20}) || job.AngleDeg != 90 {
		t.Errorf("placed at %+v, turned %g", job.Placed, job.AngleDeg)
	}
	// A quarter turn clockwise about the piece's center, 90, 70: the
	// label's top left corner, 62, 52, goes to 108, 42.
	if got := job.Cuts[0].Points[0]; !near(got, PointMM{X: 108, Y: 42}) {
		t.Errorf("the cut's first corner is at %+v", got)
	}
	// The engraving is the label turned: 200 dots across, 500 down, its
	// black top left dot now at the top right.
	b := job.Engrave.Bounds()
	if b.Dx() != 200 || b.Dy() != 500 {
		t.Fatalf("engraving %v", b)
	}
	if job.Engrave.Pix[b.Dx()-1] != 0 || job.Engrave.Pix[0] != 0xff {
		t.Error("the engraving is not turned a quarter clockwise")
	}
	if math.Abs(job.Label.X-88) > 0.01 || math.Abs(job.Label.Y-42) > 0.01 || math.Abs(job.Label.Width-20) > 0.01 || math.Abs(job.Label.Height-50) > 0.01 {
		t.Errorf("the engraving covers %+v", job.Label)
	}

	// Turned, a piece that fits square may not.
	in.Workpiece = RectMM{X: 1, Y: 50, Width: 120, Height: 40}
	in.AngleDeg = 30
	if _, _, err := MakeJob(model, in); !errors.Is(err, ErrDoesNotFitWorkpiece) {
		t.Errorf("err = %v, want it to reach outside the work area", err)
	}
}

// TestCalibrationJobEngravesTheMarks: dots where the layout puts them,
// never mirrored, only on a laser with a camera.
func TestCalibrationJobEngravesTheMarks(t *testing.T) {
	model := testModel(t, TestLaserPanel)
	acrylic := model.Materials[1]
	job, layout, err := CalibrationJob(model, acrylic)
	if err != nil {
		t.Fatal(err)
	}
	if !job.Calibration || job.Mirror || job.Material.FromBack || len(job.Cuts) != 0 {
		t.Errorf("job %+v", job)
	}
	if len(layout.Marks) != 9 || layout.Marks[0] == (vision.Point{}) {
		t.Fatalf("layout %+v", layout)
	}
	// Each mark's center is burned, and the middle between two is not.
	dot := func(p vision.Point) uint8 {
		b := job.Engrave.Bounds()
		x := int((p.X - job.Label.X) / job.Label.Width * float64(b.Dx()))
		y := int((p.Y - job.Label.Y) / job.Label.Height * float64(b.Dy()))
		return job.Engrave.GrayAt(x, y).Y
	}
	for _, m := range layout.Marks {
		if dot(m) != 0 {
			t.Errorf("mark %v is not burned", m)
		}
	}
	if dot(vision.Point{X: (layout.Marks[0].X + layout.Marks[1].X) / 2, Y: layout.Marks[0].Y}) != 0xff {
		t.Error("between marks is burned")
	}
	if _, _, err := CalibrationJob(testModel(t, TestLaserButton), acrylic); err == nil {
		t.Error("a laser without a camera made calibration marks")
	}
}

// TestTheSimulatedLaserPhotographsItsBed: the marks once after engraving
// them, then the workpiece, unless a test chooses.
func TestTheSimulatedLaserPhotographsItsBed(t *testing.T) {
	RegisterTestLaser()
	ctx := context.Background()
	m, err := MachineFor(DriverTestLaser, Connection{Type: ConnTCP, Address: "sim-camera.test"}, Settings{Model: TestLaserPanel})
	if err != nil {
		t.Fatal(err)
	}
	cam := m.(Camera)
	job, _, err := CalibrationJob(testModel(t, TestLaserPanel), testLaserMaterials()[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{simcamera.Calibration, simcamera.Workpiece} {
		got, err := cam.Photo(ctx)
		if err != nil || string(got) != string(simcamera.Photo(want)) {
			t.Errorf("photo %s: %v", want, err)
		}
	}
	sim := TestLaserAt("sim-camera.test:9100")
	sim.SetPhoto([]byte("chosen"))
	if got, _ := cam.Photo(ctx); string(got) != "chosen" {
		t.Errorf("photo %q", got)
	}
	if _, err := (&testLaserMachine{sim: sim, model: testModel(t, TestLaserButton)}).Photo(ctx); err == nil {
		t.Error("the button model took a photo")
	}
}
