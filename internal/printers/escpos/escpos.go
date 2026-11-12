// Package escpos encodes print jobs as ESC/POS raster images (GS v 0),
// which most inexpensive receipt and label printers take, and in the
// variants Phomemo's label makers speak. Each label is sent as one bitmap,
// so none of the printer's fonts or barcode commands are used.
//
// Plain ESC/POS follows Epson's ESC/POS command reference. Phomemo
// publishes no reference: its preambles and media commands are as the
// community describes them (docs/printing.md#phomemo). Like package tspl
// it works on 1-bit images and physical sizes only.
package escpos

import (
	"fmt"
	"math"
	"sort"
)

// Family is the dialect a model speaks.
type Family string

const (
	// FamilyGeneric is plain ESC/POS: initialize, the raster, then feed
	// and cut.
	FamilyGeneric Family = "generic"
	// FamilyPhomemoM is Phomemo's M series: each label is preceded by its
	// speed, density and media type, and followed by Phomemo's own end.
	FamilyPhomemoM Family = "phomemo-m"
	// FamilyPhomemoD is Phomemo's D series and the Q30: narrow tape, each
	// label preceded by Phomemo's preamble and ended by a zero feed, so
	// the printer finds the next gap.
	FamilyPhomemoD Family = "phomemo-d"
	// FamilyPhomemoT is Phomemo's continuous paper printers, such as the
	// T02: centered, in bands, fed at the end.
	FamilyPhomemoT Family = "phomemo-t"
)

// Phomemo reports whether the family is one of Phomemo's.
func (f Family) Phomemo() bool {
	return f == FamilyPhomemoM || f == FamilyPhomemoD || f == FamilyPhomemoT
}

// Model is an ESC/POS printer.
type Model struct {
	Name   string
	Family Family
	// DPI is the head resolution: 203 on every model listed.
	DPI int
	// HeadDots is the width of the print head.
	HeadDots int
	// MaxRollMM is the widest roll the model takes, at least its head.
	MaxRollMM float64
	// RightAligned is set when the roll sits against one end of the head,
	// the end of each raster line, rather than in its middle.
	RightAligned bool
}

var models = map[string]Model{}

func init() {
	for _, m := range []Model{
		// 58 mm paper under a 48 mm head, and 80 mm paper under a 72 mm
		// one: the usual widths of 203 DPI receipt printers.
		{Name: "Generic ESC/POS 58 mm", Family: FamilyGeneric, HeadDots: 384, MaxRollMM: 58},
		{Name: "Generic ESC/POS 80 mm", Family: FamilyGeneric, HeadDots: 576, MaxRollMM: 80},
		// 48 mm heads for rolls up to 50 mm.
		{Name: "M110", Family: FamilyPhomemoM, HeadDots: 384, MaxRollMM: 50},
		{Name: "M120", Family: FamilyPhomemoM, HeadDots: 384, MaxRollMM: 50},
		// 3-inch models: a 76 mm head on the M200, 72 mm on the M220 and
		// M221, whose roll sits against one end of it on the M220.
		{Name: "M200", Family: FamilyPhomemoM, HeadDots: 608, MaxRollMM: 80},
		{Name: "M220", Family: FamilyPhomemoM, HeadDots: 576, MaxRollMM: 80, RightAligned: true},
		{Name: "M221", Family: FamilyPhomemoM, HeadDots: 576, MaxRollMM: 80},
		// 12 mm heads for tape up to 15 mm, Bluetooth only.
		{Name: "D30", Family: FamilyPhomemoD, HeadDots: 96, MaxRollMM: 15},
		{Name: "D35", Family: FamilyPhomemoD, HeadDots: 96, MaxRollMM: 15},
		{Name: "Q30", Family: FamilyPhomemoD, HeadDots: 96, MaxRollMM: 15},
		// A 48 mm head for 53 mm paper.
		{Name: "T02", Family: FamilyPhomemoT, HeadDots: 384, MaxRollMM: 53},
	} {
		m.DPI = 203
		models[m.Name] = m
	}
}

// LookupModel returns a model by name.
func LookupModel(name string) (Model, bool) {
	m, ok := models[name]
	return m, ok
}

// Models lists the model names, Phomemo's or the generic ones.
func Models(phomemo bool) []string {
	var names []string
	for n, m := range models {
		if m.Family.Phomemo() == phomemo {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// HeadMM is the width of a head of dots at the model's resolution, in
// millimeters.
func (mo Model) HeadMM(dots int) float64 { return float64(dots) / float64(mo.DPI) * 25.4 }

// Dots converts millimeters to dots at the model's resolution.
func (mo Model) Dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// Continuous labels must be between these lengths, in millimeters: BAM!'s
// own bounds, since no figure is published.
const (
	MinContinuousMM = 10
	MaxContinuousMM = 1000
)

// Media is a roll of labels or paper.
type Media struct {
	ID   string
	Name string
	// WidthMM and LengthMM are the label size; LengthMM is 0 for
	// continuous media.
	WidthMM, LengthMM float64
	// Family is the models whose roll it is; empty for custom stock, which
	// any model may take.
	Family Family
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

var catalog = map[string]Media{}

func init() {
	add := func(f Family, id, name string, w, l float64) {
		catalog[id] = Media{ID: id, Name: name, WidthMM: w, LengthMM: l, Family: f}
	}
	add(FamilyGeneric, "58mm", "58 mm paper", 58, 0)
	add(FamilyGeneric, "80mm", "80 mm paper", 80, 0)
	// Phomemo's M series rolls, as width × length.
	for _, s := range [][2]float64{{30, 20}, {40, 20}, {40, 30}, {40, 60}, {50, 20}, {50, 30}, {50, 50}, {50, 80}, {70, 80}} {
		add(FamilyPhomemoM, fmt.Sprintf("%gx%g", s[0], s[1]), fmt.Sprintf("%g × %g mm", s[0], s[1]), s[0], s[1])
	}
	add(FamilyPhomemoM, "50-continuous", "50 mm continuous", 50, 0)
	// D series tape, as width across the tape × length along it.
	for _, s := range [][2]float64{{12, 22}, {12, 30}, {12, 40}, {14, 30}, {15, 30}, {15, 40}} {
		add(FamilyPhomemoD, fmt.Sprintf("%gx%g", s[0], s[1]), fmt.Sprintf("%g × %g mm tape", s[0], s[1]), s[0], s[1])
	}
	add(FamilyPhomemoT, "53-continuous", "53 mm paper", 53, 0)
}

// LookupMedia returns a roll by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the rolls for Phomemo's models or the generic ones.
func MediaIDs(phomemo bool) []string {
	var ids []string
	for id, m := range catalog {
		if m.Family.Phomemo() == phomemo {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Takes reports whether the model takes the roll.
func (mo Model) Takes(m Media) error {
	switch {
	case m.Family != "" && m.Family != mo.Family:
		return fmt.Errorf("%s is not a roll for the %s", m.Name, mo.Name)
	case m.WidthMM > mo.MaxRollMM:
		return fmt.Errorf("%s labels are wider than the %s takes (%g mm)", m.Name, mo.Name, mo.MaxRollMM)
	case mo.Family == FamilyPhomemoD && !m.DieCut():
		return fmt.Errorf("the %s prints only on die-cut labels", mo.Name)
	case mo.Family == FamilyPhomemoT && m.DieCut():
		return fmt.Errorf("the %s prints only on continuous paper", mo.Name)
	}
	return nil
}
