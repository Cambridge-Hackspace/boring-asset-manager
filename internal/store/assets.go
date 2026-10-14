package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

var (
	ErrAssetNotFound       = errors.New("asset not found")
	ErrGeneratorCodeExists = errors.New("a generator code of this type is already assigned to another active asset")
	ErrDuplicateGenType    = errors.New("duplicate generator type specified for asset")
)

// normalizeCategories ensures an asset is assigned to Uncategorized if no categories are provided,
// or removes Uncategorized if user categories are provided.
func (s *Store) normalizeCategories(categoryIDs []string) ([]string, error) {
	_ = s.EnsureUncategorizedCategory()

	var normalized []string
	seen := make(map[string]bool)

	for _, cid := range categoryIDs {
		cid = strings.TrimSpace(cid)
		if cid == "" || cid == UncategorizedCategoryID || seen[cid] {
			continue
		}
		// Verify category exists
		if _, err := s.GetCategoryByID(cid); err != nil {
			return nil, fmt.Errorf("invalid category %q: %w", cid, err)
		}
		seen[cid] = true
		normalized = append(normalized, cid)
	}

	if len(normalized) == 0 {
		return []string{UncategorizedCategoryID}, nil
	}
	return normalized, nil
}

// normalizeGeneratorKeys trims assigned codes, rejects duplicate generator types and
// validates each code against its generator's character set and capacity constraints.
func normalizeGeneratorKeys(keys []models.AssetGeneratorKey) ([]models.AssetGeneratorKey, error) {
	normalized := make([]models.AssetGeneratorKey, 0, len(keys))
	genTypeSeen := make(map[string]bool)
	for _, gk := range keys {
		gk.AssignedCode = strings.TrimSpace(gk.AssignedCode)
		if genTypeSeen[gk.GeneratorType] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateGenType, gk.GeneratorType)
		}
		genTypeSeen[gk.GeneratorType] = true

		if err := generators.ValidateGeneratorCode(gk.GeneratorType, gk.AssignedCode); err != nil {
			return nil, err
		}
		normalized = append(normalized, gk)
	}
	return normalized, nil
}

// resolveFieldValues validates supplied field values against the effective field set (falling
// back to the global corpus for keys outside the asset's categories) and maps each field ID to
// its trimmed value. Empty values are kept so callers can treat them as deletions; unknown keys
// with empty values are ignored.
//
// It must run before a write transaction begins: the SQLite pool holds a single connection, so
// a non-transactional lookup issued while a transaction is open would block forever.
func (s *Store) resolveFieldValues(fieldMap map[string]models.EffectiveField, values map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(values))
	for key, val := range values {
		val = strings.TrimSpace(val)

		var f *models.Field
		if ef, ok := fieldMap[key]; ok {
			f = &ef.Field
		} else {
			corpusField, err := s.GetFieldByKey(key)
			if err != nil {
				if val == "" {
					continue
				}
				return nil, fmt.Errorf("unknown field key %q: %w", key, err)
			}
			f = corpusField
		}

		if err := fields.ValidateFieldValue(f, val); err != nil {
			// Name the field, as the required-field error does, so clients can
			// show the message beside it.
			return nil, fmt.Errorf("field %q (%s): %w", f.Label, f.Key, err)
		}
		resolved[f.ID] = val
	}
	return resolved, nil
}

