package xtool_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool/xtoolsim"
)

func v2Device(t *testing.T, firmware string) (*xtool.V2, *xtoolsim.V2) {
	t.Helper()
	sim := xtoolsim.NewV2(firmware)
	t.Cleanup(func() {
		xtool.CloseV2()
		sim.Close()
	})
	return &xtool.V2{Addr: sim.Addr(), Timing: xtool.V2Timing{Request: 5 * time.Second, Heartbeat: 50 * time.Millisecond, PongTimeout: 2 * time.Second, Idle: time.Minute}}, sim
}

func reading(st xtool.Status, name string) (xtool.Reading, bool) {
	for _, r := range st.Readings {
		if r.Name == name {
			return r, true
		}
	}
	return xtool.Reading{}, false
}

// gcode is a job long enough to need several windows and packets.
func gcode() []byte {
	var b bytes.Buffer
	for i := range 2000 {
		b.WriteString("G1X")
		b.WriteString(strings.Repeat("1", i%7+1))
		b.WriteString("Y1S100\n")
	}
	return b.Bytes()
}

func TestV2RunsAJobSentReadyForTheButton(t *testing.T) {
	ctx := context.Background()
	dev, sim := v2Device(t, "40.52.016.05")

	fw, err := dev.Firmware(ctx)
	if err != nil || fw != "40.52.016.05" {
		t.Fatalf("firmware %q %v", fw, err)
	}
	st, err := dev.Status(ctx)
	if err != nil || st.State != xtool.StateIdle {
		t.Fatalf("status %+v %v", st, err)
	}
	for _, name := range []string{xtool.ReadingLid, xtool.ReadingSafetyKey, xtool.ReadingDrawer, xtool.ReadingFlame, xtool.ReadingMachine} {
		if r, ok := reading(st, name); !ok || !r.OK {
			t.Errorf("%s: %+v %v", name, r, ok)
		}
	}

	// Sent ready: uploaded whole, in windows and packets, and not started.
	job := gcode()
	if err := dev.SendJob(ctx, job, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sim.File("tmp.gcode"), job) {
		t.Fatalf("the machine got %d bytes of %d", len(sim.File("tmp.gcode")), len(job))
	}
	if st, _ = dev.Status(ctx); st.State != xtool.StateReady {
		t.Fatalf("after sending: %s", st.State)
	}
	if a := sim.Actions(); len(a) != 0 {
		t.Fatalf("started without the button: %v", a)
	}

	// The button starts it; then pause, resume, and it finishes.
	sim.PressButton()
	sim.Set(func(s *xtoolsim.V2) { s.Progress = 0.4 })
	if st, _ = dev.Status(ctx); st.State != xtool.StateRunning || st.Progress != 0.4 {
		t.Fatalf("running: %+v", st)
	}
	if err := dev.Control(ctx, xtool.ActionPause); err != nil {
		t.Fatal(err)
	}
	if st, _ = dev.Status(ctx); st.State != xtool.StatePaused {
		t.Fatalf("paused: %s", st.State)
	}
	if err := dev.Control(ctx, xtool.ActionResume); err != nil {
		t.Fatal(err)
	}
	sim.Finish()
	if st, _ = dev.Status(ctx); st.State != xtool.StateDone || st.Progress != 1 {
		t.Fatalf("done: %+v", st)
	}
	if got := strings.Join(sim.Actions(), ","); got != "button,pause,start" {
		t.Errorf("actions %s", got)
	}
}

func TestV2StartsAJobAndCancelsIt(t *testing.T) {
	ctx := context.Background()
	dev, sim := v2Device(t, "40.51.020.04")
	if err := dev.SendJob(ctx, []byte("G90\nG0S0\n"), true); err != nil {
		t.Fatal(err)
	}
	if st, _ := dev.Status(ctx); st.State != xtool.StateRunning {
		t.Fatalf("running: %s", st.State)
	}
	if err := dev.Control(ctx, xtool.ActionStop); err != nil {
		t.Fatal(err)
	}
	if st, _ := dev.Status(ctx); st.State != xtool.StateIdle {
		t.Fatalf("cancelled: %s", st.State)
	}
	if got := strings.Join(sim.Actions(), ","); got != "start,stop" {
		t.Errorf("actions %s", got)
	}
}

