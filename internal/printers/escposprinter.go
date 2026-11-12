package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/escpos"
)

// DriverESCPOS drives receipt and label printers that take ESC/POS raster
// images.
const DriverESCPOS = "escpos"

// DriverPhomemo drives Phomemo's label makers, which speak their own
// variants of ESC/POS.
const DriverPhomemo = "phomemo"

// escposDriver adapts package escpos, which knows nothing of this
// registry: plain ESC/POS, or Phomemo's variants of it.
type escposDriver struct{ phomemo bool }

// escposOptions are the escpos driver options.
type escposOptions struct {
	HeadDots *int    `json:"head_dots,omitempty"`
	Cut      *string `json:"cut,omitempty"`
	FeedMM   *int    `json:"feed_mm,omitempty"`
}

// phomemoOptions are the phomemo driver options.
type phomemoOptions struct {
	Density    *int  `json:"density,omitempty"`
	PrintSpeed *int  `json:"print_speed,omitempty"`
	Rotate180  *bool `json:"rotate_180,omitempty"`
}

// Defaults for the options.
const (
	escposDefaultCut    = escpos.CutPartial
	phomemoDefaultDense = 10
	phomemoDefaultSpeed = 5
)

func (d escposDriver) Info() DriverInfo {
	names := escpos.Models(d.phomemo)
	info := DriverInfo{Driver: DriverESCPOS, Models: names, ModelDPI: map[string]int{}}
	if d.phomemo {
		info.Driver = DriverPhomemo
		info.Options = []OptionInfo{{
			Name: "density", Type: "int", Default: phomemoDefaultDense,
			Description: "Print darkness, 1 (lightest) to 15 (darkest); M110, M120, M200, M220 and M221 only.",
		}, {
			Name: "print_speed", Type: "int", Default: phomemoDefaultSpeed,
			Description: "Print speed, 1 (slowest) to 5 (fastest); M110, M120, M200, M220 and M221 only.",
		}, {
			Name: "rotate_180", Type: "bool", Default: false,
			Description: "Print labels the other way up.",
		}}
	} else {
		info.Options = []OptionInfo{{
			Name: "head_dots", Type: "int", Default: nil,
			Description: "The print head's width in dots, for a printer whose head is not the usual 384 (58 mm paper) or 576 (80 mm paper); unset is the model's.",
		}, {
			Name: "cut", Type: "string", Default: string(escposDefaultCut), Values: stringsOf(escpos.Cuts()),
			Description: "How to cut each label off: none for a printer without a cutter, partial to leave it hanging by a tab, or full.",
		}, {
			Name: "feed_mm", Type: "int", Default: 0,
			Description: "Paper fed after each label before it is cut, in millimeters (0-100): for a printer without a cutter, enough to reach its tear bar.",
		}}
	}
	info.CustomStock = &CustomStockInfo{MaxWidthMM: map[string]float64{}, MinLengthMM: escpos.MinContinuousMM, MaxLengthMM: escpos.MaxContinuousMM}
	for _, n := range names {
		m, _ := escpos.LookupModel(n)
		info.ModelDPI[n] = m.DPI
		info.CustomStock.MaxWidthMM[n] = m.MaxRollMM
	}
	for _, id := range escpos.MediaIDs(d.phomemo) {
		m, _ := escpos.LookupMedia(id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()}
		for _, n := range names {
			if mo, _ := escpos.LookupModel(n); mo.Takes(m) == nil {
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

// phomemoDriver is the escpos driver for Phomemo's printers, with their
// Bluetooth LE profile. Plain ESC/POS printers' profiles differ by maker,
// and are given on the connection.
type phomemoDriver struct{ escposDriver }

// BLE is Phomemo's Bluetooth LE profile. It acknowledges nothing, so jobs
// are written without waiting.
func (phomemoDriver) BLE() BLEProfile {
	return BLEProfile{Service: "0000ff00-0000-1000-8000-00805f9b34fb", Write: "0000ff02-0000-1000-8000-00805f9b34fb"}
}

// escposSettings is a printer's settings, resolved.
type escposSettings struct {
	model     escpos.Model
	media     escpos.Media
	headDots  int
	rotate180 bool
	opts      escpos.JobOptions
}

func (d escposDriver) settings(s Settings) (escposSettings, error) {
	var r escposSettings
	model, ok := escpos.LookupModel(s.Model)
	if !ok || model.Family.Phomemo() != d.phomemo {
		return r, fmt.Errorf("model must be one of %s", strings.Join(escpos.Models(d.phomemo), ", "))
	}
	media, ok := escpos.LookupMedia(s.Media)
	if s.Stock != nil {
		// These printers are sent only an image, and find any gaps
		// themselves, so any roll the model takes will do.
		media, ok = escpos.Media{ID: s.Stock.ID, Name: s.Stock.Name, WidthMM: s.Stock.WidthMM, LengthMM: s.Stock.LengthMM}, true
	}
	if !ok || media.Family.Phomemo() != d.phomemo && media.Family != "" {
		return r, fmt.Errorf("media must be one of %s, or custom stock", strings.Join(escpos.MediaIDs(d.phomemo), ", "))
	}
	if err := model.Takes(media); err != nil {
		return r, err
	}
	r = escposSettings{model: model, media: media, headDots: model.HeadDots}
	if d.phomemo {
		var o phomemoOptions
		if err := decodeOptions(s.Options, &o); err != nil {
			return r, err
		}
		if model.Family != escpos.FamilyPhomemoM && (o.Density != nil || o.PrintSpeed != nil) {
			return r, fmt.Errorf("options: the %s takes no density or print speed", model.Name)
		}
		r.opts = escpos.JobOptions{Density: phomemoDefaultDense, Speed: phomemoDefaultSpeed}
		if o.Density != nil {
			r.opts.Density = *o.Density
		}
		if o.PrintSpeed != nil {
			r.opts.Speed = *o.PrintSpeed
		}
		if o.Rotate180 != nil {
			r.rotate180 = *o.Rotate180
		}
	} else {
		var o escposOptions
		if err := decodeOptions(s.Options, &o); err != nil {
			return r, err
		}
		r.opts.Cut = escposDefaultCut
		if o.Cut != nil {
			r.opts.Cut = escpos.Cut(*o.Cut)
		}
		if o.FeedMM != nil {
			r.opts.FeedMM = *o.FeedMM
		}
		if o.HeadDots != nil {
			if *o.HeadDots < 8 || *o.HeadDots > 2048 {
				return r, fmt.Errorf("options: head_dots must be 8-2048")
			}
			r.headDots = *o.HeadDots
		}
	}
	if err := r.opts.Check(model); err != nil {
		return r, fmt.Errorf("options: %v", strings.TrimPrefix(err.Error(), "escpos: "))
	}
	return r, nil
}

// decodeOptions decodes a driver's options, refusing any it does not take.
func decodeOptions(raw json.RawMessage, o any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(o); err != nil {
		return fmt.Errorf("options: %v", err)
	}
	return nil
}

func (d escposDriver) Validate(s Settings) error {
	_, err := d.settings(s)
	return err
}

func (d escposDriver) Geometry(s Settings) (Geometry, error) {
	r, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	return Geometry{
		DPI: r.model.DPI, Prints: "the " + r.model.Name + " prints", Roll: r.media.Name,
		WidthMM: r.media.WidthMM, LengthMM: r.media.LengthMM,
		MinLengthMM: escpos.MinContinuousMM, MaxLengthMM: escpos.MaxContinuousMM,
		// A label wider than the head is cut to it around its center.
		ReachMM: r.model.HeadMM(r.headDots),
	}, nil
}

func (d escposDriver) Plan(s Settings, f Fit) (Plan, error) {
	r, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return escposPlan{r: r, placement: escpos.Placement{Rotate: f.Rotate, Rotate180: r.rotate180}}, nil
}

type escposPlan struct {
	r         escposSettings
	placement escpos.Placement
}

func (p escposPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = escpos.Prepare(p.r.model, p.r.headDots, label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return escpos.EncodeJob(p.r.model, p.r.media, pages, p.r.opts)
}
