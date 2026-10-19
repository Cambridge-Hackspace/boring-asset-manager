package labels

import (
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// readmeExample is the template document from README §4.1.
const readmeExample = `{
  "id": "brother_ql_29x90",
  "name": "Standard Die-Cut 29mm x 90mm",
  "units": "mm",
  "width": 90.0,
  "height": 29.0,
  "dpi": 300,
  "elements": [
    {"type": "generator", "generator_type": "qr", "error_correction": "M",
     "x": 3.0, "y": 3.0, "width": 23.0, "height": 23.0,
     "content_binding": "{{base_url}}/a/{{asset.id}}"},
    {"type": "text", "x": 28.0, "y": 5.0, "width": 58.0, "height": 6.0,
     "font_size": 14, "font_weight": "bold", "content_binding": "{{asset.name}}"},
    {"type": "text", "x": 28.0, "y": 12.0, "width": 58.0, "height": 4.0,
     "font_size": 9, "font_family": "mono", "content_binding": "{{field.location}}"},
    {"type": "static_image", "x": 75.0, "y": 18.0, "width": 10.0, "height": 8.0,
     "image_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"}
  ]
}`

func TestParseReadmeExample(t *testing.T) {
	tpl, err := Parse([]byte(readmeExample))
	if err != nil {
		t.Fatalf("README example rejected: %v", err)
	}
	name := tpl.Elements[1]
	if name.FontWeight != WeightBold || name.FontFamily != FamilySans || name.Align != AlignLeft || name.MinFontSize != 6 {
		t.Errorf("text defaults not applied: %+v", name)
	}
	if small := tpl.Elements[2]; small.MinFontSize != 6 {
		t.Errorf("min_font_size = %g, want 6", small.MinFontSize)
	}
	if got := tpl.FieldKeys(); !reflect.DeepEqual(got, []string{"location"}) {
		t.Errorf("FieldKeys() = %v", got)
	}
	if got := tpl.ImageIDs(); !reflect.DeepEqual(got, []string{"7c9e6679-7425-40de-944b-e07fc1f90ae7"}) {
		t.Errorf("ImageIDs() = %v", got)
	}
}

func TestMinFontSizeDefaultsBelowSmallFonts(t *testing.T) {
	tpl := validTemplate()
	tpl.Elements = []Element{{Type: ElementText, Width: 10, Height: 5, FontSize: 4, ContentBinding: "x"}}
	tpl.Normalize()
	if tpl.Elements[0].MinFontSize != 4 {
		t.Errorf("min_font_size = %g, want 4", tpl.Elements[0].MinFontSize)
	}
}

func validTemplate() *Template {
	return &Template{ID: "t", Name: "T", Units: UnitsMM, Width: 50, Height: 20, DPI: 300, Elements: []Element{}}
}

func problemPaths(t *testing.T, err error) []string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}
	var paths []string
	for _, p := range ve.Problems {
		paths = append(paths, p.Path)
	}
	return paths
}

