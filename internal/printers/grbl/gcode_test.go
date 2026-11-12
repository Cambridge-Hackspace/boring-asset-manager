package grbl

import (
	"flag"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.gcode")

// dots is a 1-bit image from rows of '#' (black) and '.'.
func dots(rows ...string) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, len(rows[0]), len(rows)))
	for y, row := range rows {
		for x, c := range row {
			v := uint8(0xFF)
			if c == '#' {
				v = 0
			}
			img.Pix[y*img.Stride+x] = v
		}
	}
	return img
}

// golden compares got with testdata/name, or writes it with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs:\ngot\n%s\nwant\n%s", name, got, want)
	}
}

// tag is a job engraving a small mark and cutting a tag's outline.
func tag(bed Bed) Program {
	return Program{
		Bed: bed,
		// Five dots across 5 mm, three rows over 3 mm: 1 mm a dot.
		Raster:  &Raster{X: 10, Y: 20, Width: 5, Height: 3, Dots: dots("##.##", ".....", "#...#")},
		Engrave: Setting{Power: 30, Speed: 150, Passes: 2},
		Paths:   []Path{{Points: []Point{{8, 18}, {17, 18}, {17, 25}, {8, 25}}, Closed: true}},
		Cut:     Setting{Power: 100, Speed: 5, Passes: 1},
	}
}

func TestGCodeGoldens(t *testing.T) {
	for _, c := range []struct {
		name string
		bed  Bed
	}{
		// Front left, as most diode lasers count, at S1000.
		{"tag-bottom-left.gcode", Bed{WidthMM: 400, HeightMM: 400, FlipY: true, SMax: 1000}},
		// Top left, counting down the bed, at S255.
		{"tag-top-left-s255.gcode", Bed{WidthMM: 100, HeightMM: 50, SMax: 255}},
	} {
		got, err := GCode(tag(c.bed))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, c.name, got)
	}
	frame, err := Frame(Bed{WidthMM: 400, HeightMM: 400, FlipY: true, SMax: 1000}, 8, 18, 9, 7, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "frame.gcode", frame)
}

func TestGCodeIsForLaserMode(t *testing.T) {
	got, err := GCode(tag(Bed{WidthMM: 400, HeightMM: 400, SMax: 1000}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "G21\nG90\nM4S0\n") || !strings.HasSuffix(s, "M5S0\n") {
		t.Errorf("not begun and ended for laser mode:\n%s", s)
	}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.HasPrefix(line, "G0") && !strings.HasSuffix(line, "S0") {
			t.Errorf("a travel with the laser on: %s", line)
		}
		if strings.HasPrefix(line, "$") {
			t.Errorf("a setting changed: %s", line)
		}
	}
	// Engraved twice at 30% of S1000, cut once at full power.
	if n := strings.Count(s, "S300"); n != 8 {
		t.Errorf("engraved %d runs, want 8 (4 a pass):\n%s", n, s)
	}
	if strings.Index(s, "S1000") < strings.LastIndex(s, "S300") {
		t.Errorf("cut before engraving finished:\n%s", s)
	}
}

func TestGCodeRefusesWhatLeavesTheBedOrCannotBurn(t *testing.T) {
	p := tag(Bed{WidthMM: 12, HeightMM: 400, SMax: 1000})
	if _, err := GCode(p); err == nil {
		t.Error("a job off the bed was written")
	}
	p = tag(Bed{WidthMM: 400, HeightMM: 400})
	if _, err := GCode(p); err == nil {
		t.Error("a job was written with no full power")
	}
	p = tag(Bed{WidthMM: 400, HeightMM: 400, SMax: 1000})
	p.Cut.Power = 0
	if _, err := GCode(p); err == nil {
		t.Error("a cut at no power was written")
	}
	if _, err := Frame(Bed{WidthMM: 400, HeightMM: 400, SMax: 1000}, 395, 0, 10, 10, 0, 50); err == nil {
		t.Error("a frame off the bed was written")
	}
}

var sValue = regexp.MustCompile(`S(\d+)`)

func TestFrameAtNoPowerNeverFires(t *testing.T) {
	got, err := Frame(Bed{WidthMM: 400, HeightMM: 400, SMax: 1000}, 10, 20, 30, 40, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range sValue.FindAllStringSubmatch(string(got), -1) {
		if n, _ := strconv.Atoi(m[1]); n != 0 {
			t.Fatalf("framing at no power fires the laser at S%d:\n%s", n, got)
		}
	}
	for _, corner := range []string{"X10Y20", "X40Y20", "X40Y60", "X10Y60"} {
		if !strings.Contains(string(got), corner) {
			t.Errorf("no corner %s:\n%s", corner, got)
		}
	}
	if !strings.Contains(string(got), "F3000") {
		t.Errorf("not at 50 mm/s:\n%s", got)
	}
}
