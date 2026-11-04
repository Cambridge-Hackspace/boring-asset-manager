package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind is what a cell holds.
type Kind int

const (
	Text Kind = iota
	Number
	Bool
	Date
	DateTime
)

// A Value is one cell, written as its kind: text is always a text cell,
// never a formula, so nothing written can run in a spreadsheet.
type Value struct {
	Kind   Kind
	Text   string
	Number float64
	Bool   bool
	Time   time.Time
}

// A Sheet is a table: its first row is the header, which is bold, frozen
// and filtered.
type Sheet struct {
	Name string
	Rows [][]Value
}

const (
	// maxCellText is the most characters Excel keeps in a cell.
	maxCellText = 32767
	// maxSheetName is the longest a sheet's name may be.
	maxSheetName = 31
)

// The styles cells are written with, by index into styles.xml's cellXfs.
const (
	styleHeader   = 1
	styleDate     = 2
	styleDateTime = 3
)

// firstSerialDate is the first date written as a date: before it, Excel's
// serials count its mistaken 29 February 1900, so they are written as text.
var firstSerialDate = time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)

// TextCell is s as text.
func TextCell(s string) Value { return Value{Kind: Text, Text: s} }

// NumberCell is f as a number.
func NumberCell(f float64) Value { return Value{Kind: Number, Number: f} }

// BoolCell is b as TRUE or FALSE.
func BoolCell(b bool) Value { return Value{Kind: Bool, Bool: b} }

// DateCell is t's date; DateTimeCell is its date and time.
func DateCell(t time.Time) Value     { return Value{Kind: Date, Time: t} }
func DateTimeCell(t time.Time) Value { return Value{Kind: DateTime, Time: t} }

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var isoDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)

// NumberOrText is s as a number when it is one that reads back as itself,
// so that 5 is a number but 007, 1.50 and 1e3 stay text.
func NumberOrText(s string) Value {
	f, err := strconv.ParseFloat(s, 64)
	if err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && number(f) == s {
		return NumberCell(f)
	}
	return TextCell(s)
}

// DateOrText is s as a date when it is an ISO date, or a date and time
// with no zone, which a spreadsheet keeps; otherwise text.
func DateOrText(s string) Value {
	if isoDate.MatchString(s) {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return DateCell(t)
		}
	}
	if isoDateTime.MatchString(s) {
		if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
			return DateTimeCell(t)
		}
	}
	return TextCell(s)
}

// Guess is s as a number or a date when it reads back as itself as one,
// and otherwise text, for cells whose kind is not known.
func Guess(s string) Value {
	if c := NumberOrText(s); c.Kind == Number {
		return c
	}
	return DateOrText(s)
}

