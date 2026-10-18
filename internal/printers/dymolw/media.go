// Package dymolw encodes print jobs for DYMO LabelWriter printers in their
// raster command languages: the original one spoken by the 400 and 450
// series and the 4XL, and the second generation spoken by the 550 series,
// the 5XL and the LabelWriter Wireless.
//
// The first language is documented in DYMO's LabelWriter 450 Series
// Technical Reference Manual, which the version 1 encoder follows. DYMO has
// published no reference for the second; its command format was learned by
// reading DYMO's open-source Linux drivers, and no code is taken from them.
// Like package brotherql it works on 1-bit images and physical sizes only.
package dymolw

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
)

// DPI is the print head resolution of every supported model.
const DPI = 300

// Protocol is a generation of the LabelWriter command language.
type Protocol int

const (
	// ProtocolV1 is the language of the 400 and 450 series and the 4XL.
	ProtocolV1 Protocol = iota + 1
	// ProtocolV2 is the language of the 550 series, the 5XL and the
	// LabelWriter Wireless.
	ProtocolV2
)

// Model is a LabelWriter printer.
type Model struct {
	Name     string
	Protocol Protocol
	// HeadDots is the width of the print head.
	HeadDots int
	// TwinRoll models hold two rolls and take a roll selection.
	TwinRoll bool
}

// Head widths: 672 dots (56 mm) for standard models, 1248 dots (104 mm) for
// the extra-wide ones.
const (
	standardHead = 672
	wideHead     = 1248
)

var models = map[string]Model{
	"LabelWriter 400":            {Name: "LabelWriter 400", Protocol: ProtocolV1, HeadDots: standardHead},
	"LabelWriter 400 Turbo":      {Name: "LabelWriter 400 Turbo", Protocol: ProtocolV1, HeadDots: standardHead},
	"LabelWriter Twin Turbo":     {Name: "LabelWriter Twin Turbo", Protocol: ProtocolV1, HeadDots: standardHead, TwinRoll: true},
	"LabelWriter 450":            {Name: "LabelWriter 450", Protocol: ProtocolV1, HeadDots: standardHead},
	"LabelWriter 450 Turbo":      {Name: "LabelWriter 450 Turbo", Protocol: ProtocolV1, HeadDots: standardHead},
	"LabelWriter 450 Twin Turbo": {Name: "LabelWriter 450 Twin Turbo", Protocol: ProtocolV1, HeadDots: standardHead, TwinRoll: true},
	"LabelWriter 450 Duo":        {Name: "LabelWriter 450 Duo", Protocol: ProtocolV1, HeadDots: standardHead},
	"LabelWriter 4XL":            {Name: "LabelWriter 4XL", Protocol: ProtocolV1, HeadDots: wideHead},
	"LabelWriter 550":            {Name: "LabelWriter 550", Protocol: ProtocolV2, HeadDots: standardHead},
	"LabelWriter 550 Turbo":      {Name: "LabelWriter 550 Turbo", Protocol: ProtocolV2, HeadDots: standardHead},
	"LabelWriter 5XL":            {Name: "LabelWriter 5XL", Protocol: ProtocolV2, HeadDots: wideHead},
	"LabelWriter Wireless":       {Name: "LabelWriter Wireless", Protocol: ProtocolV2, HeadDots: standardHead},
}

// LookupModel returns a model by name.
func LookupModel(name string) (Model, bool) {
	m, ok := models[name]
	return m, ok
}