func TestV2FramesWithTheFrameFile(t *testing.T) {
	ctx := context.Background()
	dev, sim := v2Device(t, "40.51.020.04")
	frame := []byte("G90\nG0X1Y1S0\n")
	if err := dev.Frame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sim.File("tmpFrame.gcode"), frame) || sim.File("tmp.gcode") != nil {
		t.Fatalf("frame file %q, job file %q", sim.File("tmpFrame.gcode"), sim.File("tmp.gcode"))
	}
	if sim.Framed() != 1 {
		t.Fatalf("framed %d", sim.Framed())
	}
	if st, _ := dev.Status(ctx); st.State != xtool.StateFraming {
		t.Fatalf("framing: %s", st.State)
	}
	sim.Finish()
	waitFor(t, func() bool { st, _ := dev.Status(ctx); return st.State == xtool.StateIdle })
}

func TestV2ReportsInterlocks(t *testing.T) {
	ctx := context.Background()
	dev, sim := v2Device(t, "40.51.020.04")
	sim.Set(func(s *xtoolsim.V2) {
		s.Peripherals["gap"] = "off"
		s.Peripherals["machine_lock"] = "off"
		delete(s.Peripherals, "drawer") // this machine has none
	})
	st, err := dev.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := reading(st, xtool.ReadingLid); r.OK || r.Message != "The lid is open" {
		t.Errorf("lid %+v", r)
	}
	if r, _ := reading(st, xtool.ReadingSafetyKey); r.OK || r.Message != "The safety key is out" {
		t.Errorf("key %+v", r)
	}
	if _, ok := reading(st, xtool.ReadingDrawer); ok {
		t.Error("a drawer the machine has not got")
	}

	// A flame alarm, pushed, holds for a while, idle or not.
	dev.FlameHold = time.Second
	sim.Push("/fire/alarm", "FIRE_RECOGNITION", "FIRE", nil)
	waitFor(t, func() bool { st, _ := dev.Status(ctx); r, _ := reading(st, xtool.ReadingFlame); return !r.OK })
	sim.Push("/work/mode", "STATUS_CONTROLLER", "MODE_CHANGE", map[string]string{"mode": "P_IDLE"})
	if st, _ := dev.Status(ctx); st.Readings != nil {
		if r, _ := reading(st, xtool.ReadingFlame); r.OK {
			t.Error("the flame alarm cleared when the machine was idle")
		}
	}
	waitFor(t, func() bool { st, _ := dev.Status(ctx); r, _ := reading(st, xtool.ReadingFlame); return r.OK })

	// The emergency stop.
	sim.Push("/emergency_stop/status", "EMERGENCY_STOP", "VOLTAGE_TRIGGER", nil)
	waitFor(t, func() bool { st, _ := dev.Status(ctx); r, _ := reading(st, xtool.ReadingMachine); return !r.OK })
	sim.Push("/emergency_stop/status", "EMERGENCY_STOP", "RESUME", nil)
	waitFor(t, func() bool { st, _ := dev.Status(ctx); r, _ := reading(st, xtool.ReadingMachine); return r.OK })

	// An error mode is a problem, and a failed job.
	sim.Set(func(s *xtoolsim.V2) { s.Mode = "P_ERROR" })
	st, _ = dev.Status(ctx)
	if r, _ := reading(st, xtool.ReadingMachine); r.OK || st.State != xtool.StateFailed {
		t.Errorf("error mode: %s %+v", st.State, r)
	}
}

