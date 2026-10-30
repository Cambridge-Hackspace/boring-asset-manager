package ir

import (
	"os"
	"strings"
	"testing"
)

func TestParseReadsIsolexsGolden(t *testing.T) {
	data, err := os.ReadFile("../testdata/sample.ir.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	l3 := doc.Icon("switch-l3")
	if l3 == nil || l3.Category != "net" || l3.Footprint == nil || l3.Canvas != nil {
		t.Fatalf("switch-l3: %+v", l3)
	}
	kinds := map[string]bool{}
	var walk func([]Shape)
	walk = func(ss []Shape) {
		for _, s := range ss {
			kinds[s.Kind] = true
			walk(s.Shapes)
		}
	}
	for _, icon := range doc.Icons {
		walk(icon.Shapes)
	}
	for _, k := range []string{KindPolygon, KindPath, KindGroup} {
		if !kinds[k] {
			t.Errorf("no %s read from the sample", k)
		}
	}
	if doc.Icon("nope") != nil {
		t.Error("found an icon that is not there")
	}
}

// isolex 0.4's IR, version 2: groups drawn through a matrix, a clip and
// fixed strokes.
func TestParseReadsVersionTwo(t *testing.T) {
	data, err := os.ReadFile("../testdata/isolex-0.6.0/transforms.ir.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.IRVersion != 2 {
		t.Fatalf("version %d", doc.IRVersion)
	}
	var clipped, fixed, matrices int
	for _, icon := range doc.Icons {
		for _, s := range icon.Shapes {
			if s.Kind != KindGroup {
				continue
			}
			matrices++
			if s.Clip != nil {
				clipped++
			}
			if s.FixedStrokes() {
				fixed++
			}
		}
	}
	if matrices == 0 || clipped == 0 || fixed == 0 {
		t.Fatalf("groups %d, clipped %d, fixed %d", matrices, clipped, fixed)
	}
	// a badge is a matrix of its scale and place
	data, err = os.ReadFile("../testdata/isolex-0.6.0/sample.ir.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if doc, err = Parse(data); err != nil {
		t.Fatal(err)
	}
	for _, icon := range doc.Icons {
		for _, s := range icon.Shapes {
			if s.Kind == KindGroup {
				m := s.Map()
				if m[0] != m[3] || m[1] != 0 || m[2] != 0 {
					t.Fatalf("%s: a badge's matrix %v", icon.Name, m)
				}
				return
			}
		}
	}
	t.Fatal("the sample badges nothing")
}

// A version 1 group, a badge's, is the same map written another way.
func TestMapIsEitherVersionsGroupAsAMatrix(t *testing.T) {
	v1 := Shape{Kind: KindGroup, TX: 3, TY: 4, Scale: 0.5}
	if got := v1.Map(); got != [6]float64{0.5, 0, 0, 0.5, 3, 4} {
		t.Fatal(got)
	}
	m := [6]float64{0, 1, -1, 0, 10, 0}
	v2 := Shape{Kind: KindGroup, Matrix: &m}
	if v2.Map() != m {
		t.Fatal(v2.Map())
	}
	// a quarter-turn after the badge's map: (2, 0) goes to (3+1, 4) and then
	// to (10-4, 4)
	if got := Apply(Mul(m, v1.Map()), [2]float64{2, 0}); got != [2]float64{6, 4} {
		t.Fatal(got)
	}
	if Mul(Identity, m) != m || Mul(m, Identity) != m {
		t.Fatal("the identity moves something")
	}
}

func TestParseRefusesWhatItDoesNotKnow(t *testing.T) {
	for in, want := range map[string]string{
		`{"ir_version":3,"icons":[]}`: "version 3, but this build reads versions 1 and 2",
		`{"ir_version":2,"icons":[{"name":"a","shapes":[{"kind":"group","shapes":[]}]}]}`:                                 "without a matrix",
		`{"ir_version":2,"icons":[{"name":"a","shapes":[{"kind":"group","matrix":[1,0,0,1,0,0],"clip":[[0,0],[1,1]]}]}]}`: "fewer than three points",
		`{"ir_version":1,"icons":[{"name":"a","shapes":[{"kind":"group","shapes":[{"kind":"spline"}]}]}]}`:                `unknown kind "spline"`,
		`[]`: "isolex IR",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", in, err)
		}
	}
}
