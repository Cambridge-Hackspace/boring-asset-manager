package printers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/simcamera"
)

// DriverTestLaser is a simulated laser, for tests (docs/printing.md#lasers):
// it is registered only where a test turns it on ([printers] test_laser),
// takes a tcp connection it never dials, and engraves nothing. Its machines
// live in this process, one for each address, and finish a job a few
// seconds after starting it. The panel model has a camera, which shows
// simcamera's photos: the calibration marks in the first photo after
// engraving them, and a workpiece in every other.
const DriverTestLaser = "test-laser"

// The test laser's models: one started with its own button, and one
// started by BAM!.
const (
	TestLaserButton = "button"
	TestLaserPanel  = "panel"
)

// TestLaserDuration is how long a simulated laser takes over a job,
// unless a test sets its own.
const TestLaserDuration = 3 * time.Second

var testLaserOnce sync.Once

// RegisterTestLaser registers the test laser, once.
func RegisterTestLaser() {
	testLaserOnce.Do(func() {
		if err := Register(testLaser{}); err != nil {
			panic(err)
		}
	})
}

// testLaserMaterials are the test laser's presets.
func testLaserMaterials() []Material {
	return []Material{
		{ID: "birch-plywood-3mm", Name: "Birch plywood, 3 mm", Engrave: EngraveSetting{Power: 30, Speed: 150, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 8, Passes: 2}},
		{ID: "clear-acrylic-3mm", Name: "Clear acrylic, 3 mm", Engrave: EngraveSetting{Power: 20, Speed: 200, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 6, Passes: 1}, FromBack: true},
		{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 80, Speed: 300, Passes: 1, LPI: 508}},
	}
}

type testLaser struct{}

func (testLaser) OneShotModels() []OneShotModel {
	interlocks := []string{InterlockLid, InterlockSafetyKey, InterlockFlame, InterlockExhaust}
	return []OneShotModel{
		{ID: TestLaserButton, WorkWidthMM: 115, WorkHeightMM: 115, Materials: testLaserMaterials(), StartButton: true, Frames: true, Interlocks: interlocks},
		{ID: TestLaserPanel, WorkWidthMM: simcamera.WorkWidthMM, WorkHeightMM: simcamera.WorkHeightMM, Materials: testLaserMaterials(), Frames: true, Interlocks: interlocks, Camera: true},
	}
}

func (d testLaser) Info() DriverInfo {
	info := DriverInfo{Driver: DriverTestLaser, ModelDPI: map[string]int{}, Media: []MediaInfo{}}
	for _, m := range d.OneShotModels() {
		info.Models = append(info.Models, m.ID)
		info.ModelDPI[m.ID] = m.Materials[0].Engrave.LPI
	}
	return info
}

func (d testLaser) model(s Settings) (OneShotModel, error) {
	for _, m := range d.OneShotModels() {
		if m.ID == s.Model {
			return m, nil
		}
	}
	return OneShotModel{}, fmt.Errorf("model must be %s or %s", TestLaserButton, TestLaserPanel)
}

func (d testLaser) Validate(s Settings) error {
	if _, err := d.model(s); err != nil {
		return err
	}
	if len(s.Options) > 0 && string(s.Options) != "null" && string(s.Options) != "{}" {
		var none map[string]any
		if json.Unmarshal(s.Options, &none) != nil || len(none) > 0 {
			return errors.New("options: the test laser takes none")
		}
	}
	return nil
}

func (d testLaser) Geometry(s Settings) (Geometry, error) {
	m, err := d.model(s)
	if err != nil {
		return Geometry{}, err
	}
	return OneShotGeometry(m, "the test laser"), nil
}

func (testLaser) Plan(Settings, Fit) (Plan, error) { return nil, errOneShot }

func (d testLaser) Machine(c Connection, s Settings) (Machine, error) {
	m, err := d.model(s)
	if err != nil {
		return nil, err
	}
	if c.Type != ConnTCP {
		return nil, fmt.Errorf("%w: the test laser takes a tcp connection, which it never dials", ErrUnreachable)
	}
	return &testLaserMachine{sim: TestLaserAt(c.Address), model: m}, nil
}

// SimulatedLaser is one test laser's machine: what it reports and the jobs
// it was sent, which a test may change and read.
type SimulatedLaser struct {
	mu         sync.Mutex
	duration   time.Duration
	state      string
	interlocks map[string]Interlock
	job        *Job
	// started is when the job began running, less the time it has run
	// before a pause; ran is how long it ran before the pause.
	started time.Time
	ran     time.Duration
	// Jobs are those started, Framed those framed, in order.
	jobs, framed []*Job
	// photo is what its camera shows, when a test sets it; marks is set
	// from starting calibration marks until they are photographed.
	photo  []byte
	marks  bool
	photos int
}

