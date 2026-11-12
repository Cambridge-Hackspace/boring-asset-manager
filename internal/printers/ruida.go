package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ruida"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/serialport"
)

// DriverRuida drives CO2 lasers on Ruida controllers (docs/printing.md#ruida),
// over Ethernet or USB. BAM! uploads a job to the controller's memory, and
// the person at the machine starts it from the controller's panel: BAM!
// never starts a Ruida.
const DriverRuida = "ruida"

type ruidaDriver struct{}

func init() {
	if err := Register(ruidaDriver{}); err != nil {
		panic(err)
	}
}

// Ruida models: CO2 lasers by their tube's power, which their presets and
// default work areas follow. The work area is set for each printer.
const (
	RuidaCO240W  = "co2-40w"
	RuidaCO260W  = "co2-60w"
	RuidaCO280W  = "co2-80w"
	RuidaCO2100W = "co2-100w"
)

// Defaults of the ruida driver's options.
const (
	ruidaDefaultOrigin = "top_right"
	ruidaDefaultFile   = "BAM"
	ruidaUSBBaud       = 115200
)

// ruidaUDPTiming is how long controllers are waited for over UDP; tests
// shorten it.
var ruidaUDPTiming = ruida.DefaultUDPTiming

// ruidaFileName is a name the controller keeps: up to eight letters and
// digits.
var ruidaFileName = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)

type ruidaOptions struct {
	bedOptions
	Magic     *int    `json:"magic"`
	FileName  *string `json:"file_name"`
	ReplyPort *int    `json:"reply_port"`
}

// ruidaModels are the models, with their default work areas.
var ruidaModels = []struct {
	id   string
	w, h float64
}{{RuidaCO240W, 500, 300}, {RuidaCO260W, 600, 400}, {RuidaCO280W, 700, 500}, {RuidaCO2100W, 900, 600}}

// ruidaMaterials are a model's presets: starting points to try on scrap.
func ruidaMaterials(model string) []Material {
	type preset struct{ ply, plyCut, acr, acrCut, card, cardCut [2]float64 }
	p := map[string]preset{
		RuidaCO240W:  {[2]float64{25, 250}, [2]float64{65, 12}, [2]float64{18, 250}, [2]float64{70, 8}, [2]float64{10, 300}, [2]float64{18, 60}},
		RuidaCO260W:  {[2]float64{20, 300}, [2]float64{60, 15}, [2]float64{15, 300}, [2]float64{65, 10}, [2]float64{8, 400}, [2]float64{15, 80}},
		RuidaCO280W:  {[2]float64{18, 350}, [2]float64{55, 20}, [2]float64{12, 350}, [2]float64{60, 12}, [2]float64{6, 450}, [2]float64{12, 100}},
		RuidaCO2100W: {[2]float64{15, 400}, [2]float64{50, 25}, [2]float64{10, 400}, [2]float64{55, 15}, [2]float64{5, 500}, [2]float64{10, 120}},
	}[model]
	return []Material{
		{ID: "birch-plywood-3mm", Name: "Birch plywood, 3 mm", Engrave: EngraveSetting{Power: p.ply[0], Speed: p.ply[1], Passes: 1, LPI: 254}, Cut: &CutSetting{Power: p.plyCut[0], Speed: p.plyCut[1], Passes: 1}},
		{ID: "clear-acrylic-3mm", Name: "Clear acrylic, 3 mm", Engrave: EngraveSetting{Power: p.acr[0], Speed: p.acr[1], Passes: 1, LPI: 254}, Cut: &CutSetting{Power: p.acrCut[0], Speed: p.acrCut[1], Passes: 1}, FromBack: true},
		{ID: "cardstock", Name: "Cardstock", Engrave: EngraveSetting{Power: p.card[0], Speed: p.card[1], Passes: 1, LPI: 254}, Cut: &CutSetting{Power: p.cardCut[0], Speed: p.cardCut[1], Passes: 1}},
	}
}

func (ruidaDriver) OneShotModels() []OneShotModel {
	var out []OneShotModel
	for _, m := range ruidaModels {
		// A Ruida starts with its own button, and frames from its panel;
		// BAM! reads none of its interlocks, which it enforces itself.
		out = append(out, OneShotModel{ID: m.id, WorkWidthMM: m.w, WorkHeightMM: m.h, Materials: ruidaMaterials(m.id), StartButton: true, Interlocks: []string{}})
	}
	return out
}

func (d ruidaDriver) OneShotModelFor(s Settings) (OneShotModel, error) {
	m, o, err := d.settings(s)
	if err != nil {
		return m, err
	}
	return o.sized(m), nil
}

