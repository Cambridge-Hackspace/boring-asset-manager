package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/epsonlw"
)

// DriverEpsonLabelWorks drives Epson LabelWorks tape printers.
const DriverEpsonLabelWorks = "epson_labelworks"

// epsonLabelWorks adapts package epsonlw, which knows nothing of this
// registry.
type epsonLabelWorks struct{}

// epsonLWOptions are the epson_labelworks driver options.
type epsonLWOptions struct {
	CutAfter *string `json:"cut_after,omitempty"`
	Density  *int    `json:"density,omitempty"`
	MarginMM *int    `json:"margin_mm,omitempty"`
	HalfCut  *bool   `json:"half_cut,omitempty"`
}

// TCPProtocols implements TCPProtocolDriver: a networked LabelWorks printer
// may need the TPRT handshake (tprt.go).
func (epsonLabelWorks) TCPProtocols() []string { return []string{ProtocolTPRT} }

func (epsonLabelWorks) Info() DriverInfo {
	names := epsonlw.Models()
	info := DriverInfo{
		Driver: DriverEpsonLabelWorks, Models: names, ModelDPI: map[string]int{},
		Options: []OptionInfo{{
			Name: "cut_after", Type: "string", Default: string(epsonlw.CutEach), Values: stringsOf(epsonlw.Cuts()),
			Description: "Cut each label off as it finishes, only the job's last, or never.",
		}, {
			Name: "density", Type: "int", Default: 0,
			Description: fmt.Sprintf("Darker above 0, lighter below: %d to %d.", epsonlw.MinDensity, epsonlw.MaxDensity),
		}, {
			Name: "margin_mm", Type: "int", Default: epsonlw.DefaultMarginMM,
			Description: fmt.Sprintf("Tape fed before and after each label, in millimeters: 0-%d.", epsonlw.MaxMarginMM),
		}, {
			Name: "half_cut", Type: "bool", Default: false,
			Description: "Cut the tape but not its backing, on models with a half cutter such as the LW-PX800.",
		}},
	}
	for _, n := range names {
		m, _ := epsonlw.LookupModel(n)
		info.ModelDPI[n] = m.DPI
	}
	for _, id := range epsonlw.MediaIDs() {
		m, _ := epsonlw.LookupMedia(id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM}
		for _, n := range names {
			if mo, _ := epsonlw.LookupModel(n); mo.Takes(m) == nil {
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

// settings resolves a printer's settings: its cartridge's layout, and its
// options.
func (epsonLabelWorks) settings(s Settings) (epsonlw.Layout, epsonlw.JobOptions, error) {
	model, ok := epsonlw.LookupModel(s.Model)
	if !ok {
		return epsonlw.Layout{}, epsonlw.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(epsonlw.Models(), ", "))
	}
	// Each tape's printable dots are known only for Epson's widths.
	if s.Stock != nil {
		return epsonlw.Layout{}, epsonlw.JobOptions{}, errNoCustomStock("Epson LabelWorks printers")
	}
	media, ok := epsonlw.LookupMedia(s.Media)
	if !ok {
		return epsonlw.Layout{}, epsonlw.JobOptions{}, fmt.Errorf("media must be one of %s", strings.Join(epsonlw.MediaIDs(), ", "))
	}
	layout, err := model.Layout(media)
	if err != nil {
		return layout, epsonlw.JobOptions{}, err
	}

	opts := epsonlw.DefaultOptions(model)
	if len(s.Options) > 0 && string(s.Options) != "null" {
		var o epsonLWOptions
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return layout, opts, fmt.Errorf("options: %v", err)
		}
		if o.CutAfter != nil {
			opts.Cut = epsonlw.Cut(*o.CutAfter)
		}
		if o.Density != nil {
			opts.Density = *o.Density
		}
		if o.HalfCut != nil {
			opts.HalfCut = *o.HalfCut
		}
		if o.MarginMM != nil {
			if *o.MarginMM < 0 || *o.MarginMM > epsonlw.MaxMarginMM {
				return layout, opts, fmt.Errorf("options: margin_mm must be 0-%d", epsonlw.MaxMarginMM)
			}
			opts.MarginLines = model.Dots(float64(*o.MarginMM))
		}
	}
	if err := opts.Check(model); err != nil {
		return layout, opts, fmt.Errorf("options: %w", err)
	}
	return layout, opts, nil
}

func (d epsonLabelWorks) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d epsonLabelWorks) Geometry(s Settings) (Geometry, error) {
	l, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	mo := l.Model
	// The template is the printed length; the margins are fed around it.
	return Geometry{
		DPI: mo.DPI, Prints: "the " + mo.Name + " prints", Roll: l.Media.Name,
		WidthMM: l.Media.WidthMM, MaxLengthMM: epsonlw.MaxLengthMM,
		// The page is cropped to the printable dots around its center.
		ReachMM: mo.MM(l.PrintableDots),
	}, nil
}

func (d epsonLabelWorks) Plan(s Settings, f Fit) (Plan, error) {
	l, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return epsonLWPlan{layout: l, placement: epsonlw.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type epsonLWPlan struct {
	layout    epsonlw.Layout
	placement epsonlw.Placement
	opts      epsonlw.JobOptions
}

func (p epsonLWPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.layout.Prepare(label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return epsonlw.EncodeJob(p.layout, pages, p.opts)
}
