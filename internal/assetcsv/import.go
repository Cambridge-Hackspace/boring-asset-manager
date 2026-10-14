package assetcsv

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// Limits on an import.
const (
	MaxBytes = 5 << 20
	MaxRows  = 5000
	// maxProblems bounds the problems reported; ProblemCount has them all.
	maxProblems = 500
)

// TargetIgnore maps a column to nothing.
const TargetIgnore = "ignore"

// ErrUnreadable is wrapped when a file cannot be imported at all.
var ErrUnreadable = errors.New("unreadable file")

// Source is what planning reads; *store.Store is one.
type Source interface {
	ListAllCategories() ([]models.Category, error)
	ListFields() ([]models.Field, error)
	ListAssets(categoryID, search string) ([]models.AssetWithDetails, error)
	GetAssetByID(id string) (*models.AssetWithDetails, error)
	GetEffectiveFieldsForCategories(ids []string) ([]models.EffectiveField, error)
}

// Problem is something wrong with a file, at a line (the header is line 1)
// and, where it applies, a column.
type Problem struct {
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

// Column is a header with its suggested and chosen targets, and the first
// value in it, to help people map it.
type Column struct {
	Header    string `json:"header"`
	Suggested string `json:"suggested"`
	Target    string `json:"target"`
	Sample    string `json:"sample"`
}

// Plan is what an import would do.
type Plan struct {
	Columns           []Column  `json:"columns"`
	Rows              int       `json:"rows"`
	Create            int       `json:"create"`
	Update            int       `json:"update"`
	Unchanged         int       `json:"unchanged"`
	Problems          []Problem `json:"problems"`
	ProblemCount      int       `json:"problem_count"`
	UnknownCategories []string  `json:"unknown_categories"`
	// Changes holds the rows to apply, once there are no problems.
	Changes []store.ImportRow `json:"-"`
}

func (p *Plan) problem(row int, column, format string, args ...any) {
	p.ProblemCount++
	if len(p.Problems) < maxProblems {
		p.Problems = append(p.Problems, Problem{Row: row, Column: column, Message: fmt.Sprintf(format, args...)})
	}
}

// record is a data row and the line it starts on.
type record struct {
	line  int
	cells []string
}

// parse reads a file's header and rows, skipping blank rows.
func parse(data []byte) ([]string, []record, error) {
	if len(data) > MaxBytes {
		return nil, nil, fmt.Errorf("%w: files are limited to %d bytes", ErrUnreadable, MaxBytes)
	}
	data = bytes.TrimPrefix(data, []byte(BOM))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil, fmt.Errorf("%w: the file is empty", ErrUnreadable)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	var rows []record
	for {
		cells, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
		}
		line, _ := r.FieldPos(0)
		blank := true
		for i := range cells {
			cells[i] = Undefuse(strings.TrimSpace(cells[i]))
			blank = blank && cells[i] == ""
		}
		if blank {
			continue
		}
		if len(rows) == MaxRows {
			return nil, nil, fmt.Errorf("%w: imports are limited to %d rows", ErrUnreadable, MaxRows)
		}
		rows = append(rows, record{line: line, cells: cells})
	}
	return header, rows, nil
}

// Suggest proposes a target for each header: the export's own column
// names, a generator type, or a field's key or label. Columns it cannot
// place get "".
func Suggest(header []string, corpus []models.Field) []string {
	byName := map[string]string{}
	for _, f := range corpus {
		byName[strings.ToLower(f.Label)] = FieldPrefix + f.Key
	}
	for _, f := range corpus {
		byName[strings.ToLower(f.Key)] = FieldPrefix + f.Key
		byName[strings.ToLower(FieldPrefix+f.Key)] = FieldPrefix + f.Key
	}
	for _, t := range generators.AllowedGeneratorTypes() {
		byName[t] = CodePrefix + t
		byName[CodePrefix+t] = CodePrefix + t
	}
	for _, c := range []string{ColumnID, ColumnName, ColumnCategories} {
		byName[c] = c
	}
	out := make([]string, len(header))
	used := map[string]bool{}
	for i, h := range header {
		if t := byName[strings.ToLower(strings.TrimSpace(h))]; t != "" && !used[t] {
			out[i] = t
			used[t] = true
		}
	}
	return out
}

