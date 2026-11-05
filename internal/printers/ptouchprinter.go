package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ptouch"
)

// DriverBrotherPTouch drives Brother P-touch tape printers.
const DriverBrotherPTouch = "brother_ptouch"

// ptouchDriver adapts package ptouch, which knows nothing of this registry.
type ptouchDriver struct{}

// ptouchOptions are the brother_ptouch driver options.
type ptouchOptions struct {
	CutAfter *string `json:"cut_after,omitempty"`
	HalfCut  *bool   `json:"half_cut,omitempty"`
	Chain    *bool   `json:"chain,omitempty"`
	Mirror   *bool   `json:"mirror,omitempty"`
	MarginMM *int    `json:"margin_mm,omitempty"`
}

func (ptouchDriver) Info() DriverInfo {
	names := ptouch.Models()
	info := DriverInfo{
		Driver: DriverBrotherPTouch, Models: names, ModelDPI: map[string]int{},
		Options: []OptionInfo{{
			Name: "cut_after", Type: "string", Default: string(ptouch.CutEach), Values: []string{string(ptouch.CutEach), string(ptouch.CutEnd)},
			Description: "Cut each label off as it finishes, or only the job's last.",
		}, {
			Name: "half_cut", Type: "bool", Default: false,
			Description: "Cut through the tape between labels but not its backing, so a job is one strip to peel labels from. Not on the PT-P700 or PT-P710BT.",
		}, {
			Name: "chain", Type: "bool", Default: false,
			Description: "Leave the last label in the printer, so the next job wastes no tape before its first.",
		}, {
			Name: "mirror", Type: "bool", Default: false,
			Description: "Print labels mirrored, to read through clear tape.",
		}, {
			Name: "margin_mm", Type: "int", Default: nil,
			Description: "Tape fed before and after each label, in millimeters: 2-127 at 180 DPI, 1-127 on the PT-P900 series; unset is the least.",
		}},
	}
	for _, n := range names {
		m, _ := ptouch.LookupModel(n)
		info.ModelDPI[n] = m.DPI
	}
	for _, id := range ptouch.MediaIDs() {
		m, _ := ptouch.LookupMedia(id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM}
		for _, n := range names {
			if mo, _ := ptouch.LookupModel(n); mo.Takes(m) == nil {
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
func (ptouchDriver) settings(s Settings) (ptouch.Layout, ptouch.JobOptions, error) {
	model, ok := ptouch.LookupModel(s.Model)
	if !ok {
		return ptouch.Layout{}, ptouch.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(ptouch.Models(), ", "))
	}
	// A P-touch identifies the cassette loaded, and placing a label on the
	// head takes Brother's figures for its width.
	if s.Stock != nil {
		return ptouch.Layout{}, ptouch.JobOptions{}, errNoCustomStock("Brother P-touch printers")
	}
	media, ok := ptouch.LookupMedia(s.Media)
	if !ok {
		return ptouch.Layout{}, ptouch.JobOptions{}, fmt.Errorf("media must be one of %s", strings.Join(ptouch.MediaIDs(), ", "))
	}
	layout, err := model.Layout(media)
	if err != nil {
		return layout, ptouch.JobOptions{}, err
	}

	opts := ptouch.JobOptions{Cut: ptouch.CutEach, MarginDots: model.MinMarginDots}
	if len(s.Options) > 0 && string(s.Options) != "null" {
		var o ptouchOptions
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return layout, opts, fmt.Errorf("options: %v", err)
		}
		if o.CutAfter != nil {
			opts.Cut = ptouch.Cut(*o.CutAfter)
		}
		if o.HalfCut != nil {
			opts.HalfCut = *o.HalfCut
		}
		if o.Chain != nil {
			opts.Chain = *o.Chain
		}
		if o.Mirror != nil {
			opts.Mirror = *o.Mirror
		}
		if o.MarginMM != nil {
			if *o.MarginMM < 0 || *o.MarginMM > 127 {
				return layout, opts, fmt.Errorf("options: margin_mm must be 0-127")
			}
			opts.MarginDots = model.Dots(float64(*o.MarginMM))
		}
	}
	if err := opts.Check(model); err != nil {
		return layout, opts, fmt.Errorf("options: %w", err)
	}
	return layout, opts, nil
}

func (d ptouchDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d ptouchDriver) Geometry(s Settings) (Geometry, error) {
	l, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	mo := l.Model
	// The template is the printed length; the margins are fed around it.
	return Geometry{
		DPI: mo.DPI, Prints: "the " + mo.Name + " prints", Roll: l.Media.Name,
		WidthMM: l.Media.WidthMM, MinLengthMM: mo.MM(mo.MinLines), MaxLengthMM: mo.MM(mo.MaxLinesFor(l.Media)),
		// The page is cropped to the printable pins around its center.
		ReachMM: mo.MM(l.PrintableDots),
	}, nil
}

func (d ptouchDriver) Plan(s Settings, f Fit) (Plan, error) {
	l, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return ptouchPlan{layout: l, placement: ptouch.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type ptouchPlan struct {
	layout    ptouch.Layout
	placement ptouch.Placement
	opts      ptouch.JobOptions
}

func (p ptouchPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.layout.Prepare(label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return ptouch.EncodeJob(p.layout, pages, p.opts)
}
