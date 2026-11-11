package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
)

// DriverXTool drives xTool lasers (docs/printing.md#xtool): the F1 and the
// F1 Ultra on WS-V2 firmware, and on REST V1 firmware the same two, the P2
// and the M1. A model names its protocol, since which one a machine speaks
// depends on its firmware.
const DriverXTool = "xtool"

type xtoolDriver struct{}

func init() {
	if err := Register(xtoolDriver{}); err != nil {
		panic(err)
	}
}

// xtoolOptions are the xtool driver's options.
type xtoolOptions struct {
	// AllowUnknownFirmware lets BAM! drive firmware it does not know.
	AllowUnknownFirmware *bool `json:"allow_unknown_firmware"`
	// FlameHoldMinutes is how long starting is refused after the machine
	// reports a flame.
	FlameHoldMinutes *int `json:"flame_hold_minutes"`
}

// xtoolMaxFlameHold bounds flame_hold_minutes: a day at most.
const xtoolMaxFlameHold = 24 * 60

// xtoolMaterials are a model's presets: starting points to try on scrap,
// not xTool's own figures (docs/printing.md#xtool).
func xtoolMaterials(m xtool.Model) []Material {
	switch {
	case m.ID == xtool.ModelP2V1:
		// A 55 W CO2 laser.
		return []Material{
			{ID: "birch-plywood-3mm", Name: "Birch plywood, 3 mm", Engrave: EngraveSetting{Power: 20, Speed: 300, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 70, Speed: 15, Passes: 1}},
			{ID: "clear-acrylic-3mm", Name: "Clear acrylic, 3 mm", Engrave: EngraveSetting{Power: 15, Speed: 300, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 80, Speed: 10, Passes: 1}, FromBack: true},
			{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 8, Speed: 400, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 15, Speed: 80, Passes: 1}},
		}
	case m.ID == xtool.ModelM1V1:
		// A 10 W diode laser on a gantry.
		return []Material{
			{ID: "basswood-plywood-3mm", Name: "Basswood plywood, 3 mm", Engrave: EngraveSetting{Power: 40, Speed: 100, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 100, Speed: 4, Passes: 3}},
			{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 80, Speed: 80, Passes: 1, LPI: 254}},
			{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 15, Speed: 150, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 60, Speed: 15, Passes: 1}},
		}
	}
	// The F1 and the F1 Ultra: galvo diode lasers, fast and fine.
	return []Material{
		{ID: "basswood-plywood-3mm", Name: "Basswood plywood, 3 mm", Engrave: EngraveSetting{Power: 40, Speed: 400, Passes: 1, LPI: 254}},
		{ID: "anodized-aluminum", Name: "Anodized aluminum", Engrave: EngraveSetting{Power: 70, Speed: 300, Passes: 1, LPI: 508}},
		{ID: "leather", Name: "Leather", Engrave: EngraveSetting{Power: 25, Speed: 500, Passes: 1, LPI: 254}},
		{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: 15, Speed: 600, Passes: 1, LPI: 254}, Cut: &CutSetting{Power: 60, Speed: 100, Passes: 3}},
	}
}

// xtoolInterlocks are those a model is asked for.
func xtoolInterlocks(m xtool.Model) []string {
	out := []string{InterlockLid, InterlockSafetyKey, InterlockDrawer}
	if m.Protocol == xtool.WSV2 {
		// Flame alarms are pushed over WS-V2; REST V1 has no documented
		// way to read them.
		out = append(out, InterlockFlame)
	}
	return append(out, InterlockMachine, InterlockFirmware)
}

func (xtoolDriver) OneShotModels() []OneShotModel {
	var out []OneShotModel
	for _, m := range xtool.Models() {
		out = append(out, OneShotModel{
			ID: m.ID, WorkWidthMM: m.WorkWidthMM, WorkHeightMM: m.WorkHeightMM, Materials: xtoolMaterials(m),
			StartButton: m.StartButton, Frames: m.Frames, Interlocks: xtoolInterlocks(m),
		})
	}
	return out
}

