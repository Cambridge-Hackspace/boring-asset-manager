package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolm"
)

// DriverDymoLabelManager drives DYMO LabelManager tape printers.
const DriverDymoLabelManager = "dymo_labelmanager"

// dymoLabelManager adapts package dymolm, which knows nothing of this
// registry.
type dymoLabelManager struct{}

// dymoLMOptions are the dymo_labelmanager driver options.
type dymoLMOptions struct {
	TapeType *string `json:"tape_type,omitempty"`
	CutAfter *string `json:"cut_after,omitempty"`
	MarginMM *int    `json:"margin_mm,omitempty"`
}

func (dymoLabelManager) Info() DriverInfo {
	names := dymolm.Models()
	info := DriverInfo{
		Driver: DriverDymoLabelManager, Models: names, ModelDPI: map[string]int{},
		Options: []OptionInfo{{
			Name: "tape_type", Type: "string", Default: string(dymolm.TapeBlackOnWhite), Values: stringsOf(dymolm.TapeTypes()),
			Description: "The tape's colors, which set how hot the head prints.",
		}, {
			Name: "cut_after", Type: "string", Default: string(dymolm.CutEach), Values: []string{string(dymolm.CutEach), string(dymolm.CutEnd)},
			Description: "Cut each label off as it finishes, or only the job's last, with a cut mark printed between the others. The 500TS and Wireless PnP only: the others print the mark.",
		}, {
			Name: "margin_mm", Type: "int", Default: nil,
			Description: "Tape fed before and after each label, in millimeters: from the distance between the head and the cutter (about 8 mm) to 127; unset is DYMO's, about 10.6 mm.",
		}},
	}
	for _, n := range names {
		m, _ := dymolm.LookupModel(n)
		info.ModelDPI[n] = m.DPI
	}
	for _, id := range dymolm.MediaIDs() {
		m, _ := dymolm.LookupMedia(id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM}
		for _, n := range names {
			if mo, _ := dymolm.LookupModel(n); mo.Takes(m) == nil {
				mi.Models = append(mi.Models, n)
			}
		}
		if len(mi.Models) == len(names) {
			mi.Models = nil
		}
		info.Media = append(info.Media, mi)
	}
	return info
}

// settings resolves a printer's settings: where its cassette falls on the
// head, and its options.
func (dymoLabelManager) settings(s Settings) (dymolm.Layout, dymolm.JobOptions, error) {
	model, ok := dymolm.LookupModel(s.Model)
	if !ok {
		return dymolm.Layout{}, dymolm.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(dymolm.Models(), ", "))
	}
	// Placing a label on the tape takes DYMO's figures for its width.
	if s.Stock != nil {
		return dymolm.Layout{}, dymolm.JobOptions{}, errNoCustomStock("DYMO LabelManager printers")
	}
	media, ok := dymolm.LookupMedia(s.Media)
	if !ok {
		return dymolm.Layout{}, dymolm.JobOptions{}, fmt.Errorf("media must be one of %s", strings.Join(dymolm.MediaIDs(), ", "))
	}
	layout, err := model.Layout(media)
	if err != nil {
		return layout, dymolm.JobOptions{}, err
	}

	opts := dymolm.DefaultOptions(model)
	if len(s.Options) > 0 && string(s.Options) != "null" {
		var o dymoLMOptions
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return layout, opts, fmt.Errorf("options: %v", err)
		}
		if o.TapeType != nil {
			opts.Tape = dymolm.TapeType(*o.TapeType)
			if !containsValue(dymolm.TapeTypes(), opts.Tape) {
				return layout, opts, fmt.Errorf("options: tape_type must be one of %s", strings.Join(stringsOf(dymolm.TapeTypes()), ", "))
			}
		}
		if o.CutAfter != nil {
			if !model.AutoCut {
				return layout, opts, fmt.Errorf("options: the %s has no automatic cutter, so cut_after does not apply", model.Name)
			}
			opts.Cut = dymolm.Cut(*o.CutAfter)
		}
		if o.MarginMM != nil {
			if *o.MarginMM < 0 || *o.MarginMM > dymolm.MaxMarginMM {
				return layout, opts, fmt.Errorf("options: margin_mm must be 0-%d", dymolm.MaxMarginMM)
			}
			opts.MarginLines = model.Dots(float64(*o.MarginMM))
		}
	}
	if err := opts.Check(model); err != nil {
		return layout, opts, fmt.Errorf("options: %w", err)
	}
	return layout, opts, nil
}

func (d dymoLabelManager) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d dymoLabelManager) Geometry(s Settings) (Geometry, error) {
	l, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	mo := l.Model
	// The template is the printed length; the margins are fed around it,
	// and a short label is lengthened with tape to the model's shortest.
	return Geometry{
		DPI: mo.DPI, Prints: "the " + mo.Name + " prints", Roll: l.Media.Name,
		WidthMM: l.Media.WidthMM, MaxLengthMM: dymolm.MaxLengthMM,
		// The page is cropped to the printable dots around its center.
		ReachMM: mo.MM(l.PrintableDots),
	}, nil
}

func (d dymoLabelManager) Plan(s Settings, f Fit) (Plan, error) {
	l, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return dymoLMPlan{layout: l, placement: dymolm.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type dymoLMPlan struct {
	layout    dymolm.Layout
	placement dymolm.Placement
	opts      dymolm.JobOptions
}

func (p dymoLMPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.layout.Prepare(label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return dymolm.EncodeJob(p.layout, pages, p.opts)
}