// Models lists the supported model names.
func Models() []string {
	names := make([]string, 0, len(models))
	for n := range models {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Continuous labels must be between these lengths, in raster lines.
const (
	minContinuousLines = 118   // 10 mm
	maxContinuousLines = 32000 // 2.7 m
)

// lengthTolerance absorbs rounding when matching template sizes to media, in
// millimeters.
const lengthTolerance = 0.5

// Media is a LabelWriter roll. Labels sit against the guide at the first dot
// of the head, so a label narrower than the head prints from dot 0.
type Media struct {
	ID   string
	Name string
	// WidthMM and LengthMM are the nominal label size; LengthMM is 0 for
	// continuous media.
	WidthMM, LengthMM float64
	// WidthDots and LengthDots are the label size in dots and raster lines.
	WidthDots, LengthDots int
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

// CustomMedia is a roll known only by its label size, for custom stock.
func CustomMedia(id, name string, widthMM, lengthMM float64) Media {
	return media(id, name, widthMM, lengthMM)
}

// media builds a roll from its nominal label size.
func media(id, name string, widthMM, lengthMM float64) Media {
	return Media{ID: id, Name: name, WidthMM: widthMM, LengthMM: lengthMM,
		WidthDots: mmToDots(widthMM), LengthDots: mmToDots(lengthMM)}
}

func mmToDots(mm float64) int { return int(math.Round(mm / 25.4 * DPI)) }

var catalog = map[string]Media{}

func init() {
	for _, m := range []Media{
		media("30252", "30252 / 99010 address, 28 × 89 mm", 28, 89),
		media("30321", "30321 / 99012 large address, 36 × 89 mm", 36, 89),
		media("30256", "30256 shipping, 59 × 102 mm", 59, 102),
		media("30323", "30323 / 99014 shipping, 54 × 101 mm", 54, 101),
		media("30334", "30334 / 11354 multipurpose, 57 × 32 mm", 57, 32),
		media("30336", "30336 / 11352 multipurpose, 25 × 54 mm", 25, 54),
		media("30332", "30332 / 11353 square, 25 × 25 mm", 25, 25),
		media("30347", "30347 multipurpose, 25 × 38 mm", 25, 38),
		media("30330", "30330 / 11355 return address, 19 × 51 mm", 19, 51),
		media("30345", "30345 multipurpose, 19 × 64 mm", 19, 64),
		media("1744907", "1744907 extra-large shipping, 104 × 159 mm", 104, 159),
		media("30270", "30270 continuous, 57 mm", 57, 0),
	} {
		catalog[m.ID] = m
	}
}

// LookupMedia returns a roll by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the supported rolls.
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
	// Rolls a little wider than the head (59 mm shipping labels on a 56 mm
	// head) are normal: the head prints all it can reach. Rolls meant for
	// the extra-wide models are not.
	if m.WidthMM > 70 && mo.HeadDots < wideHead {
		return fmt.Errorf("%s labels need an extra-wide model such as the 4XL or 5XL", m.Name)
	}
	if m.WidthMM > mo.MaxLabelMM() {
		return fmt.Errorf("%s labels are wider than the %s takes (%.0f mm)", m.Name, mo.Name, mo.MaxLabelMM())
	}
	return nil
}

// MaxLabelMM is the widest roll the model takes: a few millimeters wider
// than its head, as DYMO's own 59 mm labels on a 56 mm head are.
func (mo Model) MaxLabelMM() float64 {
	if mo.HeadDots < wideHead {
		return 70
	}
	return float64(wideHead)/DPI*25.4 + 4
}

// Continuous labels must be between these lengths, in millimeters.
var (
	MinContinuousMM = float64(minContinuousLines) / DPI * 25.4
	MaxContinuousMM = float64(maxContinuousLines) / DPI * 25.4
)

// ErrDoesNotFit is wrapped by every fit check failure.
var ErrDoesNotFit = errors.New("template does not fit the loaded media")

// Placement says how a label image maps onto the roll.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the roll.
	Rotate bool
}

// Fit checks that a label of the given physical size at dpi suits the media.
// One side must equal the label width; on die-cut media the other must equal
// the label length. A label whose height matches the label width is rotated.
func (m Media) Fit(widthMM, heightMM float64, dpi int) (Placement, error) {
	if dpi != DPI {
		return Placement{}, fmt.Errorf("%w: LabelWriter printers print at %d DPI, not %d", ErrDoesNotFit, DPI, dpi)
	}
	matches := func(a, b float64) bool { return math.Abs(a-b) <= lengthTolerance }

	for _, p := range []Placement{{Rotate: false}, {Rotate: true}} {
		across, along := widthMM, heightMM
		if p.Rotate {
			across, along = heightMM, widthMM
		}
		if !matches(across, m.WidthMM) {
			continue
		}
		if m.DieCut() {
			if matches(along, m.LengthMM) {
				return p, nil
			}
			continue
		}
		lines := int(math.Round(along / 25.4 * DPI))
		if lines < minContinuousLines || lines > maxContinuousLines {
			return Placement{}, fmt.Errorf("%w: continuous labels must be %.0f-%.0f mm long (got %.1f mm)",
				ErrDoesNotFit, float64(minContinuousLines)/DPI*25.4, float64(maxContinuousLines)/DPI*25.4, along)
		}
		return p, nil
	}
	if m.DieCut() {
		return Placement{}, fmt.Errorf("%w: %s needs a %g × %g mm template (got %g × %g mm)",
			ErrDoesNotFit, m.Name, m.WidthMM, m.LengthMM, widthMM, heightMM)
	}
	return Placement{}, fmt.Errorf("%w: %s needs one side of the template to be %g mm (got %g × %g mm)",
		ErrDoesNotFit, m.Name, m.WidthMM, widthMM, heightMM)
}

// Prepare turns a rendered label into a page for the model: rotated if the
// placement says so, then centered on the roll's page size and cut to
// PageDots. The result has one row per raster line.
func (m Media) Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	lines := h
	if m.DieCut() {
		lines = m.LengthDots
	}

	page := image.NewGray(image.Rect(0, 0, m.PageDots(mo), lines))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	offX := (w - m.WidthDots) / 2
	offY := (h - lines) / 2
	for y := 0; y < lines; y++ {
		sy := y + offY
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < page.Rect.Dx(); x++ {
			sx := x + offX
			if sx < 0 || sx >= w {
				continue
			}
			page.Pix[y*page.Stride+x] = src.Pix[(sy-src.Rect.Min.Y)*src.Stride+(sx-src.Rect.Min.X)]
		}
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
