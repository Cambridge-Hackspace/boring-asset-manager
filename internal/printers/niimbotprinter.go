package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
)

// DriverNiimbot drives Niimbot's thermal label printers.
const DriverNiimbot = "niimbot"

// ProtocolNiimbot is the conversation a Niimbot printer's connection holds
// (niimbot.Converse): each command waits for the printer's answer.
const ProtocolNiimbot = "niimbot"

// niimbotDriver adapts package niimbot, which knows nothing of this
// registry.
type niimbotDriver struct{}

// niimbotOptions are the niimbot driver options.
type niimbotOptions struct {
	Density   *int    `json:"density,omitempty"`
	PrintTask *string `json:"print_task,omitempty"`
}

func (niimbotDriver) Info() DriverInfo {
	names := niimbot.Models()
	tasks := make([]string, 0, len(niimbot.Tasks()))
	for _, t := range niimbot.Tasks() {
		tasks = append(tasks, string(t))
	}
	info := DriverInfo{
		Driver: DriverNiimbot, Models: names, ModelDPI: map[string]int{},
		Options: []OptionInfo{{
			Name: "density", Type: "int", Default: nil,
			Description: "Print darkness: 1-3 on the D11, D110, D101 and B18, and 1-5 on the rest; unset is the model's default, 2 or 3.",
		}, {
			Name: "print_task", Type: "string", Default: nil, Values: tasks,
			Description: "The sequence of commands the printer's firmware takes; unset is the one known for the model. Change it only for firmware that prints nothing with it.",
		}},
		CustomStock: &CustomStockInfo{
			MaxWidthMM: map[string]float64{}, MinLengthMM: niimbot.MinContinuousMM, MaxLengthMM: niimbot.MaxContinuousMM,
			Note: "Most Niimbot printers print only on Niimbot's own rolls, whose RFID tags they read: give the size printed on the roll.",
		},
	}
	for _, n := range names {
		m, _ := niimbot.LookupModel(n)
		info.ModelDPI[n] = m.DPI
		info.CustomStock.MaxWidthMM[n] = m.MaxRollMM
	}
	for _, id := range niimbot.MediaIDs() {
		m, _ := niimbot.LookupMedia(id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()}
		for _, n := range names {
			if mo, _ := niimbot.LookupModel(n); mo.Takes(m) == nil {
				mi.Models = append(mi.Models, n)
			}
		}
		info.Media = append(info.Media, mi)
	}
	return info
}

// BLE is Niimbot's Bluetooth LE profile, which most of its printers have:
// one characteristic jobs are written to and answers are notified on, in
// conversation (ProtocolNiimbot).
func (niimbotDriver) BLE() BLEProfile {
	const c = "bef8d6c9-9c21-4c9e-b632-bd58c1009f9f"
	return BLEProfile{Service: "e7810a71-73ae-499d-8c15-faa9aef0c3f2", Write: c, Notify: c, Protocol: ProtocolNiimbot}
}

// Protocol is the conversation a serial device connection holds.
func (niimbotDriver) Protocol() string { return ProtocolNiimbot }

// settings resolves a printer's settings: its model, roll and options.
func (niimbotDriver) settings(s Settings) (niimbot.Model, niimbot.Media, niimbot.JobOptions, error) {
	model, ok := niimbot.LookupModel(s.Model)
	if !ok {
		return model, niimbot.Media{}, niimbot.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(niimbot.Models(), ", "))
	}
	media, ok := niimbot.LookupMedia(s.Media)
	if s.Stock != nil {
		// The printer is told only whether the roll has gaps, and finds them
		// itself, so any roll the model takes will do.
		media, ok = niimbot.SizedMedia(model, s.Stock.ID, s.Stock.Name, s.Stock.WidthMM, s.Stock.LengthMM), true
	}
	if !ok {
		return model, media, niimbot.JobOptions{}, fmt.Errorf("media must be one of %s, or custom stock", strings.Join(niimbot.MediaIDs(), ", "))
	}
	if err := model.Takes(media); err != nil {
		return model, media, niimbot.JobOptions{}, err
	}
	opts := niimbot.JobOptions{Density: model.Density, Task: model.Task, LabelType: media.LabelType()}
	if len(s.Options) > 0 && string(s.Options) != "null" {
		var o niimbotOptions
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return model, media, opts, fmt.Errorf("options: %v", err)
		}
		if o.Density != nil {
			opts.Density = *o.Density
		}
		if o.PrintTask != nil {
			opts.Task = niimbot.Task(*o.PrintTask)
		}
	}
	if err := opts.Check(model); err != nil {
		return model, media, opts, fmt.Errorf("options: %v", err)
	}
	return model, media, opts, nil
}

func (d niimbotDriver) Validate(s Settings) error {
	_, _, _, err := d.settings(s)
	return err
}

func (d niimbotDriver) Geometry(s Settings) (Geometry, error) {
	model, media, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	return Geometry{
		DPI: model.DPI, Prints: "the " + model.Name + " prints", Roll: media.Name,
		WidthMM: media.WidthMM, LengthMM: media.LengthMM,
		MinLengthMM: niimbot.MinContinuousMM, MaxLengthMM: niimbot.MaxContinuousMM,
		// A label wider than the head is cut to it around its center.
		ReachMM: model.HeadMM(),
	}, nil
}

func (d niimbotDriver) Plan(s Settings, f Fit) (Plan, error) {
	model, _, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return niimbotPlan{model: model, placement: niimbot.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type niimbotPlan struct {
	model     niimbot.Model
	placement niimbot.Placement
	opts      niimbot.JobOptions
}

func (p niimbotPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = niimbot.Prepare(p.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return niimbot.EncodeJob(p.model, pages, p.opts)
}
