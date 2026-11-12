package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/industrial"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// DriverDPL drives Datamax-O'Neil and Honeywell printers in DPL, DriverSBPL
// SATO's in SBPL, and DriverEZPL Godex's in EZPL.
const (
	DriverDPL  = "dpl"
	DriverSBPL = "sbpl"
	DriverEZPL = "ezpl"
)

// industrialDriver adapts package industrial, which knows nothing of this
// registry, for one of its languages.
type industrialDriver struct{ lang industrial.Language }

// dplOptions are the dpl driver options.
type dplOptions struct {
	Heat        *int    `json:"heat,omitempty"`
	Speed       *int    `json:"speed,omitempty"`
	MediaSensor *string `json:"media_sensor,omitempty"`
	ImageModule *string `json:"image_module,omitempty"`
}

// sbplOptions are the sbpl driver options.
type sbplOptions struct {
	Darkness    *int    `json:"darkness,omitempty"`
	Speed       *int    `json:"speed,omitempty"`
	MediaSensor *string `json:"media_sensor,omitempty"`
	STXETX      *bool   `json:"stx_etx,omitempty"`
}

// ezplOptions are the ezpl driver options.
type ezplOptions struct {
	Darkness    *int    `json:"darkness,omitempty"`
	Speed       *int    `json:"speed,omitempty"`
	MediaType   *string `json:"media_type,omitempty"`
	MediaSensor *string `json:"media_sensor,omitempty"`
}

