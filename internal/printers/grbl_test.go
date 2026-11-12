package printers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl/grblsim"
)

func grblSim(t *testing.T, f func(*grblsim.Sim)) *grblsim.Sim {
	t.Helper()
	timing := grblTiming
	grblTiming = grbl.Timing{Boot: 500 * time.Millisecond, Answer: time.Second, Hold: time.Second}
	sim := grblsim.New()
	if f != nil {
		sim.Set(f)
	}
	t.Cleanup(func() {
		CloseGRBL()
		sim.Close()
		grblTiming = timing
	})
	return sim
}

func grblMachineAt(t *testing.T, addr, options string) (Machine, OneShotModel) {
	t.Helper()
	s := Settings{Model: GRBLDiode10W}
	if options != "" {
		s.Options = json.RawMessage(options)
	}
	c := Connection{Type: ConnTCP, Address: addr}
	if err := Validate("Diode", DriverGRBL, c, s); err != nil {
		t.Fatal(err)
	}
	m, err := MachineFor(DriverGRBL, c, s)
	if err != nil {
		t.Fatal(err)
	}
	model, err := OneShotModelOf(DriverGRBL, s)
	if err != nil {
		t.Fatal(err)
	}
	return m, model
}

func TestTheGRBLDriverDescribesItsLasers(t *testing.T) {
	if !IsOneShot(DriverGRBL) {
		t.Fatal("grbl is not a one-shot driver")
	}
	for _, m := range (grblDriver{}).OneShotModels() {
		if m.StartButton || !m.Frames || !slices.Equal(m.Interlocks, []string{InterlockMachine}) {
			t.Errorf("%s: %+v", m.ID, m)
		}
		for _, mat := range m.Materials {
			if _, err := ValidateMaterial(mat); err != nil {
				t.Errorf("%s %s: %v", m.ID, mat.ID, err)
			}
		}
	}
	// The work area is the printer's, from its options.
	m, err := OneShotModelOf(DriverGRBL, Settings{Model: GRBLDiode20W, Options: json.RawMessage(`{"work_width_mm":430,"work_height_mm":390}`)})
	if err != nil || m.WorkWidthMM != 430 || m.WorkHeightMM != 390 {
		t.Errorf("sized: %+v %v", m, err)
	}
	if m, _ := OneShotModelOf(DriverGRBL, Settings{Model: GRBLDiode20W}); m.WorkWidthMM != 400 {
		t.Errorf("default: %+v", m)
	}
	conn := Connection{Type: ConnTCP, Address: "192.0.2.7"}
	for _, bad := range []string{`{"work_width_mm":0}`, `{"origin":"middle"}`, `{"baud":1234}`, `{"rx_buffer":8}`, `{"frame_power":50}`, `{"frame_speed":0}`, `{"max_power_s":-1}`, `{"gap_check":true}`} {
		if err := Validate("x", DriverGRBL, conn, Settings{Model: GRBLDiode10W, Options: json.RawMessage(bad)}); err == nil {
			t.Errorf("%s was taken", bad)
		}
	}
	if err := Validate("x", DriverGRBL, conn, Settings{Model: GRBLDiode10W, Options: json.RawMessage(`{"origin":"top_right","baud":230400,"rx_buffer":1024,"frame_power":1,"frame_speed":80,"max_power_s":255}`)}); err != nil {
		t.Error(err)
	}
	if err := Validate("x", DriverGRBL, Connection{Type: ConnIPP, Address: "ipp://x/y"}, Settings{Model: GRBLDiode10W}); err != nil {
		t.Error(err) // refused when reached, below
	}
	if _, err := MachineFor(DriverGRBL, Connection{Type: ConnIPP, Address: "ipp://x/y"}, Settings{Model: GRBLDiode10W}); err == nil {
		t.Error("an ipp connection was taken")
	}
	if _, _, err := PlanLabel(DriverGRBL, Settings{Model: GRBLDiode10W}, LabelSpec{WidthMM: 20, HeightMM: 10, DPI: 254}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("printing to a GRBL laser: %v", err)
	}
}

