package xlsx

import (
	"bytes"
	"encoding/csv"
)

// CSV writes a sheet as CSV, for the CSV import to read
// (docs/csv.md#excel-workbooks). Empty columns at its edges are left out.
// Each of the sheet's rows is a line, empty ones blank, so that the import
// names the rows the sheet does; the CSV reader skips blank lines, so the
// first row with anything in it is the header.
func (w *Workbook) CSV(sheet string) ([]byte, error) {
	rows, err := w.Rows(sheet)
	if err != nil {
		return nil, err
	}
	first, last := -1, -1
	for _, r := range rows {
		for i, c := range r {
			if c == "" {
				continue
			}
			if first < 0 || i < first {
				first = i
			}
			if i > last {
				last = i
			}
		}
	}
	if first < 0 {
		return nil, unreadable("sheet %q is empty", sheet)
	}
	var b bytes.Buffer
	cw := csv.NewWriter(&b)
	for _, r := range rows {
		if blank(r) {
			cw.Flush()
			b.WriteString("\n")
			continue
		}
		cells := make([]string, last-first+1)
		for i := range cells {
			if first+i < len(r) {
				cells[i] = r[first+i]
			}
		}
		if err := cw.Write(cells); err != nil {
			return nil, err
		}
	}
	cw.Flush()
	return b.Bytes(), cw.Error()
}
