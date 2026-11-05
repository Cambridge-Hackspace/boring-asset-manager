// Package zebra encodes print jobs in Zebra's label languages: ZPL II,
// which Zebra's printers speak and many others emulate (Honeywell, TSC,
// Godex, Citizen, Brother's TD series in ZPL mode, and inexpensive 4-inch
// printers), and EPL2, which the older LP and TLP 2824 and 2844 speak.
// Each label is sent as one graphic, so none of the printer's fonts or
// barcode commands are used.
//
// It follows Zebra's ZPL II Programming Guide and EPL2 Programming Guide.
// Like package tspl it works on 1-bit images and physical sizes only.
package zebra

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// Language is the label language a model speaks.
type Language string

const (
	ZPL  Language = "zpl"
	EPL2 Language = "epl2"
)

// Model is a Zebra or Zebra-compatible printer.
type Model struct {
	Name     string
	Language Language
	// DPI is the head resolution: 203 (8 dots per mm), 300 (12) or 600
	// (24).
	DPI int
	// HeadDots is the width of the print head, as the model's maximum print
	// width.
	HeadDots int
}

var models = map[string]Model{}

func init() {
	add := func(lang Language, name string, dpi, dots int) {
		models[name] = Model{Name: name, Language: lang, DPI: dpi, HeadDots: dots}
	}
	// Zebra's figures for maximum print width: 4.09 in (832 dots) at 203
	// DPI on the 4-inch desktops and industrials, 4.16 in at 300, 4.27 in
	// on the ZD620 and ZD621 at 300, 4.0 in at 600, and 2.2 in on the
	// 2-inch ZD410.
	for _, m := range []string{"Zebra GK420d", "Zebra GK420t", "Zebra GX420d", "Zebra ZD220"} {
		add(ZPL, m, 203, 832)
	}
	add(ZPL, "Zebra GX430t", 300, 1248)
	add(ZPL, "Zebra ZD410", 203, 448)
	for _, m := range []string{"Zebra ZD420", "Zebra ZD421", "Zebra ZT230"} {
		add(ZPL, m+" 203 DPI", 203, 832)
		add(ZPL, m+" 300 DPI", 300, 1248)
	}
	for _, m := range []string{"Zebra ZD620", "Zebra ZD621"} {
		add(ZPL, m+" 203 DPI", 203, 832)
		add(ZPL, m+" 300 DPI", 300, 1280)
	}
	for _, m := range []string{"Zebra ZT410", "Zebra ZT411"} {
		add(ZPL, m+" 203 DPI", 203, 832)
		add(ZPL, m+" 300 DPI", 300, 1248)
		add(ZPL, m+" 600 DPI", 600, 2400)
	}
	// Emulators, by resolution and head: 4 inches unless named.
	add(ZPL, "Generic ZPL 203 DPI", 203, 832)
	add(ZPL, "Generic ZPL 300 DPI", 300, 1248)
	add(ZPL, "Generic ZPL 600 DPI", 600, 2400)
	add(ZPL, "Generic ZPL 2 in 203 DPI", 203, 448)
	add(ZPL, "Generic ZPL 8 in 203 DPI", 203, 1728)

	// EPL2: the 2824s have a 2.2 in head, the 2844s 4.09 in.
	add(EPL2, "Zebra LP 2824", 203, 448)
	add(EPL2, "Zebra TLP 2824", 203, 448)
	add(EPL2, "Zebra LP 2844", 203, 832)
	add(EPL2, "Zebra TLP 2844", 203, 832)
	add(EPL2, "Generic EPL2 203 DPI", 203, 832)
}

// LookupModel returns a model by name, in a language.
func LookupModel(lang Language, name string) (Model, bool) {
	m, ok := models[name]
	return m, ok && m.Language == lang
}

