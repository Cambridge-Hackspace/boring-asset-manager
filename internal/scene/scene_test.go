package scene

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden scenes")

// The layouts of a few locations, held to testdata/scenes.golden.json:
// what every scene is drawn from must not change unnoticed.
func TestGoldenScenes(t *testing.T) {
	cases := []struct {
		Name  string
		Floor *[3]float64
		Items []Item
	}{
		{"empty", nil, nil},
		{"a shelf", &[3]float64{4, 1, 2}, []Item{
			{ID: "c", Name: "Crate", Size: [3]float64{1, 1, 1}},
			{ID: "a", Name: "bin", Size: [3]float64{1, 1, 0.5}},
			{ID: "b", Name: "Bin", Size: [3]float64{1, 1, 0.5}},
			{ID: "d", Name: "Drill", Size: [3]float64{0.5, 0.5, 0.3}},
			{ID: "e", Name: "Extension lead", Size: [3]float64{2, 1, 0.2}},
		}},
		{"a room sized to fit", nil, []Item{
			{ID: "1", Name: "Bench", Size: [3]float64{3, 1, 1}},
			{ID: "2", Name: "Rack", Size: [3]float64{1, 1, 4.5}},
			{ID: "3", Name: "Server", Size: [3]float64{2, 1, 0.56}},
			{ID: "4", Name: "Unknown"},
			{ID: "5", Name: "Wide table", Size: [3]float64{5, 2, 1}},
		}},
		{"a room partly arranged", &[3]float64{3, 2, 2}, []Item{
			{ID: "1", Name: "Bench", Size: [3]float64{2, 1, 1}, At: &[3]float64{1, 0, 0}},
			{ID: "2", Name: "Box", Size: [3]float64{1, 1, 1}},
			{ID: "3", Name: "Lamp", Size: [3]float64{1, 1, 0.5}, At: &[3]float64{1, 0, 1}},
			{ID: "4", Name: "Stool", Size: [3]float64{1, 1, 1}},
			{ID: "5", Name: "Trolley", Size: [3]float64{1, 1, 1}, At: &[3]float64{4, 1, 0}},
		}},
	}
	got := map[string]Scene{}
	for _, c := range cases {
		got[c.Name] = Layout(c.Floor, c.Items)
	}
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "scenes.golden.json")
	if *update {
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(data)+"\n" {
		t.Errorf("the scenes changed; if they should, run go test ./internal/scene -update\n%s", data)
	}
}

// Packed items never overlap, stay on a given floor's width, and each is
// drawn after every item it stands in front of and covers.
func TestLayoutKeepsItemsApartAndInOrder(t *testing.T) {
	var items []Item
	for i := 0; i < 40; i++ {
		items = append(items, Item{ID: fmt.Sprintf("%02d", i), Name: fmt.Sprintf("Thing %02d", 39-i),
			Size: [3]float64{float64(1 + i%3), float64(1 + i%2), 0.5 + float64(i%4)}})
	}
	s := Layout(&[3]float64{6, 2, 1}, items)
	if s.Floor[0] != 6 || s.Floor[1] < 2 || len(s.Items) != 40 {
		t.Fatalf("floor %v, %d items", s.Floor, len(s.Items))
	}
	at := map[string]int{}
	for k, p := range s.Items {
		at[p.ID] = k
		if p.X+p.Size[0] > s.Floor[0] || p.Y+p.Size[1] > s.Floor[1] {
			t.Errorf("%s off the floor: %+v", p.ID, p)
		}
	}
	for i, a := range s.Items {
		for _, b := range s.Items[i+1:] {
			if !behind(a, b) && !behind(b, a) {
				t.Errorf("%s and %s overlap", a.ID, b.ID)
			}
			// b is drawn after a: a must not stand in front of it, where they cover each other
			ax0, ay0, ax1, ay1 := screen(a)
			bx0, by0, bx1, by1 := screen(b)
			covers := !(ax1 <= bx0 || bx1 <= ax0 || ay1 <= by0 || by1 <= ay0)
			if covers && behind(b, a) && !behind(a, b) {
				t.Errorf("%s drawn over %s, which stands in front of it", b.ID, a.ID)
			}
		}
	}
	if a, b := Layout(nil, items), Layout(nil, items); fmt.Sprint(a) != fmt.Sprint(b) {
		t.Error("not deterministic")
	}
}

// Depth order across heights: a box on top of another is drawn after it.
func TestStackedBoxesAreDrawnBottomUp(t *testing.T) {
	low := Placed{ID: "z", X: 0, Y: 0, Z: 0, Size: [3]float64{1, 1, 1}}
	high := Placed{ID: "a", X: 0, Y: 0, Z: 1, Size: [3]float64{1, 1, 1}}
	s := finish([2]float64{1, 1}, []Placed{high, low})
	if s.Items[0].ID != "z" || s.Items[1].ID != "a" {
		t.Errorf("%v", s.Items)
	}
	if x, y := P(1, 0, 0); x != 34.64101615137755 || y != 20 {
		t.Errorf("P(1,0,0) = %v, %v", x, y)
	}
}

// Placed items stand where they were placed, and may share space, which is
// flagged on both; the rest pack around them, never into them; and the
// floor grows to take what was placed beyond it.
func TestPlacedItemsStayAndOverlapsAreFlagged(t *testing.T) {
	s := Layout(&[3]float64{3, 2, 2}, []Item{
		{ID: "bench", Name: "Bench", Size: [3]float64{2, 1, 1}, At: &[3]float64{1, 0, 0}},
		{ID: "box", Name: "Box", Size: [3]float64{1, 1, 1}},
		{ID: "crate", Name: "Crate", Size: [3]float64{1, 1, 1}, At: &[3]float64{2, 0, 0.5}},
		{ID: "lamp", Name: "Lamp", Size: [3]float64{1, 1, 0.5}, At: &[3]float64{1, 0, 1}},
		{ID: "stool", Name: "Stool", Size: [3]float64{1, 1, 1}},
		{ID: "trolley", Name: "Trolley", Size: [3]float64{1, 1, 1}, At: &[3]float64{4, 1, 0}},
		{ID: "odd", Name: "Odd", At: &[3]float64{-2, math.Inf(1), math.NaN()}},
	})
	by := map[string]Placed{}
	for _, p := range s.Items {
		by[p.ID] = p
	}
	if b := by["bench"]; b.X != 1 || b.Y != 0 || !b.Arranged || fmt.Sprint(b.Overlaps) != "[crate]" {
		t.Errorf("bench %+v", b)
	}
	if c := by["crate"]; fmt.Sprint(c.Overlaps) != "[bench]" {
		t.Errorf("crate %+v", c)
	}
	// on the bench, not in it
	if l := by["lamp"]; l.Z != 1 || l.Overlaps != nil {
		t.Errorf("lamp %+v", l)
	}
	if o := by["odd"]; o.X != 0 || o.Y != 0 || o.Z != 0 {
		t.Errorf("odd %+v", o)
	}
	// packed into what is left, on the floor as wide as the trolley makes
	// it: past the odd one, the bench and the crate
	if b, st := by["box"], by["stool"]; b.Arranged || b.X != 3 || b.Y != 0 || st.X != 4 || st.Y != 0 || b.Overlaps != nil {
		t.Errorf("box %+v stool %+v", b, st)
	}
	if s.Floor != [2]float64{5, 2} {
		t.Errorf("floor %v", s.Floor)
	}
}
