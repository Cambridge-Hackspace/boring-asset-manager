package store

import (
	"reflect"
	"sort"
	"testing"
)

func TestWordNetIsLookedUpForActiveAssetsAndCategories(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tools, err := st.CreateCategory("Power tools", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAsset("Cordless drill", []string{tools.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	gone, err := st.CreateAsset("Old torch", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAsset(gone.Asset.ID); err != nil { // archives it
		t.Fatal(err)
	}
	names, err := st.CatalogNames()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if want := []string{"Cordless drill", "Power tools"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names %v, want %v", names, want)
	}
}

func TestWordNetsSourceIsPrincetonsUntilAnAdminSetsAnother(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	const princeton = "https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz"
	if got, err := st.WordNetSource(princeton); err != nil || got != princeton {
		t.Fatalf("by default %q, %v", got, err)
	}
	if err := st.SetWordNetSource("http://mirror.lan/wn3.1.dict.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.WordNetSource(princeton); got != "http://mirror.lan/wn3.1.dict.tar.gz" {
		t.Errorf("set, %q", got)
	}
	if err := st.SetWordNetSource(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.WordNetSource(princeton); got != princeton {
		t.Errorf("cleared, %q", got)
	}
}