func TestValidateReportsEveryProblemWithItsPath(t *testing.T) {
	five := 5
	cases := []struct {
		name   string
		mutate func(*Template)
		paths  []string
	}{
		{"bad id", func(t *Template) { t.ID = "Has Spaces" }, []string{"id"}},
		{"blank name", func(t *Template) { t.Name = "  " }, []string{"name"}},
		{"units", func(t *Template) { t.Units = "cm" }, []string{"units"}},
		{"dpi", func(t *Template) { t.DPI = 72 }, []string{"dpi"}},
		{"zero size", func(t *Template) { t.Width, t.Height = 0, -1 }, []string{"height", "width"}},
		{"huge canvas", func(t *Template) { t.Units, t.Width = UnitsIn, 40 }, []string{"width"}},
		{"unknown element", func(t *Template) {
			t.Elements = []Element{{Type: "barcode", Width: 1, Height: 1}}
		}, []string{"elements[0].type"}},
		{"outside canvas", func(t *Template) {
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", X: 40, Y: 15, Width: 20, Height: 10}}
		}, []string{"elements[0].height", "elements[0].width"}},
		{"negative position", func(t *Template) {
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", X: -1, Width: 5, Height: 5}}
		}, []string{"elements[0].x"}},
		{"generator type", func(t *Template) {
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "ean13", Width: 5, Height: 5}}
		}, []string{"elements[0].generator_type"}},
		{"error correction", func(t *Template) {
			t.Elements = []Element{
				{Type: ElementGenerator, GeneratorType: "micro_qr", ErrorCorrection: "H", Width: 5, Height: 5},
				{Type: ElementGenerator, GeneratorType: "code128", ErrorCorrection: "L", Width: 5, Height: 5},
			}
		}, []string{"elements[0].error_correction", "elements[1].error_correction"}},
		{"quiet zone", func(t *Template) {
			big := 50
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", QuietZone: &big, Width: 5, Height: 5}}
		}, []string{"elements[0].quiet_zone"}},
		{"foreign properties", func(t *Template) {
			t.Elements = []Element{
				{Type: ElementGenerator, GeneratorType: "qr", FontSize: 9, Width: 5, Height: 5},
				{Type: ElementText, FontSize: 9, MinFontSize: 6, FontWeight: "normal", FontFamily: "sans", Align: "left",
					ContentBinding: "x", QuietZone: &five, Width: 5, Height: 5},
				{Type: ElementStaticImage, ImageID: "7c9e6679-7425-40de-944b-e07fc1f90ae7", ContentBinding: "x", Width: 5, Height: 5},
			}
		}, []string{"elements[0].font_size", "elements[1].quiet_zone", "elements[2].content_binding"}},
		{"text styling", func(t *Template) {
			t.Elements = []Element{{Type: ElementText, FontSize: 9, MinFontSize: 12, FontWeight: "heavy", FontFamily: "serif",
				Align: "justify", ContentBinding: "x", Width: 5, Height: 5}}
		}, []string{"elements[0].align", "elements[0].font_family", "elements[0].font_weight", "elements[0].min_font_size"}},
		{"text needs content", func(t *Template) {
			t.Elements = []Element{{Type: ElementText, FontSize: 9, MinFontSize: 6, FontWeight: "normal", FontFamily: "sans",
				Align: "left", Width: 5, Height: 5}}
		}, []string{"elements[0].content_binding"}},
		{"bad binding", func(t *Template) {
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", ContentBinding: "{{asset.serial}}", Width: 5, Height: 5}}
		}, []string{"elements[0].content_binding"}},
		{"image id", func(t *Template) {
			t.Elements = []Element{{Type: ElementStaticImage, ImageID: "../../etc/passwd", Width: 5, Height: 5}}
		}, []string{"elements[0].image_id"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tpl := validTemplate()
			c.mutate(tpl)
			got := problemPaths(t, tpl.Validate())
			if !reflect.DeepEqual(got, c.paths) {
				t.Errorf("problem paths = %v, want %v", got, c.paths)
			}
		})
	}
}

func TestElementsFlushWithTheCanvasEdgeAreAllowed(t *testing.T) {
	tpl := validTemplate()
	tpl.Width, tpl.Height = 29, 90
	tpl.Elements = []Element{{Type: ElementGenerator, GeneratorType: "micro_qr", X: 0.1 + 0.2, Width: 29 - 0.3, Height: 90}}
	if err := tpl.Validate(); err != nil {
		t.Errorf("element ending exactly at the edge rejected: %v", err)
	}
}

