package code128

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden.json")

func TestPatternTable(t *testing.T) {
	seen := map[string]int{}
	for v, p := range patterns {
		wantLen, wantWidth := 6, 11
		if v == stop {
			wantLen, wantWidth = 7, 13
		}
		width := 0
		for _, w := range p {
			width += int(w - '0')
		}
		if len(p) != wantLen || width != wantWidth {
			t.Errorf("value %d: pattern %s has %d elements over %d modules, want %d over %d", v, p, len(p), width, wantLen, wantWidth)
		}
		if prev, dup := seen[p]; dup {
			t.Errorf("values %d and %d share pattern %s", prev, v, p)
		}
		seen[p] = v
	}
}

func TestSymbolValues(t *testing.T) {
	cases := []struct {
		payload string
		want    []int
	}{
		// Check characters computed by hand: (start + Σ position × value) mod 103.
		{"PJJ123C", []int{startB, 48, 42, 42, 17, 18, 19, 35, 55, stop}},
		{"12", []int{startC, 12, 14, stop}},
		{"123", []int{startB, 17, 18, 19, 8, stop}},
		{"1234", []int{startC, 12, 34, 82, stop}},
		{"12345", []int{startC, 12, 34, codeB, 21, 54, stop}},
		// A run of four digits at the end is worth switching for.
		{"BAM-0042", []int{startB, 34, 33, 45, 13, codeC, 0, 42, 47, stop}},
		// An odd run puts its first digit in code set B.
		{"BAM-00042", []int{startB, 34, 33, 45, 13, 16, codeC, 0, 42, 62, stop}},
		// Four digits mid-payload cost as much in C (with both switches) as in B.
		{"A1234B", []int{startB, 33, 17, 18, 19, 20, 34, 90, stop}},
		{"A123456B", []int{startB, 33, codeC, 12, 34, 56, codeB, 34, 80, stop}},
	}
	for _, c := range cases {
		code, err := Encode(c.payload)
		if err != nil {
			t.Fatalf("%q: %v", c.payload, err)
		}
		if !reflect.DeepEqual(code.Values, c.want) {
			t.Errorf("%q: values %v, want %v", c.payload, code.Values, c.want)
		}
		if want := 11*(len(code.Values)-1) + 13; len(code.Modules) != want {
			t.Errorf("%q: %d modules, want %d", c.payload, len(code.Modules), want)
		}
	}
}

func TestRejectsUnsupportedInput(t *testing.T) {
	for _, payload := range []string{"tab\there", "caf\xc3\xa9", "\x7f"} {
		if _, err := Encode(payload); !errors.Is(err, ErrUnsupportedCharacter) {
			t.Errorf("%q: got %v, want ErrUnsupportedCharacter", payload, err)
		}
	}
	if _, err := Encode(""); err == nil {
		t.Error("empty payload: want an error")
	}
}

type goldenCase struct {
	Payload string `json:"payload"`
	Modules string `json:"modules"`
}

var goldenPayloads = []string{
	"BAM-ASSET-001",
	"0123456789",
	"PJJ123C",
	"Hackspace 42!",
	"~{|}`_^]\\[@?>=<;:/.-,+*)('&%$#\"! ",
	"98765432109876543",
	"x9y88z777w6666",
}

// TestGoldens pins the exact bar patterns. Code 128 encodings are not unique,
// so they are not compared with another encoder; tools/goldens/verify_code128.py
// instead decodes every golden with zxing-cpp. Regenerate with -update and
// rerun that script.
func TestGoldens(t *testing.T) {
	var got []goldenCase
	for _, p := range goldenPayloads {
		code, err := Encode(p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		var sb strings.Builder
		for _, bar := range code.Modules {
			if bar {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
		got = append(got, goldenCase{Payload: p, Modules: sb.String()})
	}

	if *update {
		raw, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/golden.json", append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []goldenCase
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("encodings differ from testdata/golden.json")
	}
}

func FuzzEncode(f *testing.F) {
	for _, p := range goldenPayloads {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		code, err := Encode(payload)
		if err != nil {
			return
		}
		if want := 11*(len(code.Values)-1) + 13; len(code.Modules) != want {
			t.Fatalf("%d values produced %d modules", len(code.Values), len(code.Modules))
		}
		if !code.Modules[0] || !code.Modules[len(code.Modules)-1] {
			t.Fatal("symbol must begin and end with a bar")
		}
	})
}
