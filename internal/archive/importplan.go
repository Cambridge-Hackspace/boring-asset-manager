package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fieldkey"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// here is what the instance has that an import is matched against.
type here struct {
	fields    map[string]hereField // by key
	actions   map[string][]string  // by key: id, label, confirm question
	relTypes  map[string][]string  // by name: id, inverse name, symmetric, single, location
	scopes    map[string]string    // by name: id
	templates map[string][]string  // by name: id, units, width, height, dpi, layout
	reports   map[string][]string  // by kind and name: id, spec
	flows     map[string][]string  // by address: id, name, definition
	images    map[string]string    // by SHA-256: id
	library   map[string][]string  // by path: id, source
	defined   map[string]bool      // icon names the library and categories' icons define
	iconNames map[string]bool      // categories' icons' names
	palette   map[string][2]string
	lastToken int64
	cats      map[string]*hereCat
}

type hereField struct{ id, label, kind, options string }

type hereCat struct {
	name, parent string
	scopes       []string
}

// queryRows reads rows of text, NULL as "".
func queryRows(ctx context.Context, st *store.Store, q querier, query string) ([][]string, error) {
	rows, err := q.QueryContext(ctx, st.Rebind(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func loadHere(ctx context.Context, st *store.Store, q querier) (*here, error) {
	h := &here{fields: map[string]hereField{}, actions: map[string][]string{}, relTypes: map[string][]string{}, scopes: map[string]string{},
		templates: map[string][]string{}, reports: map[string][]string{}, flows: map[string][]string{}, images: map[string]string{},
		library: map[string][]string{}, defined: map[string]bool{}, iconNames: map[string]bool{}, palette: map[string][2]string{}, cats: map[string]*hereCat{}}
	scopeNames := map[string]string{}
	steps := []struct {
		query string
		each  func([]string)
	}{
		{"SELECT field_key, id, label, validation_type, options FROM fields", func(r []string) { h.fields[r[0]] = hereField{r[1], r[2], r[3], r[4]} }},
		{"SELECT action_key, id, label, confirm_question FROM asset_actions", func(r []string) { h.actions[r[0]] = r[1:] }},
		{"SELECT name, id, inverse_name, is_symmetric, is_single, is_location FROM relation_types", func(r []string) { h.relTypes[r[0]] = r[1:] }},
		{"SELECT name, id FROM scopes", func(r []string) { h.scopes[r[0]], scopeNames[r[1]] = r[1], r[0] }},
		{"SELECT name, id, units, width, height, dpi, layout_json FROM templates ORDER BY id", func(r []string) {
			if _, ok := h.templates[r[0]]; !ok {
				h.templates[r[0]] = r[1:]
			}
		}},
		{"SELECT kind, name, id, spec FROM reports ORDER BY id", func(r []string) {
			if _, ok := h.reports[r[0]+"\x00"+r[1]]; !ok {
				h.reports[r[0]+"\x00"+r[1]] = r[2:]
			}
		}},
		{"SELECT slug, id, name, definition FROM flows", func(r []string) { h.flows[r[0]] = r[1:] }},
		{"SELECT sha256, id FROM images ORDER BY id", func(r []string) {
			if _, ok := h.images[r[0]]; !ok && r[0] != "" {
				h.images[r[0]] = r[1]
			}
		}},
		{"SELECT path, id, source FROM icon_library", func(r []string) {
			h.library[r[0]] = r[1:]
			for _, n := range ScanIcons(r[2]).Defines {
				h.defined[n] = true
			}
		}},
		{"SELECT icon_name, source FROM category_icons", func(r []string) {
			h.iconNames[r[0]] = true
			for _, n := range ScanIcons(r[1]).Defines {
				h.defined[n] = true
			}
		}},
		{"SELECT token, light, dark, sort_order FROM icon_palette", func(r []string) {
			h.palette[r[0]] = [2]string{r[1], r[2]}
			if n, _ := strconv.ParseInt(r[3], 10, 64); n > h.lastToken {
				h.lastToken = n
			}
		}},
		{"SELECT id, name, parent_id FROM categories", func(r []string) { h.cats[r[0]] = &hereCat{name: r[1], parent: r[2]} }},
		{"SELECT category_id, scope_id FROM category_scopes", func(r []string) {
			if c := h.cats[r[0]]; c != nil {
				c.scopes = append(c.scopes, scopeNames[r[1]])
			}
		}},
	}
	for _, s := range steps {
		rows, err := queryRows(ctx, st, q, s.query)
		if err != nil {
			return nil, fmt.Errorf("reading what the import is matched against: %w", err)
		}
		for _, r := range rows {
			s.each(r)
		}
	}
	return h, nil
}

// importNamespace, in tests, is the namespace imports' new IDs are made
// in, so they are known.
var importNamespace *uuid.UUID

// tableRows are rows to insert into a table.
type tableRows struct {
	t    *Table
	rows []map[string]any
}

// built is what a plan inserts: the rows of the tables read whole, ready,
// and how the rows streamed are mapped.
type built struct {
	ids  newIDs
	rows []tableRows
	// archive ID to ID here, of what is imported or used as it is; what
	// is skipped, or not merged, is absent.
	cats, fields, relTypes, images map[string]string
	// imagesStored are the label images whose files are stored, by ID in
	// the archive.
	imagesStored map[string]bool
	// iconRenames are the icon names copies are given, and catIcons the
	// categories' icons' among them, whose variants follow them.
	iconRenames map[string]string
	catIcons    map[string]bool
	kinds       []Kind
	// library, icons and palette are the icon sources and tokens added,
	// to compile.
	library, icons []store.IconFile
	palette        []models.IconColor
}

// matcher works out a plan.
type matcher struct {
	ctx  context.Context
	h    *here
	a    *archived
	opts ImportOptions
	plan *ImportPlan
	b    *built
	may  Reader
	// items by kind and archive ID, and the choices used
	items map[string]*ImportItem
	used  map[string]bool
	// keys an archive's field and action keys have here, and those
	// skipped
	fieldKeys, actionKeys     map[string]string
	skipKeys, skipActionKeys  map[string]bool
	reports, templates, flows map[string]string
	skippedFlows              map[string]bool
	skippedTemplates          map[string]bool
	scopeNames                map[string]string // archive scope ID to its name here
}

func (m *matcher) problem(format string, args ...any) {
	m.plan.Problems = append(m.plan.Problems, fmt.Sprintf(format, args...))
}

func (m *matcher) note(it *ImportItem, s string) {
	if it.Note != "" {
		it.Note += " "
	}
	it.Note += s
}

// item adds an item of the archive's, clashing with existing if that is
// not nil.
func (m *matcher) item(kind, id, name string, existing *Ref, identical bool, choices []Choice) *ImportItem {
	it := &ImportItem{Ref: Ref{Kind: kind, ID: id, Name: name}, Action: ChoiceCreate}
	if existing != nil {
		it.Existing, it.Identical, it.Choices = existing, identical, choices
		switch {
		case identical && hasChoice(choices, ChoiceExisting):
			it.Action = ChoiceExisting
		case hasChoice(choices, ChoiceCopy):
			it.Action = ChoiceCopy
		default:
			it.Action = choices[0]
		}
	}
	m.items[it.Key()] = it
	return it
}

func hasChoice(cs []Choice, c Choice) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

var allChoices = []Choice{ChoiceExisting, ChoiceCopy, ChoiceSkip}

// choose applies the importer's choices.
func (m *matcher) choose() error {
	keys := make([]string, 0, len(m.opts.Choices))
	for k := range m.opts.Choices {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := m.opts.Choices[k]
		it := m.items[k]
		if it == nil {
			return fmt.Errorf("%w: the archive holds nothing that is %s", ErrImport, k)
		}
		if len(it.Choices) == 0 {
			if c == ChoiceCreate {
				continue
			}
			return fmt.Errorf("%w: %s %q clashes with nothing here, so is created", ErrImport, strings.ReplaceAll(it.Kind, "_", " "), it.Name)
		}
		if !hasChoice(it.Choices, c) {
			var names []string
			for _, x := range it.Choices {
				names = append(names, string(x))
			}
			return fmt.Errorf("%w: for %s %q, choose %s", ErrImport, strings.ReplaceAll(it.Kind, "_", " "), it.Name, strings.Join(names, ", "))
		}
		it.Action = c
	}
	return nil
}

// imported reports whether an item is created or copied.
func imported(it *ImportItem) bool { return it.Action == ChoiceCreate || it.Action == ChoiceCopy }

// build works out what importing an archive does, against what q has.
func build(ctx context.Context, st *store.Store, q querier, a *archived, opts ImportOptions) (*ImportPlan, *built, error) {
	h, err := loadHere(ctx, st, q)
	if err != nil {
		return nil, nil, err
	}
	plan := &ImportPlan{Header: a.header, Kinds: a.kinds, Items: []ImportItem{}, Permissions: []string{}, Missing: []string{}, Warnings: []string{}, Problems: []string{}}
	ns := uuid.New()
	if importNamespace != nil {
		ns = *importNamespace
	}
	b := &built{ids: newIDs(ns), cats: map[string]string{}, fields: map[string]string{}, relTypes: map[string]string{}, images: map[string]string{},
		imagesStored: map[string]bool{}, iconRenames: map[string]string{}, catIcons: map[string]bool{}, kinds: a.kinds}
	if opts.Anchor != "" {
		c := h.cats[opts.Anchor]
		if c == nil {
			return nil, nil, fmt.Errorf("%w: there is no category %s to import under", ErrImport, opts.Anchor)
		}
		plan.Anchor = &Ref{Kind: ItemCategory, ID: opts.Anchor, Name: c.name}
	}
	may := opts.May
	if may == nil {
		may = func(string, []string) bool { return true }
	}
	m := &matcher{ctx: ctx, h: h, a: a, opts: opts, plan: plan, b: b, may: may, items: map[string]*ImportItem{}, used: map[string]bool{},
		fieldKeys: map[string]string{}, actionKeys: map[string]string{}, skipKeys: map[string]bool{}, skipActionKeys: map[string]bool{},
		reports: map[string]string{}, templates: map[string]string{}, flows: map[string]string{}, skippedFlows: map[string]bool{},
		skippedTemplates: map[string]bool{}, scopeNames: map[string]string{}}
	m.match()
	if err := m.choose(); err != nil {
		return nil, nil, err
	}
	m.name()
	m.rows()
	m.permissions()
	for _, it := range m.items {
		plan.Items = append(plan.Items, *it)
	}
	sort.Slice(plan.Items, func(i, j int) bool { return lessRef(plan.Items[i].Ref, plan.Items[j].Ref) })
	plan.Files, plan.FileBytes = a.files, a.bytes
	sort.Strings(plan.Problems)
	plan.Fingerprint = fingerprint(plan)
	return plan, b, nil
}

// match makes an item of each thing the archive holds that is matched by
// a stable key, and of each category.
func (m *matcher) match() {
	h, rows := m.h, m.a.rows
	for _, r := range rows["scopes"] {
		var ex *Ref
		if id, ok := h.scopes[str(r, "name")]; ok {
			ex = &Ref{Kind: ItemScope, ID: id, Name: str(r, "name")}
		}
		m.item(ItemScope, str(r, "id"), str(r, "name"), ex, true, allChoices)
	}
	for _, r := range rows["relation_types"] {
		var ex *Ref
		same := false
		if e, ok := h.relTypes[str(r, "name")]; ok {
			ex = &Ref{Kind: ItemRelationType, ID: e[0], Name: str(r, "name")}
			same = e[1] == str(r, "inverse_name") && truthy(e[2]) == boolOf(r["is_symmetric"]) && truthy(e[3]) == boolOf(r["is_single"]) && truthy(e[4]) == boolOf(r["is_location"])
		}
		m.item(ItemRelationType, str(r, "id"), str(r, "name"), ex, same, allChoices)
	}
	for _, r := range rows["fields"] {
		var ex *Ref
		same := false
		key := str(r, "field_key")
		if e, ok := h.fields[key]; ok {
			ex = &Ref{Kind: ItemField, ID: e.id, Name: e.label + " (" + key + ")"}
			same = e.label == str(r, "label") && e.kind == str(r, "validation_type") && sameJSON(e.options, str(r, "options"))
		}
		m.item(ItemField, str(r, "id"), str(r, "label")+" ("+key+")", ex, same, allChoices)
	}
	for _, r := range rows["asset_actions"] {
		var ex *Ref
		same := false
		key := str(r, "action_key")
		if e, ok := h.actions[key]; ok {
			ex = &Ref{Kind: ItemAction, ID: e[0], Name: e[1] + " (" + key + ")"}
			same = e[1] == str(r, "label") && e[2] == str(r, "confirm_question")
		}
		m.item(ItemAction, str(r, "id"), str(r, "label")+" ("+key+")", ex, same, allChoices)
	}
	for _, r := range rows["reports"] {
		kind := ItemReport
		if str(r, "kind") == "scan" {
			kind = ItemScanTemplate
		}
		var ex *Ref
		same := false
		if e, ok := h.reports[str(r, "kind")+"\x00"+str(r, "name")]; ok {
			ex = &Ref{Kind: kind, ID: e[0], Name: str(r, "name")}
			same = sameJSON(e[1], str(r, "spec"))
		}
		m.item(kind, str(r, "id"), str(r, "name"), ex, same, allChoices)
	}
	// images are the same by their content, and used as they are
	for _, r := range rows["images"] {
		it := m.item(ItemImage, str(r, "id"), str(r, "filename"), nil, false, nil)
		if id, ok := h.images[str(r, "sha256")]; ok {
			it.Action, it.Existing, it.Identical = ChoiceExisting, &Ref{Kind: ItemImage, ID: id, Name: str(r, "filename")}, true
		}
	}
	for _, r := range rows["templates"] {
		var ex *Ref
		same := false
		if e, ok := h.templates[str(r, "name")]; ok {
			ex = &Ref{Kind: ItemLabelTemplate, ID: e[0], Name: str(r, "name")}
			w, _ := strconv.ParseFloat(e[2], 64)
			ht, _ := strconv.ParseFloat(e[3], 64)
			same = e[1] == str(r, "units") && w == floatOf(r["width"]) && ht == floatOf(r["height"]) && e[4] == fmt.Sprint(r["dpi"]) &&
				sameJSON(e[5], m.imagesAsHere(str(r, "layout_json")))
		}
		m.item(ItemLabelTemplate, str(r, "id"), str(r, "name"), ex, same, allChoices)
	}
	for _, r := range rows["flows"] {
		var ex *Ref
		same := false
		slug := str(r, "slug")
		if e, ok := h.flows[slug]; ok {
			ex = &Ref{Kind: ItemFlow, ID: e[0], Name: e[1]}
			same = e[1] == str(r, "name") && sameJSON(e[2], str(r, "definition"))
		}
		m.item(ItemFlow, str(r, "id"), str(r, "name"), ex, same, allChoices)
	}
	for _, r := range rows["icon_library"] {
		path, src := str(r, "path"), str(r, "source")
		var ex *Ref
		same := false
		if e, ok := h.library[path]; ok {
			ex = &Ref{Kind: ItemIconFile, ID: e[0], Name: path}
			same = e[1] == src
		} else if n := m.clash(src, ""); n != "" {
			ex = &Ref{Kind: ItemIconFile, Name: n}
		}
		m.item(ItemIconFile, str(r, "id"), path, ex, same, []Choice{ChoiceExisting, ChoiceCopy})
	}
	if hasKind(m.a.kinds, KindCatalog) {
		for _, r := range rows["category_icons"] {
			name := str(r, "icon_name")
			var ex *Ref
			if n := m.clash(str(r, "source"), name); n != "" {
				ex = &Ref{Kind: ItemCategoryIcon, Name: n}
			}
			m.item(ItemCategoryIcon, str(r, "category_id"), name, ex, false, []Choice{ChoiceCopy, ChoiceSkip})
		}
	} else if len(rows["category_icons"]) > 0 {
		m.plan.Warnings = append(m.plan.Warnings, "Categories' icons come with their categories, which are not being imported, and are left out.")
	}
	for _, r := range rows["categories"] {
		m.item(ItemCategory, str(r, "id"), str(r, "name"), nil, false, nil)
	}
	if len(rows["icon_settings"]) > 0 {
		m.plan.Warnings = append(m.plan.Warnings, "This instance's icon settings are kept; the archive's are not imported.")
	}
	for _, r := range rows["icon_palette"] {
		if e, ok := m.h.palette[str(r, "token")]; ok && (e[0] != str(r, "light") || e[1] != str(r, "dark")) {
			m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("The palette's %s is kept as this instance has it.", str(r, "token")))
		}
	}
}

// clash names the icon definition here a source's, or a category's icon
// name, clashes with; "" when none does.
func (m *matcher) clash(src, name string) string {
	if name != "" && m.h.iconNames[name] {
		return name
	}
	for _, d := range ScanIcons(src).Defines {
		if m.h.defined[d] {
			return d
		}
	}
	return ""
}

func truthy(s string) bool { return s == "1" || s == "true" || s == "t" }

func boolOf(v any) bool { b, _ := v.(bool); return b }

func floatOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	}
	return 0
}

