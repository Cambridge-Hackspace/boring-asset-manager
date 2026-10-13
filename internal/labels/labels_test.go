package labels

import (
	"errors"
	"reflect"
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
