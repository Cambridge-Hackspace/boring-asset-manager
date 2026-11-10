package labels

import (
	"reflect"
	"strings"
	"testing"
)

func TestPalettes(t *testing.T) {
	for _, c := range []struct {
		inks []string
		want Palette
	}{
		{nil, PaletteBW},
		{[]string{InkBlack, InkWhite}, PaletteBW},
		{[]string{InkBlack, InkRed}, PaletteBWR},
		{[]string{InkYellow}, PaletteBWY},
		{[]string{InkRed, InkYellow}, PaletteBWRY},
		{[]string{InkGreen}, PaletteBWRYGB},
		{[]string{InkBlue, InkRed}, PaletteBWRYGB},
		{[]string{InkOrange}, PaletteBWRYGBO},
	} {
		if got := PaletteFor(c.inks); got != c.want {
			t.Errorf("PaletteFor(%v) = %s, want %s", c.inks, got, c.want)
		}
	}
	for _, p := range Palettes {
		if !p.Valid() || !p.Has(InkBlack) || !p.Has(InkWhite) {
			t.Errorf("%s: every palette has black and white", p)
		}
		if len(p.Colors()) != len(p.Inks()) {
			t.Errorf("%s: a color for each ink", p)
		}
	}
	if Palette("rgb").Valid() || !reflect.DeepEqual(Palette("rgb").Inks(), []string{InkBlack, InkWhite}) {
		t.Error("a palette that is not one is black and white")
	}
	if got := PaletteBWR.Missing([]string{InkOrange, InkBlack, InkRed, InkYellow}); !reflect.DeepEqual(got, []string{InkYellow, InkOrange}) {
		t.Errorf("missing from bwr: %v", got)
	}
	if PaletteBWR.Resolve(InkYellow) != InkBlack || PaletteBWR.Resolve(InkRed) != InkRed {
		t.Error("an ink a palette lacks resolves to black")
	}
	if got := PaletteBWR.Describe(); got != "black, white and red" {
		t.Errorf("described as %q", got)
	}
}

func TestTemplateInks(t *testing.T) {
	tpl, err := Parse([]byte(`{"name":"Inks","units":"mm","width":60,"height":30,"dpi":300,"layering":"list","elements":[
		{"type":"text","x":1,"y":1,"width":20,"height":5,"font_size":10,"content_binding":"plain"},
		{"type":"text","x":1,"y":7,"width":20,"height":5,"font_size":10,"fill":"red","content_binding":"red"},
		{"type":"generator","generator_type":"qr","x":30,"y":1,"width":14,"height":14,"fill":"blue"},
		{"type":"shape","shape":"rectangle","x":1,"y":14,"width":20,"height":10,"stroke":"orange","fill":"white"},
		{"type":"static_image","x":45,"y":1,"width":10,"height":10,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tpl.Inks(), []string{InkBlack, InkWhite, InkRed, InkBlue, InkOrange}; !reflect.DeepEqual(got, want) {
		t.Errorf("inks %v, want %v", got, want)
	}
}

func TestInksValidated(t *testing.T) {
	doc := func(element string) string {
		return `{"name":"Inks","units":"mm","width":60,"height":30,"dpi":300,"elements":[` + element + `]}`
	}
	for _, ok := range []string{
		`{"type":"text","x":1,"y":1,"width":20,"height":5,"font_size":10,"fill":"yellow","content_binding":"x"}`,
		`{"type":"shape","shape":"ellipse","x":1,"y":1,"width":20,"height":5,"stroke":"green","fill":"orange"}`,
		`{"type":"generator","generator_type":"qr","x":1,"y":1,"width":14,"height":14,"fill":"red"}`,
	} {
		if _, err := Parse([]byte(doc(ok))); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, c := range []struct{ element, path string }{
		{`{"type":"text","x":1,"y":1,"width":20,"height":5,"font_size":10,"fill":"purple","content_binding":"x"}`, "elements[0].fill"},
		{`{"type":"shape","shape":"ellipse","x":1,"y":1,"width":20,"height":5,"stroke":"#ff0000"}`, "elements[0].stroke"},
		{`{"type":"generator","generator_type":"qr","x":1,"y":1,"width":14,"height":14,"fill":"white"}`, "elements[0].fill"},
		{`{"type":"generator","generator_type":"qr","x":1,"y":1,"width":14,"height":14,"fill":"none"}`, "elements[0].fill"},
		{`{"type":"static_image","x":1,"y":1,"width":14,"height":14,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","fill":"red"}`, "elements[0].fill"},
	} {
		_, err := Parse([]byte(doc(c.element)))
		if err == nil || !strings.Contains(err.Error(), c.path) {
			t.Errorf("%s: want a problem at %s, got %v", c.element, c.path, err)
		}
	}
}
