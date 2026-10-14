package printers

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolw"
)

// DriverDymoLabelWriter drives DYMO LabelWriter printers.
const DriverDymoLabelWriter = "dymo_labelwriter"

// dymoLabelWriter adapts package dymolw, which knows nothing of this registry.
type dymoLabelWriter struct{}

// dymoOptions are the dymo_labelwriter driver options.
type dymoOptions struct {
	Density dymolw.Density `json:"density,omitempty"`
	Roll    dymolw.Roll    `json:"roll,omitempty"`
}

func (dymoLabelWriter) Info() DriverInfo {
	info := DriverInfo{
		Driver: DriverDymoLabelWriter,
		Models: dymolw.Models(),
		Options: []OptionInfo{{
			Name: "density", Type: "string", Default: string(dymolw.DensityNormal),
			Values:      stringsOf(dymolw.Densities()),
			Description: "Print darkness.",
		}, {
			Name: "roll", Type: "string", Default: string(dymolw.RollAuto),
			Values:      stringsOf(dymolw.Rolls()),
			Description: "Which roll to print from; twin-roll models only.",
		}},
	}
	for _, id := range dymolw.MediaIDs() {
		m, _ := dymolw.LookupMedia(id)
		info.Media = append(info.Media, MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()})
	}
	return info
}

func stringsOf[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// options decodes and checks the options for a model.
func (dymoLabelWriter) options(model dymolw.Model, raw json.RawMessage) (dymolw.JobOptions, error) {
	var o dymoOptions
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return dymolw.JobOptions{}, fmt.Errorf("options: %v", err)
		}
	}
	if o.Density != "" && !containsValue(dymolw.Densities(), o.Density) {
		return dymolw.JobOptions{}, fmt.Errorf("options: density must be one of %s", strings.Join(stringsOf(dymolw.Densities()), ", "))
	}
	if o.Roll != "" {
		if !model.TwinRoll {
			return dymolw.JobOptions{}, fmt.Errorf("options: the %s has one roll", model.Name)
		}
		if !containsValue(dymolw.Rolls(), o.Roll) {
			return dymolw.JobOptions{}, fmt.Errorf("options: roll must be one of %s", strings.Join(stringsOf(dymolw.Rolls()), ", "))
		}
	}
	return dymolw.JobOptions{Density: o.Density, Roll: o.Roll}, nil
}

func containsValue[T comparable](list []T, v T) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func (d dymoLabelWriter) settings(s Settings) (dymolw.Model, dymolw.Media, dymolw.JobOptions, error) {
	model, ok := dymolw.LookupModel(s.Model)
	if !ok {
		return model, dymolw.Media{}, dymolw.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(dymolw.Models(), ", "))
	}
	media, ok := dymolw.LookupMedia(s.Media)
	if !ok {
		return model, media, dymolw.JobOptions{}, fmt.Errorf("media must be one of %s", strings.Join(dymolw.MediaIDs(), ", "))
	}
	if err := model.Suits(media); err != nil {
		return model, media, dymolw.JobOptions{}, err
	}
	opts, err := d.options(model, s.Options)
	return model, media, opts, err
}

func (d dymoLabelWriter) Validate(s Settings) error {
	_, _, _, err := d.settings(s)
	return err
}

func (d dymoLabelWriter) Plan(s Settings, label LabelSpec) (Plan, error) {
	model, media, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	placement, err := media.Fit(label.WidthMM, label.HeightMM, label.DPI)
	if err != nil {
		if errors.Is(err, dymolw.ErrDoesNotFit) {
			return nil, fmt.Errorf("%w: %v", ErrDoesNotFit, strings.TrimPrefix(err.Error(), dymolw.ErrDoesNotFit.Error()+": "))
		}
		return nil, err
	}
	return dymoPlan{model: model, media: media, placement: placement, opts: opts}, nil
}

type dymoPlan struct {
	model     dymolw.Model
	media     dymolw.Media
	placement dymolw.Placement
	opts      dymolw.JobOptions
}

// newJobID tags second-generation jobs; the printer only echoes it back.
var newJobID = func() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:])
}

func (p dymoPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.media.Prepare(p.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	opts := p.opts
	opts.JobID = newJobID()
	return dymolw.EncodeJob(p.model, p.media, pages, opts)
}
