package printers

import (
	"errors"
	"image"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolm"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ptouch"
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
		{"Brother 4-inch die-cut", DriverBrotherQL, "QL-1100", "dk-11241", 102, 152, 300, false, true},
		{"Brother 4-inch die-cut landscape rotates", DriverBrotherQL, "QL-1110NWB", "dk-11241", 152, 102, 300, true, true},
		{"Brother 4-inch die-cut wrong length", DriverBrotherQL, "QL-1100", "dk-11240", 102, 152, 300, false, false},
		{"Brother 103 mm die-cut", DriverBrotherQL, "QL-1100", "dk-11247", 103, 164, 300, false, true},
		{"Brother 4-inch continuous", DriverBrotherQL, "QL-1115NWB", "dk-22243", 102, 400, 300, false, true},
		{"Brother 4-inch continuous longest", DriverBrotherQL, "QL-1100", "dk-22243", 102, 3000, 300, false, true},
		{"Brother 4-inch continuous too long", DriverBrotherQL, "QL-1100", "dk-22243", 102, 3010, 300, false, false},
		{"Brother QL-1100 shortest", DriverBrotherQL, "QL-1100", "dk-22243", 102, 25.5, 300, false, true},
		{"Brother QL-1100 too short", DriverBrotherQL, "QL-1100", "dk-22243", 102, 24, 300, false, false},
		{"Brother QL-1050 4-inch", DriverBrotherQL, "QL-1050", "dk-11240", 102, 51, 300, false, true},
		{"Brother QL-700 shorter tape", DriverBrotherQL, "QL-700", "dk-22210", 29, 13, 300, false, true},
		{"Brother QL-500 not so short", DriverBrotherQL, "QL-500", "dk-22210", 29, 13, 300, false, false},

		{"TD-2 die-cut", DriverBrotherTD, "TD-2020", "rd-51x26", 51, 26, 203, false, true},
		{"TD-2 at 300 DPI", DriverBrotherTD, "TD-2135N", "rd-58", 58, 80, 300, false, true},
		{"TD-2 wrong dpi", DriverBrotherTD, "TD-2020", "rd-51x26", 51, 26, 300, false, false},
		{"TD-4 4x6", DriverBrotherTD, "TD-4420DN", "rd-102x152", 101.6, 152.4, 203, false, true},
		{"TD-4 4x6 landscape rotates", DriverBrotherTD, "TD-4550DNWB", "rd-102x152", 152.4, 101.6, 300, true, true},
		{"TD-4 continuous too short", DriverBrotherTD, "TD-4410D", "rd-102", 102, 10, 203, false, false},

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

		{"ZPL 2-inch head", DriverZPL, "Zebra ZD410", "2x1", 50.8, 25.4, 203, false, true},
		{"ZPL 4-inch head", DriverZPL, "Zebra GK420d", "4x6", 101.6, 152.4, 203, false, true},
		{"ZPL 4-inch head, landscape rotates", DriverZPL, "Zebra GK420d", "4x6", 152.4, 101.6, 203, true, true},
		{"ZPL 300 DPI", DriverZPL, "Zebra ZD621 300 DPI", "4x6", 101.6, 152.4, 300, false, true},
		{"ZPL 600 DPI", DriverZPL, "Zebra ZT411 600 DPI", "2x1", 50.8, 25.4, 600, false, true},
		{"ZPL 300 DPI template on a 203 DPI printer", DriverZPL, "Zebra GK420d", "4x6", 101.6, 152.4, 300, false, false},
		{"ZPL continuous", DriverZPL, "Generic ZPL 8 in 203 DPI", "continuous-4", 101.6, 300, 203, false, true},
		{"EPL2 2-inch head", DriverEPL2, "Zebra LP 2824", "2x1", 50.8, 25.4, 203, false, true},

		{"DPL 4-inch head", DriverDPL, "Datamax I-Class 203 DPI", "4x6", 101.6, 152.4, 203, false, true},
		{"DPL 600 DPI reaches 2,040 dots", DriverDPL, "Datamax I-Class 600 DPI", "2x1", 50.8, 25.4, 600, false, true},
		{"SBPL 300 DPI", DriverSBPL, "SATO CL4NX 305 DPI", "4x6", 101.6, 152.4, 300, false, true},
		{"SBPL 600 DPI continuous too long", DriverSBPL, "SATO CL4NX 609 DPI", "continuous-4", 101.6, 500, 600, false, false},
		{"EZPL landscape rotates", DriverEZPL, "Godex G500", "4x6", 152.4, 101.6, 203, true, true},
		{"EZPL 300 DPI template on a 203 DPI printer", DriverEZPL, "Godex RT200", "4x6", 101.6, 152.4, 300, false, false},
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

// TestFourInchRolls checks the wide DK rolls on the 1,296-pin heads, and
// that a 62 mm model refuses them before any label is fitted.
func TestFourInchRolls(t *testing.T) {
	_, f, err := PlanLabel(DriverBrotherQL, Settings{Model: "QL-1100", Media: "dk-11241"}, LabelSpec{WidthMM: 102, HeightMM: 152, DPI: 300,
		Content: []RectMM{{X: 0.5, Y: 10, Width: 5, Height: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	// 1,164 of the head's 1,296 pins print, about 1.7 mm in from each side.
	if math.Abs(f.Printable.Width-98.552) > 0.01 || len(f.Outside) != 1 {
		t.Errorf("printable %+v, outside %v", f.Printable, f.Outside)
	}
	for _, model := range []string{"QL-810W", "QL-700", "QL-500"} {
		s := Settings{Model: model, Media: "dk-11241"}
		if _, _, err := PlanLabel(DriverBrotherQL, s, LabelSpec{WidthMM: 102, HeightMM: 152, DPI: 300}); err == nil || !strings.Contains(err.Error(), "does not take") {
			t.Errorf("%s planned a 4-inch roll: %v", model, err)
		}
		if err := Validate("P", DriverBrotherQL, tcp("ql.lan", 9100), s); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%s with a 4-inch roll: %v", model, err)
		}
	}
}

// TestTDCutterLength checks that a TD-4 cutting its labels holds them to
// the cutter's shortest, 20 mm, where it otherwise prints from 12 mm.
func TestTDCutterLength(t *testing.T) {
	s := Settings{Model: "TD-4410D", Media: "rd-102"}
	label := LabelSpec{WidthMM: 101.6, HeightMM: 15, DPI: 203}
	if _, _, err := PlanLabel(DriverBrotherTD, s, label); err != nil {
		t.Errorf("uncut: %v", err)
	}
	s.Options = []byte(`{"cut":true}`)
	if _, _, err := PlanLabel(DriverBrotherTD, s, label); !errors.Is(err, ErrDoesNotFit) || !strings.Contains(err.Error(), "20-") {
		t.Errorf("cut: %v", err)
	}
}

// TestPTouchPrintableArea checks the fit check against each tape width's
// printable area on a model of each head, from Brother's page sizes and
// tables of pins (section 2.3 of the P-touch references): a label as wide
// as the tape prints only on its middle pins, and content beyond them is
// warned of.
func TestPTouchPrintableArea(t *testing.T) {
	cases := []struct {
		model, media string
		width        float64
		dpi, pins    int
	}{
		{"PT-P750W", "tze-3.5", 3.5, 180, 24},
		{"PT-P750W", "tze-6", 6, 180, 32},
		{"PT-P750W", "tze-9", 9, 180, 50},
		{"PT-P750W", "tze-12", 12, 180, 70},
		{"PT-P750W", "tze-18", 18, 180, 112},
		{"PT-P750W", "tze-24", 24, 180, 128},
		{"PT-P700", "hse-5.8", 5.8, 180, 28},
		{"PT-E550W", "hse-21", 21, 180, 120},
		{"PT-P950NW", "tze-3.5", 3.5, 360, 48},
		{"PT-P950NW", "tze-6", 6, 360, 64},
		{"PT-P950NW", "tze-9", 9, 360, 106},
		{"PT-P950NW", "tze-12", 12, 360, 150},
		{"PT-P950NW", "tze-18", 18, 360, 234},
		{"PT-P950NW", "tze-24", 24, 360, 320},
		{"PT-P910BT", "tze-36", 36, 360, 454},
		{"PT-P900W", "hse-31", 31, 360, 360},
	}
	for _, c := range cases {
		reach := float64(c.pins) / float64(c.dpi) * 25.4
		// Landscape, as tape labels are: 60 mm along the tape, turned.
		label := LabelSpec{WidthMM: 60, HeightMM: c.width, DPI: c.dpi,
			Content: []RectMM{{X: 5, Y: 0, Width: 10, Height: c.width}, {X: 20, Y: (c.width - reach) / 2, Width: 10, Height: reach}}}
		_, f, err := PlanLabel(DriverBrotherPTouch, Settings{Model: c.model, Media: c.media}, label)
		if err != nil {
			t.Errorf("%s on %s: %v", c.media, c.model, err)
			continue
		}
		if !f.Rotate || math.Abs(f.Printable.Height-reach) > 0.01 || math.Abs(f.Printable.Y-(c.width-reach)/2) > 0.01 {
			t.Errorf("%s on %s: printable %+v, want %.2f mm across", c.media, c.model, f.Printable, reach)
		}
		// The full-width box reaches the unprinted edges, unless the head
		// spans the tape; the box on the printable pins never does.
		wantOutside := []int{0}
		if c.width-reach < 0.05 {
			wantOutside = nil
		}
		if !slices.Equal(f.Outside, wantOutside) {
			t.Errorf("%s on %s: outside %v", c.media, c.model, f.Outside)
		}
	}
	// Lengths: 4.4 mm to 7,086 lines (just under 1000 mm) at 180 DPI, tube
	// to 500 mm.
	for _, c := range []struct {
		model, media string
		length       float64
		fits         bool
	}{
		{"PT-P750W", "tze-12", 4.5, true},
		{"PT-P750W", "tze-12", 4, false},
		{"PT-P750W", "tze-12", 999, true},
		{"PT-P750W", "hse-11.7", 600, false},
		{"PT-P950NW", "tze-12", 4, true},
		{"PT-P950NW", "hse-11.7", 500, true},
	} {
		m, _ := ptouch.LookupMedia(c.media)
		mo, _ := ptouch.LookupModel(c.model)
		_, _, err := PlanLabel(DriverBrotherPTouch, Settings{Model: c.model, Media: c.media}, LabelSpec{WidthMM: c.length, HeightMM: m.WidthMM, DPI: mo.DPI})
		if (err == nil) != c.fits {
			t.Errorf("%g mm on %s: %v", c.length, c.media, err)
		}
	}
}

// TestPTouchPlan prints through the registry with the driver's options.
func TestPTouchPlan(t *testing.T) {
	s := Settings{Model: "PT-P750W", Media: "tze-12", Options: []byte(`{"cut_after":"last_label","half_cut":true,"mirror":true,"margin_mm":4}`)}
	plan, f, err := PlanLabel(DriverBrotherPTouch, s, LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 180})
	if err != nil {
		t.Fatal(err)
	}
	label := blankLabel(40, 12, 180)
	stream, err := plan.Encode([]*image.Gray{label, label})
	if err != nil {
		t.Fatal(err)
	}
	mo, _ := ptouch.LookupModel("PT-P750W")
	pages, err := mo.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	// Turned, 40 mm along the tape is 283 lines; 4 mm is 28 dots.
	p := pages[0]
	if !f.Rotate || len(pages) != 2 || len(p.Lines) != 283 || p.Various != 0x80 || p.Advanced != 0x0C || p.MarginDots != 28 || p.WidthMM != 12 {
		t.Errorf("%d pages, the first %+v with %d lines", len(pages), p, len(p.Lines))
	}
}

// TestDymoLabelManagerPrintableArea checks the fit check against each tape
// width's printable height on each model, in dots from DYMO's imageable
// areas: a label as tall as the tape prints only on its middle dots, and
// content beyond them is warned of.
func TestDymoLabelManagerPrintableArea(t *testing.T) {
	cases := []struct {
		model, media string
		width        float64
		dpi, dots    int
	}{
		{"LabelManager PnP", "d1-6", 6, 180, 30},
		{"LabelManager PnP", "d1-9", 9, 180, 50},
		{"LabelManager PnP", "d1-12", 12, 180, 58},
		{"LabelManager 280", "d1-6", 6, 180, 30},
		{"LabelManager 280", "d1-9", 9, 180, 50},
		{"LabelManager 280", "d1-12", 12, 180, 58},
		{"LabelManager 420P", "d1-6", 6, 180, 30},
		{"LabelManager 420P", "d1-9", 9, 180, 50},
		{"LabelManager 420P", "d1-12", 12, 180, 72},
		{"LabelManager 420P", "d1-19", 19, 180, 100},
		{"LabelManager Wireless PnP", "d1-6", 6, 300, 60},
		{"LabelManager Wireless PnP", "d1-9", 9, 300, 83},
		{"LabelManager Wireless PnP", "d1-12", 12, 300, 106},
		{"LabelManager Wireless PnP", "d1-19", 19, 300, 166},
		{"LabelManager Wireless PnP", "d1-24", 24, 300, 213},
		{"LabelManager 500TS", "d1-6", 6, 300, 60},
		{"LabelManager 500TS", "d1-9", 9, 300, 83},
		{"LabelManager 500TS", "d1-12", 12, 300, 106},
		{"LabelManager 500TS", "d1-19", 19, 300, 166},
		{"LabelManager 500TS", "d1-24", 24, 300, 213},
	}
	for _, c := range cases {
		reach := float64(c.dots) / float64(c.dpi) * 25.4
		// Landscape, as tape labels are: 60 mm along the tape, turned.
		label := LabelSpec{WidthMM: 60, HeightMM: c.width, DPI: c.dpi,
			Content: []RectMM{{X: 5, Y: 0, Width: 10, Height: c.width}, {X: 20, Y: (c.width - reach) / 2, Width: 10, Height: reach}}}
		_, f, err := PlanLabel(DriverDymoLabelManager, Settings{Model: c.model, Media: c.media}, label)
		if err != nil {
			t.Errorf("%s on %s: %v", c.media, c.model, err)
			continue
		}
		if !f.Rotate || math.Abs(f.Printable.Height-reach) > 0.01 || math.Abs(f.Printable.Y-(c.width-reach)/2) > 0.01 {
			t.Errorf("%s on %s: printable %+v, want %.2f mm across", c.media, c.model, f.Printable, reach)
		}
		if !slices.Equal(f.Outside, []int{0}) {
			t.Errorf("%s on %s: outside %v", c.media, c.model, f.Outside)
		}
	}
	// Tape a model does not take is refused, as is a label longer than
	// DYMO's tallest page, 12,800 points; a short one is lengthened.
	for _, c := range []struct {
		model, media string
		length       float64
		fits         bool
	}{
		{"LabelManager PnP", "d1-19", 40, false},
		{"LabelManager 420P", "d1-24", 40, false},
		{"LabelManager PnP", "d1-12", 2, true},
		{"LabelManager PnP", "d1-12", 4515, true},
		{"LabelManager Wireless PnP", "d1-12", 4520, false},
	} {
		m, _ := dymolm.LookupMedia(c.media)
		mo, _ := dymolm.LookupModel(c.model)
		_, _, err := PlanLabel(DriverDymoLabelManager, Settings{Model: c.model, Media: c.media}, LabelSpec{WidthMM: c.length, HeightMM: m.WidthMM, DPI: mo.DPI})
		if (err == nil) != c.fits {
			t.Errorf("%g mm on %s on the %s: %v", c.length, c.media, c.model, err)
		}
	}
}

// TestDymoLabelManagerPlan prints through the registry with the driver's
// options.
func TestDymoLabelManagerPlan(t *testing.T) {
	s := Settings{Model: "LabelManager Wireless PnP", Media: "d1-12", Options: []byte(`{"tape_type":"black_on_yellow","cut_after":"last_label","margin_mm":15}`)}
	plan, f, err := PlanLabel(DriverDymoLabelManager, s, LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 300})
	if err != nil {
		t.Fatal(err)
	}
	label := blankLabel(40, 12, 300)
	stream, err := plan.Encode([]*image.Gray{label, label})
	if err != nil {
		t.Fatal(err)
	}
	mo, _ := dymolm.LookupModel("LabelManager Wireless PnP")
	job, err := mo.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	// Turned, 40 mm along the tape is 472 lines; 15 mm is 177. Cut once,
	// at the end, with a cut mark between: margins before and after each,
	// and the mark. The first margin is fed less the tape between head and
	// cutter, which is fed at the end.
	if want := 2*472 + 4*177 + 1; !f.Rotate || job.TapeType != 4 || len(job.Cuts) != 1 || len(job.Lines) != want {
		t.Errorf("tape type %d, cuts %v, %d lines, want %d", job.TapeType, job.Cuts, len(job.Lines), want)
	}
	if _, _, err := PlanLabel(DriverDymoLabelManager, Settings{Model: "LabelManager 420P", Media: "d1-12", Options: []byte(`{"cut_after":"last_label"}`)},
		LabelSpec{WidthMM: 40, HeightMM: 12, DPI: 180}); err == nil {
		t.Error("the 420P took cut_after")
	}
}
