package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Packages (#144, README §2.3): part of an instance, chosen, with what it
// needs, to share. A package is an archive like a backup, of type
// "package", holding only the rows of what was chosen and what that needs,
// and never people, authentication or this instance's own settings.

// Selection is what a package is made of, by kind, each by ID.
type Selection struct {
	// Categories are single categories, and Subtrees categories with every
	// category under them.
	Categories []string `json:"categories,omitempty"`
	Subtrees   []string `json:"subtrees,omitempty"`
	Fields     []string `json:"fields,omitempty"`
	Actions    []string `json:"actions,omitempty"`
	// RelationTypes are link and relation types.
	RelationTypes  []string `json:"relation_types,omitempty"`
	LabelTemplates []string `json:"label_templates,omitempty"`
	// ScanTemplates are reports of kind scan, and Reports the others.
	ScanTemplates []string `json:"scan_templates,omitempty"`
	Reports       []string `json:"reports,omitempty"`
	Flows         []string `json:"flows,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	// Icons is the whole icon library, with the palette and the icon
	// settings.
	Icons  bool             `json:"icons,omitempty"`
	Assets *AssetsSelection `json:"assets,omitempty"`
}

// AssetsSelection chooses assets: every one, or those in the categories
// chosen, with or without their photos, links and history. Archived assets
// are left out.
type AssetsSelection struct {
	All     bool `json:"all,omitempty"`
	Photos  bool `json:"photos,omitempty"`
	Links   bool `json:"links,omitempty"`
	History bool `json:"history,omitempty"`
}

// Empty reports whether nothing is chosen.
func (s Selection) Empty() bool {
	return len(s.Categories)+len(s.Subtrees)+len(s.Fields)+len(s.Actions)+len(s.RelationTypes)+
		len(s.LabelTemplates)+len(s.ScanTemplates)+len(s.Reports)+len(s.Flows)+len(s.Scopes) == 0 &&
		!s.Icons && s.Assets == nil
}

// PackageInfo is what a package says of itself, in its header and
// manifest: what a marketplace would list it by.
type PackageInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	Version     string `json:"version,omitempty"`
	// Preview is the member holding its preview image, if it has one.
	Preview string `json:"preview,omitempty"`
}

// The kinds of item a plan lists.
const (
	ItemCategory      = "category"
	ItemField         = "field"
	ItemAction        = "action"
	ItemRelationType  = "relation_type"
	ItemLabelTemplate = "label_template"
	ItemScanTemplate  = "scan_template"
	ItemReport        = "report"
	ItemFlow          = "flow"
	ItemScope         = "scope"
	ItemIconFile      = "icon_library_file"
	ItemImage         = "image"
	ItemPrinter       = "printer"
	ItemPool          = "printer_pool"
	ItemAsset         = "asset"
	// ItemIcons is the icon library, palette and settings, chosen whole.
	ItemIcons = "icons"
	// ItemAssets is the assets chosen, together.
	ItemAssets = "assets"
)

// Ref names an item.
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Item is one thing a package holds: chosen, or needed by what was, or
// both.
type Item struct {
	Ref
	Chosen bool `json:"chosen"`
	// NeededBy are what need it, which cannot go without it.
	NeededBy []Ref `json:"needed_by"`
}

// LeftOut is something what was chosen names that the package goes
// without, and why.
type LeftOut struct {
	Ref
	NeededBy []Ref  `json:"needed_by"`
	Reason   string `json:"reason"`
}

// AssetCounts count the assets a package holds and what comes with them.
type AssetCounts struct {
	Assets int `json:"assets"`
	Photos int `json:"photos"`
	Links  int `json:"links"`
	Events int `json:"events"`
	// Unreadable are assets chosen that the requester cannot read.
	Unreadable int `json:"unreadable"`
}

// Plan is what a selection comes to: every item, with what needs it, the
// assets, and what is left out.
type Plan struct {
	Items   []Item       `json:"items"`
	Assets  *AssetCounts `json:"assets,omitempty"`
	LeftOut []LeftOut    `json:"left_out"`

	sel   Selection
	rows  map[string]map[string]bool
	whole map[string]bool
	cats  map[string]bool
	// events are the assets whose history the requester may read.
	events map[string]bool
	// extra are rows written besides the database's: what a top category
	// inherits, as its own. scan are top categories' inherited scan
	// templates.
	extra map[string][]map[string]any
	scan  map[string]string
}

// Reader reports whether the requester holds perm in any of scopes, or
// anywhere when scopes is nil. nil reads everything, as the CLI does.
type Reader func(perm string, scopes []string) bool

// ErrSelection is a selection that names what does not exist, or what the
// requester may not read.
var ErrSelection = errors.New("archive: the selection cannot be packaged")

// querier reads, in a transaction or not.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Resolve works out what a selection packages: what was chosen, what that
// needs, transitively, and the assets.
func Resolve(ctx context.Context, st *store.Store, q querier, sel Selection, may Reader) (*Plan, error) {
	if may == nil {
		may = func(string, []string) bool { return true }
	}
	if sel.Empty() {
		return nil, fmt.Errorf("%w: nothing is chosen", ErrSelection)
	}
	r := &resolver{ctx: ctx, st: st, q: q, may: may, items: map[Ref]*Item{}, left: map[Ref]*LeftOut{}}
	if err := r.load(); err != nil {
		return nil, err
	}
	if err := r.choose(sel); err != nil {
		return nil, err
	}
	for len(r.queue) > 0 {
		x := r.queue[0]
		r.queue = r.queue[1:]
		r.needs(x)
	}
	plan := &Plan{sel: sel, Items: []Item{}, LeftOut: []LeftOut{}}
	if sel.Assets != nil {
		if err := r.assets(plan); err != nil {
			return nil, err
		}
		for len(r.queue) > 0 {
			x := r.queue[0]
			r.queue = r.queue[1:]
			r.needs(x)
		}
		// a place a flow names stays behind only if the package lacks it
		for x := range r.left {
			if x.Kind == ItemAsset && r.includedAsset[x.ID] {
				delete(r.left, x)
			}
		}
	}
	for _, x := range r.order {
		it := r.items[x]
		it.Name = r.name(x)
		sort.Slice(it.NeededBy, func(i, j int) bool { return lessRef(it.NeededBy[i], it.NeededBy[j]) })
		plan.Items = append(plan.Items, *it)
	}
	sort.SliceStable(plan.Items, func(i, j int) bool { return lessRef(plan.Items[i].Ref, plan.Items[j].Ref) })
	for _, l := range r.left {
		l.Name = r.name(l.Ref)
		plan.LeftOut = append(plan.LeftOut, *l)
	}
	sort.Slice(plan.LeftOut, func(i, j int) bool { return lessRef(plan.LeftOut[i].Ref, plan.LeftOut[j].Ref) })
	r.rowsOf(plan)
	return plan, nil
}

func lessRef(a, b Ref) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

type category struct {
	name, parent, scanTemplate string
	fields, actions, relTypes  []string
	labelTemplates, scopes     []string
	required                   map[string]bool
	icon                       *iconSource
}

type iconSource struct {
	owner  Ref
	name   string
	fields []string
	names  IconNames
}

type flowRow struct {
	slug, name string
	def        flows.Definition
}

type resolver struct {
	ctx context.Context
	st  *store.Store
	q   querier
	may Reader

	cats         map[string]*category
	children     map[string][]string
	names        map[Ref]string
	fieldKeys    map[string]string // key -> id
	actionKeys   map[string]string
	reportKinds  map[string]string
	templateImgs map[string][]string
	// what templates and reports name: fields by key, icons by name, and
	// categories
	templateFields map[string][]string
	templateIcons  map[string][]string
	reportFields   map[string][]string
	reportCats     map[string][]string
	flows          map[string]*flowRow
	library        map[string]*iconSource
	defined        map[string][]Ref // icon name -> what defines it
	scopeNames     map[string]string
	scopesOf       func([]string) []string
	chosenCats     map[string]bool
	assetIDs       []string
	assetCats      map[string][]string
	assetIcons     map[string]string
	includedAsset  map[string]bool
	readEvents     map[string]bool

	items map[Ref]*Item
	order []Ref
	queue []Ref
	left  map[Ref]*LeftOut
}

// strings reads rows of text columns, NULL as "".
func (r *resolver) strings(query string, args ...any) ([][]string, error) {
	rows, err := r.q.QueryContext(r.ctx, r.st.Rebind(query), args...)
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

// load reads the catalog: everything a package can hold but assets.
func (r *resolver) load() error {
	r.cats, r.children, r.names = map[string]*category{}, map[string][]string{}, map[Ref]string{}
	r.fieldKeys, r.actionKeys, r.reportKinds = map[string]string{}, map[string]string{}, map[string]string{}
	r.templateImgs, r.flows, r.library = map[string][]string{}, map[string]*flowRow{}, map[string]*iconSource{}
	r.defined, r.scopeNames = map[string][]Ref{}, map[string]string{}
	r.templateFields, r.templateIcons = map[string][]string{}, map[string][]string{}
	r.reportFields, r.reportCats = map[string][]string{}, map[string][]string{}
	read := func(query string, each func([]string)) error {
		rows, err := r.strings(query)
		if err != nil {
			return fmt.Errorf("reading what can be packaged: %w", err)
		}
		for _, row := range rows {
			each(row)
		}
		return nil
	}
	steps := []struct {
		query string
		each  func([]string)
	}{
		{"SELECT id, name, parent_id, scan_template_id FROM categories", func(c []string) {
			r.cats[c[0]] = &category{name: c[1], parent: c[2], scanTemplate: c[3]}
			r.names[Ref{Kind: ItemCategory, ID: c[0]}] = c[1]
		}},
		{"SELECT id, field_key, label FROM fields", func(c []string) {
			r.fieldKeys[c[1]] = c[0]
			r.names[Ref{Kind: ItemField, ID: c[0]}] = c[2]
		}},
		{"SELECT id, action_key, label FROM asset_actions", func(c []string) {
			r.actionKeys[c[1]] = c[0]
			r.names[Ref{Kind: ItemAction, ID: c[0]}] = c[2]
		}},
		{"SELECT id, name FROM relation_types", func(c []string) { r.names[Ref{Kind: ItemRelationType, ID: c[0]}] = c[1] }},
		{"SELECT id, name, layout_json FROM templates", func(c []string) {
			r.names[Ref{Kind: ItemLabelTemplate, ID: c[0]}] = c[1]
			// the images it places, the fields it binds and the icons it
			// draws by name, which come with it
			var layout labels.Template
			if json.Unmarshal([]byte(c[2]), &layout) == nil {
				r.templateImgs[c[0]] = layout.ImageIDs()
				r.templateFields[c[0]] = layout.FieldKeys()
				r.templateIcons[c[0]] = layout.IconNames()
			}
		}},
		{"SELECT id, filename FROM images", func(c []string) { r.names[Ref{Kind: ItemImage, ID: c[0]}] = c[1] }},
		{"SELECT id, name, kind, spec FROM reports", func(c []string) {
			r.reportKinds[c[0]] = c[2]
			r.reportFields[c[0]], r.reportCats[c[0]] = ReportNames(c[3])
			kind := ItemReport
			if c[2] == "scan" {
				kind = ItemScanTemplate
			}
			r.names[Ref{Kind: kind, ID: c[0]}] = c[1]
		}},
		{"SELECT id, slug, name, definition FROM flows", func(c []string) {
			f := &flowRow{slug: c[1], name: c[2]}
			_ = json.Unmarshal([]byte(c[3]), &f.def)
			r.flows[c[0]] = f
			r.names[Ref{Kind: ItemFlow, ID: c[0]}] = c[2]
		}},
		{"SELECT id, name FROM scopes", func(c []string) {
			r.scopeNames[c[0]] = c[1]
			r.names[Ref{Kind: ItemScope, ID: c[0]}] = c[1]
		}},
		{"SELECT id, name FROM printers", func(c []string) { r.names[Ref{Kind: ItemPrinter, ID: c[0]}] = c[1] }},
		{"SELECT id, name FROM printer_pools", func(c []string) { r.names[Ref{Kind: ItemPool, ID: c[0]}] = c[1] }},
		{"SELECT id, path, source FROM icon_library", func(c []string) {
			ref := Ref{Kind: ItemIconFile, ID: c[0]}
			r.names[ref] = c[1]
			r.library[c[0]] = &iconSource{owner: ref, names: ScanIcons(c[2])}
		}},
	}
	for _, s := range steps {
		if err := read(s.query, s.each); err != nil {
			return err
		}
	}
	for id, c := range r.cats {
		if c.parent != "" {
			r.children[c.parent] = append(r.children[c.parent], id)
		}
	}
	joins := []struct {
		query string
		each  func(*category, string)
	}{
		{"SELECT category_id, action_id FROM category_actions", func(c *category, v string) { c.actions = append(c.actions, v) }},
		{"SELECT category_id, relation_type_id FROM category_relation_types", func(c *category, v string) { c.relTypes = append(c.relTypes, v) }},
		{"SELECT category_id, template_id FROM category_label_templates ORDER BY sort_order", func(c *category, v string) { c.labelTemplates = append(c.labelTemplates, v) }},
		{"SELECT category_id, scope_id FROM category_scopes", func(c *category, v string) { c.scopes = append(c.scopes, v) }},
		{"SELECT category_id, field_id FROM icon_param_bindings", func(c *category, v string) {
			if c.icon != nil {
				c.icon.fields = append(c.icon.fields, v)
			}
		}},
		{"SELECT category_id, field_id FROM icon_text_bindings WHERE field_id IS NOT NULL", func(c *category, v string) {
			if c.icon != nil {
				c.icon.fields = append(c.icon.fields, v)
			}
		}},
	}
	if err := read("SELECT category_id, icon_name, source FROM category_icons", func(c []string) {
		if cat := r.cats[c[0]]; cat != nil {
			cat.icon = &iconSource{owner: Ref{Kind: ItemCategory, ID: c[0]}, name: c[1], names: ScanIcons(c[2])}
		}
	}); err != nil {
		return err
	}
	if err := read("SELECT category_id, field_id, is_required FROM category_fields", func(c []string) {
		if cat := r.cats[c[0]]; cat != nil {
			cat.fields = append(cat.fields, c[1])
			if cat.required == nil {
				cat.required = map[string]bool{}
			}
			cat.required[c[1]] = c[2] == "1" || c[2] == "true" || c[2] == "t"
		}
	}); err != nil {
		return err
	}
	for _, j := range joins {
		each := j.each
		if err := read(j.query, func(c []string) {
			if cat := r.cats[c[0]]; cat != nil {
				each(cat, c[1])
			}
		}); err != nil {
			return err
		}
	}
	for _, f := range r.library {
		for _, n := range f.names.Defines {
			r.defined[n] = append(r.defined[n], f.owner)
		}
	}
	for _, c := range r.cats {
		if c.icon != nil {
			for _, n := range c.icon.names.Defines {
				r.defined[n] = append(r.defined[n], c.icon.owner)
			}
		}
	}
	// as store.ScopeResolver has them, from what is read here, so a
	// package is read in one transaction
	r.scopesOf = func(ids []string) []string {
		return rbac.EffectiveScopes(ids, func(id string) string {
			if c := r.cats[id]; c != nil {
				return c.parent
			}
			return ""
		}, func(id string) []string {
			var out []string
			if c := r.cats[id]; c != nil {
				for _, s := range c.scopes {
					out = append(out, r.scopeNames[s])
				}
			}
			return out
		})
	}
	return nil
}

func (r *resolver) name(x Ref) string {
	switch x.Kind {
	case ItemIcons:
		return "The icon library, palette and settings"
	case ItemAssets:
		return "Assets"
	}
	if n, ok := r.names[x]; ok {
		return n
	}
	return x.ID
}

// exists reports whether an item is there to package.
func (r *resolver) exists(x Ref) bool {
	switch x.Kind {
	case ItemScanTemplate:
		return r.reportKinds[x.ID] == "scan"
	case ItemReport:
		k, ok := r.reportKinds[x.ID]
		return ok && k != "scan"
	}
	_, ok := r.names[x]
	return ok
}

// readable reports whether the requester may read an item of a kind.
func (r *resolver) readable(kind string) (bool, string) {
	switch kind {
	case ItemScope:
		if r.may(rbac.PermUsersManage, nil) || r.may(rbac.PermCategoriesManage, nil) {
			return true, ""
		}
		return false, "only those who manage users or categories see scopes"
	case ItemFlow:
		if r.may(rbac.PermFlowsManage, nil) || r.may(rbac.PermFlowsRun, nil) {
			return true, ""
		}
		return false, "only those who manage or run flows see them"
	case ItemPrinter, ItemPool:
		return false, "printers and pools are this instance's own: imported, a flow that printed adds its labels to the print queue instead"
	}
	if r.may(rbac.PermAssetsRead, nil) {
		return true, ""
	}
	return false, "you cannot read assets"
}

// choose adds what the selection names, refusing what does not exist or
// may not be read.
func (r *resolver) choose(sel Selection) error {
	var chosen []Ref
	add := func(kind string, ids []string) {
		for _, id := range ids {
			chosen = append(chosen, Ref{Kind: kind, ID: id})
		}
	}
	r.chosenCats = map[string]bool{}
	for _, id := range sel.Subtrees {
		if r.cats[id] == nil {
			return fmt.Errorf("%w: there is no category %s", ErrSelection, id)
		}
		for _, d := range r.subtree(id) {
			chosen = append(chosen, Ref{Kind: ItemCategory, ID: d})
		}
	}
	add(ItemCategory, sel.Categories)
	add(ItemField, sel.Fields)
	add(ItemAction, sel.Actions)
	add(ItemRelationType, sel.RelationTypes)
	add(ItemLabelTemplate, sel.LabelTemplates)
	add(ItemScanTemplate, sel.ScanTemplates)
	add(ItemReport, sel.Reports)
	add(ItemFlow, sel.Flows)
	add(ItemScope, sel.Scopes)
	for _, x := range chosen {
		if !r.exists(x) {
			return fmt.Errorf("%w: there is no %s %s", ErrSelection, strings.ReplaceAll(x.Kind, "_", " "), x.ID)
		}
		if ok, why := r.readable(x.Kind); !ok {
			return fmt.Errorf("%w: %s %q: %s", ErrSelection, strings.ReplaceAll(x.Kind, "_", " "), r.name(x), why)
		}
		if x.Kind == ItemCategory {
			r.chosenCats[x.ID] = true
		}
		r.include(x, nil).Chosen = true
	}
	if sel.Icons {
		if ok, why := r.readable(ItemIcons); !ok {
			return fmt.Errorf("%w: icons: %s", ErrSelection, why)
		}
		r.include(Ref{Kind: ItemIcons, ID: "icons"}, nil).Chosen = true
		for id := range r.library {
			r.include(Ref{Kind: ItemIconFile, ID: id}, nil).Chosen = true
		}
	}
	return nil
}

// subtree is a category and every category under it.
func (r *resolver) subtree(id string) []string {
	out := []string{id}
	for i := 0; i < len(out) && i < 100000; i++ {
		out = append(out, r.children[out[i]]...)
	}
	return out
}

// include adds an item, needed by by, unless nil, and queues what it needs
// the first time.
func (r *resolver) include(x Ref, by *Ref) *Item {
	it, ok := r.items[x]
	if !ok {
		it = &Item{Ref: x, NeededBy: []Ref{}}
		r.items[x] = it
		r.order = append(r.order, x)
		r.queue = append(r.queue, x)
	}
	if by != nil && *by != x {
		ref := Ref{Kind: by.Kind, ID: by.ID, Name: r.name(*by)}
		for _, n := range it.NeededBy {
			if n.Kind == ref.Kind && n.ID == ref.ID {
				return it
			}
		}
		it.NeededBy = append(it.NeededBy, ref)
	}
	return it
}

// need adds what x needs, or notes it left out when it is not there or may
// not be read.
func (r *resolver) need(x Ref, by Ref) {
	if x.ID == "" {
		return
	}
	ok, why := r.readable(x.Kind)
	if ok && !r.exists(x) {
		ok, why = false, "it is no longer here"
	}
	if !ok {
		l := r.left[x]
		if l == nil {
			l = &LeftOut{Ref: x, NeededBy: []Ref{}, Reason: why}
			r.left[x] = l
		}
		l.NeededBy = append(l.NeededBy, Ref{Kind: by.Kind, ID: by.ID, Name: r.name(by)})
		return
	}
	r.include(x, &by)
}

// needs adds what an item needs.
func (r *resolver) needs(x Ref) {
	switch x.Kind {
	case ItemCategory:
		c := r.cats[x.ID]
		for _, id := range c.fields {
			r.need(Ref{Kind: ItemField, ID: id}, x)
		}
		// what it inherits, which it keeps when its parent stays behind
		fields, labels, scan := r.inherited(x.ID)
		for _, f := range fields {
			r.need(Ref{Kind: ItemField, ID: f.id}, x)
		}
		for _, id := range labels {
			r.need(Ref{Kind: ItemLabelTemplate, ID: id}, x)
		}
		r.need(Ref{Kind: ItemScanTemplate, ID: scan}, x)
		for _, id := range c.actions {
			r.need(Ref{Kind: ItemAction, ID: id}, x)
		}
		for _, id := range c.relTypes {
			r.need(Ref{Kind: ItemRelationType, ID: id}, x)
		}
		for _, id := range c.labelTemplates {
			r.need(Ref{Kind: ItemLabelTemplate, ID: id}, x)
		}
		r.need(Ref{Kind: ItemScanTemplate, ID: c.scanTemplate}, x)
		if c.icon != nil {
			for _, id := range c.icon.fields {
				r.need(Ref{Kind: ItemField, ID: id}, x)
			}
			r.needIcons(c.icon.names.Uses, x)
		}
		// the flows that file into it or take from it
		ids := make([]string, 0, len(r.flows))
		for id := range r.flows {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			for _, c := range r.flows[id].def.CategoryIDs {
				if c == x.ID {
					r.need(Ref{Kind: ItemFlow, ID: id}, x)
				}
			}
		}
	case ItemLabelTemplate:
		for _, id := range r.templateImgs[x.ID] {
			r.need(Ref{Kind: ItemImage, ID: id}, x)
		}
		r.needFields(r.templateFields[x.ID], x)
		r.needIcons(r.templateIcons[x.ID], x)
	case ItemReport, ItemScanTemplate:
		r.needFields(r.reportFields[x.ID], x)
		for _, id := range r.reportCats[x.ID] {
			r.need(Ref{Kind: ItemCategory, ID: id}, x)
		}
	case ItemFlow:
		r.flowNeeds(x)
	case ItemIconFile:
		r.needIcons(r.library[x.ID].names.Uses, x)
	}
}

type inheritedField struct {
	id       string
	required bool
}

// inherited is what a category takes from the categories above it: their
// fields, closest first, that it does not record itself, and their label
// templates and scan template, when it names none of its own.
func (r *resolver) inherited(id string) ([]inheritedField, []string, string) {
	c := r.cats[id]
	own := map[string]bool{}
	for _, f := range c.fields {
		own[f] = true
	}
	var fields []inheritedField
	var labels []string
	scan := ""
	needLabels, needScan := len(c.labelTemplates) == 0, c.scanTemplate == ""
	for p, depth := c.parent, 0; p != "" && depth < 100; depth++ {
		pc := r.cats[p]
		if pc == nil {
			break
		}
		for _, f := range pc.fields {
			if !own[f] {
				own[f] = true
				fields = append(fields, inheritedField{id: f, required: pc.required[f]})
			}
		}
		if needLabels && labels == nil && len(pc.labelTemplates) > 0 {
			labels = pc.labelTemplates
		}
		if needScan && scan == "" {
			scan = pc.scanTemplate
		}
		p = pc.parent
	}
	return fields, labels, scan
}

// needFields adds the fields with these keys.
func (r *resolver) needFields(keys []string, by Ref) {
	for _, key := range keys {
		if id, ok := r.fieldKeys[key]; ok {
			r.need(Ref{Kind: ItemField, ID: id}, by)
		}
	}
}

// ReportNames are the field keys a report's spec binds or groups by, and
// the categories its filter names.
func ReportNames(spec string) (fieldKeys, categoryIDs []string) {
	var s reports.Spec
	if json.Unmarshal([]byte(spec), &s) != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	add := func(key string) {
		if key != "" && !seen[key] {
			seen[key] = true
			fieldKeys = append(fieldKeys, key)
		}
	}
	for _, sec := range s.Sections {
		for _, col := range sec.Columns {
			if b, err := labels.ParseBinding(col.Binding); err == nil {
				for _, ref := range b.Refs() {
					if ref.Kind == labels.RefField {
						add(ref.Arg)
					}
				}
			}
		}
		if key, ok := strings.CutPrefix(sec.GroupBy, "field:"); ok {
			add(key)
		}
	}
	return fieldKeys, s.Filter.CategoryIDs
}

// needIcons adds what defines the icon names used.
func (r *resolver) needIcons(names []string, by Ref) {
	for _, n := range names {
		for _, owner := range r.defined[n] {
			r.need(owner, by)
		}
	}
}

// flowNeeds adds what a flow names: its categories, the label templates
// and actions it uses, the fields it asks or sets, and the flow whose
// filings it finds. Printers and pools, and the places it puts things,
// stay behind.
func (r *resolver) flowNeeds(x Ref) {
	f := r.flows[x.ID]
	d := &f.def
	cat := func(id string) { r.need(Ref{Kind: ItemCategory, ID: id}, x) }
	field := func(key string) {
		if id, ok := r.fieldKeys[key]; ok {
			r.need(Ref{Kind: ItemField, ID: id}, x)
		}
	}
	for _, id := range d.CategoryIDs {
		cat(id)
	}
	field(d.LeaveUntil)
	for _, s := range d.Steps {
		for _, id := range s.Categories {
			cat(id)
		}
		cat(s.Fixed)
		if s.When != nil && s.When.Value == flows.ValueCategoryID {
			cat(s.When.To)
		}
		for _, fs := range s.Fields {
			field(fs.Key)
		}
		if s.Match != nil {
			field(s.Match.Field)
		}
		r.need(Ref{Kind: ItemFlow, ID: s.FiledBy}, x)
		if s.Location != nil && s.Location.AssetID != "" {
			r.stays(Ref{Kind: ItemAsset, ID: s.Location.AssetID}, x, "a place is one of this instance's assets: imported, the flow asks for the place to be chosen again")
		}
		if s.Label != nil {
			r.need(Ref{Kind: ItemLabelTemplate, ID: s.Label.TemplateID}, x)
			if kind, id, ok := strings.Cut(s.Label.Destination, ":"); ok {
				ref := Ref{Kind: ItemPrinter, ID: id}
				if kind == "pool" {
					ref.Kind = ItemPool
				}
				r.need(ref, x)
			}
		}
		for _, c := range s.Changes {
			cat(c.CategoryID)
			if id, ok := r.actionKeys[c.Action]; ok {
				r.need(Ref{Kind: ItemAction, ID: id}, x)
			}
			for key := range c.Fields {
				field(key)
			}
			if c.AssetID != "" {
				r.stays(Ref{Kind: ItemAsset, ID: c.AssetID}, x, "a place is one of this instance's assets: imported, the flow asks for the place to be chosen again")
			}
		}
	}
}

// stays notes an asset a flow names as staying behind, unless the package
// holds it.
func (r *resolver) stays(x Ref, by Ref, why string) {
	l := r.left[x]
	if l == nil {
		l = &LeftOut{Ref: x, NeededBy: []Ref{}, Reason: why}
		r.left[x] = l
	}
	l.NeededBy = append(l.NeededBy, Ref{Kind: by.Kind, ID: by.ID, Name: r.name(by)})
}

// assets finds the assets chosen that the requester may read, and adds
// what they need: their categories, and the icons they draw as their own.
func (r *resolver) assets(plan *Plan) error {
	sel := plan.sel.Assets
	rows, err := r.strings("SELECT a.id, a.name, a.icon_name, ac.category_id FROM assets a LEFT JOIN asset_categories ac ON ac.asset_id = a.id WHERE a.archived_at IS NULL ORDER BY a.id")
	if err != nil {
		return fmt.Errorf("reading assets: %w", err)
	}
	r.assetCats, r.assetIcons = map[string][]string{}, map[string]string{}
	var order []string
	for _, row := range rows {
		id := row[0]
		if _, ok := r.assetCats[id]; !ok {
			order = append(order, id)
			r.assetCats[id] = []string{}
			r.names[Ref{Kind: ItemAsset, ID: id}] = row[1]
			r.assetIcons[id] = row[2]
		}
		if row[3] != "" {
			r.assetCats[id] = append(r.assetCats[id], row[3])
		}
	}
	counts := &AssetCounts{}
	plan.Assets = counts
	r.includedAsset, r.readEvents = map[string]bool{}, map[string]bool{}
	whole := Ref{Kind: ItemAssets, ID: "assets"}
	r.include(whole, nil).Chosen = true
	for _, id := range order {
		cats := r.assetCats[id]
		if !sel.All {
			in := false
			for _, c := range cats {
				in = in || r.chosenCats[c]
			}
			if !in {
				continue
			}
		}
		scopes := r.scopesOf(cats)
		if !r.may(rbac.PermAssetsRead, scopes) {
			counts.Unreadable++
			continue
		}
		r.includedAsset[id] = true
		r.assetIDs = append(r.assetIDs, id)
		if sel.History && r.may(rbac.PermEventsRead, scopes) {
			r.readEvents[id] = true
		}
		for _, c := range cats {
			r.need(Ref{Kind: ItemCategory, ID: c}, whole)
		}
		if icon := r.assetIcons[id]; icon != "" {
			r.needIcons([]string{icon}, whole)
		}
	}
	counts.Assets = len(r.assetIDs)
	if sel.Links {
		links, err := r.strings("SELECT id, relation_type_id, from_asset_id, to_asset_id FROM asset_links")
		if err != nil {
			return fmt.Errorf("reading links: %w", err)
		}
		for _, l := range links {
			if r.includedAsset[l[2]] && r.includedAsset[l[3]] {
				r.need(Ref{Kind: ItemRelationType, ID: l[1]}, whole)
				counts.Links++
			}
		}
	}
	for _, c := range []struct {
		on    bool
		query string
		in    map[string]bool
		n     *int
	}{
		{sel.Photos, "SELECT asset_id FROM asset_photos", r.includedAsset, &counts.Photos},
		{sel.History, "SELECT asset_id FROM events WHERE asset_id IS NOT NULL", r.readEvents, &counts.Events},
	} {
		if !c.on {
			continue
		}
		rows, err := r.strings(c.query)
		if err != nil {
			return fmt.Errorf("counting what comes with assets: %w", err)
		}
		for _, row := range rows {
			if c.in[row[0]] {
				*c.n++
			}
		}
	}
	return nil
}

// rowsOf works out the rows of each table the package holds, by key, from
// the items and the assets.
func (r *resolver) rowsOf(plan *Plan) {
	ids := map[string]map[string]bool{}
	for _, it := range plan.Items {
		if ids[it.Kind] == nil {
			ids[it.Kind] = map[string]bool{}
		}
		ids[it.Kind][it.ID] = true
	}
	has := func(kind, id string) bool { return ids[kind][id] }
	plan.cats = ids[ItemCategory]
	plan.rows = map[string]map[string]bool{}
	plan.whole = map[string]bool{}
	plan.extra = map[string][]map[string]any{}
	plan.scan = map[string]string{}
	set := func(table string, keys ...string) {
		if plan.rows[table] == nil {
			plan.rows[table] = map[string]bool{}
		}
		plan.rows[table][rowKey(keys)] = true
	}
	for kind, table := range map[string]string{
		ItemCategory: "categories", ItemField: "fields", ItemAction: "asset_actions", ItemRelationType: "relation_types",
		ItemLabelTemplate: "templates", ItemImage: "images", ItemScanTemplate: "reports", ItemReport: "reports",
		ItemFlow: "flows", ItemScope: "scopes", ItemIconFile: "icon_library",
	} {
		for id := range ids[kind] {
			set(table, id)
		}
	}
	for id := range ids[ItemCategory] {
		c := r.cats[id]
		for _, f := range c.fields {
			if has(ItemField, f) {
				set("category_fields", id, f)
			}
		}
		for _, a := range c.actions {
			if has(ItemAction, a) {
				set("category_actions", id, a)
			}
		}
		for _, t := range c.relTypes {
			if has(ItemRelationType, t) {
				set("category_relation_types", id, t)
			}
		}
		for _, t := range c.labelTemplates {
			if has(ItemLabelTemplate, t) {
				set("category_label_templates", id, t)
			}
		}
		for _, s := range c.scopes {
			if has(ItemScope, s) {
				set("category_scopes", id, s)
			}
		}
		if !ids[ItemCategory][c.parent] {
			// a top category of the package keeps what it inherits
			fields, labels, scan := r.inherited(id)
			for _, f := range fields {
				if has(ItemField, f.id) {
					plan.extra["category_fields"] = append(plan.extra["category_fields"], map[string]any{"category_id": id, "field_id": f.id, "is_required": f.required})
				}
			}
			for i, t := range labels {
				if has(ItemLabelTemplate, t) {
					plan.extra["category_label_templates"] = append(plan.extra["category_label_templates"], map[string]any{"category_id": id, "template_id": t, "sort_order": int64(i)})
				}
			}
			if scan != "" && has(ItemScanTemplate, scan) {
				plan.scan[id] = scan
			}
		}
		if c.icon != nil {
			set("category_icons", id)
			// its bindings, by its category
			plan.whole["icon_param_bindings:"+id] = true
			plan.whole["icon_text_bindings:"+id] = true
		}
	}
	if plan.sel.Icons {
		plan.whole["icon_palette"] = true
		plan.whole["icon_settings"] = true
	}
	plan.events = r.readEvents
	for _, id := range r.assetIDs {
		set("assets", id)
	}
}

// rowKey is a row's key as one string.
func rowKey(vals []string) string { return strings.Join(vals, "\x00") }

// packaged reports whether a package holds a table's row, and rewrites what
// it must not hold: who made or did things, drafts, and what names rows it
// does not hold. key are the row's key values and get reads a column.
func (p *Plan) packaged(t *Table, vals []any) (bool, error) {
	get := func(col string) string {
		s, _ := text(vals[colIndex(t, col)])
		return s
	}
	setNull := func(col string) { vals[colIndex(t, col)] = nil }
	keyOf := func(cols ...string) string {
		k := make([]string, len(cols))
		for i, c := range cols {
			k[i] = get(c)
		}
		return rowKey(k)
	}
	asset := func(col string) bool { return p.rows["assets"][get(col)] }
	switch t.Name {
	case "categories":
		if !p.rows[t.Name][get("id")] {
			return false, nil
		}
		if !p.cats[get("parent_id")] {
			setNull("parent_id")
		}
		if s := p.scan[get("id")]; s != "" {
			vals[colIndex(t, "scan_template_id")] = s
		}
		if !p.rows["reports"][get("scan_template_id")] {
			setNull("scan_template_id")
		}
		return true, nil
	case "fields", "asset_actions", "relation_types", "templates", "images", "reports", "scopes", "assets":
		return p.rows[t.Name][get("id")], nil
	case "flows":
		if !p.rows[t.Name][get("id")] {
			return false, nil
		}
		setNull("created_by")
		return true, nil
	case "icon_library":
		if !p.rows[t.Name][get("id")] {
			return false, nil
		}
		setNull("draft")
		setNull("draft_errors")
		return true, nil
	case "category_icons":
		if !p.rows[t.Name][get("category_id")] {
			return false, nil
		}
		setNull("draft")
		setNull("draft_errors")
		return true, nil
	case "category_fields":
		return p.rows[t.Name][keyOf("category_id", "field_id")], nil
	case "category_actions":
		return p.rows[t.Name][keyOf("category_id", "action_id")], nil
	case "category_relation_types":
		return p.rows[t.Name][keyOf("category_id", "relation_type_id")], nil
	case "category_label_templates":
		return p.rows[t.Name][keyOf("category_id", "template_id")], nil
	case "category_scopes":
		return p.rows[t.Name][keyOf("category_id", "scope_id")], nil
	case "icon_param_bindings", "icon_text_bindings":
		if !p.whole[t.Name+":"+get("category_id")] {
			return false, nil
		}
		if f := get("field_id"); f != "" && !p.rows["fields"][f] {
			return false, nil
		}
		return true, nil
	case "icon_palette", "icon_settings":
		return p.whole[t.Name], nil
	case "asset_categories":
		return asset("asset_id") && p.cats[get("category_id")], nil
	case "asset_field_values":
		return asset("asset_id") && p.rows["fields"][get("field_id")], nil
	case "asset_generator_keys":
		return asset("asset_id"), nil
	case "asset_photos":
		return p.sel.Assets != nil && p.sel.Assets.Photos && asset("asset_id"), nil
	case "asset_links":
		if p.sel.Assets == nil || !p.sel.Assets.Links || !asset("from_asset_id") || !asset("to_asset_id") || !p.rows["relation_types"][get("relation_type_id")] {
			return false, nil
		}
		if p.rows["asset_links"] == nil {
			p.rows["asset_links"] = map[string]bool{}
		}
		p.rows["asset_links"][get("id")] = true
		return true, nil
	case "asset_placements":
		return p.rows["asset_links"][get("link_id")], nil
	case "events":
		if !p.events[get("asset_id")] {
			return false, nil
		}
		setNull("actor_user_id")
		if !p.cats[get("category_id")] {
			setNull("category_id")
		}
		payload, err := withoutPeople(vals[colIndex(t, "payload")])
		if err != nil {
			return false, err
		}
		vals[colIndex(t, "payload")] = payload
		return true, nil
	}
	// everything else: people, authentication, this instance's settings,
	// loans and runs, revisions with who made them
	return false, nil
}

// withoutPeople is an event's payload without who did it or borrowed.
func withoutPeople(v any) (any, error) {
	s, ok := text(v)
	if !ok || s == "" {
		return v, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("reading an event's payload: %w", err)
	}
	delete(m, "actor")
	delete(m, "borrower")
	b, err := json.Marshal(m)
	return string(b), err
}

// MaxPreview is the most bytes a package's preview image may have.
const MaxPreview = 1 << 20

// PreviewPath is the member a preview image is kept as, by its type: PNG,
// JPEG or WebP.
func PreviewPath(image []byte) (string, error) {
	if len(image) > MaxPreview {
		return "", fmt.Errorf("%w: a preview image is at most %d bytes", ErrSelection, MaxPreview)
	}
	switch http.DetectContentType(image) {
	case "image/png":
		return "preview.png", nil
	case "image/jpeg":
		return "preview.jpg", nil
	case "image/webp":
		return "preview.webp", nil
	}
	return "", fmt.Errorf("%w: a preview image is a PNG, JPEG or WebP", ErrSelection)
}

// packageKinds are the kinds of information a package holds: never
// people, authentication or this instance's own settings, whose tables
// packaged leaves out.
func packageKinds(p *Plan) []Kind {
	has := map[Kind]bool{}
	for _, t := range Tables {
		if len(p.rows[t.Name]) > 0 {
			has[t.Kind] = true
		}
	}
	if p.whole["icon_palette"] {
		has[KindIcons] = true
	}
	var out []Kind
	for _, k := range Kinds {
		if has[k] {
			out = append(out, k)
		}
	}
	return out
}
