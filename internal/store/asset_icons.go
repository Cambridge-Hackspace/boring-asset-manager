package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Which icon an asset is drawn with (README §3.7): its own, if it has one,
// or else that of the nearest category up from its categories with one,
// chosen as its templates are. An icon of its own names anything the build
// draws, variants and library icons included, and is checked against the
// build when it is set; it is not tied to the build after that, so an
// icon taken away later is drawn as the unknown icon, never quietly
// replaced by its categories'.

// IconUpdate sets an asset's own icon to Name; an empty Name gives it its
// categories' again.
type IconUpdate struct {
	Name string
}

// publishedCategoryIcons maps each category with a published icon to its
// name.
func publishedCategoryIcons(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string]string, error) {
	rows, err := q.Query(`SELECT category_id, icon_name FROM category_icons WHERE source IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading category icons: %w", err)
	}
	defer rows.Close()
	icons := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		icons[id] = name
	}
	return icons, rows.Err()
}

// chooseIcon is the icon an asset in these categories gets from them, or
// nil when none of them, nor their ancestors, has one.
func (t categoryTraits) chooseIcon(categoryIDs []string) *models.ResolvedIcon {
	choice := t.choose(t.icons, categoryIDs, "")
	if choice.TemplateID == "" {
		return nil
	}
	return &models.ResolvedIcon{Name: choice.TemplateID, From: models.IconFromCategory, SourceCategoryID: choice.SourceCategoryID,
		SourceCategoryName: t.names[choice.SourceCategoryID]}
}

// shownIcons maps every category that has an icon, its own or its
// nearest ancestor's, to that icon's name.
func (t categoryTraits) shownIcons() map[string]string {
	shown := map[string]string{}
	for id := range t.parents {
		for c, d := id, 0; c != "" && d < 100; c, d = t.parents[c], d+1 {
			if name := t.icons[c]; name != "" {
				shown[id] = name
				break
			}
		}
	}
	return shown
}

// ShownCategoryIcons maps every category that has an icon, its own or its
// nearest ancestor's, to that icon's name.
func (s *Store) ShownCategoryIcons() (map[string]string, error) {
	t, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	return t.shownIcons(), nil
}

// contextIcon resolves the icon of an asset with its own icon own, if not
// nil, in these categories.
func (s *Store) contextIcon(ctx *assetContext, own *string, categoryIDs []string) (*models.ResolvedIcon, error) {
	traits, err := s.contextTraits(ctx)
	if err != nil {
		return nil, err
	}
	icon := traits.chooseIcon(categoryIDs)
	if own != nil {
		if ctx.iconNames == nil {
			if ctx.iconNames, err = s.BuiltIconNames(); err != nil {
				return nil, err
			}
		}
		icon = &models.ResolvedIcon{Name: *own, From: models.IconFromAsset, Missing: !ctx.iconNames[*own]}
	}
	if icon != nil {
		icon.Color = traits.choose(traits.colors, categoryIDs, "").TemplateID
	}
	return icon, nil
}

// BuiltIconNames returns the names the current build draws, none when
// there is no build.
func (s *Store) BuiltIconNames() (map[string]bool, error) {
	b, err := s.CurrentIconBuild()
	if err != nil || b == nil {
		return map[string]bool{}, err
	}
	var doc struct {
		Icons []struct {
			Name string `json:"name"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(b.IR, &doc); err != nil {
		return nil, fmt.Errorf("reading the icon build: %w", err)
	}
	names := make(map[string]bool, len(doc.Icons))
	for _, i := range doc.Icons {
		names[i.Name] = true
	}
	return names, nil
}

// checkAssetIcon checks an icon an asset is to be given: one the build
// draws, unless it already has it.
func (s *Store) checkAssetIcon(name string, current *string) error {
	if name == "" || (current != nil && *current == name) {
		return nil
	}
	names, err := s.BuiltIconNames()
	if err != nil {
		return err
	}
	if !names[name] {
		return fmt.Errorf("no icon is drawn by the name %q; choose one the Icons page shows", name)
	}
	return nil
}

// setAssetIconTx sets an asset's own icon within tx; "" clears it.
func (s *Store) setAssetIconTx(tx *sql.Tx, id, name string) error {
	var v any
	if name != "" {
		v = name
	}
	_, err := s.TxExec(tx, `UPDATE assets SET icon_name = ? WHERE id = ?`, v, id)
	return err
}