// Models lists the names of the models that speak a language.
func Models(lang Language) []string {
	var names []string
	for n, m := range models {
		if m.Language == lang {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// HeadMM is the width of the model's head in millimeters.
func (mo Model) HeadMM() float64 { return float64(mo.HeadDots) / float64(mo.DPI) * 25.4 }

// MaxLabelMM is the widest roll the model takes: a little wider than its
// head, which prints all it can reach.
func (mo Model) MaxLabelMM() float64 { return mo.HeadMM() + 2 }

// Dots is a length in millimeters as the model's dots.
func (mo Model) Dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// Media is a roll of labels.
type Media struct {
	ID   string
	Name string
	// WidthMM and LengthMM are the label size; LengthMM is 0 for
	// continuous media.
	WidthMM, LengthMM float64
	// GapMM is the gap between die-cut labels.
	GapMM float64
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

// standardGap is the gap on common die-cut thermal label rolls (1/8 in).
const standardGap = 3.2

var catalog = map[string]Media{}

func init() {
	inch := func(id, name string, w, l float64) Media {
		gap := standardGap
		if l == 0 {
			gap = 0
		}
		mm := func(in float64) float64 { return math.Round(in*25.4*100) / 100 }
		return Media{ID: id, Name: name, WidthMM: mm(w), LengthMM: mm(l), GapMM: gap}
	}
	for _, m := range []Media{
		inch("4x6", "4 × 6 in shipping", 4, 6),
		inch("4x4", "4 × 4 in", 4, 4),
		inch("4x3", "4 × 3 in", 4, 3),
		inch("4x2", "4 × 2 in", 4, 2),
		inch("4x1", "4 × 1 in", 4, 1),
		inch("3x2", "3 × 2 in", 3, 2),
		inch("3x1", "3 × 1 in", 3, 1),
		inch("2.25x1.25", "2.25 × 1.25 in", 2.25, 1.25),
		inch("2x1", "2 × 1 in", 2, 1),
		inch("1.5x1", "1.5 × 1 in", 1.5, 1),
		inch("1x1", "1 × 1 in", 1, 1),
		inch("continuous-4", "4 in continuous", 4, 0),
		inch("continuous-2", "2 in continuous", 2, 0),
	} {
		catalog[m.ID] = m
	}
}

// LookupMedia returns a roll by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the common rolls.
func MediaIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Suits reports whether the roll fits in the model.
func (mo Model) Suits(m Media) error {
	if m.WidthMM > mo.MaxLabelMM() {
		return fmt.Errorf("%s labels are wider than the %s head (%.0f mm)", m.Name, mo.Name, mo.HeadMM())
	}
	return nil
}

// Continuous labels must be between these lengths, in millimeters.
const (
	MinContinuousMM = 10
	MaxContinuousMM = 1000
)

// Placement says how a label image maps onto the roll.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the roll.
	Rotate bool
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, then cut to what the head reaches, around its center.
func Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, min(w, mo.HeadDots), h))
	offX := (w - page.Rect.Dx()) / 2
	for y := 0; y < h; y++ {
		copy(page.Pix[y*page.Stride:y*page.Stride+page.Rect.Dx()], src.Pix[y*src.Stride+offX:])
	}
	return page
}

func rotateCounterclockwise(src *image.Gray) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// (x, y) moves to (y, w-1-x).
			dst.Pix[(w-1-x)*dst.Stride+y] = src.Pix[y*src.Stride+x]
		}
	}
	return dst
}

// rows packs a page into rows of bytes, most significant bit first, with
// a bit set where a dot prints: a pixel below 50% gray.
func rows(page *image.Gray) (widthBytes int, out [][]byte) {
	w, h := page.Rect.Dx(), page.Rect.Dy()
	widthBytes = (w + 7) / 8
	out = make([][]byte, h)
	for y := 0; y < h; y++ {
		row := make([]byte, widthBytes)
		src := page.Pix[y*page.Stride:]
		for x := 0; x < w; x++ {
			if src[x] < 0x80 {
				row[x/8] |= 0x80 >> uint(x%8)
			}
		}
		out[y] = row
	}
	return widthBytes, out
}

// checkPages refuses an empty job, or a page wider than the head.
func checkPages(mo Model, pages []*image.Gray) error {
	if len(pages) == 0 {
		return fmt.Errorf("%s: a job needs at least one page", mo.Language)
	}
	for i, page := range pages {
		if page.Rect.Dx() > mo.HeadDots {
			return fmt.Errorf("%s: page %d is %d dots wide, the %s head has %d", mo.Language, i, page.Rect.Dx(), mo.Name, mo.HeadDots)
		}
	}
	return nil
}

// runs groups a job's pages: a run of the same page is printed as copies
// of one.
func runs(pages []*image.Gray, each func(page *image.Gray, copies int) error) error {
	for i := 0; i < len(pages); {
		copies := 1
		for i+copies < len(pages) && pages[i+copies] == pages[i] {
			copies++
		}
		if err := each(pages[i], copies); err != nil {
			return err
		}
		i += copies
	}
	return nil
}

// labelSize is the label's width and length in dots: the media's, or for
// continuous media, as long as the page.
func labelSize(mo Model, m Media, page *image.Gray) (width, length int) {
	width = min(mo.Dots(m.WidthMM), mo.HeadDots)
	if m.DieCut() {
		return width, mo.Dots(m.LengthMM)
	}
	return width, page.Rect.Dy()
}
