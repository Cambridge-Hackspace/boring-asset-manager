package wordnet_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/wordnet"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/wordnet/wordnettest"
)

func open(t *testing.T) *wordnet.DB {
	t.Helper()
	archive, _ := wordnettest.Archive()
	db, err := wordnet.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReadsASynsetAtItsOffset(t *testing.T) {
	db := open(t)
	base := db.Base("flashlights")
	if !reflect.DeepEqual(base, []string{"flashlight"}) {
		t.Fatalf("flashlights is based on %v", base)
	}
	items, err := db.Seed([]string{"Flashlight"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []wordnet.Item{{Label: "flashlight", AltLabels: []string{"torch"}, Note: "a small portable battery-powered electric lamp", Broader: []string{}}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %+v, want %+v", items, want)
	}
}

func TestSeedsTheNounsAnInstanceUses(t *testing.T) {
	db := open(t)
	items, err := db.Seed([]string{"Cordless drills", "Torch", "Soldering iron stand", "Dice set", "Tool wall", "M3 x 10 bolts"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]wordnet.Item{}
	for _, it := range items {
		if _, dup := got[it.Label]; dup {
			t.Fatalf("two terms called %q", it.Label)
		}
		got[it.Label] = it
	}
	for label, want := range map[string]wordnet.Item{
		// A collocation, from two words side by side, made singular.
		"cordless drill": {AltLabels: []string{"battery drill"}, Broader: []string{"drill (tool)"}},
		// Two meanings of one word, told apart by what each is a kind of.
		"drill (tool)":     {AltLabels: []string{"drill"}, Broader: []string{"tool"}},
		"drill (activity)": {AltLabels: []string{"drill"}, Broader: []string{}},
		// Every meaning of torch: the flashlight it also names, and the
		// one that burns.
		"flashlight":     {AltLabels: []string{"torch"}, Broader: []string{}},
		"torch":          {AltLabels: []string{"flambeau"}, Broader: []string{}},
		"soldering iron": {AltLabels: []string{}, Broader: []string{"tool"}},
		"iron":           {AltLabels: []string{"Fe"}, Broader: []string{}},
		// dice is die by the exception list.
		"die":  {AltLabels: []string{}, Broader: []string{"tool"}},
		"tool": {AltLabels: []string{}, Broader: []string{}},
	} {
		it, ok := got[label]
		if !ok {
			t.Errorf("no term %q in %v", label, items)
			continue
		}
		if !reflect.DeepEqual(it.AltLabels, want.AltLabels) || !reflect.DeepEqual(it.Broader, want.Broader) {
			t.Errorf("%s: other names %v, broader %v; want %v, %v", label, it.AltLabels, it.Broader, want.AltLabels, want.Broader)
		}
	}
	if len(got) != 9 {
		t.Errorf("seeded %d terms, want 9: %v", len(got), items)
	}
	// Its gloss is its note, without the examples quoted after it.
	if n := got["tool"].Note; n != "an implement used in the practice of a vocation" {
		t.Errorf("tool's note is %q", n)
	}
}

func TestSeedsOnlyTheMostUsedMeaningsWhenAsked(t *testing.T) {
	db := open(t)
	items, err := db.Seed([]string{"Drill", "Torch"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	if !reflect.DeepEqual(labels, []string{"drill", "flashlight"}) {
		t.Errorf("seeded %v", labels)
	}
}

func TestWordsAreLookedUpAloneAndInPairs(t *testing.T) {
	got := wordnet.Words([]string{"Soldering iron, 60W", "an M3 bolt-cutter", "Soldering IRON"})
	want := []string{"soldering", "soldering_iron", "iron", "bolt-cutter"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOnlyTheExpectedFileIsAccepted(t *testing.T) {
	archive, src := wordnettest.Archive()
	if data, err := src.Check(bytes.NewReader(archive)); err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("the file itself: %v", err)
	}
	changed := append([]byte{}, archive...)
	changed[len(changed)/2] ^= 0xff
	if _, err := src.Check(bytes.NewReader(changed)); !errors.Is(err, wordnet.ErrMismatch) {
		t.Errorf("a changed file: %v, want ErrMismatch", err)
	}
	if _, err := src.Check(bytes.NewReader(append(archive, 0))); !errors.Is(err, wordnet.ErrMismatch) {
		t.Errorf("a longer file: %v, want ErrMismatch", err)
	}
	if _, err := src.Check(bytes.NewReader(archive[:len(archive)-1])); !errors.Is(err, wordnet.ErrMismatch) {
		t.Errorf("a file cut short: %v, want ErrMismatch", err)
	}
}

func TestABrokenDatabaseIsRefused(t *testing.T) {
	idx, data, _ := wordnettest.Files()
	if _, err := wordnet.Parse(idx, data[:20], nil); err == nil {
		t.Error("an index pointing past the data was accepted")
	}
	if _, err := wordnet.Parse([]byte("  1 just the license\n"), data, nil); err == nil {
		t.Error("an index of no words was accepted")
	}
	db, err := wordnet.Parse(idx, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Synset(3); err == nil {
		t.Error("a synset read from the middle of a line was accepted")
	}
}

// The real file's notice is kept as WordNet asks.
func TestTheNoticeNamesPrinceton(t *testing.T) {
	if !bytes.Contains([]byte(wordnet.Notice), []byte("WordNet 3.1 Copyright 2011 by Princeton University")) {
		t.Error("the notice lost its copyright line")
	}
	if wordnet.WordNet31.Title() != "WordNet 3.1" {
		t.Errorf("titled %q", wordnet.WordNet31.Title())
	}
}