// CreateAsset creates an asset with multi-category bindings, validated generator keys, and dynamic field values.
func (s *Store) CreateAsset(name string, categoryIDs []string, generatorKeys []models.AssetGeneratorKey, fieldValues map[string]string) (*models.AssetWithDetails, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("asset name cannot be empty")
	}

	// 1. Normalize categories (fallback to Uncategorized if empty)
	cats, err := s.normalizeCategories(categoryIDs)
	if err != nil {
		return nil, err
	}

	// 2. Validate generator keys
	generatorKeys, err = normalizeGeneratorKeys(generatorKeys)
	if err != nil {
		return nil, err
	}

	// 3. Resolve effective fields & validate field values
	effectiveFields, err := s.GetEffectiveFieldsForCategories(cats)
	if err != nil {
		return nil, fmt.Errorf("resolving effective fields: %w", err)
	}

	fieldMap := make(map[string]models.EffectiveField)
	for _, ef := range effectiveFields {
		fieldMap[ef.Field.Key] = ef
	}

	// Check required fields
	for _, ef := range effectiveFields {
		val := ""
		if fieldValues != nil {
			val = strings.TrimSpace(fieldValues[ef.Field.Key])
		}
		if ef.IsRequired && val == "" {
			return nil, fmt.Errorf("required field %q (%s) is missing", ef.Field.Label, ef.Field.Key)
		}
	}

	// Validate supplied values against field definitions (corpus fields outside the
	// asset's categories are accepted and retained)
	resolvedValues, err := s.resolveFieldValues(fieldMap, fieldValues)
	if err != nil {
		return nil, err
	}

	// 4. Persistence Transaction
	assetID := uuid.NewString()
	now := time.Now().UTC()

	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning asset transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert asset
	_, err = s.TxExec(tx, "INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", assetID, name, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting asset: %w", err)
	}

	// Insert categories
	for _, cid := range cats {
		_, err = s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", assetID, cid)
		if err != nil {
			return nil, fmt.Errorf("attaching category %s: %w", cid, err)
		}
	}

	// Insert generator keys
	for _, gk := range generatorKeys {
		_, err = s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", assetID, gk.GeneratorType, gk.AssignedCode)
		if err != nil {
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
				return nil, fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, gk.GeneratorType, gk.AssignedCode)
			}
			return nil, fmt.Errorf("inserting generator key: %w", err)
		}
	}

	// Insert field values
	for fieldID, val := range resolvedValues {
		if val == "" {
			continue
		}
		_, err = s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", assetID, fieldID, val, now)
		if err != nil {
			return nil, fmt.Errorf("inserting field value: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing asset: %w", err)
	}

	return s.GetAssetByID(assetID)
}

// GetAssetByID retrieves an asset and resolves its assigned categories, generator keys, and field values.
func (s *Store) GetAssetByID(id string) (*models.AssetWithDetails, error) {
	query := `SELECT id, name, created_at, updated_at, archived_at FROM assets WHERE id = ?`
	var a models.Asset
	err := s.QueryRow(query, id).Scan(&a.ID, &a.Name, &a.CreatedAt, &a.UpdatedAt, &a.ArchivedAt)
	if err == sql.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying asset: %w", err)
	}

	// Categories
	catQuery := `
		SELECT c.id, c.name, c.parent_id, c.color_hex, c.is_consumable, c.is_running_low, c.created_at, c.updated_at
		FROM asset_categories ac
		JOIN categories c ON ac.category_id = c.id
		WHERE ac.asset_id = ?
		ORDER BY c.name ASC
	`
	rows, err := s.Query(catQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying asset categories: %w", err)
	}
	defer rows.Close()

	var categories []models.Category
	var catIDs []string
	for rows.Next() {
		var (
			c         models.Category
			parentVal sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.Name, &parentVal, &c.ColorHex, &c.IsConsumable, &c.IsRunningLow, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if parentVal.Valid && parentVal.String != "" {
			c.ParentID = &parentVal.String
		}
		categories = append(categories, c)
		catIDs = append(catIDs, c.ID)
	}

	// Generator keys
	gkQuery := `SELECT asset_id, generator_type, assigned_code FROM asset_generator_keys WHERE asset_id = ? ORDER BY generator_type ASC`
	gkRows, err := s.Query(gkQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying generator keys: %w", err)
	}
	defer gkRows.Close()

	var generatorKeys []models.AssetGeneratorKey
	for gkRows.Next() {
		var gk models.AssetGeneratorKey
		if err := gkRows.Scan(&gk.AssetID, &gk.GeneratorType, &gk.AssignedCode); err != nil {
			return nil, err
		}
		generatorKeys = append(generatorKeys, gk)
	}

	// Field values
	fvQuery := `
		SELECT f.field_key, afv.value
		FROM asset_field_values afv
		JOIN fields f ON afv.field_id = f.id
		WHERE afv.asset_id = ?
	`
	fvRows, err := s.Query(fvQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying asset field values: %w", err)
	}
	defer fvRows.Close()

	fieldValues := make(map[string]string)
	for fvRows.Next() {
		var key, val string
		if err := fvRows.Scan(&key, &val); err != nil {
			return nil, err
		}
		fieldValues[key] = val
	}

	// Effective fields
	effectiveFields, err := s.GetEffectiveFieldsForCategories(catIDs)
	if err != nil {
		effectiveFields = []models.EffectiveField{}
	}

	// Lists are always arrays in the API, never null, even when empty.
	if categories == nil {
		categories = []models.Category{}
	}
	if generatorKeys == nil {
		generatorKeys = []models.AssetGeneratorKey{}
	}
	if effectiveFields == nil {
		effectiveFields = []models.EffectiveField{}
	}

	return &models.AssetWithDetails{
		Asset:           a,
		Categories:      categories,
		GeneratorKeys:   generatorKeys,
		FieldValues:     fieldValues,
		EffectiveFields: effectiveFields,
	}, nil
}

