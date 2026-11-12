// Package industrial encodes print jobs in the label languages of
// industrial printers: DPL, which Datamax-O'Neil and Honeywell printers
// speak; SBPL, SATO's; and EZPL, Godex's. Most of these printers also
// emulate ZPL (package zebra), so these languages are for those that do not,
// or are set not to. Each label is sent as one graphic, so none of the
// printer's fonts or barcode commands are used.
//
// It follows Datamax-O'Neil's Class Series Programmer's Manual (DPL),
// SATO's CL4NX Programming Reference (SBPL) and Godex's EZPL Programmer's
// Manual. Like package zebra it works on 1-bit images and physical sizes
// only, and it takes zebra's media, which is given by its size.
package industrial

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// Language is the label language a model speaks.
type Language string

const (
	DPL  Language = "dpl"
	SBPL Language = "sbpl"
	EZPL Language = "ezpl"
)

// Model is a printer that speaks one of the languages.
type Model struct {
	Name     string
	Language Language
	// DPI is the head resolution: 203 (8 dots per mm), 300 (12) or 600
	// (24). SATO calls the last two 305 and 609.
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
	// DPL. Datamax-O'Neil's maximum print widths: 4.1 in at 203 DPI and
	// 4.16 in at 300 and 600 on the 4-inch E-, M- and I-Class; Honeywell's
	// 108 mm at 203 DPI and 106 mm at 300 on the PM43.
	for _, class := range []string{"E-Class", "M-Class", "I-Class"} {
		add(DPL, "Datamax "+class+" 203 DPI", 203, 832)
		add(DPL, "Datamax "+class+" 300 DPI", 300, 1248)
	}
	add(DPL, "Datamax I-Class 600 DPI", 600, 2496)
	add(DPL, "Honeywell PM43 203 DPI", 203, 864)
	add(DPL, "Honeywell PM43 300 DPI", 300, 1248)
	add(DPL, "Generic DPL 203 DPI", 203, 832)
	add(DPL, "Generic DPL 300 DPI", 300, 1248)
	add(DPL, "Generic DPL 600 DPI", 600, 2496)

	// SBPL: the widest graphic each head takes, in SATO's programming
	// reference: 104, 156 and 312 bytes on the CL4NX, 152 and 248 on the
	// CL6NX, with the head at its standard position. The WS4 are 4-inch.
	add(SBPL, "SATO CL4NX 203 DPI", 203, 832)
	add(SBPL, "SATO CL4NX 305 DPI", 300, 1248)
	add(SBPL, "SATO CL4NX 609 DPI", 600, 2496)
	add(SBPL, "SATO CL6NX 203 DPI", 203, 1216)
	add(SBPL, "SATO CL6NX 305 DPI", 300, 1984)
	add(SBPL, "SATO WS408", 203, 832)
	add(SBPL, "SATO WS412", 300, 1248)
	add(SBPL, "Generic SBPL 203 DPI", 203, 832)
	add(SBPL, "Generic SBPL 305 DPI", 300, 1248)

	// EZPL. Godex's maximum print widths: 4.25 in (108 mm) at 203 DPI and
	// 4.16 in (105.7 mm) at 300 on the G500 series and RT200 series; 4.09 in
	// at 203 and 4.16 in at 300 on the EZ-1000Plus series.
	add(EZPL, "Godex G500", 203, 864)
	add(EZPL, "Godex G530", 300, 1248)
	add(EZPL, "Godex RT200", 203, 864)
	add(EZPL, "Godex RT230", 300, 1248)
	add(EZPL, "Godex EZ-1100Plus", 203, 832)
	add(EZPL, "Godex EZ-1200Plus", 203, 832)
	add(EZPL, "Godex EZ-1300Plus", 300, 1248)
	add(EZPL, "Generic EZPL 203 DPI", 203, 832)
	add(EZPL, "Generic EZPL 300 DPI", 300, 1248)
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

// MaxDPLRowBytes is the most one row of a DPL image holds: its record
// gives the count in two hexadecimal digits.
const MaxDPLRowBytes = 0xFF

// ReachDots is how wide a label the model prints: its head, or for DPL, as
// much of it as one image row holds (2,040 dots, which only a 600 DPI head
// exceeds).
func (mo Model) ReachDots() int {
	if mo.Language == DPL {
		return min(mo.HeadDots, MaxDPLRowBytes*8)
	}
	return mo.HeadDots
}

// HeadMM is the width of the model's head in millimeters.
func (mo Model) HeadMM() float64 { return float64(mo.HeadDots) / float64(mo.DPI) * 25.4 }

// ReachMM is ReachDots in millimeters.
func (mo Model) ReachMM() float64 { return float64(mo.ReachDots()) / float64(mo.DPI) * 25.4 }

// MaxLabelMM is the widest roll the model takes: a little wider than its
// head, which prints all it can reach.
func (mo Model) MaxLabelMM() float64 { return mo.HeadMM() + 2 }

// Dots is a length in millimeters as the model's dots.
func (mo Model) Dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// Suits reports whether the roll fits in the model.
func (mo Model) Suits(m zebra.Media) error {
	if m.WidthMM > mo.MaxLabelMM() {
		return fmt.Errorf("%s labels are wider than the %s head (%.0f mm)", m.Name, mo.Name, mo.HeadMM())
	}
	return nil
}

// Placement says how a label image maps onto the roll.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the roll.
	Rotate bool
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, then cut to what the model reaches, around its center.
func Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, min(w, mo.ReachDots()), h))
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

