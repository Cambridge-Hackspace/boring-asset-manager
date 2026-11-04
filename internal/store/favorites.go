package store

import (
	"errors"
	"fmt"
	"time"
)

// The kinds of thing a person can star (#225, docs/favorites.md).
const (
	FavoriteAsset    = "asset"
	FavoriteCategory = "category"
	FavoriteReport   = "report"
	FavoriteFlow     = "flow"
)

// FavoriteKinds lists the kinds, in the order the home page shows them.
var FavoriteKinds = []string{FavoriteAsset, FavoriteCategory, FavoriteReport, FavoriteFlow}

// ErrInvalidFavorite is a kind that cannot be starred.
var ErrInvalidFavorite = errors.New("a favorite is an asset, category, report or flow")

// Favorite is one thing a person has starred.
type Favorite struct {
	Kind      string
	ObjectID  string
	CreatedAt time.Time
}

func validFavoriteKind(kind string) bool {
	for _, k := range FavoriteKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Favorites lists what a person has starred, oldest first. It holds
// whatever they starred, whether or not they can still see it, or it is
// still there: the caller keeps those they can.
func (s *Store) Favorites(userID string) ([]Favorite, error) {
	rows, err := s.Query("SELECT kind, object_id, created_at FROM user_favorites WHERE user_id = ? ORDER BY created_at, kind, object_id", userID)
	if err != nil {
		return nil, fmt.Errorf("listing favorites: %w", err)
	}
	defer rows.Close()
	out := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Kind, &f.ObjectID, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("listing favorites: %w", err)
		}
		f.CreatedAt = f.CreatedAt.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// FavoriteIDs is the set of a person's favorites of one kind, by ID.
func (s *Store) FavoriteIDs(userID, kind string) (map[string]bool, error) {
	rows, err := s.Query("SELECT object_id FROM user_favorites WHERE user_id = ? AND kind = ?", userID, kind)
	if err != nil {
		return nil, fmt.Errorf("listing favorites: %w", err)
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("listing favorites: %w", err)
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// AddFavorite stars something for a person. Starring it again changes
// nothing, and keeps when it was first starred. The caller checks it is
// there and theirs to see.
func (s *Store) AddFavorite(userID, kind, objectID string) (*Favorite, error) {
	if !validFavoriteKind(kind) {
		return nil, ErrInvalidFavorite
	}
	now := dbTime(time.Now())
	_, err := s.Exec("INSERT INTO user_favorites (user_id, kind, object_id, created_at) VALUES (?, ?, ?, ?)", userID, kind, objectID, now)
	if err != nil && !isUniqueViolation(err) {
		return nil, fmt.Errorf("adding a favorite: %w", err)
	}
	f := Favorite{Kind: kind, ObjectID: objectID}
	if err := s.QueryRow("SELECT created_at FROM user_favorites WHERE user_id = ? AND kind = ? AND object_id = ?", userID, kind, objectID).Scan(&f.CreatedAt); err != nil {
		return nil, fmt.Errorf("adding a favorite: %w", err)
	}
	f.CreatedAt = f.CreatedAt.UTC()
	return &f, nil
}

// RemoveFavorite unstars something for a person. One not starred is
// already as asked.
func (s *Store) RemoveFavorite(userID, kind, objectID string) error {
	if !validFavoriteKind(kind) {
		return ErrInvalidFavorite
	}
	if _, err := s.Exec("DELETE FROM user_favorites WHERE user_id = ? AND kind = ? AND object_id = ?", userID, kind, objectID); err != nil {
		return fmt.Errorf("removing a favorite: %w", err)
	}
	return nil
}