func (d industrialDriver) Info() DriverInfo {
	info := DriverInfo{Driver: string(d.lang), Models: industrial.Models(d.lang), ModelDPI: map[string]int{}}
	switch d.lang {
	case industrial.DPL:
		info.Options = []OptionInfo{{
			Name: "heat", Type: "int", Default: nil,
			Description: "Print darkness, 0 (lightest) to 30 (darkest); unset keeps the printer's own setting.",
		}, {
			Name: "speed", Type: "int", Default: nil,
			Description: "Print speed in inches per second, 1 to 12, as far as the model goes; unset keeps the printer's own setting.",
		}, {
			Name: "media_sensor", Type: "string", Default: "gap", Values: []string{"gap", "mark"},
			Description: "How the printer finds die-cut labels: by the gaps between them, or by black marks on the back.",
		}, {
			Name: "image_module", Type: "string", Default: "D",
			Description: "The memory module each label's image is loaded to, as a letter; D is the printer's own memory.",
		}}
	case industrial.SBPL:
		info.Options = []OptionInfo{{
			Name: "darkness", Type: "int", Default: nil,
			Description: "Print darkness, 1 (lightest) to 10 (darkest); unset keeps the printer's own setting.",
		}, {
			Name: "speed", Type: "int", Default: nil,
			Description: "Print speed in inches per second, 2 to 10, as far as the model goes; unset keeps the printer's own setting.",
		}, {
			Name: "media_sensor", Type: "string", Default: nil, Values: []string{"gap", "mark", "none"},
			Description: "How the printer finds labels: by gaps, by black marks, or not at all for continuous media; unset keeps the printer's own setting.",
		}, {
			Name: "stx_etx", Type: "bool", Default: false,
			Description: "Wrap each job in STX and ETX, for a printer whose interface protocol expects it.",
		}}
	default:
		info.Options = []OptionInfo{{
			Name: "darkness", Type: "int", Default: nil,
			Description: "Print darkness, 0 (lightest) to 19 (darkest); unset keeps the printer's own setting.",
		}, {
			Name: "speed", Type: "int", Default: nil,
			Description: "Print speed in inches per second, 2 to 7, as far as the model goes; unset keeps the printer's own setting.",
		}, {
			Name: "media_type", Type: "string", Default: nil, Values: []string{"direct", "transfer"},
			Description: "Direct thermal labels, or thermal transfer with a ribbon; unset keeps the printer's own setting.",
		}, {
			Name: "media_sensor", Type: "string", Default: "gap", Values: []string{"gap", "mark"},
			Description: "How the printer finds die-cut labels: by the gaps between them, or by black marks as wide as the roll's gap.",
		}}
	}
	custom := &CustomStockInfo{MaxWidthMM: map[string]float64{}, MinLengthMM: zebra.MinContinuousMM, MaxLengthMM: zebra.MaxContinuousMM}
	for _, name := range info.Models {
		m, _ := industrial.LookupModel(d.lang, name)
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

// industrialSettings are a printer's settings as package industrial takes
// them.
type industrialSettings struct {
	model industrial.Model
	media zebra.Media
	dpl   industrial.DPLOptions
	sbpl  industrial.SBPLOptions
	ezpl  industrial.EZPLOptions
}

func (d industrialDriver) settings(s Settings) (industrialSettings, error) {
	var out industrialSettings
	model, ok := industrial.LookupModel(d.lang, s.Model)
	if !ok {
		return out, fmt.Errorf("model must be one of %s", strings.Join(industrial.Models(d.lang), ", "))
	}
	media, ok := zebra.LookupMedia(s.Media)
	if s.Stock != nil {
		// Media is given by its size, so any roll the head spans will do.
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
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	var err error
	switch d.lang {
	case industrial.DPL:
		var o dplOptions
		if err := decode(&o); err != nil {
			return out, err
		}
		out.dpl = industrial.DPLOptions{Heat: o.Heat, Speed: o.Speed, Sensor: str(o.MediaSensor), Module: str(o.ImageModule)}
		err = out.dpl.Check()
	case industrial.SBPL:
		var o sbplOptions
		if err := decode(&o); err != nil {
			return out, err
		}
		out.sbpl = industrial.SBPLOptions{Darkness: o.Darkness, Speed: o.Speed, Sensor: str(o.MediaSensor), Framed: o.STXETX != nil && *o.STXETX}
		err = out.sbpl.Check()
	default:
		var o ezplOptions
		if err := decode(&o); err != nil {
			return out, err
		}
		out.ezpl = industrial.EZPLOptions{Darkness: o.Darkness, Speed: o.Speed, MediaType: str(o.MediaType), Sensor: str(o.MediaSensor)}
		err = out.ezpl.Check()
	}
	if err != nil {
		return out, fmt.Errorf("options: %w", err)
	}
	return out, nil
}

func (d industrialDriver) Validate(s Settings) error {
	_, err := d.settings(s)
	return err
}

func (d industrialDriver) Geometry(s Settings) (Geometry, error) {
	is, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	maxLength := float64(zebra.MaxContinuousMM)
	if most, ok := industrial.MaxSBPLLengthDots[is.model.DPI]; d.lang == industrial.SBPL && ok {
		maxLength = min(maxLength, math.Floor(float64(most)/float64(is.model.DPI)*25.4))
	}
	return Geometry{
		DPI: is.model.DPI, Prints: "the " + is.model.Name + " prints", Roll: is.media.Name,
		WidthMM: is.media.WidthMM, LengthMM: is.media.LengthMM,
		MinLengthMM: zebra.MinContinuousMM, MaxLengthMM: maxLength,
		// A label wider than the model reaches is cut to it around its
		// center.
		ReachMM: is.model.ReachMM(),
	}, nil
}

func (d industrialDriver) Plan(s Settings, f Fit) (Plan, error) {
	is, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return industrialPlan{lang: d.lang, s: is, placement: industrial.Placement{Rotate: f.Rotate}}, nil
}

type industrialPlan struct {
	lang      industrial.Language
	s         industrialSettings
	placement industrial.Placement
}

func (p industrialPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once, so the encoder
	// can also print them as copies.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = industrial.Prepare(p.s.model, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	switch p.lang {
	case industrial.DPL:
		return industrial.EncodeDPL(p.s.model, p.s.media, pages, p.s.dpl)
	case industrial.SBPL:
		return industrial.EncodeSBPL(p.s.model, p.s.media, pages, p.s.sbpl)
	default:
		return industrial.EncodeEZPL(p.s.model, p.s.media, pages, p.s.ezpl)
	}
}
