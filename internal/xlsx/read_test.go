package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The same sheet, as each program writes it, reads the same: past its empty
// first row and column, numbers as written, dates as ISO dates, booleans
// as true and false, and formulas as their cached values.
func TestWorkbooksFromEachProgramReadAlike(t *testing.T) {
	want := "\nName,Quantity,Bought,Working,Total\nSaw,0.1,2024-03-05,true,0.2\nClamp,5,2023-12-31,false,10\n"
	for _, name := range []string{"excel.xlsx", "libreoffice.xlsx", "google.xlsx"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if !IsWorkbook(data) {
			t.Errorf("%s: not seen as a workbook", name)
		}
		w, err := Open(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(w.Sheets, []string{"Tools", "Bins"}) {
			t.Errorf("%s: sheets %v", name, w.Sheets)
		}
		got, err := w.CSV("Tools")
		if err != nil || string(got) != want {
			t.Errorf("%s: Tools read as %q, %v", name, got, err)
		}
		if got, err := w.CSV("Bins"); err != nil || string(got) != "Name\nParts bin A3 by the lathe\n" {
			t.Errorf("%s: Bins read as %q, %v", name, got, err)
		}
		if _, err := w.CSV("Nope"); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: a sheet it has not: %v", name, err)
		}
	}
}

// build makes a workbook of one sheet, Sheet1, with the rows given, and
// any other parts.
func build(t *testing.T, rows string, parts map[string]string) []byte {
	t.Helper()
	all := map[string]string{
		"xl/workbook.xml": `<workbook xmlns="` + mainNS + `" xmlns:r="` + relNS + `"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relNS + `/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="` + mainNS + `"><sheetData>` + rows + `</sheetData></worksheet>`,
	}
	for k, v := range parts {
		all[k] = v
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range all {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(data))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const (
	mainNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	relNS  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

func sheet1(t *testing.T, data []byte) (string, error) {
	t.Helper()
	w, err := Open(data)
	if err != nil {
		return "", err
	}
	out, err := w.CSV("Sheet1")
	return string(out), err
}

func TestCellsThatCannotBeRead(t *testing.T) {
	for what, c := range map[string]struct{ rows, parts, want string }{
		"a formula with no cached value": {`<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row><row r="2"><c r="A2"><f>B1*2</f></c></row>`, "", "cell A2: its formula has no saved value"},
		"an error value":                 {`<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c></row><row r="2"><c r="A2" t="e"><v>#DIV/0!</v></c></row>`, "", "cell A2: it holds the error #DIV/0!"},
		"a shared string that is not":    {`<row r="1"><c r="C1" t="s"><v>4</v></c></row>`, "", "cell C1: names shared string"},
		"merged header cells": {`<row r="2"><c r="A2" t="inlineStr"><is><t>Name</t></is></c></row>`,
			"merged", "the header row has merged cells (A2:B2)"},
		"nothing at all": {``, "", "is empty"},
	} {
		parts := map[string]string{}
		if c.parts == "merged" {
			parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + mainNS + `"><sheetData>` + c.rows + `</sheetData><mergeCells count="1"><mergeCell ref="A2:B2"/></mergeCells></worksheet>`
		}
		_, err := sheet1(t, build(t, c.rows, parts))
		if !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", what, err)
		}
	}
}

func TestHostileWorkbooksAreRefused(t *testing.T) {
	// A part that inflates past the ratio: a bomb, in small.
	bomb := strings.Repeat("0", 2<<20)
	if _, err := Open(build(t, "", map[string]string{"xl/bomb.xml": bomb})); err == nil || !strings.Contains(err.Error(), "compressed past 100 to 1") {
		t.Errorf("a bomb: %v", err)
	}
	// Entities, declared in a DTD, which a workbook never has.
	billion := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;">]>` +
		`<sst xmlns="` + mainNS + `"><si><t>&lol2;</t></si></sst>`
	if _, err := Open(build(t, "", map[string]string{"xl/sharedStrings.xml": billion})); err == nil || !strings.Contains(err.Error(), "DTD") {
		t.Errorf("entity expansion: %v", err)
	}
	external := `<?xml version="1.0"?><!DOCTYPE x [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><worksheet xmlns="` + mainNS + `"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>&xxe;</t></is></c></row></sheetData></worksheet>`
	if _, err := sheet1(t, build(t, "", map[string]string{"xl/worksheets/sheet1.xml": external})); err == nil || !strings.Contains(err.Error(), "DTD") {
		t.Errorf("an external entity: %v", err)
	}
	// Too many sheets.
	var sheets, rels strings.Builder
	for i := 1; i <= MaxSheets+1; i++ {
		fmt.Fprintf(&sheets, `<sheet name="S%d" sheetId="%d" r:id="rId%d"/>`, i, i, i)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Target="worksheets/sheet1.xml"/>`, i)
	}
	many := build(t, "", map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="` + mainNS + `" xmlns:r="` + relNS + `"><sheets>` + sheets.String() + `</sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`,
	})
	if _, err := Open(many); err == nil || !strings.Contains(err.Error(), "limited to 50") {
		t.Errorf("51 sheets: %v", err)
	}
	// Too many rows.
	if _, err := sheet1(t, build(t, `<row r="5002"><c r="A5002"><v>1</v></c></row>`, nil)); err == nil || !strings.Contains(err.Error(), "more than 5000 rows") {
		t.Errorf("5002 rows: %v", err)
	}
	// Not a workbook at all, and an OpenDocument one.
	if _, err := Open([]byte("Name\nSaw\n")); err == nil || !strings.Contains(err.Error(), "not an .xlsx file") {
		t.Errorf("a CSV: %v", err)
	}
	var ods bytes.Buffer
	z := zip.NewWriter(&ods)
	f, _ := z.Create("content.xml")
	f.Write([]byte("<office:document-content/>"))
	z.Close()
	if _, err := Open(ods.Bytes()); err == nil || !strings.Contains(err.Error(), ".ods") {
		t.Errorf("an OpenDocument spreadsheet: %v", err)
	}
}

func TestNumbersAndDates(t *testing.T) {
	for f, want := range map[float64]string{0.1: "0.1", 0.1 + 0.2: "0.3", 5: "5", 5.0000000000000009: "5", -12.5: "-12.5", 1e20: "100000000000000000000", 0.000001: "0.000001"} {
		if got := number(f); got != want {
			t.Errorf("number(%v) = %q, want %q", f, got, want)
		}
	}
	w := &Workbook{}
	for serial, want := range map[float64]string{45356: "2024-03-05", 45356.5: "2024-03-05T12:00:00", 61: "1900-03-01", 1: "1900-01-01"} {
		if got := w.date(serial); got != want {
			t.Errorf("date(%v) = %q, want %q", serial, got, want)
		}
	}
	w.date1904 = true
	if got := w.date(0); got != "1904-01-01" {
		t.Errorf("1904's first day: %q", got)
	}
	for code, want := range map[string]bool{"yyyy-mm-dd": true, `0.00" days"`: false, "[Red]0.00": false, "h:mm": true, `\d0`: false, "General": false} {
		if got := isDateFormat(code); got != want {
			t.Errorf("isDateFormat(%q) = %v", code, got)
		}
	}
	if Cell(7, 1) != "B7" || Cell(1, 26) != "AA1" || Cell(3, 0) != "A3" {
		t.Errorf("cell names: %s %s %s", Cell(7, 1), Cell(1, 26), Cell(3, 0))
	}
}
