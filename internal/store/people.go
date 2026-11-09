package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// People in fields (docs/data-model.md#person-fields): a person field names
// people by user ID, in asset_field_values, and asset_field_people indexes
// whom each names. A person without an account is a user who cannot sign
// in (users.can_sign_in false): a name, with an email or none, no password,
// no identity and no roles.

var (
	// ErrPersonNotFound is returned when nothing names a person.
	ErrPersonNotFound = errors.New("no one has that name or email")
	// ErrPersonAmbiguous is returned when a name is several people's.
	ErrPersonAmbiguous = errors.New("several people have that name")
	// ErrHasAccount is returned for merging someone who can sign in.
	ErrHasAccount = errors.New("only a person without an account can be merged into another")
	// ErrNoAccount is returned for giving roles to a person without an
	// account.
	ErrNoAccount = errors.New("a person without an account holds no roles: set a password or invite them first")
	// ErrFieldInUse is returned for making a field a person field, or one
	// no longer, while assets have values for it.
	ErrFieldInUse = errors.New("a field becomes a person field, or stops being one, only while no asset has a value for it")
)

// noEmailPrefix begins the email stored for a person without one, which
// is followed by their ID so that it stays unique. No one signs in or is
// found by it, and it reads as no email.
const noEmailPrefix = "!"

// shownEmail is a users row's email as it reads: none for a person
// without one.
func shownEmail(stored string) string {
	if strings.HasPrefix(stored, noEmailPrefix) {
		return ""
	}
	return stored
}

// CreatePerson adds a person without an account, to name in fields: a
// display name, which is required, and an email, which is not. An email
// already someone's is ErrEmailTaken.
func (s *Store) CreatePerson(name, email string) (*models.User, error) {
	name, err := normalizeUserName(name)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fmt.Errorf("%w: a name is required", ErrInvalidUser)
	}
	id := uuid.NewString()
	stored := auth.NormalizeEmail(email)
	switch {
	case stored == "":
		stored = noEmailPrefix + id
	case strings.HasPrefix(stored, noEmailPrefix):
		return nil, fmt.Errorf("%w: an email cannot begin with %s", ErrInvalidUser, noEmailPrefix)
	default:
		if err := textcheck.Check("email", stored, textcheck.MaxName); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidUser, err)
		}
	}
	now := dbTime(time.Now())
	if _, err := s.Exec("INSERT INTO users (id, email, name, password_hash, is_active, can_sign_in, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, stored, name, NoPassword, true, false, now, now); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("inserting person: %w", err)
	}
	return s.GetUserByID(id)
}

// FieldPeople are the people with these IDs as person fields show them,
// by ID: archived ones too, marked inactive, as their fields stay set. An
// ID naming no one is left out.
func (s *Store) FieldPeople(ids []string) (map[string]models.FieldPerson, error) {
	return s.fieldPeopleTx(nil, ids)
}

func (s *Store) fieldPeopleTx(tx *sql.Tx, ids []string) (map[string]models.FieldPerson, error) {
	out := map[string]models.FieldPerson{}
	if len(ids) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := "SELECT id, name, is_active, archived_at, can_sign_in FROM users WHERE id IN (" + marks + ")"
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = s.TxQuery(tx, query, args...)
	} else {
		rows, err = s.Query(query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the people in fields: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p models.FieldPerson
		var active, signsIn bool
		var archived sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &active, &archived, &signsIn); err != nil {
			return nil, err
		}
		p.Name = PersonName(models.Person{Name: p.Name})
		p.Inactive = !active || archived.Valid
		p.NoAccount = !signsIn
		out[p.ID] = p
	}
	return out, rows.Err()
}