func (d ruidaDriver) Info() DriverInfo {
	info := DriverInfo{Driver: DriverRuida, ModelDPI: map[string]int{}, Media: []MediaInfo{}}
	for _, m := range d.OneShotModels() {
		info.Models = append(info.Models, m.ID)
		info.ModelDPI[m.ID] = m.Materials[0].Engrave.LPI
	}
	info.Options = append(bedOptionInfo(ruidaDefaultOrigin), OptionInfo{
		Name: "file_name", Type: "string", Default: ruidaDefaultFile,
		Description: "The name a job is kept under in the controller's memory, to choose on its panel: 1-8 letters and digits.",
	}, OptionInfo{
		Name: "magic", Type: "int", Default: ruida.DefaultMagic,
		Description: "The controller's swizzle key: 136 (0x88) for most, such as the 644XG and 644XS; 17 (0x11) for the 634XG; 56 (0x38) for the RDL9635.",
	}, OptionInfo{
		Name: "reply_port", Type: "int", Default: ruida.PortReply,
		Description: "The port on this server the controller answers to, over Ethernet: 40200, as the controller has it; 0 takes any.",
	})
	return info
}

func (ruidaDriver) settings(s Settings) (OneShotModel, ruidaOptions, error) {
	var model OneShotModel
	found := false
	for _, m := range (ruidaDriver{}).OneShotModels() {
		if m.ID == s.Model {
			model, found = m, true
		}
	}
	if !found {
		return model, ruidaOptions{}, fmt.Errorf("model must be one of %s, %s, %s, %s", RuidaCO240W, RuidaCO260W, RuidaCO280W, RuidaCO2100W)
	}
	var o ruidaOptions
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
	case o.Magic != nil && (*o.Magic < 0 || *o.Magic > 255):
		return model, o, errors.New("options: magic must be 0-255")
	case o.FileName != nil && !ruidaFileName.MatchString(*o.FileName):
		return model, o, errors.New("options: file_name must be 1-8 letters and digits")
	case o.ReplyPort != nil && (*o.ReplyPort < 0 || *o.ReplyPort > 65535):
		return model, o, errors.New("options: reply_port must be 0-65535")
	}
	return model, o, nil
}

func (d ruidaDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d ruidaDriver) Geometry(s Settings) (Geometry, error) {
	m, err := d.OneShotModelFor(s)
	if err != nil {
		return Geometry{}, err
	}
	return OneShotGeometry(m, "the Ruida laser"), nil
}

func (ruidaDriver) Plan(Settings, Fit) (Plan, error) { return nil, errOneShot }

// Machine reaches the controller: over tcp, its address on the network,
// spoken to in UDP on port 50200 unless another is given; over a device
// connection, its USB serial port.
func (d ruidaDriver) Machine(c Connection, s Settings) (Machine, error) {
	m, o, err := d.settings(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPrinter, err)
	}
	magic := byte(ruida.DefaultMagic)
	if o.Magic != nil {
		magic = byte(*o.Magic)
	}
	x := &ruidaMachine{model: o.sized(m), file: ruidaDefaultFile}
	x.flipX, x.flipY = originFlips(o.origin(ruidaDefaultOrigin))
	if o.FileName != nil {
		x.file = *o.FileName
	}
	switch c.Type {
	case ConnTCP:
		host, portText, err := net.SplitHostPort(c.Address)
		if err != nil {
			return nil, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
		}
		if port, _ := strconv.Atoi(portText); port == DefaultPort {
			portText = strconv.Itoa(ruida.PortController)
		}
		reply := ruida.PortReply
		if o.ReplyPort != nil {
			reply = *o.ReplyPort
		}
		addr := net.JoinHostPort(host, portText)
		x.key = "udp:" + addr
		x.link = &ruida.UDP{Addr: addr, LocalPort: reply, Magic: magic, Timing: ruidaUDPTiming}
	case ConnDevice:
		path := c.Address
		x.key = "device:" + path
		x.link = &ruida.Stream{Key: path, Magic: magic, Open: func() (io.ReadWriteCloser, error) {
			return serialport.Open(path, ruidaUSBBaud, true)
		}}
	default:
		return nil, fmt.Errorf("%w: a Ruida controller takes a tcp connection, its address on the network, or a device one, its USB serial port", ErrInvalidPrinter)
	}
	return x, nil
}

// ruidaState is what BAM! knows of a controller's job beyond what it
// reports: that one was sent ready, and seen running.
type ruidaState struct {
	ready, seen, paused, done bool
}

var (
	ruidaMu     sync.Mutex
	ruidaStates = map[string]*ruidaState{}
)

// ResetRuida forgets every controller's job, for tests.
func ResetRuida() {
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	ruidaStates = map[string]*ruidaState{}
}

