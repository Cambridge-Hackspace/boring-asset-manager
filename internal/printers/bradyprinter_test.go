package printers

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brady"
)

func TestBradyValidate(t *testing.T) {
	dieCut := &Stock{ID: "11111111-2222-3333-4444-555555555555", Name: "M6-11-427", WidthMM: 38.1, LengthMM: 19.05, GapMM: 3}
	tape := &Stock{ID: "11111111-2222-3333-4444-666666666666", Name: "1 in vinyl", WidthMM: 25.4}
	wide := &Stock{ID: "11111111-2222-3333-4444-777777777777", Name: "4 in", WidthMM: 101.6}
	short := &Stock{ID: "11111111-2222-3333-4444-888888888888", Name: "tiny", WidthMM: 12, LengthMM: 6}
	for _, c := range []struct {
		why, model, media, options string
		stock                      *Stock
		ok                         bool
	}{
		{"die-cut labels", "M611", dieCut.ID, "", dieCut, true},
		{"continuous tape, options", "M611", tape.ID, `{"cut_after":"last_label","part":"M6C-1000-595-WT"}`, tape, true},
		{"the printer's cut", "M611", tape.ID, `{"cut_after":"printer"}`, tape, true},
		{"no cartridge", "M611", "", "", nil, false},
		{"a built-in roll", "M611", "2x1", "", nil, false},
		{"wider than the M611 takes", "M611", wide.ID, "", wide, false},
		{"shorter than it feeds", "M611", short.ID, "", short, false},
		{"another model", "M610", tape.ID, "", tape, false},
		{"cut", "M611", tape.ID, `{"cut_after":"never"}`, tape, false},
		{"part", "M611", tape.ID, `{"part":"M6;rm"}`, tape, false},
		{"an unknown option", "M611", tape.ID, `{"density":3}`, tape, false},
	} {
		s := Settings{Model: c.model, Media: c.media, Stock: c.stock, Options: json.RawMessage(c.options)}
		err := Validate("Brady", DriverBrady, Connection{Type: ConnTCP, Address: "m611.lan"}, s)
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.why, err)
		}
		if !c.ok && !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%s: got %v, want ErrInvalidPrinter", c.why, err)
		}
	}

	d, _ := Lookup(DriverBrady)
	info := d.Info()
	if info.ModelDPI["M611"] != 300 || len(info.Media) != 0 || info.CustomStock == nil || info.CustomStock.MaxWidthMM["M611"] != 50.8 ||
		info.CustomStock.MaxLengthMM != 1016 || len(info.Options) != 2 || info.Options[0].Default != "each_label" {
		t.Errorf("info %+v", info)
	}
}

// A label printed on a Brady reaches the printer as a job it can be read
// back from: its header, then each page's BMP.
func TestBradyLabelsReachThePrinter(t *testing.T) {
	stock := &Stock{ID: "11111111-2222-3333-4444-555555555555", Name: "M6-11-427", WidthMM: 38.1, LengthMM: 19.05, GapMM: 3}
	s := Settings{Model: "M611", Media: stock.ID, Stock: stock, Options: json.RawMessage(`{"part":" M6-11-427 "}`)}
	d, _ := Lookup(DriverBrady)
	g, err := d.Geometry(s)
	if err != nil || g.DPI != 300 || g.WidthMM != 38.1 || !g.DieCut() {
		t.Fatalf("geometry %+v %v", g, err)
	}
	// A label designed upright is turned to run its width along the
	// cartridge, as fits.
	plan, err := planWith(d, s, LabelSpec{WidthMM: 19.05, HeightMM: 38.1, DPI: 300})
	if err != nil {
		t.Fatal(err)
	}
	label := blankLabel(19.05, 38.1, 300)
	label.Pix[0] = 0 // the top left dot is dark
	job, err := plan.Encode([]*image.Gray{label, label})
	if err != nil {
		t.Fatal(err)
	}

	fp := newFakePrinter(t)
	if err := NewDispatcher(nil).Send(context.Background(), local(fp.port()), job); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && got == nil; time.Sleep(5 * time.Millisecond) {
		fp.mu.Lock()
		if len(fp.jobs) == 1 {
			got = fp.jobs[0]
		}
		fp.mu.Unlock()
	}
	j, err := brady.DecodeJob(got)
	if err != nil {
		t.Fatal(err)
	}
	if j.Header.NumberOfPages != 2 || j.Header.SubstratePart != "M6-11-427" || j.Header.PostPrint != "Cut;1" {
		t.Fatalf("header %+v", j.Header)
	}
	img := j.Images[1]
	// Turned counterclockwise, the top left dot is at the bottom left.
	if img.Rect.Dx() != 450 || img.Rect.Dy() != 225 || img.Pix[(img.Rect.Dy()-1)*img.Stride] != 0 || img.Pix[0] != 0xFF {
		t.Fatalf("page %v", img.Rect)
	}
	if p := j.Pages[0]; p.LabelWidth != 1500 || p.LabelHeight != 750 {
		t.Errorf("page %+v", p)
	}
}
