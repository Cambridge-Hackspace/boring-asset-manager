package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

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
// nil, in these categories, with these field values by key, and in these
// states, worst first.
func (s *Store) contextIcon(ctx *assetContext, own *string, categoryIDs []string, fieldValues map[string]string, states []string) (*models.ResolvedIcon, error) {
	traits, err := s.contextTraits(ctx)
	if err != nil {
		return nil, err
	}
	icon := traits.chooseIcon(categoryIDs)
	if icon == nil && own == nil {
		return nil, nil
	}
	if ctx.iconNames == nil {
		if ctx.iconNames, err = s.BuiltIconNames(); err != nil {
			return nil, err
		}
	}
	if own != nil {
		icon = &models.ResolvedIcon{Name: *own, From: models.IconFromAsset, Missing: !ctx.iconNames[*own]}
	}
	icon.Color = traits.choose(traits.colors, categoryIDs, "").TemplateID
	if icon.Missing {
		return icon, nil
	}
	wrapper, params, err := s.contextWrapper(ctx, icon.Name, fieldValues)
	if err != nil {
		return nil, err
	}
	if wrapper != "" {
		icon.Base, icon.Bound, icon.Name = icon.Name, wrapper, wrapper
		icon.Params = make(map[string]float64, len(params))
		for _, p := range params {
			icon.Params[p.param], _ = strconv.ParseFloat(p.value, 64)
		}
	}
	if len(states) == 0 {
		return icon, nil
	}
	if ctx.iconVariants == nil {
		if ctx.iconVariants, err = s.IconStateVariants(); err != nil {
			return nil, err
		}
	}
	showState(icon, states, ctx.iconVariants, ctx.iconNames)
	return icon, nil
}

// showState draws icon as the variant of the first of states, worst first,
// that drawn draws for it, by variants; it is left as it is when none.
func showState(icon *models.ResolvedIcon, states []string, variants map[string]string, drawn map[string]bool) {
	for _, st := range states {
		v := variants[st]
		if v == "" || !drawn[icon.Name+"-"+v] {
			continue
		}
		if icon.Base == "" {
			icon.Base = icon.Name
		}
		icon.State, icon.Variant = st, v
		icon.Name += "-" + v
		return
	}
}

// assetStates are the states an asset is in that its icon can show, worst
// first.
func assetStates(d *models.AssetWithDetails) []string {
	var in []string
	switch d.Asset.Condition {
	case models.ConditionOutOfService:
		in = append(in, models.IconStateOutOfService)
	case models.ConditionNeedsMaintenance:
		in = append(in, models.IconStateNeedsMaintenance)
	}
	if l := d.Loan; l != nil {
		if l.Overdue {
			in = append(in, models.IconStateOverdue)
		}
		in = append(in, models.IconStateOnLoan)
	}
	if st := d.Asset.Stock; st != nil {
		if st.Quantity <= 0 {
			in = append(in, models.IconStateOutOfStock)
		}
		if st.IsRunningLow {
			in = append(in, models.IconStateLowStock)
		}
	}
	return in
}

// variantPattern is a variant's name: what isolex accepts after the '-'
// it joins an icon's name and its variant's with.
var variantPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,31}$`)

// IconStateVariants maps each state (models.IconStates) to the variant it
// is drawn with, "" for none: bam's defaults, but for those chosen.
func (s *Store) IconStateVariants() (map[string]string, error) {
	variants := make(map[string]string, len(models.IconStates))
	for _, st := range models.IconStates {
		variants[st] = models.DefaultIconVariants[st]
	}
	var stored sql.NullString
	err := s.QueryRow(`SELECT states FROM icon_settings WHERE id = 'icons'`).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading icon states: %w", err)
	}
	if stored.Valid {
		var chosen map[string]string
		if err := json.Unmarshal([]byte(stored.String), &chosen); err != nil {
			return nil, fmt.Errorf("reading icon states: %w", err)
		}
		for st, v := range chosen {
			if _, known := variants[st]; known {
				variants[st] = v
			}
		}
	}
	return variants, nil
}

// SetIconStateVariants replaces the variants states are drawn with; a state
// left out is drawn with none. It needs no build: which icons have each
// variant is read from the build as assets are drawn.
func (s *Store) SetIconStateVariants(variants map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(models.IconStates))
	for _, st := range models.IconStates {
		out[st] = ""
	}
	for st, v := range variants {
		if _, known := out[st]; !known {
			return nil, fmt.Errorf("%w: there is no state %q", ErrInvalidIconStates, st)
		}
		if v != "" && !variantPattern.MatchString(v) {
			return nil, fmt.Errorf("%w: a variant's name is 1 to 32 letters, digits, '-', '_' and '.', starting with a letter, digit or '_'", ErrInvalidIconStates)
		}
		out[st] = v
	}
	text, _ := json.Marshal(out)
	if _, err := s.Exec(`UPDATE icon_settings SET states = ? WHERE id = 'icons'`, string(text)); err != nil {
		return nil, fmt.Errorf("saving icon states: %w", err)
	}
	return out, nil
}

// ErrInvalidIconStates is a state's variant refused.
var ErrInvalidIconStates = errors.New("invalid icon states")

// MissingIcon is a name assets give themselves as their icon that the
// current build does not draw, so they are drawn with the unknown icon.
type MissingIcon struct {
	Name   string `json:"name"`
	Assets int    `json:"assets"`
}

// MissingAssetIcons lists the icons active assets have as their own that
// the build does not draw, by name, with how many have each.
func (s *Store) MissingAssetIcons() ([]MissingIcon, error) {
	drawn, err := s.BuiltIconNames()
	if err != nil {
		return nil, err
	}
	rows, err := s.Query(`SELECT icon_name, COUNT(*) FROM assets WHERE icon_name IS NOT NULL AND archived_at IS NULL GROUP BY icon_name ORDER BY icon_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	missing := []MissingIcon{}
	for rows.Next() {
		var m MissingIcon
		if err := rows.Scan(&m.Name, &m.Assets); err != nil {
			return nil, err
		}
		if !drawn[m.Name] {
			missing = append(missing, m)
		}
	}
	return missing, rows.Err()
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
	if IsIconWrapper(name) {
		return fmt.Errorf("%q draws an icon for some assets' field values, and cannot be given as one's own", name)
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
