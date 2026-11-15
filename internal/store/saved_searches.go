package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Saved searches (#390, docs/data-model.md#saved-searches): a search,
// plain words or a query, kept under a name to run again. Each is its
// person's own; a shared one is listed for everyone who can read assets.

// SavedSearch is one saved search, with its person's name for those it is
// shared with.
type SavedSearch struct {
	ID        string
	UserID    string
	OwnerName string
	Name      string
	Search    string
	Shared    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Limits on a saved search, in characters.
const (
	SavedSearchNameMax = 100
	SavedSearchMax     = 2000
)

var (
	// ErrSavedSearchNotFound is a saved search that is not there, or not
	// the person's to see.
	ErrSavedSearchNotFound = errors.New("saved search not found")
	// ErrSavedSearchNameTaken is a name the person already saved a search under.
	ErrSavedSearchNameTaken = errors.New("you already have a saved search of that name")
	// ErrInvalidSavedSearch is a saved search without a name or a search, or too long.
	ErrInvalidSavedSearch = fmt.Errorf("a saved search needs a name, of up to %d characters, and a search, of up to %d", SavedSearchNameMax, SavedSearchMax)
)

func cleanSavedSearch(name, search string) (string, string, error) {
	name, search = strings.TrimSpace(name), strings.TrimSpace(search)
	if name == "" || search == "" || utf8.RuneCountInString(name) > SavedSearchNameMax || utf8.RuneCountInString(search) > SavedSearchMax {
		return "", "", ErrInvalidSavedSearch
	}
	return name, search, nil
}

const savedSearchColumns = `s.id, s.user_id, COALESCE(NULLIF(u.name, ''), u.email), s.name, s.search_text, s.is_shared, s.created_at, s.updated_at
	FROM saved_searches s JOIN users u ON u.id = s.user_id`

func scanSavedSearch(row interface{ Scan(...any) error }) (*SavedSearch, error) {
	var v SavedSearch
	if err := row.Scan(&v.ID, &v.UserID, &v.OwnerName, &v.Name, &v.Search, &v.Shared, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.CreatedAt, v.UpdatedAt = v.CreatedAt.UTC(), v.UpdatedAt.UTC()
	return &v, nil
}

// SavedSearches lists a person's own saved searches and those shared with
// everyone, theirs first, each by name. userID empty, for someone signed
// out, lists the shared ones alone.
func (s *Store) SavedSearches(userID string) ([]SavedSearch, error) {
	rows, err := s.Query("SELECT "+savedSearchColumns+" WHERE s.user_id = ? OR s.is_shared = ?", userID, true)
	if err != nil {
		return nil, fmt.Errorf("listing saved searches: %w", err)
	}
	defer rows.Close()
	out := []SavedSearch{}
	for rows.Next() {
		v, err := scanSavedSearch(rows)
		if err != nil {
			return nil, fmt.Errorf("listing saved searches: %w", err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing saved searches: %w", err)
	}
	sortSavedSearches(out, userID)
	return out, nil
}

// sortSavedSearches puts the person's own first, then by name, ignoring
// case, then by ID so the order is the same every time.
func sortSavedSearches(list []SavedSearch, userID string) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if (a.UserID == userID) != (b.UserID == userID) {
			return a.UserID == userID
		}
		if la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name); la != lb {
			return la < lb
		}
		return a.ID < b.ID
	})
}

// GetSavedSearch is one saved search, when it is the person's own or
// shared; userID empty sees shared ones alone.
func (s *Store) GetSavedSearch(id, userID string) (*SavedSearch, error) {
	v, err := scanSavedSearch(s.QueryRow("SELECT "+savedSearchColumns+" WHERE s.id = ? AND (s.user_id = ? OR s.is_shared = ?)", id, userID, true))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSavedSearchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading a saved search: %w", err)
	}
	return v, nil
}

// nameTaken reports whether the person has another saved search of this
// name, ignoring case; except is the one being renamed.
func (s *Store) savedSearchNameTaken(userID, name, except string) (bool, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM saved_searches WHERE user_id = ? AND LOWER(name) = LOWER(?) AND id <> ?", userID, name, except).Scan(&n); err != nil {
		return false, fmt.Errorf("checking a saved search's name: %w", err)
	}
	return n > 0, nil
}

// CreateSavedSearch saves a search under a name for a person. The caller
// checks they may share it.
func (s *Store) CreateSavedSearch(userID, name, search string, shared bool) (*SavedSearch, error) {
	name, search, err := cleanSavedSearch(name, search)
	if err != nil {
		return nil, err
	}
	if taken, err := s.savedSearchNameTaken(userID, name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrSavedSearchNameTaken
	}
	id, now := uuid.NewString(), dbTime(time.Now())
	if _, err := s.Exec("INSERT INTO saved_searches (id, user_id, name, search_text, is_shared, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, userID, name, search, shared, now, now); err != nil {
		return nil, fmt.Errorf("saving a search: %w", err)
	}
	return s.GetSavedSearch(id, userID)
}

// UpdateSavedSearch renames a person's own saved search, changes its
// search, or shares or stops sharing it. Another's is not found. The
// caller checks they may share it.
func (s *Store) UpdateSavedSearch(id, userID, name, search string, shared bool) (*SavedSearch, error) {
	name, search, err := cleanSavedSearch(name, search)
	if err != nil {
		return nil, err
	}
	if taken, err := s.savedSearchNameTaken(userID, name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrSavedSearchNameTaken
	}
	res, err := s.Exec("UPDATE saved_searches SET name = ?, search_text = ?, is_shared = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		name, search, shared, dbTime(time.Now()), id, userID)
	if err != nil {
		return nil, fmt.Errorf("changing a saved search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// MariaDB counts rows changed, not matched: one saved again as it
		// was is still there.
		if _, err := s.ownSavedSearch(id, userID); err != nil {
			return nil, err
		}
	}
	return s.GetSavedSearch(id, userID)
}

func (s *Store) ownSavedSearch(id, userID string) (*SavedSearch, error) {
	v, err := scanSavedSearch(s.QueryRow("SELECT "+savedSearchColumns+" WHERE s.id = ? AND s.user_id = ?", id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSavedSearchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading a saved search: %w", err)
	}
	return v, nil
}

// DeleteSavedSearch deletes a person's own saved search. Another's is not
// found.
func (s *Store) DeleteSavedSearch(id, userID string) error {
	res, err := s.Exec("DELETE FROM saved_searches WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("deleting a saved search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSavedSearchNotFound
	}
	return nil
}
