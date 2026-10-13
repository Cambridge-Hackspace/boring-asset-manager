package qr

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type goldenCase struct {
	Name        string   `json:"name"`
	Payload     string   `json:"payload"`
	Level       string   `json:"level"`
	Version     int      `json:"version"`
	Mask        int      `json:"mask"`
	WantVersion int      `json:"want_version"`
	WantMask    int      `json:"want_mask"`
	Rows        []string `json:"rows"`
}

func parseLevel(t *testing.T, s string) Level {
	t.Helper()
	i := strings.Index("LMQH", s)
	if len(s) != 1 || i < 0 {
		t.Fatalf("bad level %q", s)
	}
	return Level(i)
}

// TestMatchesSegnoGoldens compares symbols module for module with segno
// (tools/goldens), including the version and mask it chooses when they are
// left automatic.
func TestMatchesSegnoGoldens(t *testing.T) {
	raw, err := os.ReadFile("testdata/segno.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			code, err := encode(c.Payload, parseLevel(t, c.Level), c.Version, c.Mask)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if code.Version != c.WantVersion || code.Mask != c.WantMask {
				t.Fatalf("got version %d mask %d, want version %d mask %d", code.Version, code.Mask, c.WantVersion, c.WantMask)
			}
			if code.Size != len(c.Rows) {
				t.Fatalf("size %d, want %d", code.Size, len(c.Rows))
			}
			for y, row := range c.Rows {
				for x := range row {
					if code.Dark(x, y) != (row[x] == '1') {
						t.Fatalf("module (%d,%d) differs from segno", x, y)
					}
				}
			}
		})
	}
}

func TestDataCapacityMatchesStandard(t *testing.T) {
	// Spot checks against ISO/IEC 18004 Table 7.
	cases := []struct {
		version int
		level   Level
		want    int
	}{
		{1, L, 19}, {1, M, 16}, {1, Q, 13}, {1, H, 9},
		{7, M, 124}, {10, M, 216}, {21, Q, 512},
		{40, L, 2956}, {40, H, 1276},
	}
	for _, c := range cases {
		if got := dataCodewords(c.version, c.level); got != c.want {
			t.Errorf("dataCodewords(%d, %s) = %d, want %d", c.version, c.level, got, c.want)
		}
	}
}

func TestAlignmentPositions(t *testing.T) {
	cases := map[int][]int{
		1:  nil,
		2:  {6, 18},
		7:  {6, 22, 38},
		22: {6, 26, 50, 74, 98},
		32: {6, 34, 60, 86, 112, 138},
		40: {6, 30, 58, 86, 114, 142, 170},
	}
	for version, want := range cases {
		got := alignmentPositions(version)
		if len(got) != len(want) {
			t.Errorf("version %d: %v, want %v", version, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("version %d: %v, want %v", version, got, want)
				break
			}
		}
	}
}

func TestCapacityLimits(t *testing.T) {
	cases := []struct {
		payload string
		level   Level
		fits    bool
	}{
		{strings.Repeat("a", 2953), L, true},
		{strings.Repeat("a", 2954), L, false},
		{strings.Repeat("a", 1273), H, true},
		{strings.Repeat("a", 1274), H, false},
		{strings.Repeat("7", 7089), L, true},
		{strings.Repeat("7", 7090), L, false},
		{strings.Repeat("A", 4296), L, true},
		{strings.Repeat("A", 4297), L, false},
	}
	for _, c := range cases {
		code, err := Encode(c.payload, c.level)
		if c.fits && (err != nil || code.Version != 40) {
			t.Errorf("%d×%q at %s: got %v, want a version 40 symbol", len(c.payload), c.payload[:1], c.level, err)
		}
		if !c.fits && !errors.Is(err, ErrTooLong) {
			t.Errorf("%d×%q at %s: got %v, want ErrTooLong", len(c.payload), c.payload[:1], c.level, err)
		}
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	if _, err := Encode("x", Level(4)); !errors.Is(err, ErrInvalidLevel) {
		t.Errorf("level 4: got %v, want ErrInvalidLevel", err)
	}
	if _, err := encode(strings.Repeat("a", 100), L, 1, -1); !errors.Is(err, ErrTooLong) {
		t.Errorf("pinned version too small: got %v, want ErrTooLong", err)
	}
	if _, err := encode("a", L, 41, -1); !errors.Is(err, ErrTooLong) {
		t.Errorf("version 41: got %v, want ErrTooLong", err)
	}
}

func FuzzEncode(f *testing.F) {
	for _, seed := range []string{"", "0", "HELLO WORLD", "https://bam.example/a/1", "caf\xc3\xa9", "\xff\x00"} {
		f.Add(seed, uint8(0))
	}
	f.Fuzz(func(t *testing.T, payload string, level uint8) {
		code, err := Encode(payload, Level(level%4))
		if err != nil {
			if !errors.Is(err, ErrTooLong) {
				t.Fatalf("unexpected error: %v", err)
			}
			return
		}
		if code.Size != 17+4*code.Version || len(code.Modules) != code.Size*code.Size {
			t.Fatalf("version %d symbol has size %d and %d modules", code.Version, code.Size, len(code.Modules))
		}
	})
}
