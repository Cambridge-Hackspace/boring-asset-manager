package printers

import (
	"bytes"
	"encoding/json"
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

func (d brotherQL) media(s Settings) (brotherql.Media, error) {
	if s.Stock != nil {
		return brotherql.Media{}, errNoCustomStock("Brother QL printers")
	}
	media, ok := brotherql.LookupMedia(s.Media)
	if !ok {
		return media, fmt.Errorf("unknown media %q", s.Media)
	}
	return media, nil
}

func (d brotherQL) Geometry(s Settings) (Geometry, error) {
	media, err := d.media(s)
	if err != nil {
		return Geometry{}, err
	}
	dots := func(n int) float64 { return float64(n) / brotherql.DPI * 25.4 }
	g := Geometry{
		DPI: brotherql.DPI, Prints: "Brother QL printers print", Roll: media.Name,
		WidthMM: media.WidthMM, LengthMM: media.LengthMM,
		MinLengthMM: dots(brotherql.MinContinuousLines), MaxLengthMM: dots(brotherql.MaxContinuousLines),
		// The page is cropped to the printable pins around its center, and a
		// die-cut label to its printable lines.
		ReachMM: dots(media.PrintableDots),
	}
	if media.DieCut() {
		g.ReachAlongMM = dots(media.PrintableLines)
	}
	return g, nil
}

func (d brotherQL) Plan(s Settings, f Fit) (Plan, error) {
	media, err := d.media(s)
	if err != nil {
		return nil, err
	}
	opts, err := d.options(s.Options)
	if err != nil {
		return nil, err
	}
	return brotherPlan{media: media, placement: brotherql.Placement{Rotate: f.Rotate}, opts: opts}, nil
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
