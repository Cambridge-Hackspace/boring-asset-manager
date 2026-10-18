package printers

import (
	"errors"
	"strings"
	"testing"
)

// TestFitLabel runs the cases each driver's own fit check had, before the
// check moved here, through each driver's geometry.
func TestFitLabel(t *testing.T) {
	cases := []struct {
		name, driver, model, media string
		width, height              float64
		dpi                        int
		rotate, fits               bool
	}{
		{"Brother die-cut portrait", DriverBrotherQL, "QL-810W", "dk-11201", 29, 90, 300, false, true},
		{"Brother die-cut landscape rotates", DriverBrotherQL, "QL-810W", "dk-11201", 90, 29, 300, true, true},
		{"Brother die-cut within tolerance", DriverBrotherQL, "QL-810W", "dk-11201", 29.2, 89.7, 300, false, true},
		{"Brother die-cut wrong length", DriverBrotherQL, "QL-810W", "dk-11201", 29, 60, 300, false, false},
		{"Brother continuous portrait", DriverBrotherQL, "QL-810W", "dk-22210", 29, 50, 300, false, true},
		{"Brother continuous landscape rotates", DriverBrotherQL, "QL-810W", "dk-22205", 100, 62, 300, true, true},
		{"Brother continuous too short", DriverBrotherQL, "QL-810W", "dk-22210", 29, 20, 300, false, false},
		{"Brother continuous longest", DriverBrotherQL, "QL-810W", "dk-22210", 29, 1000, 300, false, true},
		{"Brother continuous too long", DriverBrotherQL, "QL-810W", "dk-22210", 29, 1100, 300, false, false},
		{"Brother wrong width", DriverBrotherQL, "QL-810W", "dk-22205", 50, 29, 300, false, false},
		{"Brother wrong dpi", DriverBrotherQL, "QL-810W", "dk-22210", 29, 50, 203, false, false},
		{"Brother round", DriverBrotherQL, "QL-810W", "dk-11207", 58, 58, 300, false, true},
		{"Brother narrow tape", DriverBrotherQL, "QL-810W", "dk-22214", 12, 40, 300, false, true},

		{"DYMO portrait", DriverDymoLabelWriter, "LabelWriter 450", "30252", 28, 89, 300, false, true},
		{"DYMO landscape rotates", DriverDymoLabelWriter, "LabelWriter 450", "30252", 89, 28, 300, true, true},
		{"DYMO within tolerance", DriverDymoLabelWriter, "LabelWriter 450", "30252", 28.4, 88.6, 300, false, true},
		{"DYMO wrong length", DriverDymoLabelWriter, "LabelWriter 450", "30252", 28, 60, 300, false, false},
		{"DYMO continuous", DriverDymoLabelWriter, "LabelWriter 450", "30270", 57, 40, 300, false, true},
		{"DYMO continuous landscape", DriverDymoLabelWriter, "LabelWriter 450", "30270", 100, 57, 300, true, true},
		{"DYMO continuous too short", DriverDymoLabelWriter, "LabelWriter 450", "30270", 57, 5, 300, false, false},
		{"DYMO wrong width", DriverDymoLabelWriter, "LabelWriter 450", "30270", 62, 40, 300, false, false},
		{"DYMO wrong dpi", DriverDymoLabelWriter, "LabelWriter 450", "30252", 28, 89, 203, false, false},

		{"TSPL portrait", DriverTSPL, "Rollo X1038", "4x6", 101.6, 152.4, 203, false, true},
		{"TSPL landscape rotates", DriverTSPL, "Rollo X1038", "4x6", 152.4, 101.6, 203, true, true},
		{"TSPL small", DriverTSPL, "Rollo X1038", "2x1", 50.8, 25.4, 203, false, true},
		{"TSPL within tolerance", DriverTSPL, "Rollo X1038", "2x1", 51, 25, 203, false, true},
		{"TSPL wrong size", DriverTSPL, "Rollo X1038", "2x1", 40, 25.4, 203, false, false},
		{"TSPL continuous", DriverTSPL, "Rollo X1038", "continuous-4", 101.6, 30, 203, false, true},
		{"TSPL continuous too long", DriverTSPL, "Rollo X1038", "continuous-4", 101.6, 1200, 203, false, false},
		{"TSPL 300 DPI template on a 203 DPI printer", DriverTSPL, "Rollo X1038", "4x6", 101.6, 152.4, 300, false, false},
		{"TSPL 300 DPI printer", DriverTSPL, "Generic TSPL 300 DPI", "4x6", 101.6, 152.4, 300, false, true},
	}
	for _, c := range cases {
		_, f, err := PlanLabel(c.driver, Settings{Model: c.model, Media: c.media}, LabelSpec{WidthMM: c.width, HeightMM: c.height, DPI: c.dpi})
		if c.fits != (err == nil) {
			t.Errorf("%s: err = %v, want fits=%v", c.name, err, c.fits)
			continue
		}
		if err != nil && !errors.Is(err, ErrDoesNotFit) {
			t.Errorf("%s: error does not wrap ErrDoesNotFit: %v", c.name, err)
		}
		if err == nil && f.Rotate != c.rotate {
			t.Errorf("%s: rotate = %v, want %v", c.name, f.Rotate, c.rotate)
		}
	}
}

