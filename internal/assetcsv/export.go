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
	CodePrefix       = "code."
	FieldPrefix      = "field."
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
// for those drawn with their categories'), a field column for every field
// any asset has a value for,
// by key, and, when any asset has links and refs is given, a links column,
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
	stocked, linked, iconed := false, false, false
	for _, a := range assets {
		stocked = stocked || a.Asset.Stock != nil
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
		for _, k := range fieldKeys {
			row = append(row, a.FieldValues[k])
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
