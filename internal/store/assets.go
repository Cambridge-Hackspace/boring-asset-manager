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
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
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
	return s.CreateAssetWithStock(name, categoryIDs, generatorKeys, fieldValues, nil)
}

// CreateAssetWithStock creates an asset as CreateAsset does, counting its
// stock by quantity when stock is not nil.
func (s *Store) CreateAssetWithStock(name string, categoryIDs []string, generatorKeys []models.AssetGeneratorKey, fieldValues map[string]string, stock *StockInput) (*models.AssetWithDetails, error) {
	if stock != nil {
		if err := stock.normalize(); err != nil {
			return nil, err
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("asset name cannot be empty")
	}
	if err := textcheck.Check("asset name", name, textcheck.MaxName); err != nil {
		return nil, err
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

	err = s.withInventory(func(ic *inventoryChange) error {
		if err := s.insertAsset(ic.tx, assetID, name, now, cats, generatorKeys, resolvedValues); err != nil {
			return err
		}
		var recordQuantity func() error
		if stock != nil {
			if recordQuantity, err = s.setStockTx(ic.tx, assetID, nil, stock); err != nil {
				return err
			}
		}
		return s.recordAfter(ic.tx, assetID, nil, models.TriggerManual, recordQuantity)
	})
	if err != nil {
		return nil, err
	}

	return s.GetAssetByID(assetID)
}

// insertAsset writes a new, validated asset within tx.
func (s *Store) insertAsset(tx *sql.Tx, assetID, name string, now time.Time, cats []string, generatorKeys []models.AssetGeneratorKey, resolvedValues map[string]string) error {
	// Insert asset
	_, err := s.TxExec(tx, "INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", assetID, name, now, now)
	if err != nil {
		return fmt.Errorf("inserting asset: %w", err)
	}

	// Insert categories
	for _, cid := range cats {
		_, err = s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", assetID, cid)
		if err != nil {
			return fmt.Errorf("attaching category %s: %w", cid, err)
		}
	}

	// Insert generator keys
	for _, gk := range generatorKeys {
		_, err = s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", assetID, gk.GeneratorType, gk.AssignedCode)
		if err != nil {
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
				return fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, gk.GeneratorType, gk.AssignedCode)
			}
			return fmt.Errorf("inserting generator key: %w", err)
		}
	}

	// Insert field values
	for fieldID, val := range resolvedValues {
		if val == "" {
			continue
		}
		_, err = s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", assetID, fieldID, val, now)
		if err != nil {
			return fmt.Errorf("inserting field value: %w", err)
		}
	}
	return nil
}

// GetAssetByID retrieves an asset and resolves its assigned categories, generator keys, and field values.
func (s *Store) GetAssetByID(id string) (*models.AssetWithDetails, error) {
	return s.readAsset(id, &assetContext{})
}