func TestParseRejectsMalformedDocuments(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown property": `{"id":"t","name":"T","units":"mm","width":1,"height":1,"dpi":300,"elements":[],"colour":"red"}`,
		"unknown element property": `{"id":"t","name":"T","units":"mm","width":10,"height":10,"dpi":300,
			"elements":[{"type":"text","x":0,"y":0,"width":1,"height":1,"font_size":9,"content_binding":"x","colour":"red"}]}`,
		"not json":      `{"id":`,
		"trailing data": `{"id":"t","name":"T","units":"mm","width":1,"height":1,"dpi":300} {}`,
		"wrong type":    `{"id":"t","name":"T","units":"mm","width":"wide","height":1,"dpi":300}`,
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseBinding(t *testing.T) {
	cases := []struct {
		in   string
		refs []Ref
	}{
		{"plain text", nil},
		{"", nil},
		{"{{asset.id}}", []Ref{{Kind: RefAssetID}}},
		{"{{ asset.name }}!", []Ref{{Kind: RefAssetName}}},
		{"{{base_url}}/a/{{asset.id}}", []Ref{{Kind: RefBaseURL}, {Kind: RefAssetID}}},
		{"{{asset.code.micro_qr}}", []Ref{{Kind: RefAssetCode, Arg: "micro_qr"}}},
		{"Room {{field.room_no}}", []Ref{{Kind: RefField, Arg: "room_no"}}},
		{"a }} b", nil},
	}
	for _, c := range cases {
		b, err := ParseBinding(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(b.Refs(), c.refs) {
			t.Errorf("%q: refs %v, want %v", c.in, b.Refs(), c.refs)
		}
	}

	for _, bad := range []string{"{{asset.id", "{{}}", "{{asset.serial}}", "{{asset.code.ean13}}", "{{field.Bad-Key}}", "{{field.}}"} {
		if _, err := ParseBinding(bad); !errors.Is(err, ErrInvalidBinding) {
			t.Errorf("%q: got %v, want ErrInvalidBinding", bad, err)
		}
	}
}

func TestResolve(t *testing.T) {
	b, err := ParseBinding("{{base_url}}/a/{{asset.id}} {{asset.name}} [{{asset.code.qr}}] [{{field.location}}] [{{field.missing}}] [{{asset.code.code128}}]")
	if err != nil {
		t.Fatal(err)
	}
	got := b.Resolve(Data{
		AssetID:   "1234",
		AssetName: "Drill",
		BaseURL:   "https://bam.example/",
		Codes:     map[string]string{"qr": "Q-1"},
		Fields:    map[string]string{"location": "Shelf 3"},
	})
	want := "https://bam.example/a/1234 Drill [Q-1] [Shelf 3] [] []"
	if got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestGeneratorBindingDefaultsToAssetCode(t *testing.T) {
	e := Element{Type: ElementGenerator, GeneratorType: "micro_qr"}
	b, err := e.Binding()
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Resolve(Data{Codes: map[string]string{"micro_qr": "BAM-7"}}); got != "BAM-7" {
		t.Errorf("default binding resolved to %q", got)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	tpl := validTemplate()
	tpl.DPI = 1
	err := tpl.Validate()
	if err == nil || !strings.Contains(err.Error(), "dpi: must be between 150 and 600") {
		t.Errorf("unexpected message: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(readmeExample))
	f.Add([]byte(`{"id":"t","name":"T","units":"in","width":1,"height":1,"dpi":150,"elements":[]}`))
	f.Fuzz(func(t *testing.T, doc []byte) {
		tpl, err := Parse(doc)
		if err != nil {
			var ve *ValidationError
			if !errors.As(err, &ve) || len(ve.Problems) == 0 {
				t.Fatalf("Parse returned a non-validation error: %v", err)
			}
			return
		}
		// Whatever Parse accepts must stay valid.
		if err := tpl.Validate(); err != nil {
			t.Fatalf("accepted template fails revalidation: %v", err)
		}
	})
}

func TestAssetDetailPlaceholders(t *testing.T) {
	b, err := ParseBinding("{{asset.categories}}|{{ asset.location }}|{{asset.condition}}|{{stock.quantity}} {{stock.unit}}|{{loan.borrower}} until {{loan.due}}")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(b.Refs()); n != 7 {
		t.Errorf("%d placeholders", n)
	}
	d := Data{Categories: "Tools, Drills", Location: "Shelf 2 › Bin A3", Condition: "Working", Quantity: "40", Unit: "pcs", Borrower: "Ada", Due: "2026-10-20"}
	if got := b.Resolve(d); got != "Tools, Drills|Shelf 2 › Bin A3|Working|40 pcs|Ada until 2026-10-20" {
		t.Errorf("resolved %q", got)
	}
	// An asset with none of them leaves them empty.
	if got := b.Resolve(Data{}); got != "||| | until " {
		t.Errorf("resolved %q", got)
	}
	for _, bad := range []string{"{{stock}}", "{{loan.lender}}", "{{asset.locations}}"} {
		if _, err := ParseBinding(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestScanURL(t *testing.T) {
	b, err := ParseBinding("{{scan_url}}")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Resolve(Data{AssetID: "1234", BaseURL: "https://bam.example/"}); got != "https://bam.example/scan/1234" {
		t.Errorf("scan_url = %q", got)
	}
}

func TestValidateAdaptiveTemplates(t *testing.T) {
	adaptive := func() *Template {
		tpl := validTemplate()
		tpl.Sizing = SizingAdaptive
		tpl.Adapt = &Adapt{MinWidth: 25, MaxWidth: 100, MinHeight: 10, MaxHeight: 40}
		tpl.Normalize()
		return tpl
	}
	withAlternative := adaptive()
	withAlternative.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, MinModuleMM: 0.3,
		WhenTooSmall: TooSmallAlternative, Alternative: &Element{Type: ElementText, ContentBinding: "{{field.serial}}", FontSize: 8}}}
	withAlternative.Normalize()
	if err := withAlternative.Validate(); err != nil || withAlternative.Elements[0].Alternative.FontWeight != WeightNormal {
		t.Errorf("an alternative: %v, %+v", err, withAlternative.Elements[0].Alternative)
	}
	if keys := withAlternative.FieldKeys(); !reflect.DeepEqual(keys, []string{"serial"}) {
		t.Errorf("an alternative's fields: %v", keys)
	}
	if tpl := adaptive(); tpl.Validate() != nil || tpl.Adapt.ContinuousLength != LengthProportional {
		t.Errorf("a valid adaptive template: %v, %+v", tpl.Validate(), tpl.Adapt)
	}
	fixed := validTemplate()
	fixed.Sizing = SizingFixed
	if fixed.Normalize(); fixed.Sizing != "" || fixed.Validate() != nil {
		t.Errorf("a template fixed by name: %q, %v", fixed.Sizing, fixed.Validate())
	}
	cases := []struct {
		name   string
		mutate func(*Template)
		paths  []string
	}{
		{"unknown sizing", func(t *Template) { t.Sizing = "stretchy" }, []string{"sizing"}},
		{"no range", func(t *Template) { t.Adapt = nil }, []string{"adapt"}},
		{"a range on a fixed template", func(t *Template) { t.Sizing = "" }, []string{"adapt"}},
		{"a range without its own size", func(t *Template) { t.Adapt.MinWidth = 60 }, []string{"adapt.min_width"}},
		{"a bad range", func(t *Template) { t.Adapt.MinHeight = 0 }, []string{"adapt.min_height"}},
		{"too wide", func(t *Template) { t.Adapt.MaxWidth = 2000 }, []string{"adapt.max_width"}},
		{"continuous length", func(t *Template) { t.Adapt.ContinuousLength = "stretch" }, []string{"adapt.continuous_length"}},
		{"anchors", func(t *Template) {
			t.Elements = []Element{{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, AnchorX: "left", AnchorY: AnchorEnd}}
		}, []string{"elements[0].anchor_x"}},
		{"limits", func(t *Template) {
			t.Elements = []Element{
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, MinSize: 4, MaxSize: 3, MinModuleMM: 9},
				{Type: ElementText, ContentBinding: "x", FontSize: 9, MinFontSize: 6, FontWeight: "normal", FontFamily: "sans", Align: "left", Width: 5, Height: 5, MinModuleMM: 1, MinSize: -1},
			}
		}, []string{"elements[0].max_size", "elements[0].min_module_mm", "elements[1].min_module_mm", "elements[1].min_size"}},
		{"alternatives", func(t *Template) {
			t.Elements = []Element{
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, WhenTooSmall: TooSmallAlternative},
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, WhenTooSmall: "shrink"},
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, Alternative: &Element{Type: ElementGenerator, GeneratorType: "micro_qr"}},
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, WhenTooSmall: TooSmallAlternative, Alternative: &Element{Type: ElementStaticImage}},
				{Type: ElementGenerator, GeneratorType: "qr", Width: 5, Height: 5, WhenTooSmall: TooSmallAlternative, Alternative: &Element{Type: ElementGenerator, GeneratorType: "ean"}},
			}
		}, []string{"elements[0].alternative", "elements[1].when_too_small", "elements[2].alternative", "elements[3].alternative.type", "elements[4].alternative.generator_type"}},
	}
	for _, c := range cases {
		tpl := adaptive()
		c.mutate(tpl)
		paths := problemPaths(t, tpl.Validate())
		sort.Strings(paths)
		if !reflect.DeepEqual(paths, c.paths) {
			t.Errorf("%s: problems at %v, want %v", c.name, paths, c.paths)
		}
	}
}

// TestListOrder draws a document written before layering in the order its
// elements were drawn then, and lists them so when it is saved; one in list
// order is drawn as listed.
func TestListOrder(t *testing.T) {
	const legacy = `{"name":"T","units":"mm","width":40,"height":20,"dpi":300,"elements":[
		{"type":"generator","generator_type":"qr","x":0,"y":0,"width":10,"height":10},
		{"type":"text","x":10,"y":0,"width":10,"height":5,"font_size":8,"content_binding":"first"},
		{"type":"static_image","x":20,"y":0,"width":10,"height":10,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"},
		{"type":"text","x":10,"y":5,"width":10,"height":5,"font_size":8,"content_binding":"second"}]}`
	tpl, err := Parse([]byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	if got := tpl.DrawingOrder(); !reflect.DeepEqual(got, []int{2, 1, 3, 0}) || tpl.Layering != "" {
		t.Errorf("legacy drawing order: %v, layering %q", got, tpl.Layering)
	}
	tpl.ListOrder()
	var got []string
	for _, e := range tpl.Elements {
		got = append(got, e.Type+":"+e.ContentBinding)
	}
	want := []string{"static_image:", "text:first", "text:second", "generator:"}
	if !reflect.DeepEqual(got, want) || tpl.Layering != LayeringList || !reflect.DeepEqual(tpl.DrawingOrder(), []int{0, 1, 2, 3}) {
		t.Errorf("listed: %v, layering %q", got, tpl.Layering)
	}

	listed := strings.Replace(legacy, `"elements"`, `"layering":"list","elements"`, 1)
	if tpl, err = Parse([]byte(listed)); err != nil {
		t.Fatal(err)
	}
	if got := tpl.DrawingOrder(); !reflect.DeepEqual(got, []int{0, 1, 2, 3}) {
		t.Errorf("list drawing order: %v", got)
	}
	var ve *ValidationError
	if _, err := Parse([]byte(strings.Replace(listed, `"list"`, `"type"`, 1))); !errors.As(err, &ve) || ve.Problems[0].Path != "layering" {
		t.Errorf("unknown layering: %v", err)
	}
}

// TestRotation checks turned elements against the canvas by where they
// lie once turned, so a box may start before the canvas.
func TestRotation(t *testing.T) {
	doc := func(element string) string {
		return `{"name":"T","units":"mm","width":20,"height":50,"dpi":300,"layering":"list","elements":[` + element + `]}`
	}
	// 40 × 6 mm, turned upright, centered on a 20 × 50 mm label.
	upright := `{"type":"text","x":-10,"y":22,"width":40,"height":6,"font_size":8,"content_binding":"A","rotation":90}`
	tpl, err := Parse([]byte(doc(upright)))
	if err != nil {
		t.Fatalf("upright: %v", err)
	}
	if x, y, w, h := tpl.Elements[0].Bounds(); x != 7 || y != 5 || w != 6 || h != 40 {
		t.Errorf("bounds (%g, %g) %g × %g", x, y, w, h)
	}
	e := Element{X: 0, Y: 0, Width: 10, Height: 10, Rotation: 45}
	if x, _, w, _ := e.Bounds(); math.Abs(w-10*math.Sqrt2) > 1e-9 || math.Abs(x-(5-5*math.Sqrt2)) > 1e-9 {
		t.Errorf("45°: x %g width %g", x, w)
	}

	for _, tc := range []struct {
		element string
		want    []string
	}{
		{strings.Replace(upright, `"rotation":90`, `"rotation":0`, 1), []string{"elements[0].x"}},
		{strings.Replace(upright, `"rotation":90`, `"rotation":45`, 1), []string{"elements[0].rotation", "elements[0].rotation"}},
		{strings.Replace(upright, `"rotation":90`, `"rotation":360`, 1), []string{"elements[0].rotation", "elements[0].x"}},
		{strings.Replace(upright, `"rotation":90`, `"rotation":-90`, 1), []string{"elements[0].rotation", "elements[0].x"}},
		{`{"type":"generator","generator_type":"qr","x":0,"y":0,"width":10,"height":10,"when_too_small":"alternative",
			"alternative":{"type":"text","x":0,"y":0,"width":1,"height":1,"font_size":8,"content_binding":"A","rotation":90}}`, []string{"elements[0].alternative.rotation"}},
	} {
		_, err := Parse([]byte(doc(tc.element)))
		if got := problemPaths(t, err); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.element, got, tc.want)
		}
	}
}

// TestMargins checks a code's margins and alignment, and that only codes
// take them.
func TestMargins(t *testing.T) {
	doc := func(element string) string {
		return `{"name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[` + element + `]}`
	}
	code := func(props string) string {
		return `{"type":"generator","generator_type":"qr","x":0,"y":0,"width":20,"height":10,` + props + `}`
	}
	for _, ok := range []string{
		code(`"margin":{"top":0,"left":20}`),
		code(`"margin":{"left":10,"right":10,"top":5},"margin_unit":"length","align":"right","vertical_align":"bottom"`),
		code(`"margin_unit":"modules","align":"left"`),
	} {
		if _, err := Parse([]byte(doc(ok))); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, tc := range []struct {
		element string
		want    []string
	}{
		{code(`"margin":{"left":21,"top":-1}`), []string{"elements[0].margin.left", "elements[0].margin.top"}},
		{code(`"margin":{"left":10.5,"bottom":5.5},"margin_unit":"length"`), []string{"elements[0].margin.bottom", "elements[0].margin.left"}},
		{code(`"margin":{"left":1},"quiet_zone":2`), []string{"elements[0].margin"}},
		{code(`"margin_unit":"inches"`), []string{"elements[0].margin_unit"}},
		{code(`"align":"justify","vertical_align":"center"`), []string{"elements[0].align", "elements[0].vertical_align"}},
		{`{"type":"text","x":0,"y":0,"width":20,"height":10,"font_size":8,"content_binding":"A","margin":{"left":1},"vertical_align":"top"}`,
			[]string{"elements[0].margin", "elements[0].vertical_align"}},
		{`{"type":"static_image","x":0,"y":0,"width":20,"height":10,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","align":"left"}`, []string{"elements[0].align"}},
	} {
		_, err := Parse([]byte(doc(tc.element)))
		got := problemPaths(t, err)
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.element, got, tc.want)
		}
	}
}

