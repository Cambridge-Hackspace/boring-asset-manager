package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// DriverZPL drives printers that speak Zebra's ZPL II, and DriverEPL2 the
// older Zebras that speak EPL2.
const (
	DriverZPL  = "zpl"
	DriverEPL2 = "epl2"
)

// zebraDriver adapts package zebra, which knows nothing of this registry,
// for one of its languages.
type zebraDriver struct{ lang zebra.Language }

// zplOptions are the zpl driver options.
type zplOptions struct {
	MediaType *string `json:"media_type,omitempty"`
	Darkness  *int    `json:"darkness,omitempty"`
	Speed     *int    `json:"speed,omitempty"`
	PrintMode *string `json:"print_mode,omitempty"`
	Calibrate *bool   `json:"calibrate,omitempty"`
	Compress  *bool   `json:"compress,omitempty"`
}

// epl2Options are the epl2 driver options.
type epl2Options struct {
	Density *int `json:"density,omitempty"`
}

func (d zebraDriver) Info() DriverInfo {
	info := DriverInfo{Driver: string(d.lang), Models: zebra.Models(d.lang), ModelDPI: map[string]int{}}
	if d.lang == zebra.ZPL {
		info.Options = []OptionInfo{{
			Name: "media_type", Type: "string", Default: nil, Values: []string{"direct", "transfer"},
			Description: "Direct thermal labels, or thermal transfer with a ribbon; unset keeps the printer's own setting.",
		}, {
			Name: "darkness", Type: "int", Default: nil,
			Description: "Print darkness, 0 (lightest) to 30 (darkest); unset keeps the printer's own setting.",
		}, {
			Name: "speed", Type: "int", Default: nil,
			Description: "Print speed in inches per second, 1 to 14, as far as the model goes; unset keeps the printer's own setting.",
		}, {
			Name: "print_mode", Type: "string", Default: nil, Values: []string{"tear_off", "peel_off", "cutter"},
			Description: "What happens to each label after printing; unset keeps the printer's own setting.",
		}, {
			Name: "calibrate", Type: "bool", Default: false,
			Description: "Measure the labels before each job, as after loading a new roll.",
		}, {
			Name: "compress", Type: "bool", Default: true,
			Description: "Send labels with Zebra's compression; turn it off for a printer that emulates ZPL and mishandles it.",
		}}
	} else {
		info.Options = []OptionInfo{{
			Name: "density", Type: "int", Default: nil,
			Description: "Print darkness, 0 (lightest) to 15 (darkest); unset keeps the printer's own setting.",
		}}
	}
	custom := &CustomStockInfo{MaxWidthMM: map[string]float64{}, MinLengthMM: zebra.MinContinuousMM, MaxLengthMM: zebra.MaxContinuousMM}
	for _, name := range info.Models {
		m, _ := zebra.LookupModel(d.lang, name)
		info.ModelDPI[name] = m.DPI
		custom.MaxWidthMM[name] = math.Floor(m.MaxLabelMM()*10) / 10
	}
	info.CustomStock = custom
	for _, id := range zebra.MediaIDs() {
		m, _ := zebra.LookupMedia(id)
		info.Media = append(info.Media, MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()})
	}
	return info
}

// zebraSettings are a printer's settings as package zebra takes them.
type zebraSettings struct {
	model zebra.Model
	media zebra.Media
	zpl   zebra.ZPLOptions
	epl2  zebra.EPL2Options
}

func (d zebraDriver) settings(s Settings) (zebraSettings, error) {
	var out zebraSettings
	model, ok := zebra.LookupModel(d.lang, s.Model)
	if !ok {
		return out, fmt.Errorf("model must be one of %s", strings.Join(zebra.Models(d.lang), ", "))
	}
	media, ok := zebra.LookupMedia(s.Media)
	if s.Stock != nil {
		// Zebra media is given by its size, so any roll the head spans will
		// do.
		media, ok = zebra.Media{ID: s.Stock.ID, Name: s.Stock.Name, WidthMM: s.Stock.WidthMM, LengthMM: s.Stock.LengthMM, GapMM: s.Stock.GapMM}, true
	}
	if !ok {
		return out, fmt.Errorf("media must be one of %s, or custom stock", strings.Join(zebra.MediaIDs(), ", "))
	}
	if err := model.Suits(media); err != nil {
		return out, err
	}
	out.model, out.media = model, media
	decode := func(v any) error {
		if len(s.Options) == 0 || string(s.Options) == "null" {
			return nil
		}
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return fmt.Errorf("options: %v", err)
		}
		return nil
	}
	if d.lang == zebra.EPL2 {
		var o epl2Options
		if err := decode(&o); err != nil {
			return out, err
		}
		out.epl2 = zebra.EPL2Options{Density: o.Density}
		if err := out.epl2.Check(); err != nil {
			return out, fmt.Errorf("options: %w", err)
		}
		return out, nil
	}
	var o zplOptions
	if err := decode(&o); err != nil {
		return out, err
	}
	z := zebra.ZPLOptions{Darkness: o.Darkness, Speed: o.Speed}
	if o.MediaType != nil {
		z.MediaType = *o.MediaType
	}
	if o.PrintMode != nil {
		z.PrintMode = *o.PrintMode
	}
	if o.Calibrate != nil {
		z.Calibrate = *o.Calibrate
	}
	if o.Compress != nil {
		z.Uncompressed = !*o.Compress
	}
	if err := z.Check(); err != nil {
		return out, fmt.Errorf("options: %w", err)
	}
	out.zpl = z
	return out, nil
}

func (d zebraDriver) Validate(s Settings) error {
	_, err := d.settings(s)
	return err
}

func (d zebraDriver) Geometry(s Settings) (Geometry, error) {
	z, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	return Geometry{
		DPI: z.model.DPI, Prints: "the " + z.model.Name + " prints", Roll: z.media.Name,
		WidthMM: z.media.WidthMM, LengthMM: z.media.LengthMM,
		MinLengthMM: zebra.MinContinuousMM, MaxLengthMM: zebra.MaxContinuousMM,
		// A label wider than the head is cut to it around its center.
		ReachMM: z.model.HeadMM(),
	}, nil
}

func (d zebraDriver) Plan(s Settings, f Fit) (Plan, error) {
	z, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return zebraPlan{lang: d.lang, s: z, placement: zebra.Placement{Rotate: f.Rotate}}, nil
}

type zebraPlan struct {
	lang      zebra.Language
	s         zebraSettings
	placement zebra.Placement
}

func (p zebraPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once, so the encoder
	// can also print them as copies.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = zebra.Prepare(p.s.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	if p.lang == zebra.EPL2 {
		return zebra.EncodeEPL2(p.s.model, p.s.media, pages, p.s.epl2)
	}
	return zebra.EncodeZPL(p.s.model, p.s.media, pages, p.s.zpl)
}