// readAsset reads an asset as GetAssetByID does, taking what it needs from
// whole tables from ctx, which a list shares across its assets.
func (s *Store) readAsset(id string, ctx *assetContext) (*models.AssetWithDetails, error) {
	query := `SELECT id, name, created_at, updated_at, archived_at, asset_condition, condition_note, ` + stockColumns + ` FROM assets WHERE id = ?`
	var (
		a  models.Asset
		sc stockScan
	)
	err := s.QueryRow(query, id).Scan(append([]any{&a.ID, &a.Name, &a.CreatedAt, &a.UpdatedAt, &a.ArchivedAt, &a.Condition, &a.ConditionNote}, sc.dest()...)...)
	if err == sql.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying asset: %w", err)
	}
	a.Stock = sc.stock()

	// Categories
	catQuery := `
		SELECT ` + categoryColumns("c.") + `
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
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
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

	details := &models.AssetWithDetails{
		Asset:           a,
		Categories:      categories,
		GeneratorKeys:   generatorKeys,
		FieldValues:     fieldValues,
		EffectiveFields: effectiveFields,
	}
	if err := s.loanDetails(details, ctx); err != nil {
		return nil, err
	}
	if err := s.linkDetails(details, ctx); err != nil {
		return nil, err
	}
	if err := s.photoDetails(details, ctx); err != nil {
		return nil, err
	}
	traits, err := s.contextTraits(ctx)
	if err != nil {
		return nil, err
	}
	details.Scopes = traits.scopesOf(catIDs)
	details.LabelTemplate = traits.chooseLabel(catIDs)
	details.Actions = traits.offered(catIDs)
	if details.ScanTemplate, err = s.contextScan(ctx, catIDs); err != nil {
		return nil, err
	}
	return details, nil
}

// ListAssets returns active assets, optionally filtered by category or search term.
func (s *Store) ListAssets(categoryID string, search string) ([]models.AssetWithDetails, error) {
	return s.FilterAssets(AssetFilter{CategoryID: categoryID, Search: search})
}

// ListArchivedAssets returns archived assets, most recently archived first,
// filtered as ListAssets filters active ones.
func (s *Store) ListArchivedAssets(categoryID string, search string) ([]models.AssetWithDetails, error) {
	return s.FilterAssets(AssetFilter{CategoryID: categoryID, Search: search, Archived: true})
}

// ConditionProblem, as AssetFilter.Condition, matches every asset whose
// condition is not ok.
const ConditionProblem = "problem"

// ErrInvalidCondition is returned for a condition that is not one of
// models.ValidCondition's, or a filter that is neither one nor
// ConditionProblem.
var ErrInvalidCondition = errors.New("condition must be ok, needs_maintenance or out_of_service")

// AssetFilter chooses which assets a list holds. Empty fields match every
// asset; Archived lists archived assets instead of active ones.
type AssetFilter struct {
	CategoryID string
	Search     string
	Archived   bool
	// Condition is a condition, or ConditionProblem.
	Condition string
	// Loan is LoansOpen for assets lent out, LoansOverdue for those past
	// due, or LoansAll.
	Loan string
}

// FilterAssets lists the assets a filter matches: active ones newest first,
// archived ones most recently archived first.
func (s *Store) FilterAssets(f AssetFilter) ([]models.AssetWithDetails, error) {
	categoryID, search, archived := f.CategoryID, f.Search, f.Archived
	var (
		whereClauses []string
		args         []any
		order        = "a.created_at DESC"
	)

	if archived {
		whereClauses = append(whereClauses, "a.archived_at IS NOT NULL")
		order = "a.archived_at DESC, a.created_at DESC"
	} else {
		whereClauses = append(whereClauses, "a.archived_at IS NULL")
	}

	if categoryID != "" {
		whereClauses = append(whereClauses, "a.id IN (SELECT asset_id FROM asset_categories WHERE category_id = ?)")
		args = append(args, categoryID)
	}

	switch {
	case f.Condition == "":
	case f.Condition == ConditionProblem:
		whereClauses = append(whereClauses, "a.asset_condition <> ?")
		args = append(args, models.ConditionOK)
	case models.ValidCondition(f.Condition):
		whereClauses = append(whereClauses, "a.asset_condition = ?")
		args = append(args, f.Condition)
	default:
		return nil, ErrInvalidCondition
	}

	switch f.Loan {
	case LoansAll:
	case LoansOpen:
		whereClauses = append(whereClauses, "a.id IN (SELECT asset_id FROM asset_loans WHERE checked_in_at IS NULL)")
	case LoansOverdue:
		whereClauses = append(whereClauses, "a.id IN (SELECT asset_id FROM asset_loans WHERE checked_in_at IS NULL AND due_at < ?)")
		args = append(args, time.Now().UTC())
	default:
		return nil, ErrInvalidLoanFilter
	}

	query := fmt.Sprintf(`
		SELECT a.id
		FROM assets a
		WHERE %s
		ORDER BY %s
	`, strings.Join(whereClauses, " AND "), order)

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

	// The search is matched here rather than with LIKE, which treats % and _
	// as wildcards and, on some databases, backslash as an escape, and whose
	// LOWER() folds only ASCII letters on SQLite. Here every letter folds,
	// the same way whatever the database.
	term := strings.ToLower(search)
	matches := func(a *models.AssetWithDetails) bool {
		if strings.Contains(strings.ToLower(a.Asset.Name), term) {
			return true
		}
		for _, gk := range a.GeneratorKeys {
			if strings.Contains(strings.ToLower(gk.AssignedCode), term) {
				return true
			}
		}
		return false
	}

	var results []models.AssetWithDetails
	ctx := &assetContext{list: true}
	for _, id := range assetIDs {
		detail, err := s.readAsset(id, ctx)
		if err != nil {
			continue
		}
		if search != "" && !matches(detail) {
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
	return s.UpdateAssetWithStock(id, name, categoryIDs, generatorKeys, fieldValues, nil)
}

// StockUpdate replaces an asset's stock with Stock; a nil Stock stops
// counting it by quantity.
type StockUpdate struct {
	Stock *StockInput
}

// UpdateAssetWithStock updates an asset as UpdateAsset does, and replaces its
// stock when stock is not nil.
func (s *Store) UpdateAssetWithStock(id, name string, categoryIDs []string, generatorKeys []models.AssetGeneratorKey, fieldValues map[string]string, stock *StockUpdate) (*models.AssetWithDetails, error) {
	if stock != nil && stock.Stock != nil {
		if err := stock.Stock.normalize(); err != nil {
			return nil, err
		}
	}
	existing, err := s.GetAssetByID(id)
	if err != nil {
		return nil, err
	}
	if existing.Asset.ArchivedAt != nil {
		return nil, ErrAssetArchived
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = existing.Asset.Name
	}
	if err := textcheck.Check("asset name", name, textcheck.MaxName); err != nil {
		return nil, err
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
	err = s.withInventory(func(ic *inventoryChange) error {
		before, err := s.snapshotAssetTx(ic.tx, id)
		if err != nil {
			return err
		}
		if err := s.updateAssetTx(ic.tx, id, name, now, cats, replaceKeys, generatorKeys, resolvedValues); err != nil {
			return err
		}
		var recordQuantity func() error
		if stock != nil {
			// Read again under the lock: the quantity may have moved since.
			was, err := s.readStockTx(ic.tx, id)
			if err != nil {
				return err
			}
			if recordQuantity, err = s.setStockTx(ic.tx, id, was, stock.Stock); err != nil {
				return err
			}
		}
		return s.recordAfter(ic.tx, id, before, models.TriggerManual, recordQuantity)
	})
	if err != nil {
		return nil, err
	}

	return s.GetAssetByID(id)
}

// updateAssetTx writes a validated asset update within tx.
func (s *Store) updateAssetTx(tx *sql.Tx, id, name string, now time.Time, cats []string, replaceKeys bool, generatorKeys []models.AssetGeneratorKey, resolvedValues map[string]string) error {
	// Update asset name & updated_at
	_, err := s.TxExec(tx, "UPDATE assets SET name = ?, updated_at = ? WHERE id = ?", name, now, id)
	if err != nil {
		return err
	}

	// Update categories
	_, err = s.TxExec(tx, "DELETE FROM asset_categories WHERE asset_id = ?", id)
	if err != nil {
		return err
	}
	for _, cid := range cats {
		_, err = s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", id, cid)
		if err != nil {
			return err
		}
	}

	// Replace generator keys when supplied, even with none
	if replaceKeys {
		_, err = s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ?", id)
		if err != nil {
			return err
		}
		for _, gk := range generatorKeys {
			_, err = s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", id, gk.GeneratorType, gk.AssignedCode)
			if err != nil {
				errMsg := strings.ToLower(err.Error())
				if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
					return fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, gk.GeneratorType, gk.AssignedCode)
				}
				return err
			}
		}
	}

	// Upsert field values (Data Retention Rule: Existing values not mentioned are untouched!)
	for fieldID, val := range resolvedValues {
		// Delete prior value for this specific field and insert new one
		_, err = s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ? AND field_id = ?", id, fieldID)
		if err != nil {
			return err
		}
		if val != "" {
			_, err = s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", id, fieldID, val, now)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteAsset soft-deletes an asset by setting its archived_at timestamp and releasing its generator codes.
func (s *Store) DeleteAsset(id string) error {
	_, err := s.GetAssetByID(id)
	if err != nil {
		return err
	}

	return s.withInventory(func(ic *inventoryChange) error {
		var lent int
		if err := s.TxQueryRow(ic.tx, "SELECT COUNT(*) FROM asset_loans WHERE asset_id = ? AND checked_in_at IS NULL", id).Scan(&lent); err != nil {
			return fmt.Errorf("checking loans: %w", err)
		}
		if lent > 0 {
			return ErrAssetOnLoan
		}
		if held, err := s.holdsActiveTx(ic.tx, id); err != nil {
			return err
		} else if held > 0 {
			return ErrContainerNotEmpty
		}
		before, err := s.snapshotAssetTx(ic.tx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		_, err = s.TxExec(ic.tx, "UPDATE assets SET archived_at = ?, updated_at = ? WHERE id = ?", now, now, id)
		if err != nil {
			return fmt.Errorf("archiving asset: %w", err)
		}

		// Generator codes are unique only across active assets, so an archived asset
		// releases its codes for reassignment.
		_, err = s.TxExec(ic.tx, "DELETE FROM asset_generator_keys WHERE asset_id = ?", id)
		if err != nil {
			return fmt.Errorf("releasing generator keys: %w", err)
		}
		if before.archived {
			return nil
		}
		// The history keeps the codes it released, which a restore needs.
		return s.recordAssetEvent(ic.tx, models.EventAssetArchived, id, models.EventPayload{
			Trigger: models.TriggerManual,
			Changes: diffCodes(before.codes, nil),
		})
	})
}

// ErrAssetNotArchived is returned when restoring an asset that is active.
var ErrAssetNotArchived = errors.New("the asset is not archived")

// RestoreAsset brings an archived asset back with the codes archiving
// released, except any another asset has taken since, which it returns.
// An asset archived before archiving recorded its codes comes back without
// any.
func (s *Store) RestoreAsset(id string) (*models.AssetWithDetails, []models.AssetGeneratorKey, error) {
	skipped := []models.AssetGeneratorKey{}
	err := s.withInventory(func(ic *inventoryChange) error {
		before, err := s.snapshotAssetTx(ic.tx, id)
		if err != nil {
			return err
		}
		if !before.archived {
			return ErrAssetNotArchived
		}
		released, err := s.releasedCodesTx(ic.tx, id)
		if err != nil {
			return err
		}
		if _, err := s.TxExec(ic.tx, "UPDATE assets SET archived_at = NULL, updated_at = ? WHERE id = ?", time.Now().UTC(), id); err != nil {
			return fmt.Errorf("restoring asset: %w", err)
		}
		// Each change names a code it got back (to) or one taken meanwhile
		// (from).
		changes := []models.EventChange{}
		for _, c := range released {
			var taken int
			err := s.TxQueryRow(ic.tx, "SELECT COUNT(*) FROM asset_generator_keys WHERE generator_type = ? AND assigned_code = ?", c.Key, c.From).Scan(&taken)
			if err != nil {
				return fmt.Errorf("checking code: %w", err)
			}
			if taken > 0 {
				skipped = append(skipped, models.AssetGeneratorKey{AssetID: id, GeneratorType: c.Key, AssignedCode: c.From})
				changes = append(changes, models.EventChange{What: "code", Key: c.Key, Label: c.Label, From: c.From})
				continue
			}
			if _, err := s.TxExec(ic.tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", id, c.Key, c.From); err != nil {
				if IsUniqueViolation(err) {
					return fmt.Errorf("%w: %s", ErrGeneratorCodeExists, c.From)
				}
				return fmt.Errorf("restoring code: %w", err)
			}
			changes = append(changes, models.EventChange{What: "code", Key: c.Key, Label: c.Label, To: c.From})
		}
		return s.recordAssetEvent(ic.tx, models.EventAssetRestored, id, models.EventPayload{Trigger: models.TriggerManual, Changes: changes})
	})
	if err != nil {
		return nil, nil, err
	}
	a, err := s.GetAssetByID(id)
	return a, skipped, err
}

// releasedCodesTx returns the codes an asset's last archiving released, as
// its history recorded them.
func (s *Store) releasedCodesTx(tx *sql.Tx, id string) ([]models.EventChange, error) {
	row := s.TxQueryRow(tx, "SELECT "+eventColumns+" FROM events WHERE asset_id = ? AND kind = ? ORDER BY created_at DESC, id DESC LIMIT 1", id, models.EventAssetArchived)
	e, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the asset's archiving: %w", err)
	}
	var codes []models.EventChange
	for _, c := range e.Payload.Changes {
		if c.What == "code" && c.From != "" {
			codes = append(codes, c)
		}
	}
	return codes, nil
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
