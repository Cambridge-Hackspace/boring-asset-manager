// Package reports runs saved reports (docs/reports.md): a filter over assets and
// sections that show what it finds, as tables whose columns are content
// bindings like those of label templates.
package reports

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

// ErrInvalidSpec is wrapped by every problem with a report's spec.
var ErrInvalidSpec = errors.New("invalid report")

// Limits on a spec, so a report stays something a person reads.
const (
	MaxSections = 20
	MaxColumns  = 30
	// MaxBinding bounds a column's binding, in characters.
	MaxBinding = 1000
)

// Spec is a report: what it looks at and how it shows it.
type Spec struct {
	Filter   Filter    `json:"filter"`
	Sections []Section `json:"sections"`
	// Pools are what a table grouped by pool needs, set by whoever runs
	// the report; without them its assets are in no pool.
	Pools *PoolData `json:"-"`
}

// PoolData are the pools assets count in (docs/stock-and-lending.md#pools):
// Of maps each asset to the pools whose totals it adds to, and Totals each
// pool to its total, over the assets the reader may see, whatever the
// report's filter finds (docs/reports.md#by-pool-or-by-bin).
//
// The shopping list (#214) should offer the same choice, by pool or by bin,
// and group by pool from the same data.
type PoolData struct {
	Of     map[string][]string
	Totals map[string]int
}

// NotInAPool titles the group, in a table by pool, of the assets in none.
const NotInAPool = "Not in a pool"

// Filter chooses the assets a report looks at. Empty fields match every
// asset.
type Filter struct {
	// CategoryIDs keeps assets in any of these categories or below them.
	CategoryIDs []string `json:"category_ids,omitempty"`
	Search      string   `json:"search,omitempty"`
	// Archived looks at archived assets instead of active ones.
	Archived bool `json:"archived,omitempty"`
	// RunningLow keeps assets flagged running low themselves, or in a
	// category flagged running low, or below one.
	RunningLow bool `json:"running_low,omitempty"`
	// Loan is "open" or "overdue"; Condition a condition or "problem".
	Loan      string `json:"loan,omitempty"`
	Condition string `json:"condition,omitempty"`
	// Vendor keeps assets bought from this vendor, by ID.
	Vendor string `json:"vendor,omitempty"`
	// PurchasedBetween keeps assets ordered within it, those with no order
	// date by when they were received.
	PurchasedBetween *DateRange `json:"purchased_between,omitempty"`
	// WarrantyEndsBefore keeps assets whose warranty ends before this date,
	// YYYY-MM-DD.
	WarrantyEndsBefore string `json:"warranty_ends_before,omitempty"`
	// Contract keeps assets a contract covers, by ID (docs/contracts.md); a
	// report filtering by one binds {{asset.contract.*}} to it.
	Contract string `json:"contract,omitempty"`
}

