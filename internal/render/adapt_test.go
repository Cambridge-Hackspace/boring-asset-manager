package render

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// adaptiveTag is a 62 × 29 mm label taking 25-102 mm by 20-60 mm: a code
// pinned to the left, a name stretching beside it, and a note scaled.
func adaptiveTag() *labels.Template {
	t := &labels.Template{
		Name: "Tag", Units: labels.UnitsMM, Width: 62, Height: 29, DPI: 300, Sizing: labels.SizingAdaptive,
		Adapt: &labels.Adapt{MinWidth: 25, MaxWidth: 102, MinHeight: 20, MaxHeight: 60},
		Elements: []labels.Element{
			{Type: labels.ElementGenerator, GeneratorType: "qr", X: 2, Y: 2, Width: 25, Height: 25, AnchorX: labels.AnchorStart, AnchorY: labels.AnchorStart},
			{Type: labels.ElementText, X: 29, Y: 2, Width: 31, Height: 10, FontSize: 16, ContentBinding: "{{asset.name}}", AnchorX: labels.AnchorBoth, AnchorY: labels.AnchorStart},
			{Type: labels.ElementText, X: 29, Y: 15, Width: 31, Height: 10, FontSize: 12, MinFontSize: 6, ContentBinding: "{{asset.categories}}"},
		},
	}
	t.Normalize()
	return t
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAdaptFollowsAnchors(t *testing.T) {
	tag := adaptiveTag()
	if err := tag.Validate(); err != nil {
		t.Fatal(err)
	}
	laid, err := Adapt(tag, 90, 29, 203, nil)
	if err != nil {
		t.Fatal(err)
	}
	if laid.Adaptive() || laid.Adapt != nil || laid.Width != 90 || laid.Height != 29 || laid.DPI != 203 {
		t.Fatalf("laid out: %+v", laid)
	}
	code, name, note := laid.Elements[0], laid.Elements[1], laid.Elements[2]
	if code.X != 2 || code.Width != 25 || code.Y != 2 {
		t.Errorf("pinned to the start: %+v", code)
	}
	// Both margins kept: 29 from the left, 2 from the right.
	if name.X != 29 || !near(name.Width, 59) || name.FontSize != 16 {
		t.Errorf("stretched: %+v", name)
	}
	// Scaled by 90/62 across and 1 down; its font by the lesser.
	if !near(note.X, 29*90.0/62) || !near(note.Width, 31*90.0/62) || note.Y != 15 || note.FontSize != 12 {
		t.Errorf("scaled: %+v", note)
	}

	// Taller, the scaled note's font grows with the lesser of its scales.
	laid, _ = Adapt(tag, 90, 40, 300, nil)
	if fs := laid.Elements[2].FontSize; !near(fs, 12*40.0/29) {
		t.Errorf("font on a taller label: %v", fs)
	}

	for _, c := range []struct {
		anchor                             string
		start, length, design, size, scale float64
		wantStart, wantLength              float64
	}{
		{labels.AnchorEnd, 50, 10, 62, 90, 90.0 / 62, 78, 10},
		{labels.AnchorCenter, 26, 10, 62, 90, 90.0 / 62, 40, 10},
		{"", 10, 10, 62, 31, 0.5, 5, 5},
	} {
		if s, l := follow(c.anchor, c.start, c.length, c.design, c.size, c.scale); !near(s, c.wantStart) || !near(l, c.wantLength) {
			t.Errorf("%q: %v, %v", c.anchor, s, l)
		}
	}
}

func TestAdaptRefuses(t *testing.T) {
	long := &labels.Data{Codes: map[string]string{"qr": strings.Repeat("bin-000123-", 36)}}
	cases := []struct {
		name          string
		width, height float64
		dpi           int
		sample        *labels.Data
		want          string
	}{
		{"outside its range", 120, 29, 300, nil, "it takes labels 25-102 × 20-60 mm"},
		// The stretched name loses its room when the label is narrower than
		// the code and the margins.
		{"no room", 30, 29, 300, nil, "elements[1]: would have no room"},
		// A code taller than the label.
		{"off the label", 62, 20, 300, nil, "elements[0]: would run off a 62 × 20 mm label"},
		{"a long code", 62, 29, 150, long, "elements[0]: at 150 DPI, for the code"},
	}
	for _, c := range cases {
		_, err := Adapt(adaptiveTag(), c.width, c.height, c.dpi, c.sample)
		if !errors.Is(err, ErrDoesNotAdapt) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}

	// Narrower, the scaled note moves under the pinned code (without the
	// name, whose font would first fall below its minimum).
	tag := adaptiveTag()
	tag.Elements = []labels.Element{tag.Elements[0], tag.Elements[2]}
	if _, err := Adapt(tag, 40, 29, 300, nil); err == nil || !strings.Contains(err.Error(), "elements[0]: would overlap elements[1]") {
		t.Errorf("overlapping: %v", err)
	}

	// A note that shrinks below its minimum font.
	tag = adaptiveTag()
	tag.Elements = tag.Elements[2:]
	tag.Elements[0].MinFontSize = 10
	if _, err := Adapt(tag, 31, 29, 300, nil); err == nil || !strings.Contains(err.Error(), "below its minimum of 10 pt") {
		t.Errorf("text below its minimum: %v", err)
	}
	// Short codes fit.
	if _, err := Adapt(adaptiveTag(), 62, 29, 300, &labels.Data{Codes: map[string]string{"qr": "BIN-1"}}); err != nil {
		t.Errorf("a short code: %v", err)
	}
	// A fixed template is not adapted.
	fixed := adaptiveTag()
	fixed.Sizing, fixed.Adapt = "", nil
	if _, err := Adapt(fixed, 62, 29, 300, nil); err == nil {
		t.Error("adapted a fixed template")
	}
}