// ChoosablePeople are the people a person field in these scopes may be set
// to: the active people who may read an asset in them, and every person
// without an account, by name. Whoever asks must see the people around
// such assets (the API checks assets.track).
func (s *Store) ChoosablePeople(scopes []string) ([]models.FieldPerson, error) {
	readers, err := s.PeopleWhoRead(models.CommentOnAsset, scopes)
	if err != nil {
		return nil, err
	}
	out := make([]models.FieldPerson, 0, len(readers))
	for _, p := range readers {
		out = append(out, models.FieldPerson{ID: p.ID, Name: p.Name})
	}
	rows, err := s.Query("SELECT id, name FROM users WHERE can_sign_in = FALSE AND is_active = TRUE AND archived_at IS NULL")
	if err != nil {
		return nil, fmt.Errorf("listing people without accounts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p models.FieldPerson
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		p.Name = PersonName(models.Person{Name: p.Name})
		p.NoAccount = true
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// MatchPeople keeps the people whose name holds q, ignoring case, those
// whose name starts with it first; an empty q keeps them all.
func MatchPeople(people []models.FieldPerson, q string) []models.FieldPerson {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return people
	}
	var first, rest []models.FieldPerson
	for _, p := range people {
		name := strings.ToLower(p.Name)
		switch {
		case strings.HasPrefix(name, q):
			first = append(first, p)
		case strings.Contains(name, q):
			rest = append(rest, p)
		}
	}
	return append(first, rest...)
}

// FindPerson finds the one person ref names, who is not archived: by user
// ID, by email, or by a display name only one person has, ignoring case.
// A name several people have is ErrPersonAmbiguous, and one no one has
// ErrPersonNotFound.
func (s *Store) FindPerson(ref string) (*models.User, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrPersonNotFound
	}
	if u, err := s.GetUserByID(ref); err == nil && u.ArchivedAt == nil {
		return u, nil
	} else if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	if u, err := s.GetUserByEmail(ref); err == nil {
		return u, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	rows, err := s.Query("SELECT id FROM users WHERE LOWER(name) = ? AND archived_at IS NULL", strings.ToLower(ref))
	if err != nil {
		return nil, fmt.Errorf("finding people by name: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch len(ids) {
	case 0:
		return nil, ErrPersonNotFound
	case 1:
		return s.GetUserByID(ids[0])
	}
	return nil, ErrPersonAmbiguous
}

// isPersonField reports whether a field, by ID, is a person field.
func (s *Store) isPersonFieldTx(tx *sql.Tx, fieldID string) (bool, error) {
	var kind string
	err := s.TxQueryRow(tx, "SELECT validation_type FROM fields WHERE id = ?", fieldID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the field: %w", err)
	}
	return kind == fields.TypePerson, nil
}

// syncFieldPeopleTx keeps asset_field_people with an asset's value for a
// field, written within tx: the people it names, when it is a person
// field, and none otherwise. IDs that name no one are left out.
func (s *Store) syncFieldPeopleTx(tx *sql.Tx, assetID, fieldID, value string) error {
	if _, err := s.TxExec(tx, "DELETE FROM asset_field_people WHERE asset_id = ? AND field_id = ?", assetID, fieldID); err != nil {
		return fmt.Errorf("clearing the people in a field: %w", err)
	}
	if value == "" {
		return nil
	}
	person, err := s.isPersonFieldTx(tx, fieldID)
	if err != nil || !person {
		return err
	}
	ids := fields.PersonIDs(value)
	known, err := s.fieldPeopleTx(tx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			continue
		}
		if _, err := s.TxExec(tx, "INSERT INTO asset_field_people (asset_id, field_id, user_id) VALUES (?, ?, ?)", assetID, fieldID, id); err != nil {
			return fmt.Errorf("recording the people in a field: %w", err)
		}
	}
	return nil
}

// checkPeopleExist refuses a person field's value naming someone who has
// no account here, archived or not.
func (s *Store) checkPeopleExist(f *models.Field, value string) error {
	ids := fields.PersonIDs(value)
	known, err := s.FieldPeople(ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			return fmt.Errorf("field %q (%s): %w: no one has the ID %q", f.Label, f.Key, fields.ErrInvalidFieldValue, id)
		}
	}
	return nil
}

// fieldHasValues reports whether any asset has a value for a field.
func (s *Store) fieldHasValues(fieldID string) (bool, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM asset_field_values WHERE field_id = ?", fieldID).Scan(&n); err != nil {
		return false, fmt.Errorf("counting the field's values: %w", err)
	}
	return n > 0, nil
}

