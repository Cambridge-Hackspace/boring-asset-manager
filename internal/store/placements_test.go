package store

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

// Placing a thing in its location's scene: kept by the location link,
// recorded in its history, refused off the floor or with no location,
// dropped when it is moved elsewhere, and cleared to be packed again.
func TestPlacingAssets(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	contains := relation(t, st, "contains")
	lab, shed, bench, loose := asset(t, st, "Lab"), asset(t, st, "Shed"), asset(t, st, "Bench"), asset(t, st, "Loose")
	link(t, st, lab, contains, bench, true)

	if _, err := st.PlaceAsset(loose, "", &[3]float64{1, 1, 0}); !errors.Is(err, ErrNotInLocation) {
		t.Errorf("no location: %v", err)
	}
	if _, err := st.PlaceAsset(bench, shed, &[3]float64{1, 1, 0}); !errors.Is(err, ErrNotInLocation) {
		t.Errorf("not in the shed: %v", err)
	}
	for _, bad := range [][3]float64{{-1, 0, 0}, {0, math.NaN(), 0}, {0, 0, MaxPlacement + 1}} {
		if _, err := st.PlaceAsset(bench, "", &bad); !errors.Is(err, ErrInvalidPlacement) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if _, err := st.PlaceAsset("missing", "", nil); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("missing: %v", err)
	}

	in, err := st.PlaceAsset(bench, "", &[3]float64{2, 1, 0.5})
	if err != nil || in != lab {
		t.Fatalf("%s %v", in, err)
	}
	if _, err := st.PlaceAsset(bench, lab, &[3]float64{3, 1, 0.5}); err != nil {
		t.Fatal(err)
	}
	// the same again changes nothing
	if _, err := st.PlaceAsset(bench, "", &[3]float64{3, 1, 0.5}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Placements(lab)
	if err != nil || !reflect.DeepEqual(got, map[string][3]float64{bench: {3, 1, 0.5}}) {
		t.Fatalf("%v %v", got, err)
	}
	events, _, err := st.ListAssetEvents(bench, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var moves [][2]string
	for _, e := range events {
		for _, c := range e.Payload.Changes {
			if c.What == "place" {
				if c.Key != lab || c.Label != "Lab" {
					t.Errorf("%+v", c)
				}
				moves = append(moves, [2]string{c.From, c.To})
			}
		}
	}
	if !reflect.DeepEqual(moves, [][2]string{{"2, 1, 0.5", "3, 1, 0.5"}, {"", "2, 1, 0.5"}}) {
		t.Errorf("moves %v", moves)
	}

	// cleared, it is packed again
	if _, err := st.PlaceAsset(bench, "", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Placements(lab); len(got) != 0 {
		t.Errorf("cleared: %v", got)
	}
	// moved elsewhere, its place in the lab goes with it
	if _, err := st.PlaceAsset(bench, "", &[3]float64{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	link(t, st, shed, contains, bench, true)
	if got, _ := st.Placements(lab); len(got) != 0 {
		t.Errorf("moved: %v", got)
	}
	if got, _ := st.Placements(shed); len(got) != 0 {
		t.Errorf("in the shed: %v", got)
	}
	if err := st.DeleteAsset(bench); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlaceAsset(bench, "", nil); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("archived: %v", err)
	}
}