// Prepare reads a file and plans its import. A nil mapping uses the
// suggestions. Problems with the mapping or the rows are in the plan; an
// error means the file could not be read, or the data behind it could not.
func Prepare(src Source, data []byte, mapping []string) (*Plan, error) {
	header, rows, err := parse(data)
	if err != nil {
		return nil, err
	}
	corpus, err := src.ListFields()
	if err != nil {
		return nil, err
	}
	suggested := Suggest(header, corpus)
	if mapping == nil {
		mapping = suggested
	}
	if len(mapping) != len(header) {
		return nil, fmt.Errorf("%w: the mapping has %d entries for %d columns", ErrUnreadable, len(mapping), len(header))
	}
	plan := &Plan{Rows: len(rows), Problems: []Problem{}, UnknownCategories: []string{}}
	for i, h := range header {
		col := Column{Header: h, Suggested: suggested[i], Target: strings.TrimSpace(mapping[i])}
		for _, rec := range rows {
			if i < len(rec.cells) && rec.cells[i] != "" {
				col.Sample = rec.cells[i]
				break
			}
		}
		plan.Columns = append(plan.Columns, col)
	}
	fieldsByKey := map[string]models.Field{}
	for _, f := range corpus {
		fieldsByKey[f.Key] = f
	}
	if !checkMapping(plan, fieldsByKey) {
		return plan, nil
	}

	cats, err := src.ListAllCategories()
	if err != nil {
		return nil, err
	}
	active, err := src.ListAssets("", "")
	if err != nil {
		return nil, err
	}
	p := &planner{
		src: src, plan: plan, cats: NewCategories(cats), fields: fieldsByKey,
		assets: map[string]*models.AssetWithDetails{}, effective: map[string][]models.EffectiveField{},
		unknown: map[string]bool{}, owners: map[string][]owner{},
	}
	for i := range active {
		a := &active[i]
		p.assets[a.Asset.ID] = a
		for _, gk := range a.GeneratorKeys {
			p.claim(gk.GeneratorType, gk.AssignedCode, owner{asset: a.Asset.ID, name: a.Asset.Name})
		}
	}
	seenIDs := map[string]int{}
	for _, rec := range rows {
		if err := p.row(rec, seenIDs); err != nil {
			return nil, err
		}
	}
	p.checkCodes()
	sort.Strings(plan.UnknownCategories)
	if plan.ProblemCount > 0 {
		plan.Changes = nil
	}
	return plan, nil
}

// checkMapping reports mapping problems, returning whether there were none.
func checkMapping(plan *Plan, fieldsByKey map[string]models.Field) bool {
	used := map[string]string{}
	hasName, hasID := false, false
	for _, c := range plan.Columns {
		t := c.Target
		switch {
		case t == "":
			plan.problem(1, c.Header, "column %q is not mapped; choose where it goes or ignore it", c.Header)
			continue
		case t == TargetIgnore:
			continue
		case t == ColumnID:
			hasID = true
		case t == ColumnName:
			hasName = true
		case t == ColumnCategories:
		case strings.HasPrefix(t, CodePrefix) && generators.IsValidGeneratorType(strings.TrimPrefix(t, CodePrefix)):
		case strings.HasPrefix(t, FieldPrefix):
			if _, ok := fieldsByKey[strings.TrimPrefix(t, FieldPrefix)]; !ok {
				plan.problem(1, c.Header, "no field has the key %q", strings.TrimPrefix(t, FieldPrefix))
				continue
			}
		default:
			plan.problem(1, c.Header, "unknown target %q", t)
			continue
		}
		if prev, dup := used[t]; dup {
			plan.problem(1, c.Header, "columns %q and %q both map to %s", prev, c.Header, t)
		}
		used[t] = c.Header
	}
	if !hasName && !hasID {
		plan.problem(1, "", "map a column to name (to create assets) or to id (to update them)")
	}
	return plan.ProblemCount == 0
}

// owner holds a code in the final state: an asset, or a row creating one.
type owner struct {
	asset string // ID of an existing asset, or ""
	row   int    // line of the row that sets the code, or 0
	name  string
}

