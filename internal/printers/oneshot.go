package printers

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// One-shot printers (docs/printing.md#lasers) make one piece a job, on
// whatever a person has put on the machine's bed: a laser engraving a label
// onto a workpiece, and cutting along its outline. There is no roll. A
// driver reports each model's work area and the materials it takes, with a
// preset for engraving and cutting each; a job names its material, the
// workpiece's size and place on the bed, and where the label goes on it.
//
// A one-shot printer is never printed to as a roll printer is: the print
// queue, its pools and its batches never offer one, and printing to one is
// refused. A job is prepared, framed and started one at a time, and starting
// one is for a person at the machine (the safety model, in the docs).

// OneShotDriver is a Driver for one-shot printers.
type OneShotDriver interface {
	Driver
	// OneShotModels describes each model: its work area, its materials and
	// what the machine does for itself.
	OneShotModels() []OneShotModel
	// Machine reaches the machine a printer names, to frame, start and
	// watch its jobs.
	Machine(c Connection, s Settings) (Machine, error)
}

// OneShotModel describes one model of a one-shot printer.
type OneShotModel struct {
	ID string `json:"id"`
	// WorkWidthMM and WorkHeightMM are the work area: how far the head
	// reaches across and down the bed, from its top left corner.
	WorkWidthMM  float64 `json:"work_width_mm"`
	WorkHeightMM float64 `json:"work_height_mm"`
	// Materials are the presets the driver starts each printer with, which
	// an admin may tune (docs/printing.md#materials).
	Materials []Material `json:"materials"`
	// StartButton is set when the machine starts a job sent to it ready
	// with its own button, rather than when BAM! says start.
	StartButton bool `json:"start_button"`
	// Frames is set when the machine can trace a job's outline with its
	// low-power pointer.
	Frames bool `json:"frames"`
	// Interlocks are those the machine reports, which are checked before
	// a job starts: InterlockLid and the rest.
	Interlocks []string `json:"interlocks"`
}

// Interlocks a machine may report.
const (
	InterlockLid       = "lid"
	InterlockSafetyKey = "safety_key"
	InterlockFlame     = "flame"
	InterlockExhaust   = "exhaust"
)

// Material is what a workpiece is made of, and how a printer engraves and
// cuts it.
type Material struct {
	// ID names it among a printer's materials: lowercase letters, digits
	// and hyphens.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Engrave is how labels are engraved on it.
	Engrave EngraveSetting `json:"engrave"`
	// Cut is how outlines are cut through it; nil when the printer cannot
	// cut it, and then a template's cut outline is left uncut.
	Cut *CutSetting `json:"cut,omitempty"`
	// FromBack is set for a material engraved from the back, such as clear
	// acrylic read through its face: its jobs are mirrored.
	FromBack bool `json:"from_back,omitempty"`
}

// EngraveSetting is how a material is engraved: the laser's power, as a
// percentage of the most it gives, how fast the head moves, in millimeters
// a second, how many times it goes over, and how many lines an inch it
// engraves, which is the resolution the label is drawn at.
type EngraveSetting struct {
	Power  float64 `json:"power"`
	Speed  float64 `json:"speed"`
	Passes int     `json:"passes"`
	LPI    int     `json:"lpi"`
}

// CutSetting is how a material is cut: power, speed and passes, as for
// engraving.
type CutSetting struct {
	Power  float64 `json:"power"`
	Speed  float64 `json:"speed"`
	Passes int     `json:"passes"`
}

// Bounds of a material's settings.
const (
	MinLPI     = 50
	MaxLPI     = 1270
	MaxPasses  = 50
	MaxSpeedMM = 2000
	// MaxMaterials bounds how many materials a printer has.
	MaxMaterials = 100
)

// ErrInvalidMaterial is wrapped by every refusal of a material.
var ErrInvalidMaterial = errors.New("invalid material")

