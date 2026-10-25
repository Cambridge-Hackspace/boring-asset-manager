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
	stocked, linked, iconed := false, false, false
	keys := map[string]bool{}
	for _, a := range assets {
		stocked = stocked || a.Asset.Stock != nil
		iconed = iconed || a.Asset.IconName != nil
		linked = linked || (refs != nil && len(a.Links) > 0)
		for k, v := range a.FieldValues {
			if v != "" {
				keys[k] = true
			}
		}
	}
	fieldKeys := make([]string, 0, len(keys))
	for k := range keys {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)

	header := []string{ColumnID, ColumnName, ColumnCategories}
	for _, t := range generatorTypes {
		header = append(header, CodePrefix+t)
	}
	if stocked {
		header = append(header, ColumnQuantity, ColumnUnit, ColumnThreshold)
	}
	if iconed {
		header = append(header, ColumnIcon)
	}
	for _, k := range fieldKeys {
		header = append(header, FieldPrefix+k)
	}
	if linked {
		header = append(header, ColumnLinks)
	}

	if _, err := io.WriteString(w, BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, a := range assets {
		codes := map[string]string{}
		for _, gk := range a.GeneratorKeys {
			codes[gk.GeneratorType] = gk.AssignedCode
		}
		row := []string{a.Asset.ID, Defuse(a.Asset.Name), Defuse(cats.Refs(a.Categories))}
		for _, t := range generatorTypes {
			row = append(row, Defuse(codes[t]))
		}
		if stocked {
			row = append(row, stockCells(a.Asset.Stock)...)
		}
		if iconed {
			icon := ""
			if a.Asset.IconName != nil {
				icon = *a.Asset.IconName
			}
			row = append(row, Defuse(icon))
		}
		for _, k := range fieldKeys {
			row = append(row, Defuse(a.FieldValues[k]))
		}
		if linked {
			row = append(row, Defuse(linksCell(a.Links, refs)))
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
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
	return []string{strconv.Itoa(s.Quantity), Defuse(s.Unit), threshold}
}

// linksCell writes an asset's links as verb:asset entries.
func linksCell(links []models.AssetLink, refs *AssetRefs) string {
	entries := make([]string, len(links))
	for i, l := range links {
		entries[i] = store.RelationSlug(l.Label) + ":" + refs.Ref(l.Asset.ID, l.Asset.Name)
	}
	return strings.Join(entries, LinkSeparator)
}
