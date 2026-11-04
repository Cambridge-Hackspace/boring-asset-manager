// Package xlsx reads the values of an Excel workbook (.xlsx), a sheet at a
// time, for importing assets (docs/csv.md#excel-workbooks). It reads only
// what an import needs, shared strings, sheets, cell types and cached
// values, and nothing else: formatting, comments and macros are ignored.
// It also writes tables as workbooks, for exports
// (docs/csv.md#exporting-to-excel), with typed cells and never a formula.
//
// A workbook is a ZIP of XML, so a hostile one is refused before it costs
// much: past MaxUncompressed in all, or a part compressed past MaxRatio to
// one, or one with a DTD, which is where entity expansion and external
// entities live.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxUncompressed is the most a workbook's parts may hold, in all.
	MaxUncompressed = 50 << 20
	// MaxRatio is the most a part may be compressed, uncompressed to
	// compressed.
	MaxRatio = 100
	// MaxSheets is the most sheets a workbook may have.
	MaxSheets = 50
	// MaxRows is the most rows a sheet may have.
	MaxRows = 5000
)

// ErrUnreadable is wrapped by every reason a workbook cannot be read.
var ErrUnreadable = errors.New("unreadable workbook")

func unreadable(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnreadable}, args...)...)
}

// IsWorkbook reports whether data looks like a ZIP, as a workbook is,
// rather than text.
func IsWorkbook(data []byte) bool {
	return bytes.HasPrefix(data, []byte("PK\x03\x04"))
}

// Workbook is an opened workbook: its sheets, by name, in order.
type Workbook struct {
	Sheets []string

	files    map[string]*zip.File
	paths    map[string]string // sheet name → part
	strings  []string
	dates    map[int]bool // style index → whether it shows a date
	date1904 bool
	read     int64 // bytes read so far, against MaxUncompressed
}

// Open reads a workbook's list of sheets, its shared strings and styles.
func Open(data []byte) (*Workbook, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, unreadable("not an .xlsx file; save it as an Excel workbook (.xlsx) or CSV")
	}
	w := &Workbook{files: map[string]*zip.File{}, paths: map[string]string{}, dates: map[int]bool{}}
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
		if f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > MaxRatio {
			return nil, unreadable("%s is compressed past %d to 1", f.Name, MaxRatio)
		}
		w.files[f.Name] = f
	}
	if total > MaxUncompressed {
		return nil, unreadable("it holds more than %d MB uncompressed", MaxUncompressed>>20)
	}
	if _, ok := w.files["xl/workbook.xml"]; !ok {
		if _, ods := w.files["content.xml"]; ods {
			return nil, unreadable("OpenDocument spreadsheets (.ods) are not taken; save it as an Excel workbook (.xlsx) or CSV")
		}
		return nil, unreadable("not an .xlsx file; save it as an Excel workbook (.xlsx) or CSV")
	}
	if err := w.readWorkbook(); err != nil {
		return nil, err
	}
	if err := w.readStrings(); err != nil {
		return nil, err
	}
	if err := w.readStyles(); err != nil {
		return nil, err
	}
	return w, nil
}

// part reads a part, counting it against MaxUncompressed whatever its
// header said, and refusing a DTD.
func (w *Workbook) part(name string) ([]byte, error) {
	f, ok := w.files[name]
	if !ok {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, unreadable("%s: %v", name, err)
	}
	defer rc.Close()
	left := MaxUncompressed - w.read
	data, err := io.ReadAll(io.LimitReader(rc, left+1))
	if err != nil {
		return nil, unreadable("%s: %v", name, err)
	}
	w.read += int64(len(data))
	if int64(len(data)) > left {
		return nil, unreadable("it holds more than %d MB uncompressed", MaxUncompressed>>20)
	}
	if f.CompressedSize64 > 0 && uint64(len(data))/f.CompressedSize64 > MaxRatio {
		return nil, unreadable("%s is compressed past %d to 1", name, MaxRatio)
	}
	if bytes.Contains(data, []byte("<!DOCTYPE")) || bytes.Contains(data, []byte("<!ENTITY")) {
		return nil, unreadable("%s declares a DTD, which a workbook never needs", name)
	}
	return data, nil
}

func decoder(data []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	// No entity but XML's own five.
	d.Entity = map[string]string{}
	return d
}

