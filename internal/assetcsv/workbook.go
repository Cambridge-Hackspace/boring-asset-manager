package assetcsv

import (
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

// AllSheet is the name of the workbook's sheet of every asset.
const AllSheet = "All"

// Workbook is the export as sheets (docs/csv.md#exporting-to-excel): All,
// with Export's columns, then a sheet per category any asset is in, by
// path, with that category's fields, inherited ones included, as its field
// columns. An asset in several categories is on each of their sheets.
// Cells are typed: quantities and number fields as numbers, date fields as
// dates, where each reads back as written. There are at most
// xlsx.MaxSheets sheets, so that the workbook imports again; categories
// past those are only on All.
//
// fields are every field, for their types; categoryFields gives a
// category's fields.
func Workbook(assets []models.AssetWithDetails, cats *Categories, generatorTypes []string, refs *AssetRefs,
	fields []models.Field, categoryFields func(id string) ([]models.EffectiveField, error)) ([]xlsx.Sheet, error) {
	kinds := map[string]string{}
	for _, f := range fields {
		switch f.ValidationType {
		case kindNumber, kindDate:
			kinds[f.Key] = f.ValidationType
		}
	}
	taken := map[string]bool{}
	sheet := func(name string, of []models.AssetWithDetails, fieldKeys []string) xlsx.Sheet {
		cols, rows := table(of, cats, generatorTypes, refs, fieldKeys, kinds)
		header := make([]xlsx.Value, len(cols))
		for i, c := range cols {
			header[i] = xlsx.TextCell(c.name)
		}
		out := xlsx.Sheet{Name: xlsx.SheetName(name, taken), Rows: [][]xlsx.Value{header}}
		for _, row := range rows {
			cells := make([]xlsx.Value, len(row))
			for i, v := range row {
				switch cols[i].kind {
				case kindNumber:
					cells[i] = xlsx.NumberOrText(v)
				case kindDate:
					cells[i] = xlsx.DateOrText(v)
				default:
					cells[i] = xlsx.TextCell(v)
				}
			}
			out.Rows = append(out.Rows, cells)
		}
		return out
	}

	sheets := []xlsx.Sheet{sheet(AllSheet, assets, filledFieldKeys(assets))}
	byCategory := map[string][]models.AssetWithDetails{}
	var ids []string
	for _, a := range assets {
		for _, c := range a.Categories {
			if c.ID == uncategorized {
				continue
			}
			if _, seen := byCategory[c.ID]; !seen {
				ids = append(ids, c.ID)
			}
			byCategory[c.ID] = append(byCategory[c.ID], a)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return cats.Path(ids[i]) < cats.Path(ids[j]) })
	for _, id := range ids {
		if len(sheets) == xlsx.MaxSheets {
			break
		}
		effective, err := categoryFields(id)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(effective))
		seen := map[string]bool{}
		for _, f := range effective {
			if !seen[f.Field.Key] {
				seen[f.Field.Key] = true
				keys = append(keys, f.Field.Key)
			}
		}
		sort.Strings(keys)
		sheets = append(sheets, sheet(cats.byID[id].Name, byCategory[id], keys))
	}
	return sheets, nil
}
