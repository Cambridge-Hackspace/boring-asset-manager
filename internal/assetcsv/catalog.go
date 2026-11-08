package assetcsv

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// A vendor's catalog as CSV (docs/csv.md#vendor-catalogs): one row per
// entry, written and read with the same columns.

// Catalog columns.
const (
	CatalogID           = "id"
	CatalogCategory     = "category"
	CatalogAsset        = "asset"
	CatalogPartNumber   = "part_number"
	CatalogDescription  = "description"
	CatalogURL          = "url"
	CatalogPackSize     = "pack_size"
	CatalogPackUnit     = "pack_unit"
	CatalogPrice        = "price"
	CatalogCurrency     = "currency"
	CatalogPriceChecked = "price_checked"
	CatalogPreferred    = "preferred"
)

// CatalogColumns are a catalog's columns, in the order an export writes
// them.
var CatalogColumns = []string{CatalogID, CatalogCategory, CatalogAsset, CatalogPartNumber, CatalogDescription, CatalogURL,
	CatalogPackSize, CatalogPackUnit, CatalogPrice, CatalogCurrency, CatalogPriceChecked, CatalogPreferred}

// catalogPriceColumns are those only purchases.read sees.
var catalogPriceColumns = map[string]bool{CatalogPrice: true, CatalogCurrency: true, CatalogPriceChecked: true}