func TestV2ReconnectsAfterTheMachineGoesAway(t *testing.T) {
	ctx := context.Background()
	dev, sim := v2Device(t, "40.51.020.04")
	if _, err := dev.Status(ctx); err != nil {
		t.Fatal(err)
	}
	// Every connection dropped, as a machine restarting does: the next
	// request may fail, and one after it connects again.
	sim.Drop()
	waitFor(t, func() bool { _, err := dev.Status(ctx); return err == nil })
	if n := sim.Connects(); n != 2 {
		t.Errorf("connected %d times", n)
	}
}

func TestV2RefusalsCarryTheirCode(t *testing.T) {
	ctx := context.Background()
	dev, _ := v2Device(t, "40.51.020.04")
	err := dev.Control(ctx, xtool.ActionPause) // nothing is running
	var de *xtool.DeviceError
	if !errors.As(err, &de) || de.Code != 1 {
		t.Fatalf("got %v", err)
	}
	err = dev.Request(ctx, "GET", "/v1/peripheral/param", map[string]any{"type": "water_pump"}, nil, nil)
	if !errors.Is(err, xtool.ErrNotSupported) {
		t.Fatalf("code 10 is not ErrNotSupported: %v", err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRESTRunsAJob(t *testing.T) {
	ctx := context.Background()
	for _, dialect := range []string{xtool.DialectProcessing, xtool.DialectCNC} {
		t.Run(dialect, func(t *testing.T) {
			sim := xtoolsim.NewREST(dialect, "40.51.019.00")
			defer sim.Close()
			dev := &xtool.REST{Base: sim.Server.URL, Dialect: dialect}
			fw, err := dev.Firmware(ctx)
			if err != nil || fw != "40.51.019.00" {
				t.Fatalf("firmware %q %v", fw, err)
			}
			st, err := dev.Status(ctx)
			if err != nil || st.State != xtool.StateIdle {
				t.Fatalf("status %+v %v", st, err)
			}
			if r, ok := reading(st, xtool.ReadingLid); !ok || !r.OK {
				t.Errorf("lid %+v", r)
			}
			job := gcode()
			if err := dev.SendJob(ctx, job, true); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sim.File("tmp.gcode"), job) {
				t.Fatal("the job did not arrive whole")
			}
			if st, _ = dev.Status(ctx); st.State != xtool.StateRunning {
				t.Fatalf("running: %s", st.State)
			}
			if err := dev.Control(ctx, xtool.ActionPause); err != nil {
				t.Fatal(err)
			}
			if st, _ = dev.Status(ctx); st.State != xtool.StatePaused {
				t.Fatalf("paused: %s", st.State)
			}
			if err := dev.Control(ctx, xtool.ActionResume); err != nil {
				t.Fatal(err)
			}
			sim.Finish()
			if st, _ = dev.Status(ctx); st.State != xtool.StateDone {
				t.Fatalf("done: %s", st.State)
			}
			if err := dev.Frame(ctx, []byte("G90\n")); !errors.Is(err, xtool.ErrNotSupported) {
				t.Errorf("framing over REST V1: %v", err)
			}
		})
	}
}

func TestRESTReportsInterlocksAndRefusals(t *testing.T) {
	ctx := context.Background()
	sim := xtoolsim.NewREST(xtool.DialectProcessing, "40.51.019.00")
	defer sim.Close()
	dev := &xtool.REST{Base: sim.Server.URL, Dialect: xtool.DialectProcessing}
	sim.Set(func(s *xtoolsim.REST) {
		s.Peripherals["gap"] = "off"
		delete(s.Peripherals, "drawer")
	})
	st, err := dev.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := reading(st, xtool.ReadingLid); r.OK {
		t.Errorf("lid %+v", r)
	}
	if _, ok := reading(st, xtool.ReadingDrawer); ok {
		t.Error("a drawer the machine has not got")
	}
	// Nothing to pause: the machine answers "fail".
	if err := dev.Control(ctx, xtool.ActionPause); err == nil {
		t.Error("a refusal was taken as done")
	}
}