// DateRange is from one date to another, both included, each YYYY-MM-DD,
// and either empty for no limit.
type DateRange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// validDate reports whether s is a date written YYYY-MM-DD.
func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Section is one part of a report: Type is "table", or "chart" (see
// charts.go).
type Section struct {
	Type string `json:"type"`
	// Chart is bar, donut or line; Measure what it adds up; Period, for a
	// line, week or month.
	Chart   string   `json:"chart,omitempty"`
	Measure string   `json:"measure,omitempty"`
	Period  string   `json:"period,omitempty"`
	Title   string   `json:"title,omitempty"`
	Columns []Column `json:"columns,omitempty"`
	// SortBy is the column rows sort by, from 0; Descending reverses it.
	SortBy     int  `json:"sort_by,omitempty"`
	Descending bool `json:"descending,omitempty"`
	// GroupBy is, for a table, "" (by bin: a row for each asset),
	// "category": rows are grouped under each category their asset is in,
	// or "pool": a group for each pool, with its total and threshold, and
	// its places under it. A bar or donut chart's bars group by it.
	GroupBy string `json:"group_by,omitempty"`
	// Source, in a scan template's table, is what its rows are: "contents",
	// what the scanned asset holds. Actions are an actions section's.
	Source  string   `json:"source,omitempty"`
	Actions []string `json:"actions,omitempty"`
}

// Column is a table column: a heading and a content binding.
type Column struct {
	Heading string `json:"heading"`
	Binding string `json:"binding"`
}

// Parse decodes and checks a spec. Bindings are checked as label templates
// check theirs; fieldKnown says whether a field key exists.
func Parse(raw []byte, fieldKnown func(key string) bool) (*Spec, error) {
	var spec Spec
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	if err := spec.check(fieldKnown); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *Spec) check(fieldKnown func(string) bool) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, fmt.Sprintf(format, args...))
	}
	switch s.Filter.Loan {
	case "", "open", "overdue":
	default:
		return fail("filter.loan must be open or overdue")
	}
	switch s.Filter.Condition {
	case "", "problem", models.ConditionOK, models.ConditionNeedsMaintenance, models.ConditionOutOfService:
	default:
		return fail("filter.condition must be ok, needs_maintenance, out_of_service or problem")
	}
	if err := textcheck.Check("filter.search", s.Filter.Search, textcheck.MaxName); err != nil {
		return fail("%v", err)
	}
	if err := textcheck.Check("filter.vendor", s.Filter.Vendor, textcheck.MaxName); err != nil {
		return fail("%v", err)
	}
	if err := textcheck.Check("filter.contract", s.Filter.Contract, textcheck.MaxName); err != nil {
		return fail("%v", err)
	}
	if b := s.Filter.PurchasedBetween; b != nil {
		if b.From != "" && !validDate(b.From) || b.To != "" && !validDate(b.To) {
			return fail("filter.purchased_between takes dates written YYYY-MM-DD")
		}
		if b.From != "" && b.To != "" && b.From > b.To {
			return fail("filter.purchased_between must run from a date to one no earlier")
		}
	}
	if e := s.Filter.WarrantyEndsBefore; e != "" && !validDate(e) {
		return fail("filter.warranty_ends_before is a date written YYYY-MM-DD")
	}
	if len(s.Sections) == 0 {
		return fail("a report needs at least one section")
	}
	if len(s.Sections) > MaxSections {
		return fail("at most %d sections", MaxSections)
	}
	for i, sec := range s.Sections {
		where := fmt.Sprintf("section %d", i+1)
		if err := textcheck.Check(where+" title", sec.Title, textcheck.MaxName); err != nil {
			return fail("%v", err)
		}
		if sec.Type == "chart" {
			if err := checkChart(sec, where, fieldKnown); err != nil {
				return err
			}
			continue
		}
		if sec.Type != "table" {
			return fail("%s: type must be table or chart", where)
		}
		if sec.Chart != "" || sec.Measure != "" || sec.Period != "" {
			return fail("%s: a table has no chart, measure or period", where)
		}
		if sec.Source != "" || len(sec.Actions) > 0 {
			return fail("%s: only scan templates have sources and actions", where)
		}
		if err := checkColumns(sec.Columns, where, fieldKnown); err != nil {
			return err
		}
		if sec.SortBy < 0 || sec.SortBy >= len(sec.Columns) {
			return fail("%s: sort_by must name one of its columns", where)
		}
		if sec.GroupBy != "" && sec.GroupBy != "category" && sec.GroupBy != "pool" {
			return fail("%s: group_by must be category or pool", where)
		}
	}
	return nil
}

// checkColumns checks a section's 1 to MaxColumns columns: their headings,
// and their bindings as label templates check theirs.
func checkColumns(cols []Column, where string, fieldKnown func(string) bool) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, fmt.Sprintf(format, args...))
	}
	if len(cols) == 0 || len(cols) > MaxColumns {
		return fail("%s: it needs 1 to %d columns", where, MaxColumns)
	}
	for j, c := range cols {
		col := fmt.Sprintf("%s, column %d", where, j+1)
		if err := textcheck.Check(col+" heading", c.Heading, textcheck.MaxName); err != nil {
			return fail("%v", err)
		}
		if err := textcheck.Check(col+" binding", c.Binding, MaxBinding); err != nil {
			return fail("%v", err)
		}
		b, err := labels.ParseBinding(c.Binding)
		if err != nil {
			return fail("%s: %v", col, err)
		}
		for _, ref := range b.Refs() {
			if ref.Kind == labels.RefField && fieldKnown != nil && !fieldKnown(ref.Arg) {
				return fail("%s: unknown field %q", col, ref.Arg)
			}
		}
	}
	return nil
}