var materialID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidateMaterial checks a material's ID, name and settings, returning it
// with its name trimmed. Errors wrap ErrInvalidMaterial.
func ValidateMaterial(m Material) (Material, error) {
	var problems []string
	m.Name = strings.TrimSpace(m.Name)
	if !materialID.MatchString(m.ID) {
		problems = append(problems, "id must be 1-64 lowercase letters, digits and hyphens")
	}
	if m.Name == "" {
		problems = append(problems, "name must be 1-255 characters")
	} else if p := textcheck.Problem(m.Name, textcheck.MaxName); p != "" {
		problems = append(problems, "name "+p)
	}
	check := func(what string, power, speed float64, passes int) {
		if !(power > 0 && power <= 100) {
			problems = append(problems, what+" power must be more than 0 and at most 100 percent")
		}
		if !(speed > 0 && speed <= MaxSpeedMM) {
			problems = append(problems, fmt.Sprintf("%s speed must be more than 0 and at most %d mm a second", what, MaxSpeedMM))
		}
		if passes < 1 || passes > MaxPasses {
			problems = append(problems, fmt.Sprintf("%s passes must be 1-%d", what, MaxPasses))
		}
	}
	check("engraving", m.Engrave.Power, m.Engrave.Speed, m.Engrave.Passes)
	if m.Engrave.LPI < MinLPI || m.Engrave.LPI > MaxLPI {
		problems = append(problems, fmt.Sprintf("engraving lines per inch must be %d-%d", MinLPI, MaxLPI))
	}
	if m.Cut != nil {
		check("cutting", m.Cut.Power, m.Cut.Speed, m.Cut.Passes)
	}
	if len(problems) > 0 {
		return m, fmt.Errorf("%w: %s", ErrInvalidMaterial, strings.Join(problems, "; "))
	}
	return m, nil
}

// IsOneShot reports whether a driver drives one-shot printers.
func IsOneShot(driver string) bool {
	d, err := Lookup(driver)
	if err != nil {
		return false
	}
	_, ok := d.(OneShotDriver)
	return ok
}

// OneShotModelOf is the model a one-shot printer is, as its driver
// describes it.
func OneShotModelOf(driver string, s Settings) (OneShotModel, error) {
	d, err := Lookup(driver)
	if err != nil {
		return OneShotModel{}, err
	}
	od, ok := d.(OneShotDriver)
	if !ok {
		return OneShotModel{}, fmt.Errorf("%w: %s is not a one-shot printer's driver", ErrInvalidPrinter, driver)
	}
	for _, m := range od.OneShotModels() {
		if m.ID == s.Model {
			return m, nil
		}
	}
	return OneShotModel{}, fmt.Errorf("%w: %s has no model %q", ErrInvalidPrinter, driver, s.Model)
}

// MachineFor reaches the machine a one-shot printer names.
func MachineFor(driver string, c Connection, s Settings) (Machine, error) {
	d, err := Lookup(driver)
	if err != nil {
		return nil, err
	}
	od, ok := d.(OneShotDriver)
	if !ok {
		return nil, fmt.Errorf("%w: %s is not a one-shot printer's driver", ErrInvalidPrinter, driver)
	}
	c, err = NormalizeConnection(c)
	if err != nil {
		return nil, err
	}
	return od.Machine(c, s)
}

// errOneShot is why a one-shot printer is not printed to.
var errOneShot = fmt.Errorf("%w: it makes one piece a job, prepared and started from an asset's page or the template editor, not printed to", ErrDoesNotFit)

// checkOneShot checks the parts of a one-shot printer's definition every
// one-shot driver shares: no roll, no relay, and a model the driver has.
func checkOneShot(d OneShotDriver, c Connection, s Settings) error {
	if c.Relay != "" {
		return fmt.Errorf("%w: connection: a %s is reached by this server, not through a relay", ErrInvalidPrinter, d.Info().Driver)
	}
	if s.Media != "" || s.Stock != nil {
		return fmt.Errorf("%w: a one-shot printer has no media: what it works on is chosen for each job", ErrInvalidPrinter)
	}
	return nil
}

// OneShotGeometry is what the fit check takes from a one-shot printer: its
// work area, which a label must fit within. It is laid out at any
// resolution, since it is engraved at its material's.
func OneShotGeometry(m OneShotModel, name string) Geometry {
	return Geometry{OneShot: true, Prints: name, Roll: "the work area", WidthMM: m.WorkWidthMM, LengthMM: m.WorkHeightMM}
}

// Machine is a one-shot printer's machine, as its driver reaches it.
type Machine interface {
	// Status is what the machine is doing, and its interlocks.
	Status(ctx context.Context) (MachineStatus, error)
	// Frame traces the job's outline with the low-power pointer, so the
	// person at the machine can check where it goes.
	Frame(ctx context.Context, job *Job) error
	// Start sends the job and starts it, or, on a machine started with
	// its own button, sends it ready for the button.
	Start(ctx context.Context, job *Job) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Cancel(ctx context.Context) error
}

