package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

const (
	UncategorizedCategoryID   = "uncategorized"
	UncategorizedCategoryName = "Uncategorized"
	DefaultCategoryColorHex   = "#3b82f6"
)

var (
	ErrCategoryNotFound             = errors.New("category not found")
	ErrUnitRequiresConsumable       = errors.New("only consumable categories can total their stock in a unit")
	ErrCycleDetected                = errors.New("cannot set parent: circular reference detected")
	ErrSelfParent                   = errors.New("category cannot be its own parent")
	ErrParentNotFound               = errors.New("parent category not found")
	ErrSystemCategoryInTree         = errors.New("the system Uncategorized category cannot have a parent or children")
	ErrRunningLowRequiresConsumable = errors.New("only consumable categories can be flagged as running low")
	colorHexRegex                   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// ValidateColorHex ensures color string is a valid 6-character hex code.
func ValidateColorHex(hex string) string {
	hex = strings.TrimSpace(hex)
	if hex == "" || !colorHexRegex.MatchString(hex) {
		return DefaultCategoryColorHex
	}
	return strings.ToLower(hex)
}

// EnsureUncategorizedCategory ensures the virtual/system category exists in the store.
func (s *Store) EnsureUncategorizedCategory() error {
	var count int
	err := s.QueryRow("SELECT COUNT(*) FROM categories WHERE id = ?", UncategorizedCategoryID).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking uncategorized category: %w", err)
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	query := `INSERT INTO categories (id, name, parent_id, color_hex, is_consumable, is_running_low, created_at, updated_at) VALUES (?, ?, NULL, ?, FALSE, FALSE, ?, ?)`
	_, err = s.Exec(query, UncategorizedCategoryID, UncategorizedCategoryName, "#6b7280", now, now)
	if err != nil {
		return fmt.Errorf("inserting uncategorized category: %w", err)
	}
	return nil
}

// DetectCycle checks whether setting parentID for targetID would create a circular dependency.
func (s *Store) DetectCycle(targetID string, proposedParentID *string) error {
	if proposedParentID == nil || *proposedParentID == "" {
		return nil
	}
	if *proposedParentID == targetID {
		return ErrSelfParent
	}

	currentID := *proposedParentID
	visited := make(map[string]bool)
	visited[targetID] = true

	for depth := 0; depth < 100; depth++ {
		if visited[currentID] {
			return ErrCycleDetected
		}
		visited[currentID] = true

		var parent sql.NullString
		err := s.QueryRow("SELECT parent_id FROM categories WHERE id = ?", currentID).Scan(&parent)
		if err == sql.ErrNoRows {
			return ErrParentNotFound
		}
		if err != nil {
			return fmt.Errorf("checking parent category %s: %w", currentID, err)
		}

		if !parent.Valid || parent.String == "" {
			return nil // Reached tree root safely
		}
		currentID = parent.String
	}

	return ErrCycleDetected
}

// validateCategoryAttributes enforces invariants shared by category creation and updates:
// the Uncategorized system category stays outside the tree, and the running-low flag and
// low stock threshold are only meaningful for consumable categories.
func validateCategoryAttributes(id string, in CategoryInput) error {
	if in.ParentID != nil && (*in.ParentID == UncategorizedCategoryID || id == UncategorizedCategoryID) {
		return ErrSystemCategoryInTree
	}
	if in.IsRunningLow && !in.IsConsumable {
		return ErrRunningLowRequiresConsumable
	}
	if in.Unit != "" {
		if !in.IsConsumable {
			return ErrUnitRequiresConsumable
		}
		if err := textcheck.Check("unit", in.Unit, MaxStockUnit); err != nil {
			return err
		}
	}
	if in.LowStockThreshold != nil {
		if !in.IsConsumable {
			return ErrThresholdRequiresConsumable
		}
		if *in.LowStockThreshold < 1 || *in.LowStockThreshold > MaxLowStockThreshold {
			return ErrThresholdOutOfRange
		}
	}
	return nil
}

// CategoryInput is everything a caller sets on a category.
type CategoryInput struct {
	Name              string
	ParentID          *string
	ColorHex          string
	IsConsumable      bool
	Unit              string
	IsLendable        bool
	IsRunningLow      bool
	LowStockThreshold *int
}

// categoryColumns lists a category's columns, each prefixed, in scanCategory's order.
func categoryColumns(prefix string) string {
	cols := []string{"id", "name", "parent_id", "color_hex", "is_consumable", "stock_unit", "is_lendable", "is_running_low", "low_stock_threshold", "scan_template_id", "created_at", "updated_at"}
	for i, c := range cols {
		cols[i] = prefix + c
	}
	return strings.Join(cols, ", ")
}

// scanCategory reads a row selected with categoryColumns.
func scanCategory(row interface{ Scan(...any) error }) (models.Category, error) {
	var (
		c         models.Category
		parentVal sql.NullString
		threshold sql.NullInt64
		template  sql.NullString
	)
	err := row.Scan(&c.ID, &c.Name, &parentVal, &c.ColorHex, &c.IsConsumable, &c.Unit, &c.IsLendable, &c.IsRunningLow, &threshold, &template, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if parentVal.Valid && parentVal.String != "" {
		c.ParentID = &parentVal.String
	}
	if threshold.Valid {
		t := int(threshold.Int64)
		c.LowStockThreshold = &t
	}
	if template.Valid && template.String != "" {
		c.ScanTemplateID = &template.String
	}
	c.LabelTemplateIDs = []string{}
	return c, nil
}

// labelTemplateLists maps each category naming label templates to them, in
// order of preference.
func labelTemplateLists(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string][]string, error) {
	rows, err := q.Query(`SELECT category_id, template_id FROM category_label_templates ORDER BY category_id, sort_order`)
	if err != nil {
		return nil, fmt.Errorf("reading label templates: %w", err)
	}
	defer rows.Close()
	lists := map[string][]string{}
	for rows.Next() {
		var category, template string
		if err := rows.Scan(&category, &template); err != nil {
			return nil, fmt.Errorf("scanning a label template: %w", err)
		}
		lists[category] = append(lists[category], template)
	}
	return lists, rows.Err()
}

// withLabelTemplates sets categories' label templates.
func (s *Store) withLabelTemplates(categories []models.Category) error {
	lists, err := labelTemplateLists(storeQuerier{s})
	if err != nil {
		return err
	}
	for i := range categories {
		setLabelTemplates(&categories[i], lists[categories[i].ID])
	}
	return nil
}

func setLabelTemplates(c *models.Category, list []string) {
	c.LabelTemplateIDs, c.LabelTemplateID = []string{}, nil
	if len(list) > 0 {
		c.LabelTemplateIDs = list
		first := list[0]
		c.LabelTemplateID = &first
	}
}

// CreateCategory creates a new category record with cycle validation.
func (s *Store) CreateCategory(name string, parentID *string, colorHex string, isConsumable, isRunningLow bool) (*models.Category, error) {
	return s.CreateCategoryFrom(CategoryInput{Name: name, ParentID: parentID, ColorHex: colorHex, IsConsumable: isConsumable, IsRunningLow: isRunningLow})
}

// CreateCategoryFrom creates a category. A category created running low
// records a running-low event, and one created with a threshold above its
// (empty) active count flags itself.
func (s *Store) CreateCategoryFrom(in CategoryInput) (*models.Category, error) {
	in.Name, in.Unit = strings.TrimSpace(in.Name), strings.TrimSpace(in.Unit)
	if in.Name == "" {
		return nil, errors.New("category name cannot be empty")
	}
	if err := textcheck.Check("category name", in.Name, textcheck.MaxName); err != nil {
		return nil, err
	}

	in.ColorHex = ValidateColorHex(in.ColorHex)
	categoryID := uuid.NewString()

	if in.ParentID != nil && *in.ParentID != "" {
		if err := s.DetectCycle(categoryID, in.ParentID); err != nil {
			return nil, err
		}
	} else {
		in.ParentID = nil
	}
	if err := validateCategoryAttributes(categoryID, in); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	err := s.withInventory(func(ic *inventoryChange) error {
		query := `INSERT INTO categories (id, name, parent_id, color_hex, is_consumable, stock_unit, is_lendable, is_running_low, low_stock_threshold, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		if _, err := s.TxExec(ic.tx, query, categoryID, in.Name, in.ParentID, in.ColorHex, in.IsConsumable, in.Unit, in.IsLendable, in.IsRunningLow, in.LowStockThreshold, now, now); err != nil {
			return fmt.Errorf("inserting category: %w", err)
		}
		return s.setFlagByHand(ic, categoryID, false, in.IsRunningLow)
	})
	if err != nil {
		return nil, err
	}
	return s.GetCategoryByID(categoryID)
}

// cloneCategoryLinks are the tables a category's own settings are kept in,
// each with the columns copied beside category_id.
var cloneCategoryLinks = []struct{ table, columns string }{
	{"category_fields", "field_id, is_required"},
	{"category_relation_types", "relation_type_id"},
	{"category_actions", "action_id, cascades"},
	{"category_label_templates", "template_id, sort_order"},
	{"category_scopes", "scope_id"},
}

// CloneCategory makes a copy of a category beside it, under the same
// parent: its settings, fields, suggested link types, actions, scopes and
// scan and label templates (the same templates, not copies of them), but
// not its subcategories or its assets. An empty name names it "<name>
// copy". The copy is not flagged running low by hand; with a threshold, it
// flags itself, as a new category does.
func (s *Store) CloneCategory(id, name string) (*models.Category, error) {
	if id == UncategorizedCategoryID {
		return nil, ErrSystemCategoryInTree
	}
	src, err := s.GetCategoryByID(id)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = src.Name + " copy"
	}
	if err := textcheck.Check("category name", name, textcheck.MaxName); err != nil {
		return nil, err
	}
	cloneID := uuid.NewString()
	now := time.Now().UTC()
	err = s.withInventory(func(ic *inventoryChange) error {
		res, err := s.TxExec(ic.tx, `INSERT INTO categories (id, name, parent_id, color_hex, is_consumable, stock_unit, is_lendable, is_running_low, low_stock_threshold, scan_template_id, created_at, updated_at)
			SELECT ?, ?, parent_id, color_hex, is_consumable, stock_unit, is_lendable, ?, low_stock_threshold, scan_template_id, ?, ? FROM categories WHERE id = ?`,
			cloneID, name, false, now, now, id)
		if err != nil {
			return fmt.Errorf("copying category: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			// Deleted since it was read.
			return ErrCategoryNotFound
		}
		for _, l := range cloneCategoryLinks {
			q := fmt.Sprintf("INSERT INTO %s (category_id, %s) SELECT ?, %s FROM %s WHERE category_id = ?", l.table, l.columns, l.columns, l.table)
			if _, err := s.TxExec(ic.tx, q, cloneID, id); err != nil {
				return fmt.Errorf("copying %s: %w", l.table, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetCategoryByID(cloneID)
}

// GetCategoryByID retrieves a category by UUID.
func (s *Store) GetCategoryByID(id string) (*models.Category, error) {
	query := `SELECT ` + categoryColumns("") + ` FROM categories WHERE id = ?`
	c, err := scanCategory(s.QueryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, ErrCategoryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying category by id: %w", err)
	}
	list := []models.Category{c}
	if err := s.withLabelTemplates(list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// ListCategories returns all categories. The virtual 'uncategorized' category is hidden if it contains 0 assets.
func (s *Store) ListCategories() ([]models.Category, error) {
	query := `SELECT ` + categoryColumns("") + ` FROM categories ORDER BY name ASC`
	rows, err := s.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying categories: %w", err)
	}
	defer rows.Close()

	var all []models.Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		all = append(all, c)
	}

	// Check if uncategorized has active (non-archived) assets
	var uncategorizedAssetCount int
	_ = s.QueryRow(`
		SELECT COUNT(*)
		FROM asset_categories ac
		JOIN assets a ON ac.asset_id = a.id
		WHERE ac.category_id = ? AND a.archived_at IS NULL
	`, UncategorizedCategoryID).Scan(&uncategorizedAssetCount)

	result := make([]models.Category, 0, len(all))
	for _, cat := range all {
		if cat.ID == UncategorizedCategoryID && uncategorizedAssetCount == 0 {
			continue
		}
		result = append(result, cat)
	}
	if err := s.withLabelTemplates(result); err != nil {
		return nil, err
	}
	return result, nil
}

// UpdateCategory updates category properties with cycle prevention. It
// leaves the category without a threshold; see UpdateCategoryFrom.
func (s *Store) UpdateCategory(id, name string, parentID *string, colorHex string, isConsumable, isRunningLow bool) (*models.Category, error) {
	return s.UpdateCategoryFrom(id, CategoryInput{Name: name, ParentID: parentID, ColorHex: colorHex, IsConsumable: isConsumable, IsRunningLow: isRunningLow})
}

// UpdateCategoryFrom replaces a category's properties with cycle prevention.
// Changing the flag records an event as a manual change; moving the category
// or changing its threshold can make it, or its old and new ancestors, cross
// their thresholds.
func (s *Store) UpdateCategoryFrom(id string, in CategoryInput) (*models.Category, error) {
	if _, err := s.GetCategoryByID(id); err != nil {
		return nil, err
	}

	in.Name, in.Unit = strings.TrimSpace(in.Name), strings.TrimSpace(in.Unit)
	if in.Name == "" {
		return nil, errors.New("category name cannot be empty")
	}
	if err := textcheck.Check("category name", in.Name, textcheck.MaxName); err != nil {
		return nil, err
	}
	in.ColorHex = ValidateColorHex(in.ColorHex)

	if in.ParentID != nil && *in.ParentID != "" {
		if err := s.DetectCycle(id, in.ParentID); err != nil {
			return nil, err
		}
	} else {
		in.ParentID = nil
	}
	if err := validateCategoryAttributes(id, in); err != nil {
		return nil, err
	}

	err := s.withInventory(func(ic *inventoryChange) error {
		// Read the flag under the lock, so two simultaneous changes record one event.
		var was bool
		err := s.TxQueryRow(ic.tx, "SELECT is_running_low FROM categories WHERE id = ?", id).Scan(&was)
		if err == sql.ErrNoRows {
			return ErrCategoryNotFound
		}
		if err != nil {
			return fmt.Errorf("reading category: %w", err)
		}
		query := `UPDATE categories SET name = ?, parent_id = ?, color_hex = ?, is_consumable = ?, stock_unit = ?, is_lendable = ?, is_running_low = ?, low_stock_threshold = ?, updated_at = ? WHERE id = ?`
		if _, err := s.TxExec(ic.tx, query, in.Name, in.ParentID, in.ColorHex, in.IsConsumable, in.Unit, in.IsLendable, in.IsRunningLow, in.LowStockThreshold, time.Now().UTC(), id); err != nil {
			return fmt.Errorf("updating category: %w", err)
		}
		return s.setFlagByHand(ic, id, was, in.IsRunningLow)
	})
	if err != nil {
		return nil, err
	}
	return s.GetCategoryByID(id)
}

// DeleteCategory deletes a category record, reattaching any assets it leaves without a category to Uncategorized.
func (s *Store) DeleteCategory(id string) error {
	if id == UncategorizedCategoryID {
		return errors.New("cannot delete the system uncategorized category")
	}

	_, err := s.GetCategoryByID(id)
	if err != nil {
		return err
	}
	if err := s.EnsureUncategorizedCategory(); err != nil {
		return err
	}

	return s.withInventory(func(ic *inventoryChange) error {
		// Asset bindings cascade away with the category; child categories become roots.
		if _, err := s.TxExec(ic.tx, "DELETE FROM categories WHERE id = ?", id); err != nil {
			return fmt.Errorf("deleting category: %w", err)
		}

		// Assets left without any category fall back to the system Uncategorized category.
		_, err := s.TxExec(ic.tx, `
			INSERT INTO asset_categories (asset_id, category_id)
			SELECT a.id, ?
			FROM assets a
			WHERE NOT EXISTS (SELECT 1 FROM asset_categories ac WHERE ac.asset_id = a.id)
		`, UncategorizedCategoryID)
		if err != nil {
			return fmt.Errorf("reattaching orphaned assets to uncategorized: %w", err)
		}
		return nil
	})
}

// AssignCategoryField maps a field to a category with a required/optional constraint.
func (s *Store) AssignCategoryField(categoryID, fieldID string, isRequired bool) error {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return err
	}
	if _, err := s.GetFieldByID(fieldID); err != nil {
		return err
	}

	var exists int
	err := s.QueryRow("SELECT COUNT(*) FROM category_fields WHERE category_id = ? AND field_id = ?", categoryID, fieldID).Scan(&exists)
	if err != nil {
		return err
	}

	if exists > 0 {
		_, err = s.Exec("UPDATE category_fields SET is_required = ? WHERE category_id = ? AND field_id = ?", isRequired, categoryID, fieldID)
		return err
	}

	_, err = s.Exec("INSERT INTO category_fields (category_id, field_id, is_required) VALUES (?, ?, ?)", categoryID, fieldID, isRequired)
	return err
}

// RemoveCategoryField removes a field mapping from a category.
func (s *Store) RemoveCategoryField(categoryID, fieldID string) error {
	_, err := s.Exec("DELETE FROM category_fields WHERE category_id = ? AND field_id = ?", categoryID, fieldID)
	return err
}

// GetDirectCategoryFields retrieves fields mapped directly to this category.
func (s *Store) GetDirectCategoryFields(categoryID string) ([]models.EffectiveField, error) {
	query := `
		SELECT f.id, f.field_key, f.label, f.validation_type, f.options, f.created_at, f.updated_at, cf.is_required
		FROM category_fields cf
		JOIN fields f ON cf.field_id = f.id
		WHERE cf.category_id = ?
		ORDER BY f.field_key ASC
	`
	rows, err := s.Query(query, categoryID)
	if err != nil {
		return nil, fmt.Errorf("querying direct category fields: %w", err)
	}
	defer rows.Close()

	var result []models.EffectiveField
	for rows.Next() {
		var (
			f          models.Field
			optionsVal sql.NullString
			isRequired bool
		)
		if err := rows.Scan(
			&f.ID, &f.Key, &f.Label, &f.ValidationType, &optionsVal, &f.CreatedAt, &f.UpdatedAt, &isRequired,
		); err != nil {
			return nil, fmt.Errorf("scanning category field: %w", err)
		}
		if optionsVal.Valid && optionsVal.String != "" {
			f.Options = json.RawMessage(optionsVal.String)
		}
		result = append(result, models.EffectiveField{
			Field:            f,
			IsRequired:       isRequired,
			SourceCategoryID: categoryID,
		})
	}

	return result, nil
}

// GetEffectiveCategoryFields computes the recursive inheritance of fields up the ancestor tree.
// Conflict Resolution Rule: If Field X is marked Required anywhere in the ancestor chain, Required takes precedence.
func (s *Store) GetEffectiveCategoryFields(categoryID string) ([]models.EffectiveField, error) {
	return s.GetEffectiveFieldsForCategories([]string{categoryID})
}

// GetEffectiveFieldsForCategories computes the union of inherited fields across multiple categories.
// For each category, ascends the ancestor tree to root, collecting all fields.
// Conflict Resolution Rule: If a field is Required in any category or ancestor, it is marked Required.
func (s *Store) GetEffectiveFieldsForCategories(categoryIDs []string) ([]models.EffectiveField, error) {
	if len(categoryIDs) == 0 {
		return nil, nil
	}

	// 1. Collect all ancestor category IDs across all assigned categories, in
	// order: each category, then its parent, and so on. The first category
	// found to assign a field is reported as its source, so a field assigned
	// at several levels is attributed to the nearest one.
	allCategoryIDs := make(map[string]bool)
	var ordered []string
	for _, catID := range categoryIDs {
		curr := catID
		for depth := 0; depth < 100 && curr != ""; depth++ {
			if allCategoryIDs[curr] {
				break
			}
			allCategoryIDs[curr] = true
			ordered = append(ordered, curr)

			var parentVal sql.NullString
			err := s.QueryRow("SELECT parent_id FROM categories WHERE id = ?", curr).Scan(&parentVal)
			if err != nil {
				break
			}
			if parentVal.Valid && parentVal.String != "" {
				curr = parentVal.String
			} else {
				break
			}
		}
	}

	if len(allCategoryIDs) == 0 {
		return nil, nil
	}

	// 2. Query fields for all accumulated category IDs
	// We map field ID -> EffectiveField
	effectiveMap := make(map[string]models.EffectiveField)

	for _, catID := range ordered {
		direct, err := s.GetDirectCategoryFields(catID)
		if err != nil {
			return nil, err
		}

		for _, item := range direct {
			existing, exists := effectiveMap[item.Field.ID]
			if !exists {
				effectiveMap[item.Field.ID] = item
			} else {
				// Conflict resolution: Required constraint takes precedence
				if item.IsRequired {
					existing.IsRequired = true
					effectiveMap[item.Field.ID] = existing
				}
			}
		}
	}

	// 3. Return slice sorted by field key
	result := make([]models.EffectiveField, 0, len(effectiveMap))
	for _, ef := range effectiveMap {
		result = append(result, ef)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Field.Key < result[j].Field.Key
	})

	return result, nil
}

// ListAllCategories returns every category, including Uncategorized.
func (s *Store) ListAllCategories() ([]models.Category, error) {
	rows, err := s.Query(`SELECT ` + categoryColumns("") + ` FROM categories ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("querying categories: %w", err)
	}
	defer rows.Close()
	all := []models.Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return all, s.withLabelTemplates(all)
}
