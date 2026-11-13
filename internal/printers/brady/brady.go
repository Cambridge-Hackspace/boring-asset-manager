// Package brady encodes print jobs for Brady printers that take JSON jobs
// over TCP port 9100, starting with the M611. Each page is one 1-bit BMP,
// compressed in LZ4's block format, so none of the printer's own label
// files are used.
//
// Brady publishes nothing about the format. It is reconstructed, as facts
// only, from pybrady's specification (gitlab.com/ggiesen/pybrady,
// docs/brady_specification.md §4, MPL-2.0), which its author recovered from
// Brady's Express Labels app. No code or data is taken from either. Like
// package tspl it works on 1-bit images and physical sizes only.
package brady

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Model is a Brady printer that takes JSON jobs.
type Model struct {
	Name string
	DPI  int
	// MaxWidthMM is the widest label it takes, and prints all of.
	MaxWidthMM float64
	// MinLengthMM and MaxLengthMM bound a label's length.
	MinLengthMM, MaxLengthMM float64
}

var models = map[string]Model{
	// Brady's technical specifications: 300 DPI, labels up to 2 in wide and
	// 40 in long; pybrady's §7.1: at least 0.375 in long.
	"M611": {Name: "M611", DPI: 300, MaxWidthMM: 50.8, MinLengthMM: 9.525, MaxLengthMM: 1016},
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

// HeadDots is how many dots the model prints across its widest label.
func (mo Model) HeadDots() int { return mo.Dots(mo.MaxWidthMM) }

// Dots converts millimeters to the model's dots, rounding down.
func (mo Model) Dots(mm float64) int { return int(math.Floor(mm/25.4*float64(mo.DPI) + 1e-9)) }

// Media is the cartridge loaded: its labels' size.
type Media struct {
	ID   string
	Name string
	// WidthMM and LengthMM are the label size; LengthMM is 0 for a
	// continuous cartridge.
	WidthMM, LengthMM float64
}

// DieCut reports whether the cartridge holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

// Suits reports whether the cartridge fits in the model.
func (mo Model) Suits(m Media) error {
	if m.WidthMM > mo.MaxWidthMM {
		return fmt.Errorf("%s labels are wider than the %s takes (%g mm)", m.Name, mo.Name, mo.MaxWidthMM)
	}
	if m.DieCut() && (m.LengthMM < mo.MinLengthMM || m.LengthMM > mo.MaxLengthMM) {
		return fmt.Errorf("%s labels must be %g-%g mm long on the %s", m.Name, mo.MinLengthMM, mo.MaxLengthMM, mo.Name)
	}
	return nil
}

// Cut is when the printer cuts labels off.
type Cut string

// The cuts a job can ask for. The printer's own setting is the one its
// menu holds.
const (
	CutEach    Cut = "each_label"
	CutEnd     Cut = "last_label"
	CutPrinter Cut = "printer"
)

// Cuts lists the cuts, the default first.
func Cuts() []Cut { return []Cut{CutEach, CutEnd, CutPrinter} }

// JobOptions are the per-printer choices that change the job.
type JobOptions struct {
	Cut Cut
	// Part is the Brady part number of the cartridge loaded, sent as the
	// job's substrate; empty sends none.
	Part string
	// Name is the job's name, as the printer may show it.
	Name string
}

// MaxPartLen bounds a part number.
const MaxPartLen = 64

var partPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ./-]*$`)

// Check reports whether the options can be sent.
func (o JobOptions) Check() error {
	switch o.Cut {
	case CutEach, CutEnd, CutPrinter:
	default:
		return fmt.Errorf("cut_after must be one of %s", strings.Join([]string{string(CutEach), string(CutEnd), string(CutPrinter)}, ", "))
	}
	if len(o.Part) > MaxPartLen || o.Part != "" && !partPattern.MatchString(o.Part) {
		return fmt.Errorf("part must be a Brady part number such as M6-11-427: letters, digits, spaces, dots, slashes and hyphens, at most %d", MaxPartLen)
	}
	return nil
}

// Placement says how a label image maps onto the cartridge.
type Placement struct {
	// Rotate turns the image 90° counterclockwise so its height runs across
	// the cartridge.
	Rotate bool
}

// Prepare turns a rendered label into a page: rotated if the placement says
// so, then cut to what the model prints across, around its center.
func Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, min(w, mo.HeadDots()), h))
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
