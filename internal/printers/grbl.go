package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/serialport"
)

// DriverGRBL drives diode lasers on GRBL controllers, and on firmware that
// speaks as GRBL does (docs/printing.md#grbl), over USB serial or TCP. GRBL
// reports few interlocks: lasers.run's Start is the gate.
const DriverGRBL = "grbl"

type grblDriver struct{}

func init() {
	if err := Register(grblDriver{}); err != nil {
		panic(err)
	}
}

// GRBL models: diode lasers by their optical power, which their presets
// follow. The work area defaults to 400 × 400 mm, the commonest, and is
// set for each printer.
const (
	GRBLDiode10W = "diode-10w"
	GRBLDiode20W = "diode-20w"
	GRBLDiode40W = "diode-40w"
)

// Defaults of the grbl driver's options.
const (
	grblDefaultWork       = 400
	grblDefaultOrigin     = "bottom_left"
	grblDefaultBaud       = 115200
	grblDefaultTCPPort    = 23
	grblDefaultFrameSpeed = 50
	grblMaxFramePower     = 20
	grblMaxRXBuffer       = 16384
)

// grblIdle is how long a controller's connection is kept open unused.
var grblIdle = 2 * time.Minute

// grblTiming is how long controllers are waited for; tests shorten it.
var grblTiming = grbl.DefaultTiming

type grblOptions struct {
	bedOptions
	Baud       *int `json:"baud"`
	RXBuffer   *int `json:"rx_buffer"`
	MaxPowerS  *int `json:"max_power_s"`
	FramePower *int `json:"frame_power"`
	FrameSpeed *int `json:"frame_speed"`
}

// grblMaterials are a model's presets: starting points to try on scrap.
func grblMaterials(model string) []Material {
	switch model {
	case GRBLDiode20W:
		return []Material{
			{ID: "basswood-plywood-3mm", Name: "Basswood plywood, 3 mm", Engrave: EngraveSetting{Power: 30, Speed: 150, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 6, Passes: 2}},
			{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 60, Speed: 100, Passes: 1, LPI: 254}},
			{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 10, Speed: 200, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 40, Speed: 25, Passes: 1}},
		}
	case GRBLDiode40W:
		return []Material{
			{ID: "basswood-plywood-3mm", Name: "Basswood plywood, 3 mm", Engrave: EngraveSetting{Power: 20, Speed: 200, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 10, Passes: 1}},
			{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 40, Speed: 150, Passes: 1, LPI: 254}},
			{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 8, Speed: 300, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 25, Speed: 40, Passes: 1}},
		}
	}
	return []Material{
		{ID: "basswood-plywood-3mm", Name: "Basswood plywood, 3 mm", Engrave: EngraveSetting{Power: 40, Speed: 100, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 4, Passes: 3}},
		{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 80, Speed: 80, Passes: 1, LPI: 254}},
		{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 15, Speed: 150, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 60, Speed: 15, Passes: 1}},
	}
}

func (grblDriver) OneShotModels() []OneShotModel {
	var out []OneShotModel
	for _, id := range []string{GRBLDiode10W, GRBLDiode20W, GRBLDiode40W} {
		out = append(out, OneShotModel{
			ID: id, WorkWidthMM: grblDefaultWork, WorkHeightMM: grblDefaultWork, Materials: grblMaterials(id),
			Frames: true, Interlocks: []string{InterlockMachine},
		})
	}
	return out
}

func (d grblDriver) OneShotModelFor(s Settings) (OneShotModel, error) {
	m, o, err := d.settings(s)
	if err != nil {
		return m, err
	}
	return o.sized(m), nil
}

func (d grblDriver) Info() DriverInfo {
	info := DriverInfo{Driver: DriverGRBL, ModelDPI: map[string]int{}, Media: []MediaInfo{}}
	for _, m := range d.OneShotModels() {
		info.Models = append(info.Models, m.ID)
		info.ModelDPI[m.ID] = m.Materials[0].Engrave.LPI
	}
	info.Options = append(bedOptionInfo(grblDefaultOrigin), OptionInfo{
		Name: "baud", Type: "int", Default: grblDefaultBaud,
		Description: "The serial port's speed, over a device connection: 115200 for GRBL 1.1.",
	}, OptionInfo{
		Name: "rx_buffer", Type: "int", Default: grbl.DefaultRXBuffer,
		Description: fmt.Sprintf("The controller's receive buffer, in bytes (16-%d): 128 on an Arduino; grblHAL and FluidNC say theirs in the Bf field of a status report. Larger than the controller's loses G-code.", grblMaxRXBuffer),
	}, OptionInfo{
		Name: "max_power_s", Type: "int", Default: 0,
		Description: "The S value of full power; 0 reads it from the controller's $30.",
	}, OptionInfo{
		Name: "frame_power", Type: "int", Default: 0,
		Description: fmt.Sprintf("The laser's power while framing, in percent (0-%d): low enough to show a dot without marking. 0 frames with the laser off, the head alone showing the outline.", grblMaxFramePower),
	}, OptionInfo{
		Name: "frame_speed", Type: "int", Default: grblDefaultFrameSpeed,
		Description: "How fast framing traces the outline, in millimeters a second.",
	})
	return info
}

