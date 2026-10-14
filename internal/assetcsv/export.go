package assetcsv

import (
	"encoding/csv"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Column names, and the prefixes of generator and field columns.
const (
	ColumnID         = "id"
	ColumnName       = "name"
	ColumnCategories = "categories"
	CodePrefix       = "code."
	FieldPrefix      = "field."
)

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
// generator type, and a field column for every field any asset has a value
// for, by key.
func Export(w io.Writer, assets []models.AssetWithDetails, cats *Categories, generatorTypes []string) error {
	keys := map[string]bool{}
	for _, a := range assets {
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
	for _, k := range fieldKeys {
		header = append(header, FieldPrefix+k)
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
		for _, k := range fieldKeys {
			row = append(row, Defuse(a.FieldValues[k]))
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
