package printers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// DriverBrotherQL drives Brother QL-800 series printers.
const DriverBrotherQL = "brother_ql"

// brotherQL adapts package brotherql, which knows nothing of this registry.
type brotherQL struct{}

// brotherOptions are the brother_ql driver options.
type brotherOptions struct {
	// Cut cuts each label off as it finishes.
	Cut *bool `json:"cut,omitempty"`
}

func (brotherQL) Info() DriverInfo {
	info := DriverInfo{
		Driver:   DriverBrotherQL,
		Models:   brotherql.Models(),
		ModelDPI: sameDPI(brotherql.Models(), brotherql.DPI),
		Options: []OptionInfo{{
			Name: "cut", Type: "bool", Default: true,
			Description: "Cut each label off as it finishes; otherwise the tape feeds on uncut.",
		}},
	}
	for _, id := range brotherql.MediaIDs() {
		m, _ := brotherql.LookupMedia(id)
		info.Media = append(info.Media, MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()})
	}
	return info
}

func (brotherQL) options(raw json.RawMessage) (brotherql.JobOptions, error) {
	opts := brotherql.JobOptions{Cut: true}
	if len(raw) == 0 || string(raw) == "null" {
		return opts, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var o brotherOptions
	if err := dec.Decode(&o); err != nil {
		return opts, fmt.Errorf("options: %v", err)
	}
	if o.Cut != nil {
		opts.Cut = *o.Cut
	}
	return opts, nil
}

func (d brotherQL) Validate(s Settings) error {
	if !contains(brotherql.Models(), s.Model) {
		return fmt.Errorf("model must be one of %s", strings.Join(brotherql.Models(), ", "))
	}
	// The printer identifies the DK roll loaded, and placing a label on its
	// head takes Brother's figures for that roll, so a size is not enough.
	if s.Stock != nil {
		return errNoCustomStock("Brother QL printers")
	}
	if _, ok := brotherql.LookupMedia(s.Media); !ok {
		return fmt.Errorf("media must be one of %s", strings.Join(brotherql.MediaIDs(), ", "))
	}
	_, err := d.options(s.Options)
	return err
}

func (d brotherQL) Plan(s Settings, label LabelSpec) (Plan, error) {
	if s.Stock != nil {
		return nil, errNoCustomStock("Brother QL printers")
	}
	media, ok := brotherql.LookupMedia(s.Media)
	if !ok {
		return nil, fmt.Errorf("unknown media %q", s.Media)
	}
	opts, err := d.options(s.Options)
	if err != nil {
		return nil, err
	}
	placement, err := media.Fit(label.WidthMM, label.HeightMM, label.DPI)
	if err != nil {
		if errors.Is(err, brotherql.ErrDoesNotFit) {
			return nil, fmt.Errorf("%w: %v", ErrDoesNotFit, strings.TrimPrefix(err.Error(), brotherql.ErrDoesNotFit.Error()+": "))
		}
		return nil, err
	}
	return brotherPlan{media: media, placement: placement, opts: opts}, nil
}

type brotherPlan struct {
	media     brotherql.Media
	placement brotherql.Placement
	opts      brotherql.JobOptions
}

func (p brotherPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.media.Prepare(label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return brotherql.EncodeJob(p.media, pages, p.opts)
}
