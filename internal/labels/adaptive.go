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
