// Package reaper holds the battery reaper runs (run.sh) and checks it
// against .reaper.toml.
package reaper

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRunImagesArePulled checks that every image the reaper battery's
// scripts run is listed under run.images, which reaper pulls before the run,
// written as reaper accepts it: a registry host and a digest, no tag.
func TestRunImagesArePulled(t *testing.T) {
	root := filepath.Join("..", "..")
	manifest, err := os.ReadFile(filepath.Join(root, ".reaper.toml"))
	if err != nil {
		t.Fatal(err)
	}
	images := regexp.MustCompile(`(?s)\nimages = \[(.*?)\]`).FindSubmatch(manifest)
	if images == nil {
		t.Fatal(".reaper.toml has no run.images")
	}
	listed := string(images[1])
	accepted := regexp.MustCompile(`^(localhost|[a-z0-9-]+(\.[a-z0-9-]+)+)(:[0-9]+)?(/[a-z0-9._-]+)+@sha256:[0-9a-f]{64}$`)
	assignment := regexp.MustCompile(`(?m)^[A-Z_]+_IMAGE="([^"]+)"`)
	for _, script := range []string{"tools/reaper/run.sh", "tools/web/build.sh", "tools/web/e2e.sh"} {
		data, err := os.ReadFile(filepath.Join(root, script))
		if err != nil {
			t.Fatal(err)
		}
		found := assignment.FindAllSubmatch(data, -1)
		if len(found) == 0 {
			t.Errorf("%s names no images", script)
		}
		for _, m := range found {
			image := string(m[1])
			if !accepted.MatchString(image) {
				t.Errorf("%s: reaper would refuse %s", script, image)
			}
			if !strings.Contains(listed, `"`+image+`"`) {
				t.Errorf("%s runs %s, which run.images does not list", script, image)
			}
		}
	}
}