// Machine states.
const (
	MachineIdle    = "idle"
	MachineFraming = "framing"
	// MachineReady is a job sent ready, waiting for the machine's button.
	MachineReady     = "ready"
	MachineRunning   = "running"
	MachinePaused    = "paused"
	MachineDone      = "done"
	MachineCancelled = "cancelled"
	MachineFailed    = "failed"
)

// MachineStatus is what a machine reports.
type MachineStatus struct {
	State string `json:"state"`
	// Progress is how much of the job is done, from 0 to 1.
	Progress float64 `json:"progress"`
	// Interlocks are the machine's, each OK or saying what is wrong.
	Interlocks []Interlock `json:"interlocks"`
	Message    string      `json:"message,omitempty"`
}

// Interlock is one of a machine's safety checks.
type Interlock struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Problems are what the machine's interlocks report wrong, in words.
func (s MachineStatus) Problems() []string {
	var out []string
	for _, i := range s.Interlocks {
		if !i.OK {
			msg := i.Message
			if msg == "" {
				msg = InterlockName(i.Name) + " reports a problem"
			}
			out = append(out, msg)
		}
	}
	return out
}

// Busy reports whether the machine is working on a job, or holding one.
func (s MachineStatus) Busy() bool {
	return s.State == MachineFraming || s.State == MachineRunning || s.State == MachinePaused || s.State == MachineReady
}

// InterlockName is an interlock as people say it.
func InterlockName(name string) string {
	switch name {
	case InterlockLid:
		return "The lid"
	case InterlockSafetyKey:
		return "The safety key"
	case InterlockFlame:
		return "Flame detection"
	case InterlockExhaust:
		return "The exhaust"
	}
	return name
}

// PointMM is a point on a label or a machine's bed, in millimeters.
type PointMM struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Polyline is a path to cut: its points in order, back to the first when
// Closed.
type Polyline struct {
	Points []PointMM `json:"points"`
	Closed bool      `json:"closed,omitempty"`
}

// Job is one piece for a one-shot printer: what is engraved and cut, where
// on the bed, and how.
type Job struct {
	Model    string
	Material Material
	// Workpiece and Label are where the workpiece lies on the bed, and
	// where the label goes on it, in millimeters from the work area's top
	// left corner.
	Workpiece RectMM
	Label     RectMM
	// Mirror is set when the material is engraved from the back: the
	// label is mirrored, left for right, within its place.
	Mirror bool
	// Engrave is the label as engraved, 1-bit at the material's lines per
	// inch, black burned, covering Label; nil when nothing is engraved.
	Engrave *image.Gray
	// Cuts are cut after engraving, on the bed.
	Cuts []Polyline
}