// ListAssets returns active assets, optionally filtered by category or search term.
func (s *Store) ListAssets(categoryID string, search string) ([]models.AssetWithDetails, error) {
	var (
		whereClauses []string
		args         []any
	)

	whereClauses = append(whereClauses, "a.archived_at IS NULL")

	if categoryID != "" {
		whereClauses = append(whereClauses, "a.id IN (SELECT asset_id FROM asset_categories WHERE category_id = ?)")
		args = append(args, categoryID)
	}

	if search != "" {
		// LOWER() on both sides keeps matching case-insensitive on PostgreSQL, whose LIKE is case-sensitive.
		likeTerm := "%" + strings.ToLower(search) + "%"
		whereClauses = append(whereClauses, "(LOWER(a.name) LIKE ? OR a.id IN (SELECT asset_id FROM asset_generator_keys WHERE LOWER(assigned_code) LIKE ?))")
		args = append(args, likeTerm, likeTerm)
	}

	query := fmt.Sprintf(`
		SELECT a.id
		FROM assets a
		WHERE %s
		ORDER BY a.created_at DESC
	`, strings.Join(whereClauses, " AND "))

	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing assets: %w", err)
	}
	defer rows.Close()

	var assetIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		assetIDs = append(assetIDs, id)
	}

	var results []models.AssetWithDetails
	for _, id := range assetIDs {
		detail, err := s.GetAssetByID(id)
		if err != nil {
			continue
		}
		results = append(results, *detail)
	}

	return results, nil
}

