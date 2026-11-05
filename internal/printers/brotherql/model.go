package brotherql

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// Series is a family of Brother printers that share how media works: the
// QL range takes DK rolls, which it identifies and Brother tabulates; the
// TD series takes any roll its head spans, known by size.
type Series string

const (
	SeriesQL Series = "QL"
	SeriesTD Series = "TD"
)

// Model is a printer that speaks Brother's raster command language: its
// head, and which of the language's commands it takes.
type Model struct {
	Name   string
	Series Series
	// DPI is the head resolution, the same across and along the tape.
	DPI int
	// Pins is the width of the head; each raster line is Pins/8 bytes.
	Pins int
	// Invalidate is how many NUL bytes start a job, clearing whatever the
	// printer was part way through receiving.
	Invalidate int

	// ModeSwitch: the model takes "switch dynamic command mode" (ESC i a)
	// and must be switched to raster mode; the rest speak nothing else.
	ModeSwitch bool
	// VariousMode: the model takes "various mode" (ESC i M).
	VariousMode bool
	// Cutter: the model can cut labels, by bit 6 of various mode. On the
	// TD-4 series the cutter is an optional unit.
	Cutter bool
	// CutEvery: the model takes "cut each * labels" (ESC i A).
	CutEvery bool
	// Expanded: the model takes "expanded mode" (ESC i K), whose bit 3
	// cuts at the end of the job.
	Expanded bool
	// Compression: the model takes TIFF (PackBits) raster lines ("M" 02).
	Compression bool
	// InfoFlags is the valid flag of the print information command (ESC
	// i z): which of its fields the printer checks against the roll.
	InfoFlags byte
	// SmallRoundFeed: the model needs a 35-dot margin on 12 mm round
	// labels, where other die-cut labels take none.
	SmallRoundFeed bool

	// MinLines and MaxLines bound continuous labels, in raster lines.
	// CutMinLines, when set, is the shortest label the cutter cuts.
	MinLines, MaxLines, CutMinLines int

	// table is the head's pin table, and lines the model's line, for DK
	// rolls; MinMediaMM and MaxMediaMM bound the rolls a TD model takes.
	table                  pinTable
	lines                  lineSet
	MinMediaMM, MaxMediaMM float64
}

// Print information valid flags (ESC i z {n1}).
const (
	infoKind     = 0x02
	infoWidth    = 0x04
	infoLength   = 0x08
	infoQuality  = 0x40
	infoRecovery = 0x80
)

// qlInfoFlags are the QL range's: recovery on, media type, width, length
// and quality fields valid, so the printer refuses a job for another roll.
const qlInfoFlags = infoRecovery | infoKind | infoWidth | infoLength | infoQuality

var models = map[string]Model{}

