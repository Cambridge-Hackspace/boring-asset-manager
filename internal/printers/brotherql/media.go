// Package brotherql encodes print jobs for Brother QL-800 series label
// printers (QL-810W, QL-820NWB) in their raster command language, for
// sending to the printer's raw TCP port.
//
// It works on 1-bit images and physical sizes only; it knows nothing about
// templates or assets.
package brotherql

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
)

// DPI is the print head resolution.
const DPI = 300

// Head geometry: 720 pins, sent as 90 bytes per raster line.
const (
	headPins     = 720
	bytesPerLine = headPins / 8
)

// Continuous labels must be between these lengths, in raster lines.
const (
	minContinuousLines = 295
	maxContinuousLines = 11811
)

// lengthTolerance absorbs rounding when matching template sizes to media, in
// millimeters.
const lengthTolerance = 0.5

// Media is a Brother DK roll.
type Media struct {
	ID   string
	Name string
	// WidthMM is the tape width.
	WidthMM float64
	// LengthMM is the label length for die-cut media, 0 for continuous.
	LengthMM float64
	// PrintableDots is how many head pins the printer fires across the tape.
	PrintableDots int
	// RightMarginDots is how many pins sit beside the tape on the right.
	RightMarginDots int
	// PrintableLines is the printable length of a die-cut label, in raster lines.
	PrintableLines int
	// FeedMarginDots is the feed added before and after continuous labels.
	FeedMarginDots int
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

var catalog = map[string]Media{
	"dk-22210": {ID: "dk-22210", Name: "DK-22210 29 mm continuous", WidthMM: 29, PrintableDots: 306, RightMarginDots: 6, FeedMarginDots: 35},
	"dk-22205": {ID: "dk-22205", Name: "DK-22205 62 mm continuous", WidthMM: 62, PrintableDots: 696, RightMarginDots: 12, FeedMarginDots: 35},
	"dk-11201": {ID: "dk-11201", Name: "DK-11201 29 × 90 mm die-cut", WidthMM: 29, LengthMM: 90, PrintableDots: 306, RightMarginDots: 6, PrintableLines: 991},
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

// Models lists the printers the raster encoding is written for.
func Models() []string {
	return []string{"QL-810W", "QL-820NWB"}
}

// ErrDoesNotFit is wrapped by every fit check failure.
var ErrDoesNotFit = errors.New("template does not fit the loaded media")

// Placement says how a label image maps onto the tape.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the tape.
	Rotate bool
}

// Fit checks that a label of the given physical size at dpi suits the media.
// One side must equal the tape width; on die-cut media the other must equal
// the label length. A label whose height matches the tape width is rotated.
func (m Media) Fit(widthMM, heightMM float64, dpi int) (Placement, error) {
	if dpi != DPI {
		return Placement{}, fmt.Errorf("%w: Brother QL printers print at %d DPI, not %d", ErrDoesNotFit, DPI, dpi)
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
			return Placement{}, fmt.Errorf("%w: continuous labels must be %.1f-%.0f mm long (got %.1f mm)",
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

// Prepare turns a rendered label into a page for the printer: rotated if
// the placement says so, then cropped to the printable area around its
// center. The result is PrintableDots wide with one row per raster line.
func (m Media) Prepare(label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	lines := h
	if m.DieCut() {
		lines = m.PrintableLines
	}

	page := image.NewGray(image.Rect(0, 0, m.PrintableDots, lines))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	offX := (w - m.PrintableDots) / 2
	offY := (h - lines) / 2
	for y := 0; y < lines; y++ {
		sy := y + offY
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < m.PrintableDots; x++ {
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
