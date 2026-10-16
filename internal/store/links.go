package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

var (
	ErrRelationTypeNotFound = errors.New("relation type not found")
	// ErrRelationTypeInUse refuses deleting a type, or changing how it
	// behaves, while links use it.
	ErrRelationTypeInUse = errors.New("relation type is in use")
	// ErrInvalidRelationType wraps the reason a type cannot be saved.
	ErrInvalidRelationType = errors.New("invalid relation type")
	ErrLinkNotFound        = errors.New("link not found")
	ErrLinkExists          = errors.New("the assets are already linked that way")
	ErrLinkToSelf          = errors.New("an asset cannot be linked to itself")
	// ErrLinkCycle refuses a link that would put a thing inside itself.
	ErrLinkCycle = errors.New("that link would make a loop")
	// ErrContainerNotEmpty refuses archiving an asset that holds active
	// assets.
	ErrContainerNotEmpty = errors.New("it still holds other assets; move them out first")
)

// linksLock serializes changes to links (migration 015).
const linksLock = "asset_links"

// MaxRelationName is the longest name of a relation, in characters.
const MaxRelationName = 64

// RelationTypeInput is a relation type as created or edited.
type RelationTypeInput struct {
	Name, InverseName                 string
	IsSymmetric, IsSingle, IsLocation bool
}

func (in *RelationTypeInput) normalize() error {
	in.Name, in.InverseName = strings.TrimSpace(in.Name), strings.TrimSpace(in.InverseName)
	if in.IsSymmetric {
		// It reads the same from each end, so it is neither single nor a
		// location, which need a direction.
		if in.IsSingle || in.IsLocation {
			return fmt.Errorf("%w: a symmetric relation cannot be single or a location", ErrInvalidRelationType)
		}
		in.InverseName = in.Name
	}
	if in.IsLocation && !in.IsSingle {
		return fmt.Errorf("%w: a location relation must be single, since a thing is in one place", ErrInvalidRelationType)
	}
	for _, n := range []struct{ what, v string }{{"name", in.Name}, {"inverse name", in.InverseName}} {
		if n.v == "" {
			return fmt.Errorf("%w: a %s is required", ErrInvalidRelationType, n.what)
		}
		if err := textcheck.Check(n.what, n.v, MaxRelationName); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRelationType, err)
		}
	}
	return nil
}

const relationTypeColumns = "t.id, t.name, t.inverse_name, t.is_symmetric, t.is_single, t.is_location, t.created_at, t.updated_at"