// ruidaMachine is a Ruida controller, as a one-shot printer's Machine.
type ruidaMachine struct {
	model        OneShotModel
	flipX, flipY bool
	key          string
	file         string
	link         ruida.Link
}

// state is the controller's, held with ruidaMu.
func (x *ruidaMachine) state() *ruidaState {
	s, ok := ruidaStates[x.key]
	if !ok {
		s = &ruidaState{}
		ruidaStates[x.key] = s
	}
	return s
}

func (x *ruidaMachine) running(ctx context.Context) (bool, error) {
	v, err := x.link.Read(ctx, ruida.MemMachineStatus)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return v&ruida.MachineRunning != 0, nil
}

// Status is ready from sending a job until the controller runs it, running
// while it does, and done once it stops; BAM! knows no more of a job, and
// none of the controller's interlocks.
func (x *ruidaMachine) Status(ctx context.Context) (MachineStatus, error) {
	running, err := x.running(ctx)
	if err != nil {
		return MachineStatus{}, err
	}
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	s := x.state()
	out := MachineStatus{State: MachineIdle, Interlocks: []Interlock{}}
	switch {
	case running && s.paused:
		out.State = MachinePaused
		s.ready, s.seen = false, true
	case running:
		out.State = MachineRunning
		s.ready, s.seen = false, true
	case s.paused:
		out.State = MachinePaused
	case s.ready:
		out.State = MachineReady
		out.Message = fmt.Sprintf("Sent to the controller as %s: choose it on the panel and press Start", x.file)
	case s.seen || s.done:
		out.State, out.Progress = MachineDone, 1
		s.seen, s.done = false, true
	}
	return out, nil
}

// Frame is the panel's: BAM! does not frame a Ruida.
func (x *ruidaMachine) Frame(context.Context, *Job) error {
	return errors.New("a Ruida controller frames a job from its panel's Frame key, once the job is sent")
}

// Start uploads the job to the controller's memory, ready for the person at
// the machine to start from the panel. It never starts it.
func (x *ruidaMachine) Start(ctx context.Context, job *Job) error {
	running, err := x.running(ctx)
	if err != nil {
		return err
	}
	if running {
		return errors.New("the controller is running a job")
	}
	file, err := ruida.File(ruidaProgram(ruida.Bed{WidthMM: x.model.WorkWidthMM, HeightMM: x.model.WorkHeightMM, FlipX: x.flipX, FlipY: x.flipY}, job))
	if err != nil {
		return err
	}
	if err := x.link.Send(ctx, ruida.Upload(x.file, file)); err != nil {
		return fmt.Errorf("%w: sending the job: %v", ErrUnreachable, err)
	}
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	*x.state() = ruidaState{ready: true}
	return nil
}

// ruidaProgram is a job as the ruida package writes it.
func ruidaProgram(bed ruida.Bed, job *Job) ruida.Program {
	p := ruida.Program{Bed: bed}
	if job.Engrave != nil {
		e := job.Material.Engrave
		p.Raster = &ruida.Raster{X: job.Label.X, Y: job.Label.Y, Width: job.Label.Width, Height: job.Label.Height, Dots: job.Engrave}
		p.Engrave = ruida.Setting{Power: e.Power, Speed: e.Speed, Passes: e.Passes}
	}
	if cut := job.Material.Cut; cut != nil && len(job.Cuts) > 0 {
		p.Cut = ruida.Setting{Power: cut.Power, Speed: cut.Speed, Passes: cut.Passes}
		for _, line := range job.Cuts {
			path := ruida.Path{Closed: line.Closed}
			for _, pt := range line.Points {
				path.Points = append(path.Points, ruida.Point{X: pt.X, Y: pt.Y})
			}
			p.Paths = append(p.Paths, path)
		}
	}
	return p
}

func (x *ruidaMachine) Pause(ctx context.Context) error {
	if err := x.link.Send(ctx, ruida.PauseProcess()); err != nil {
		return err
	}
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	x.state().paused = true
	return nil
}

// Resume goes on with a job paused, as the panel's Start does.
func (x *ruidaMachine) Resume(ctx context.Context) error {
	if err := x.link.Send(ctx, ruida.RestoreProcess()); err != nil {
		return err
	}
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	x.state().paused = false
	return nil
}

// Cancel stops the job. One sent ready and not started stays in the
// controller's memory, under its name: BAM! cannot take it back.
func (x *ruidaMachine) Cancel(ctx context.Context) error {
	if err := x.link.Send(ctx, ruida.StopProcess()); err != nil {
		return err
	}
	ruidaMu.Lock()
	defer ruidaMu.Unlock()
	*x.state() = ruidaState{}
	return nil
}