func (w *Workbook) readWorkbook() error {
	data, err := w.part("xl/workbook.xml")
	if err != nil {
		return err
	}
	var book struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := decoder(data).Decode(&book); err != nil {
		return unreadable("xl/workbook.xml: %v", err)
	}
	w.date1904 = book.Pr.Date1904 == "1" || book.Pr.Date1904 == "true"
	rels, err := w.part("xl/_rels/workbook.xml.rels")
	if err != nil {
		return err
	}
	var r struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := decoder(rels).Decode(&r); err != nil {
		return unreadable("xl/_rels/workbook.xml.rels: %v", err)
	}
	targets := map[string]string{}
	for _, rel := range r.Rels {
		t := rel.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join("xl", t)
		}
		targets[rel.ID] = t
	}
	if len(book.Sheets) > MaxSheets {
		return unreadable("it has %d sheets; workbooks are limited to %d", len(book.Sheets), MaxSheets)
	}
	for _, s := range book.Sheets {
		p, ok := targets[s.ID]
		if !ok {
			return unreadable("sheet %q has no part", s.Name)
		}
		w.Sheets = append(w.Sheets, s.Name)
		w.paths[s.Name] = p
	}
	if len(w.Sheets) == 0 {
		return unreadable("it has no sheets")
	}
	return nil
}

// textOf joins a string item's runs, leaving out phonetic guides.
type stringItem struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (s stringItem) text() string {
	if len(s.Runs) == 0 {
		return s.T
	}
	var b strings.Builder
	b.WriteString(s.T)
	for _, r := range s.Runs {
		b.WriteString(r.T)
	}
	return b.String()
}

func (w *Workbook) readStrings() error {
	data, err := w.part("xl/sharedStrings.xml")
	if err != nil || data == nil {
		return err
	}
	var sst struct {
		Items []stringItem `xml:"si"`
	}
	if err := decoder(data).Decode(&sst); err != nil {
		return unreadable("xl/sharedStrings.xml: %v", err)
	}
	for _, it := range sst.Items {
		w.strings = append(w.strings, it.text())
	}
	return nil
}

// builtinDates are the number formats Excel defines that show a date or
// time.
var builtinDates = map[int]bool{14: true, 15: true, 16: true, 17: true, 18: true, 19: true, 20: true, 21: true, 22: true, 45: true, 46: true, 47: true}

// isDateFormat reports whether a custom number format shows a date or
// time: it has a d, m, y, h or s outside quotes, brackets and escapes.
func isDateFormat(code string) bool {
	quoted, bracket := false, false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '\\':
			i++
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case bracket:
		case strings.IndexByte("dmyhsDMYHS", c) >= 0:
			return true
		}
	}
	return false
}

func (w *Workbook) readStyles() error {
	data, err := w.part("xl/styles.xml")
	if err != nil || data == nil {
		return err
	}
	var styles struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			NumFmt int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := decoder(data).Decode(&styles); err != nil {
		return unreadable("xl/styles.xml: %v", err)
	}
	custom := map[int]bool{}
	for _, f := range styles.NumFmts {
		custom[f.ID] = isDateFormat(f.Code)
	}
	for i, xf := range styles.Xfs {
		if d, ok := custom[xf.NumFmt]; ok {
			w.dates[i] = d
		} else {
			w.dates[i] = builtinDates[xf.NumFmt]
		}
	}
	return nil
}

// Cell is a cell's place, as Excel names it: "B7".
func Cell(row, col int) string {
	name := ""
	for col++; col > 0; col = (col - 1) / 26 {
		name = string(rune('A'+(col-1)%26)) + name
	}
	return name + strconv.Itoa(row)
}

// parseRef reads a cell reference into its 1-based row and 0-based column.
func parseRef(ref string) (row, col int, ok bool) {
	i := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A'+1)
		i++
	}
	if i == 0 || col > 16384 {
		return 0, 0, false
	}
	row, err := strconv.Atoi(ref[i:])
	if err != nil || row < 1 {
		return 0, 0, false
	}
	return row, col - 1, true
}

