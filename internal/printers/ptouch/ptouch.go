// Package ptouch encodes print jobs for Brother P-touch tape printers in
// their raster command language: a close relative of the QL's (package
// brotherql), with its own tape cassettes, cut modes and raster transfer.
//
// The figures come from Brother's raster command references, linked from
// https://support.brother.com/g/s/es/dev/en/command/reference/index.html:
//
//   - Software Developer's Manual, Raster Command Reference PT-H500/P700/E500,
//     version 1.11, cv_pth500p700e500_eng_raster_111.pdf;
//   - Raster Command Reference PT-E550W/P750W/P710BT, version 1.02,
//     cv_pte550wp750wp710bt_eng_raster_102.pdf;
//   - Raster Command Reference PT-P900/P900W/P950NW/P910BT, version 1.02,
//     cv_ptp900_eng_raster_102.pdf.
//
// Section numbers below are those references'. Like package brotherql, it
// works on 1-bit images and physical sizes only.
package ptouch

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// Placement says how a label image maps onto the tape.
type Placement = brotherql.Placement

// Kind is what a cassette holds.
type Kind int

const (
	// TZe is laminated (or non-laminated) tape.
	TZe Kind = iota
	// HS21 and HS31 are heat-shrink tube with 2:1 and 3:1 shrink ratios.
	HS21
	HS31
)

// Media is a tape cassette.
type Media struct {
	ID   string
	Name string
	// WidthMM is the tape, or the flattened tube, across.
	WidthMM float64
	Kind    Kind
	// infoWidth is the width the printer reports for the cassette, and is
	// sent in the print information command (section 4, "Media width and
	// length"): 3.5 mm tape is 4.
	infoWidth int
}

// Media types in the print information command and the status (section 4,
// "Print information command" and "Media type").
const (
	typeTape = 0x00 // as Brother's examples send for TZe tape
	typeHS21 = 0x11
	typeHS31 = 0x17
)

func (m Media) mediaType() byte {
	switch m.Kind {
	case HS21:
		return typeHS21
	case HS31:
		return typeHS31
	}
	return typeTape
}

// head names one of the two print heads the references tabulate.
type head int

const (
	// head128 is the 180 DPI, 128-pin head of the PT-P700, PT-E550W,
	// PT-P750W and PT-P710BT: section 2.3.5 of their references.
	head128 head = iota
	// head560 is the 360 DPI, 560-pin head of the PT-P900 series: section
	// 2.3.5 of its reference.
	head560
	numHeads
)

// pins is where a cassette sits on a head: its printable pins, and the pins
// beside it on the right, which come first in each raster line.
type pins struct{ printable, right int }

// catalog is every cassette, with its place on each head that takes it.
// The heads' tables (section 2.3.5) give each width's printable pins and
// the pins beside it; their page sizes (section 2.3.2) agree. Tape 36 mm
// wide, and 31 mm tube, fit only the 560-pin head.
var catalog = map[string]Media{}

var placements = map[string][numHeads]*pins{}

func init() {
	add := func(m Media, on128, on560 *pins) {
		catalog[m.ID] = m
		placements[m.ID] = [numHeads]*pins{on128, on560}
	}
	p := func(printable, right int) *pins { return &pins{printable, right} }
	tape := func(id string, width float64, info int) Media {
		return Media{ID: id, Name: "TZe tape " + mmText(width) + " mm", WidthMM: width, Kind: TZe, infoWidth: info}
	}
	tube := func(id string, width float64, kind Kind, info int) Media {
		ratio := "2:1"
		if kind == HS31 {
			ratio = "3:1"
		}
		return Media{ID: id, Name: "HSe tube " + mmText(width) + " mm (" + ratio + ")", WidthMM: width, Kind: kind, infoWidth: info}
	}
	add(tape("tze-3.5", 3.5, 4), p(24, 52), p(48, 264))
	add(tape("tze-6", 6, 6), p(32, 48), p(64, 256))
	add(tape("tze-9", 9, 9), p(50, 39), p(106, 235))
	add(tape("tze-12", 12, 12), p(70, 29), p(150, 213))
	add(tape("tze-18", 18, 18), p(112, 8), p(234, 171))
	add(tape("tze-24", 24, 24), p(128, 0), p(320, 128))
	add(tape("tze-36", 36, 36), nil, p(454, 61))
	// The PT-P900 reference gives the 2:1 tubes' reported widths; the 3:1
	// tubes' are given nowhere, so they are sent rounded, and only their
	// type is checked (see EncodeJob).
	add(tube("hse-5.8", 5.8, HS21, 6), p(28, 50), p(56, 260))
	add(tube("hse-8.8", 8.8, HS21, 9), p(48, 40), p(96, 240))
	add(tube("hse-11.7", 11.7, HS21, 12), p(66, 31), p(132, 222))
	add(tube("hse-17.7", 17.7, HS21, 18), p(106, 11), p(212, 182))
	add(tube("hse-23.6", 23.6, HS21, 24), p(128, 0), p(256, 160))
	add(tube("hse-5.2", 5.2, HS31, 5), p(20, 54), p(40, 268))
	add(tube("hse-9", 9, HS31, 9), p(44, 42), p(88, 244))
	add(tube("hse-11.2", 11.2, HS31, 11), p(50, 39), p(100, 238))
	add(tube("hse-21", 21, HS31, 21), p(120, 4), p(240, 168))
	add(tube("hse-31", 31, HS31, 31), nil, p(360, 108))
}