// TestShapes checks shapes, what applies to each, and their paint, and
// fills in their defaults.
func TestShapes(t *testing.T) {
	doc := func(element string) string {
		return `{"name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[` + element + `]}`
	}
	tpl, err := Parse([]byte(doc(`{"type":"shape","shape":"polygon","x":0,"y":0,"width":10,"height":10,"points":[[0,0],[1,0],[0.5,1]]},
		{"type":"shape","shape":"line","x":0,"y":5,"width":20,"height":0},
		{"type":"text","x":0,"y":0,"width":10,"height":5,"font_size":8,"content_binding":"A","fill":"white"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if p := tpl.Elements[0]; p.Stroke != PaintBlack || p.Fill != PaintNone || p.StrokeWidth != DefaultStrokeMM || p.Closed == nil || !*p.Closed {
		t.Errorf("defaults: %+v", p)
	}

	shape := func(props string) string {
		return `{"type":"shape","x":0,"y":0,"width":10,"height":10,` + props + `}`
	}
	for _, tc := range []struct {
		element string
		want    []string
	}{
		{shape(`"shape":"star"`), []string{"elements[0].shape"}},
		{shape(`"shape":"ellipse","stroke":"none"`), []string{"elements[0].stroke"}},
		{shape(`"shape":"ellipse","fill":"red","stroke_width":-1`), []string{"elements[0].fill", "elements[0].stroke_width"}},
		{shape(`"shape":"rectangle","corner_radius":6,"flip":true`), []string{"elements[0].corner_radius", "elements[0].flip"}},
		{shape(`"shape":"line","fill":"black","points":[[0,0],[1,1]]`), []string{"elements[0].fill", "elements[0].points"}},
		{`{"type":"shape","shape":"line","x":0,"y":0,"width":0,"height":0}`, []string{"elements[0].width"}},
		{shape(`"shape":"polygon","points":[[0,0],[1,1]]`), []string{"elements[0].points"}},
		{shape(`"shape":"polygon","closed":false,"fill":"black","points":[[0,0],[1,2]]`), []string{"elements[0].fill", "elements[0].points[1]"}},
		{`{"type":"text","x":0,"y":0,"width":10,"height":5,"font_size":8,"content_binding":"A","fill":"none","stroke":"black"}`, []string{"elements[0].fill", "elements[0].stroke"}},
		{`{"type":"generator","generator_type":"qr","x":0,"y":0,"width":10,"height":10,"fill":"white"}`, []string{"elements[0].fill"}},
	} {
		_, err := Parse([]byte(doc(tc.element)))
		got := problemPaths(t, err)
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.element, got, tc.want)
		}
	}
	// Too many points.
	pts := strings.Repeat("[0,0],", MaxPolygonPoints) + "[1,1]"
	if _, err := Parse([]byte(doc(shape(`"shape":"polygon","points":[` + pts + `]`)))); err == nil {
		t.Error("a polygon of too many points")
	}
}

// TestTurned turns a template a quarter turn with everything on it, its
// anchors following their sides, and four turns give it back.
func TestTurned(t *testing.T) {
	tpl, err := Parse([]byte(`{"name":"T","units":"mm","width":60,"height":20,"dpi":300,"layering":"list","sizing":"adaptive",
		"adapt":{"min_width":30,"max_width":90,"min_height":10,"max_height":40,"continuous_length":"fixed"},"turnable":true,"feed":"along","elements":[
		{"type":"generator","generator_type":"qr","x":2,"y":2,"width":16,"height":16,"anchor_x":"start","anchor_y":"end"},
		{"type":"text","x":20,"y":4,"width":38,"height":6,"font_size":10,"content_binding":"A","rotation":10}]}`))
	if err != nil {
		t.Fatal(err)
	}
	right := tpl.Turned(true)
	if right.Width != 20 || right.Height != 60 || right.Adapt.MinWidth != 10 || right.Adapt.MaxHeight != 90 || !right.Turnable || right.Feed != FeedAlong {
		t.Fatalf("turned: %+v %+v", right, right.Adapt)
	}
	// The code's center, (10, 10), goes to (20 - 10, 10); it reads turned.
	if c := right.Elements[0]; c.X != 2 || c.Y != 2 || c.Rotation != 90 || c.AnchorX != AnchorStart || c.AnchorY != AnchorStart {
		t.Errorf("the code: %+v", c)
	}
	if err := right.Validate(); err != nil {
		t.Errorf("turned, it is invalid: %v", err)
	}
	// Left undoes right, and four turns either way give it back.
	if back := right.Turned(false); !reflect.DeepEqual(back, tpl) {
		t.Errorf("right then left: %+v", back)
	}
	four := tpl
	for range 4 {
		four = four.Turned(false)
	}
	if !reflect.DeepEqual(four, tpl) {
		t.Errorf("four turns: %+v", four)
	}

	var ve *ValidationError
	if _, err := Parse([]byte(`{"name":"T","units":"mm","width":60,"height":20,"dpi":300,"feed":"sideways","turnable":true,"elements":[]}`)); !errors.As(err, &ve) ||
		!reflect.DeepEqual(problemPaths(t, err), []string{"feed", "turnable"}) {
		t.Errorf("feed and turnable: %v", err)
	}
}

// TestParsePathData reads every command, relative and absolute, the
// shorthands and arcs, and writes what it read back.
func TestParsePathData(t *testing.T) {
	segs, err := ParsePathData("m10 10 h5 v5 H0 V0 l1,1 2-2 c1 1 2 2 3 3 s4 0 5 5 q1 1 2 0 t2 0 z M0 0 L1 1")
	if err != nil {
		t.Fatal(err)
	}
	got := FormatPathData(segs)
	want := "M10 10 L15 10 L15 15 L0 15 L0 0 L1 1 L3 -1 C4 0 5 1 6 2 C7 3 10 2 11 7 Q12 8 13 7 Q14 6 15 7 Z M0 0 L1 1"
	if got != want {
		t.Errorf("read\n%s\nwant\n%s", got, want)
	}
	again, err := ParsePathData(got)
	if err != nil || FormatPathData(again) != got {
		t.Errorf("round trip: %v %s", err, FormatPathData(again))
	}
	// A half circle, as two quarter-turn cubics, ending where the arc ends.
	arc, err := ParsePathData("M0 0 A1 1 0 0 1 2 0")
	if err != nil || len(arc) != 3 || arc[2].End() != [2]float64{2, 0} {
		t.Errorf("arc: %+v %v", arc, err)
	}
	if top := arc[1].End(); math.Abs(top[0]-1) > 1e-9 || math.Abs(top[1]+1) > 1e-9 {
		t.Errorf("the arc's top: %v", top)
	}
	for _, bad := range []string{"", "L1 1", "M1", "M0 0 X 1", "M0 0 A1 1 0 2 0 1 1", "M1e999 0", strings.Repeat("M0 0 ", MaxPathSegments+1)} {
		if _, err := ParsePathData(bad); err == nil {
			t.Errorf("%.30q: read", bad)
		}
	}
}

// TestPaths checks a path shape's data, its points within its box, and
// how many segments a template's paths may have.
func TestPaths(t *testing.T) {
	doc := func(elements string) string {
		return `{"name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[` + elements + `]}`
	}
	path := func(props string) string {
		return `{"type":"shape","shape":"path","x":0,"y":0,"width":10,"height":10,` + props + `}`
	}
	if _, err := Parse([]byte(doc(path(`"d":"M0 0 L1 0 L0.5 1 Z","fill":"black","fill_rule":"evenodd"`)))); err != nil {
		t.Error(err)
	}
	for _, tc := range []struct {
		element string
		want    []string
	}{
		{path(`"d":"M0 0 L2 1"`), []string{"elements[0].d"}},
		{path(`"d":"nonsense"`), []string{"elements[0].d"}},
		{path(`"d":"M0 0 L1 1","fill_rule":"odd"`), []string{"elements[0].fill_rule"}},
		{`{"type":"shape","shape":"rectangle","x":0,"y":0,"width":10,"height":10,"d":"M0 0 L1 1","fill_rule":"evenodd"}`, []string{"elements[0].d", "elements[0].fill_rule"}},
	} {
		_, err := Parse([]byte(doc(tc.element)))
		got := problemPaths(t, err)
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.element, got, tc.want)
		}
	}
	// Eleven paths of 2,000 segments: past the template's 20,000.
	d := "M0 0" + strings.Repeat(" L1 1", MaxPathSegments-1)
	many := strings.TrimSuffix(strings.Repeat(path(`"d":"`+d+`"`)+",", 11), ",")
	if _, err := Parse([]byte(doc(many))); err == nil || !strings.Contains(err.Error(), "segments") {
		t.Errorf("too many segments: %v", err)
	}
}

// TestWorkspaceProperties checks guides, groups, locks and mirrors, which
// change how the editor works but not what prints, and that guides turn
// with a template.
func TestWorkspaceProperties(t *testing.T) {
	doc := func(guides, element string) string {
		return `{"name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","guides":` + guides + `,"elements":[` + element + `]}`
	}
	img := `{"type":"static_image","x":0,"y":0,"width":10,"height":10,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	tpl, err := Parse([]byte(doc(`{"x":[5,20],"y":[10]}`, img+`,"mirror":"across","group":"logo","locked":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	if right := tpl.Turned(true); !reflect.DeepEqual(right.Guides, &Guides{X: []float64{10}, Y: []float64{5, 20}}) {
		t.Errorf("turned guides: %+v", right.Guides)
	}
	for _, tc := range []struct {
		guides, element string
		want            []string
	}{
		{`{"x":[41]}`, img + `}`, []string{"guides.x"}},
		{`{"y":[` + strings.TrimSuffix(strings.Repeat("1,", MaxGuides+1), ",") + `]}`, img + `}`, []string{"guides.y"}},
		{`{}`, img + `,"mirror":"sideways"}`, []string{"elements[0].mirror"}},
		{`{}`, `{"type":"text","x":0,"y":0,"width":10,"height":5,"font_size":8,"content_binding":"A","mirror":"across","group":" logo"}`, []string{"elements[0].group", "elements[0].mirror"}},
	} {
		_, err := Parse([]byte(doc(tc.guides, tc.element)))
		got := problemPaths(t, err)
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s: %v, want %v", tc.guides, tc.element, got, tc.want)
		}
	}
}