func TestFitLabelSaysWhy(t *testing.T) {
	cases := []struct {
		driver, model, media string
		label                LabelSpec
		want                 string
	}{
		{DriverBrotherQL, "QL-810W", "dk-11201", LabelSpec{WidthMM: 29, HeightMM: 60, DPI: 300}, "DK-11201 29 × 90 mm die-cut needs a 29 × 90 mm template (got 29 × 60 mm)"},
		{DriverBrotherQL, "QL-810W", "dk-22205", LabelSpec{WidthMM: 50, HeightMM: 29, DPI: 300}, "DK-22205 62 mm continuous needs one side of the template to be 62 mm (got 50 × 29 mm)"},
		{DriverBrotherQL, "QL-810W", "dk-22210", LabelSpec{WidthMM: 29, HeightMM: 20, DPI: 300}, "continuous labels must be 25-1000 mm long (got 20 mm)"},
		{DriverBrotherQL, "QL-810W", "dk-22210", LabelSpec{WidthMM: 29, HeightMM: 50, DPI: 203}, "Brother QL printers print at 300 DPI, not 203"},
		{DriverTSPL, "Rollo X1038", "4x6", LabelSpec{WidthMM: 101.6, HeightMM: 152.4, DPI: 300}, "the Rollo X1038 prints at 203 DPI, not 300"},
	}
	for _, c := range cases {
		_, _, err := PlanLabel(c.driver, Settings{Model: c.model, Media: c.media}, c.label)
		if err == nil || !strings.HasSuffix(err.Error(), c.want) {
			t.Errorf("%s %s: err = %v, want it to end %q", c.driver, c.media, err, c.want)
		}
	}
}

func TestFitWarnings(t *testing.T) {
	box := func(x, y, w, h float64) RectMM { return RectMM{X: x, Y: y, Width: w, Height: h} }
	stock := func(w, l float64) *Stock { return &Stock{ID: "s", Name: "Custom", WidthMM: w, LengthMM: l} }
	cases := []struct {
		name          string
		driver, model string
		media         string
		stock         *Stock
		label         LabelSpec
		want          []string
	}{
		{"content well inside", DriverBrotherQL, "QL-810W", "dk-11201", nil,
			LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300, Content: []RectMM{box(5, 5, 20, 19)}}, nil},
		{"nothing said without content", DriverBrotherQL, "QL-810W", "dk-11201", nil,
			LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300}, nil},
		// The QL prints the middle 25.9 mm of 29 mm tape: content at the
		// edge of a rotated label's height is outside it.
		{"Brother's unprinted edge", DriverBrotherQL, "QL-810W", "dk-11201", nil,
			LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300, Content: []RectMM{box(10, 0.5, 20, 10)}},
			[]string{"it reaches the middle 25.9 mm of the label's 29 mm width and the middle 83.9 mm of its 90 mm length"}},
		{"Brother's unprinted length", DriverBrotherQL, "QL-810W", "dk-11201", nil,
			LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300, Content: []RectMM{box(1, 10, 10, 10)}},
			[]string{"does not print"}},
		{"Brother continuous tape has no edge along", DriverBrotherQL, "QL-810W", "dk-22205", nil,
			LabelSpec{WidthMM: 62, HeightMM: 40, DPI: 300, Content: []RectMM{box(5, 0, 50, 40)}}, nil},
		// A first-generation LabelWriter reaches 56.9 mm of a 59 mm label,
		// from the guide.
		{"DYMO's far edge", DriverDymoLabelWriter, "LabelWriter 450", "30256", nil,
			LabelSpec{WidthMM: 59, HeightMM: 102, DPI: 300, Content: []RectMM{box(50, 10, 8, 10)}},
			[]string{"it reaches the first 56.9 mm of the label's 59 mm width"}},
		{"DYMO's near edge prints", DriverDymoLabelWriter, "LabelWriter 450", "30256", nil,
			LabelSpec{WidthMM: 59, HeightMM: 102, DPI: 300, Content: []RectMM{box(2, 10, 8, 10)}}, nil},
		{"second-generation DYMO receives it all", DriverDymoLabelWriter, "LabelWriter 550", "30256", nil,
			LabelSpec{WidthMM: 59, HeightMM: 102, DPI: 300, Content: []RectMM{box(50, 10, 8, 10)}}, nil},
		{"die-cut drift", DriverTSPL, "Rollo X1038", "2x1", nil,
			LabelSpec{WidthMM: 50.8, HeightMM: 25.4, DPI: 203, Content: []RectMM{box(0.5, 5, 20, 10)}},
			[]string{"within 1 mm of the label's edge"}},
		{"a label wider than the head", DriverTSPL, "Rollo X1038", "", stock(106, 50),
			LabelSpec{WidthMM: 106, HeightMM: 50, DPI: 203, Content: []RectMM{box(0.5, 10, 50, 10)}},
			[]string{"the middle 104.1 mm"}},
		{"custom stock on a DYMO 550", DriverDymoLabelWriter, "LabelWriter 550", "", stock(50, 30),
			LabelSpec{WidthMM: 50, HeightMM: 30, DPI: 300},
			[]string{"The LabelWriter 550 prints only on genuine DYMO labels"}},
	}
	for _, c := range cases {
		s := Settings{Model: c.model, Media: c.media, Stock: c.stock}
		if c.stock != nil {
			s.Media = c.stock.ID
		}
		_, f, err := PlanLabel(c.driver, s, c.label)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(f.Warnings) != len(c.want) {
			t.Errorf("%s: warnings %q, want %d", c.name, f.Warnings, len(c.want))
			continue
		}
		for i, w := range c.want {
			if !strings.Contains(f.Warnings[i], w) {
				t.Errorf("%s: warning %q, want it to say %q", c.name, f.Warnings[i], w)
			}
		}
	}
}
