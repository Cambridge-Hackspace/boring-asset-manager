package printers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Geometry is what a printer as configured offers a label: its resolution,
// the roll loaded, and how much of each label its head reaches. Drivers
// report it; FitLabel decides from it, the same way for every driver.
type Geometry struct {
	// DPI is the resolution templates must be designed at.
	DPI int
	// Prints names who prints at DPI, for messages: "Brother QL printers
	// print" or "the Rollo X1038 prints".
	Prints string
	// Roll names the roll loaded, for messages.
	Roll string
	// WidthMM is the label width across the roll; LengthMM the length of a
	// die-cut label, or 0 for a continuous roll.
	WidthMM, LengthMM float64
	// MinLengthMM and MaxLengthMM bound labels from a continuous roll.
	MinLengthMM, MaxLengthMM float64
	// ReachMM is how much of the label's width the head prints: centered
	// on the label, or from its near edge when FromNearEdge is set. 0 means
	// all of it.
	ReachMM      float64
	FromNearEdge bool
	// ReachAlongMM is how much of a die-cut label's length is printed,
	// centered; 0 means all of it.
	ReachAlongMM float64
	// Warnings are about the printer as configured, whatever the label.
	Warnings []string
}

// DieCut reports whether the roll holds pre-cut labels.
func (g Geometry) DieCut() bool { return g.LengthMM > 0 }

// GeometryOf is the geometry a driver reports for a printer.
func GeometryOf(driver string, s Settings) (Geometry, error) {
	d, err := Lookup(driver)
	if err != nil {
		return Geometry{}, err
	}
	return d.Geometry(s)
}

// RectMM is a box on a label, in millimeters from its top left corner.
type RectMM struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Fit is how labels suit a printer: whether they are turned to run along
// the roll, and what to know before printing them.
type Fit struct {
	// Rotate turns the label 90° counterclockwise so its height runs
	// across the roll.
	Rotate bool
	// Warnings never stop printing.
	Warnings []string
}

// SizeTolerance absorbs rounding when matching template sizes to labels, in
// millimeters.
const SizeTolerance = 0.5

// DriftMM is how far die-cut labels may shift as they feed: content closer
// than this to their edge may be cut off.
const DriftMM = 1.0

// FitLabel checks a label against a printer's geometry. One side of the
// label must be the roll's width; on die-cut rolls the other must be the
// label length, and on continuous ones within the printer's range. A label
// whose height is the roll's width is rotated. Errors wrap ErrDoesNotFit.
//
// The label's content, when given, is checked against what the head
// reaches and against die-cut labels' edges, for warnings.
func FitLabel(g Geometry, label LabelSpec) (Fit, error) {
	// Every reason it does not fit, so they can all be put right at once.
	var reasons []string
	if label.DPI != g.DPI {
		reasons = append(reasons, fmt.Sprintf("it is laid out at %d DPI, but %s at %d DPI", label.DPI, g.Prints, g.DPI))
	}
	matches := func(a, b float64) bool { return math.Abs(a-b) <= SizeTolerance }
	for _, rotate := range []bool{false, true} {
		across, along := label.WidthMM, label.HeightMM
		if rotate {
			across, along = along, across
		}
		if !matches(across, g.WidthMM) {
			continue
		}
		if g.DieCut() {
			if !matches(along, g.LengthMM) {
				continue
			}
		} else if halfDot := 12.7 / float64(g.DPI); along < g.MinLengthMM-halfDot || along > g.MaxLengthMM+halfDot {
			// Within half a dot, the label rounds to a length in range.
			return Fit{}, doesNotFit(append(reasons, fmt.Sprintf("it is %s mm long, but continuous labels must be %s-%s mm long",
				mm(along), mm(g.MinLengthMM), mm(g.MaxLengthMM))))
		}
		if len(reasons) > 0 {
			return Fit{}, doesNotFit(reasons)
		}
		f := Fit{Rotate: rotate, Warnings: append([]string{}, g.Warnings...)}
		f.Warnings = append(f.Warnings, contentWarnings(g, label, rotate, across, along)...)
		return f, nil
	}
	if g.DieCut() {
		return Fit{}, doesNotFit(append(reasons, fmt.Sprintf("it is %s × %s mm, but the roll loaded, %s, takes %s × %s mm",
			mm(label.WidthMM), mm(label.HeightMM), g.Roll, mm(g.WidthMM), mm(g.LengthMM))))
	}
	return Fit{}, doesNotFit(append(reasons, fmt.Sprintf("it is %s × %s mm, but the roll loaded, %s, needs one side to be %s mm",
		mm(label.WidthMM), mm(label.HeightMM), g.Roll, mm(g.WidthMM))))
}

