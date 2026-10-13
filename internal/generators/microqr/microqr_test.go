package microqr

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
			code, err := encode(c.Payload, Level(strings.Index("LMQ", c.Level)), c.Version, c.Mask)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if code.Version != c.WantVersion || code.Mask != c.WantMask {
				t.Fatalf("got M%d mask %d, want M%d mask %d", code.Version, code.Mask, c.WantVersion, c.WantMask)
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

func TestCapacityLimits(t *testing.T) {
	cases := []struct {
		payload string
		level   Level
		version int // 0 when the payload must not fit
	}{
		{"12345", L, 2},
		{"1234567890", L, 2},
		{"12345678901", L, 3},
		{"ABCDEF", L, 2},
		{"ABCDEFG", L, 3},
		{strings.Repeat("9", 35), L, 4},
		{strings.Repeat("9", 36), L, 0},
		{strings.Repeat("Z", 21), L, 4},
		{strings.Repeat("Z", 22), L, 0},
		{strings.Repeat("Z", 18), M, 4},
		{strings.Repeat("Z", 19), M, 0},
		{strings.Repeat("Z", 13), Q, 4},
		{strings.Repeat("Z", 14), Q, 0},
	}
	for _, c := range cases {
		code, err := Encode(c.payload, c.level)
		switch {
		case c.version == 0 && !errors.Is(err, ErrTooLong):
			t.Errorf("%q at %s: got %v, want ErrTooLong", c.payload, c.level, err)
		case c.version != 0 && err != nil:
			t.Errorf("%q at %s: %v", c.payload, c.level, err)
		case c.version != 0 && code.Version != c.version:
			t.Errorf("%q at %s: got %s, want M%d", c.payload, c.level, code.Designator(), c.version)
		}
	}
}

func TestQOnlyExistsInM4(t *testing.T) {
	code, err := Encode("1", Q)
	if err != nil {
		t.Fatal(err)
	}
	if code.Version != 4 {
		t.Errorf("level Q chose %s, want M4", code.Designator())
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	if _, err := Encode("lowercase", L); !errors.Is(err, ErrUnsupportedCharacter) {
		t.Errorf("lowercase: got %v, want ErrUnsupportedCharacter", err)
	}
	if _, err := Encode("A", Level(3)); !errors.Is(err, ErrInvalidLevel) {
		t.Errorf("level 3: got %v, want ErrInvalidLevel", err)
	}
	if _, err := encode("A", Q, 3, -1); !errors.Is(err, ErrTooLong) {
		t.Errorf("M3-Q: got %v, want ErrTooLong", err)
	}
}

func FuzzEncode(f *testing.F) {
	for _, seed := range []string{"", "7", "BAM-42", "lower", strings.Repeat("9", 35)} {
		f.Add(seed, uint8(0))
	}
	f.Fuzz(func(t *testing.T, payload string, level uint8) {
		code, err := Encode(payload, Level(level%3))
		if err != nil {
			if !errors.Is(err, ErrTooLong) && !errors.Is(err, ErrUnsupportedCharacter) {
				t.Fatalf("unexpected error: %v", err)
			}
			return
		}
		if code.Size != 2*code.Version+9 || len(code.Modules) != code.Size*code.Size {
			t.Fatalf("%s symbol has size %d and %d modules", code.Designator(), code.Size, len(code.Modules))
		}
	})
}
