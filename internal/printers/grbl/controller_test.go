package grbl_test

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl/grblsim"
)

var fast = grbl.Timing{Boot: time.Second, Answer: time.Second, Hold: time.Second}

// connect connects to a simulated controller whose buffer holds rx bytes.
func connect(t *testing.T, sim *grblsim.Sim, rx int) *grbl.Controller {
	t.Helper()
	conn, err := net.Dial("tcp", sim.Addr())
	if err != nil {
		t.Fatal(err)
	}
	c := grbl.New(conn, rx, fast)
	t.Cleanup(func() { c.Close() })
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func newSim(t *testing.T, f func(*grblsim.Sim)) *grblsim.Sim {
	t.Helper()
	sim := grblsim.New()
	if f != nil {
		sim.Set(f)
	}
	t.Cleanup(sim.Close)
	return sim
}

// program is lines of G-code, n of them, numbered so each is its own.
func program(n int) ([]byte, []string) {
	var lines []string
	for i := range n {
		lines = append(lines, fmt.Sprintf("G1X%d.%03dY%dS%d", i%400, i, i%37, i%1000))
	}
	return []byte(strings.Join(lines, "\n") + "\n"), lines
}

func waitJob(t *testing.T, c *grbl.Controller, done func(grbl.Job) bool) grbl.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, _ := c.Job()
		if done(j) {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %+v", j)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStreamingNeverOverfillsASmallBuffer(t *testing.T) {
	// A 48-byte buffer, slow to empty: the sender must wait on it.
	sim := newSim(t, func(s *grblsim.Sim) { s.RX, s.LineTime = 48, time.Millisecond })
	c := connect(t, sim, 48)
	gcode, lines := program(300)
	if err := c.Stream(gcode, false); err != nil {
		t.Fatal(err)
	}
	j := waitJob(t, c, func(j grbl.Job) bool { return !j.Active() })
	if !j.Finished() || j.Acked != len(lines) {
		t.Fatalf("job %+v", j)
	}
	if sim.Overflowed() {
		t.Fatalf("the controller's buffer was overfilled: %d bytes in 48", sim.MaxBuffered())
	}
	// Character counting keeps it full, not a line at a time.
	if sim.MaxBuffered() < 30 {
		t.Errorf("the buffer held %d bytes at most: the sender waited a line at a time", sim.MaxBuffered())
	}
	if got := sim.Lines(); !slices.Equal(got, lines) {
		t.Errorf("the controller took %d lines, differing from the %d sent", len(got), len(lines))
	}
}

func TestSettingsAndStatus(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) { s.Settings[30] = "255" })
	c := connect(t, sim, 128)
	if v, ok := c.Setting(30); !ok || v != "255" {
		t.Errorf("$30 = %q", v)
	}
	if v, _ := c.Setting(32); v != "1" {
		t.Errorf("$32 = %q", v)
	}
	r, err := c.Status(context.Background())
	if err != nil || r.State != "Idle" || r.Fields["MPos"] != "0.000,0.000,0.000" {
		t.Errorf("status %+v %v", r, err)
	}
	sim.SetState("Door:1")
	if r, _ := c.Status(context.Background()); r.Base() != "Door" {
		t.Errorf("door: %+v", r)
	}
}

func TestAControllerOverTheNetworkNeedNotWelcome(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) { s.NoBanner = true })
	start := time.Now()
	connect(t, sim, 128)
	if time.Since(start) < fast.Boot/2 {
		t.Error("did not wait for a welcome")
	}
}