// UsesLinkCounts reports whether a column of the report counts followed
// links ({{asset.needs}} and the rest), which are read only then.
func (s *Spec) UsesLinkCounts() bool {
	for _, sec := range s.Sections {
		for _, c := range sec.Columns {
			b, err := labels.ParseBinding(c.Binding)
			if err != nil {
				continue
			}
			for _, ref := range b.Refs() {
				if ref.IsLinkCount() {
					return true
				}
			}
		}
	}
	return false
}

// Result is a report run against assets.
type Result struct {
	Sections []SectionResult `json:"sections"`
	// Assets is how many assets the filter found.
	Assets int `json:"assets"`
}

// SectionResult is a table, with its headings and its rows in groups, or a
// chart, with its series.
type SectionResult struct {
	Type    string        `json:"type"`
	Title   string        `json:"title"`
	Columns []string      `json:"columns,omitempty"`
	Groups  []GroupResult `json:"groups,omitempty"`
	Chart   string        `json:"chart,omitempty"`
	Measure string        `json:"measure,omitempty"`
	Series  []Series      `json:"series,omitempty"`
	// Actions, for a scan's actions section, are the quick actions it
	// offers; the web app shows those the viewer may take.
	Actions []string `json:"actions,omitempty"`
}

// GroupResult is a group of rows, under a title when grouped.
type GroupResult struct {
	Title string `json:"title"`
	// RunningLow marks a category group whose category is flagged.
	RunningLow bool  `json:"running_low,omitempty"`
	Rows       []Row `json:"rows"`
	// Pool is, for a pool's group, its total and threshold.
	Pool *PoolSummary `json:"pool,omitempty"`
}

// PoolSummary is a pool's line in a table by pool.
type PoolSummary struct {
	CategoryID        string `json:"category_id"`
	Total             int    `json:"total"`
	Unit              string `json:"unit"`
	LowStockThreshold *int   `json:"low_stock_threshold"`
}

// Label is how a table's CSV names the group: its title, and for a pool
// its total and threshold, as "M5 bolts: 440 pcs (threshold 200 pcs)".
func (g GroupResult) Label() string {
	if g.Pool == nil {
		return g.Title
	}
	amount := func(n int) string { return strings.TrimSpace(strconv.Itoa(n) + " " + g.Pool.Unit) }
	label := g.Title + ": " + amount(g.Pool.Total)
	if t := g.Pool.LowStockThreshold; t != nil {
		label += " (threshold " + amount(*t) + ")"
	}
	return label
}

// Row is one asset's row.
type Row struct {
	AssetID string   `json:"asset_id"`
	Cells   []string `json:"cells"`
}

// Matches applies the parts of a filter the store does not: categories,
// running low, where it came from, and the contract covering it.
// categories is every category, for their parents and flags. The asset is as the request may see it, so a
// vendor hidden from it matches no vendor.
func (f Filter) Matches(a *models.AssetWithDetails, categories []models.Category) bool {
	if !f.matchesPurchase(a.Purchase) {
		return false
	}
	if f.Contract != "" && !coveredBy(a, f.Contract) {
		return false
	}
	if len(f.CategoryIDs) == 0 && !f.RunningLow {
		return true
	}
	byID := make(map[string]models.Category, len(categories))
	for _, c := range categories {
		byID[c.ID] = c
	}
	wanted := map[string]bool{}
	for _, id := range f.CategoryIDs {
		wanted[id] = true
	}
	inCategory, flagged := len(f.CategoryIDs) == 0, false
	direct := make([]string, len(a.Categories))
	for i, c := range a.Categories {
		direct[i] = c.ID
	}
	// Every category it counts in: its own, those above them, and those
	// they imply (docs/stock-and-lending.md#pools).
	reached := models.Reach(direct, func(id string) string {
		if c, ok := byID[id]; ok && c.ParentID != nil {
			return *c.ParentID
		}
		return ""
	}, func(id string) []string { return byID[id].Implies })
	for id := range reached.Counted {
		cat, ok := byID[id]
		if !ok {
			continue
		}
		inCategory = inCategory || wanted[id]
		flagged = flagged || (cat.IsConsumable && cat.IsRunningLow)
	}
	if f.RunningLow && !(flagged || (a.Asset.Stock != nil && a.Asset.Stock.IsRunningLow)) {
		return false
	}
	return inCategory
}

