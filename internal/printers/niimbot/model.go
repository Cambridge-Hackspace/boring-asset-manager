package niimbot

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// Task is the sequence of commands a model prints with. Models and their
// firmware differ in it, and the community's descriptions name each after
// a model that uses it (docs/printing.md#niimbot).
type Task string

const (
	// TaskD110 starts the print with a 1-byte print start; each page clears
	// the buffer, starts, gives its size as rows and columns and its
	// quantity apart, and waits for the printer's status to count it
	// printed.
	TaskD110 Task = "d110"
	// TaskB1 starts the print with a 7-byte print start that gives the
	// number of pages; each page starts and gives its size with its
	// quantity, and the printer's status is waited on once, for the last.
	TaskB1 Task = "b1"
	// TaskB21V1 starts the print with a 1-byte print start; each page
	// starts and gives its size as rows and columns, and print end is
	// asked again until the printer says it has finished.
	TaskB21V1 Task = "b21_v1"
)

// Tasks lists the print tasks.
func Tasks() []Task { return []Task{TaskB1, TaskB21V1, TaskD110} }

// Label types, as SetLabelType names them and a roll's RFID tag reports
// them.
const (
	LabelGaps        = 1
	LabelBlackMark   = 2
	LabelContinuous  = 3
	LabelPerforated  = 4
	LabelTransparent = 5
	LabelPVCTag      = 6
	LabelBlackGap    = 10
	LabelTube        = 11
)

// Family is the kind of roll a model takes.
type Family string

const (
	// FamilyD is the D series' narrow rolls.
	FamilyD Family = "d"
	// FamilyB18 is the B18's thermal transfer tape.
	FamilyB18 Family = "b18"
	// FamilyB is the B series' and K3's rolls.
	FamilyB Family = "b"
)

// Model is a Niimbot printer.
type Model struct {
	Name string
	// DPI is the head's resolution: 203 on every model listed.
	DPI int
	// HeadDots is the width of the print head, in dots.
	HeadDots int
	// MaxRollMM is the widest roll the model takes, a little wider than
	// its head, which prints the middle of it.
	MaxRollMM float64
	Family    Family
	// Density is the range the model takes and its default.
	MinDensity, MaxDensity, Density int
	// Continuous is set when the model takes continuous rolls.
	Continuous bool
	// Task is the print task the model's firmware is known to take, or
	// assumed to where none is recorded.
	Task Task
}

var models = map[string]Model{}

