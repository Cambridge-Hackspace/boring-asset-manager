package printers

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"
)

func testModel(t *testing.T, id string) OneShotModel {
	t.Helper()
	for _, m := range (testLaser{}).OneShotModels() {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no model %s", id)
	return OneShotModel{}
}

// labelImage is a 1-bit label of w × h dots with its top left dot black.
func labelImage(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	img.Pix[0] = 0
	return img
}

func TestMakeJobPlacesTheLabelOnTheWorkpiece(t *testing.T) {
	model := testModel(t, TestLaserPanel)
	birch := model.Materials[0]
	in := JobInput{Material: birch, Workpiece: RectMM{X: 10, Y: 20, Width: 60, Height: 40}, LabelWidthMM: 50, LabelHeightMM: 20,
		Engrave: labelImage(500, 200), Cuts: []Polyline{{Points: []PointMM{{0, 0}, {50, 0}, {50, 20}, {0, 20}}, Closed: true}}}
	job, warnings, err := MakeJob(model, in)
	if err != nil {
		t.Fatal(err)
	}
	// Centered on the workpiece, and the outline moved with it.
	if job.Label != (RectMM{X: 15, Y: 30, Width: 50, Height: 20}) {
		t.Errorf("label at %+v", job.Label)
	}
	if got := job.Cuts[0].Points[2]; got != (PointMM{65, 50}) {
		t.Errorf("cut corner at %+v", got)
	}
	if job.Mirror || len(warnings) != 0 {
		t.Errorf("mirror %v, warnings %v", job.Mirror, warnings)
	}
	if o := job.Outline(); o != job.Label {
		t.Errorf("outline %+v", o)
	}

	// Placed by hand.
	in.LabelAt = &PointMM{X: 2, Y: 3}
	if job, _, err = MakeJob(model, in); err != nil || job.Label.X != 12 || job.Label.Y != 23 {
		t.Errorf("placed: %+v %v", job, err)
	}
}

func TestMakeJobMirrorsMaterialsEngravedFromTheBack(t *testing.T) {
	model := testModel(t, TestLaserPanel)
	acrylic := model.Materials[1]
	if !acrylic.FromBack {
		t.Fatal("expected clear acrylic to be engraved from the back")
	}
	job, warnings, err := MakeJob(model, JobInput{Material: acrylic, Workpiece: RectMM{Width: 50, Height: 20}, LabelWidthMM: 50, LabelHeightMM: 20,
		Engrave: labelImage(500, 200), Cuts: []Polyline{{Points: []PointMM{{1, 1}, {10, 1}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !job.Mirror || job.Engrave.Pix[0] != 0xFF || job.Engrave.Pix[499] != 0 {
		t.Errorf("not mirrored: mirror %v, first row ends %d %d", job.Mirror, job.Engrave.Pix[0], job.Engrave.Pix[499])
	}
	if got := job.Cuts[0].Points; got[0] != (PointMM{49, 1}) || got[1] != (PointMM{40, 1}) {
		t.Errorf("cut not mirrored: %+v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "face down") {
		t.Errorf("warnings %v", warnings)
	}
}

func TestMakeJobRefusesWhatDoesNotFit(t *testing.T) {
	model := testModel(t, TestLaserButton) // 115 × 115 mm
	birch := model.Materials[0]
	for name, in := range map[string]JobInput{
		"workpiece off the bed": {Workpiece: RectMM{X: 100, Width: 30, Height: 30}, LabelWidthMM: 10, LabelHeightMM: 10},
		"label too big":         {Workpiece: RectMM{Width: 30, Height: 30}, LabelWidthMM: 40, LabelHeightMM: 10},
		"label off the piece":   {Workpiece: RectMM{Width: 30, Height: 30}, LabelWidthMM: 20, LabelHeightMM: 10, LabelAt: &PointMM{X: 15}},
		"no workpiece":          {LabelWidthMM: 20, LabelHeightMM: 10},
		"nothing to do":         {Workpiece: RectMM{Width: 30, Height: 30}, LabelWidthMM: 20, LabelHeightMM: 10, Engrave: image.NewGray(image.Rect(0, 0, 0, 0))},
	} {
		in.Material = birch
		if in.Engrave == nil && name != "nothing to do" {
			in.Engrave = labelImage(10, 10)
		}
		if _, _, err := MakeJob(model, in); !errors.Is(err, ErrDoesNotFitWorkpiece) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMakeJobLeavesCutsUncutOnAMaterialNotCut(t *testing.T) {
	model := testModel(t, TestLaserPanel)
	aluminum := model.Materials[2]
	job, warnings, err := MakeJob(model, JobInput{Material: aluminum, Workpiece: RectMM{Width: 50, Height: 20}, LabelWidthMM: 50, LabelHeightMM: 20,
		Engrave: labelImage(10, 10), Cuts: []Polyline{{Points: []PointMM{{1, 1}, {10, 1}}}}})
	if err != nil || len(job.Cuts) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "uncut") {
		t.Errorf("job %+v, warnings %v, err %v", job, warnings, err)
	}
}

func TestValidateMaterial(t *testing.T) {
	good := Material{ID: "oak-6mm", Name: " Oak, 6 mm ", Engrave: EngraveSetting{Power: 40, Speed: 100, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 4, Passes: 3}}
	if m, err := ValidateMaterial(good); err != nil || m.Name != "Oak, 6 mm" {
		t.Fatalf("%+v %v", m, err)
	}
	for name, change := range map[string]func(*Material){
		"id":     func(m *Material) { m.ID = "Oak 6" },
		"name":   func(m *Material) { m.Name = " " },
		"power":  func(m *Material) { m.Engrave.Power = 101 },
		"speed":  func(m *Material) { m.Engrave.Speed = 0 },
		"passes": func(m *Material) { m.Cut = &CutSetting{Power: 10, Speed: 1, Passes: 0} },
		"lpi":    func(m *Material) { m.Engrave.LPI = 10 },
	} {
		m := good
		change(&m)
		if _, err := ValidateMaterial(m); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestOneShotPrintersAreNeverPrintedTo(t *testing.T) {
	RegisterTestLaser()
	s := Settings{Model: TestLaserPanel}
	if err := Validate("Laser", DriverTestLaser, Connection{Type: ConnTCP, Address: "laser.test"}, s); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PlanLabel(DriverTestLaser, s, LabelSpec{WidthMM: 50, HeightMM: 20, DPI: 300}); !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "one piece a job") {
		t.Errorf("planning a print: %v", err)
	}
	// It has no roll, and is never reached through a relay.
	if err := Validate("Laser", DriverTestLaser, Connection{Type: ConnTCP, Address: "laser.test"}, Settings{Model: TestLaserPanel, Media: "62"}); err == nil {
		t.Error("a laser took media")
	}
	if err := Validate("Laser", DriverTestLaser, Connection{Type: ConnTCP, Address: "laser.test", Relay: "shed"}, s); err == nil {
		t.Error("a laser took a relay")
	}
	// Its labels fit its work area, at any resolution.
	g, err := GeometryOf(DriverTestLaser, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FitLabel(g, LabelSpec{WidthMM: 190, HeightMM: 20, DPI: 72, Inks: []string{"red"}}); err != nil {
		t.Errorf("fit: %v", err)
	}
	if _, err := FitLabel(g, LabelSpec{WidthMM: 210, HeightMM: 20, DPI: 72}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("too big: %v", err)
	}
	found := false
	for _, d := range Drivers() {
		if d.Driver == DriverTestLaser {
			found = len(d.OneShot) == 2 && d.OneShot[0].WorkWidthMM == 115
		}
	}
	if !found {
		t.Error("the drivers list does not describe the test laser's models")
	}
}

func TestTheSimulatedLaserRunsAJob(t *testing.T) {
	RegisterTestLaser()
	ctx := context.Background()
	m, err := MachineFor(DriverTestLaser, Connection{Type: ConnTCP, Address: "sim-run.test"}, Settings{Model: TestLaserButton})
	if err != nil {
		t.Fatal(err)
	}
	sim := TestLaserAt("sim-run.test:9100")
	sim.SetDuration(time.Hour)
	job := &Job{Label: RectMM{Width: 10, Height: 10}}
	if err := m.Frame(ctx, job); err != nil || len(sim.Framed()) != 1 {
		t.Fatalf("frame: %v", err)
	}
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	st, _ := m.Status(ctx)
	if st.State != MachineReady || len(st.Interlocks) != 4 || len(st.Problems()) != 0 {
		t.Fatalf("after start: %+v", st)
	}
	sim.PressButton()
	if st, _ = m.Status(ctx); st.State != MachineRunning {
		t.Fatalf("after the button: %+v", st)
	}
	if err := m.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	sim.SetInterlock(InterlockLid, false, "The lid is open")
	if st, _ = m.Status(ctx); st.State != MachinePaused || len(st.Problems()) != 1 || st.Problems()[0] != "The lid is open" {
		t.Fatalf("paused, lid open: %+v", st)
	}
	_ = m.Cancel(ctx)
	if st, _ = m.Status(ctx); st.State != MachineCancelled {
		t.Fatalf("cancelled: %+v", st)
	}
}
