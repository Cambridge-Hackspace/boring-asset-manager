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
// renders as any other. Each element follows the label by its anchors, and
// text scales its font with its box. The label must be within the
// template's range; and the layout is refused when an element would run
// off the label or out of room, text would fall below its minimum size,
// two elements that do not overlap as designed would, or, given sample
// asset data, a code would not fit its box at dpi.
func Adapt(t *labels.Template, width, height float64, dpi int, sample *labels.Data) (*labels.Template, error) {
	refuse := func(path, format string, args ...any) (*labels.Template, error) {
		return nil, &AdaptError{Path: path, Message: fmt.Sprintf(format, args...)}
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
	out.Elements = make([]labels.Element, len(t.Elements))
	sx, sy := width/t.Width, height/t.Height
	for i, e := range t.Elements {
		path := fmt.Sprintf("elements[%d]", i)
		e.X, e.Width = follow(e.AnchorX, e.X, e.Width, t.Width, width, sx)
		e.Y, e.Height = follow(e.AnchorY, e.Y, e.Height, t.Height, height, sy)
		const eps = 1e-6
		if e.Width <= eps || e.Height <= eps {
			return refuse(path, "would have no room on a %s × %s %s label", length(width), length(height), t.Units)
		}
		if e.X < -eps || e.Y < -eps || e.X+e.Width > width+eps || e.Y+e.Height > height+eps {
			return refuse(path, "would run off a %s × %s %s label", length(width), length(height), t.Units)
		}
		e.X, e.Y = math.Max(0, e.X), math.Max(0, e.Y)
		e.Width, e.Height = math.Min(e.Width, width-e.X), math.Min(e.Height, height-e.Y)
		if e.Type == labels.ElementText {
			orig := t.Elements[i]
			e.FontSize = math.Min(orig.FontSize*math.Min(e.Width/orig.Width, e.Height/orig.Height), labels.MaxFontSize)
			if e.FontSize < e.MinFontSize-1e-9 {
				return refuse(path, "its text would be %s pt, below its minimum of %s pt", length(e.FontSize), length(e.MinFontSize))
			}
		}
		out.Elements[i] = e
	}
	for i := range t.Elements {
		for j := i + 1; j < len(t.Elements); j++ {
			if !overlap(t.Elements[i], t.Elements[j]) && overlap(out.Elements[i], out.Elements[j]) {
				return refuse(fmt.Sprintf("elements[%d]", i), "would overlap elements[%d] on a %s × %s %s label", j, length(width), length(height), t.Units)
			}
		}
	}
	if err := out.Validate(); err != nil {
		return refuse("", "laid out for %s × %s %s at %d DPI: %v", length(width), length(height), t.Units, dpi, err)
	}
	if sample != nil {
		if err := symbolsFit(&out, *sample); err != nil {
			return nil, err
		}
	}
	return &out, nil
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

func overlap(a, b labels.Element) bool {
	const eps = 1e-6
	return a.X < b.X+b.Width-eps && b.X < a.X+a.Width-eps && a.Y < b.Y+b.Height-eps && b.Y < a.Y+a.Height-eps
}

// symbolsFit checks that each code element, laid out, holds its code from
// sample data at the template's DPI. Codes sample data lacks are not
// checked.
func symbolsFit(t *labels.Template, sample labels.Data) error {
	dotsPerUnit := float64(t.DPI) * t.InchesPerUnit()
	canvas := image.Rect(0, 0, max(1, round(t.Width*dotsPerUnit)), max(1, round(t.Height*dotsPerUnit)))
	for i := range t.Elements {
		e := &t.Elements[i]
		if e.Type != labels.ElementGenerator {
			continue
		}
		binding, err := e.Binding()
		if err != nil {
			continue
		}
		payload := binding.Resolve(sample)
		if payload == "" {
			continue
		}
		box := image.Rect(round(e.X*dotsPerUnit), round(e.Y*dotsPerUnit),
			round((e.X+e.Width)*dotsPerUnit), round((e.Y+e.Height)*dotsPerUnit)).Intersect(canvas)
		if err := (&Scene{}).placeSymbol(e, payload, box); err != nil {
			return &AdaptError{Path: fmt.Sprintf("elements[%d]", i), Message: fmt.Sprintf("at %d DPI, for the code %q: %v", t.DPI, payload, err)}
		}
	}
	return nil
}

// length writes a length to a hundredth, without trailing zeros.
func length(v float64) string {
	return fmt.Sprint(math.Round(v*100) / 100)
}
