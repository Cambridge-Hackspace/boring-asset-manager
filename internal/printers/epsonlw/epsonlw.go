// Package epsonlw encodes print jobs for Epson LabelWorks tape printers, in
// the raster stream Epson's printers take: ESC { commands that set up the job
// and each page, then ESC . raster lines across the tape.
//
// Epson publishes no command reference. The protocol was reconstructed, as
// facts only, from community descriptions: tyalie/RE-epson-label-printer,
// eoleedi/ha-epson-labelworks, igrbtn/LW700Print and hikalium/sr5900p (all
// MIT), checked against oxplot/nospero and elliotmatson/epson-labelworks-mcp
// (no license, read for facts only). No code, text or test vectors are taken
// from any of them, and Epson's SDK is not used. Like package ptouch, it works
// on 1-bit images and physical sizes only.
package epsonlw

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// Placement says how a label image maps onto the tape.
type Placement = brotherql.Placement

// Media is a LabelWorks tape cartridge. Tape is continuous: the template sets
// a label's length, and the cartridge only its width.
type Media struct {
	ID      string
	Name    string
	WidthMM float64
}

// printableDots is how many dots print across each width of tape, at 180
// DPI. Only 12 mm (72 dots) has been measured on a printer, an LW-700; the
// others are the figures community drivers use, and are provisional until
// checked on hardware.
var printableDots = map[float64]int{4: 22, 6: 36, 9: 54, 12: 72, 18: 108, 24: 128}

var catalog = map[string]Media{}

func init() {
	for w := range printableDots {
		m := Media{ID: fmt.Sprintf("lk-%g", w), Name: fmt.Sprintf("LK tape %g mm", w), WidthMM: w}
		catalog[m.ID] = m
	}
}

// LookupMedia returns a cartridge by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the cartridges, narrowest first.
func MediaIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return catalog[ids[i]].WidthMM < catalog[ids[j]].WidthMM })
	return ids
}

// Model is a LabelWorks printer.
type Model struct {
	Name string
	DPI  int
	// Level is Epson's capability level for the model, which decides the
	// page commands it takes.
	Level int
	// MinTapeMM and MaxTapeMM bound the tapes it takes.
	MinTapeMM, MaxTapeMM float64
}

var models = map[string]Model{}

func init() {
	// Epson's capability levels, resolutions and tapes for each model, as
	// tyalie/RE-epson-label-printer tabulates them.
	for _, m := range []Model{
		{Name: "LW-600P", DPI: 180, Level: 1, MinTapeMM: 6, MaxTapeMM: 24},
		{Name: "LW-PX400", DPI: 180, Level: 3, MinTapeMM: 4, MaxTapeMM: 24},
		{Name: "LW-C410", DPI: 180, Level: 5, MinTapeMM: 4, MaxTapeMM: 18},
	} {
		models[m.Name] = m
	}
}

// LookupModel returns a model by name.
func LookupModel(name string) (Model, bool) {
	m, ok := models[name]
	return m, ok
}

// Models lists the models by name.
func Models() []string {
	names := make([]string, 0, len(models))
	for n := range models {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MM turns dots at the model's resolution into millimeters.
func (mo Model) MM(dots int) float64 { return float64(dots) / float64(mo.DPI) * 25.4 }

// Dots turns millimeters into dots at the model's resolution.
func (mo Model) Dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// MaxLengthMM is the longest label. No source gives Epson's limit, and the
// page length the stream carries allows far more: this is the limit
// Brother's tape printers have.
const MaxLengthMM = 1000.0

// MaxLines is the longest label in lines.
func (mo Model) MaxLines() int { return mo.Dots(MaxLengthMM) }

// Takes reports whether the model takes the cartridge, and if not, why.
func (mo Model) Takes(m Media) error {
	if _, ok := printableDots[m.WidthMM]; !ok || m.WidthMM < mo.MinTapeMM || m.WidthMM > mo.MaxTapeMM {
		return fmt.Errorf("the %s does not take %s", mo.Name, m.Name)
	}
	return nil
}

// Layout is how a cartridge's labels are sent to a model.
type Layout struct {
	Model Model
	Media Media
	// PrintableDots is how many dots print across the tape: the length of
	// every raster line. The printer places the line on the tape.
	PrintableDots int
}

// Layout sets a cartridge the model takes up for printing.
func (mo Model) Layout(m Media) (Layout, error) {
	if err := mo.Takes(m); err != nil {
		return Layout{}, err
	}
	return Layout{Model: mo, Media: m, PrintableDots: printableDots[m.WidthMM]}, nil
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, cropped across the tape to its printable dots around its center, then
// turned half round. Tape is continuous, so every line is kept.
//
// The page's first row is the first raster line sent, and its first column
// the line's first dot. A label turned to run along the tape is sent from its
// left edge, its bottom edge first in each line, as the community drivers
// that have printed on the LW-600P and LW-PX400 send it.
func (l Layout) Prepare(label *image.Gray, p Placement) *image.Gray {
	return turnHalf(brotherql.PreparePage(label, p, l.PrintableDots, 0))
}

func turnHalf(src *image.Gray) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride:]
		out := dst.Pix[(h-1-y)*dst.Stride:]
		for x := 0; x < w; x++ {
			out[w-1-x] = row[x]
		}
	}
	return dst
}