// Rows reads a sheet's values, as text, by row: Rows[i] is row i+1, and
// rows with nothing in them are empty. A formula without a cached value,
// or an error value, is refused, naming the cell.
func (w *Workbook) Rows(sheet string) ([][]string, error) {
	p, ok := w.paths[sheet]
	if !ok {
		return nil, unreadable("it has no sheet called %q", sheet)
	}
	data, err := w.part(p)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, unreadable("sheet %q has no part", sheet)
	}
	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				R  string     `xml:"r,attr"`
				T  string     `xml:"t,attr"`
				S  int        `xml:"s,attr"`
				F  *struct{}  `xml:"f"`
				V  *string    `xml:"v"`
				Is stringItem `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
		Merges []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCells>mergeCell"`
	}
	if err := decoder(data).Decode(&ws); err != nil {
		return nil, unreadable("sheet %q: %v", sheet, err)
	}
	var rows [][]string
	next := 1
	for _, r := range ws.Rows {
		n := r.R
		if n == 0 {
			n = next
		}
		next = n + 1
		if n > MaxRows+1 {
			return nil, unreadable("sheet %q has more than %d rows", sheet, MaxRows)
		}
		for len(rows) < n {
			rows = append(rows, nil)
		}
		var cells []string
		col := 0
		for _, c := range r.Cells {
			if c.R != "" {
				_, cc, ok := parseRef(c.R)
				if !ok {
					return nil, unreadable("sheet %q: %q is not a cell", sheet, c.R)
				}
				col = cc
			}
			name := Cell(n, col)
			v, err := w.value(c.T, c.S, c.F != nil, c.V, c.Is)
			if err != nil {
				return nil, unreadable("sheet %q, cell %s: %v", sheet, name, err)
			}
			for len(cells) < col {
				cells = append(cells, "")
			}
			if len(cells) == col {
				cells = append(cells, v)
			} else {
				cells[col] = v
			}
			col++
		}
		rows[n-1] = cells
	}
	// A merged cell across the header row is refused: which column would
	// its name be?
	header := 0
	for i, r := range rows {
		if !blank(r) {
			header = i + 1
			break
		}
	}
	for _, m := range ws.Merges {
		from, to, _ := strings.Cut(m.Ref, ":")
		r1, _, ok1 := parseRef(from)
		r2, _, ok2 := parseRef(to)
		if ok1 && ok2 && header >= r1 && header <= r2 {
			return nil, unreadable("sheet %q: the header row has merged cells (%s); unmerge them, so each column has its own name", sheet, m.Ref)
		}
	}
	return rows, nil
}

func blank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// value is a cell's value as text.
func (w *Workbook) value(t string, style int, formula bool, v *string, is stringItem) (string, error) {
	if t == "inlineStr" {
		return is.text(), nil
	}
	if v == nil {
		if formula {
			return "", errors.New("its formula has no saved value; open it in a spreadsheet, save it again, and import that")
		}
		return "", nil
	}
	switch t {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(*v))
		if err != nil || i < 0 || i >= len(w.strings) {
			return "", fmt.Errorf("names shared string %q, which there is not", *v)
		}
		return w.strings[i], nil
	case "str":
		return *v, nil
	case "b":
		if strings.TrimSpace(*v) == "1" {
			return "true", nil
		}
		return "false", nil
	case "e":
		return "", fmt.Errorf("it holds the error %s", *v)
	case "d":
		// An ISO 8601 date, as some writers store it.
		return strings.TrimSuffix(strings.TrimSpace(*v), "T00:00:00"), nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(*v), 64)
	if err != nil {
		return "", fmt.Errorf("%q is not a number", *v)
	}
	if w.dates[style] {
		return w.date(f), nil
	}
	return number(f), nil
}

// number writes a number as it shows, to Excel's 15 significant digits:
// 0.1 stays 0.1, and 5.0 is 5.
func number(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'g', 15, 64), 64)
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// date writes a date serial as an ISO date, with its time when it has one.
func (w *Workbook) date(serial float64) string {
	// 1900's serials count from 1899-12-30, as Excel's do past its
	// mistaken 29 February 1900; 1904's from 1904-01-01.
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if serial < 60 {
		// Before it: Excel counts 1900-01-01 as 1.
		epoch = epoch.AddDate(0, 0, 1)
	}
	if w.date1904 {
		epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	days := math.Floor(serial)
	secs := math.Round((serial - days) * 86400)
	t := epoch.AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second)
	if secs == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02T15:04:05")
}