func TestAnErrorStopsTheJobAndTheMachine(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) {
		s.Refuse = func(line string) int {
			if strings.HasPrefix(line, "G1X50.050") {
				return 22
			}
			return 0
		}
	})
	c := connect(t, sim, 128)
	gcode, _ := program(200)
	if err := c.Stream(gcode, false); err != nil {
		t.Fatal(err)
	}
	j := waitJob(t, c, func(j grbl.Job) bool { return j.Err != "" })
	if !strings.Contains(j.Err, "error:22") || !strings.Contains(j.Err, "line 51") {
		t.Errorf("error %q", j.Err)
	}
	// Held, then reset: the controller forgets what followed.
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(sim.Realtime(), 0x18) {
		if time.Now().After(deadline) {
			t.Fatalf("not reset after an error: %q", sim.Realtime())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rt := sim.Realtime(); rt[0] != '!' {
		t.Errorf("reset without a hold first: %q", rt)
	}
	if n := len(sim.Lines()); n >= 200 {
		t.Errorf("the controller ran %d lines, all of them", n)
	}
}

func TestAnAlarmFailsTheJob(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) { s.LineTime = 2 * time.Millisecond })
	c := connect(t, sim, 128)
	gcode, _ := program(200)
	if err := c.Stream(gcode, false); err != nil {
		t.Fatal(err)
	}
	waitJob(t, c, func(j grbl.Job) bool { return j.Acked > 10 })
	sim.Alarm(1)
	j := waitJob(t, c, func(j grbl.Job) bool { return j.Err != "" })
	if !strings.Contains(j.Err, "ALARM:1") {
		t.Errorf("error %q", j.Err)
	}
	if r, _ := c.Status(context.Background()); r.Base() != "Alarm" {
		t.Errorf("state %s", r.State)
	}
	if c.Message() != "ALARM:1" {
		t.Errorf("message %q", c.Message())
	}
	// Another job is taken, for the controller to refuse while it is in
	// alarm: the driver checks its state before streaming.
	if err := c.Stream(gcode, false); err != nil {
		t.Errorf("a stream after a failed one: %v", err)
	}
}

func TestPauseResumeAndCancel(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) { s.LineTime = 2 * time.Millisecond })
	c := connect(t, sim, 128)
	gcode, _ := program(300)
	if err := c.Stream(gcode, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Stream(gcode, false); err == nil {
		t.Error("a second job was streamed over the first")
	}
	waitJob(t, c, func(j grbl.Job) bool { return j.Acked > 5 })
	if err := c.Pause(); err != nil {
		t.Fatal(err)
	}
	if r, _ := c.Status(context.Background()); r.State != "Hold:0" {
		t.Errorf("paused: %s", r.State)
	}
	held, _ := c.Job()
	time.Sleep(30 * time.Millisecond)
	if still, _ := c.Job(); still.Acked != held.Acked {
		t.Errorf("lines run while held: %d then %d", held.Acked, still.Acked)
	}
	if err := c.Resume(); err != nil {
		t.Fatal(err)
	}
	waitJob(t, c, func(j grbl.Job) bool { return j.Acked > held.Acked })
	if err := c.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := c.Job()
	if !j.Stopped || j.Err != "" {
		t.Errorf("cancelled: %+v", j)
	}
	// Held to a stop before the reset, so the controller keeps its place
	// and raises no alarm.
	if r, _ := c.Status(context.Background()); r.State != "Idle" {
		t.Errorf("after cancelling: %s", r.State)
	}
	if rt := string(sim.Realtime()); !strings.HasSuffix(rt, "!\x18") {
		t.Errorf("real-time commands %q", rt)
	}
	n := len(sim.Lines())
	time.Sleep(30 * time.Millisecond)
	if len(sim.Lines()) != n {
		t.Error("lines were sent after cancelling")
	}
}

func TestALostConnectionFailsTheJob(t *testing.T) {
	sim := newSim(t, func(s *grblsim.Sim) { s.LineTime = 2 * time.Millisecond })
	c := connect(t, sim, 128)
	gcode, _ := program(300)
	if err := c.Stream(gcode, false); err != nil {
		t.Fatal(err)
	}
	waitJob(t, c, func(j grbl.Job) bool { return j.Acked > 5 })
	sim.Close()
	j := waitJob(t, c, func(j grbl.Job) bool { return j.Err != "" })
	if !strings.Contains(j.Err, "lost") {
		t.Errorf("error %q", j.Err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Error("the controller is not closed")
	}
}

func TestALineLongerThanTheBufferIsRefused(t *testing.T) {
	sim := newSim(t, nil)
	c := connect(t, sim, 16)
	if err := c.Stream([]byte("G1X100.123Y200.456S1000\n"), false); err == nil {
		t.Error("a line longer than the buffer was taken")
	}
}
