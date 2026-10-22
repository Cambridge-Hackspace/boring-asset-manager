package reports

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Kinds of saved report: a report runs over the assets its filter finds; a
// scan template is what scanning one asset's code shows (README §6.4).
const (
	KindReport = "report"
	KindScan   = "scan"
)

// SourceContents, as a scan table's source, lists what the scanned asset
// holds: the assets it contains through location links.
const SourceContents = "contents"

// ScanActions are the quick actions a scan template may offer, in the order
// the web app shows them: open the asset's page, edit it, use some of its
// stock, borrow it, give it back, report a problem, the actions its
// categories offer, such as Identify (README §5.4), and start a new asset
// like it, or in it (README §3.2).
var ScanActions = []string{"open", "edit", "use", "borrow", "return", "report", "actions", "add_similar", "add_inside"}

// ParseScan decodes and checks a scan template's spec. It has no filter,
// since it shows the one asset scanned, and its sections are details (the
// asset's own values, a binding each), tables of its contents, and quick
// actions; no charts.
func ParseScan(raw []byte, fieldKnown func(key string) bool) (*Spec, error) {
	var spec Spec
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, fmt.Sprintf(format, args...))
	}
	f := spec.Filter
	if len(f.CategoryIDs) > 0 || f.Search != "" || f.Archived || f.RunningLow || f.Loan != "" || f.Condition != "" {
		return nil, fail("a scan template has no filter: it shows the asset scanned")
	}
	if len(spec.Sections) == 0 || len(spec.Sections) > MaxSections {
		return nil, fail("a scan template needs 1 to %d sections", MaxSections)
	}
	for i, sec := range spec.Sections {
		where := fmt.Sprintf("section %d", i+1)
		if err := textcheck.Check(where+" title", sec.Title, textcheck.MaxName); err != nil {
			return nil, fail("%v", err)
		}
		if sec.Chart != "" || sec.Measure != "" || sec.Period != "" {
			return nil, fail("%s: a scan template has no charts", where)
		}
		switch sec.Type {
		case "details":
			if sec.Source != "" || len(sec.Actions) > 0 || sec.SortBy != 0 || sec.Descending || sec.GroupBy != "" {
				return nil, fail("%s: details are bindings of the asset alone", where)
			}
			if err := checkColumns(sec.Columns, where, fieldKnown); err != nil {
				return nil, err
			}
		case "table":
			if sec.Source != SourceContents {
				return nil, fail("%s: a table's source must be contents", where)
			}
			if len(sec.Actions) > 0 || sec.GroupBy != "" {
				return nil, fail("%s: a table of contents has no actions or groups", where)
			}
			if err := checkColumns(sec.Columns, where, fieldKnown); err != nil {
				return nil, err
			}
			if sec.SortBy < 0 || sec.SortBy >= len(sec.Columns) {
				return nil, fail("%s: sort_by must name one of its columns", where)
			}
		case "actions":
			if sec.Source != "" || len(sec.Columns) > 0 || sec.SortBy != 0 || sec.Descending || sec.GroupBy != "" {
				return nil, fail("%s: actions list actions only", where)
			}
			if len(sec.Actions) == 0 {
				return nil, fail("%s: name at least one action", where)
			}
			seen := map[string]bool{}
			for _, a := range sec.Actions {
				if !knownAction(a) {
					return nil, fail("%s: unknown action %q; actions are %s", where, a, strings.Join(ScanActions, ", "))
				}
				if seen[a] {
					return nil, fail("%s: %s is listed twice", where, a)
				}
				seen[a] = true
			}
		default:
			return nil, fail("%s: type must be details, table or actions", where)
		}
	}
	return &spec, nil
}

func knownAction(a string) bool {
	for _, k := range ScanActions {
		if a == k {
			return true
		}
	}
	return false
}

// RunScan lays out a scan template for the asset scanned, with what it holds
// for tables of contents, given what each asset's bindings resolve against.
func (s *Spec) RunScan(asset *models.AssetWithDetails, contents []models.AssetWithDetails, data func(*models.AssetWithDetails) labels.Data) *Result {
	res := &Result{Sections: []SectionResult{}, Assets: 1}
	var held []labels.Data
	for _, sec := range s.Sections {
		out := SectionResult{Type: sec.Type, Title: sec.Title}
		switch sec.Type {
		case "actions":
			out.Actions = append([]string{}, sec.Actions...)
		case "details":
			out.Columns, out.Groups = make([]string, len(sec.Columns)), []GroupResult{{Rows: []Row{rowOf(asset, data(asset), sec.Columns)}}}
			for i, c := range sec.Columns {
				out.Columns[i] = c.Heading
			}
		case "table":
			if held == nil {
				held = make([]labels.Data, len(contents))
				for i := range contents {
					held[i] = data(&contents[i])
				}
			}
			out.Columns = make([]string, len(sec.Columns))
			for i, c := range sec.Columns {
				out.Columns[i] = c.Heading
			}
			rows := make([]Row, len(contents))
			for i := range contents {
				rows[i] = rowOf(&contents[i], held[i], sec.Columns)
			}
			sortRows(rows, sec.SortBy, sec.Descending)
			out.Groups = []GroupResult{{Rows: rows}}
		}
		res.Sections = append(res.Sections, out)
	}
	return res
}

// rowOf is an asset's row of cells, one per column's binding.
func rowOf(a *models.AssetWithDetails, d labels.Data, cols []Column) Row {
	row := Row{AssetID: a.Asset.ID, Cells: make([]string, len(cols))}
	for i, c := range cols {
		b, _ := labels.ParseBinding(c.Binding) // checked by ParseScan
		row.Cells[i] = b.Resolve(d)
	}
	return row
}
