package printers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool/xtoolsim"
)

func xtoolModel(t *testing.T, id string) OneShotModel {
	t.Helper()
	m, err := OneShotModelOf(DriverXTool, Settings{Model: id})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// xtoolJob is a job of a 20 × 10 mm label with a cut outline, on a 30 ×
// 20 mm workpiece at 10, 10 mm.
func xtoolJob(t *testing.T, model OneShotModel) *Job {
	t.Helper()
	material := model.Materials[slices.IndexFunc(model.Materials, func(m Material) bool { return m.Cut != nil })]
	w, h := LabelDots(20, 10, material.Engrave.LPI)
	job, _, err := MakeJob(model, JobInput{
		Material: material, Workpiece: RectMM{X: 10, Y: 10, Width: 30, Height: 20}, LabelWidthMM: 20, LabelHeightMM: 10,
		Engrave: labelImage(w, h), Cuts: []Polyline{{Points: []PointMM{{0, 0}, {20, 0}, {20, 10}, {0, 10}}, Closed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func xtoolMachineAt(t *testing.T, model, addr string, options string) Machine {
	t.Helper()
	s := Settings{Model: model}
	if options != "" {
		s.Options = json.RawMessage(options)
	}
	if err := Validate("xTool", DriverXTool, Connection{Type: ConnTCP, Address: addr}, s); err != nil {
		t.Fatal(err)
	}
	m, err := MachineFor(DriverXTool, Connection{Type: ConnTCP, Address: addr}, s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func xtoolSim(t *testing.T, firmware string) *xtoolsim.V2 {
	t.Helper()
	sim := xtoolsim.NewV2(firmware)
	t.Cleanup(func() {
		xtool.CloseV2()
		sim.Close()
	})
	return sim
}

func TestTheXToolDriverDescribesItsLasers(t *testing.T) {
	if !IsOneShot(DriverXTool) {
		t.Fatal("xtool is not a one-shot driver")
	}
	f1 := xtoolModel(t, xtool.ModelF1)
	if f1.WorkWidthMM != 115 || f1.WorkHeightMM != 115 || !f1.StartButton || !f1.Frames {
		t.Errorf("F1: %+v", f1)
	}
	for _, m := range (xtoolDriver{}).OneShotModels() {
		for _, mat := range m.Materials {
			if _, err := ValidateMaterial(mat); err != nil {
				t.Errorf("%s %s: %v", m.ID, mat.ID, err)
			}
		}
		if !slices.Contains(m.Interlocks, InterlockFirmware) || !slices.Contains(m.Interlocks, InterlockLid) {
			t.Errorf("%s interlocks %v", m.ID, m.Interlocks)
		}
	}
	// A V1 model frames nothing, and is started by BAM!.
	if v1 := xtoolModel(t, xtool.ModelF1V1); v1.Frames || v1.StartButton {
		t.Errorf("F1 on V1 firmware: %+v", v1)
	}
	if err := Validate("x", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: xtool.ModelF1, Options: json.RawMessage(`{"allow_unknown_firmware":true}`)}); err != nil {
		t.Error(err)
	}
	if err := Validate("x", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: xtool.ModelF1, Options: json.RawMessage(`{"gap_check":false}`)}); err == nil {
		t.Error("an unknown option was taken")
	}
	if err := Validate("x", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: xtool.ModelF1, Options: json.RawMessage(`{"flame_hold_minutes":0}`)}); err == nil {
		t.Error("no flame hold was taken")
	}
	if err := Validate("x", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: "F9"}); err == nil {
		t.Error("an unknown model was taken")
	}
	if err := Validate("x", DriverXTool, Connection{Type: ConnTCP, Address: "192.0.2.7"}, Settings{Model: xtool.ModelF1, Media: "62"}); err == nil {
		t.Error("a laser was given media")
	}
	if _, _, err := PlanLabel(DriverXTool, Settings{Model: xtool.ModelF1}, LabelSpec{WidthMM: 20, HeightMM: 10, DPI: 254}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("printing to an xTool: %v", err)
	}
}

func TestAnXToolOnWSV2RunsAJobFromStartToFinish(t *testing.T) {
	ctx := context.Background()
	sim := xtoolSim(t, "40.51.020.04")
	model := xtoolModel(t, xtool.ModelF1)
	m := xtoolMachineAt(t, xtool.ModelF1, sim.Addr(), "")
	job := xtoolJob(t, model)

	st, err := m.Status(ctx)
	if err != nil || st.State != MachineIdle || len(st.Problems()) != 0 {
		t.Fatalf("status %+v %v", st, err)
	}

	// Framing sends the outline, with the laser off, and runs it.
	if err := m.Frame(ctx, job); err != nil {
		t.Fatal(err)
	}
	frame := string(sim.File("tmpFrame.gcode"))
	if frame == "" || sim.Framed() != 1 {
		t.Fatalf("framed %d: %q", sim.Framed(), frame)
	}
	for _, m := range sValueOf(frame) {
		if m != "0" {
			t.Fatalf("the frame fires the laser:\n%s", frame)
		}
	}
	if st, _ = m.Status(ctx); st.State != MachineFraming {
		t.Fatalf("framing: %s", st.State)
	}
	sim.Finish()
	waitForState(t, m, MachineIdle)

	// Started: sent ready for the button, with the material's settings.
	framed := len(sim.Actions()) // the frame's start
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	gcode := string(sim.File("tmp.gcode"))
	engrave, cut := job.Material.Engrave, job.Material.Cut
	for _, want := range []string{
		"S" + strconv.Itoa(int(engrave.Power*10)),      // engraving power, out of 1000
		"F" + strconv.Itoa(int(engrave.Speed*60)),      // and speed, a minute
		"S" + strconv.Itoa(int(cut.Power*10)),          // cutting power
		"G1F" + strconv.Itoa(int(cut.Speed*60)) + "S0", // and speed
	} {
		if !strings.Contains(gcode, want) {
			t.Errorf("the job has no %s", want)
		}
	}
	if st, _ = m.Status(ctx); st.State != MachineReady {
		t.Fatalf("ready: %s", st.State)
	}
	if len(sim.Actions()) != framed {
		t.Fatalf("BAM! started a button job: %v", sim.Actions())
	}
	sim.PressButton()
	waitForState(t, m, MachineRunning)
	if err := m.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachinePaused)
	if err := m.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineRunning)
	sim.Finish()
	waitForState(t, m, MachineDone)

	// Another, cancelled.
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineIdle)
}

func TestAnXToolOnRESTV1IsStartedByBAM(t *testing.T) {
	ctx := context.Background()
	sim := xtoolsim.NewREST(xtool.DialectProcessing, "40.52.016.04")
	defer sim.Close()
	model := xtoolModel(t, xtool.ModelF1UltraV1)
	m := xtoolMachineAt(t, xtool.ModelF1UltraV1, sim.Addr(), "")
	job := xtoolJob(t, model)
	if err := m.Frame(ctx, job); err == nil {
		t.Error("a V1 machine framed")
	}
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	if sim.File("tmp.gcode") == nil {
		t.Fatal("no job sent")
	}
	waitForState(t, m, MachineRunning)
	sim.Finish()
	waitForState(t, m, MachineDone)
	if got := strings.Join(sim.Actions(), ","); got != "start" {
		t.Errorf("actions %s", got)
	}
}

func TestAnXToolWithAnInterlockOpenRefusesToStart(t *testing.T) {
	ctx := context.Background()
	sim := xtoolSim(t, "40.51.020.04")
	m := xtoolMachineAt(t, xtool.ModelF1, sim.Addr(), "")

	sim.Set(func(s *xtoolsim.V2) { s.Peripherals["gap"] = "off" })
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p := st.Problems(); len(p) != 1 || p[0] != "The lid is open" {
		t.Errorf("lid open: %v", p)
	}
	sim.Set(func(s *xtoolsim.V2) { s.Peripherals["gap"] = "on" })

	sim.Push("/fire/alarm", "FIRE_RECOGNITION", "FIRE", nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ = m.Status(ctx)
		if len(st.Problems()) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a flame alarm did not refuse starting")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.ContainsFunc(st.Interlocks, func(i Interlock) bool { return i.Name == InterlockFlame && !i.OK }) {
		t.Errorf("flame: %+v", st.Interlocks)
	}
}

func TestAnXToolWithUnknownFirmwareIsRefused(t *testing.T) {
	ctx := context.Background()
	// F1 Ultra firmware, on a printer set up as an F1.
	sim := xtoolSim(t, "40.52.016.05")
	model := xtoolModel(t, xtool.ModelF1)
	job := xtoolJob(t, model)
	m := xtoolMachineAt(t, xtool.ModelF1, sim.Addr(), "")

	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p := st.Problems(); len(p) != 1 || !strings.Contains(p[0], "40.52.016.05") || !strings.Contains(p[0], "allow") {
		t.Errorf("problems %v", p)
	}
	if err := m.Start(ctx, job); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("started: %v", err)
	}
	if err := m.Frame(ctx, job); err == nil {
		t.Error("framed")
	}
	if sim.File("tmp.gcode") != nil || sim.File("tmpFrame.gcode") != nil {
		t.Fatal("something was sent")
	}

	// Allowed on the printer, it runs, and says so.
	m = xtoolMachineAt(t, xtool.ModelF1, sim.Addr(), `{"allow_unknown_firmware":true}`)
	st, err = m.Status(ctx)
	if err != nil || len(st.Problems()) != 0 || !strings.Contains(st.Message, "allows it") {
		t.Fatalf("allowed: %+v %v", st, err)
	}
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
}

func TestXToolStatesAreMachineStates(t *testing.T) {
	for _, s := range []string{xtool.StateIdle, xtool.StateFraming, xtool.StateReady, xtool.StateRunning, xtool.StatePaused, xtool.StateDone, xtool.StateFailed} {
		if xtoolStates[s] != s {
			t.Errorf("%s is %q", s, xtoolStates[s])
		}
	}
	for _, r := range []string{xtool.ReadingLid, xtool.ReadingSafetyKey, xtool.ReadingDrawer, xtool.ReadingFlame, xtool.ReadingMachine} {
		if InterlockName(r) == r {
			t.Errorf("no name for the interlock %s", r)
		}
	}
}

func waitForState(t *testing.T, m Machine, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := m.Status(context.Background())
		if err == nil && st.State == state {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %q, %v; want %s", st.State, err, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sValueOf is every S value in G-code.
func sValueOf(gcode string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(gcode, func(r rune) bool { return r == '\n' }) {
		if i := strings.Index(f, "S"); i >= 0 {
			j := i + 1
			for j < len(f) && f[j] >= '0' && f[j] <= '9' {
				j++
			}
			out = append(out, f[i+1:j])
		}
	}
	return out
}