func (d xtoolDriver) Info() DriverInfo {
	info := DriverInfo{Driver: DriverXTool, ModelDPI: map[string]int{}, Media: []MediaInfo{}}
	for _, m := range d.OneShotModels() {
		info.Models = append(info.Models, m.ID)
		info.ModelDPI[m.ID] = m.Materials[0].Engrave.LPI
	}
	info.Options = []OptionInfo{{
		Name: "allow_unknown_firmware", Type: "bool", Default: false,
		Description: "Drive this laser even when its firmware is not one BAM! knows for its model. A protocol mismatch on a laser is a safety problem: allow it only after checking the machine yourself.",
	}, {
		Name: "flame_hold_minutes", Type: "int", Default: int(xtool.DefaultFlameHold / time.Minute),
		Description: fmt.Sprintf("How long, in minutes, BAM! refuses to start after the machine reports a flame (1-%d). The machine does not say when one is out.", xtoolMaxFlameHold),
	}}
	return info
}

func (xtoolDriver) settings(s Settings) (xtool.Model, xtoolOptions, error) {
	m, ok := xtool.LookupModel(s.Model)
	if !ok {
		var ids []string
		for _, m := range xtool.Models() {
			ids = append(ids, m.ID)
		}
		return m, xtoolOptions{}, fmt.Errorf("model must be one of %s", strings.Join(ids, ", "))
	}
	var o xtoolOptions
	if len(s.Options) > 0 && string(s.Options) != "null" {
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return m, o, fmt.Errorf("options: %v", err)
		}
	}
	if o.FlameHoldMinutes != nil && (*o.FlameHoldMinutes < 1 || *o.FlameHoldMinutes > xtoolMaxFlameHold) {
		return m, o, fmt.Errorf("options: flame_hold_minutes must be 1-%d", xtoolMaxFlameHold)
	}
	return m, o, nil
}

func (d xtoolDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d xtoolDriver) Geometry(s Settings) (Geometry, error) {
	m, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	for _, om := range d.OneShotModels() {
		if om.ID == m.ID {
			return OneShotGeometry(om, "the xTool "+m.ID), nil
		}
	}
	return Geometry{}, fmt.Errorf("no model %s", m.ID)
}

func (xtoolDriver) Plan(Settings, Fit) (Plan, error) { return nil, errOneShot }

// Machine reaches the machine at a tcp connection's host. Its port, when
// one other than the default is given, replaces the protocol's standard
// one: 28900 for WS-V2, 8080 for REST V1.
func (d xtoolDriver) Machine(c Connection, s Settings) (Machine, error) {
	m, o, err := d.settings(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPrinter, err)
	}
	if c.Type != ConnTCP {
		return nil, fmt.Errorf("%w: an xTool laser takes a tcp connection: its address on the network", ErrInvalidPrinter)
	}
	host, portText, err := net.SplitHostPort(c.Address)
	if err != nil {
		return nil, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
	}
	port, _ := strconv.Atoi(portText)
	var dev xtool.Device
	switch m.Protocol {
	case xtool.WSV2:
		if port == DefaultPort {
			port = xtool.PortWSV2
		}
		v2 := &xtool.V2{Addr: net.JoinHostPort(host, strconv.Itoa(port))}
		if o.FlameHoldMinutes != nil {
			v2.FlameHold = time.Duration(*o.FlameHoldMinutes) * time.Minute
		}
		dev = v2
	default:
		if port == DefaultPort {
			port = xtool.PortRESTV1
		}
		dev = &xtool.REST{Base: "http://" + net.JoinHostPort(host, strconv.Itoa(port)), Dialect: m.Dialect}
	}
	return &xtoolMachine{dev: dev, model: m, allowUnknown: o.AllowUnknownFirmware != nil && *o.AllowUnknownFirmware}, nil
}

// xtoolMachine is an xTool laser, as a one-shot printer's Machine.
type xtoolMachine struct {
	dev          xtool.Device
	model        xtool.Model
	allowUnknown bool
}

// firmware checks the machine's firmware: the interlock it is reported as,
// and why it is refused, if it is.
func (x *xtoolMachine) firmware(ctx context.Context) (Interlock, string, error) {
	fw, err := x.dev.Firmware(ctx)
	if err != nil {
		return Interlock{}, "", err
	}
	if x.model.KnownFirmware(fw) {
		return Interlock{Name: InterlockFirmware, OK: true}, "", nil
	}
	if x.allowUnknown {
		return Interlock{Name: InterlockFirmware, OK: true, Message: fmt.Sprintf("Firmware %s is not one BAM! knows for the %s, but this printer allows it", fw, x.model.ID)}, "", nil
	}
	why := fmt.Sprintf("The firmware, %s, is not one BAM! knows for the %s (it knows %s); an admin may allow it on the printer, after checking the machine", fw, x.model.ID, x.model.KnownFirmwareText())
	return Interlock{Name: InterlockFirmware, Message: why}, why, nil
}

