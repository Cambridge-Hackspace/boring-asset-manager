package store

import (
	"errors"
	"testing"
)

func TestFavoritesAreEachPersonsOwn(t *testing.T) {
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

	first, err := st.AddFavorite(ada.ID, FavoriteAsset, "a1")
	if err != nil {
		t.Fatal(err)
	}
	// Starred again: nothing changes, and it keeps when it was first starred.
	again, err := st.AddFavorite(ada.ID, FavoriteAsset, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !again.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("starred again at %v, first at %v", again.CreatedAt, first.CreatedAt)
	}
	for _, kind := range []string{FavoriteCategory, FavoriteReport, FavoriteFlow} {
		if _, err := st.AddFavorite(ada.ID, kind, "x1"); err != nil {
			t.Fatalf("starring a %s: %v", kind, err)
		}
	}
	if _, err := st.AddFavorite(bob.ID, FavoriteAsset, "a2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddFavorite(ada.ID, "user", bob.ID); !errors.Is(err, ErrInvalidFavorite) {
		t.Errorf("starring a user: %v, want ErrInvalidFavorite", err)
	}

	list, err := st.Favorites(ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("Ada's favorites: %+v, want 4", list)
	}
	ids, err := st.FavoriteIDs(ada.ID, FavoriteAsset)
	if err != nil {
		t.Fatal(err)
	}
	if !ids["a1"] || ids["a2"] || len(ids) != 1 {
		t.Errorf("Ada's starred assets: %v, want a1 alone", ids)
	}

	if err := st.RemoveFavorite(ada.ID, FavoriteAsset, "a1"); err != nil {
		t.Fatal(err)
	}
	// Removing one not starred is already as asked.
	if err := st.RemoveFavorite(ada.ID, FavoriteAsset, "a1"); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.FavoriteIDs(ada.ID, FavoriteAsset); len(ids) != 0 {
		t.Errorf("Ada's starred assets after removing: %v", ids)
	}
	if ids, _ := st.FavoriteIDs(bob.ID, FavoriteAsset); !ids["a2"] {
		t.Errorf("Bob's starred assets: %v, want a2", ids)
	}
}