// UpdateAsset updates an asset, synchronizing category bindings and upserting field values while
// strictly obeying the Data Retention Rule (orphaned field values are retained).
//
// A nil categoryIDs or generatorKeys leaves that part of the asset as it is; an empty, non-nil
// slice clears it (an asset with no categories falls back to Uncategorized). An empty name
// keeps the current one, and fieldValues only touches the keys it names.
func (s *Store) UpdateAsset(id, name string, categoryIDs []string, generatorKeys []models.AssetGeneratorKey, fieldValues map[string]string) (*models.AssetWithDetails, error) {
	existing, err := s.GetAssetByID(id)
	if err != nil {
		return nil, err
	}
	if existing.Asset.ArchivedAt != nil {
		return nil, errors.New("cannot update an archived asset")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = existing.Asset.Name
	}

	if categoryIDs == nil {
		categoryIDs = make([]string, 0, len(existing.Categories))
		for _, c := range existing.Categories {
			categoryIDs = append(categoryIDs, c.ID)
		}
	}
	cats, err := s.normalizeCategories(categoryIDs)
	if err != nil {
		return nil, err
	}

	// Validate generator keys
	replaceKeys := generatorKeys != nil
	generatorKeys, err = normalizeGeneratorKeys(generatorKeys)
	if err != nil {
		return nil, err
	}

	// Merge field values: start with existing field values, apply new ones
	mergedValues := make(map[string]string)
	for k, v := range existing.FieldValues {
		mergedValues[k] = v
	}
	if fieldValues != nil {
		for k, v := range fieldValues {
			mergedValues[k] = strings.TrimSpace(v)
		}
	}

	// Resolve effective fields for new categories
	effectiveFields, err := s.GetEffectiveFieldsForCategories(cats)
	if err != nil {
		return nil, err
	}
	fieldMap := make(map[string]models.EffectiveField)
	for _, ef := range effectiveFields {
		fieldMap[ef.Field.Key] = ef
	}

	// Enforce required constraints on merged values
	for _, ef := range effectiveFields {
		val := mergedValues[ef.Field.Key]
		if ef.IsRequired && strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("required field %q (%s) is missing", ef.Field.Label, ef.Field.Key)
		}
	}

	// Validate newly provided field values
	resolvedValues, err := s.resolveFieldValues(fieldMap, fieldValues)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning update transaction: %w", err)
	}
	defer tx.Rollback()

	// Update asset name & updated_at
	_, err = s.TxExec(tx, "UPDATE assets SET name = ?, updated_at = ? WHERE id = ?", name, now, id)
	if err != nil {
		return nil, err
	}

	// Update categories
	_, err = s.TxExec(tx, "DELETE FROM asset_categories WHERE asset_id = ?", id)
	if err != nil {
		return nil, err
	}
	for _, cid := range cats {
		_, err = s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", id, cid)
		if err != nil {
			return nil, err
		}
	}

	// Replace generator keys when supplied, even with none
	if replaceKeys {
		_, err = s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ?", id)
		if err != nil {
			return nil, err
		}
		for _, gk := range generatorKeys {
			_, err = s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", id, gk.GeneratorType, gk.AssignedCode)
			if err != nil {
				errMsg := strings.ToLower(err.Error())
				if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
					return nil, fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, gk.GeneratorType, gk.AssignedCode)
				}
				return nil, err
			}
		}
	}

	// Upsert field values (Data Retention Rule: Existing values not mentioned are untouched!)
	for fieldID, val := range resolvedValues {
		// Delete prior value for this specific field and insert new one
		_, err = s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ? AND field_id = ?", id, fieldID)
		if err != nil {
			return nil, err
		}
		if val != "" {
			_, err = s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", id, fieldID, val, now)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing asset update: %w", err)
	}

	return s.GetAssetByID(id)
}

// DeleteAsset soft-deletes an asset by setting its archived_at timestamp and releasing its generator codes.
func (s *Store) DeleteAsset(id string) error {
	_, err := s.GetAssetByID(id)
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning archive transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	_, err = s.TxExec(tx, "UPDATE assets SET archived_at = ?, updated_at = ? WHERE id = ?", now, now, id)
	if err != nil {
		return fmt.Errorf("archiving asset: %w", err)
	}

	// Generator codes are unique only across active assets, so an archived asset
	// releases its codes for reassignment.
	_, err = s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ?", id)
	if err != nil {
		return fmt.Errorf("releasing generator keys: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing asset archive: %w", err)
	}
	return nil
}

// EffectiveFieldsForAsset resolves the fields an asset in these categories
// would have, before the asset exists: the same categories and inheritance
// CreateAsset uses, so no categories means Uncategorized.
func (s *Store) EffectiveFieldsForAsset(categoryIDs []string) ([]models.EffectiveField, error) {
	cats, err := s.normalizeCategories(categoryIDs)
	if err != nil {
		return nil, err
	}
	fields, err := s.GetEffectiveFieldsForCategories(cats)
	if fields == nil {
		fields = []models.EffectiveField{}
	}
	return fields, err
}
