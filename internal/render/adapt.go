package render

import (
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// ErrDoesNotAdapt is wrapped by every refusal to lay an adaptive template
// out for a label.
var ErrDoesNotAdapt = errors.New("the template does not adapt to this label")

// AdaptError says which element keeps an adaptive template from a label.
type AdaptError struct {
	Path    string
	Message string
}

func (e *AdaptError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

func (e *AdaptError) Unwrap() error { return ErrDoesNotAdapt }

// Adapt lays an adaptive template out for a label width × height, in the
// template's units, printed at dpi: an ordinary fixed template, which
// renders as any other, and notices of elements left off or replaced by
// their alternatives. Each element follows the label by its anchors, is
// kept within its maximum size, and text scales its font with its box.
//
// The label must be within the template's range, and is refused when an
// element would run off it or elements would overlap that do not as
// designed. An element that does not fit, because it would have no room or
// be under its minimum size, text would fall below its minimum, or, given
// sample asset data, a code would not fit its box at dpi, does as its
// TooSmall says: refuses the label, is left off, or gives way to its
// alternative, which must fit in its place.
func Adapt(t *labels.Template, width, height float64, dpi int, sample *labels.Data) (*labels.Template, []string, error) {
	refuse := func(path, format string, args ...any) (*labels.Template, []string, error) {
		return nil, nil, &AdaptError{Path: path, Message: fmt.Sprintf(format, args...)}
	}
	if !t.Adaptive() {
		return refuse("", "the template is not adaptive")
	}
	if !t.Takes(width, height) {
		a := t.Adapt
		return refuse("", "it takes labels %s-%s × %s-%s %s, not %s × %s",
			length(a.MinWidth), length(a.MaxWidth), length(a.MinHeight), length(a.MaxHeight), t.Units, length(width), length(height))
	}
	out := *t
	out.Width, out.Height, out.DPI = width, height, dpi
	out.Sizing, out.Adapt = "", nil
	out.Elements = nil
	sx, sy := width/t.Width, height/t.Height
	on := fmt.Sprintf("a %s × %s %s label", length(width), length(height), t.Units)
	var notices []string
	// placed are the design boxes of the elements kept, by index, to check
	// overlaps against.
	var placed []int
	for i, design := range t.Elements {
		path := fmt.Sprintf("elements[%d]", i)
		e := design
		e.X, e.Width = follow(e.AnchorX, e.X, e.Width, t.Width, width, sx)
		e.Y, e.Height = follow(e.AnchorY, e.Y, e.Height, t.Height, height, sy)
		const eps = 1e-6
		// Its box is placed, then turned.
		if bx, by, bw, bh := e.Bounds(); e.Width > eps && e.Height > eps && (bx < -eps || by < -eps || bx+bw > width+eps || by+bh > height+eps) {
			return refuse(path, "would run off %s", on)
		}
		if e.Rotation == 0 {
			e.X, e.Y = math.Max(0, e.X), math.Max(0, e.Y)
			e.Width, e.Height = math.Min(e.Width, width-e.X), math.Min(e.Height, height-e.Y)
		}
		if e.MaxSize > 0 {
			// Kept at its maximum, centered where its box would be.
			if e.Width > e.MaxSize {
				e.X, e.Width = e.X+(e.Width-e.MaxSize)/2, e.MaxSize
			}
			if e.Height > e.MaxSize {
				e.Y, e.Height = e.Y+(e.Height-e.MaxSize)/2, e.MaxSize
			}
		}
		why := fits(&out, &e, &design, sample)
		if why != "" {
			switch e.TooSmall() {
			case labels.TooSmallHide:
				notices = append(notices, fmt.Sprintf("%s (%s) is left off %s: %s.", path, kind(&design), on, why))
				continue
			case labels.TooSmallAlternative:
				alt := *design.Alternative
				alt.X, alt.Y, alt.Width, alt.Height, alt.Rotation = e.X, e.Y, e.Width, e.Height, e.Rotation
				if altWhy := fits(&out, &alt, &alt, sample); altWhy != "" {
					return refuse(path, "%s, nor does its alternative: %s", why, altWhy)
				}
				notices = append(notices, fmt.Sprintf("%s (%s) gives way to its alternative (%s) on %s: %s.", path, kind(&design), kind(&alt), on, why))
				e = alt
			default:
				return refuse(path, "%s on %s", why, on)
			}
		}
		e.Alternative, e.WhenTooSmall, e.MinSize, e.MaxSize = nil, "", 0, 0
		for k, j := range placed {
			if !overlap(t.Elements[j], design) && overlap(out.Elements[k], e) {
				return refuse(fmt.Sprintf("elements[%d]", j), "would overlap elements[%d] on %s", i, on)
			}
		}
		placed = append(placed, i)
		out.Elements = append(out.Elements, e)
	}
	if out.Elements == nil {
		out.Elements = []labels.Element{}
	}
	if err := out.Validate(); err != nil {
		return refuse("", "laid out for %s at %d DPI: %v", on, dpi, err)
	}
	return &out, notices, nil
}

// fits says why an element laid out in its box would not fit, or "" when
// it would: no room, under its minimum size, text below its minimum, or a
// code too big for its box with the sample data. design is the element as
// designed, for its font.
func fits(t *labels.Template, e, design *labels.Element, sample *labels.Data) string {
	const eps = 1e-6
	if e.Width <= eps || e.Height <= eps {
		return "it would have no room"
	}
	if e.MinSize > 0 && (e.Width < e.MinSize-eps || e.Height < e.MinSize-eps) {
		return fmt.Sprintf("its box would be %s × %s %s, under its minimum of %s", length(e.Width), length(e.Height), t.Units, length(e.MinSize))
	}
	switch e.Type {
	case labels.ElementText:
		if design.Width > 0 && design.Height > 0 && design != e {
			e.FontSize = math.Min(design.FontSize*math.Min(e.Width/design.Width, e.Height/design.Height), labels.MaxFontSize)
		}
		if e.FontSize < e.MinFontSize-1e-9 {
			return fmt.Sprintf("its text would be %s pt, below its minimum of %s pt", length(e.FontSize), length(e.MinFontSize))
		}
	case labels.ElementGenerator:
		if sample == nil {
			return ""
		}
		binding, err := e.Binding()
		if err != nil {
			return ""
		}
		payload := binding.Resolve(*sample)
		if payload == "" {
			return ""
		}
		dotsPerUnit := float64(t.DPI) * t.InchesPerUnit()
		box := image.Rect(round(e.X*dotsPerUnit), round(e.Y*dotsPerUnit), round((e.X+e.Width)*dotsPerUnit), round((e.Y+e.Height)*dotsPerUnit))
		if err := (&Scene{DPI: t.DPI}).placeSymbol(e, payload, box); err != nil {
			return fmt.Sprintf("at %d DPI, for the code %q: %v", t.DPI, payload, err)
		}
	}
	return ""
}

// kind names an element for notices.
func kind(e *labels.Element) string {
	switch e.Type {
	case labels.ElementGenerator:
		return e.GeneratorType + " code"
	case labels.ElementText:
		return "text"
	}
	return "image"
}

// follow places an element's start and length along one side of the label,
// as its anchor says, when the side grows from design to size (by scale).
func follow(anchor string, start, length, design, size, scale float64) (float64, float64) {
	switch anchor {
	case labels.AnchorStart:
		return start, length
	case labels.AnchorEnd:
		return size - (design - start), length
	case labels.AnchorBoth:
		return start, size - (design - start - length) - start
	case labels.AnchorCenter:
		return size/2 + (start - design/2), length
	default:
		return start * scale, length * scale
	}
}

// overlap reports whether two elements' turned bounds overlap.
func overlap(a, b labels.Element) bool {
	const eps = 1e-6
	ax, ay, aw, ah := a.Bounds()
	bx, by, bw, bh := b.Bounds()
	return ax < bx+bw-eps && bx < ax+aw-eps && ay < by+bh-eps && by < ay+ah-eps
}

// length writes a length to a hundredth, without trailing zeros.
func length(v float64) string {
	return fmt.Sprint(math.Round(v*100) / 100)
}

// Span is the range of sizes an adaptive template is laid out at, keeping
// its own shape, without refusing: its own size scaled from MinScale to
// MaxScale. Smaller and Larger say what stops it going further, when its
// own range does not stop it first.
type Span struct {
	MinScale, MaxScale float64
	Smaller, Larger    string
}

// spanSteps is how many sizes Range tries each way from the template's own.
const spanSteps = 100

// Range works out the span of an adaptive template at dpi, with sample
// asset data for its codes, trying sizes from its own outward within its
// range. Its own size must lay out, or the error says why it does not.
func Range(t *labels.Template, dpi int, sample *labels.Data) (Span, error) {
	if !t.Adaptive() || t.Adapt == nil {
		return Span{}, &AdaptError{Message: "the template is not adaptive"}
	}
	try := func(scale float64) error {
		_, _, err := Adapt(t, t.Width*scale, t.Height*scale, dpi, sample)
		return err
	}
	if err := try(1); err != nil {
		return Span{}, err
	}
	a := t.Adapt
	lo := math.Max(a.MinWidth/t.Width, a.MinHeight/t.Height)
	hi := math.Min(a.MaxWidth/t.Width, a.MaxHeight/t.Height)
	span := Span{MinScale: 1, MaxScale: 1}
	for i := 1; i <= spanSteps; i++ {
		scale := 1 - (1-lo)*float64(i)/spanSteps
		if err := try(scale); err != nil {
			span.Smaller = err.Error()
			break
		}
		span.MinScale = scale
	}
	for i := 1; i <= spanSteps; i++ {
		scale := 1 + (hi-1)*float64(i)/spanSteps
		if err := try(scale); err != nil {
			span.Larger = err.Error()
			break
		}
		span.MaxScale = scale
	}
	return span, nil
}