// MergePerson merges a person without an account into another person, by
// ID: every field that named the first names the second instead, each
// asset's history saying so, and the first is archived. It answers how
// many assets changed.
func (s *Store) MergePerson(fromID, intoID string) (int, error) {
	if fromID == intoID {
		return 0, fmt.Errorf("%w: a person cannot be merged into themselves", ErrInvalidUser)
	}
	from, err := s.GetUserByID(fromID)
	if err != nil {
		return 0, err
	}
	if from.ArchivedAt != nil {
		return 0, ErrUserNotFound
	}
	if from.CanSignIn {
		return 0, ErrHasAccount
	}
	into, err := s.GetUserByID(intoID)
	if err != nil {
		return 0, err
	}
	if into.ArchivedAt != nil {
		return 0, ErrUserNotFound
	}
	note := "Merged " + PersonName(models.Person{Name: from.Name}) + " into " + PersonName(models.Person{Name: into.Name}) + "."
	changed := 0
	err = s.withInventory(func(ic *inventoryChange) error {
		rows, err := s.TxQuery(ic.tx, `SELECT p.asset_id, p.field_id, v.value FROM asset_field_people p
			JOIN asset_field_values v ON v.asset_id = p.asset_id AND v.field_id = p.field_id
			WHERE p.user_id = ? ORDER BY p.asset_id, p.field_id`, fromID)
		if err != nil {
			return fmt.Errorf("finding the fields naming them: %w", err)
		}
		type naming struct{ asset, field, value string }
		var found []naming
		for rows.Next() {
			var n naming
			if err := rows.Scan(&n.asset, &n.field, &n.value); err != nil {
				rows.Close()
				return err
			}
			found = append(found, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		byAsset := map[string][]naming{}
		var assets []string
		for _, n := range found {
			if _, ok := byAsset[n.asset]; !ok {
				assets = append(assets, n.asset)
			}
			byAsset[n.asset] = append(byAsset[n.asset], n)
		}
		now := time.Now().UTC()
		for _, assetID := range assets {
			before, err := s.snapshotAssetTx(ic.tx, assetID)
			if err != nil {
				return err
			}
			for _, n := range byAsset[assetID] {
				ids := fields.PersonIDs(n.value)
				for i, id := range ids {
					if id == fromID {
						ids[i] = intoID
					}
				}
				value := fields.PersonValue(ids)
				if _, err := s.TxExec(ic.tx, "UPDATE asset_field_values SET value = ?, updated_at = ? WHERE asset_id = ? AND field_id = ?", value, now, assetID, n.field); err != nil {
					return fmt.Errorf("moving a field to them: %w", err)
				}
				if err := s.syncFieldPeopleTx(ic.tx, assetID, n.field, value); err != nil {
					return err
				}
			}
			after, err := s.snapshotAssetTx(ic.tx, assetID)
			if err != nil {
				return err
			}
			if err := s.recordAssetEvent(ic.tx, models.EventAssetEdited, assetID, models.EventPayload{Trigger: models.TriggerManual,
				Changes: diffAssets(before, after), Note: note}); err != nil {
				return err
			}
			changed++
		}
		// Whatever else is theirs goes with them: their mentions stay, and
		// they leave the user list.
		_, err = s.TxExec(ic.tx, "UPDATE users SET archived_at = ?, is_active = ?, updated_at = ? WHERE id = ?", now, false, now, fromID)
		return err
	})
	return changed, err
}

// allowSignIn lets a person without an account sign in from now on, once
// a way to has been given them: a password, or an identity.
func (s *Store) allowSignIn(userID string) error {
	if _, err := s.Exec("UPDATE users SET can_sign_in = TRUE WHERE id = ? AND can_sign_in = FALSE", userID); err != nil {
		return fmt.Errorf("letting them sign in: %w", err)
	}
	return nil
}

// JoinPerson joins a first sign-in through a provider to a person without
// an account, by their user ID: the identity is linked to them, and they
// can sign in from now on, their fields already theirs. Only a person who
// cannot sign in, has no password and no identity is joined; anyone else
// is ErrHasAccount, and the identity is not linked.
func (s *Store) JoinPerson(userID, providerID, subject, email string) (*models.User, error) {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u.ArchivedAt != nil || !u.IsActive {
		return nil, ErrUserNotFound
	}
	identities, err := s.UserIdentities(userID)
	if err != nil {
		return nil, err
	}
	if u.CanSignIn || HasPassword(u) || len(identities) > 0 {
		return nil, ErrHasAccount
	}
	if _, err := s.LinkIdentity(userID, providerID, subject, email); err != nil {
		return nil, err
	}
	return s.GetUserByID(userID)
}

// fieldPeople are the people with these IDs as FieldPeople reads them,
// reading only those the context has not read yet; nil for none.
func (ctx *assetContext) fieldPeople(s *Store, ids []string) (map[string]models.FieldPerson, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if ctx.people == nil {
		ctx.people = map[string]models.FieldPerson{}
	}
	var missing []string
	asked := map[string]bool{}
	for _, id := range ids {
		if _, ok := ctx.people[id]; !ok && !asked[id] {
			asked[id] = true
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		got, err := s.FieldPeople(missing)
		if err != nil {
			return nil, err
		}
		for _, id := range missing {
			// An ID naming no one is remembered as no one.
			ctx.people[id] = got[id]
		}
	}
	out := map[string]models.FieldPerson{}
	for _, id := range ids {
		if p := ctx.people[id]; p.ID != "" {
			out[id] = p
		}
	}
	return out, nil
}

// fieldNamesSeveral reports whether any asset's value for a field names
// more than one person.
func (s *Store) fieldNamesSeveral(fieldID string) (bool, error) {
	var n int
	if err := s.QueryRow(`SELECT COUNT(*) FROM (SELECT asset_id FROM asset_field_people WHERE field_id = ?
		GROUP BY asset_id HAVING COUNT(*) > 1) several`, fieldID).Scan(&n); err != nil {
		return false, fmt.Errorf("counting the people in the field: %w", err)
	}
	return n > 0, nil
}

// peopleOfImportedTx keeps the person fields of assets an import brought
// naming only people here: an archive from another instance names its
// own, whom no one here is. Their people are recorded in
// asset_field_people, which an import does not bring.
func (s *Store) peopleOfImportedTx(tx *sql.Tx, assetIDs []string) error {
	for _, id := range assetIDs {
		rows, err := s.TxQuery(tx, `SELECT v.field_id, v.value FROM asset_field_values v JOIN fields f ON f.id = v.field_id
			WHERE v.asset_id = ? AND f.validation_type = ?`, id, fields.TypePerson)
		if err != nil {
			return fmt.Errorf("reading the imported person fields: %w", err)
		}
		values := map[string]string{}
		for rows.Next() {
			var fieldID, value string
			if err := rows.Scan(&fieldID, &value); err != nil {
				rows.Close()
				return err
			}
			values[fieldID] = value
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for fieldID, value := range values {
			ids := fields.PersonIDs(value)
			known, err := s.fieldPeopleTx(tx, ids)
			if err != nil {
				return err
			}
			var kept []string
			for _, pid := range ids {
				if _, ok := known[pid]; ok {
					kept = append(kept, pid)
				}
			}
			switch {
			case len(kept) == 0:
				_, err = s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ? AND field_id = ?", id, fieldID)
			case len(kept) < len(ids):
				_, err = s.TxExec(tx, "UPDATE asset_field_values SET value = ? WHERE asset_id = ? AND field_id = ?", fields.PersonValue(kept), id, fieldID)
			}
			if err != nil {
				return fmt.Errorf("keeping the imported person fields: %w", err)
			}
			if err := s.syncFieldPeopleTx(tx, id, fieldID, fields.PersonValue(kept)); err != nil {
				return err
			}
		}
	}
	return nil
}
