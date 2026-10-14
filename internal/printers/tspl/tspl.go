// Package tspl encodes print jobs in TSPL, the label language of TSC
// printers, which many other direct thermal printers also speak: Rollo,
// Munbyn, Beeprt, iDPRT and others. Each label is sent as one bitmap, so
// none of the printer's fonts or barcode commands are used.
//
// It follows TSC's TSPL/TSPL2 Programming Manual. Like package brotherql it
// works on 1-bit images and physical sizes only.
package tspl

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
)

// Model is a TSPL printer.
type Model struct {
	Name string
	// DPI is the head resolution: TSPL printers come in 203 (8 dots per
	// mm) and 300 (12 dots per mm).
	DPI int
	// HeadDots is the width of the print head.
	HeadDots int
}

var models = map[string]Model{
	"Rollo X1038":          {Name: "Rollo X1038", DPI: 203, HeadDots: 832},
	"Rollo X1040":          {Name: "Rollo X1040", DPI: 203, HeadDots: 832},
	"Munbyn ITPP941":       {Name: "Munbyn ITPP941", DPI: 203, HeadDots: 832},
	"Generic TSPL 203 DPI": {Name: "Generic TSPL 203 DPI", DPI: 203, HeadDots: 832},
	"Generic TSPL 300 DPI": {Name: "Generic TSPL 300 DPI", DPI: 300, HeadDots: 1248},
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

// HeadMM is the width of the model's head in millimeters.
func (mo Model) HeadMM() float64 { return float64(mo.HeadDots) / float64(mo.DPI) * 25.4 }

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
	if m.WidthMM > mo.HeadMM()+2 {
		return fmt.Errorf("%s labels are wider than the %s head (%.0f mm)", m.Name, mo.Name, mo.HeadMM())
	}
	return nil
}

// Continuous labels must be between these lengths, in millimeters.
const (
	minContinuousMM = 10
	maxContinuousMM = 1000
)

// lengthTolerance absorbs rounding when matching template sizes to media, in
// millimeters.
const lengthTolerance = 0.5

// ErrDoesNotFit is wrapped by every fit check failure.
var ErrDoesNotFit = errors.New("template does not fit the loaded media")

// Placement says how a label image maps onto the roll.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the roll.
	Rotate bool
}

// Fit checks that a label of the given physical size at dpi suits the model
// and media. One side must equal the label width; on die-cut media the other
// must equal the label length. A label whose height matches the label width
// is rotated.
func (m Media) Fit(mo Model, widthMM, heightMM float64, dpi int) (Placement, error) {
	if dpi != mo.DPI {
		return Placement{}, fmt.Errorf("%w: the %s prints at %d DPI, not %d", ErrDoesNotFit, mo.Name, mo.DPI, dpi)
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
		if along < minContinuousMM || along > maxContinuousMM {
			return Placement{}, fmt.Errorf("%w: continuous labels must be %d-%d mm long (got %.1f mm)",
				ErrDoesNotFit, minContinuousMM, maxContinuousMM, along)
		}
		return p, nil
	}
	if m.DieCut() {
		return Placement{}, fmt.Errorf("%w: %s needs a %.4g × %.4g mm template (got %g × %g mm)",
			ErrDoesNotFit, m.Name, m.WidthMM, m.LengthMM, widthMM, heightMM)
	}
	return Placement{}, fmt.Errorf("%w: %s needs one side of the template to be %.4g mm (got %g × %g mm)",
		ErrDoesNotFit, m.Name, m.WidthMM, widthMM, heightMM)
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, then cut to what the head reaches.
func Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, min(w, mo.HeadDots), h))
	offX := (w - page.Rect.Dx()) / 2
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride+offX:]
		copy(page.Pix[y*page.Stride:y*page.Stride+page.Rect.Dx()], row)
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

// JobOptions are the per-printer choices that change the job.
type JobOptions struct {
	// Density is the darkness, 0-15; nil leaves the printer's setting.
	Density *int
	// Rotate180 prints labels the other way up (DIRECTION 1).
	Rotate180 bool
}

// EncodeJob builds the TSPL program that prints pages, each prepared for the
// model (see Prepare) and dark wherever a pixel is below 50% gray. A run of
// the same page is printed as copies of one image.
func EncodeJob(mo Model, m Media, pages []*image.Gray, opts JobOptions) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("tspl: a job needs at least one page")
	}
	if opts.Density != nil && (*opts.Density < 0 || *opts.Density > 15) {
		return nil, fmt.Errorf("tspl: density %d is outside 0-15", *opts.Density)
	}
	for i, page := range pages {
		if page.Rect.Dx() > mo.HeadDots {
			return nil, fmt.Errorf("tspl: page %d is %d dots wide, the %s head has %d", i, page.Rect.Dx(), mo.Name, mo.HeadDots)
		}
	}

	var buf bytes.Buffer
	line := func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
		buf.WriteString("\r\n")
	}
	mm := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " mm" }

	// Continuous labels are as long as the first page.
	length := m.LengthMM
	if !m.DieCut() {
		length = float64(pages[0].Rect.Dy()) / float64(mo.DPI) * 25.4
		length = math.Round(length*100) / 100
	}
	line("SIZE %s,%s", mm(m.WidthMM), mm(length))
	line("GAP %s,0 mm", mm(m.GapMM))
	direction := 0
	if opts.Rotate180 {
		direction = 1
	}
	line("DIRECTION %d,0", direction)
	line("REFERENCE 0,0")
	if opts.Density != nil {
		line("DENSITY %d", *opts.Density)
	}

	for i := 0; i < len(pages); {
		page := pages[i]
		copies := 1
		for i+copies < len(pages) && pages[i+copies] == page {
			copies++
		}
		i += copies

		w, h := page.Rect.Dx(), page.Rect.Dy()
		widthBytes := (w + 7) / 8
		// Center the page across the label.
		x := max(0, (int(math.Round(m.WidthMM/25.4*float64(mo.DPI)))-w)/2)
		line("CLS")
		fmt.Fprintf(&buf, "BITMAP %d,0,%d,%d,0,", x, widthBytes, h)
		row := make([]byte, widthBytes)
		for y := 0; y < h; y++ {
			// A set bit leaves the dot white.
			for k := range row {
				row[k] = 0xFF
			}
			src := page.Pix[y*page.Stride:]
			for px := 0; px < w; px++ {
				if src[px] < 0x80 {
					row[px/8] &^= 0x80 >> uint(px%8)
				}
			}
			buf.Write(row)
		}
		buf.WriteString("\r\n")
		line("PRINT 1,%d", copies)
	}
	return buf.Bytes(), nil
}