func init() {
	for _, m := range []Model{
		// 12 mm heads of 96 dots, density 1-3.
		{Name: "D11", HeadDots: 96, MaxRollMM: 15, Family: FamilyD, MinDensity: 1, MaxDensity: 3, Density: 2, Task: TaskD110},
		{Name: "D110", HeadDots: 96, MaxRollMM: 15, Family: FamilyD, MinDensity: 1, MaxDensity: 3, Density: 2, Task: TaskD110},
		{Name: "B18", HeadDots: 96, MaxRollMM: 15, Family: FamilyB18, MinDensity: 1, MaxDensity: 3, Density: 2, Continuous: true, Task: TaskB1},
		// A 24 mm head of 192 dots.
		{Name: "D101", HeadDots: 192, MaxRollMM: 25, Family: FamilyD, MinDensity: 1, MaxDensity: 3, Density: 2, Task: TaskB1},
		// 48 mm heads of 384 dots, density 1-5.
		{Name: "B1", HeadDots: 384, MaxRollMM: 50, Family: FamilyB, MinDensity: 1, MaxDensity: 5, Density: 3, Task: TaskB1},
		{Name: "B21", HeadDots: 384, MaxRollMM: 50, Family: FamilyB, MinDensity: 1, MaxDensity: 5, Density: 3, Continuous: true, Task: TaskB21V1},
		// A 72 mm head of 576 dots.
		{Name: "B3S", HeadDots: 576, MaxRollMM: 75, Family: FamilyB, MinDensity: 1, MaxDensity: 5, Density: 3, Continuous: true, Task: TaskB1},
		// An 80 mm head of 640 dots.
		{Name: "K3", HeadDots: 640, MaxRollMM: 80, Family: FamilyB, MinDensity: 1, MaxDensity: 5, Density: 3, Task: TaskB1},
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

// Dots converts millimeters to dots at the model's resolution.
func (mo Model) Dots(mm float64) int { return int(math.Round(mm / 25.4 * float64(mo.DPI))) }

// Continuous labels must be between these lengths, in millimeters: BAM!'s
// own bounds, since no figure is published.
const (
	MinContinuousMM = 10
	MaxContinuousMM = 200
)

// Media is a roll of labels.
type Media struct {
	ID   string
	Name string
	// WidthMM and LengthMM are the label size; LengthMM is 0 for
	// continuous media.
	WidthMM, LengthMM float64
	Family            Family
}

// DieCut reports whether the roll holds pre-cut labels.
func (m Media) DieCut() bool { return m.LengthMM > 0 }

// LabelType is the label type a job on the roll names.
func (m Media) LabelType() byte {
	if m.DieCut() {
		return LabelGaps
	}
	return LabelContinuous
}

var catalog = map[string]Media{}

func init() {
	for _, m := range []Media{
		roll(FamilyD, 12, 22), roll(FamilyD, 12, 30), roll(FamilyD, 12, 40), roll(FamilyD, 15, 30), roll(FamilyD, 15, 50),
		roll(FamilyB18, 14, 30), roll(FamilyB18, 14, 40), roll(FamilyB18, 14, 50), roll(FamilyB18, 14, 0),
		roll(FamilyB, 30, 15), roll(FamilyB, 40, 30), roll(FamilyB, 50, 30), roll(FamilyB, 50, 50), roll(FamilyB, 50, 0),
	} {
		catalog[m.ID] = m
	}
}

// roll is a family's roll of labels w × l mm, or continuous w mm wide when
// l is 0.
func roll(f Family, w, l float64) Media {
	prefix, what := "", ""
	if f == FamilyB18 {
		prefix, what = "b18-", " tape"
	}
	if l == 0 {
		return Media{ID: fmt.Sprintf("%s%g-continuous", prefix, w), Name: fmt.Sprintf("%g mm continuous%s", w, what), WidthMM: w, Family: f}
	}
	return Media{ID: fmt.Sprintf("%s%gx%g", prefix, w, l), Name: fmt.Sprintf("%g × %g mm%s", w, l, what), WidthMM: w, LengthMM: l, Family: f}
}

// SizedMedia is custom stock, known by its size alone, for a model.
func SizedMedia(mo Model, id, name string, widthMM, lengthMM float64) Media {
	return Media{ID: id, Name: name, WidthMM: widthMM, LengthMM: lengthMM, Family: mo.Family}
}

// LookupMedia returns a roll by ID.
func LookupMedia(id string) (Media, bool) {
	m, ok := catalog[id]
	return m, ok
}

// MediaIDs lists the built-in rolls.
func MediaIDs() []string {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Takes reports whether the model takes the roll.
func (mo Model) Takes(m Media) error {
	switch {
	case m.Family != mo.Family:
		return fmt.Errorf("%s is not a roll the %s takes", m.Name, mo.Name)
	case m.WidthMM > mo.MaxRollMM:
		return fmt.Errorf("%s is wider than the %s takes (%g mm)", m.Name, mo.Name, mo.MaxRollMM)
	case !m.DieCut() && !mo.Continuous:
		return fmt.Errorf("the %s takes no continuous rolls", mo.Name)
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
// so, then centered across the head and cut to what it reaches. The page
// is as wide as the head, and the printer is told so.
func Prepare(mo Model, label *image.Gray, p Placement) *image.Gray {
	src := label
	if p.Rotate {
		src = rotateCounterclockwise(label)
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	page := image.NewGray(image.Rect(0, 0, mo.HeadDots, h))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	off := (w - mo.HeadDots) / 2
	for y := 0; y < h; y++ {
		for x := 0; x < mo.HeadDots; x++ {
			if sx := x + off; sx >= 0 && sx < w {
				page.Pix[y*page.Stride+x] = src.Pix[y*src.Stride+sx]
			}
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