func (grblDriver) settings(s Settings) (OneShotModel, grblOptions, error) {
	var model OneShotModel
	found := false
	for _, m := range (grblDriver{}).OneShotModels() {
		if m.ID == s.Model {
			model, found = m, true
		}
	}
	if !found {
		return model, grblOptions{}, fmt.Errorf("model must be one of %s, %s, %s", GRBLDiode10W, GRBLDiode20W, GRBLDiode40W)
	}
	var o grblOptions
	if len(s.Options) > 0 && string(s.Options) != "null" {
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return model, o, fmt.Errorf("options: %v", err)
		}
	}
	if err := o.check(); err != nil {
		return model, o, err
	}
	switch {
	case o.Baud != nil && !serialport.ValidBaud(*o.Baud):
		return model, o, fmt.Errorf("options: baud must be one of %s", joinInts(serialport.Bauds))
	case o.RXBuffer != nil && (*o.RXBuffer < 16 || *o.RXBuffer > grblMaxRXBuffer):
		return model, o, fmt.Errorf("options: rx_buffer must be 16-%d", grblMaxRXBuffer)
	case o.MaxPowerS != nil && (*o.MaxPowerS < 0 || *o.MaxPowerS > 1000000):
		return model, o, errors.New("options: max_power_s must be 0-1000000")
	case o.FramePower != nil && (*o.FramePower < 0 || *o.FramePower > grblMaxFramePower):
		return model, o, fmt.Errorf("options: frame_power must be 0-%d", grblMaxFramePower)
	case o.FrameSpeed != nil && (*o.FrameSpeed < 1 || *o.FrameSpeed > MaxSpeedMM):
		return model, o, fmt.Errorf("options: frame_speed must be 1-%d", MaxSpeedMM)
	}
	return model, o, nil
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

func (d grblDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d grblDriver) Geometry(s Settings) (Geometry, error) {
	m, err := d.OneShotModelFor(s)
	if err != nil {
		return Geometry{}, err
	}
	return OneShotGeometry(m, "the GRBL laser"), nil
}

func (grblDriver) Plan(Settings, Fit) (Plan, error) { return nil, errOneShot }

// Machine reaches the controller: over a device connection, its serial
// port; over tcp, its address, the port 23 unless another is given.
func (d grblDriver) Machine(c Connection, s Settings) (Machine, error) {
	m, o, err := d.settings(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPrinter, err)
	}
	x := &grblMachine{model: o.sized(m), opts: o}
	x.flipX, x.flipY = originFlips(o.origin(grblDefaultOrigin))
	switch c.Type {
	case ConnTCP:
		host, portText, err := net.SplitHostPort(c.Address)
		if err != nil {
			return nil, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
		}
		if port, _ := strconv.Atoi(portText); port == DefaultPort {
			portText = strconv.Itoa(grblDefaultTCPPort)
		}
		x.key = "tcp:" + net.JoinHostPort(host, portText)
		addr := net.JoinHostPort(host, portText)
		x.dial = func(ctx context.Context) (io.ReadWriteCloser, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "tcp", addr)
		}
	case ConnDevice:
		baud := grblDefaultBaud
		if o.Baud != nil {
			baud = *o.Baud
		}
		x.key = "device:" + c.Address
		path := c.Address
		x.dial = func(context.Context) (io.ReadWriteCloser, error) {
			return serialport.Open(path, baud, false)
		}
	default:
		return nil, fmt.Errorf("%w: a GRBL laser takes a device connection, its serial port, or a tcp one", ErrInvalidPrinter)
	}
	x.rx = grbl.DefaultRXBuffer
	if o.RXBuffer != nil {
		x.rx = *o.RXBuffer
	}
	return x, nil
}

var (
	grblMu          sync.Mutex
	grblControllers = map[string]*grbl.Controller{}
	grblOpening     = map[string]*sync.Mutex{}
)