// Outline is the part of the bed the job reaches: the label's place, and
// any cut beyond it. Framing traces it.
func (j *Job) Outline() RectMM {
	x0, y0, x1, y1 := j.Label.X, j.Label.Y, j.Label.X+j.Label.Width, j.Label.Y+j.Label.Height
	for _, c := range j.Cuts {
		for _, p := range c.Points {
			x0, y0, x1, y1 = math.Min(x0, p.X), math.Min(y0, p.Y), math.Max(x1, p.X), math.Max(y1, p.Y)
		}
	}
	return RectMM{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// JobInput is what a job is made from: the label, drawn and outlined, and
// where it goes.
type JobInput struct {
	Material Material
	// Workpiece is its size and its place on the bed.
	Workpiece RectMM
	// LabelAt is where the label's top left corner goes on the
	// workpiece, from its top left corner; nil centers it.
	LabelAt *PointMM
	// LabelWidthMM and LabelHeightMM are the label's size.
	LabelWidthMM, LabelHeightMM float64
	// Engrave is the label drawn 1-bit at the material's lines per inch,
	// without its cut outlines.
	Engrave *image.Gray
	// Cuts are its cut outlines, in millimeters on the label.
	Cuts []Polyline
}

// ErrDoesNotFitWorkpiece is wrapped when a job's label does not fit its
// workpiece, or its workpiece the work area.
var ErrDoesNotFitWorkpiece = errors.New("the job does not fit")

// MakeJob checks a job against its machine's work area and makes it: the
// label placed on the workpiece, mirrored when its material is engraved
// from the back, and its cut outlines moved onto the bed. Its warnings are
// for the person starting it. Errors wrap ErrDoesNotFitWorkpiece.
func MakeJob(model OneShotModel, in JobInput) (*Job, []string, error) {
	var reasons, warnings []string
	w := in.Workpiece
	if !(w.Width > 0 && w.Height > 0) {
		reasons = append(reasons, "the workpiece's width and height must be more than 0 mm")
	}
	if w.X < -SizeTolerance || w.Y < -SizeTolerance || w.X+w.Width > model.WorkWidthMM+SizeTolerance || w.Y+w.Height > model.WorkHeightMM+SizeTolerance {
		reasons = append(reasons, fmt.Sprintf("the workpiece reaches outside the work area, which is %s × %s mm", mm(model.WorkWidthMM), mm(model.WorkHeightMM)))
	}
	at := PointMM{X: (w.Width - in.LabelWidthMM) / 2, Y: (w.Height - in.LabelHeightMM) / 2}
	if in.LabelAt != nil {
		at = *in.LabelAt
	}
	if in.LabelWidthMM > w.Width+SizeTolerance || in.LabelHeightMM > w.Height+SizeTolerance {
		reasons = append(reasons, fmt.Sprintf("the label is %s × %s mm, larger than the %s × %s mm workpiece", mm(in.LabelWidthMM), mm(in.LabelHeightMM), mm(w.Width), mm(w.Height)))
	} else if at.X < -SizeTolerance || at.Y < -SizeTolerance || at.X+in.LabelWidthMM > w.Width+SizeTolerance || at.Y+in.LabelHeightMM > w.Height+SizeTolerance {
		reasons = append(reasons, "the label reaches off the workpiece where it is placed")
	}
	if len(reasons) > 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrDoesNotFitWorkpiece, strings.Join(reasons, "; "))
	}
	job := &Job{
		Model: model.ID, Material: in.Material, Workpiece: w, Mirror: in.Material.FromBack,
		Label: RectMM{X: w.X + at.X, Y: w.Y + at.Y, Width: in.LabelWidthMM, Height: in.LabelHeightMM},
	}
	if in.Engrave != nil && !blank(in.Engrave) {
		job.Engrave = in.Engrave
		if job.Mirror {
			job.Engrave = mirrorAcross(in.Engrave)
		}
	}
	switch {
	case len(in.Cuts) > 0 && in.Material.Cut == nil:
		warnings = append(warnings, fmt.Sprintf("%s is not cut on this printer, so the label's cut outline is left uncut.", in.Material.Name))
	case len(in.Cuts) > 0:
		for _, c := range in.Cuts {
			out := Polyline{Closed: c.Closed, Points: make([]PointMM, len(c.Points))}
			for i, p := range c.Points {
				if job.Mirror {
					p.X = in.LabelWidthMM - p.X
				}
				out.Points[i] = PointMM{X: job.Label.X + p.X, Y: job.Label.Y + p.Y}
			}
			job.Cuts = append(job.Cuts, out)
		}
	}
	if job.Engrave == nil && len(job.Cuts) == 0 {
		return nil, nil, fmt.Errorf("%w: the label has nothing to engrave or cut", ErrDoesNotFitWorkpiece)
	}
	if o := job.Outline(); o.X < -SizeTolerance || o.Y < -SizeTolerance || o.X+o.Width > model.WorkWidthMM+SizeTolerance || o.Y+o.Height > model.WorkHeightMM+SizeTolerance {
		return nil, nil, fmt.Errorf("%w: the cut outline reaches outside the work area", ErrDoesNotFitWorkpiece)
	}
	if job.Mirror {
		warnings = append(warnings, fmt.Sprintf("%s is engraved from the back, so the label is mirrored: lay the workpiece face down.", in.Material.Name))
	}
	return job, warnings, nil
}

// blank reports whether a 1-bit image has nothing black on it.
func blank(img *image.Gray) bool {
	return !slices.ContainsFunc(img.Pix, func(v uint8) bool { return v < 0x80 })
}

// mirrorAcross is an image mirrored left for right.
func mirrorAcross(img *image.Gray) *image.Gray {
	b := img.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		row := img.Pix[(y)*img.Stride : y*img.Stride+b.Dx()]
		dst := out.Pix[y*out.Stride : y*out.Stride+b.Dx()]
		for x, v := range row {
			dst[b.Dx()-1-x] = v
		}
	}
	return out
}

// LabelDots is how many dots a label of a size is drawn in at a material's
// lines per inch.
func LabelDots(widthMM, heightMM float64, lpi int) (int, int) {
	return max(1, int(math.Round(widthMM/25.4*float64(lpi)))), max(1, int(math.Round(heightMM/25.4*float64(lpi))))
}
