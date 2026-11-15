package store

import (
	"errors"
	"strings"
	"testing"
)

func savedNames(list []SavedSearch) string {
	names := []string{}
	for _, v := range list {
		names = append(names, v.Name)
	}
	return strings.Join(names, ",")
}

func TestSavedSearchesAreTheirPersonsOwnUnlessShared(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	ada, err := st.CreateUser("ada@hackspace.org", "Password123!", true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser("bob@hackspace.org", "Password123!", true)
	if err != nil {
		t.Fatal(err)
	}

	light, err := st.CreateSavedSearch(ada.ID, " light filament ", " field.weight < 200 ", false)
	if err != nil {
		t.Fatal(err)
	}
	if light.Name != "light filament" || light.Search != "field.weight < 200" || light.Shared || light.OwnerName != "ada@hackspace.org" {
		t.Errorf("saved %+v", light)
	}
	if _, err := st.CreateSavedSearch(ada.ID, "Broken tools", "condition = out_of_service", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSavedSearch(bob.ID, "Bob's drills", "drill", false); err != nil {
		t.Fatal(err)
	}
	// Names are each person's own, ignoring case; another person may use one.
	if _, err := st.CreateSavedSearch(ada.ID, "LIGHT FILAMENT", "pla", false); !errors.Is(err, ErrSavedSearchNameTaken) {
		t.Errorf("a second of one name: %v", err)
	}
	if _, err := st.CreateSavedSearch(bob.ID, "Light filament", "pla", false); err != nil {
		t.Errorf("another person's name: %v", err)
	}
	for _, bad := range [][2]string{{"", "drill"}, {"Drills", "  "}, {strings.Repeat("n", SavedSearchNameMax+1), "drill"}, {"Long", strings.Repeat("q", SavedSearchMax+1)}} {
		if _, err := st.CreateSavedSearch(ada.ID, bad[0], bad[1], false); !errors.Is(err, ErrInvalidSavedSearch) {
			t.Errorf("saving %q as %q: %v", bad[1], bad[0], err)
		}
	}

	// Each sees their own first, then what is shared; signed out, only what is shared.
	for who, want := range map[string]string{ada.ID: "Broken tools,light filament", bob.ID: "Bob's drills,Light filament,Broken tools", "": "Broken tools"} {
		list, err := st.SavedSearches(who)
		if err != nil {
			t.Fatal(err)
		}
		if got := savedNames(list); got != want {
			t.Errorf("%q sees %q, want %q", who, got, want)
		}
	}
	if _, err := st.GetSavedSearch(light.ID, bob.ID); !errors.Is(err, ErrSavedSearchNotFound) {
		t.Errorf("another's unshared search: %v", err)
	}

	// Only its person changes or deletes one, shared or not.
	if _, err := st.UpdateSavedSearch(light.ID, bob.ID, "Mine now", "pla", true); !errors.Is(err, ErrSavedSearchNotFound) {
		t.Errorf("changing another's: %v", err)
	}
	if _, err := st.UpdateSavedSearch(light.ID, ada.ID, "Broken TOOLS", "pla", true); !errors.Is(err, ErrSavedSearchNameTaken) {
		t.Errorf("renaming onto another of hers: %v", err)
	}
	changed, err := st.UpdateSavedSearch(light.ID, ada.ID, "Light filament", "field.weight < 250", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Name != "Light filament" || changed.Search != "field.weight < 250" || !changed.Shared {
		t.Errorf("changed to %+v", changed)
	}
	// Saved again as it is, it is still there.
	if _, err := st.UpdateSavedSearch(light.ID, ada.ID, "Light filament", "field.weight < 250", true); err != nil {
		t.Errorf("saved again unchanged: %v", err)
	}
	if got, err := st.GetSavedSearch(light.ID, bob.ID); err != nil || got.OwnerName != "ada@hackspace.org" {
		t.Errorf("shared now, bob reads %+v, %v", got, err)
	}
	if err := st.DeleteSavedSearch(light.ID, bob.ID); !errors.Is(err, ErrSavedSearchNotFound) {
		t.Errorf("deleting another's: %v", err)
	}
	if err := st.DeleteSavedSearch(light.ID, ada.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSavedSearch(light.ID, ada.ID); !errors.Is(err, ErrSavedSearchNotFound) {
		t.Errorf("deleted, then read: %v", err)
	}
}