// CloseGRBL closes every GRBL connection, for tests.
func CloseGRBL() {
	grblMu.Lock()
	all := grblControllers
	grblControllers = map[string]*grbl.Controller{}
	grblMu.Unlock()
	for _, c := range all {
		c.Close()
	}
}

// grblMachine is a GRBL laser, as a one-shot printer's Machine.
type grblMachine struct {
	model        OneShotModel
	opts         grblOptions
	flipX, flipY bool
	key          string
	rx           int
	dial         func(ctx context.Context) (io.ReadWriteCloser, error)
}

// controller is the open connection to the controller, opened if there is
// none. It is kept open across requests, since a job streams through it,
// and closed when it has been unused a while. One caller at a time opens a
// controller's port, so it is never opened twice.
func (x *grblMachine) controller(ctx context.Context) (*grbl.Controller, error) {
	grblMu.Lock()
	opening, ok := grblOpening[x.key]
	if !ok {
		opening = &sync.Mutex{}
		grblOpening[x.key] = opening
	}
	grblMu.Unlock()
	opening.Lock()
	defer opening.Unlock()
	grblMu.Lock()
	if c, ok := grblControllers[x.key]; ok {
		select {
		case <-c.Done():
			delete(grblControllers, x.key)
		default:
			grblMu.Unlock()
			return c, nil
		}
	}
	grblMu.Unlock()
	rw, err := x.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	c := grbl.New(rw, x.rx, grblTiming)
	if err := c.Connect(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("%w: the controller did not answer as GRBL does: %v", ErrUnreachable, err)
	}
	grblMu.Lock()
	grblControllers[x.key] = c
	grblMu.Unlock()
	go func(key string) {
		for {
			select {
			case <-c.Done():
				return
			case <-time.After(grblIdle / 4):
			}
			if c.Idle(grblIdle) {
				grblMu.Lock()
				if grblControllers[key] == c {
					delete(grblControllers, key)
				}
				grblMu.Unlock()
				c.Close()
				return
			}
		}
	}(x.key)
	return c, nil
}