func (x *xtoolMachine) Status(ctx context.Context) (MachineStatus, error) {
	fw, _, err := x.firmware(ctx)
	if err != nil {
		return MachineStatus{}, err
	}
	st, err := x.dev.Status(ctx)
	if err != nil {
		return MachineStatus{}, err
	}
	out := MachineStatus{State: xtoolStates[st.State], Progress: st.Progress}
	if out.State == "" {
		out.State = MachineIdle
	}
	for _, r := range st.Readings {
		out.Interlocks = append(out.Interlocks, Interlock{Name: r.Name, OK: r.OK, Message: r.Message})
	}
	out.Interlocks = append(out.Interlocks, fw)
	if fw.OK && fw.Message != "" {
		out.Message = fw.Message
	}
	return out, nil
}

// xtoolStates are the xtool package's states as a Machine's.
var xtoolStates = map[string]string{
	xtool.StateIdle: MachineIdle, xtool.StateFraming: MachineFraming, xtool.StateReady: MachineReady,
	xtool.StateRunning: MachineRunning, xtool.StatePaused: MachinePaused, xtool.StateDone: MachineDone,
	xtool.StateFailed: MachineFailed,
}

// refuseFirmware is why the machine is not moved, when its firmware is not
// one BAM! knows and the printer does not allow it.
func (x *xtoolMachine) refuseFirmware(ctx context.Context) error {
	_, why, err := x.firmware(ctx)
	if err != nil {
		return err
	}
	if why != "" {
		return fmt.Errorf("refused: %s", why)
	}
	return nil
}

func (x *xtoolMachine) Frame(ctx context.Context, job *Job) error {
	if !x.model.Frames {
		return fmt.Errorf("the %s cannot frame a job", x.model.ID)
	}
	if err := x.refuseFirmware(ctx); err != nil {
		return err
	}
	o := job.Outline()
	gcode, err := xtool.FrameGCode(x.model.WorkWidthMM, x.model.WorkHeightMM, x.model.MirrorX, o.X, o.Y, o.Width, o.Height)
	if err != nil {
		return err
	}
	return x.dev.Frame(ctx, gcode)
}

func (x *xtoolMachine) Start(ctx context.Context, job *Job) error {
	if err := x.refuseFirmware(ctx); err != nil {
		return err
	}
	gcode, err := xtool.GCode(xtoolProgram(x.model, job))
	if err != nil {
		return err
	}
	return x.dev.SendJob(ctx, gcode, !x.model.StartButton)
}

// xtoolProgram is a job as the xtool package writes it.
func xtoolProgram(m xtool.Model, job *Job) xtool.Program {
	p := xtool.Program{WidthMM: m.WorkWidthMM, HeightMM: m.WorkHeightMM, MirrorX: m.MirrorX}
	if job.Engrave != nil {
		e := job.Material.Engrave
		p.Raster = &xtool.Raster{X: job.Label.X, Y: job.Label.Y, Width: job.Label.Width, Height: job.Label.Height, Dots: job.Engrave}
		p.Engrave = xtool.Setting{Power: e.Power, Speed: e.Speed, Passes: e.Passes}
	}
	if c := job.Material.Cut; c != nil && len(job.Cuts) > 0 {
		p.Cut = xtool.Setting{Power: c.Power, Speed: c.Speed, Passes: c.Passes}
		for _, cut := range job.Cuts {
			path := xtool.Path{Closed: cut.Closed}
			for _, pt := range cut.Points {
				path.Points = append(path.Points, xtool.Point{X: pt.X, Y: pt.Y})
			}
			p.Paths = append(p.Paths, path)
		}
	}
	return p
}

func (x *xtoolMachine) Pause(ctx context.Context) error {
	return x.dev.Control(ctx, xtool.ActionPause)
}

func (x *xtoolMachine) Resume(ctx context.Context) error {
	if err := x.refuseFirmware(ctx); err != nil {
		return err
	}
	return x.dev.Control(ctx, xtool.ActionResume)
}

// Cancel stops the job, whatever the firmware: stopping is never refused.
func (x *xtoolMachine) Cancel(ctx context.Context) error {
	return x.dev.Control(ctx, xtool.ActionStop)
}
