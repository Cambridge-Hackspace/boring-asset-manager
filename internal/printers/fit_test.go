package printers

import (
	"errors"
	"math"
	"slices"
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
		{DriverBrotherQL, "QL-810W", "dk-11201", LabelSpec{WidthMM: 29, HeightMM: 60, DPI: 300}, "it is 29 × 60 mm, but the roll loaded, DK-11201 29 × 90 mm die-cut, takes 29 × 90 mm"},
		{DriverBrotherQL, "QL-810W", "dk-22205", LabelSpec{WidthMM: 50, HeightMM: 29, DPI: 300}, "it is 50 × 29 mm, but the roll loaded, DK-22205 62 mm continuous, needs one side to be 62 mm"},
		{DriverBrotherQL, "QL-810W", "dk-22210", LabelSpec{WidthMM: 29, HeightMM: 20, DPI: 300}, "it is 20 mm long, but continuous labels must be 25-1000 mm long"},
		{DriverBrotherQL, "QL-810W", "dk-22210", LabelSpec{WidthMM: 29, HeightMM: 50, DPI: 203}, "it is laid out at 203 DPI, but Brother QL printers print at 300 DPI"},
		{DriverTSPL, "Rollo X1038", "4x6", LabelSpec{WidthMM: 101.6, HeightMM: 152.4, DPI: 300}, "it is laid out at 300 DPI, but the Rollo X1038 prints at 203 DPI"},
		// Every reason at once: the resolution and the size.
		{DriverBrotherQL, "QL-810W", "dk-22205", LabelSpec{WidthMM: 101.6, HeightMM: 152.4, DPI: 203},
			"it is laid out at 203 DPI, but Brother QL printers print at 300 DPI; it is 101.6 × 152.4 mm, but the roll loaded, DK-22205 62 mm continuous, needs one side to be 62 mm"},
	}
	for _, c := range cases {
		_, _, err := PlanLabel(c.driver, Settings{Model: c.model, Media: c.media}, c.label)
		if err == nil || !errors.Is(err, ErrDoesNotFit) || Why(err) != c.want {
			t.Errorf("%s %s: err = %v, want %q", c.driver, c.media, err, c.want)
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
			[]string{"it prints only the middle 25.9 mm of the label's 29 mm width and the middle 83.9 mm of its 90 mm length"}},
		{"Brother's unprinted length", DriverBrotherQL, "QL-810W", "dk-11201", nil,
			LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300, Content: []RectMM{box(1, 10, 10, 10)}},
			[]string{"does not print"}},
		{"Brother continuous tape has no edge along", DriverBrotherQL, "QL-810W", "dk-22205", nil,
			LabelSpec{WidthMM: 62, HeightMM: 40, DPI: 300, Content: []RectMM{box(5, 0, 50, 40)}}, nil},
		// A first-generation LabelWriter reaches 56.9 mm of a 59 mm label,
		// from the guide.
		{"DYMO's far edge", DriverDymoLabelWriter, "LabelWriter 450", "30256", nil,
			LabelSpec{WidthMM: 59, HeightMM: 102, DPI: 300, Content: []RectMM{box(50, 10, 8, 10)}},
			[]string{"it prints only the first 56.9 mm of the label's 59 mm width"}},
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