func init() {
	// The QL range, at 300 DPI. Each model's commands are the ones its
	// reference lists for it; where the QL-500 to QL-1060N reference's
	// overview (section 3.1) and command details (section 5) disagree on
	// which models take "cut each * labels" and expanded mode, the overview's
	// narrower list is used, since both commands only restate the printer's
	// defaults when cutting.
	ql500 := Model{
		Series: SeriesQL, DPI: 300, Pins: 720, Invalidate: 200, InfoFlags: qlInfoFlags,
		MinLines: 295, MaxLines: 11811, table: pins720, lines: lineQL500,
	}
	add := func(base Model, names ...string) {
		for _, n := range names {
			m := base
			m.Name = n
			models[n] = m
		}
	}
	// QL-500 to QL-1060N reference, version 6.0: sections 3.1 and 5 for the
	// commands, 3.2.3 for the feed and 3.2.4 for the lengths. The QL-500 has
	// no cutter and takes no mode setting; only the QL-580N and QL-650TD of
	// the 62 mm models take raster mode switching and TIFF, and the
	// QL-650TD's TIFF only over its serial port, so it is sent uncompressed.
	// The reference gives no invalidate length; 200 bytes, as in the later
	// references, are sent.
	add(with(ql500, func(m *Model) { m.SmallRoundFeed = true }), "QL-500")
	add(with(ql500, func(m *Model) { m.VariousMode, m.Cutter, m.SmallRoundFeed = true, true, true }), "QL-550", "QL-560")
	cutting := with(ql500, func(m *Model) {
		m.VariousMode, m.Cutter, m.CutEvery, m.Expanded, m.SmallRoundFeed = true, true, true, true, true
		m.MinLines = 150
	})
	add(cutting, "QL-570", "QL-700")
	add(with(cutting, func(m *Model) { m.ModeSwitch, m.Compression = true, true }), "QL-580N")
	add(with(ql500, func(m *Model) { m.ModeSwitch, m.VariousMode, m.Cutter, m.Expanded = true, true, true, true }), "QL-650TD")
	add(with(ql500, func(m *Model) {
		m.Pins, m.table, m.lines = 1296, pins1296QL1050, lineQL1050
		m.ModeSwitch, m.VariousMode, m.Cutter, m.CutEvery, m.Expanded, m.Compression = true, true, true, true, true, true
		m.MaxLines = 35433
	}), "QL-1050", "QL-1060N")

	// QL-600/710W/720NW reference, version 1.02: section 2.1, 2.3.3 and
	// 2.3.4. (The QL-600 shares the reference but not, it says, every
	// command, so is left out.)
	full := with(ql500, func(m *Model) {
		m.ModeSwitch, m.VariousMode, m.Cutter, m.CutEvery, m.Expanded, m.Compression = true, true, true, true, true, true
	})
	add(with(full, func(m *Model) { m.lines, m.MinLines = lineQL710, 150 }), "QL-710W", "QL-720NW")

	// QL-800 series reference, version 1.01. The reference asks for 400
	// invalidate bytes and allows continuous labels from 150 lines; the
	// QL-810W and QL-820NWB have always been sent 200 and held to 295, and
	// the QL-800 is kept with them. The QL-800 alone takes no compression
	// (section 4, "Select compression mode").
	add(with(full, func(m *Model) { m.lines = lineQL800 }), "QL-810W", "QL-820NWB")
	add(with(full, func(m *Model) { m.lines, m.Compression = lineQL800, false }), "QL-800")

	// QL-1100 series reference, version 1.00: sections 2.1, 2.3.3, 2.3.4.
	ql1100 := with(full, func(m *Model) {
		m.Pins, m.table, m.lines, m.Invalidate = 1296, pins1296QL1100, lineQL1100, 350
		m.MinLines, m.MaxLines = 301, 35434
	})
	add(ql1100, "QL-1100", "QL-1110NWB")
	add(with(ql1100, func(m *Model) { m.lines = lineQL1115 }), "QL-1115NWB")

	// The TD-2 series: TD-2000 series reference, version 1.01, sections
	// 2.1, 2.3 and 4. It takes neither "cut each * labels" nor expanded
	// mode, and its various mode has no cutter bit (bit 3 turns the print
	// over, bit 4 is the peeler). Brother's own example of the print
	// information command validates only the type and width; with custom
	// stock nothing is validated, since the printer is set up for the roll
	// on its own (see EncodeJob). Media is 19-63 mm wide (TD-2020/2125N/
	// 2135N User's Guide, "Product Specifications").
	td2 := Model{
		Series: SeriesTD, Invalidate: 200, ModeSwitch: true, VariousMode: true, Compression: true,
		InfoFlags: infoRecovery | infoQuality, lines: lineTD2, MinMediaMM: 19, MaxMediaMM: 63,
	}
	add(with(td2, func(m *Model) { m.DPI, m.Pins, m.MinLines, m.MaxLines = 203, 448, 96, 7992 }),
		"TD-2020", "TD-2120N", "TD-2125N", "TD-2125NWB")
	add(with(td2, func(m *Model) { m.DPI, m.Pins, m.MinLines, m.MaxLines = 300, 672, 142, 11811 }),
		"TD-2030A", "TD-2130N", "TD-2135N", "TD-2135NWB")

	// The TD-4 series (direct thermal; the TD-4T thermal transfer models
	// speak FBPL and ZPL, not this language): TD-4000 series reference,
	// version 1.03, sections 4.1, 4.3 and 7. Its commands are the QL's; the
	// cutter is an optional unit and cuts nothing shorter than 20 mm. The
	// invalidate length depends on the model (section 7, "Invalidate").
	// Brother's example print information validates nothing. Media is
	// 19-118 mm wide (TD-4210D/4410D/4420DN/4520DN User's Guide, "Media
	// Specification"). The linerless models are left out.
	td4 := Model{
		Series: SeriesTD, ModeSwitch: true, VariousMode: true, Cutter: true, CutEvery: true, Expanded: true, Compression: true,
		InfoFlags: infoRecovery, lines: lineTD4, MinMediaMM: 19, MaxMediaMM: 118,
	}
	td4at203 := with(td4, func(m *Model) { m.DPI, m.Pins, m.MinLines, m.MaxLines, m.CutMinLines = 203, 832, 96, 23977, 160 })
	td4at300 := with(td4, func(m *Model) { m.DPI, m.Pins, m.MinLines, m.MaxLines, m.CutMinLines = 300, 1280, 142, 35433, 236 })
	add(with(td4at203, func(m *Model) { m.Invalidate = 350 }), "TD-4210D", "TD-4410D", "TD-4420DN")
	add(with(td4at300, func(m *Model) { m.Invalidate = 350 }), "TD-4510D", "TD-4520DN", "TD-4550DNWB")
	add(with(td4at203, func(m *Model) { m.Invalidate = 661 }), "TD-4215D", "TD-4415D", "TD-4425DN", "TD-4455DNWB")
	add(with(td4at300, func(m *Model) { m.Invalidate = 661 }), "TD-4525DN", "TD-4555DNWB")
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

// Models lists a series' models by name.
func Models(s Series) []string {
	var names []string
	for n, m := range models {
		if m.Series == s {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// dots turns millimeters into dots at the model's resolution.
func (mo Model) dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// MM turns dots at the model's resolution into millimeters.
func (mo Model) MM(dots int) float64 { return float64(dots) / float64(mo.DPI) * 25.4 }

// Edge offsets of TD media: the print area of every RD roll in the TD
// references (sections 2.3.2 and 4.3.2) starts 1.5 mm in from each side
// and, on die-cut labels, 3 mm in from each end; continuous labels are fed
// a 3 mm margin (sections 2.3.3 and 4.3.3).
const (
	tdSideMM = 1.5
	tdEndMM  = 3.0
)

// Takes reports whether the model takes the roll, and if not, why.
func (mo Model) Takes(m Media) error {
	if mo.Series == SeriesQL {
		if m.sized() || m.lines&mo.lines == 0 || m.right[mo.table] == none {
			return fmt.Errorf("the %s does not take %s", mo.Name, m.Name)
		}
		return nil
	}
	if !m.sized() || m.lines&mo.lines == 0 {
		return fmt.Errorf("the %s does not take %s", mo.Name, m.Name)
	}
	if m.WidthMM < mo.MinMediaMM || m.WidthMM > mo.MaxMediaMM {
		return fmt.Errorf("the %s takes rolls %g-%g mm wide, not %g mm", mo.Name, mo.MinMediaMM, mo.MaxMediaMM, m.WidthMM)
	}
	if m.DieCut() {
		if mo.dots(m.LengthMM)-2*mo.dots(tdEndMM) < 1 {
			return fmt.Errorf("the %s prints nothing on a label %g mm long", mo.Name, m.LengthMM)
		}
		if mo.dots(m.LengthMM) > mo.MaxLines {
			return fmt.Errorf("the %s prints labels at most %g mm long", mo.Name, math.Floor(mo.MM(mo.MaxLines)))
		}
	}
	return nil
}

// Layout is where a roll's labels fall on a model's head.
type Layout struct {
	Model Model
	Media Media
	// PrintableDots is how many pins print across the roll, and
	// RightMarginDots how many sit beside it on the right.
	PrintableDots, RightMarginDots int
	// PrintableLines is the printable length of a die-cut label, in raster
	// lines; 0 on a continuous roll.
	PrintableLines int
	// FeedDots is the margin sent with each label.
	FeedDots int
}

// Layout places a roll the model takes on its head: by Brother's tables for
// a DK roll, and for a roll known by size by the TD references' offsets,
// centered on the head as the TD references' tables of pins have every RD
// roll. Those offsets give Brother's printable pins for each RD roll to
// within a dot, except that a 60 mm label on a 300 DPI TD-2, which is wider
// than the head, is printed across the whole head where Brother keeps 2 mm
// off each side.
func (mo Model) Layout(m Media) (Layout, error) {
	if err := mo.Takes(m); err != nil {
		return Layout{}, err
	}
	l := Layout{Model: mo, Media: m}
	if mo.Series == SeriesQL {
		l.PrintableDots, l.RightMarginDots, l.PrintableLines = m.PrintableDots, m.right[mo.table], m.PrintableLines
		// Continuous tape is fed a 3 mm margin, die-cut labels none.
		switch {
		case !m.DieCut():
			l.FeedDots = 35
		case mo.SmallRoundFeed && m.ID == "dk-11219":
			l.FeedDots = 35
		}
		return l, nil
	}
	l.PrintableDots = min(mo.Pins, mo.dots(m.WidthMM)-2*mo.dots(tdSideMM))
	l.RightMarginDots = (mo.Pins - l.PrintableDots) / 2
	if m.DieCut() {
		l.PrintableLines = mo.dots(m.LengthMM) - 2*mo.dots(tdEndMM)
	} else {
		l.FeedDots = mo.dots(tdEndMM)
	}
	return l, nil
}

// Prepare turns a rendered label into a page for the layout: rotated if the
// placement says so, then cropped to the printable area around its center.
func (l Layout) Prepare(label *image.Gray, p Placement) *image.Gray {
	return prepare(label, p, l.PrintableDots, l.PrintableLines)
}
