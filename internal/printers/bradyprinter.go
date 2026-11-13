package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brady"
)

// DriverBrady drives Brady printers that take JSON jobs, such as the M611.
const DriverBrady = "brady"

// bradyDriver adapts package brady, which knows nothing of this registry.
type bradyDriver struct{}

// bradyOptions are the brady driver options.
type bradyOptions struct {
	CutAfter *string `json:"cut_after,omitempty"`
	Part     *string `json:"part,omitempty"`
}

func (bradyDriver) Info() DriverInfo {
	names := brady.Models()
	info := DriverInfo{
		Driver: DriverBrady, Models: names, ModelDPI: map[string]int{},
		// Brady's cartridges are many and its part list is its own, so each
		// is added as custom stock, by its size.
		Media: []MediaInfo{},
		Options: []OptionInfo{{
			Name: "cut_after", Type: "string", Default: string(brady.CutEach), Values: stringsOf(brady.Cuts()),
			Description: "Cut each label off as it finishes, only the job's last, or as the printer's own menu says.",
		}, {
			Name: "part", Type: "string", Default: "",
			Description: "The Brady part number on the cartridge loaded, such as M6-11-427, sent with each job.",
		}},
	}
	custom := &CustomStockInfo{MaxWidthMM: map[string]float64{}, Note: "Add the cartridge loaded by its labels' size; its part number goes in the part option."}
	for _, n := range names {
		m, _ := brady.LookupModel(n)
		info.ModelDPI[n] = m.DPI
		custom.MaxWidthMM[n] = m.MaxWidthMM
		custom.MinLengthMM, custom.MaxLengthMM = m.MinLengthMM, m.MaxLengthMM
	}
	info.CustomStock = custom
	return info
}

func (bradyDriver) settings(s Settings) (brady.Model, brady.Media, brady.JobOptions, error) {
	model, ok := brady.LookupModel(s.Model)
	if !ok {
		return model, brady.Media{}, brady.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(brady.Models(), ", "))
	}
	if s.Stock == nil {
		return model, brady.Media{}, brady.JobOptions{}, fmt.Errorf("media must be custom stock: add the cartridge loaded by its labels' size")
	}
	media := brady.Media{ID: s.Stock.ID, Name: s.Stock.Name, WidthMM: s.Stock.WidthMM, LengthMM: s.Stock.LengthMM}
	if err := model.Suits(media); err != nil {
		return model, media, brady.JobOptions{}, err
	}
	opts := brady.JobOptions{Cut: brady.CutEach}
	if len(s.Options) > 0 && string(s.Options) != "null" {
		var o bradyOptions
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return model, media, opts, fmt.Errorf("options: %v", err)
		}
		if o.CutAfter != nil {
			opts.Cut = brady.Cut(*o.CutAfter)
		}
		if o.Part != nil {
			opts.Part = strings.TrimSpace(*o.Part)
		}
	}
	if err := opts.Check(); err != nil {
		return model, media, opts, fmt.Errorf("options: %w", err)
	}
	return model, media, opts, nil
}

func (d bradyDriver) Validate(s Settings) error {
	_, _, _, err := d.settings(s)
	return err
}

func (d bradyDriver) Geometry(s Settings) (Geometry, error) {
	model, media, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	return Geometry{
		DPI: model.DPI, Prints: "the Brady " + model.Name + " prints", Roll: media.Name,
		WidthMM: media.WidthMM, LengthMM: media.LengthMM,
		MinLengthMM: model.MinLengthMM, MaxLengthMM: model.MaxLengthMM,
	}, nil
}

func (d bradyDriver) Plan(s Settings, f Fit) (Plan, error) {
	model, media, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return bradyPlan{model: model, media: media, placement: brady.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type bradyPlan struct {
	model     brady.Model
	media     brady.Media
	placement brady.Placement
	opts      brady.JobOptions
}

func (p bradyPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once, so each is
	// compressed once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = brady.Prepare(p.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return brady.EncodeJob(p.model, p.media, pages, p.opts)
}