func TestFitSaysWhereThePrinterPrints(t *testing.T) {
	box := func(x, y, w, h float64) RectMM { return RectMM{X: x, Y: y, Width: w, Height: h} }
	near := func(a, b RectMM) bool {
		return math.Abs(a.X-b.X) < 0.01 && math.Abs(a.Y-b.Y) < 0.01 && math.Abs(a.Width-b.Width) < 0.01 && math.Abs(a.Height-b.Height) < 0.01
	}
	// 90 × 29 mm on 29 mm die-cut labels, turned: the label's height runs
	// across the roll, where the QL prints the middle 25.9 mm, and its
	// width along it, where it prints the middle 83.9 mm.
	_, f, err := PlanLabel(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-11201"}, LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300,
		Content: []RectMM{box(10, 0.5, 20, 10), box(5, 5, 20, 19), box(1, 10, 10, 10)}})
	if err != nil {
		t.Fatal(err)
	}
	if !near(f.Printable, box(3.05, 1.55, 83.9, 25.9)) || f.EdgeMM != DriftMM {
		t.Errorf("on die-cut labels: printable %+v, edge %v", f.Printable, f.EdgeMM)
	}
	if !slices.Equal(f.Outside, []int{0, 2}) || len(f.NearEdge) != 0 {
		t.Errorf("on die-cut labels: outside %v, near the edge %v", f.Outside, f.NearEdge)
	}

	// 62 mm tape, as designed: its width across the roll, and no edges along it.
	_, f, err = PlanLabel(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-22205"}, LabelSpec{WidthMM: 62, HeightMM: 40, DPI: 300,
		Content: []RectMM{box(0, 0, 10, 10), box(5, 0, 50, 40)}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Printable.Y != 0 || f.Printable.Height != 40 || f.Printable.X < 1 || f.Printable.Width > 60 || f.EdgeMM != 0 {
		t.Errorf("on tape: printable %+v, edge %v", f.Printable, f.EdgeMM)
	}
	if !slices.Equal(f.Outside, []int{0}) || len(f.NearEdge) != 0 {
		t.Errorf("on tape: outside %v, near the edge %v", f.Outside, f.NearEdge)
	}

	// A Rollo prints all of a 2 × 1 in label, but may cut off what is
	// within a millimeter of its edges.
	_, f, err = PlanLabel(DriverTSPL, Settings{Model: "Rollo X1038", Media: "2x1"}, LabelSpec{WidthMM: 50.8, HeightMM: 25.4, DPI: 203,
		Content: []RectMM{box(5, 5, 20, 10), box(0.5, 5, 20, 10)}})
	if err != nil {
		t.Fatal(err)
	}
	if !near(f.Printable, box(0, 0, 50.8, 25.4)) || f.EdgeMM != DriftMM || len(f.Outside) != 0 || !slices.Equal(f.NearEdge, []int{1}) {
		t.Errorf("on a Rollo: %+v", f)
	}
}

// TestFeed sends labels that fit either way through the printer as their
// feed says, on die-cut and continuous rolls, on each driver; a feed one
// that fits only the other way refuses it, saying so.
func TestFeed(t *testing.T) {
	for _, c := range []struct {
		name, driver, model, media string
		size                       float64
		dpi                        int
	}{
		{"Brother round", DriverBrotherQL, "QL-810W", "dk-11207", 58, 300},
		{"Brother continuous", DriverBrotherQL, "QL-810W", "dk-22205", 62, 300},
		{"DYMO continuous", DriverDymoLabelWriter, "LabelWriter 450", "30270", 57, 300},
		{"TSPL continuous", DriverTSPL, "Rollo X1038", "continuous-4", 101.6, 203},
	} {
		for feed, rotate := range map[string]bool{"": false, "auto": false, "across": false, "along": true} {
			_, f, err := PlanLabel(c.driver, Settings{Model: c.model, Media: c.media}, LabelSpec{WidthMM: c.size, HeightMM: c.size, DPI: c.dpi, Feed: feed})
			if err != nil || f.Rotate != rotate {
				t.Errorf("%s, feed %q: rotate %v, %v", c.name, feed, f.Rotate, err)
			}
		}
	}
	// 29 × 90 mm fits a 29 mm die-cut roll only with its width across it.
	_, _, err := PlanLabel(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-11201"}, LabelSpec{WidthMM: 29, HeightMM: 90, DPI: 300, Feed: "along"})
	if !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "fed with its width along the roll") {
		t.Errorf("along: %v", err)
	}
	_, _, err = PlanLabel(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-22205"}, LabelSpec{WidthMM: 100, HeightMM: 62, DPI: 300, Feed: "across"})
	if !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "fed with its width across the roll, but the roll loaded") {
		t.Errorf("across: %v", err)
	}
}
