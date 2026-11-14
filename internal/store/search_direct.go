package store

import (
	"database/sql"
	"fmt"
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Going straight to a search's one result (#389, docs/data-model.md#search):
// when a search finds exactly one asset, the web interface can open it,
// on its scan page where a scan template applies, instead of listing it.
// The instance sets whether it does for everyone, and each person may
// choose for themselves.

// SearchDirectSetting is the instance's setting: "true" opens a search's
// one result straight away for those who have not chosen. It is off
// until an admin turns it on, so searching lists what it finds.
const SearchDirectSetting = "search.direct"

// SearchDirect reports the instance's setting.
func (s *Store) SearchDirect() (bool, error) {
	v, err := s.Setting(SearchDirectSetting, "false")
	return v == "true", err
}

// SetSearchDirect sets the instance's setting.
func (s *Store) SetSearchDirect(on bool) error {
	return s.SetSetting(SearchDirectSetting, strconv.FormatBool(on))
}

// SetUserSearchDirect sets whether a search that finds exactly one asset
// opens it straight away for a user: on or off, or nil to follow the
// instance's setting. Like the theme, it is their preference, so
// updated_at is left as it is. Archived users cannot change it.
func (s *Store) SetUserSearchDirect(userID string, on *bool) (*models.User, error) {
	var stored any
	if on != nil {
		stored = *on
	}
	if _, err := s.Exec("UPDATE users SET search_direct = ? WHERE id = ? AND archived_at IS NULL", stored, userID); err != nil {
		return nil, fmt.Errorf("setting the user's search preference: %w", err)
	}
	u, err := s.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u.ArchivedAt != nil {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// boolOf is a nullable boolean column's value, nil for NULL.
func boolOf(b sql.NullBool) *bool {
	if !b.Valid {
		return nil
	}
	v := b.Bool
	return &v
}