// Run lays out a report's sections for the assets its filter found, given
// what each asset's bindings resolve against, and for line charts their
// history (the events HistoryKinds names) up to now.
func (s *Spec) Run(assets []models.AssetWithDetails, categories []models.Category, data func(*models.AssetWithDetails) labels.Data, history []models.Event, now time.Time) *Result {
	flagged := map[string]bool{}
	for _, c := range categories {
		flagged[c.ID] = c.IsConsumable && c.IsRunningLow
	}
	res := &Result{Sections: []SectionResult{}, Assets: len(assets)}
	resolved := make([]labels.Data, len(assets))
	for i := range assets {
		resolved[i] = data(&assets[i])
	}
	for _, sec := range s.Sections {
		if sec.Type == "chart" {
			res.Sections = append(res.Sections, SectionResult{
				Type: sec.Type, Title: sec.Title, Chart: sec.Chart, Measure: sec.Measure,
				Series: runChart(sec, assets, resolved, history, now),
			})
			continue
		}
		out := SectionResult{Type: sec.Type, Title: sec.Title, Columns: make([]string, len(sec.Columns)), Groups: []GroupResult{}}
		bindings := make([]labels.Binding, len(sec.Columns))
		for i, c := range sec.Columns {
			out.Columns[i] = c.Heading
			bindings[i], _ = labels.ParseBinding(c.Binding) // checked by Parse
		}
		rows := make([]Row, len(assets))
		for i := range assets {
			row := Row{AssetID: assets[i].Asset.ID, Cells: make([]string, len(bindings))}
			for j, b := range bindings {
				row.Cells[j] = b.Resolve(resolved[i])
			}
			rows[i] = row
		}
		sortRows(rows, sec.SortBy, sec.Descending)
		if sec.GroupBy == "pool" {
			out.Groups = s.byPool(rows, categories, flagged)
			res.Sections = append(res.Sections, out)
			continue
		}
		if sec.GroupBy != "category" {
			out.Groups = append(out.Groups, GroupResult{Rows: rows})
			res.Sections = append(res.Sections, out)
			continue
		}
		// Under each category its asset is in, groups by name.
		byAsset := make(map[string]*models.AssetWithDetails, len(assets))
		for i := range assets {
			byAsset[assets[i].Asset.ID] = &assets[i]
		}
		groups := map[string]*GroupResult{}
		var order []string
		for _, row := range rows {
			for _, c := range byAsset[row.AssetID].Categories {
				g, ok := groups[c.ID]
				if !ok {
					g = &GroupResult{Title: c.Name, RunningLow: flagged[c.ID], Rows: []Row{}}
					groups[c.ID] = g
					order = append(order, c.ID)
				}
				g.Rows = append(g.Rows, row)
			}
		}
		sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].Title < groups[order[j]].Title })
		for _, id := range order {
			out.Groups = append(out.Groups, *groups[id])
		}
		res.Sections = append(res.Sections, out)
	}
	return res
}

