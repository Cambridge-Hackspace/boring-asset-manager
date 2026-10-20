package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// liveCase is one label the editor's live renderer (internal/live, compiled
// to WebAssembly) must draw dot for dot as the server does: its inputs as the
// browser hands them over, and a hash of its dots as the server draws them.
type liveCase struct {
	Name     string            `json:"name"`
	Template json.RawMessage   `json:"template"`
	Data     json.RawMessage   `json:"data"`
	Images   map[string]string `json:"images"` // id -> PNG, base64
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	// SHA256 is of the dots, one byte each (0 black, 255 white), row by row.
	SHA256 string `json:"sha256"`
}

// TestLiveParityFixture keeps testdata/live_parity.json, which the web
// interface's tests render with the WebAssembly build and compare, up to
// date with every golden template: rewrite it with -update.
func TestLiveParityFixture(t *testing.T) {
	images := memImages{logoID: testLogo(), arrowID: testArrow()}
	encoded := map[string]string{}
	for id, img := range images {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		encoded[id] = base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	data, err := json.Marshal(testData)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(goldenTemplates))
	for name := range goldenTemplates {
		names = append(names, name)
	}
	sort.Strings(names)

	var cases []liveCase
	for _, name := range names {
		s, err := Layout(mustTemplate(t, goldenTemplates[name]), testData, images)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		gray := s.Raster(true)
		b := gray.Bounds()
		h := sha256.New()
		for y := 0; y < b.Dy(); y++ {
			h.Write(gray.Pix[y*gray.Stride : y*gray.Stride+b.Dx()])
		}
		// The images the template places, by ID.
		used := map[string]string{}
		for id := range images {
			if bytes.Contains([]byte(goldenTemplates[name]), []byte(id)) {
				used[id] = encoded[id]
			}
		}
		cases = append(cases, liveCase{Name: name, Template: json.RawMessage(goldenTemplates[name]), Data: data,
			Images: used, Width: b.Dx(), Height: b.Dy(), SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	got, err := json.MarshalIndent(cases, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "live_parity.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("%s is out of date: run go test ./internal/render -run TestLiveParityFixture -update", path)
	}
}
