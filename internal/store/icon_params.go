package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// An icon's params bound to asset fields (README §3.7). A category's own
// icon may bind any of its params to a number field; every build then
// generates, for each icon with bindings, a wrapper icon for each distinct
// set of values the active assets drawn with it have,
//
//	icon "bam--server--u_2" extends="server" { param u 2 }
//
// and an asset is drawn with the wrapper for its values. Params are late
// bound, so the parent's geometry sees them, and a wrapper has the
// parent's variants too. Wrappers never stop a build: one that does not
// compile is left out, and its assets are drawn plain.

const (
	// paramsFile is the file the wrappers are in, in a build.
	paramsFile = "bam/params.ilx"
	// wrapperPrefix starts every wrapper's name; no icon of a category's
	// may start "bam-".
	wrapperPrefix = "bam--"
	// MaxIconParamSets bounds the wrappers one icon has: the value sets
	// past it, in order, are drawn plain.
	MaxIconParamSets = 256
)

var (
	// ErrInvalidIconParams is a binding refused.
	ErrInvalidIconParams = errors.New("invalid icon params")
	// paramPattern is a param's name, as isolex reads one.
	paramPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// IsIconWrapper says whether a name is a wrapper's, which nothing but a
// build makes.
func IsIconWrapper(name string) bool { return strings.HasPrefix(name, wrapperPrefix) }

// iconWrapper is one wrapper: icon drawn with params set to values.
type iconWrapper struct {
	name, icon string
	params     []paramValue
}

type paramValue struct{ param, value string }

// source is the wrapper's line of paramsFile.
func (w iconWrapper) source() string {
	var b strings.Builder
	fmt.Fprintf(&b, "icon %q extends=%q {", w.name, w.icon)
	for _, p := range w.params {
		fmt.Fprintf(&b, " param %s %s;", p.param, p.value)
	}
	b.WriteString(" }")
	return b.String()
}

// wrapperName names the wrapper drawing icon with these params, in param
// order: bam--<icon>--<param>_<value>, '-' in a value written 'm'.
func wrapperName(icon string, params []paramValue) string {
	var b strings.Builder
	b.WriteString(wrapperPrefix + icon)
	for _, p := range params {
		b.WriteString("--" + p.param + "_" + strings.ReplaceAll(p.value, "-", "m"))
	}
	return b.String()
}

// paramNumber reads a field's value as a param's: a finite number, written
// as isolex reads it; ok is false for anything else.
func paramNumber(value string) (string, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || math.Abs(v) > 1e9 {
		return "", false
	}
	if v == 0 {
		v = 0 // not -0
	}
	return strconv.FormatFloat(v, 'f', -1, 64), true
}

// iconBinding is a param of an icon bound to a field.
type iconBinding struct {
	param, fieldID, fieldKey string
}

// readIconBindings maps each published icon with bindings to them, by
// param.
func readIconBindings(q querier) (map[string][]iconBinding, error) {
	rows, err := q(`SELECT ci.icon_name, b.param, f.id, f.field_key FROM icon_param_bindings b
		JOIN category_icons ci ON ci.category_id = b.category_id
		JOIN fields f ON f.id = b.field_id
		WHERE ci.source IS NOT NULL ORDER BY ci.icon_name, b.param`)
	if err != nil {
		return nil, fmt.Errorf("reading icon params: %w", err)
	}
	defer rows.Close()
	out := map[string][]iconBinding{}
	for rows.Next() {
		var icon string
		var b iconBinding
		if err := rows.Scan(&icon, &b.param, &b.fieldID, &b.fieldKey); err != nil {
			return nil, err
		}
		out[icon] = append(out[icon], b)
	}
	return out, rows.Err()
}

// boundParams are the params an asset with these field values, by key,
// is drawn with, in param order; none when it has a number for none.
func boundParams(bindings []iconBinding, values func(b iconBinding) (string, bool)) []paramValue {
	var out []paramValue
	for _, b := range bindings {
		raw, ok := values(b)
		if !ok {
			continue
		}
		if v, ok := paramNumber(raw); ok {
			out = append(out, paramValue{b.param, v})
		}
	}
	return out
}

// readIconWrappers works out the wrappers a build has: for each icon with
// bindings, one for each distinct set of values among the active assets
// that are drawn with it, or may be: those with it as their own, and
// those in a category it is drawn for.
func readIconWrappers(q querier) ([]iconWrapper, error) {
	bindings, err := readIconBindings(q)
	if err != nil || len(bindings) == 0 {
		return nil, err
	}
	// which icon each category is drawn with, its own or inherited
	parents, own := map[string]string{}, map[string]string{}
	rows, err := q(`SELECT c.id, COALESCE(c.parent_id, ''), COALESCE(ci.icon_name, '') FROM categories c
		LEFT JOIN category_icons ci ON ci.category_id = c.id AND ci.source IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	for rows.Next() {
		var id, parent, icon string
		if err := rows.Scan(&id, &parent, &icon); err != nil {
			rows.Close()
			return nil, err
		}
		parents[id], own[id] = parent, icon
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	shown := func(id string) string {
		for c, d := id, 0; c != "" && d < 100; c, d = parents[c], d+1 {
			if own[c] != "" {
				return own[c]
			}
		}
		return ""
	}

	// the icons each active asset may be drawn with
	drawnWith := map[string]map[string]bool{}
	add := func(asset, icon string) {
		if _, bound := bindings[icon]; !bound {
			return
		}
		if drawnWith[asset] == nil {
			drawnWith[asset] = map[string]bool{}
		}
		drawnWith[asset][icon] = true
	}
	rows, err = q(`SELECT a.id, COALESCE(a.icon_name, ''), COALESCE(ac.category_id, '') FROM assets a
		LEFT JOIN asset_categories ac ON ac.asset_id = a.id WHERE a.archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading assets: %w", err)
	}
	for rows.Next() {
		var id, icon, category string
		if err := rows.Scan(&id, &icon, &category); err != nil {
			rows.Close()
			return nil, err
		}
		if icon != "" {
			add(id, icon)
		} else if category != "" {
			add(id, shown(category))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(drawnWith) == 0 {
		return nil, nil
	}

	// their values of the bound fields
	values := map[string]map[string]string{} // asset → field ID → value
	rows, err = q(`SELECT afv.asset_id, afv.field_id, afv.value FROM asset_field_values afv
		WHERE afv.field_id IN (SELECT field_id FROM icon_param_bindings)`)
	if err != nil {
		return nil, fmt.Errorf("reading field values: %w", err)
	}
	for rows.Next() {
		var asset, field, value string
		if err := rows.Scan(&asset, &field, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if values[asset] == nil {
			values[asset] = map[string]string{}
		}
		values[asset][field] = value
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byIcon := map[string]map[string]iconWrapper{}
	for asset, icons := range drawnWith {
		for icon := range icons {
			params := boundParams(bindings[icon], func(b iconBinding) (string, bool) {
				v, ok := values[asset][b.fieldID]
				return v, ok
			})
			if len(params) == 0 {
				continue
			}
			w := iconWrapper{name: wrapperName(icon, params), icon: icon, params: params}
			if byIcon[icon] == nil {
				byIcon[icon] = map[string]iconWrapper{}
			}
			byIcon[icon][w.name] = w
		}
	}
	var out []iconWrapper
	for _, set := range byIcon {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) > MaxIconParamSets {
			names = names[:MaxIconParamSets]
		}
		for _, n := range names {
			out = append(out, set[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// paramsSource is the wrappers' file: one wrapper a line.
func paramsSource(ws []iconWrapper) string {
	var b strings.Builder
	b.WriteString("// Generated by bam for icon params bound to asset fields; each line one wrapper.\n")
	for _, w := range ws {
		b.WriteString(w.source() + "\n")
	}
	return b.String()
}

// maxWrapperCompiles bounds the compiles spent finding which wrappers do
// not compile, past which none are kept.
const maxWrapperCompiles = 64

// compileInputs compiles in, leaving out wrappers that do not compile:
// those with errors on their line, and then, should errors remain that
// leaving out every wrapper clears (a value the icon it extends cannot be
// drawn with is reported there), those found by halving the wrappers,
// with a warning saying how many were left out.
func (s *Store) compileInputs(ctx context.Context, c IconCompiler, in iconInputs) (*isolex.Result, error) {
	for {
		files, err := in.files()
		if err != nil {
			return nil, err
		}
		res, err := compile(ctx, c, files)
		if err != nil || res.OK || len(in.wrappers) == 0 {
			return res, err
		}
		bad := map[int]bool{}
		for _, d := range res.Errors {
			// line 1 is the header; wrapper i is on line i+2
			if d.File == paramsFile && d.Line >= 2 && d.Line-2 < len(in.wrappers) {
				bad[d.Line-2] = true
			}
		}
		if len(bad) > 0 {
			kept := in.wrappers[:0:0]
			for i, w := range in.wrappers {
				if !bad[i] {
					kept = append(kept, w)
				}
			}
			in.wrappers = kept
			continue
		}
		// errors elsewhere: the wrappers' doing, if the icons compile without
		with := func(ws []iconWrapper) (*isolex.Result, error) {
			try := in
			try.wrappers = ws
			files, err := try.files()
			if err != nil {
				return nil, err
			}
			return compile(ctx, c, files)
		}
		alone, err := with(nil)
		if err != nil || !alone.OK {
			return res, err
		}
		budget := maxWrapperCompiles
		var good func(ws []iconWrapper) ([]iconWrapper, error)
		good = func(ws []iconWrapper) ([]iconWrapper, error) {
			if budget <= 0 {
				return nil, nil
			}
			budget--
			r, err := with(ws)
			if err != nil || r.OK {
				return ws, err
			}
			if len(ws) == 1 {
				return nil, nil
			}
			left, err := good(ws[:len(ws)/2])
			if err != nil {
				return nil, err
			}
			right, err := good(ws[len(ws)/2:])
			return append(left, right...), err
		}
		kept, err := good(in.wrappers)
		if err != nil {
			return nil, err
		}
		out := alone
		if len(kept) > 0 {
			if r, err := with(kept); err != nil {
				return nil, err
			} else if r.OK {
				out = r
			} else {
				kept = nil
			}
		}
		first := res.Errors[0]
		out.Warnings = append(out.Warnings, isolex.Diag{File: paramsFile, Line: 1, Col: 1,
			Message: fmt.Sprintf("%d of %d icons drawn for assets' field values are left out, as those values do not draw, and those assets are drawn plain: %s:%d:%d: %s",
				len(in.wrappers)-len(kept), len(in.wrappers), first.File, first.Line, first.Col, first.Message)})
		return out, nil
	}
}

// IconParamsBound says whether any icon's params are bound to fields.
func (s *Store) IconParamsBound() (bool, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM icon_param_bindings`).Scan(&n)
	return n > 0, err
}

// IconParamBindings lists the params of a category's icon bound to
// fields, by param.
func (s *Store) IconParamBindings(categoryID string) ([]models.IconParamBinding, error) {
	rows, err := s.Query(`SELECT b.param, f.id, f.field_key, f.label FROM icon_param_bindings b
		JOIN fields f ON f.id = b.field_id WHERE b.category_id = ? ORDER BY b.param`, categoryID)
	if err != nil {
		return nil, fmt.Errorf("reading icon params: %w", err)
	}
	defer rows.Close()
	out := []models.IconParamBinding{}
	for rows.Next() {
		var b models.IconParamBinding
		if err := rows.Scan(&b.Param, &b.FieldID, &b.FieldKey, &b.FieldLabel); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetIconParamBindings replaces the bindings of a category's icon, param to
// field ID, each field a number field; then builds the icons with them.
func (s *Store) SetIconParamBindings(ctx context.Context, c IconCompiler, categoryID string, bindings map[string]string) ([]models.IconParamBinding, *models.IconSave, error) {
	var exists int
	err := s.QueryRow(`SELECT COUNT(*) FROM category_icons WHERE category_id = ? AND source IS NOT NULL`, categoryID).Scan(&exists)
	if err != nil {
		return nil, nil, err
	}
	if exists == 0 {
		return nil, nil, fmt.Errorf("%w: the category has no published icon of its own", ErrInvalidIconParams)
	}
	for param, field := range bindings {
		if !paramPattern.MatchString(param) || param == "x" || param == "y" {
			return nil, nil, fmt.Errorf("%w: %q is not a param's name: a letter or _, then letters, digits and _", ErrInvalidIconParams, param)
		}
		var kind string
		err := s.QueryRow(`SELECT validation_type FROM fields WHERE id = ?`, field).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("%w: there is no field %q", ErrInvalidIconParams, field)
		}
		if err != nil {
			return nil, nil, err
		}
		if kind != "number" {
			return nil, nil, fmt.Errorf("%w: only a number field can set a param", ErrInvalidIconParams)
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, `DELETE FROM icon_param_bindings WHERE category_id = ?`, categoryID); err != nil {
		return nil, nil, err
	}
	for param, field := range bindings {
		if _, err := s.TxExec(tx, `INSERT INTO icon_param_bindings (category_id, param, field_id) VALUES (?, ?, ?)`, categoryID, param, field); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	out, err := s.IconParamBindings(categoryID)
	if err != nil {
		return nil, nil, err
	}
	save, err := s.RebuildIcons(ctx, c)
	if err != nil && !errors.Is(err, isolex.ErrUnavailable) {
		return out, nil, err
	}
	return out, save, nil
}

// contextWrapper is the wrapper drawing icon for an asset with these field
// values, by key, when the build draws one; "" otherwise.
func (s *Store) contextWrapper(ctx *assetContext, icon string, fieldValues map[string]string) (string, []paramValue, error) {
	if ctx.iconBindings == nil {
		var err error
		if ctx.iconBindings, err = readIconBindings(s.Query); err != nil {
			return "", nil, err
		}
	}
	bindings := ctx.iconBindings[icon]
	if len(bindings) == 0 {
		return "", nil, nil
	}
	params := boundParams(bindings, func(b iconBinding) (string, bool) {
		v, ok := fieldValues[b.fieldKey]
		return v, ok
	})
	if len(params) == 0 {
		return "", nil, nil
	}
	name := wrapperName(icon, params)
	if !ctx.iconNames[name] {
		return "", nil, nil
	}
	return name, params, nil
}
