// Package brotherql encodes print jobs for Brother label printers that speak
// Brother's raster command language: the QL range and the TD-2 and TD-4
// series. Jobs are sent to the printer's raw TCP port, or any other
// connection that passes bytes through.
//
// It works on 1-bit images and physical sizes only; it knows nothing about
// templates or assets.
package brotherql

import (
	"image"
	"sort"
)

// Media is a roll of labels: a Brother DK roll, whose placement on each QL
// head Brother tabulates, or a roll known by its size alone, which a TD
// printer takes and whose placement is worked out from its size.
type Media struct {
	ID   string
	Name string
	// WidthMM is the tape width.
	WidthMM float64
	// LengthMM is the label length for die-cut media, 0 for continuous.
	LengthMM float64
	// PrintableDots is how many head pins the printer fires across a DK
	// roll, and PrintableLines the printable length of a die-cut DK label,
	// in raster lines. Both are 0 on rolls known by size, whose figures
	// depend on the model (see Model.Layout).
	PrintableDots  int
	PrintableLines int

	// right is how many pins sit beside a DK roll on the right, on each
	// head Brother tabulates; none where the head's table leaves it out.
	right [numPinTables]int
	// lines are the model lines whose references list the roll.
	lines lineSet
	// infoWidth is the width sent in the print information command when it
	// is not the nominal width: Brother's 103 mm rolls are sent as 104.
	infoWidth int
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

// sized reports whether the roll is known by its size alone.
func (m Media) sized() bool { return m.PrintableDots == 0 }

// none marks a roll missing from a head's pin table.
const none = -1

// pinTable names one of Brother's tables of where each DK roll sits on a
// QL head: the pins beside the tape on the right, which with the printable
// pins and the pins on the left make up the head.
type pinTable int

const (
	// pins720 is the 720-pin head of every QL model up to 62 mm. The
	// QL-500 to QL-1060N reference (section 3.2.5), the QL-600/710W/720NW
	// reference (section 2.3.5) and the QL-800 series reference (section
	// 2.3.5) give the same figures for the rolls they share.
	pins720 pinTable = iota
	// pins1296QL1050 is the 1,296-pin head of the QL-1050 and QL-1060N,
	// from the QL-500 to QL-1060N reference, section 3.2.5.
	pins1296QL1050
	// pins1296QL1100 is the 1,296-pin head of the QL-1100 series, from
	// the QL-1100/1110NWB/1115NWB reference, section 2.3.5.
	pins1296QL1100
	numPinTables
)

// lineSet is a set of model lines, each the models one of Brother's raster
// command references covers with one list of media.
type lineSet uint16

const (
	// lineQL500 is the QL-500, 550, 560, 570, 580N, 650TD and 700.
	lineQL500 lineSet = 1 << iota
	// lineQL1050 is the QL-1050 and QL-1060N.
	lineQL1050
	// lineQL710 is the QL-710W and QL-720NW.
	lineQL710
	// lineQL800 is the QL-800, 810W and 820NWB.
	lineQL800
	// lineQL1100 is the QL-1100 and QL-1110NWB.
	lineQL1100
	// lineQL1115 is the QL-1115NWB, which takes the QL-1100's rolls but
	// for the 103 mm ones.
	lineQL1115
	// lineTD2 and lineTD4 are the TD-2 and TD-4 series, for Brother's RD
	// rolls named in their references.
	lineTD2
	lineTD4

	everyQL = lineQL500 | lineQL1050 | lineQL710 | lineQL800 | lineQL1100 | lineQL1115
)

// dkCatalog is every DK roll the QL range takes. Its figures come from
// Brother's raster command references, linked from
// https://support.brother.com/g/s/es/dev/en/command/reference/index.html:
//
//   - Brother QL Series Command Reference (QL-500/550/560/570/580N/650TD/
//     700/1050/1060N), version 6.0, cv_qlseries_eng_raster_600.pdf:
//     sections 3.2.2 (page size) and 3.2.5 (raster line arrangement);
//   - Software Developer's Manual, Raster Command Reference
//     QL-600/QL-710W/QL-720NW, version 1.02,
//     cv_ql600710720_eng_raster_102.pdf: sections 2.3.2 and 2.3.5;
//   - Software Developer's Manual, Raster Command Reference
//     QL-800/810W/820NWB, version 1.01, cv_ql800_eng_raster_101.pdf:
//     sections 2.3.2 and 2.3.5;
//   - Software Developer's Manual, Raster Command Reference
//     QL-1100/1110NWB/1115NWB, version 1.00, cv_ql1100_eng_raster_100.pdf:
//     sections 2.3.2 and 2.3.5, and the media width table of section 4.
//
// Page size gives the printable lines of die-cut labels; the tables of pins
// give the printable dots and the pins beside the tape. Every reference that
// lists a roll gives it the same printable dots and lines. Sizes are
// Brother's nominal ones, which the printer is also sent. Part numbers are
// given where Brother sells the roll under one; the references name none,
// so those are from Brother's supplies.
//
// Two of the QL-1100 reference's figures do not add up to its 1,296 pins:
// it gives 62 mm continuous tape 544 + 696 + 44 pins, and the 62 mm die-cut
// labels 554 + 696 + 56. The QL-1050's table, and the 62 mm die-cut rows'
// own right margin, give 544 + 696 + 56, which is used for all three.
var dkCatalog = map[string]Media{}

// tdCatalog is the RD rolls the TD references size, which a TD printer
// takes like any custom stock of their size: Software Developer's Manual,
// Raster Command Reference TD-2020/2120N/2130N (and the later TD-2 models),
// version 1.01, cv_td2000_eng_raster_101.pdf, section 2.3.2, and Raster
// Command Reference TD-4410D/4420DN/4510D/4520DN/4550DNWB (and the later
// TD-4 models), version 1.03, cv_td4000d_eng_raster_103.pdf, section 4.3.2.
// Brother gives them no part numbers there.
var tdCatalog = map[string]Media{}

func init() {
	type r = [numPinTables]int
	continuous := func(id, name string, width float64, printable int, right r, lines lineSet) Media {
		return Media{ID: id, Name: name, WidthMM: width, PrintableDots: printable, right: right, lines: lines}
	}
	dieCut := func(id, name string, width, length float64, printable, printLines int, right r, lines lineSet) Media {
		return Media{ID: id, Name: name, WidthMM: width, LengthMM: length, PrintableDots: printable, PrintableLines: printLines, right: right, lines: lines}
	}
	newer := lineQL710 | lineQL800 | lineQL1100 | lineQL1115
	wide := lineQL1050 | lineQL1100 | lineQL1115
	dk103 := func(m Media) Media { m.infoWidth = 104; return m }
	for _, m := range []Media{
		continuous("dk-22214", "DK-22214 12 mm continuous", 12, 106, r{29, 74, 74}, everyQL),
		continuous("dk-22210", "DK-22210 29 mm continuous", 29, 306, r{6, 50, 50}, everyQL),
		continuous("dk-22225", "DK-22225 38 mm continuous", 38, 413, r{12, 56, 56}, everyQL),
		continuous("dk-22223", "DK-22223 50 mm continuous", 50, 554, r{12, 56, 56}, everyQL),
		continuous("dk-n55224", "DK-N55224 54 mm continuous", 54, 590, r{0, 44, 44}, everyQL),
		continuous("dk-22205", "DK-22205 62 mm continuous", 62, 696, r{12, 56, 56}, everyQL),
		continuous("dk-22243", "DK-22243 102 mm continuous", 102, 1164, r{none, 56, 56}, wide),
		dk103(continuous("dk-22246", "DK-22246 103 mm continuous", 103, 1200, r{none, none, 38}, lineQL1100)),

		dieCut("dk-11204", "DK-11204 17 × 54 mm die-cut", 17, 54, 165, 566, r{0, 44, 44}, everyQL),
		dieCut("dk-11203", "DK-11203 17 × 87 mm die-cut", 17, 87, 165, 956, r{0, 44, 44}, everyQL),
		dieCut("dk-11221", "DK-11221 23 × 23 mm die-cut", 23, 23, 236, 202, r{42, 84, 85}, everyQL),
		dieCut("brother-29x42", "29 × 42 mm die-cut", 29, 42, 306, 425, r{6, none, 50}, newer),
		dieCut("dk-11201", "DK-11201 29 × 90 mm die-cut", 29, 90, 306, 991, r{6, 50, 50}, everyQL),
		dieCut("dk-11208", "DK-11208 38 × 90 mm die-cut", 38, 90, 413, 991, r{12, 56, 56}, everyQL),
		dieCut("brother-39x48", "39 × 48 mm die-cut", 39, 48, 425, 495, r{6, 50, 50}, everyQL),
		dieCut("brother-52x29", "52 × 29 mm die-cut", 52, 29, 578, 271, r{0, 44, 44}, everyQL),
		dieCut("brother-54x29", "54 × 29 mm die-cut", 54, 29, 602, 271, r{59, none, none}, lineQL800),
		dieCut("brother-60x86", "60 × 86 mm die-cut", 60, 86, 672, 954, r{24, none, 68}, newer),
		dieCut("brother-62x29", "62 × 29 mm die-cut", 62, 29, 696, 271, r{12, 56, 56}, everyQL),
		dieCut("brother-62x60", "62 × 60 mm die-cut", 62, 60, 696, 645, r{12, none, none}, lineQL800),
		dieCut("brother-62x75", "62 × 75 mm die-cut", 62, 75, 696, 820, r{12, none, none}, lineQL800),
		dieCut("dk-11202", "DK-11202 62 × 100 mm die-cut", 62, 100, 696, 1109, r{12, 56, 56}, everyQL),
		dieCut("dk-11240", "DK-11240 102 × 51 mm die-cut", 102, 51, 1164, 526, r{none, 56, 56}, wide),
		dieCut("dk-11241", "DK-11241 102 × 152 mm die-cut", 102, 152, 1164, 1660, r{none, 56, 56}, wide),
		dk103(dieCut("dk-11247", "DK-11247 103 × 164 mm die-cut", 103, 164, 1200, 1822, r{none, none, 38}, lineQL1100)),
		dieCut("dk-11219", "DK-11219 12 mm round", 12, 12, 94, 94, r{113, 156, 156}, everyQL),
		dieCut("dk-11218", "DK-11218 24 mm round", 24, 24, 236, 236, r{42, 85, 85}, everyQL),
		dieCut("dk-11207", "DK-11207 58 mm round", 58, 58, 618, 618, r{51, 94, 94}, everyQL),
	} {
		dkCatalog[m.ID] = m
	}

	rd := func(id, size string, width, length float64, lines lineSet) Media {
		name := "RD " + size + " mm continuous"
		if length > 0 {
			name = "RD " + size + " mm die-cut"
		}
		m := SizedMedia(id, name, width, length)
		m.lines = lines
		return m
	}
	// Named by Brother's nominal size, sized by its exact one.
	both := lineTD2 | lineTD4
	for _, m := range []Media{
		rd("rd-57", "57", 57.2, 0, lineTD2),
		rd("rd-58", "58", 58, 0, both),
		rd("rd-76", "76", 76, 0, lineTD4),
		rd("rd-90", "90", 90, 0, lineTD4),
		rd("rd-102", "102", 101.6, 0, lineTD4),
		rd("rd-30x30", "30 × 30", 30, 30, lineTD2),
		rd("rd-40x40", "40 × 40", 40, 40, lineTD2),
		rd("rd-40x50", "40 × 50", 40, 50, lineTD2),
		rd("rd-40x60", "40 × 60", 40, 60, lineTD2),
		rd("rd-50x30", "50 × 30", 50, 30, lineTD2),
		rd("rd-51x26", "51 × 26", 50.8, 25.6, both),
		rd("rd-60x60", "60 × 60", 60, 60, lineTD2),
		rd("rd-76x26", "76 × 26", 76.2, 25.6, lineTD4),
		rd("rd-102x50", "102 × 50", 101.6, 49.9, lineTD4),
		rd("rd-102x152", "102 × 152", 101.6, 152.4, lineTD4),
	} {
		tdCatalog[m.ID] = m
	}
}

// SizedMedia is a roll known by its size alone, such as custom stock, for
// the TD series.
func SizedMedia(id, name string, widthMM, lengthMM float64) Media {
	return Media{ID: id, Name: name, WidthMM: widthMM, LengthMM: lengthMM, lines: lineTD2 | lineTD4}
}

func catalogFor(s Series) map[string]Media {
	if s == SeriesTD {
		return tdCatalog
	}
	return dkCatalog
}

// LookupMedia returns a DK roll by ID.
func LookupMedia(id string) (Media, bool) { return LookupSeriesMedia(SeriesQL, id) }

// LookupSeriesMedia returns a built-in roll of a series by ID: a DK roll
// for the QL range, an RD roll for the TD series.
func LookupSeriesMedia(s Series, id string) (Media, bool) {
	m, ok := catalogFor(s)[id]
	return m, ok
}

// MediaIDs lists the DK rolls.
func MediaIDs() []string { return SeriesMediaIDs(SeriesQL) }

// SeriesMediaIDs lists a series' built-in rolls.
func SeriesMediaIDs(s Series) []string {
	c := catalogFor(s)
	ids := make([]string, 0, len(c))
	for id := range c {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Placement says how a label image maps onto the tape.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the tape.
	Rotate bool
}

// Prepare turns a rendered label into a page for a DK roll: rotated if the
// placement says so, then cropped to the printable area around its center.
// The result is PrintableDots wide with one row per raster line. A roll
// known by size has no figures of its own; prepare its pages with
// Layout.Prepare.
func (m Media) Prepare(label *image.Gray, p Placement) *image.Gray {
	return prepare(label, p, m.PrintableDots, m.PrintableLines)
}

// PreparePage rotates label if p says so, then crops it to dots across, and
// to lines along when lines is set, around its center: how every Brother
// raster language takes a page.
func PreparePage(label *image.Gray, p Placement, dots, lines int) *image.Gray {
	return prepare(label, p, dots, lines)
}

// prepare rotates label if p says so, then crops it to dots across, and to
// lines along when lines is set, around its center.
func prepare(label *image.Gray, p Placement, dots, lines int) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if lines == 0 {
		lines = h
	}

	page := image.NewGray(image.Rect(0, 0, dots, lines))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	offX := (w - dots) / 2
	offY := (h - lines) / 2
	for y := 0; y < lines; y++ {
		sy := y + offY
		if sy < 0 || sy >= h {
			continue
		}
		for x := 0; x < dots; x++ {
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
