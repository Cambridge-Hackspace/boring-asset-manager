package printers

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
)

// A ZPL printer writes tags only when set to, and an EPL2 one never.
func TestZPLPrintersWriteTagsWhenSetTo(t *testing.T) {
	label := image.NewGray(image.Rect(0, 0, 406, 203))
	epcs := []string{"48532D313035"}
	for _, c := range []struct {
		driver, model, options string
		tags                   bool
	}{
		{DriverZPL, "Zebra ZT411 203 DPI", `{"rfid":true,"rfid_tries":2,"rfid_on_failure":"pause"}`, true},
		{DriverZPL, "Zebra ZT411 203 DPI", `{"rfid":false,"rfid_tries":2,"rfid_on_failure":"pause"}`, false},
		{DriverZPL, "Zebra ZT411 203 DPI", `{}`, false},
		{DriverEPL2, "Zebra LP 2844", `{}`, false},
	} {
		d, _ := Lookup(c.driver)
		p, err := planWith(d, Settings{Model: c.model, Media: "2x1", Options: json.RawMessage(c.options)}, LabelSpec{WidthMM: 50.8, HeightMM: 25.4, DPI: 203})
		if err != nil {
			t.Fatalf("%s %s: %v", c.driver, c.options, err)
		}
		tp, ok := p.(TagPlan)
		if !ok {
			t.Fatalf("%s: not a TagPlan", c.driver)
		}
		stream, err := tp.EncodeTagged([]*image.Gray{label}, epcs)
		if err != nil {
			t.Fatal(err)
		}
		wrote := strings.Contains(string(stream), "^RS8,,,2,P\n^RFW,H,,,A^FD48532D313035^FS")
		if tp.EncodesTags() != c.tags || wrote != c.tags || strings.Contains(string(stream), "^RF") != c.tags {
			t.Errorf("%s %s: EncodesTags %v, wrote %v", c.driver, c.options, tp.EncodesTags(), wrote)
		}
	}
	d, _ := Lookup(DriverZPL)
	for _, o := range []string{`{"rfid_tries":0}`, `{"rfid_tries":11}`, `{"rfid_on_failure":"void"}`, `{"rfid":"yes"}`} {
		if err := d.Validate(Settings{Model: "Zebra ZT411 203 DPI", Media: "2x1", Options: json.RawMessage(o)}); err == nil {
			t.Errorf("%s was taken", o)
		}
	}
}