func mmText(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// LookupMedia returns a cassette by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the cassettes.
func MediaIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Model is a P-touch printer.
type Model struct {
	Name string
	DPI  int
	// Pins is the width of the head; each raster line is Pins/8 bytes.
	Pins int
	head head
	// Invalidate is how many NUL bytes start a job (section 2.1).
	Invalidate int
	// Raster is the raster graphics transfer command: "g" on the
	// PT-P700, "G" on the rest (section 4). Both take a two-byte length.
	Raster byte
	// HalfCut: the model takes the half cut bit of advanced mode.
	HalfCut bool
	// CutEvery: the model takes "cut each * labels" (ESC i A).
	CutEvery bool
	// LastPage: the print information marks the last page with 2, as the
	// PT-P900 series' does, where the others mark only the first.
	LastPage bool
	// Tube: the model takes heat-shrink tube.
	Tube bool
	// MinMarginDots is the shortest feed before and after a label, and
	// MinLines, MaxLines and MaxTubeLines bound a label's length, in raster
	// lines (sections 2.3.3 and 2.3.4).
	MinMarginDots, MinLines, MaxLines, MaxTubeLines int
}

var models = map[string]Model{}

func init() {
	// PT-H500/P700/E500 reference: 180 DPI, 128 pins, a 100-byte
	// invalidate, "g" for raster lines, no half cut and no "cut each *
	// labels". Labels 4.4-1000 mm (31-7086 lines), tube to 500 mm (3543),
	// with margins from 2 mm (14 dots).
	p700 := Model{
		DPI: 180, Pins: 128, head: head128, Invalidate: 100, Raster: 'g', Tube: true,
		MinMarginDots: 14, MinLines: 31, MaxLines: 7086, MaxTubeLines: 3543,
	}
	add := func(m Model, names ...string) {
		for _, n := range names {
			m.Name = n
			models[n] = m
		}
	}
	add(p700, "PT-P700")
	// PT-E550W/P750W/P710BT reference: as the PT-P700, with "G", the half
	// cut and "cut each * labels" (neither on the PT-P710BT).
	e550 := p700
	e550.Raster = 'G'
	add(with(e550, func(m *Model) { m.HalfCut, m.CutEvery = true, true }), "PT-E550W", "PT-P750W")
	add(e550, "PT-P710BT")
	// PT-P900/P900W/P950NW/P910BT reference: 360 DPI, 560 pins, a 200-byte
	// invalidate, margins from 1 mm (14 dots), labels 4-1000 mm (57-14173
	// lines), tube 4.2-500 mm (to 7087), which the PT-P910BT does not take.
	p900 := Model{
		DPI: 360, Pins: 560, head: head560, Invalidate: 200, Raster: 'G', HalfCut: true, CutEvery: true, LastPage: true, Tube: true,
		MinMarginDots: 14, MinLines: 57, MaxLines: 14173, MaxTubeLines: 7087,
	}
	add(p900, "PT-P900", "PT-P900W", "PT-P950NW")
	add(with(p900, func(m *Model) { m.Tube = false }), "PT-P910BT")
}

func with(m Model, change func(*Model)) Model {
	change(&m)
	return m
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

// MaxLinesFor is the longest label the model prints on the cassette.
func (mo Model) MaxLinesFor(m Media) int {
	if m.Kind != TZe {
		return mo.MaxTubeLines
	}
	return mo.MaxLines
}

// Takes reports whether the model takes the cassette, and if not, why.
func (mo Model) Takes(m Media) error {
	if placements[m.ID][mo.head] == nil {
		return fmt.Errorf("the %s does not take %s", mo.Name, m.Name)
	}
	if m.Kind != TZe && !mo.Tube {
		return fmt.Errorf("the %s does not take heat-shrink tube", mo.Name)
	}
	return nil
}

// Layout is where a cassette's labels fall on a model's head.
type Layout struct {
	Model Model
	Media Media
	// PrintableDots is how many pins print across the tape, and
	// RightMarginDots how many sit beside it on the right.
	PrintableDots, RightMarginDots int
}

// Layout places a cassette the model takes on its head, by Brother's table.
func (mo Model) Layout(m Media) (Layout, error) {
	if err := mo.Takes(m); err != nil {
		return Layout{}, err
	}
	p := placements[m.ID][mo.head]
	return Layout{Model: mo, Media: m, PrintableDots: p.printable, RightMarginDots: p.right}, nil
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, then cropped across the tape to its printable pins around its center.
// Tape is continuous, so every line is kept.
func (l Layout) Prepare(label *image.Gray, p Placement) *image.Gray {
	return brotherql.PreparePage(label, p, l.PrintableDots, 0)
}
