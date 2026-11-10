package printers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// DriverBrotherQL drives Brother's QL range, which takes DK rolls, and
// DriverBrotherTD its TD-2 and TD-4 series, which take rolls of any size.
// Both speak Brother's raster command language.
const (
	DriverBrotherQL = "brother_ql"
	DriverBrotherTD = "brother_td"
)

// brotherDriver adapts package brotherql, which knows nothing of this
// registry, for one series.
type brotherDriver struct{ series brotherql.Series }

// brotherQL is the QL range's driver.
var brotherQL = brotherDriver{brotherql.SeriesQL}

// brotherOptions are the brother_ql and brother_td driver options.
type brotherOptions struct {
	// Cut cuts each label off as it finishes.
	Cut *bool `json:"cut,omitempty"`
}

func (d brotherDriver) name() string {
	if d.series == brotherql.SeriesTD {
		return DriverBrotherTD
	}
	return DriverBrotherQL
}

// what names the driver's printers in messages.
func (d brotherDriver) what() string {
	if d.series == brotherql.SeriesTD {
		return "Brother TD printers"
	}
	return "Brother QL printers"
}

func (d brotherDriver) Info() DriverInfo {
	names := brotherql.Models(d.series)
	info := DriverInfo{Driver: d.name(), Models: names, ModelDPI: map[string]int{}}
	for _, n := range names {
		m, _ := brotherql.LookupModel(n)
		info.ModelDPI[n] = m.DPI
		if m.TwoColor {
			if info.ModelPalette == nil {
				info.ModelPalette = map[string]labels.Palette{}
			}
			info.ModelPalette[n] = labels.PaletteBWR
		}
	}
	if d.series == brotherql.SeriesQL {
		info.Options = []OptionInfo{{
			Name: "cut", Type: "bool", Default: true,
			Description: "Cut each label off as it finishes; otherwise the tape feeds on uncut. The QL-500 has no cutter.",
		}}
	} else {
		info.Options = []OptionInfo{{
			Name: "cut", Type: "bool", Default: false,
			Description: "Cut each label off as it finishes, with a TD-4's optional cutter fitted; the TD-2 series has none.",
		}}
		custom := &CustomStockInfo{
			MaxWidthMM: map[string]float64{}, MinLengthMM: math.MaxFloat64,
			Note: "Set the printer up for the roll, with its size and whether it has gaps or black marks, using Brother's own tools: BAM! sends only the labels.",
		}
		for _, n := range names {
			m, _ := brotherql.LookupModel(n)
			custom.MaxWidthMM[n] = m.MaxMediaMM
			custom.MinLengthMM = math.Min(custom.MinLengthMM, math.Ceil(m.MM(m.MinLines)*10)/10)
			custom.MaxLengthMM = math.Max(custom.MaxLengthMM, math.Floor(m.MM(m.MaxLines)*10)/10)
		}
		info.CustomStock = custom
	}
	for _, id := range brotherql.SeriesMediaIDs(d.series) {
		m, _ := brotherql.LookupSeriesMedia(d.series, id)
		mi := MediaInfo{ID: m.ID, Name: m.Name, WidthMM: m.WidthMM, LengthMM: m.LengthMM, DieCut: m.DieCut()}
		if m.TwoColor {
			mi.Palette = labels.PaletteBWR
		}
		for _, n := range names {
			if mo, _ := brotherql.LookupModel(n); mo.Takes(m) == nil {
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

// settings resolves a printer's settings: its model, where its roll falls
// on the head, and its options.
func (d brotherDriver) settings(s Settings) (brotherql.Layout, brotherql.JobOptions, error) {
	names := brotherql.Models(d.series)
	model, ok := brotherql.LookupModel(s.Model)
	if !ok || model.Series != d.series {
		return brotherql.Layout{}, brotherql.JobOptions{}, fmt.Errorf("model must be one of %s", strings.Join(names, ", "))
	}
	media, ok := brotherql.LookupSeriesMedia(d.series, s.Media)
	if s.Stock != nil {
		// The QL range identifies the DK roll loaded, and placing a label on
		// its head takes Brother's figures for that roll, so a size is not
		// enough. A TD printer is set up for its roll and told nothing of
		// it, so any roll its head spans will do.
		if d.series == brotherql.SeriesQL {
			return brotherql.Layout{}, brotherql.JobOptions{}, errNoCustomStock(d.what())
		}
		media, ok = brotherql.SizedMedia(s.Stock.ID, s.Stock.Name, s.Stock.WidthMM, s.Stock.LengthMM), true
	}
	if !ok {
		ids := strings.Join(brotherql.SeriesMediaIDs(d.series), ", ")
		if d.series == brotherql.SeriesTD {
			return brotherql.Layout{}, brotherql.JobOptions{}, fmt.Errorf("media must be one of %s, or custom stock", ids)
		}
		return brotherql.Layout{}, brotherql.JobOptions{}, fmt.Errorf("media must be one of %s", ids)
	}
	layout, err := model.Layout(media)
	if err != nil {
		return layout, brotherql.JobOptions{}, err
	}
	opts, err := d.options(model, s.Options)
	return layout, opts, err
}

// options decodes and checks the options for a model. Cutting defaults to
// on for the QL range, and is on wherever the model has a cutter; asking
// for it where there is none is refused.
func (d brotherDriver) options(model brotherql.Model, raw json.RawMessage) (brotherql.JobOptions, error) {
	opts := brotherql.JobOptions{Cut: d.series == brotherql.SeriesQL && model.Cutter}
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
		if *o.Cut && !model.Cutter {
			return opts, fmt.Errorf("options: the %s has no cutter", model.Name)
		}
		opts.Cut = *o.Cut
	}
	return opts, nil
}

func (d brotherDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d brotherDriver) Geometry(s Settings) (Geometry, error) {
	l, opts, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	mo := l.Model
	minLines := mo.MinLines
	if opts.Cut && mo.CutMinLines > minLines {
		minLines = mo.CutMinLines
	}
	prints := "Brother QL printers print"
	if d.series == brotherql.SeriesTD {
		prints = "the " + mo.Name + " prints"
	}
	g := Geometry{
		DPI: mo.DPI, Prints: prints, Roll: l.Media.Name,
		WidthMM: l.Media.WidthMM, LengthMM: l.Media.LengthMM,
		MinLengthMM: mo.MM(minLines), MaxLengthMM: mo.MM(mo.MaxLines),
		// The page is cropped to the printable pins around its center, and a
		// die-cut label to its printable lines.
		ReachMM: mo.MM(l.PrintableDots),
	}
	if l.Media.DieCut() {
		g.ReachAlongMM = mo.MM(l.PrintableLines)
	}
	if l.TwoColor() {
		g.Palette = labels.PaletteBWR
	}
	return g, nil
}

func (d brotherDriver) Plan(s Settings, f Fit) (Plan, error) {
	l, opts, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return brotherPlan{layout: l, placement: brotherql.Placement{Rotate: f.Rotate}, opts: opts}, nil
}

type brotherPlan struct {
	layout    brotherql.Layout
	placement brotherql.Placement
	opts      brotherql.JobOptions
}

// Palette is black and white, and red on a roll that takes it in a model
// that prints it.
func (p brotherPlan) Palette() labels.Palette {
	if p.layout.TwoColor() {
		return labels.PaletteBWR
	}
	return labels.PaletteBW
}

// EncodeInks encodes labels drawn in the plan's palette: in two colors, red
// as red; in black and white, anything but white as black.
func (p brotherPlan) EncodeInks(images []*image.Paletted) ([]byte, error) {
	type pages struct{ black, red *image.Gray }
	prepared := map[*image.Paletted]pages{}
	black := make([]*image.Gray, len(images))
	red := make([]*image.Gray, len(images))
	for i, label := range images {
		got, ok := prepared[label]
		if !ok {
			b, r := splitInks(label)
			got = pages{p.layout.Prepare(b, p.placement), p.layout.Prepare(r, p.placement)}
			prepared[label] = got
		}
		black[i], red[i] = got.black, got.red
	}
	if !p.layout.TwoColor() {
		return brotherql.EncodeJob(p.layout, black, p.opts)
	}
	return brotherql.EncodeTwoColorJob(p.layout, black, red, p.opts)
}

// splitInks splits a label drawn in inks into black, dark wherever an ink
// but white and red is, and red, dark wherever red is.
func splitInks(label *image.Paletted) (black, red *image.Gray) {
	b := label.Bounds()
	black, red = image.NewGray(b), image.NewGray(b)
	for i := range black.Pix {
		black.Pix[i], red.Pix[i] = 0xFF, 0xFF
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := label.Palette[label.Pix[y*label.Stride+x]]
			switch c {
			case labels.InkColor(labels.InkWhite):
			case labels.InkColor(labels.InkRed):
				red.Pix[y*red.Stride+x] = 0
			default:
				black.Pix[y*black.Stride+x] = 0
			}
		}
	}
	return black, red
}

func (p brotherPlan) Encode(labels []*image.Gray) ([]byte, error) {
	// Copies repeat the same image; prepare each one once.
	prepared := map[*image.Gray]*image.Gray{}
	pages := make([]*image.Gray, len(labels))
	for i, label := range labels {
		if prepared[label] == nil {
			prepared[label] = p.layout.Prepare(label, p.placement)
		}
		pages[i] = prepared[label]
	}
	return brotherql.EncodeJob(p.layout, pages, p.opts)
}