type planner struct {
	src       Source
	plan      *Plan
	cats      *Categories
	fields    map[string]models.Field
	assets    map[string]*models.AssetWithDetails
	effective map[string][]models.EffectiveField
	unknown   map[string]bool
	// owners maps "type\x00code" to who holds it once the import is done.
	owners map[string][]owner
}

func codeKey(t, code string) string { return t + "\x00" + code }

func (p *planner) claim(t, code string, o owner) {
	k := codeKey(t, code)
	p.owners[k] = append(p.owners[k], o)
}

// release drops an existing asset's hold on its code of a type.
func (p *planner) release(assetID, t string) {
	a := p.assets[assetID]
	for _, gk := range a.GeneratorKeys {
		if gk.GeneratorType != t {
			continue
		}
		k := codeKey(t, gk.AssignedCode)
		kept := p.owners[k][:0]
		for _, o := range p.owners[k] {
			if o.asset != assetID || o.row != 0 {
				kept = append(kept, o)
			}
		}
		p.owners[k] = kept
	}
}

// row plans one record.
func (p *planner) row(rec record, seenIDs map[string]int) error {
	line, plan := rec.line, p.plan
	if len(rec.cells) != len(plan.Columns) {
		plan.problem(line, "", "the row has %d cells but the header has %d", len(rec.cells), len(plan.Columns))
		return nil
	}
	cell := func(target string) (string, string, bool) {
		for i, c := range plan.Columns {
			if c.Target == target {
				return rec.cells[i], c.Header, true
			}
		}
		return "", "", false
	}
	before := plan.ProblemCount

	// The asset, if the row names one.
	var existing *models.AssetWithDetails
	if id, col, ok := cell(ColumnID); ok && id != "" {
		if prev, dup := seenIDs[id]; dup {
			plan.problem(line, col, "this asset is also on line %d", prev)
			return nil
		}
		seenIDs[id] = line
		existing = p.assets[id]
		if existing == nil {
			if _, err := uuid.Parse(id); err == nil {
				if a, err := p.src.GetAssetByID(id); err == nil && a.Asset.ArchivedAt != nil {
					plan.problem(line, col, "asset %s is archived", id)
					return nil
				}
			}
			plan.problem(line, col, "no asset has the ID %q", id)
			return nil
		}
	}
	change := store.ImportRow{Codes: map[string]string{}, FieldValues: map[string]string{}}
	if existing != nil {
		change.ID = existing.Asset.ID
	}

	// Name.
	name, nameCol, _ := cell(ColumnName)
	switch {
	case name != "":
		change.Name = name
	case existing != nil:
		change.Name = existing.Asset.Name
	default:
		plan.problem(line, nameCol, "a new asset needs a name")
	}

	// Categories.
	var finalCats []string
	if refs, col, ok := cell(ColumnCategories); ok && refs != "" {
		ids, errs := p.cats.ResolveAll(refs)
		for _, err := range errs {
			plan.problem(line, col, "%v", err)
		}
		for _, ref := range strings.Split(refs, RefSeparator) {
			ref = strings.TrimSpace(ref)
			if _, err := p.cats.Resolve(ref); ref != "" && errors.Is(err, ErrUnknownCategory) && !p.unknown[ref] {
				p.unknown[ref] = true
				plan.UnknownCategories = append(plan.UnknownCategories, ref)
			}
		}
		change.CategoryIDs = ids
		finalCats = ids
	} else if existing != nil {
		for _, c := range existing.Categories {
			if c.ID != uncategorized {
				finalCats = append(finalCats, c.ID)
			}
		}
	} else {
		change.CategoryIDs = []string{}
	}

	// Codes.
	for _, c := range plan.Columns {
		if !strings.HasPrefix(c.Target, CodePrefix) {
			continue
		}
		code, _, _ := cell(c.Target)
		if code == "" {
			continue
		}
		t := strings.TrimPrefix(c.Target, CodePrefix)
		if err := generators.ValidateGeneratorCode(t, code); err != nil {
			plan.problem(line, c.Header, "%v", err)
			continue
		}
		change.Codes[t] = code
	}

	// Field values, merged over the asset's own.
	values := map[string]string{}
	if existing != nil {
		for k, v := range existing.FieldValues {
			values[k] = v
		}
	}
	for _, c := range plan.Columns {
		if !strings.HasPrefix(c.Target, FieldPrefix) {
			continue
		}
		v, _, _ := cell(c.Target)
		if v == "" {
			continue
		}
		f := p.fields[strings.TrimPrefix(c.Target, FieldPrefix)]
		if err := fields.ValidateFieldValue(&f, v); err != nil {
			plan.problem(line, c.Header, "%v", err)
			continue
		}
		values[f.Key] = v
		change.FieldValues[f.ID] = v
	}

	// Required fields, from the categories the asset will have.
	effective, err := p.effectiveFields(finalCats)
	if err != nil {
		return err
	}
	for _, ef := range effective {
		if ef.IsRequired && strings.TrimSpace(values[ef.Field.Key]) == "" {
			col := ""
			for _, c := range plan.Columns {
				if c.Target == FieldPrefix+ef.Field.Key {
					col = c.Header
				}
			}
			plan.problem(line, col, "required field %q (%s) is missing", ef.Field.Label, ef.Field.Key)
		}
	}

	if plan.ProblemCount > before {
		return nil
	}

	// Codes this row assigns, in the final state.
	who := owner{row: line, name: change.Name}
	if existing != nil {
		who.asset = existing.Asset.ID
	}
	for t, code := range change.Codes {
		if existing != nil {
			p.release(existing.Asset.ID, t)
		}
		p.claim(t, code, who)
	}

	switch {
	case existing == nil:
		plan.Create++
		plan.Changes = append(plan.Changes, change)
	case unchanged(existing, change, p.fields):
		plan.Unchanged++
	default:
		plan.Update++
		plan.Changes = append(plan.Changes, change)
	}
	return nil
}