// rows packs a page into rows of widthBytes bytes, most significant bit
// first, with the page offset x dots from the left and a bit set where a
// dot prints: a pixel below 50% gray. All three languages set a bit for a
// dot. Bits past the page are left clear, as is any row past its height.
func rows(page *image.Gray, x, widthBytes, height int) [][]byte {
	out := make([][]byte, height)
	for y := range out {
		row := make([]byte, widthBytes)
		if y < page.Rect.Dy() {
			src := page.Pix[y*page.Stride:]
			for px := 0; px < page.Rect.Dx() && x+px < widthBytes*8; px++ {
				if src[px] < 0x80 {
					row[(x+px)/8] |= 0x80 >> uint((x+px)%8)
				}
			}
		}
		out[y] = row
	}
	return out
}

// checkPages refuses an empty job, or a page wider than the model reaches.
func checkPages(mo Model, pages []*image.Gray) error {
	if len(pages) == 0 {
		return fmt.Errorf("%s: a job needs at least one page", mo.Language)
	}
	for i, page := range pages {
		if page.Rect.Dx() > mo.ReachDots() {
			return fmt.Errorf("%s: page %d is %d dots wide, the %s reaches %d", mo.Language, i, page.Rect.Dx(), mo.Name, mo.ReachDots())
		}
	}
	return nil
}

// runs groups a job's pages: a run of the same page is printed as copies
// of one, in batches of at most most.
func runs(pages []*image.Gray, most int, each func(page *image.Gray, copies int) error) error {
	for i := 0; i < len(pages); {
		copies := 1
		for i+copies < len(pages) && pages[i+copies] == pages[i] {
			copies++
		}
		for left := copies; left > 0; left -= most {
			if err := each(pages[i], min(left, most)); err != nil {
				return err
			}
		}
		i += copies
	}
	return nil
}

// labelSize is the label's width and length in dots: the media's, or for
// continuous media, as long as the page.
func labelSize(mo Model, m zebra.Media, page *image.Gray) (width, length int) {
	width = min(mo.Dots(m.WidthMM), mo.ReachDots())
	if m.DieCut() {
		return width, mo.Dots(m.LengthMM)
	}
	return width, page.Rect.Dy()
}

// blank is a white image, for decoders to draw a label on.
func blank(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	return img
}

// draw sets the dots of rows on img, the rows' top-left corner at (x, y).
func draw(img *image.Gray, x, y int, all [][]byte) {
	for r, row := range all {
		for k, b := range row {
			for bit := range 8 {
				px, py := x+k*8+bit, y+r
				if b&(0x80>>bit) != 0 && px < img.Rect.Dx() && py < img.Rect.Dy() {
					img.Pix[py*img.Stride+px] = 0
				}
			}
		}
	}
}
