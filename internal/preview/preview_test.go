package preview

import (
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// TestWithFields fills the fields a template binds with placeholders,
// keeping other values and leaving the data it was given as it was.
func TestWithFields(t *testing.T) {
	tpl, err := labels.Parse([]byte(`{"id":"t","name":"T","units":"mm","width":20,"height":10,"dpi":300,"layering":"list","elements":[
		{"type":"text","x":1,"y":1,"width":18,"height":8,"font_size":10,"content_binding":"{{field.room}} {{asset.name}}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	base := Sample("https://bam.example")
	base.Fields["shelf"] = "B3"
	got := WithFields(base, tpl)
	if got.Fields["room"] != "[room]" || got.Fields["shelf"] != "B3" || got.AssetName != "Sample asset" || got.BaseURL != "https://bam.example" {
		t.Errorf("got %+v", got)
	}
	if _, ok := base.Fields["room"]; ok {
		t.Error("the data given was changed")
	}
	if d := Data(tpl, "x"); d.Fields["room"] != "[room]" || d.Codes["qr"] != "BAM-0001" {
		t.Errorf("Data: %+v", d)
	}
}

func TestWithDefaults(t *testing.T) {
	for in, want := range map[string]string{
		`{}`:                        `{"id":"preview","name":"Preview"}`,
		`{"id":"","name":"  "}`:     `{"id":"preview","name":"Preview"}`,
		`{"id":"tag","name":"Tag"}`: `{"id":"tag","name":"Tag"}`,
		`{"id":7}`:                  `{"id":7,"name":"Preview"}`,
	} {
		got, err := WithDefaults([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s, %v", in, got, err)
		}
	}
	if _, err := WithDefaults([]byte(`[1]`)); err == nil {
		t.Error("an array was taken for a template")
	}
}
