package printers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
)

// DriverTSPL drives printers that speak TSC's TSPL, such as the Rollo.
const DriverTSPL = "tspl"

// tsplDriver adapts package tspl, which knows nothing of this registry.
type tsplDriver struct{}

// tsplOptions are the tspl driver options.
type tsplOptions struct {
	Density   *int  `json:"density,omitempty"`
	Rotate180 *bool `json:"rotate_180,omitempty"`
}

func (tsplDriver) Info() DriverInfo {
	info := DriverInfo{
		Driver:   DriverTSPL,
		Models:   tspl.Models(),
		ModelDPI: tsplDPI(),
		Options: []OptionInfo{{
			Name: "density", Type: "int", Default: nil,
			Description: "Print darkness, 0 (lightest) to 15 (darkest); unset keeps the printer's own setting.",
		}, {
			Name: "rotate_180", Type: "bool", Default: false,
			Description: "Print labels the other way up.",
		}},
	}
	for _, id := range tspl.MediaIDs() {
		m, _ := tspl.LookupMedia(id)
		info.Media = append(info.Media, MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()})
	}
	return info
}

func (tsplDriver) settings(s Settings) (tspl.Model, tspl.Media, tspl.JobOptions, error) {
	model, ok := tspl.LookupModel(s.Model)
	if !ok {
		return model, tspl.Media{}, tspl.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(tspl.Models(), ", "))
	}
	media, ok := tspl.LookupMedia(s.Media)
	if !ok {
		return model, media, tspl.JobOptions{}, fmt.Errorf("media must be one of %s", strings.Join(tspl.MediaIDs(), ", "))
	}
	if err := model.Suits(media); err != nil {
		return model, media, tspl.JobOptions{}, err
	}
	var o tsplOptions
	if len(s.Options) > 0 && string(s.Options) != "null" {
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return model, media, tspl.JobOptions{}, fmt.Errorf("options: %v", err)
		}
	}
	if o.Density != nil && (*o.Density < 0 || *o.Density > 15) {
		return model, media, tspl.JobOptions{}, fmt.Errorf("options: density must be 0-15")
	}
	opts := tspl.JobOptions{Density: o.Density}
	if o.Rotate180 != nil {
		opts.Rotate180 = *o.Rotate180
	}
	return model, media, opts, nil
}

func (d tsplDriver) Validate(s Settings) error {
	_, _, _, err := d.settings(s)
	return err
}

func (d tsplDriver) Plan(s Settings, label LabelSpec) (Plan, error) {
	model, media, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	placement, err := media.Fit(model, label.WidthMM, label.HeightMM, label.DPI)
	if err != nil {
		if errors.Is(err, tspl.ErrDoesNotFit) {
			return nil, fmt.Errorf("%w: %v", ErrDoesNotFit, strings.TrimPrefix(err.Error(), tspl.ErrDoesNotFit.Error()+": "))
		}
		return nil, err
	}
	return tsplPlan{model: model, media: media, placement: placement, opts: opts}, nil
}

type tsplPlan struct {
	model     tspl.Model
	media     tspl.Media
	placement tspl.Placement
	opts      tspl.JobOptions
}

func (p tsplPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once, so the encoder
	// can also print them as copies.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = tspl.Prepare(p.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return tspl.EncodeJob(p.model, p.media, pages, p.opts)
}

func tsplDPI() map[string]int {
	dpi := map[string]int{}
	for _, name := range tspl.Models() {
		m, _ := tspl.LookupModel(name)
		dpi[name] = m.DPI
	}
	return dpi
}
