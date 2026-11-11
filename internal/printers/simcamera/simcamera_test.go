package simcamera

import (
	"bytes"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "regenerate the photos")

// TestPhotos checks every scene has its photo, and with -update draws
// them again. Drawing is deterministic, but JPEG encoding may change with
// Go, so the photos are only rewritten when asked.
func TestPhotos(t *testing.T) {
	for name, s := range Scenes() {
		path := name + ".jpg"
		if *update {
			if err := os.WriteFile(path, JPEG(Render(s)), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (regenerate with -update)", name, err)
		}
		if !bytes.Equal(got, Photo(name)) {
			t.Errorf("%s: the embedded photo is not the file", name)
		}
	}
}
