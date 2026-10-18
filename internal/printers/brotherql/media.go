// Package brotherql encodes print jobs for Brother QL-800 series label
// printers (QL-810W, QL-820NWB) in their raster command language, for
// sending to the printer's raw TCP port.
//
// It works on 1-bit images and physical sizes only; it knows nothing about
// templates or assets.
package brotherql

import (
	"image"
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
	MinContinuousLines = 295
	MaxContinuousLines = 11811
)

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

// catalog is every roll the QL-800 series takes, from Brother's Software
// Developer's Manual, Raster Command Reference QL-800/810W/820NWB, version
// 1.01: section 2.3.2 (page size) for the printable lines, and section
// 2.3.5's tables of pins for the printable dots and the pins beside the
// tape. Sizes are Brother's nominal ones, which the printer is also sent.
// Part numbers are given where Brother sells the roll under one.
var catalog = map[string]Media{}

func init() {
	continuous := func(id, name string, width float64, printable, right int) Media {
		return Media{ID: id, Name: name, WidthMM: width, PrintableDots: printable, RightMarginDots: right, FeedMarginDots: 35}
	}
	dieCut := func(id, name string, width, length float64, printable, right, lines int) Media {
		return Media{ID: id, Name: name, WidthMM: width, LengthMM: length, PrintableDots: printable, RightMarginDots: right, PrintableLines: lines}
	}
	for _, m := range []Media{
		continuous("dk-22214", "DK-22214 12 mm continuous", 12, 106, 29),
		continuous("dk-22210", "DK-22210 29 mm continuous", 29, 306, 6),
		continuous("dk-22225", "DK-22225 38 mm continuous", 38, 413, 12),
		continuous("dk-22223", "DK-22223 50 mm continuous", 50, 554, 12),
		continuous("dk-n55224", "DK-N55224 54 mm continuous", 54, 590, 0),
		continuous("dk-22205", "DK-22205 62 mm continuous", 62, 696, 12),

		dieCut("dk-11204", "DK-11204 17 × 54 mm die-cut", 17, 54, 165, 0, 566),
		dieCut("dk-11203", "DK-11203 17 × 87 mm die-cut", 17, 87, 165, 0, 956),
		dieCut("dk-11221", "DK-11221 23 × 23 mm die-cut", 23, 23, 236, 42, 202),
		dieCut("brother-29x42", "29 × 42 mm die-cut", 29, 42, 306, 6, 425),
		dieCut("dk-11201", "DK-11201 29 × 90 mm die-cut", 29, 90, 306, 6, 991),
		dieCut("dk-11208", "DK-11208 38 × 90 mm die-cut", 38, 90, 413, 12, 991),
		dieCut("brother-39x48", "39 × 48 mm die-cut", 39, 48, 425, 6, 495),
		dieCut("brother-52x29", "52 × 29 mm die-cut", 52, 29, 578, 0, 271),
		dieCut("brother-54x29", "54 × 29 mm die-cut", 54, 29, 602, 59, 271),
		dieCut("brother-60x86", "60 × 86 mm die-cut", 60, 86, 672, 24, 954),
		dieCut("brother-62x29", "62 × 29 mm die-cut", 62, 29, 696, 12, 271),
		dieCut("brother-62x60", "62 × 60 mm die-cut", 62, 60, 696, 12, 645),
		dieCut("brother-62x75", "62 × 75 mm die-cut", 62, 75, 696, 12, 820),
		dieCut("dk-11202", "DK-11202 62 × 100 mm die-cut", 62, 100, 696, 12, 1109),
		dieCut("dk-11219", "DK-11219 12 mm round", 12, 12, 94, 113, 94),
		dieCut("dk-11218", "DK-11218 24 mm round", 24, 24, 236, 42, 236),
		dieCut("dk-11207", "DK-11207 58 mm round", 58, 58, 618, 51, 618),
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

// Models lists the printers the raster encoding is written for.
func Models() []string {
	return []string{"QL-810W", "QL-820NWB"}
}

// Placement says how a label image maps onto the tape.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the tape.
	Rotate bool
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
