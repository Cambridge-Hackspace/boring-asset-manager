package live

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tag = `{"units":"mm","width":20,"height":10,"dpi":300,"layering":"list","elements":[
  {"type":"text","x":1,"y":1,"width":18,"height":8,"font_size":10,"content_binding":"{{field.room}}"}]}`

const sample = `{"asset_id":"00000000-0000-4000-8000-000000000000","asset_name":"Sample asset","codes":{"qr":"BAM-0001"},"fields":{}}`

// TestRenderPlaceholders fills the fields a template binds, as the server's
// preview of no asset does, only when asked to.
func TestRenderPlaceholders(t *testing.T) {
	r := New()
	res := r.Render([]byte(tag), []byte(sample), true)
	if len(res.Problems) != 0 || res.Width != 236 || res.Height != 118 || len(res.Pixels) != 236*118*4 {
		t.Fatalf("got %dx%d, %d bytes, %v", res.Width, res.Height, len(res.Pixels), res.Problems)
	}
	if v := res.Description.Elements[0].Value; v != "[room]" {
		t.Errorf("placeholder: got %q", v)
	}
	black := 0
	for i := 0; i < len(res.Pixels); i += 4 {
		if res.Pixels[i] == 0 {
			black++
		}
		if res.Pixels[i+3] != 0xff || res.Pixels[i] != res.Pixels[i+1] || res.Pixels[i] != res.Pixels[i+2] {
			t.Fatalf("dot %d is not opaque gray: %v", i/4, res.Pixels[i:i+4])
		}
	}
	if black == 0 {
		t.Error("the text drew no dots")
	}
	if res := r.Render([]byte(tag), []byte(sample), false); res.Description.Elements[0].Value != "" {
		t.Errorf("without placeholders: got %q", res.Description.Elements[0].Value)
	}
}

// TestRenderProblems answers as the server's preview does when a template
// cannot be drawn.
func TestRenderProblems(t *testing.T) {
	r := New()
	for name, c := range map[string]struct{ template, data, path, message string }{
		"not an object": {`[1]`, sample, "", "template must be a JSON object"},
		"invalid":       {strings.Replace(tag, `"dpi":300`, `"dpi":5`, 1), sample, "dpi", ""},
		"bad data":      {tag, `[`, "", "data must be"},
		"missing image": {strings.Replace(tag, `"elements":[`, `"elements":[{"type":"static_image","x":0,"y":0,"width":5,"height":5,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"},`, 1), sample, "elements[0]", "missing"},
	} {
		res := r.Render([]byte(c.template), []byte(c.data), true)
		if len(res.Problems) == 0 || res.Pixels != nil || res.Problems[0].Path != c.path || !strings.Contains(res.Problems[0].Message, c.message) {
			t.Errorf("%s: got %+v", name, res.Problems)
		}
	}
	if err := r.AddImage("x", []byte("not an image")); err == nil || r.HasImage("x") {
		t.Error("an undecodable image was added")
	}
}

// TestParityFixture draws every case of the fixture the WebAssembly build is
// checked against, natively: the hashes are the server's own.
func TestParityFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "render", "testdata", "live_parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string            `json:"name"`
		Template json.RawMessage   `json:"template"`
		Data     json.RawMessage   `json:"data"`
		Images   map[string]string `json:"images"`
		Width    int               `json:"width"`
		Height   int               `json:"height"`
		SHA256   string            `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 9 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, c := range cases {
		r := New()
		for id, b64 := range c.Images {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.AddImage(id, data); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
		}
		res := r.Render(c.Template, c.Data, false)
		if len(res.Problems) > 0 {
			t.Errorf("%s: %+v", c.Name, res.Problems)
			continue
		}
		h := sha256.New()
		for i := 0; i < len(res.Pixels); i += 4 {
			h.Write(res.Pixels[i : i+1])
		}
		if res.Width != c.Width || res.Height != c.Height || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
			t.Errorf("%s: drawn differently from the server", c.Name)
		}
	}
}

// TestRenderHidden leaves elements hidden out of the dots, but still says
// how they were laid out, as the editor needs of text being typed.
func TestRenderHidden(t *testing.T) {
	r := New()
	all := r.Render([]byte(tag), []byte(sample), true)
	hidden := r.Render([]byte(tag), []byte(sample), true, 0)
	black := func(res Result) int {
		n := 0
		for i := 0; i < len(res.Pixels); i += 4 {
			if res.Pixels[i] == 0 {
				n++
			}
		}
		return n
	}
	if black(all) == 0 || black(hidden) != 0 {
		t.Errorf("black dots: %d drawn, %d hidden", black(all), black(hidden))
	}
	if d := hidden.Description; d == nil || len(d.Elements) != 1 || d.Elements[0].Value != "[room]" || d.Elements[0].DrawnFontSize != 10 {
		t.Errorf("hidden, still described: %+v", hidden.Description)
	}
}