func TestAGRBLLaserRunsAJobFromStartToFinish(t *testing.T) {
	ctx := context.Background()
	sim := grblSim(t, func(s *grblsim.Sim) { s.RX, s.LineTime = 64, 100*time.Microsecond })
	m, model := grblMachineAt(t, sim.Addr(), `{"rx_buffer":64,"frame_power":1}`)
	job := xtoolJob(t, model)

	st, err := m.Status(ctx)
	if err != nil || st.State != MachineIdle || len(st.Problems()) != 0 {
		t.Fatalf("status %+v %v", st, err)
	}

	// Framing traces the outline at frame_power: 1% of S1000.
	if err := m.Frame(ctx, job); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineIdle)
	framed := sim.Lines()
	if !slices.ContainsFunc(framed, endsWith("S10")) || slices.ContainsFunc(framed, endsWith("S400")) {
		t.Fatalf("framed:\n%s", strings.Join(framed, "\n"))
	}

	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineDone)
	lines := sim.Lines()[len(framed):]
	want, err := grbl.GCode(grblProgram(grbl.Bed{WidthMM: 400, HeightMM: 400, FlipY: true, SMax: 1000}, job))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, "\n") + "\n"; got != string(want) {
		t.Errorf("the controller ran %d lines, not the job's %d", len(lines), strings.Count(string(want), "\n"))
	}
	if sim.Overflowed() {
		t.Error("the controller's buffer was overfilled")
	}
	// Basswood engraves at 40% of S1000.
	if !slices.ContainsFunc(lines, endsWith("S400")) {
		t.Errorf("no engraving at 40%%:\n%s", strings.Join(lines, "\n"))
	}
}

func TestAGRBLLaserIsPausedAndCancelled(t *testing.T) {
	ctx := context.Background()
	sim := grblSim(t, func(s *grblsim.Sim) { s.LineTime = 2 * time.Millisecond })
	m, model := grblMachineAt(t, sim.Addr(), "")
	if err := m.Start(ctx, xtoolJob(t, model)); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineRunning)
	if err := m.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachinePaused)
	if err := m.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineRunning)
	if err := m.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineIdle)
	if st, _ := m.Status(ctx); len(st.Problems()) != 0 {
		t.Errorf("after cancelling: %v", st.Problems())
	}
}

func TestAGRBLLaserRefusesWhatGRBLReports(t *testing.T) {
	ctx := context.Background()
	sim := grblSim(t, nil)
	m, model := grblMachineAt(t, sim.Addr(), "")
	job := xtoolJob(t, model)

	sim.SetState("Door:0")
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p := st.Problems(); len(p) != 1 || !strings.Contains(p[0], "door is open") {
		t.Errorf("door: %v", p)
	}
	sim.SetState("Alarm")
	if err := m.Start(ctx, job); err == nil || !strings.Contains(err.Error(), "alarm") {
		t.Errorf("started in alarm: %v", err)
	}
	sim.SetState("Idle")

	// Not in laser mode: refused, the setting read again before starting.
	sim.Set(func(s *grblsim.Sim) { s.Settings[32] = "0" })
	if err := m.Start(ctx, job); err == nil || !strings.Contains(err.Error(), "$32=0") {
		t.Errorf("started out of laser mode: %v", err)
	}
	if st, _ := m.Status(ctx); len(st.Problems()) == 0 {
		t.Error("laser mode off is not a problem")
	}
	sim.Set(func(s *grblsim.Sim) {
		s.Settings[32] = "1"
		delete(s.Settings, 30)
	})
	if err := m.Start(ctx, job); err == nil || !strings.Contains(err.Error(), "max_power_s") {
		t.Errorf("started with no full power: %v", err)
	}
	if n := len(sim.Lines()); n != 0 {
		t.Fatalf("%d lines were run while refused", n)
	}
	// The option gives it.
	m, _ = grblMachineAt(t, sim.Addr(), `{"max_power_s":255}`)
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineDone)
	if !slices.ContainsFunc(sim.Lines(), endsWith("S102")) { // 40% of 255
		t.Errorf("not at S255's scale:\n%s", strings.Join(sim.Lines(), "\n"))
	}
}

func TestAGRBLJobRefusedPartWayFails(t *testing.T) {
	ctx := context.Background()
	sim := grblSim(t, func(s *grblsim.Sim) {
		s.Refuse = func(line string) int {
			if strings.HasSuffix(line, "S1000") {
				return 20
			}
			return 0
		}
	})
	m, model := grblMachineAt(t, sim.Addr(), "")
	if err := m.Start(ctx, xtoolJob(t, model)); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineFailed)
	if st, _ := m.Status(ctx); !strings.Contains(st.Message, "error:20") {
		t.Errorf("message %q", st.Message)
	}
}

func TestAGRBLConnectionIsKeptAndReopened(t *testing.T) {
	ctx := context.Background()
	sim := grblSim(t, nil)
	m, _ := grblMachineAt(t, sim.Addr(), "")
	for range 3 {
		if _, err := m.Status(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if sim.Connections() != 1 {
		t.Errorf("%d connections", sim.Connections())
	}
	CloseGRBL()
	if _, err := m.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if sim.Connections() != 2 {
		t.Errorf("%d connections after closing", sim.Connections())
	}
}

func endsWith(suffix string) func(string) bool {
	return func(s string) bool { return strings.HasSuffix(s, suffix) }
}