// SheetName makes name one a sheet may have, unlike any taken, ignoring
// case, and marks it taken: no []:*?/\, not starting or ending with an
// apostrophe, and at most 31 characters.
func SheetName(name string, taken map[string]bool) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) || r < 0x20 {
			return ' '
		}
		return r
	}, name)
	clean = strings.Trim(strings.TrimSpace(clean), "'")
	if clean == "" {
		clean = "Sheet"
	}
	out := truncate(clean, maxSheetName)
	for n := 2; taken[strings.ToLower(out)]; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		out = truncate(clean, maxSheetName-len(suffix)) + suffix
	}
	taken[strings.ToLower(out)] = true
	return out
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Write writes sheets as a workbook.
func Write(w io.Writer, sheets []Sheet) error {
	z := zip.NewWriter(w)
	add := func(name, content string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, content)
		return err
	}

	var types, book, rels, names strings.Builder
	types.WriteString(xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	book.WriteString(xml.Header + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, s := range sheets {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&book, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, attr(s.Name), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		// Excel keeps a sheet's filter as this name; it opens without one,
		// but would rather have it.
		if ref := filterRef(s); ref != "" {
			fmt.Fprintf(&names, `<definedName name="_xlnm._FilterDatabase" localSheetId="%d" hidden="1">'%s'!%s</definedName>`,
				i, attr(strings.ReplaceAll(s.Name, "'", "''")), absolute(ref))
		}
	}
	types.WriteString(`</Types>`)
	book.WriteString(`</sheets>`)
	if names.Len() > 0 {
		book.WriteString(`<definedNames>` + names.String() + `</definedNames>`)
	}
	book.WriteString(`</workbook>`)
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, len(sheets)+1)

	parts := []struct{ name, content string }{
		{"[Content_Types].xml", types.String()},
		{"_rels/.rels", xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", book.String()},
		{"xl/_rels/workbook.xml.rels", rels.String()},
		{"xl/styles.xml", styles},
	}
	for _, p := range parts {
		if err := add(p.name, p.content); err != nil {
			return err
		}
	}
	for i, s := range sheets {
		if err := add(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(s)); err != nil {
			return err
		}
	}
	return z.Close()
}

// styles: the default; the header, bold; dates; and dates with times, both
// as ISO, so they read the same wherever the workbook is opened.
const styles = xml.Header + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="2"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd hh:mm:ss"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

func columns(s Sheet) int {
	n := 0
	for _, row := range s.Rows {
		n = max(n, len(row))
	}
	return n
}

// filterRef is the range a sheet's filter covers: its header and rows.
func filterRef(s Sheet) string {
	cols := columns(s)
	if cols == 0 || len(s.Rows) == 0 {
		return ""
	}
	return "A1:" + Cell(len(s.Rows), cols-1)
}

var cellRef = regexp.MustCompile(`([A-Z]+)(\d+)`)

// absolute makes a range such as A1:C3 $A$1:$C$3.
func absolute(ref string) string {
	return cellRef.ReplaceAllString(ref, "$$$1$$$2")
}

func sheetXML(s Sheet) string {
	var b strings.Builder
	b.WriteString(xml.Header + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	// The header row stays in view as the rest scrolls.
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>` +
		`<selection pane="bottomLeft"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="15"/>`)

	widths := make([]int, columns(s))
	shown := make([][]string, len(s.Rows))
	for r, row := range s.Rows {
		shown[r] = make([]string, len(row))
		for c, cell := range row {
			shown[r][c] = display(cell)
			widths[c] = max(widths[c], utf8.RuneCountInString(shown[r][c]))
		}
	}
	if len(widths) > 0 {
		b.WriteString(`<cols>`)
		for c, w := range widths {
			// Fitted to the longest value, with room for the filter's
			// button, and kept within reason.
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, min(max(w+3, 8), 60))
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	for r, row := range s.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			ref := Cell(r+1, c)
			style := ""
			if r == 0 {
				style = fmt.Sprintf(` s="%d"`, styleHeader)
			}
			switch {
			case cell.Kind == Number && !math.IsInf(cell.Number, 0) && !math.IsNaN(cell.Number):
				fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(cell.Number, 'g', -1, 64))
			case cell.Kind == Bool:
				v := 0
				if cell.Bool {
					v = 1
				}
				fmt.Fprintf(&b, `<c r="%s"%s t="b"><v>%d</v></c>`, ref, style, v)
			case (cell.Kind == Date || cell.Kind == DateTime) && !cell.Time.Before(firstSerialDate):
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, map[Kind]int{Date: styleDate, DateTime: styleDateTime}[cell.Kind],
					strconv.FormatFloat(serial(cell), 'f', -1, 64))
			default:
				if shown[r][c] == "" {
					continue
				}
				fmt.Fprintf(&b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, text(shown[r][c]))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if ref := filterRef(s); ref != "" {
		fmt.Fprintf(&b, `<autoFilter ref="%s"/>`, ref)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

// display is a cell as it shows, for fitting its column and for text.
func display(c Value) string {
	switch c.Kind {
	case Number:
		return strconv.FormatFloat(c.Number, 'f', -1, 64)
	case Bool:
		return strings.ToUpper(strconv.FormatBool(c.Bool))
	case Date:
		return c.Time.Format("2006-01-02")
	case DateTime:
		return c.Time.Format("2006-01-02 15:04:05")
	}
	return c.Text
}

// serial is a date's Excel serial in the 1900 date system: days since
// 1899-12-30, the time of day as a fraction.
func serial(c Value) float64 {
	t := c.Time
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	days := math.Round(day.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	if c.Kind == Date {
		return days
	}
	secs := t.Hour()*3600 + t.Minute()*60 + t.Second()
	return days + float64(secs)/86400
}

// text escapes s for a cell, leaving out characters XML cannot hold, and
// cut to what Excel keeps.
func text(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || r >= 0x10000 {
			return r
		}
		return -1
	}, s)
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(truncate(s, maxCellText)))
	return b.String()
}

func attr(s string) string { return text(s) }