// sameJSON reports whether two JSON texts are the same value.
func sameJSON(a, b string) bool {
	if a == b {
		return true
	}
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// imagesAsHere is a layout with the images it places named as they are
// here, where the same image is, to compare.
func (m *matcher) imagesAsHere(layout string) string {
	for _, r := range m.a.rows["images"] {
		if id, ok := m.h.images[str(r, "sha256")]; ok {
			layout = strings.ReplaceAll(layout, str(r, "id"), id)
		}
	}
	return layout
}

// name gives copies their names here, and works out what everything is
// called and found by here.
func (m *matcher) name() {
	h := m.h
	taken := func(kind string) map[string]bool {
		out := map[string]bool{}
		for _, it := range m.items {
			if it.Kind == kind && it.Action == ChoiceCreate {
				out[it.Name] = true
			}
		}
		return out
	}
	byKind := func(kind string) []*ImportItem {
		var out []*ImportItem
		for _, it := range m.items {
			if it.Kind == kind {
				out = append(out, it)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	rowOf := func(table, col, id string) map[string]any {
		for _, r := range m.a.rows[table] {
			if str(r, col) == id {
				return r
			}
		}
		return nil
	}
	// what each archive ID is here: new, or the existing one's
	idHere := func(it *ImportItem) string {
		switch it.Action {
		case ChoiceCreate, ChoiceCopy:
			return m.b.ids.of(it.ID)
		case ChoiceExisting:
			if it.Existing != nil {
				return it.Existing.ID
			}
		}
		return ""
	}

	// fields and actions, by key
	keysTaken := map[string]bool{}
	for k := range h.fields {
		keysTaken[k] = true
	}
	for _, r := range m.a.rows["fields"] {
		keysTaken[str(r, "field_key")] = true
	}
	for _, it := range byKind(ItemField) {
		key := str(rowOf("fields", "id", it.ID), "field_key")
		switch it.Action {
		case ChoiceCopy:
			it.As = unique(key, keysTaken, "_%d", 64, fieldkey.Valid)
			m.fieldKeys[key] = it.As
		case ChoiceSkip:
			m.skipKeys[key] = true
		default:
			m.fieldKeys[key] = key
		}
		if id := idHere(it); id != "" {
			m.b.fields[it.ID] = id
		}
	}
	actionsTaken := map[string]bool{}
	for k := range h.actions {
		actionsTaken[k] = true
	}
	for _, r := range m.a.rows["asset_actions"] {
		actionsTaken[str(r, "action_key")] = true
	}
	for _, it := range byKind(ItemAction) {
		key := str(rowOf("asset_actions", "id", it.ID), "action_key")
		switch it.Action {
		case ChoiceCopy:
			it.As = unique(key, actionsTaken, "_%d", 64, func(string) bool { return true })
			m.actionKeys[key] = it.As
		case ChoiceSkip:
			m.skipActionKeys[key] = true
		default:
			m.actionKeys[key] = key
		}
	}
	named := func(kind string, here map[string]bool, format string, limit int, valid func(string) bool, into map[string]string) {
		t := taken(kind)
		for n := range here {
			t[n] = true
		}
		for _, it := range byKind(kind) {
			if it.Action == ChoiceCopy {
				it.As = unique(it.Name, t, format, limit, valid)
			}
			if id := idHere(it); id != "" && into != nil {
				into[it.ID] = id
			}
		}
	}
	keysOf := func(mp any) map[string]bool {
		out := map[string]bool{}
		for _, k := range reflect.ValueOf(mp).MapKeys() {
			out[k.String()] = true
		}
		return out
	}
	scopeIDs := map[string]string{}
	named(ItemScope, keysOf(h.scopes), "-%d", 64, rbac.ValidScopeName, scopeIDs)
	for _, it := range byKind(ItemScope) {
		if it.Action == ChoiceSkip {
			continue
		}
		m.scopeNames[it.ID] = it.Name
		if it.As != "" {
			m.scopeNames[it.ID] = it.As
		}
	}
	named(ItemRelationType, keysOf(h.relTypes), " (%d)", 255, nil, m.b.relTypes)
	reportNames := map[string]bool{}
	for k := range h.reports {
		_, n, _ := strings.Cut(k, "\x00")
		reportNames[n] = true
	}
	named(ItemReport, reportNames, " (%d)", 255, nil, m.reports)
	named(ItemScanTemplate, reportNames, " (%d)", 255, nil, m.reports)
	named(ItemLabelTemplate, keysOf(h.templates), " (%d)", 255, nil, m.templates)
	for _, it := range byKind(ItemLabelTemplate) {
		if it.Action == ChoiceSkip {
			m.skippedTemplates[it.ID] = true
		}
	}
	flowSlugs := keysOf(h.flows)
	for _, r := range m.a.rows["flows"] {
		flowSlugs[str(r, "slug")] = true
	}
	for _, it := range byKind(ItemFlow) {
		slug := str(rowOf("flows", "id", it.ID), "slug")
		if it.Action == ChoiceCopy {
			it.As = unique(slug, flowSlugs, "-%d", 64, rbac.ValidScopeName)
		}
		if it.Action == ChoiceSkip {
			m.skippedFlows[it.ID] = true
		}
		if id := idHere(it); id != "" {
			m.flows[it.ID] = id
		}
	}
	for _, it := range byKind(ItemImage) {
		m.b.images[it.ID] = idHere(it)
		if it.Action == ChoiceCreate {
			m.b.imagesStored[it.ID] = true
			if !m.a.images[it.ID] {
				m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("The image %s's file is not in the archive.", it.Name))
			}
		}
	}
	for _, it := range byKind(ItemCategory) {
		m.b.cats[it.ID] = m.b.ids.of(it.ID)
	}

	// icons copied are given new names, and so is everything they define
	iconsTaken := map[string]bool{}
	for n := range h.defined {
		iconsTaken[n] = true
	}
	for n := range h.iconNames {
		iconsTaken[n] = true
	}
	for _, r := range m.a.rows["icon_library"] {
		for _, d := range ScanIcons(str(r, "source")).Defines {
			iconsTaken[d] = true
		}
	}
	for _, r := range m.a.rows["category_icons"] {
		iconsTaken[str(r, "icon_name")] = true
		for _, d := range ScanIcons(str(r, "source")).Defines {
			iconsTaken[d] = true
		}
	}
	validIcon := func(n string) bool { return store.ValidIconName(n) == nil }
	rename := func(src string) {
		for _, d := range ScanIcons(src).Defines {
			if _, ok := m.b.iconRenames[d]; !ok {
				m.b.iconRenames[d] = unique(d, iconsTaken, "-%d", 64, validIcon)
			}
		}
	}
	paths := keysOf(h.library)
	for _, r := range m.a.rows["icon_library"] {
		paths[str(r, "path")] = true
	}
	for _, it := range byKind(ItemIconFile) {
		if it.Action == ChoiceCopy {
			r := rowOf("icon_library", "id", it.ID)
			rename(str(r, "source"))
			base := strings.TrimSuffix(it.Name, ".ilx")
			for n := 2; ; n++ {
				p := fmt.Sprintf("%s-%d.ilx", base, n)
				if !paths[p] && store.ValidIconLibraryPath(p) == nil {
					it.As, paths[p] = p, true
					break
				}
			}
		}
	}
	for _, it := range byKind(ItemCategoryIcon) {
		if it.Action == ChoiceCopy {
			r := rowOf("category_icons", "category_id", it.ID)
			rename(str(r, "source"))
			if _, ok := m.b.iconRenames[it.Name]; !ok {
				m.b.iconRenames[it.Name] = unique(it.Name, iconsTaken, "-%d", 64, validIcon)
			}
			it.As = m.b.iconRenames[it.Name]
			m.b.catIcons[it.Name] = true
		}
	}
}

// unique is base, or base with a number in format after it, that taken
// does not hold and valid accepts, within limit bytes; it is taken after.
func unique(base string, taken map[string]bool, format string, limit int, valid func(string) bool) string {
	for n := 2; ; n++ {
		suffix := fmt.Sprintf(format, n)
		b := base
		if len(b)+len(suffix) > limit {
			b = b[:limit-len(suffix)]
		}
		c := b + suffix
		if !taken[c] && (valid == nil || valid(c)) {
			taken[c] = true
			return c
		}
		if n > 100000 {
			return c
		}
	}
}

// fieldKey is the key a field the archive names by key has here, noting
// a problem when it is skipped or nowhere.
func (m *matcher) fieldKey(key, who string) string {
	if key == "" {
		return ""
	}
	if m.skipKeys[key] {
		m.problem("%s names the field %q, which is skipped", who, key)
		return key
	}
	if k, ok := m.fieldKeys[key]; ok {
		return k
	}
	if _, ok := m.h.fields[key]; !ok {
		m.problem("%s names the field %q, which neither the archive nor this instance has", who, key)
	}
	return key
}

var fieldBinding = regexp.MustCompile(`\{\{\s*field\.([a-z0-9_]+)\s*\}\}`)

// bindings renames the fields a binding names.
func (m *matcher) bindings(s, who string) string {
	return fieldBinding.ReplaceAllStringFunc(s, func(match string) string {
		key := fieldBinding.FindStringSubmatch(match)[1]
		return "{{field." + m.fieldKey(key, who) + "}}"
	})
}

var flowValue = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// flowText renames the fields a flow's text names as values, {key}.
func (m *matcher) flowText(s string) string {
	return flowValue.ReplaceAllStringFunc(s, func(match string) string {
		key := match[1 : len(match)-1]
		if k, ok := m.fieldKeys[key]; ok {
			return "{" + k + "}"
		}
		return match
	})
}

// rows works out the rows inserted.
func (m *matcher) rows() {
	b, a, h := m.b, m.a, m.h
	add := func(table string, rows []map[string]any) {
		if len(rows) > 0 {
			b.rows = append(b.rows, tableRows{t: tableByName(table), rows: rows})
		}
	}
	copied := func(kind, id string) (*ImportItem, bool) {
		it := m.items[kind+":"+id]
		return it, it != nil && imported(it)
	}
	as := func(it *ImportItem) string {
		if it.As != "" {
			return it.As
		}
		return it.Name
	}

	var scopes, relTypes, fields, actions, reports []map[string]any
	for _, r := range a.rows["scopes"] {
		if it, ok := copied(ItemScope, str(r, "id")); ok {
			scopes = append(scopes, with(r, "id", b.ids.of(it.ID), "name", as(it)))
		}
	}
	for _, r := range a.rows["relation_types"] {
		if it, ok := copied(ItemRelationType, str(r, "id")); ok {
			relTypes = append(relTypes, with(r, "id", b.ids.of(it.ID), "name", as(it)))
		}
	}
	for _, r := range a.rows["fields"] {
		if it, ok := copied(ItemField, str(r, "id")); ok {
			fields = append(fields, with(r, "id", b.ids.of(it.ID), "field_key", m.fieldKeys[str(r, "field_key")]))
		}
	}
	for _, r := range a.rows["asset_actions"] {
		if it, ok := copied(ItemAction, str(r, "id")); ok {
			actions = append(actions, with(r, "id", b.ids.of(it.ID), "action_key", m.actionKeys[str(r, "action_key")]))
		}
	}
	for _, r := range a.rows["reports"] {
		kind := ItemReport
		if str(r, "kind") == "scan" {
			kind = ItemScanTemplate
		}
		if it, ok := copied(kind, str(r, "id")); ok {
			spec := m.reportSpec(str(r, "spec"), it)
			reports = append(reports, with(r, "id", b.ids.of(it.ID), "name", as(it), "spec", spec, "is_default", false))
		}
	}
	add("scopes", scopes)
	add("relation_types", relTypes)
	add("fields", fields)
	add("asset_actions", actions)
	add("reports", reports)

	// categories, each after its parent; the top ones under the anchor
	var anchor any
	if m.opts.Anchor != "" {
		anchor = m.opts.Anchor
	}
	parentOf := map[string]string{}
	for _, r := range a.rows["categories"] {
		parentOf[str(r, "id")] = str(r, "parent_id")
	}
	depth := func(id string) int {
		d := 0
		for p := parentOf[id]; p != "" && d < 100; p = parentOf[p] {
			d++
		}
		return d
	}
	cats := append([]map[string]any(nil), a.rows["categories"]...)
	sort.SliceStable(cats, func(i, j int) bool { return depth(str(cats[i], "id")) < depth(str(cats[j], "id")) })
	var catRows []map[string]any
	for _, r := range cats {
		var parent any = anchor
		if p := str(r, "parent_id"); p != "" {
			if id, ok := b.cats[p]; ok {
				parent = id
			}
		}
		var scan any
		if id, ok := m.reports[str(r, "scan_template_id")]; ok {
			scan = id
		}
		catRows = append(catRows, with(r, "id", b.cats[str(r, "id")], "parent_id", parent, "scan_template_id", scan))
	}
	add("categories", catRows)
	join := func(table, col, kind string, ids map[string]string) {
		var out []map[string]any
		seen := map[string]bool{}
		for _, r := range a.rows[table] {
			cat, ok := b.cats[str(r, "category_id")]
			other, ok2 := ids[str(r, col)]
			if !ok || !ok2 || other == "" || seen[cat+"\x00"+other] {
				continue
			}
			seen[cat+"\x00"+other] = true
			out = append(out, with(r, "category_id", cat, col, other))
		}
		add(table, out)
	}
	actionIDs := map[string]string{}
	for _, it := range m.items {
		if it.Kind == ItemAction {
			switch it.Action {
			case ChoiceCreate, ChoiceCopy:
				actionIDs[it.ID] = b.ids.of(it.ID)
			case ChoiceExisting:
				actionIDs[it.ID] = it.Existing.ID
			}
		}
	}
	scopeIDs := map[string]string{}
	for _, it := range m.items {
		if it.Kind == ItemScope {
			switch it.Action {
			case ChoiceCreate, ChoiceCopy:
				scopeIDs[it.ID] = b.ids.of(it.ID)
			case ChoiceExisting:
				scopeIDs[it.ID] = it.Existing.ID
			}
		}
	}
	join("category_fields", "field_id", ItemField, b.fields)
	join("category_actions", "action_id", ItemAction, actionIDs)
	join("category_relation_types", "relation_type_id", ItemRelationType, b.relTypes)
	join("category_scopes", "scope_id", ItemScope, scopeIDs)

	var images, templates []map[string]any
	for _, r := range a.rows["images"] {
		if b.imagesStored[str(r, "id")] {
			images = append(images, with(r, "id", b.images[str(r, "id")]))
		}
	}
	for _, r := range a.rows["templates"] {
		if it, ok := copied(ItemLabelTemplate, str(r, "id")); ok {
			layout := m.layout(str(r, "layout_json"), fmt.Sprintf("label template %q", it.Name))
			templates = append(templates, with(r, "id", b.ids.of(it.ID), "name", as(it), "layout_json", layout))
		}
	}
	add("images", images)
	add("templates", templates)
	join("category_label_templates", "template_id", ItemLabelTemplate, m.templates)

	// icons
	var palette, library, catIcons, params, texts []map[string]any
	order := h.lastToken
	for _, r := range a.rows["icon_palette"] {
		if _, ok := h.palette[str(r, "token")]; !ok {
			order++
			palette = append(palette, with(r, "sort_order", order))
			b.palette = append(b.palette, models.IconColor{Token: str(r, "token"), Light: str(r, "light"), Dark: str(r, "dark")})
		}
	}
	for _, r := range a.rows["icon_library"] {
		if it, ok := copied(ItemIconFile, str(r, "id")); ok {
			src := RenameIcons(str(r, "source"), b.iconRenames)
			library = append(library, with(r, "id", b.ids.of(it.ID), "path", as(it), "source", src, "draft", nil, "draft_errors", nil))
			b.library = append(b.library, store.IconFile{ID: b.ids.of(it.ID), Name: as(it), Source: src})
		}
	}
	iconCats := map[string]bool{}
	for _, r := range a.rows["category_icons"] {
		cat := str(r, "category_id")
		it, ok := copied(ItemCategoryIcon, cat)
		if !ok || b.cats[cat] == "" {
			continue
		}
		iconCats[cat] = true
		src := RenameIcons(str(r, "source"), b.iconRenames)
		catIcons = append(catIcons, with(r, "category_id", b.cats[cat], "icon_name", as(it), "source", src, "draft", nil, "draft_errors", nil))
		b.icons = append(b.icons, store.IconFile{ID: b.cats[cat], Name: as(it), Source: src})
	}
	for _, table := range []string{"icon_param_bindings", "icon_text_bindings"} {
		for _, r := range a.rows[table] {
			cat := str(r, "category_id")
			if !iconCats[cat] {
				continue
			}
			var field any
			if f := str(r, "field_id"); f != "" {
				id, ok := b.fields[f]
				if !ok {
					continue
				}
				field = id
			}
			row := with(r, "category_id", b.cats[cat], "field_id", field)
			if table == "icon_param_bindings" {
				params = append(params, row)
			} else {
				texts = append(texts, row)
			}
		}
	}
	add("icon_palette", palette)
	add("icon_library", library)
	add("category_icons", catIcons)
	add("icon_param_bindings", params)
	add("icon_text_bindings", texts)

	var flowRows []map[string]any
	for _, r := range a.rows["flows"] {
		it, ok := copied(ItemFlow, str(r, "id"))
		if !ok {
			continue
		}
		var d flows.Definition
		if err := json.Unmarshal([]byte(str(r, "definition")), &d); err != nil {
			m.problem("flow %q cannot be read: %v", it.Name, err)
			continue
		}
		enabled := m.flow(it, &d) && boolOf(r["enabled"])
		def, _ := json.Marshal(d)
		slug := str(r, "slug")
		if it.As != "" {
			slug = it.As
		}
		flowRows = append(flowRows, with(r, "id", b.ids.of(it.ID), "slug", slug, "definition", string(def), "enabled", enabled, "created_by", nil))
	}
	add("flows", flowRows)

	// assets, which are streamed, counted
	if hasKind(a.kinds, KindAssets) {
		if !hasKind(a.kinds, KindCatalog) {
			m.problem("assets need their categories: import the catalog too")
		}
		c := a.counts
		for f, n := range a.values {
			if _, ok := b.fields[f]; !ok {
				c.ValuesDropped += n
			}
		}
		for t, n := range a.links {
			if _, ok := b.relTypes[t]; !ok {
				c.LinksDropped += n
			}
		}
		c.Links -= c.LinksDropped
		m.plan.Assets = &c
	}
}

// with is a copy of a row with columns set.
func with(r map[string]any, kv ...any) map[string]any {
	out := make(map[string]any, len(r))
	for k, v := range r {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// reportSpec is a report's spec as imported: the categories it looks in,
// and the fields it binds, as they are here.
func (m *matcher) reportSpec(spec string, it *ImportItem) string {
	who := fmt.Sprintf("%s %q", strings.ReplaceAll(it.Kind, "_", " "), it.Name)
	var s map[string]any
	if json.Unmarshal([]byte(spec), &s) != nil {
		return spec
	}
	if f, ok := s["filter"].(map[string]any); ok {
		if ids, ok := f["category_ids"].([]any); ok {
			var out []any
			for _, id := range ids {
				if n, ok := m.b.cats[fmt.Sprint(id)]; ok {
					out = append(out, n)
				} else if _, here := m.h.cats[fmt.Sprint(id)]; here {
					out = append(out, id)
				}
			}
			if len(out) < len(ids) {
				m.note(it, "It no longer looks in the categories that are not imported.")
			}
			if out == nil {
				out = []any{}
			}
			f["category_ids"] = out
		}
	}
	if secs, ok := s["sections"].([]any); ok {
		for _, sec := range secs {
			sec, ok := sec.(map[string]any)
			if !ok {
				continue
			}
			if cols, ok := sec["columns"].([]any); ok {
				for _, c := range cols {
					if c, ok := c.(map[string]any); ok {
						if bnd, ok := c["binding"].(string); ok {
							c["binding"] = m.bindings(bnd, who)
						}
					}
				}
			}
			if g, ok := sec["group_by"].(string); ok {
				if key, ok := strings.CutPrefix(g, "field:"); ok {
					sec["group_by"] = "field:" + m.fieldKey(key, who)
				}
			}
		}
	}
	out, _ := json.Marshal(s)
	return string(out)
}

// layout is a label template's layout as imported: its images, the fields
// it binds and the icons it draws, as they are here.
func (m *matcher) layout(layout, who string) string {
	var doc map[string]any
	if json.Unmarshal([]byte(layout), &doc) != nil {
		return layout
	}
	var element func(e map[string]any)
	element = func(e map[string]any) {
		if id, ok := e["image_id"].(string); ok && id != "" {
			if n, ok := m.b.images[id]; ok && n != "" {
				e["image_id"] = n
			} else {
				m.problem("%s places an image the archive does not hold", who)
			}
		}
		if bnd, ok := e["content_binding"].(string); ok {
			e["content_binding"] = m.bindings(bnd, who)
		}
		if icon, ok := e["icon"].(string); ok {
			e["icon"] = m.iconName(icon)
		}
		if alt, ok := e["alternative"].(map[string]any); ok {
			element(alt)
		}
	}
	if els, ok := doc["elements"].([]any); ok {
		for _, e := range els {
			if e, ok := e.(map[string]any); ok {
				element(e)
			}
		}
	}
	out, _ := json.Marshal(doc)
	return string(out)
}

// iconName is an icon's name as imported: a copy's, or a variant of a
// category's icon copied, or as it is.
func (m *matcher) iconName(name string) string {
	return iconRenamed(m.b, name)
}

func iconRenamed(b *built, name string) string {
	if n, ok := b.iconRenames[name]; ok {
		return n
	}
	for old := range b.catIcons {
		if rest, ok := strings.CutPrefix(name, old+"-"); ok {
			return b.iconRenames[old] + "-" + rest
		}
	}
	return name
}

// flow rewrites a flow's definition as imported, reporting whether it may
// stay turned on.
func (m *matcher) flow(it *ImportItem, d *flows.Definition) bool {
	who := fmt.Sprintf("flow %q", it.Name)
	on := true
	cat := func(id string) string {
		if id == "" {
			return ""
		}
		if n, ok := m.b.cats[id]; ok {
			return n
		}
		if _, ok := m.h.cats[id]; !ok {
			m.problem("%s names a category the archive does not hold: import the catalog too", who)
		}
		return id
	}
	for i, id := range d.CategoryIDs {
		d.CategoryIDs[i] = cat(id)
	}
	d.LeaveUntil = m.fieldKey(d.LeaveUntil, who)
	for i := range d.Steps {
		s := &d.Steps[i]
		s.Text, s.Name, s.NameLabel = m.flowText(s.Text), m.flowText(s.Name), m.flowText(s.NameLabel)
		for j, id := range s.Categories {
			s.Categories[j] = cat(id)
		}
		s.Fixed = cat(s.Fixed)
		if s.When != nil && s.When.Value == flows.ValueCategoryID {
			s.When.To = cat(s.When.To)
		}
		for j := range s.Fields {
			s.Fields[j].Key = m.fieldKey(s.Fields[j].Key, who)
			s.Fields[j].Default = m.flowText(s.Fields[j].Default)
		}
		for j := range s.Questions {
			s.Questions[j].Default = m.flowText(s.Questions[j].Default)
		}
		if s.Match != nil {
			s.Match.Field = m.fieldKey(s.Match.Field, who)
			s.Match.Value = m.flowText(s.Match.Value)
		}
		if s.FiledBy != "" {
			if m.skippedFlows[s.FiledBy] {
				m.problem("%s finds what the flow it names files, which is skipped", who)
			} else if id, ok := m.flows[s.FiledBy]; ok {
				s.FiledBy = id
			}
		}
		if s.Location != nil && s.Location.AssetID != "" {
			if m.a.assetIDs[s.Location.AssetID] {
				s.Location.AssetID = m.b.ids.of(s.Location.AssetID)
			} else {
				s.Location.Mode, s.Location.AssetID = flows.PlaceScan, ""
				m.note(it, "Its place is asked for, as the one it named is not imported.")
			}
		}
		if s.Label != nil {
			if t := s.Label.TemplateID; t != "" {
				if m.skippedTemplates[t] {
					m.problem("%s prints with a label template that is skipped", who)
				} else if id, ok := m.templates[t]; ok {
					s.Label.TemplateID = id
				} else {
					s.Label.TemplateID = ""
					m.note(it, "It prints with the category's label template, as the one it named is not imported.")
				}
			}
			if s.Label.Destination != "" {
				s.Label.Destination = ""
				if s.Label.Mode == flows.LabelPrint {
					s.Label.Mode = flows.LabelQueue
				}
				m.note(it, "Its labels go to the print queue, as printers are this instance's own.")
			}
		}
		var changes []flows.Change
		for _, c := range s.Changes {
			c.Note = m.flowText(c.Note)
			c.CategoryID = cat(c.CategoryID)
			if c.Action != "" {
				if m.skipActionKeys[c.Action] {
					m.problem("%s presses the action %q, which is skipped", who, c.Action)
				} else if k, ok := m.actionKeys[c.Action]; ok {
					c.Action = k
				} else if _, ok := m.h.actions[c.Action]; !ok {
					m.problem("%s presses the action %q, which neither the archive nor this instance has", who, c.Action)
				}
			}
			if len(c.Fields) > 0 {
				f := map[string]string{}
				for k, v := range c.Fields {
					f[m.fieldKey(k, who)] = m.flowText(v)
				}
				c.Fields = f
			}
			if c.Kind == flows.ChangePlace && c.AssetID != "" {
				if m.a.assetIDs[c.AssetID] {
					c.AssetID = m.b.ids.of(c.AssetID)
				} else {
					m.note(it, "It no longer moves things to a place, as the one it named is not imported, and is turned off until that is set again.")
					on = false
					continue
				}
			}
			changes = append(changes, c)
		}
		if s.Changes != nil {
			s.Changes = changes
		}
	}
	return on
}

// permissions works out what the import needs of the importer.
func (m *matcher) permissions() {
	need := map[string]bool{}
	for _, it := range m.items {
		if !imported(it) {
			continue
		}
		switch it.Kind {
		case ItemCategory, ItemField, ItemAction, ItemRelationType, ItemScope:
			need[rbac.PermCategoriesManage] = true
		case ItemLabelTemplate, ItemImage:
			need[rbac.PermTemplatesManage] = true
		case ItemReport, ItemScanTemplate:
			need[rbac.PermReportsManage] = true
		case ItemIconFile, ItemCategoryIcon:
			need[rbac.PermIconsManage] = true
		case ItemFlow:
			need[rbac.PermFlowsManage] = true
		}
	}
	if len(m.b.palette) > 0 {
		need[rbac.PermIconsManage] = true
	}
	var perms []string
	for p := range need {
		perms = append(perms, p)
	}
	sort.Strings(perms)
	for _, p := range perms {
		m.plan.Permissions = append(m.plan.Permissions, p)
		if !m.may(p, nil) {
			m.plan.Missing = append(m.plan.Missing, p)
		}
	}
	if m.plan.Assets == nil || m.plan.Assets.Assets == 0 {
		return
	}
	// assets.write in the scopes the assets will be in, here
	parents := map[string]string{}
	for _, r := range m.a.rows["categories"] {
		p, ok := m.b.cats[str(r, "parent_id")]
		if !ok {
			p = m.opts.Anchor
		}
		parents[m.b.cats[str(r, "id")]] = p
	}
	parent := func(id string) string {
		if p, ok := parents[id]; ok {
			return p
		}
		if c := m.h.cats[id]; c != nil {
			return c.parent
		}
		return ""
	}
	newScopes := map[string][]string{}
	for _, r := range m.a.rows["category_scopes"] {
		if c, ok := m.b.cats[str(r, "category_id")]; ok {
			if n, ok := m.scopeNames[str(r, "scope_id")]; ok {
				newScopes[c] = append(newScopes[c], n)
			}
		}
	}
	own := func(id string) []string {
		if s, ok := newScopes[id]; ok {
			return s
		}
		if c := m.h.cats[id]; c != nil {
			return c.scopes
		}
		return nil
	}
	seen := map[string]bool{}
	var sets []string
	for _, set := range m.a.catSets {
		var ids []string
		for _, c := range set {
			if n, ok := m.b.cats[c]; ok {
				ids = append(ids, n)
			}
		}
		scopes := rbac.EffectiveScopes(ids, parent, own)
		sort.Strings(scopes)
		key := strings.Join(scopes, ", ")
		if seen[key] {
			continue
		}
		seen[key] = true
		sets = append(sets, key)
		if scopes == nil {
			scopes = []string{}
		}
		if !m.may(rbac.PermAssetsWrite, scopes) {
			if key == "" {
				m.plan.Missing = append(m.plan.Missing, rbac.PermAssetsWrite)
			} else {
				m.plan.Missing = append(m.plan.Missing, rbac.PermAssetsWrite+" in "+key)
			}
		}
	}
	sort.Strings(sets)
	for _, s := range sets {
		if s == "" {
			m.plan.Permissions = append(m.plan.Permissions, rbac.PermAssetsWrite)
		} else {
			m.plan.Permissions = append(m.plan.Permissions, rbac.PermAssetsWrite+" in "+s)
		}
	}
	m.plan.Missing = dedupe(m.plan.Missing)
	m.plan.Permissions = dedupe(m.plan.Permissions)
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
