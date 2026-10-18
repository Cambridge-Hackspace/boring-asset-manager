package labels

import (
	"fmt"
	"math"
)

// A template is fixed, printed only at its own size and DPI, or adaptive:
// designed at one size, it is laid out again for the size of the label it
// prints on, and rendered at the printer's DPI (README §4.1).
const (
	SizingFixed    = "fixed"
	SizingAdaptive = "adaptive"
)

// How an element of an adaptive template follows the label, across (x) or
// down (y): kept in proportion (scale, the default), kept at its distance
// from the start or end edge, kept at both (stretching), or kept at its
// distance from the middle.
const (
	AnchorScale  = "scale"
	AnchorStart  = "start"
	AnchorEnd    = "end"
	AnchorBoth   = "both"
	AnchorCenter = "center"
)

// How long an adaptive template's labels are on a continuous roll, whose
// width only is given: in proportion to the width, or as designed.
const (
	LengthProportional = "proportional"
	LengthFixed        = "fixed"
)

// Adapt says which label sizes an adaptive template takes, in its units.
type Adapt struct {
	MinWidth  float64 `json:"min_width"`
	MaxWidth  float64 `json:"max_width"`
	MinHeight float64 `json:"min_height"`
	MaxHeight float64 `json:"max_height"`
	// ContinuousLength is LengthProportional or LengthFixed.
	ContinuousLength string `json:"continuous_length"`
}

// Adaptive reports whether the template is laid out again for each label.
func (t *Template) Adaptive() bool { return t.Sizing == SizingAdaptive }

// Takes reports whether an adaptive template takes a label of this size, in
// its units.
func (t *Template) Takes(width, height float64) bool {
	const eps = 1e-6
	a := t.Adapt
	return a != nil && width >= a.MinWidth-eps && width <= a.MaxWidth+eps && height >= a.MinHeight-eps && height <= a.MaxHeight+eps
}

// What an element of an adaptive template does on a label where it does
// not fit.
const (
	TooSmallRefuse      = "refuse"
	TooSmallHide        = "hide"
	TooSmallAlternative = "alternative"
)

// TooSmall is what the element does where it does not fit: as it says, or
// by default, an image is left off and anything else refuses the label.
func (e *Element) TooSmall() string {
	switch {
	case e.WhenTooSmall != "":
		return e.WhenTooSmall
	case e.Type == ElementStaticImage:
		return TooSmallHide
	}
	return TooSmallRefuse
}

var tooSmall = map[string]bool{"": true, TooSmallRefuse: true, TooSmallHide: true, TooSmallAlternative: true}

// validateLimits checks an element's limits and its alternative, which is
// checked as if it had the element's box.
func (t *Template) validateLimits(v *validator, path string, e *Element, canvasOK bool) {
	at := func(prop string) string { return path + "." + prop }
	bad := func(x float64) bool { return math.IsNaN(x) || math.IsInf(x, 0) || x < 0 }
	if bad(e.MinSize) {
		v.add(at("min_size"), "must be zero or a positive number")
	}
	if bad(e.MaxSize) {
		v.add(at("max_size"), "must be zero or a positive number")
	} else if e.MaxSize > 0 && e.MinSize > e.MaxSize {
		v.add(at("max_size"), "must be at least min_size")
	}
	if !tooSmall[e.WhenTooSmall] {
		v.add(at("when_too_small"), `must be "refuse", "hide" or "alternative"`)
	}
	alt := e.Alternative
	switch {
	case e.WhenTooSmall == TooSmallAlternative && alt == nil:
		v.add(at("alternative"), "is required when it gives way to an alternative")
	case alt != nil && e.WhenTooSmall != TooSmallAlternative:
		v.add(at("alternative"), `applies only when when_too_small is "alternative"`)
	}
	if alt == nil {
		return
	}
	if alt.Type != ElementGenerator && alt.Type != ElementText {
		v.add(at("alternative.type"), `must be "generator" or "text"`)
		return
	}
	if alt.Alternative != nil || alt.WhenTooSmall != "" {
		v.add(at("alternative"), "cannot have an alternative of its own")
		return
	}
	placed := *alt
	placed.X, placed.Y, placed.Width, placed.Height = e.X, e.Y, e.Width, e.Height
	placed.AnchorX, placed.AnchorY = "", ""
	t.validateElement(v, at("alternative"), &placed, canvasOK)
}

var anchors = map[string]bool{"": true, AnchorScale: true, AnchorStart: true, AnchorEnd: true, AnchorBoth: true, AnchorCenter: true}

// validateSizing checks the template's sizing and, for an adaptive one,
// its range, which must hold its own size.
func (t *Template) validateSizing(v *validator, canvasOK bool) {
	switch t.Sizing {
	case "":
		if t.Adapt != nil {
			v.add("adapt", "applies only to adaptive templates")
		}
		return
	case SizingAdaptive:
	default:
		v.add("sizing", `must be "fixed" or "adaptive"`)
		return
	}
	a := t.Adapt
	if a == nil {
		v.add("adapt", "is required for an adaptive template: the widths and heights it takes")
		return
	}
	finite := func(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) && x > 0 }
	for _, r := range []struct {
		lo, hi, design float64
		lop, hip, side string
	}{{a.MinWidth, a.MaxWidth, t.Width, "adapt.min_width", "adapt.max_width", "width"}, {a.MinHeight, a.MaxHeight, t.Height, "adapt.min_height", "adapt.max_height", "height"}} {
		if !finite(r.lo) || !finite(r.hi) {
			v.add(r.lop, "the range must be positive numbers")
			continue
		}
		if canvasOK && (r.lo > r.design+geometryTolerance || r.hi < r.design-geometryTolerance) {
			v.add(r.lop, fmt.Sprintf("the range must include the template's own %s", r.side))
		}
		if dots := r.hi * t.InchesPerUnit() * MaxDPI; t.Units != "" && dots > MaxCanvasDots {
			v.add(r.hip, fmt.Sprintf("exceeds %d dots at %d DPI", MaxCanvasDots, MaxDPI))
		}
	}
	if a.ContinuousLength != LengthProportional && a.ContinuousLength != LengthFixed {
		v.add("adapt.continuous_length", `must be "proportional" or "fixed"`)
	}
}
