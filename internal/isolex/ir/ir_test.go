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

func TestParseRefusesWhatItDoesNotKnow(t *testing.T) {
	for in, want := range map[string]string{
		`{"ir_version":2,"icons":[]}`: "version 2",
		`{"ir_version":1,"icons":[{"name":"a","shapes":[{"kind":"group","shapes":[{"kind":"spline"}]}]}]}`: `unknown kind "spline"`,
		`[]`: "isolex IR",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", in, err)
		}
	}
}