// doesNotFit is an error wrapping ErrDoesNotFit that gives its reasons.
func doesNotFit(reasons []string) error {
	return fmt.Errorf("%w: %s", ErrDoesNotFit, strings.Join(reasons, "; "))
}

// Why is what an error wrapping ErrDoesNotFit says after it: the reasons a
// template does not fit, as a clause ("it is laid out at 203 DPI, …").
func Why(err error) string {
	return strings.TrimPrefix(err.Error(), ErrDoesNotFit.Error()+": ")
}

// contentWarnings says when the label has content where it will not print,
// or so near a die-cut label's edge that it may be cut off.
func contentWarnings(g Geometry, label LabelSpec, rotate bool, across, along float64) []string {
	if len(label.Content) == 0 {
		return nil
	}
	// The printed part of the label, across and along the roll.
	acrossFrom, acrossTo := 0.0, across
	if g.ReachMM > 0 && g.ReachMM < across {
		if g.FromNearEdge {
			acrossTo = g.ReachMM
		} else {
			acrossFrom = (across - g.ReachMM) / 2
			acrossTo = acrossFrom + g.ReachMM
		}
	}
	alongFrom, alongTo := 0.0, along
	if g.DieCut() && g.ReachAlongMM > 0 && g.ReachAlongMM < along {
		alongFrom = (along - g.ReachAlongMM) / 2
		alongTo = alongFrom + g.ReachAlongMM
	}

	const eps = 0.05
	var unprinted, nearEdge bool
	for _, r := range label.Content {
		// Rotated, the label's height runs across the roll.
		a0, a1, l0, l1 := r.X, r.X+r.Width, r.Y, r.Y+r.Height
		if rotate {
			a0, a1, l0, l1 = r.Y, r.Y+r.Height, r.X, r.X+r.Width
		}
		if a0 < acrossFrom-eps || a1 > acrossTo+eps || l0 < alongFrom-eps || l1 > alongTo+eps {
			unprinted = true
			continue
		}
		if g.DieCut() && (a0 < DriftMM-eps || a1 > across-DriftMM+eps || l0 < DriftMM-eps || l1 > along-DriftMM+eps) {
			nearEdge = true
		}
	}
	var out []string
	if unprinted {
		out = append(out, fmt.Sprintf("Part of the template is where the printer does not print: it reaches %s of the label's %s mm width%s.",
			reachDescription(g, across), mm(across), alongDescription(g, along)))
	}
	if nearEdge {
		out = append(out, fmt.Sprintf("Part of the template is within %s mm of the label's edge, which may be cut off as die-cut labels shift a little.", mm(DriftMM)))
	}
	return out
}

func reachDescription(g Geometry, across float64) string {
	if g.ReachMM <= 0 || g.ReachMM >= across {
		return "all"
	}
	if g.FromNearEdge {
		return "the first " + mm(g.ReachMM) + " mm"
	}
	return "the middle " + mm(g.ReachMM) + " mm"
}

func alongDescription(g Geometry, along float64) string {
	if !g.DieCut() || g.ReachAlongMM <= 0 || g.ReachAlongMM >= along {
		return ""
	}
	return " and the middle " + mm(g.ReachAlongMM) + " mm of its " + mm(along) + " mm length"
}

// mm writes a length to a tenth of a millimeter, without trailing zeros.
func mm(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}
