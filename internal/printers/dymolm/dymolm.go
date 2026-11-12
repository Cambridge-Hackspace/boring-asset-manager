// Package dymolm encodes print jobs for DYMO LabelManager tape printers,
// which print D1 tape in the tape command language of DYMO's label makers.
//
// The commands come from DYMO's LabelWriter 450 Series Printers Technical
// Reference Manual (2009), whose Appendix B documents the language for the
// tape side of the LabelWriter Duo. DYMO has published no reference for the
// LabelManagers themselves. What they need besides (each model's head, the
// printable dots on each tape, the feed to the cutter, and the 500TS's
// framing) was learned by reading DYMO's open-source (GPL) Linux drivers and
// their PPDs; no code is taken from them. Like package ptouch, it works on
// 1-bit images and physical sizes only.
package dymolm

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// Placement says how a label image maps onto the tape.
type Placement = brotherql.Placement

// Media is a D1 tape cassette. Tape is continuous: the template sets a
// label's length, and the cassette only its width.
type Media struct {
	ID      string
	Name    string
	WidthMM float64
}

var catalog = map[string]Media{}

func init() {
	for _, w := range []int{6, 9, 12, 19, 24} {
		m := Media{ID: fmt.Sprintf("d1-%d", w), Name: fmt.Sprintf("D1 tape %d mm", w), WidthMM: float64(w)}
		catalog[m.ID] = m
	}
}

// LookupMedia returns a cassette by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the cassettes, narrowest first.
func MediaIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return catalog[ids[i]].WidthMM < catalog[ids[j]].WidthMM })
	return ids
}

// tape is where a cassette's labels fall on a model's head.
type tape struct {
	// printable is how many dots print across the tape, centered on the
	// head, and offset how far DYMO's driver moves them from the center,
	// in dots toward the head's last.
	printable, offset int
}

// Model is a LabelManager.
type Model struct {
	Name string
	DPI  int
	// HeadDots is the width of the print head; a full line is HeadDots/8
	// bytes.
	HeadDots int
	// AutoCut: the model has an automatic cutter. The others have one the
	// user presses.
	AutoCut bool
	// Framed: the stream is sent in ESC Y blocks, as DYMO's driver sends
	// the 500TS's.
	Framed bool
	// LeaderLines is the tape DYMO's driver feeds before and after each
	// label, CutterLines how far the cutter is ahead of the head, and
	// MinLines the shortest label it prints, all in lines.
	LeaderLines, CutterLines, MinLines int
	tapes                              map[float64]tape
}

var models = map[string]Model{}

func init() {
	// The figures are those DYMO's Linux driver gives each model: its
	// head, leader, cutter and shortest label. Each tape's printable dots
	// are the width of DYMO's imageable area for it, from the model's PPD:
	// lmpnp.ppd for the PnP, lmwpnp.ppd for the Wireless PnP, and for the
	// 420P, which has none, lm450.ppd, for a head of the same size and
	// resolution. DYMO's driver sets the 500TS up as the Wireless PnP.
	pnp := Model{DPI: 180, HeadDots: 64, LeaderLines: 75, CutterLines: 58, MinLines: 30,
		tapes: map[float64]tape{6: {30, 0}, 9: {50, 0}, 12: {58, 0}}}
	add := func(m Model, names ...string) {
		for _, n := range names {
			m.Name = n
			models[n] = m
		}
	}
	// DYMO's driver does not know the 280, which takes the PnP's tapes and
	// is driven as the PnP by community drivers: it is taken to be one.
	add(pnp, "LabelManager PnP", "LabelManager 280")
	add(Model{DPI: 180, HeadDots: 128, LeaderLines: 75, CutterLines: 58, MinLines: 63,
		tapes: map[float64]tape{6: {30, 0}, 9: {50, 0}, 12: {72, 0}, 19: {100, 0}}}, "LabelManager 420P")
	wireless := Model{DPI: 300, HeadDots: 256, AutoCut: true, LeaderLines: 125, CutterLines: 92, MinLines: 222,
		tapes: map[float64]tape{6: {60, 0}, 9: {83, 0}, 12: {106, 0}, 19: {166, 0}, 24: {213, 0}}}
	add(wireless, "LabelManager Wireless PnP")
	ts := wireless
	ts.Framed = true
	ts.tapes = map[float64]tape{6: {60, 0}, 9: {83, 0}, 12: {106, 2}, 19: {166, -4}, 24: {213, 0}}
	add(ts, "LabelManager 500TS")
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

// MaxLengthMM is the longest label: the tallest custom page DYMO's PPDs
// allow, 12,800 points.
const MaxLengthMM = 12800.0 / 72 * 25.4

// MaxLines is the longest label in lines.
func (mo Model) MaxLines() int { return int(MaxLengthMM / 25.4 * float64(mo.DPI)) }

// Takes reports whether the model takes the cassette, and if not, why.
func (mo Model) Takes(m Media) error {
	if _, ok := mo.tapes[m.WidthMM]; !ok {
		return fmt.Errorf("the %s does not take %s", mo.Name, m.Name)
	}
	return nil
}

// Layout is where a cassette's labels fall on a model's head.
type Layout struct {
	Model Model
	Media Media
	// PrintableDots is how many dots print across the tape, and FirstDot
	// the head's dot the first of them falls on.
	PrintableDots, FirstDot int
}

// Layout places a cassette the model takes on its head: centered, as DYMO's
// driver places it, moved by its offset for the tape.
func (mo Model) Layout(m Media) (Layout, error) {
	if err := mo.Takes(m); err != nil {
		return Layout{}, err
	}
	t := mo.tapes[m.WidthMM]
	return Layout{Model: mo, Media: m, PrintableDots: t.printable, FirstDot: (mo.HeadDots-t.printable)/2 + t.offset}, nil
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, cropped across the tape to its printable dots around its center, then
// turned half round. Tape is continuous, so every line is kept.
//
// The page's first row is the first line sent, and its first column the
// head's first printable dot. A label turned to run along the tape is sent
// from its left edge, its bottom edge on the head's first dot, as DYMO's
// PPDs turn landscape pages (a quarter turn clockwise) and as community
// drivers print.
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