// sMax is the S value of full power: the option's, or the controller's
// $30.
func (x *grblMachine) sMax(c *grbl.Controller) (int, string) {
	if x.opts.MaxPowerS != nil && *x.opts.MaxPowerS > 0 {
		return *x.opts.MaxPowerS, ""
	}
	v, ok := c.Setting(30)
	if !ok {
		return 0, "The controller did not say its full power ($30); set max_power_s on the printer"
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 1 {
		return 0, fmt.Sprintf("The controller's full power ($30=%s) is not one BAM! can use; set max_power_s on the printer", v)
	}
	return int(f), ""
}

// problems are why the controller may not start a job, as it reports.
func (x *grblMachine) problems(c *grbl.Controller, r grbl.Report) []string {
	var out []string
	switch r.Base() {
	case "Idle", "Run", "Jog":
	case "Hold":
		if j, ok := c.Job(); !ok || !j.Active() {
			out = append(out, "The machine is in a feed hold")
		}
	case "Alarm":
		msg := "The controller is in alarm: unlock or home it at the machine"
		if m := c.Message(); strings.HasPrefix(m, "ALARM:") {
			msg += " (" + m + ")"
		}
		out = append(out, msg)
	case "Door":
		out = append(out, "The safety door is open")
	case "Check":
		out = append(out, "The controller is in check mode ($C), which burns nothing")
	case "Home":
		out = append(out, "The machine is homing")
	case "Sleep":
		out = append(out, "The controller is asleep: reset it at the machine")
	default:
		out = append(out, fmt.Sprintf("The controller reports %q, which BAM! does not know", r.State))
	}
	switch v, ok := c.Setting(32); {
	case !ok:
		out = append(out, "The controller did not say whether laser mode is on ($32): BAM! drives GRBL only in laser mode")
	case strings.TrimSpace(v) != "1":
		out = append(out, fmt.Sprintf("Laser mode is off ($32=%s): set $32=1 on the controller, so the laser is off between cuts", v))
	}
	if _, why := x.sMax(c); why != "" {
		out = append(out, why)
	}
	return out
}

func (x *grblMachine) Status(ctx context.Context) (MachineStatus, error) {
	c, err := x.controller(ctx)
	if err != nil {
		return MachineStatus{}, err
	}
	r, err := c.Status(ctx)
	if err != nil {
		return MachineStatus{}, err
	}
	out := MachineStatus{State: MachineIdle}
	j, ok := c.Job()
	running := r.Base() == "Run" || r.Base() == "Jog"
	held := r.Base() == "Hold" || r.Base() == "Door"
	switch {
	case ok && j.Err != "":
		out.State, out.Message = MachineFailed, j.Err
	case ok && !j.Stopped && (j.Active() || running || held):
		out.State = MachineRunning
		if j.Frame {
			out.State = MachineFraming
		} else if held {
			out.State = MachinePaused
		}
		if j.Lines > 0 {
			out.Progress = float64(j.Acked) / float64(j.Lines)
		}
	case ok && j.Finished() && !j.Frame:
		out.State, out.Progress = MachineDone, 1
	}
	machine := Interlock{Name: InterlockMachine, OK: true}
	if p := x.problems(c, r); len(p) > 0 {
		machine = Interlock{Name: InterlockMachine, Message: strings.Join(p, "; ")}
	}
	out.Interlocks = []Interlock{machine}
	return out, nil
}

// ready refreshes the controller's settings and checks it may run a job,
// returning the S of full power.
func (x *grblMachine) ready(ctx context.Context, c *grbl.Controller) (int, error) {
	if j, ok := c.Job(); ok && j.Active() {
		return 0, errors.New("the controller is busy with another job")
	}
	if _, err := c.Settings(ctx); err != nil {
		return 0, err
	}
	r, err := c.Status(ctx)
	if err != nil {
		return 0, err
	}
	if p := x.problems(c, r); len(p) > 0 {
		return 0, fmt.Errorf("refused: %s", strings.Join(p, "; "))
	}
	s, _ := x.sMax(c)
	return s, nil
}

func (x *grblMachine) bed(sMax int) grbl.Bed {
	return grbl.Bed{WidthMM: x.model.WorkWidthMM, HeightMM: x.model.WorkHeightMM, FlipX: x.flipX, FlipY: x.flipY, SMax: sMax}
}

// Frame traces the job's outline: with the laser at frame_power, or off.
func (x *grblMachine) Frame(ctx context.Context, job *Job) error {
	c, err := x.controller(ctx)
	if err != nil {
		return err
	}
	sMax, err := x.ready(ctx, c)
	if err != nil {
		return err
	}
	power, speed := 0, grblDefaultFrameSpeed
	if x.opts.FramePower != nil {
		power = *x.opts.FramePower
	}
	if x.opts.FrameSpeed != nil {
		speed = *x.opts.FrameSpeed
	}
	o := job.Outline()
	gcode, err := grbl.Frame(x.bed(sMax), o.X, o.Y, o.Width, o.Height, float64(power), float64(speed))
	if err != nil {
		return err
	}
	return c.Stream(gcode, true)
}

// Start streams the job: GRBL has no button, so it starts as it arrives.
func (x *grblMachine) Start(ctx context.Context, job *Job) error {
	c, err := x.controller(ctx)
	if err != nil {
		return err
	}
	sMax, err := x.ready(ctx, c)
	if err != nil {
		return err
	}
	gcode, err := grbl.GCode(grblProgram(x.bed(sMax), job))
	if err != nil {
		return err
	}
	return c.Stream(gcode, false)
}

// grblProgram is a job as the grbl package writes it.
func grblProgram(bed grbl.Bed, job *Job) grbl.Program {
	p := grbl.Program{Bed: bed}
	if job.Engrave != nil {
		e := job.Material.Engrave
		p.Raster = &grbl.Raster{X: job.Label.X, Y: job.Label.Y, Width: job.Label.Width, Height: job.Label.Height, Dots: job.Engrave}
		p.Engrave = grbl.Setting{Power: e.Power, Speed: e.Speed, Passes: e.Passes}
	}
	if cut := job.Material.Cut; cut != nil && len(job.Cuts) > 0 {
		p.Cut = grbl.Setting{Power: cut.Power, Speed: cut.Speed, Passes: cut.Passes}
		for _, line := range job.Cuts {
			path := grbl.Path{Closed: line.Closed}
			for _, pt := range line.Points {
				path.Points = append(path.Points, grbl.Point{X: pt.X, Y: pt.Y})
			}
			p.Paths = append(p.Paths, path)
		}
	}
	return p
}

func (x *grblMachine) Pause(ctx context.Context) error {
	c, err := x.controller(ctx)
	if err != nil {
		return err
	}
	return c.Pause()
}

func (x *grblMachine) Resume(ctx context.Context) error {
	c, err := x.controller(ctx)
	if err != nil {
		return err
	}
	return c.Resume()
}

// Cancel holds the head, then resets the controller: never refused.
func (x *grblMachine) Cancel(ctx context.Context) error {
	c, err := x.controller(ctx)
	if err != nil {
		return err
	}
	return c.Cancel(ctx)
}
