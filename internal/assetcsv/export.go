package assetcsv

import (
	"encoding/csv"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Column names, and the prefixes of generator and field columns.
const (
	ColumnID         = "id"
	ColumnName       = "name"
	ColumnCategories = "categories"
	ColumnQuantity   = "quantity"
	ColumnUnit       = "unit"
	ColumnThreshold  = "low_stock_threshold"
	ColumnLinks      = "links"
	ColumnIcon       = "icon"
	// Where an asset came from (docs/data-model.md#purchases): its vendor,
	// by name, its dates, YYYY-MM-DD, and its price in its currency.
	ColumnVendor       = "vendor"
	ColumnOrderDate    = "order_date"
	ColumnReceivedDate = "received_date"
	ColumnPrice        = "price"
	ColumnCurrency     = "currency"
	ColumnOrderNumber  = "order_number"
	ColumnWarrantyEnd  = "warranty_end"
	// ColumnReviewState is an asset's review state (docs/review.md), when
	// any asset's categories use review states. An import ignores it:
	// assets it makes are drafts unless the importer confirms them.
	ColumnReviewState = "review_state"
	CodePrefix        = "code."
	FieldPrefix       = "field."
)

// A links cell lists links as verb:asset, separated by LinkSeparator.
const LinkSeparator = ";"

// BOM starts every exported file, so spreadsheets read it as UTF-8.
const BOM = "\xef\xbb\xbf"

var plainNumber = regexp.MustCompile(`^[-+]?[0-9]+(\.[0-9]+)?$`)

// Defuse stops a spreadsheet treating a cell as a formula: a cell starting
// with =, +, -, @, a tab or a carriage return gets a leading apostrophe,
// unless it is a plain number. A cell already starting with an apostrophe
// gets another, so Undefuse gives back exactly what was written.
func Defuse(s string) string {
	if s == "" || plainNumber.MatchString(s) {
		return s
	}
	if strings.ContainsRune("=+-@\t\r'", rune(s[0])) {
		return "'" + s
	}
	return s
}

// Undefuse reverses Defuse: an apostrophe followed by =, +, -, @, a tab, a
// carriage return or another apostrophe is removed.
func Undefuse(s string) string {
	if len(s) >= 2 && s[0] == '\'' && strings.ContainsRune("=+-@\t\r'", rune(s[1])) {
		return s[1:]
	}
	return s
}

// Export writes assets as CSV: id, name, categories, a code column for each
// generator type, the stock columns when any asset's stock is counted
// by quantity, an icon column when any asset has an icon of its own (blank
// for those drawn with their categories'), the purchase columns when any
// asset has purchase details (the price, currency and order number only
// when one shows any), a review_state column when any asset's categories
// use review states, a field column for every field any asset has a
// value for, by key, and, when any asset has links and refs is given, a links column,
// naming other assets as refs does.
func Export(w io.Writer, assets []models.AssetWithDetails, cats *Categories, generatorTypes []string, refs *AssetRefs) error {
	cols, rows := table(assets, cats, generatorTypes, refs, filledFieldKeys(assets), nil)
	if _, err := io.WriteString(w, BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.name
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
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

// Table is the export's columns and its rows, as Export writes them but
// not defused, with a column for every field any asset has a value for:
// for a document of the assets, which chooses among the columns.
func Table(assets []models.AssetWithDetails, cats *Categories, generatorTypes []string, refs *AssetRefs) ([]string, [][]string) {
	cols, rows := table(assets, cats, generatorTypes, refs, filledFieldKeys(assets), nil)
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	return names, rows
}

// filledFieldKeys are the keys of the fields any asset has a value for, sorted.
func filledFieldKeys(assets []models.AssetWithDetails) []string {
	keys := map[string]bool{}
	for _, a := range assets {
		for k, v := range a.FieldValues {
			if v != "" {
				keys[k] = true
			}
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fieldCell is a field's value as a cell holds it: a person field's names,
// those the exporter sees, separated by RefSeparator, as an import reads
// them back; never emails.
func fieldCell(a *models.AssetWithDetails, key string) string {
	if _, ok := a.PersonFields[key]; !ok {
		return a.FieldValues[key]
	}
	var names []string
	for _, id := range strings.Split(a.FieldValues[key], ",") {
		if p, ok := a.People[strings.TrimSpace(id)]; ok {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, RefSeparator+" ")
}

// What a column's cells hold, for a workbook.
const (
	kindText   = "text"
	kindNumber = "number"
	kindDate   = "date"
)

// column is one of the export's columns: its name, and what its cells hold.
type column struct{ name, kind string }

// table is the export's columns, and a row of each asset's values as they
// are, before CSV defuses them: Export's columns, with fieldKeys as the
// field columns, of the kinds fieldKinds gives by key (text when not).
func table(assets []models.AssetWithDetails, cats *Categories, generatorTypes []string, refs *AssetRefs, fieldKeys []string, fieldKinds map[string]string) ([]column, [][]string) {
	stocked, linked, iconed, bought, priced, reviewed := false, false, false, false, false, false
	for _, a := range assets {
		stocked = stocked || a.Asset.Stock != nil
		reviewed = reviewed || a.Review != nil
		p := a.Purchase
		bought = bought || p.VendorName != "" || p.OrderDate != "" || p.ReceivedDate != "" || p.WarrantyEnd != ""
		priced = priced || p.Price != "" || p.Currency != "" || p.OrderNumber != ""
		iconed = iconed || a.Asset.IconName != nil
		linked = linked || (refs != nil && len(a.Links) > 0)
	}

	cols := []column{{ColumnID, kindText}, {ColumnName, kindText}, {ColumnCategories, kindText}}
	for _, t := range generatorTypes {
		cols = append(cols, column{CodePrefix + t, kindText})
	}
	if stocked {
		cols = append(cols, column{ColumnQuantity, kindNumber}, column{ColumnUnit, kindText}, column{ColumnThreshold, kindNumber})
	}
	if iconed {
		cols = append(cols, column{ColumnIcon, kindText})
	}
	if bought {
		cols = append(cols, column{ColumnVendor, kindText}, column{ColumnOrderDate, kindDate}, column{ColumnReceivedDate, kindDate})
	}
	if priced {
		cols = append(cols, column{ColumnPrice, kindNumber}, column{ColumnCurrency, kindText}, column{ColumnOrderNumber, kindText})
	}
	if bought {
		cols = append(cols, column{ColumnWarrantyEnd, kindDate})
	}
	if reviewed {
		cols = append(cols, column{ColumnReviewState, kindText})
	}
	for _, k := range fieldKeys {
		kind := fieldKinds[k]
		if kind == "" {
			kind = kindText
		}
		cols = append(cols, column{FieldPrefix + k, kind})
	}
	if linked {
		cols = append(cols, column{ColumnLinks, kindText})
	}

	rows := make([][]string, 0, len(assets))
	for _, a := range assets {
		codes := map[string]string{}
		for _, gk := range a.GeneratorKeys {
			codes[gk.GeneratorType] = gk.AssignedCode
		}
		row := []string{a.Asset.ID, a.Asset.Name, cats.Refs(a.Categories)}
		for _, t := range generatorTypes {
			row = append(row, codes[t])
		}
		if stocked {
			row = append(row, stockCells(a.Asset.Stock)...)
		}
		if iconed {
			icon := ""
			if a.Asset.IconName != nil {
				icon = *a.Asset.IconName
			}
			row = append(row, icon)
		}
		p := a.Purchase
		if bought {
			row = append(row, p.VendorName, p.OrderDate, p.ReceivedDate)
		}
		if priced {
			row = append(row, p.Price, p.Currency, p.OrderNumber)
		}
		if bought {
			row = append(row, p.WarrantyEnd)
		}
		if reviewed {
			state := ""
			if a.Review != nil {
				state = a.Review.State
			}
			row = append(row, state)
		}
		for _, k := range fieldKeys {
			row = append(row, fieldCell(&a, k))
		}
		if linked {
			row = append(row, linksCell(a.Links, refs))
		}
		rows = append(rows, row)
	}
	return cols, rows
}

// stockCells are an asset's quantity, unit and threshold cells; blank when
// its stock is not counted by quantity.
func stockCells(s *models.AssetStock) []string {
	if s == nil {
		return []string{"", "", ""}
	}
	threshold := ""
	if s.LowStockThreshold != nil {
		threshold = strconv.Itoa(*s.LowStockThreshold)
	}
	return []string{strconv.Itoa(s.Quantity), s.Unit, threshold}
}

// linksCell writes an asset's links as verb:asset entries.
func linksCell(links []models.AssetLink, refs *AssetRefs) string {
	entries := make([]string, len(links))
	for i, l := range links {
		entries[i] = store.RelationSlug(l.Label) + ":" + refs.Ref(l.Asset.ID, l.Asset.Name)
	}
	return strings.Join(entries, LinkSeparator)
}