// byPool groups rows by the pools their assets add to, a pool's group with
// its total and threshold, by name, and those in none last; an asset in
// two pools is in both.
func (s *Spec) byPool(rows []Row, categories []models.Category, flagged map[string]bool) []GroupResult {
	byID := make(map[string]models.Category, len(categories))
	for _, c := range categories {
		byID[c.ID] = c
	}
	groups := map[string]*GroupResult{}
	var order []string
	none := GroupResult{Title: NotInAPool, Rows: []Row{}}
	for _, row := range rows {
		var pools []string
		if s.Pools != nil {
			pools = s.Pools.Of[row.AssetID]
		}
		in := false
		for _, id := range pools {
			c, ok := byID[id]
			if !ok {
				continue
			}
			in = true
			g, ok := groups[id]
			if !ok {
				g = &GroupResult{Title: c.Name, RunningLow: flagged[id], Rows: []Row{},
					Pool: &PoolSummary{CategoryID: id, Total: s.Pools.Totals[id], Unit: c.Unit, LowStockThreshold: c.LowStockThreshold}}
				groups[id] = g
				order = append(order, id)
			}
			g.Rows = append(g.Rows, row)
		}
		if !in {
			none.Rows = append(none.Rows, row)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].Title < groups[order[j]].Title })
	out := []GroupResult{}
	for _, id := range order {
		out = append(out, *groups[id])
	}
	if len(none.Rows) > 0 {
		out = append(out, none)
	}
	return out
}

// sortRows sorts by a column: as numbers when both cells are numbers, and
// otherwise as text ignoring case, with ties kept in order.
func sortRows(rows []Row, col int, descending bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Cells[col], rows[j].Cells[col]
		if descending {
			a, b = b, a
		}
		x, errX := strconv.ParseFloat(a, 64)
		y, errY := strconv.ParseFloat(b, 64)
		if errX == nil && errY == nil {
			return x < y
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
}

// WriteCSV writes a table section as CSV, as the asset export writes its
// file: a byte order mark, its headings, then a row per asset, with a first
// column naming the group when it is grouped, and cells a spreadsheet would
// read as a formula defused.
func WriteCSV(w io.Writer, sec SectionResult) error {
	if _, err := io.WriteString(w, assetcsv.BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	for _, row := range Table(sec) {
		if err := cw.Write(safeCells(row)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Table is a section as its CSV has it: a heading row, then a row per
// asset, with its group first when the table is grouped; for a chart, a
// row per group or period, with a column per series.
func Table(sec SectionResult) [][]string {
	if sec.Type == "chart" {
		return chartTable(sec)
	}
	grouped := len(sec.Groups) > 0 && sec.Groups[0].Title != ""
	head := append([]string{}, sec.Columns...)
	if grouped {
		head = append([]string{"Group"}, head...)
	}
	out := [][]string{head}
	for _, g := range sec.Groups {
		for _, r := range g.Rows {
			cells := r.Cells
			if grouped {
				cells = append([]string{g.Label()}, cells...)
			}
			out = append(out, cells)
		}
	}
	return out
}

// Sheet is a section as a sheet of a workbook: its table, the heading row
// as text, and each cell a number or date where it reads back as itself
// as one, and otherwise text.
func Sheet(sec SectionResult, name string, taken map[string]bool) xlsx.Sheet {
	rows := Table(sec)
	out := xlsx.Sheet{Name: xlsx.SheetName(name, taken)}
	for i, row := range rows {
		cells := make([]xlsx.Value, len(row))
		for j, v := range row {
			if i == 0 {
				cells[j] = xlsx.TextCell(v)
			} else {
				cells[j] = xlsx.Guess(v)
			}
		}
		out.Rows = append(out.Rows, cells)
	}
	return out
}

func safeCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = assetcsv.Defuse(c)
	}
	return out
}

// coveredBy reports whether a contract covers an asset, as the request
// sees it: a contract whose vendor it may not see covers nothing.
func coveredBy(a *models.AssetWithDetails, contract string) bool {
	for _, c := range a.Contracts {
		if c.ID == contract {
			return true
		}
	}
	return false
}

// matchesPurchase applies the filter's vendor, purchase dates and warranty
// end.
func (f Filter) matchesPurchase(p models.Purchase) bool {
	if f.Vendor != "" && p.VendorID != f.Vendor {
		return false
	}
	if b := f.PurchasedBetween; b != nil && (b.From != "" || b.To != "") {
		on := p.OrderDate
		if on == "" {
			on = p.ReceivedDate
		}
		if on == "" || b.From != "" && on < b.From || b.To != "" && on > b.To {
			return false
		}
	}
	if f.WarrantyEndsBefore != "" && (p.WarrantyEnd == "" || p.WarrantyEnd >= f.WarrantyEndsBefore) {
		return false
	}
	return true
}