// TestArrange lines elements up with each other and with the label, as
// they lie turned, and spreads them out evenly.
func TestArrange(t *testing.T) {
	box := func(x, y, w, h float64) Element { return Element{Type: ElementText, X: x, Y: y, Width: w, Height: h} }
	tpl := func() *Template {
		return &Template{Width: 100, Height: 50, Elements: []Element{box(10, 5, 10, 10), box(40, 20, 20, 10), box(80, 30, 5, 5)}}
	}
	at := func(t *Template) [][2]float64 {
		var out [][2]float64
		for _, e := range t.Elements {
			x, y, _, _ := e.Bounds()
			out = append(out, [2]float64{x, y})
		}
		return out
	}
	for _, tc := range []struct {
		align, distribute string
		toLabel           bool
		want              [][2]float64
	}{
		{"left", "", false, [][2]float64{{10, 5}, {10, 20}, {10, 30}}},
		{"right", "", true, [][2]float64{{90, 5}, {80, 20}, {95, 30}}},
		{"center", "", false, [][2]float64{{42.5, 5}, {37.5, 20}, {45, 30}}},
		{"middle", "", true, [][2]float64{{10, 20}, {40, 20}, {80, 22.5}}},
		{"bottom", "", false, [][2]float64{{10, 25}, {40, 25}, {80, 30}}},
		// From 10 to 85, 35 wide in all: gaps of 20.
		{"", "across", false, [][2]float64{{10, 5}, {40, 20}, {80, 30}}},
		// From 5 to 35, 25 high in all: gaps of 2.5.
		{"", "down", false, [][2]float64{{10, 5}, {40, 17.5}, {80, 30}}},
	} {
		got := tpl()
		if err := got.Arrange([]int{0, 1, 2}, tc.align, tc.distribute, tc.toLabel); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(at(got), tc.want) {
			t.Errorf("%s%s, to the label %v: %v, want %v", tc.align, tc.distribute, tc.toLabel, at(got), tc.want)
		}
	}
	// A turned element lines up by where it lies.
	turned := tpl()
	turned.Elements[1].Rotation = 90 // 20 × 10 turned: 10 wide, from 45
	if err := turned.Arrange([]int{0, 1}, "left", "", false); err != nil || turned.Elements[1].X != 5 {
		t.Errorf("turned: %+v %v", turned.Elements[1], err)
	}
	for _, bad := range [][]int{{0}, {0, 3}, {0, 0}} {
		if err := tpl().Arrange(bad, "left", "", false); err == nil {
			t.Errorf("%v: arranged", bad)
		}
	}
	if err := tpl().Arrange([]int{0, 1}, "", "across", false); err == nil {
		t.Error("spread out two")
	}
}