// ListRelationTypes lists every relation type by name, with how many links
// use each.
func (s *Store) ListRelationTypes() ([]models.RelationType, error) {
	rows, err := s.Query("SELECT " + relationTypeColumns + ", (SELECT COUNT(*) FROM asset_links l WHERE l.relation_type_id = t.id) FROM relation_types t ORDER BY t.name")
	if err != nil {
		return nil, fmt.Errorf("listing relation types: %w", err)
	}
	defer rows.Close()
	types := []models.RelationType{}
	for rows.Next() {
		var t models.RelationType
		if err := rows.Scan(&t.ID, &t.Name, &t.InverseName, &t.IsSymmetric, &t.IsSingle, &t.IsLocation, &t.CreatedAt, &t.UpdatedAt, &t.LinkCount); err != nil {
			return nil, fmt.Errorf("scanning relation type: %w", err)
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

// GetRelationType returns a relation type, or ErrRelationTypeNotFound.
func (s *Store) GetRelationType(id string) (*models.RelationType, error) {
	types, err := s.ListRelationTypes()
	if err != nil {
		return nil, err
	}
	for i := range types {
		if types[i].ID == id {
			return &types[i], nil
		}
	}
	return nil, ErrRelationTypeNotFound
}

// CreateRelationType creates a relation type; a name in use is a unique
// violation.
func (s *Store) CreateRelationType(in RelationTypeInput) (*models.RelationType, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	_, err := s.Exec("INSERT INTO relation_types (id, name, inverse_name, is_symmetric, is_single, is_location, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, in.Name, in.InverseName, in.IsSymmetric, in.IsSingle, in.IsLocation, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting relation type: %w", err)
	}
	return s.GetRelationType(id)
}

// UpdateRelationType renames a relation type. How it behaves (symmetric,
// single, location) can change only while no links use it, since links made
// under the old rules might break the new ones.
func (s *Store) UpdateRelationType(id string, in RelationTypeInput) (*models.RelationType, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	was, err := s.GetRelationType(id)
	if err != nil {
		return nil, err
	}
	if was.LinkCount > 0 && (was.IsSymmetric != in.IsSymmetric || was.IsSingle != in.IsSingle || was.IsLocation != in.IsLocation) {
		return nil, fmt.Errorf("%w: %d links use it, so only its names can change", ErrRelationTypeInUse, was.LinkCount)
	}
	_, err = s.Exec("UPDATE relation_types SET name = ?, inverse_name = ?, is_symmetric = ?, is_single = ?, is_location = ?, updated_at = ? WHERE id = ?",
		in.Name, in.InverseName, in.IsSymmetric, in.IsSingle, in.IsLocation, time.Now().UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("updating relation type: %w", err)
	}
	return s.GetRelationType(id)
}

// DeleteRelationType deletes a relation type no link uses.
func (s *Store) DeleteRelationType(id string) error {
	t, err := s.GetRelationType(id)
	if err != nil {
		return err
	}
	if t.LinkCount > 0 {
		return fmt.Errorf("%w: %d links use it", ErrRelationTypeInUse, t.LinkCount)
	}
	if _, err := s.Exec("DELETE FROM relation_types WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting relation type: %w", err)
	}
	return nil
}

// CategoryRelationTypes lists the relation types a category suggests.
func (s *Store) CategoryRelationTypes(categoryID string) ([]string, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	rows, err := s.Query("SELECT relation_type_id FROM category_relation_types WHERE category_id = ? ORDER BY relation_type_id", categoryID)
	if err != nil {
		return nil, fmt.Errorf("reading suggested relations: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetCategoryRelationTypes replaces the relation types a category suggests.
func (s *Store) SetCategoryRelationTypes(categoryID string, typeIDs []string) ([]string, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM category_relation_types WHERE category_id = ?", categoryID); err != nil {
		return nil, fmt.Errorf("clearing suggested relations: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range typeIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM relation_types WHERE id = ?", id).Scan(&n); err != nil {
			return nil, fmt.Errorf("checking relation type: %w", err)
		}
		if n == 0 {
			return nil, fmt.Errorf("%w: %s", ErrRelationTypeNotFound, id)
		}
		if _, err := s.TxExec(tx, "INSERT INTO category_relation_types (category_id, relation_type_id) VALUES (?, ?)", categoryID, id); err != nil {
			return nil, fmt.Errorf("suggesting relation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.CategoryRelationTypes(categoryID)
}

// linkDetails fills in an asset's links, its location and the relation
// types its categories suggest.
func (s *Store) linkDetails(a *models.AssetWithDetails) error {
	a.Links, a.Location, a.SuggestedRelationTypeIDs = []models.AssetLink{}, []models.LinkedAsset{}, []string{}
	rows, err := s.Query(`SELECT l.id, t.id, t.name, t.inverse_name, l.from_asset_id = ?, o.id, o.name, o.archived_at
		FROM asset_links l
		JOIN relation_types t ON t.id = l.relation_type_id
		JOIN assets o ON o.id = CASE WHEN l.from_asset_id = ? THEN l.to_asset_id ELSE l.from_asset_id END
		WHERE l.from_asset_id = ? OR l.to_asset_id = ?`, a.Asset.ID, a.Asset.ID, a.Asset.ID, a.Asset.ID)
	if err != nil {
		return fmt.Errorf("reading links: %w", err)
	}
	for rows.Next() {
		var (
			l             models.AssetLink
			name, inverse string
			archived      sql.NullTime
		)
		if err := rows.Scan(&l.ID, &l.TypeID, &name, &inverse, &l.Outgoing, &l.Asset.ID, &l.Asset.Name, &archived); err != nil {
			rows.Close()
			return fmt.Errorf("scanning link: %w", err)
		}
		l.Label, l.Asset.Archived = inverse, archived.Valid
		if l.Outgoing {
			l.Label = name
		}
		a.Links = append(a.Links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Slice(a.Links, func(i, j int) bool {
		if a.Links[i].Label != a.Links[j].Label {
			return a.Links[i].Label < a.Links[j].Label
		}
		return a.Links[i].Asset.Name < a.Links[j].Asset.Name
	})

	// Where it is: up through location links, outermost first.
	seen := map[string]bool{a.Asset.ID: true}
	for at := a.Asset.ID; len(a.Location) < 50; {
		var (
			parent   models.LinkedAsset
			archived sql.NullTime
		)
		err := s.QueryRow(`SELECT o.id, o.name, o.archived_at FROM asset_links l
			JOIN relation_types t ON t.id = l.relation_type_id AND t.is_location = TRUE
			JOIN assets o ON o.id = l.from_asset_id
			WHERE l.to_asset_id = ? ORDER BY t.name LIMIT 1`, at).Scan(&parent.ID, &parent.Name, &archived)
		if err == sql.ErrNoRows || seen[parent.ID] {
			break
		}
		if err != nil {
			return fmt.Errorf("reading location: %w", err)
		}
		parent.Archived = archived.Valid
		seen[parent.ID] = true
		a.Location = append([]models.LinkedAsset{parent}, a.Location...)
		at = parent.ID
	}

	// Suggested types, from its categories and their ancestors.
	if len(a.Categories) == 0 {
		return nil
	}
	cats, err := s.ListCategories()
	if err != nil {
		return err
	}
	parents := map[string]string{}
	for _, c := range cats {
		if c.ParentID != nil {
			parents[c.ID] = *c.ParentID
		}
	}
	wanted := map[string]bool{}
	for _, c := range a.Categories {
		for id, depth := c.ID, 0; id != "" && depth < 100; id, depth = parents[id], depth+1 {
			wanted[id] = true
		}
	}
	rows, err = s.Query("SELECT category_id, relation_type_id FROM category_relation_types")
	if err != nil {
		return fmt.Errorf("reading suggested relations: %w", err)
	}
	defer rows.Close()
	suggested := map[string]bool{}
	for rows.Next() {
		var cat, typeID string
		if err := rows.Scan(&cat, &typeID); err != nil {
			return err
		}
		if wanted[cat] && !suggested[typeID] {
			suggested[typeID] = true
			a.SuggestedRelationTypeIDs = append(a.SuggestedRelationTypeIDs, typeID)
		}
	}
	sort.Strings(a.SuggestedRelationTypeIDs)
	return rows.Err()
}

// linkEnd is an asset at one end of a link, as a change records it.
type linkEnd struct {
	id, name string
}

// AddLink links an asset to another by a relation type, reading from the
// asset: outgoing means "asset <name> other" (Bin contains Screws), and
// otherwise "asset <inverse name> other" (Screws contained in Bin). For a
// single type, linking the far end anew moves it, ending its old link.
// Links that would make a loop of a single type are refused. Both assets
// record the change in their history.
func (s *Store) AddLink(assetID, typeID, otherID string, outgoing bool) (*models.AssetWithDetails, error) {
	if assetID == otherID {
		return nil, ErrLinkToSelf
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, linksLock); err != nil {
		return nil, err
	}
	var t models.RelationType
	err = s.TxQueryRow(tx, "SELECT id, name, inverse_name, is_symmetric, is_single FROM relation_types WHERE id = ?", typeID).
		Scan(&t.ID, &t.Name, &t.InverseName, &t.IsSymmetric, &t.IsSingle)
	if err == sql.ErrNoRows {
		return nil, ErrRelationTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading relation type: %w", err)
	}
	ends := map[string]linkEnd{}
	for _, id := range []string{assetID, otherID} {
		var archived sql.NullTime
		e := linkEnd{id: id}
		err := s.TxQueryRow(tx, "SELECT name, archived_at FROM assets WHERE id = ?", id).Scan(&e.name, &archived)
		if err == sql.ErrNoRows {
			return nil, ErrAssetNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("reading asset: %w", err)
		}
		if archived.Valid {
			return nil, ErrAssetArchived
		}
		ends[id] = e
	}
	from, to := ends[assetID], ends[otherID]
	if !outgoing && !t.IsSymmetric {
		from, to = to, from
	}

	var existing int
	err = s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_links WHERE relation_type_id = ? AND ((from_asset_id = ? AND to_asset_id = ?) OR (? AND from_asset_id = ? AND to_asset_id = ?))",
		t.ID, from.id, to.id, t.IsSymmetric, to.id, from.id).Scan(&existing)
	if err != nil {
		return nil, fmt.Errorf("checking links: %w", err)
	}
	if existing > 0 {
		return nil, ErrLinkExists
	}

	// The changes each asset's history records, by asset.
	changes := map[string][]models.EventChange{}
	if t.IsSingle {
		// A loop: from is already, at some depth, under to.
		for at, depth := from.id, 0; depth < 100; depth++ {
			var parent string
			err := s.TxQueryRow(tx, "SELECT from_asset_id FROM asset_links WHERE relation_type_id = ? AND to_asset_id = ?", t.ID, at).Scan(&parent)
			if err == sql.ErrNoRows {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("checking for a loop: %w", err)
			}
			if parent == to.id {
				return nil, ErrLinkCycle
			}
			at = parent
		}
		// A move: to leaves where it was.
		var oldID, oldFrom, oldName string
		err := s.TxQueryRow(tx, `SELECT l.id, a.id, a.name FROM asset_links l JOIN assets a ON a.id = l.from_asset_id
			WHERE l.relation_type_id = ? AND l.to_asset_id = ?`, t.ID, to.id).Scan(&oldID, &oldFrom, &oldName)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("checking where it is: %w", err)
		}
		if err == nil {
			if _, err := s.TxExec(tx, "DELETE FROM asset_links WHERE id = ?", oldID); err != nil {
				return nil, fmt.Errorf("moving: %w", err)
			}
			changes[oldFrom] = append(changes[oldFrom], models.EventChange{What: "link", Key: to.id, Label: t.Name, From: to.name})
			changes[to.id] = append(changes[to.id], models.EventChange{What: "link", Key: oldFrom, Label: t.InverseName, From: oldName})
		}
	}
	if _, err := s.TxExec(tx, "INSERT INTO asset_links (id, relation_type_id, from_asset_id, to_asset_id, created_at) VALUES (?, ?, ?, ?, ?)",
		uuid.NewString(), t.ID, from.id, to.id, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("linking: %w", err)
	}
	changes[from.id] = append(changes[from.id], models.EventChange{What: "link", Key: to.id, Label: t.Name, To: to.name})
	changes[to.id] = append(changes[to.id], models.EventChange{What: "link", Key: from.id, Label: t.InverseName, To: from.name})
	if err := s.recordLinkChanges(tx, changes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// RemoveLink removes one of an asset's links, recording it on both assets.
func (s *Store) RemoveLink(assetID, linkID string) (*models.AssetWithDetails, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, linksLock); err != nil {
		return nil, err
	}
	var (
		from, to, fromName, toName, name, inverse string
		archived                                  sql.NullTime
	)
	err = s.TxQueryRow(tx, `SELECT f.id, f.name, t.id, t.name, r.name, r.inverse_name, a.archived_at FROM asset_links l
		JOIN assets f ON f.id = l.from_asset_id JOIN assets t ON t.id = l.to_asset_id
		JOIN relation_types r ON r.id = l.relation_type_id JOIN assets a ON a.id = ?
		WHERE l.id = ? AND (l.from_asset_id = ? OR l.to_asset_id = ?)`, assetID, linkID, assetID, assetID).
		Scan(&from, &fromName, &to, &toName, &name, &inverse, &archived)
	if err == sql.ErrNoRows {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading link: %w", err)
	}
	if archived.Valid {
		return nil, ErrAssetArchived
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_links WHERE id = ?", linkID); err != nil {
		return nil, fmt.Errorf("unlinking: %w", err)
	}
	err = s.recordLinkChanges(tx, map[string][]models.EventChange{
		from: {{What: "link", Key: to, Label: name, From: toName}},
		to:   {{What: "link", Key: from, Label: inverse, From: fromName}},
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// recordLinkChanges records each asset's link changes as an edit of it.
func (s *Store) recordLinkChanges(tx *sql.Tx, changes map[string][]models.EventChange) error {
	ids := make([]string, 0, len(changes))
	for id := range changes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := s.recordAssetEvent(tx, models.EventAssetEdited, id, models.EventPayload{Trigger: models.TriggerManual, Changes: changes[id]}); err != nil {
			return err
		}
	}
	return nil
}

// holdsActiveTx counts the active assets an asset holds through location
// links.
func (s *Store) holdsActiveTx(tx *sql.Tx, id string) (int, error) {
	var n int
	err := s.TxQueryRow(tx, `SELECT COUNT(*) FROM asset_links l
		JOIN relation_types t ON t.id = l.relation_type_id AND t.is_location = TRUE
		JOIN assets a ON a.id = l.to_asset_id AND a.archived_at IS NULL
		WHERE l.from_asset_id = ?`, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("checking what it holds: %w", err)
	}
	return n, nil
}
