package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida/ruidasim"
)

func ruidaSim(t *testing.T) *ruidasim.Sim {
	t.Helper()
	timing := ruidaUDPTiming
	ruidaUDPTiming = ruida.UDPTiming{Ack: 200 * time.Millisecond, Tries: 2}
	sim := ruidasim.New()
	t.Cleanup(func() {
		sim.Close()
		ResetRuida()
		ruidaUDPTiming = timing
	})
	return sim
}

// ruidaMachineAt is a Ruida machine on a simulated controller, which
// answers where it was asked from, not to port 40200.
func ruidaMachineAt(t *testing.T, addr, options string) (Machine, OneShotModel) {
	t.Helper()
	opts := map[string]any{"reply_port": 0}
	if options != "" {
		if err := json.Unmarshal([]byte(options), &opts); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(opts)
	s := Settings{Model: RuidaCO260W, Options: raw}
	c := Connection{Type: ConnTCP, Address: addr}
	if err := Validate("CO2", DriverRuida, c, s); err != nil {
		t.Fatal(err)
	}
	m, err := MachineFor(DriverRuida, c, s)
	if err != nil {
		t.Fatal(err)
	}
	model, err := OneShotModelOf(DriverRuida, s)
	if err != nil {
		t.Fatal(err)
	}
	return m, model
}

func TestTheRuidaDriverDescribesItsLasers(t *testing.T) {
	if !IsOneShot(DriverRuida) {
		t.Fatal("ruida is not a one-shot driver")
	}
	for _, m := range (ruidaDriver{}).OneShotModels() {
		// Started from the panel, framed there, its interlocks its own.
		if !m.StartButton || m.Frames || m.Interlocks == nil || len(m.Interlocks) != 0 {
			t.Errorf("%s: %+v", m.ID, m)
		}
		for _, mat := range m.Materials {
			if _, err := ValidateMaterial(mat); err != nil {
				t.Errorf("%s %s: %v", m.ID, mat.ID, err)
			}
		}
	}
	m, err := OneShotModelOf(DriverRuida, Settings{Model: RuidaCO240W, Options: json.RawMessage(`{"work_width_mm":700,"work_height_mm":500}`)})
	if err != nil || m.WorkWidthMM != 700 || m.WorkHeightMM != 500 {
		t.Errorf("sized: %+v %v", m, err)
	}
	conn := Connection{Type: ConnTCP, Address: "192.0.2.8"}
	for _, bad := range []string{`{"file_name":"TOO-LONG-NAME"}`, `{"file_name":""}`, `{"magic":300}`, `{"reply_port":70000}`, `{"origin":"centre"}`, `{"frame_power":1}`} {
		if err := Validate("x", DriverRuida, conn, Settings{Model: RuidaCO240W, Options: json.RawMessage(bad)}); err == nil {
			t.Errorf("%s was taken", bad)
		}
	}
	if err := Validate("x", DriverRuida, conn, Settings{Model: RuidaCO240W, Options: json.RawMessage(`{"file_name":"TAGS1","magic":17,"origin":"top_left"}`)}); err != nil {
		t.Error(err)
	}
	if _, _, err := PlanLabel(DriverRuida, Settings{Model: RuidaCO240W}, LabelSpec{WidthMM: 20, HeightMM: 10, DPI: 254}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("printing to a Ruida: %v", err)
	}
}

func TestARuidaJobIsSentForThePanelAndNeverStartedByBAM(t *testing.T) {
	ctx := context.Background()
	sim := ruidaSim(t)
	m, model := ruidaMachineAt(t, sim.Addr(), `{"file_name":"TAGS"}`)
	job := xtoolJob(t, model)

	st, err := m.Status(ctx)
	if err != nil || st.State != MachineIdle || len(st.Problems()) != 0 {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := m.Frame(ctx, job); err == nil {
		t.Error("BAM! framed a Ruida")
	}
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, sumOK := sim.File("TAGS")
	want, err := ruida.File(ruidaProgram(ruida.Bed{WidthMM: 600, HeightMM: 400, FlipX: true}, job))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !sumOK {
		t.Fatalf("the controller kept %d bytes, not the job's %d (sum right %v)", len(got), len(want), sumOK)
	}
	st, _ = m.Status(ctx)
	if st.State != MachineReady || !strings.Contains(st.Message, "TAGS") {
		t.Fatalf("sent: %+v", st)
	}
	time.Sleep(20 * time.Millisecond)
	if st, _ = m.Status(ctx); st.State != MachineReady {
		t.Fatalf("started without the panel: %s", st.State)
	}
	sim.PressStart()
	waitForState(t, m, MachineRunning)
	if err := m.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachinePaused)
	if err := m.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineRunning)
	if err := m.Start(ctx, job); err == nil {
		t.Error("a job was sent over one running")
	}
	sim.Finish()
	waitForState(t, m, MachineDone)
	if a := sim.Actions(); !slices.Equal(a, []string{"pause", "resume"}) {
		t.Errorf("actions %v: BAM! never starts a Ruida", a)
	}

	// Another, sent and cancelled before the panel starts it.
	if err := m.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, m, MachineIdle)
	if a := sim.Actions(); a[len(a)-1] != "stop" {
		t.Errorf("actions %v", a)
	}
}

func TestAnUnreachableRuidaIsSaidSo(t *testing.T) {
	sim := ruidaSim(t)
	m, _ := ruidaMachineAt(t, sim.Addr(), "")
	sim.Close()
	if _, err := m.Status(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Errorf("status: %v", err)
	}
}