var (
	testLasersMu sync.Mutex
	testLasers   = map[string]*SimulatedLaser{}
)

// TestLaserAt is the simulated machine at an address, made the first time
// it is asked for, idle with every interlock fine.
func TestLaserAt(address string) *SimulatedLaser {
	testLasersMu.Lock()
	defer testLasersMu.Unlock()
	sim, ok := testLasers[address]
	if !ok {
		sim = &SimulatedLaser{state: MachineIdle, interlocks: map[string]Interlock{}, duration: TestLaserDuration}
		testLasers[address] = sim
	}
	return sim
}

// SetInterlock has the machine report an interlock as fine, or not with a
// message.
func (m *SimulatedLaser) SetInterlock(name string, ok bool, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interlocks[name] = Interlock{Name: name, OK: ok, Message: message}
}

// SetDuration sets how long the machine takes over a job.
func (m *SimulatedLaser) SetDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.duration = d
}

// SetPhoto has the camera show a photo, or simcamera's again when nil.
func (m *SimulatedLaser) SetPhoto(photo []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.photo = photo
}

// Photos is how many photos the camera has taken.
func (m *SimulatedLaser) Photos() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.photos
}

// PressButton presses the machine's button, starting a job sent ready.
func (m *SimulatedLaser) PressButton() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == MachineReady {
		m.state, m.started, m.ran = MachineRunning, time.Now(), 0
	}
}

// Jobs are the jobs the machine was sent to start, in order.
func (m *SimulatedLaser) Jobs() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.jobs)
}

// Framed are the jobs the machine framed, in order.
func (m *SimulatedLaser) Framed() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.framed)
}

// advance moves a running job on with the clock, held with mu.
func (m *SimulatedLaser) advance() float64 {
	switch m.state {
	case MachineRunning:
		done := m.ran + time.Since(m.started)
		if done >= m.duration {
			m.state = MachineDone
			return 1
		}
		return float64(done) / float64(m.duration)
	case MachinePaused:
		return float64(m.ran) / float64(m.duration)
	case MachineDone:
		return 1
	}
	return 0
}

type testLaserMachine struct {
	sim   *SimulatedLaser
	model OneShotModel
}

func (t *testLaserMachine) Status(context.Context) (MachineStatus, error) {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	st := MachineStatus{Progress: m.advance(), State: m.state}
	for _, name := range t.model.Interlocks {
		i, ok := m.interlocks[name]
		if !ok {
			i = Interlock{Name: name, OK: true}
		}
		st.Interlocks = append(st.Interlocks, i)
	}
	return st, nil
}

func (t *testLaserMachine) Frame(_ context.Context, job *Job) error {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advance()
	if m.state == MachineRunning || m.state == MachinePaused {
		return errors.New("the machine is busy with a job")
	}
	m.framed = append(m.framed, job)
	return nil
}

func (t *testLaserMachine) Start(_ context.Context, job *Job) error {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advance()
	if m.state == MachineRunning || m.state == MachinePaused || m.state == MachineReady {
		return errors.New("the machine is busy with a job")
	}
	m.jobs = append(m.jobs, job)
	m.job = job
	m.marks = job.Calibration
	if t.model.StartButton {
		m.state = MachineReady
		return nil
	}
	m.state, m.started, m.ran = MachineRunning, time.Now(), 0
	return nil
}

func (t *testLaserMachine) Pause(context.Context) error {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advance()
	if m.state != MachineRunning {
		return errors.New("the machine is not running a job")
	}
	m.state, m.ran = MachinePaused, m.ran+time.Since(m.started)
	return nil
}

func (t *testLaserMachine) Resume(context.Context) error {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != MachinePaused {
		return errors.New("the machine has no job paused")
	}
	m.state, m.started = MachineRunning, time.Now()
	return nil
}

func (t *testLaserMachine) Cancel(context.Context) error {
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advance()
	if m.state == MachineRunning || m.state == MachinePaused || m.state == MachineReady {
		m.state = MachineCancelled
	}
	return nil
}

// Photo shows a test's photo, or the calibration marks just engraved, or
// a workpiece. Only the models with a camera are a Camera.
func (t *testLaserMachine) Photo(context.Context) ([]byte, error) {
	if !t.model.Camera {
		return nil, fmt.Errorf("the test laser's %s model has no camera", t.model.ID)
	}
	m := t.sim
	m.mu.Lock()
	defer m.mu.Unlock()
	m.photos++
	switch {
	case m.photo != nil:
		return m.photo, nil
	case m.marks:
		m.marks = false
		return simcamera.Photo(simcamera.Calibration), nil
	}
	return simcamera.Photo(simcamera.Workpiece), nil
}