// ExportCatalog writes a vendor's entries as CSV: what each is linked to,
// a category by its path and an asset as a links cell names it, and its
// price blank where it is hidden.
func ExportCatalog(w io.Writer, entries []models.CatalogEntry, cats *Categories, refs *AssetRefs) error {
	if _, err := io.WriteString(w, BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write(CatalogColumns); err != nil {
		return err
	}
	for _, e := range entries {
		var category, asset, pack, preferred string
		if e.CategoryID != "" {
			category = cats.Ref(e.CategoryID)
		}
		if e.AssetID != "" {
			asset = refs.Ref(e.AssetID, e.LinkedName)
		}
		if e.PackSize != nil {
			pack = strconv.Itoa(*e.PackSize)
		}
		if e.Preferred {
			preferred = "yes"
		}
		row := []string{e.ID, category, asset, e.PartNumber, e.Description, e.URL, pack, e.PackUnit, e.Price, e.Currency, e.PriceChecked, preferred}
		for i := range row {
			row[i] = Defuse(row[i])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// CatalogPlan is what importing a vendor's catalog would do.
type CatalogPlan struct {
	Rows         int       `json:"rows"`
	Create       int       `json:"create"`
	Update       int       `json:"update"`
	Unchanged    int       `json:"unchanged"`
	Problems     []Problem `json:"problems"`
	ProblemCount int       `json:"problem_count"`
	// Writes are the entries to make and replace, once there are no
	// problems.
	Writes []store.CatalogWrite `json:"-"`
}

func (p *CatalogPlan) problem(row int, column, format string, args ...any) {
	p.ProblemCount++
	if len(p.Problems) < maxProblems {
		p.Problems = append(p.Problems, Problem{Row: row, Column: column, Message: fmt.Sprintf(format, args...)})
	}
}

// CatalogImport is what planning an import of a vendor's catalog reads.
type CatalogImport struct {
	VendorID string
	// Entries are the vendor's entries the importer sees, by ID: the rows
	// naming one replace it.
	Entries map[string]models.CatalogEntry
	// Categories resolve category cells, and Assets asset cells, among the
	// assets the importer may read.
	Categories *Categories
	Assets     *AssetRefs
	// Prices is whether the importer may see, and so give, prices.
	Prices bool
}

// PlanCatalog reads a vendor's catalog as CSV, and works out what importing
// it would do: each row with an ID replaces that entry, and each without
// makes one. A column the file leaves out keeps what an entry has; the
// price columns keep it too for an importer who may not see prices, and
// may not be filled in by them.
func PlanCatalog(data []byte, in CatalogImport) (*CatalogPlan, error) {
	header, rows, err := parse(data)
	if err != nil {
		return nil, err
	}
	plan := &CatalogPlan{Rows: len(rows), Problems: []Problem{}}
	known := map[string]bool{}
	for _, c := range CatalogColumns {
		known[c] = true
	}
	at := map[string]int{}
	for i, h := range header {
		name := strings.ToLower(h)
		switch {
		case !known[name]:
			plan.problem(1, h, "%q is not a catalog column; the columns are %s", h, strings.Join(CatalogColumns, ", "))
		case at[name] > 0:
			plan.problem(1, h, "%q is given twice", h)
		default:
			at[name] = i + 1
		}
	}
	if at[CatalogCategory] == 0 && at[CatalogAsset] == 0 && at[CatalogID] == 0 {
		plan.problem(1, "", "a catalog needs a category or an asset column, to say what each entry is for")
	}
	if plan.ProblemCount > 0 {
		return plan, nil
	}
	seen := map[string]int{}
	preferredBy := map[string]int{}
	for _, rec := range rows {
		cell := func(col string) (string, bool) {
			i := at[col]
			if i == 0 {
				return "", false
			}
			if i-1 < len(rec.cells) {
				return rec.cells[i-1], true
			}
			return "", true
		}
		before := plan.ProblemCount
		var entry store.CatalogEntryInput
		var had *models.CatalogEntry
		id, _ := cell(CatalogID)
		if id != "" {
			e, ok := in.Entries[id]
			switch {
			case !ok:
				plan.problem(rec.line, CatalogID, "no entry of this vendor has the ID %q", id)
				continue
			case seen[id] > 0:
				plan.problem(rec.line, CatalogID, "the entry %q is also on line %d", id, seen[id])
				continue
			}
			seen[id] = rec.line
			had = &e
			entry = store.CatalogEntryInputOf(e)
		}
		entry.VendorID = in.VendorID
		category, hasCategory := cell(CatalogCategory)
		asset, hasAsset := cell(CatalogAsset)
		if hasCategory || hasAsset {
			entry.CategoryID, entry.AssetID = "", ""
		}
		if category != "" {
			cid, err := in.Categories.Resolve(category)
			if err != nil {
				plan.problem(rec.line, CatalogCategory, "%v", err)
			}
			entry.CategoryID = cid
		}
		if asset != "" {
			switch ids := in.Assets.Resolve(asset); len(ids) {
			case 1:
				entry.AssetID = ids[0]
			case 0:
				plan.problem(rec.line, CatalogAsset, "no asset is %q", asset)
			default:
				plan.problem(rec.line, CatalogAsset, "%d assets are %q; use an ID or a code", len(ids), asset)
			}
		}
		if category != "" && asset != "" {
			plan.problem(rec.line, CatalogAsset, "an entry is for a category or an asset, not both")
		}
		for _, f := range []struct {
			col  string
			into *string
		}{
			{CatalogPartNumber, &entry.PartNumber}, {CatalogDescription, &entry.Description}, {CatalogURL, &entry.URL},
			{CatalogPackUnit, &entry.PackUnit}, {CatalogPrice, &entry.Price}, {CatalogCurrency, &entry.Currency},
			{CatalogPriceChecked, &entry.PriceChecked},
		} {
			v, ok := cell(f.col)
			if !ok {
				continue
			}
			if catalogPriceColumns[f.col] && !in.Prices {
				if v != "" {
					plan.problem(rec.line, f.col, "seeing and giving prices needs purchases.read")
				}
				continue
			}
			*f.into = v
		}
		if v, ok := cell(CatalogPackSize); ok {
			entry.PackSize = nil
			if v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					plan.problem(rec.line, CatalogPackSize, "a pack size is a whole number, such as 100")
				} else {
					entry.PackSize = &n
				}
			}
		}
		if v, ok := cell(CatalogPreferred); ok {
			switch strings.ToLower(v) {
			case "yes", "true", "1", "y":
				entry.Preferred = true
			case "", "no", "false", "0", "n":
				entry.Preferred = false
			default:
				plan.problem(rec.line, CatalogPreferred, "preferred is yes, or blank for no")
			}
		}
		if plan.ProblemCount > before {
			continue
		}
		if err := store.CheckCatalogEntry(entry); err != nil {
			plan.problem(rec.line, "", "%s", strings.TrimPrefix(err.Error(), store.ErrInvalidCatalogEntry.Error()+": "))
			continue
		}
		if entry.Preferred {
			thing := "c:" + entry.CategoryID + "a:" + entry.AssetID
			if line := preferredBy[thing]; line > 0 {
				plan.problem(rec.line, CatalogPreferred, "line %d prefers an entry for the same, and only one is preferred", line)
				continue
			}
			preferredBy[thing] = rec.line
		}
		switch {
		case had == nil:
			plan.Create++
		case sameCatalogEntry(store.CatalogEntryInputOf(*had), entry):
			plan.Unchanged++
			continue
		default:
			plan.Update++
		}
		plan.Writes = append(plan.Writes, store.CatalogWrite{ID: id, In: entry})
	}
	if plan.ProblemCount > 0 {
		plan.Writes = nil
	}
	return plan, nil
}

// sameCatalogEntry reports whether an import leaves an entry as it is.
func sameCatalogEntry(a, b store.CatalogEntryInput) bool {
	return a.VendorID == b.VendorID && a.CategoryID == b.CategoryID && a.AssetID == b.AssetID && a.PartNumber == b.PartNumber &&
		a.Description == b.Description && a.URL == b.URL && sameInt(a.PackSize, b.PackSize) && a.PackUnit == b.PackUnit &&
		a.Price == b.Price && strings.EqualFold(a.Currency, b.Currency) && a.PriceChecked == b.PriceChecked && a.Preferred == b.Preferred
}