// effectiveFields resolves, and remembers, the fields of a set of categories.
func (p *planner) effectiveFields(cats []string) ([]models.EffectiveField, error) {
	ids := append([]string(nil), cats...)
	if len(ids) == 0 {
		ids = []string{uncategorized}
	}
	sort.Strings(ids)
	key := strings.Join(ids, ",")
	if ef, ok := p.effective[key]; ok {
		return ef, nil
	}
	ef, err := p.src.GetEffectiveFieldsForCategories(ids)
	if err != nil {
		return nil, err
	}
	p.effective[key] = ef
	return ef, nil
}

// checkCodes reports codes held by more than one asset once the import is
// done, against the rows that assign them.
func (p *planner) checkCodes() {
	keys := make([]string, 0, len(p.owners))
	for k := range p.owners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		owners := p.owners[k]
		if len(owners) < 2 {
			continue
		}
		t, code, _ := strings.Cut(k, "\x00")
		for _, o := range owners {
			if o.row == 0 {
				continue
			}
			for _, other := range owners {
				if other == o {
					continue
				}
				where := fmt.Sprintf("asset %q", other.name)
				if other.row != 0 {
					where = fmt.Sprintf("line %d", other.row)
				}
				p.plan.problem(o.row, CodePrefix+t, "code %q is also assigned to %s", code, where)
				break
			}
		}
	}
	if p.plan.ProblemCount > 0 {
		p.plan.Create, p.plan.Update, p.plan.Unchanged = 0, 0, 0
	}
}

// unchanged reports whether applying a change would leave an asset as it is.
func unchanged(a *models.AssetWithDetails, c store.ImportRow, corpus map[string]models.Field) bool {
	if c.Name != a.Asset.Name {
		return false
	}
	if c.CategoryIDs != nil {
		have := map[string]bool{}
		for _, cat := range a.Categories {
			if cat.ID != uncategorized {
				have[cat.ID] = true
			}
		}
		if len(have) != len(c.CategoryIDs) {
			return false
		}
		for _, id := range c.CategoryIDs {
			if !have[id] {
				return false
			}
		}
	}
	codes := map[string]string{}
	for _, gk := range a.GeneratorKeys {
		codes[gk.GeneratorType] = gk.AssignedCode
	}
	for t, code := range c.Codes {
		if codes[t] != code {
			return false
		}
	}
	keyOf := map[string]string{}
	for _, f := range corpus {
		keyOf[f.ID] = f.Key
	}
	for id, v := range c.FieldValues {
		if a.FieldValues[keyOf[id]] != v {
			return false
		}
	}
	return true
}
