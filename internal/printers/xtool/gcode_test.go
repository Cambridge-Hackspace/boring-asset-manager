package xtool

import (
	"image"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

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

func TestGCodeEngravesRunsBackAndForth(t *testing.T) {
	p := Program{
		WidthMM: 100, HeightMM: 100,
		// Four dots across 4 mm, two rows over 2 mm: 1 mm a dot.
		Raster:  &Raster{X: 10, Y: 20, Width: 4, Height: 2, Dots: dots("##.#", "#..#")},
		Engrave: Setting{Power: 30, Speed: 150, Passes: 1},
	}
	got, err := GCode(p)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"G90", "M4S0",
		// Row 0, left to right: burn 10-12, skip to 13, burn 13-14.
		"G0X10Y20.5S0", "G1F9000S0", "G1X12Y20.5S300", "G1X13Y20.5S0", "G1X14Y20.5S300",
		// Row 1, right to left: burn 14-13, skip to 11, burn 11-10.
		"G0X14Y21.5S0", "G1X13Y21.5S300", "G1X11Y21.5S0", "G1X10Y21.5S300",
		"G0S0", "",
	}, "\n")
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestGCodeCutsPathsAfterEngravingEachPass(t *testing.T) {
	p := Program{
		WidthMM: 100, HeightMM: 100,
		Raster:  &Raster{X: 0, Y: 0, Width: 1, Height: 1, Dots: dots("#")},
		Engrave: Setting{Power: 50, Speed: 100, Passes: 2},
		Paths:   []Path{{Points: []Point{{1, 1}, {5, 1}, {5, 5}}, Closed: true}},
		Cut:     Setting{Power: 100, Speed: 10, Passes: 2},
	}
	got, err := GCode(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if n := strings.Count(s, "S500"); n != 2 {
		t.Errorf("engraved %d times:\n%s", n, s)
	}
	// Each cut pass goes around and back to the start.
	if n := strings.Count(s, "G1X1Y1S1000"); n != 2 {
		t.Errorf("closed %d times:\n%s", n, s)
	}
	if strings.Index(s, "S1000") < strings.LastIndex(s, "S500") {
		t.Errorf("cut before engraving finished:\n%s", s)
	}
	if !strings.Contains(s, "G1F600S0") {
		t.Errorf("no cutting feed:\n%s", s)
	}
	if !strings.HasSuffix(s, "G0S0\n") {
		t.Errorf("does not end with the laser off:\n%s", s)
	}
}

func TestGCodeMirrorsXForAMachineThatRunsRightToLeft(t *testing.T) {
	p := Program{WidthMM: 220, HeightMM: 220, MirrorX: true, Paths: []Path{{Points: []Point{{10, 5}, {20, 5}}}}, Cut: Setting{Power: 10, Speed: 10, Passes: 1}}
	got, err := GCode(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "G0X210Y5S0") || !strings.Contains(string(got), "G1X200Y5S100") {
		t.Errorf("not mirrored:\n%s", got)
	}
}

func TestGCodeRefusesWhatLeavesTheBed(t *testing.T) {
	p := Program{WidthMM: 115, HeightMM: 115, Paths: []Path{{Points: []Point{{10, 5}, {120, 5}}}}, Cut: Setting{Power: 10, Speed: 10, Passes: 1}}
	if _, err := GCode(p); err == nil {
		t.Fatal("a cut off the bed was written")
	}
	p = Program{WidthMM: 115, HeightMM: 115, Raster: &Raster{X: 100, Y: 0, Width: 20, Height: 1, Dots: dots("##")}, Engrave: Setting{Power: 10, Speed: 10, Passes: 1}}
	if _, err := GCode(p); err == nil {
		t.Fatal("an engraving off the bed was written")
	}
	p = Program{WidthMM: 115, HeightMM: 115, Raster: &Raster{Width: 1, Height: 1, Dots: dots("#")}, Engrave: Setting{Power: 0, Speed: 10, Passes: 1}}
	if _, err := GCode(p); err == nil {
		t.Fatal("no power was taken")
	}
}

var sValue = regexp.MustCompile(`S(\d+)`)

func TestFrameGCodeNeverFires(t *testing.T) {
	got, err := FrameGCode(115, 115, false, 10, 20, 30, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range sValue.FindAllStringSubmatch(string(got), -1) {
		if n, _ := strconv.Atoi(m[1]); n != 0 {
			t.Fatalf("framing fires the laser at S%d:\n%s", n, got)
		}
	}
	for _, corner := range []string{"X10Y20", "X40Y20", "X40Y60", "X10Y60"} {
		if !strings.Contains(string(got), corner) {
			t.Errorf("no corner %s:\n%s", corner, got)
		}
	}
	if _, err := FrameGCode(115, 115, false, 100, 20, 30, 40); err == nil {
		t.Error("a frame off the bed was written")
	}
}
