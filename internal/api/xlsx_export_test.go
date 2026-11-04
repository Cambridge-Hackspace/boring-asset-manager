package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

// workbookPart is one part of a workbook, as written.
func workbookPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := z.Open(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

func TestExportAssetsAsAWorkbook(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	nothingPublic(t, srv)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateCategory("Tools", nil, "", false, false)
	drills, _ := srv.store.CreateCategory("Drills", &tools.ID, "", false, false)
	saws, _ := srv.store.CreateCategory("Saws", nil, "", false, false)
	serial, _ := srv.store.CreateField("serial", "Serial", "text", nil)
	watts, _ := srv.store.CreateField("watts", "Watts", "number", nil)
	bought, _ := srv.store.CreateField("bought", "Bought", "date", nil)
	for _, a := range []struct{ cat, field string }{{tools.ID, serial.ID}, {drills.ID, watts.ID}, {saws.ID, bought.ID}} {
		if err := srv.store.AssignCategoryField(a.cat, a.field, false); err != nil {
			t.Fatal(err)
		}
	}
	evil := `=HYPERLINK("https://evil.example","Drill")`
	drill, err := srv.store.CreateAsset(evil, []string{drills.ID, saws.ID}, nil, map[string]string{"serial": "007", "watts": "750", "bought": "2024-03-05"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateAsset("Hammer", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.xlsx", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != xlsxType ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="assets-`) || !strings.HasSuffix(rec.Header().Get("Content-Disposition"), `.xlsx"`) {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	data := rec.Body.Bytes()
	book, err := xlsx.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	// All, then each category with assets, by path; Tools has none of its own.
	if !reflect.DeepEqual(book.Sheets, []string{"All", "Saws", "Drills"}) {
		t.Fatalf("sheets: %v", book.Sheets)
	}
	all, _ := book.Rows("All")
	if strings.Join(all[0], ",") != "id,name,categories,code.qr,code.micro_qr,code.code128,field.bought,field.serial,field.watts" || len(all) != 3 {
		t.Fatalf("All: %q", all)
	}
	// The drill is on each of its categories' sheets, with only their
	// fields; Drills' include those it inherits from Tools.
	for sheet, want := range map[string][]string{
		"Drills": {"id", "name", "categories", "code.qr", "code.micro_qr", "code.code128", "field.serial", "field.watts"},
		"Saws":   {"id", "name", "categories", "code.qr", "code.micro_qr", "code.code128", "field.bought"},
	} {
		rows, _ := book.Rows(sheet)
		if len(rows) != 2 || !reflect.DeepEqual(rows[0], want) || rows[1][0] != drill.Asset.ID || rows[1][1] != evil {
			t.Errorf("%s: %q", sheet, rows)
		}
	}

	// Typed: the number and date fields as a number and a date; a number
	// field's text, and text that looks like a formula, as text.
	sheet := workbookPart(t, data, "xl/worksheets/sheet2.xml")
	if !strings.Contains(sheet, `s="2"><v>45356</v>`) {
		t.Errorf("bought is not a date: %s", sheet)
	}
	drillsSheet := workbookPart(t, data, "xl/worksheets/sheet3.xml")
	if !strings.Contains(drillsSheet, `<v>750</v>`) || !strings.Contains(drillsSheet, `t="inlineStr"><is><t xml:space="preserve">007</t>`) {
		t.Errorf("watts or serial: %s", drillsSheet)
	}
	for _, name := range []string{"xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml", "xl/worksheets/sheet3.xml"} {
		if p := workbookPart(t, data, name); strings.Contains(p, "<f>") || strings.Contains(p, "<f ") {
			t.Errorf("%s has a formula", name)
		}
	}

	// It imports again to the same assets.
	rec = postWorkbook(t, srv, cookie, "/api/v1/assets/import", data, "All", all[0])
	var done struct{ Created, Updated, Unchanged int }
	_ = json.Unmarshal(rec.Body.Bytes(), &done)
	if rec.Code != http.StatusOK || done.Created != 0 || done.Updated != 0 || done.Unchanged != 2 {
		t.Errorf("imported again: %d %s", rec.Code, rec.Body)
	}
	after, _ := srv.store.GetAssetByID(drill.Asset.ID)
	if after.Asset.Name != evil || after.FieldValues["serial"] != "007" || after.FieldValues["watts"] != "750" || after.FieldValues["bought"] != "2024-03-05" {
		t.Errorf("after: %+v", after)
	}

	// The CSV export's filters apply.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.xlsx?search=hamm", "")
	book, _ = xlsx.Open(rec.Body.Bytes())
	if rows, _ := book.Rows("All"); !reflect.DeepEqual(book.Sheets, []string{"All"}) || len(rows) != 2 || rows[1][1] != "Hammer" {
		t.Errorf("searched: %v %q", book.Sheets, rows)
	}
	nobody, _ := srv.store.CreateUser("nobody@test.org", "NobodySecret123!", true)
	if rec := doJSON(t, srv, sessionCookie(t, srv, nobody.ID), http.MethodGet, "/api/v1/assets/export.xlsx", ""); rec.Code != http.StatusForbidden {
		t.Errorf("without assets.read: %d", rec.Code)
	}
}

func TestReportTablesDownloadAsWorkbooks(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports?kind=report", "")
	var list struct{ Reports []models.Report }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	var runningLow string
	for _, r := range list.Reports {
		if r.Name == "Running low" {
			runningLow = r.ID
		}
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Filament","is_consumable":true,"is_running_low":true}`)
	var cat struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"=Blue PLA","category_ids":["`+cat.Category.ID+`"],"stock":{"quantity":200,"unit":"g"}}`)

	// A section, as its CSV has it.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+runningLow+"/sections/1/xlsx", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != xlsxType || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Running-low.xlsx"`) {
		t.Fatalf("section: %d %v", rec.Code, rec.Header())
	}
	book, err := xlsx.Open(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := book.Rows(book.Sheets[0])
	if want := [][]string{{"Group", "Asset", "In stock", "Where"}, {"Filament", "=Blue PLA", "200 g"}}; !reflect.DeepEqual(rows, want) {
		t.Errorf("rows: %q", rows)
	}

	// The whole report, a sheet per section.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+runningLow+"/xlsx", "")
	book, err = xlsx.Open(rec.Body.Bytes())
	if rec.Code != http.StatusOK || err != nil || len(book.Sheets) != 1 {
		t.Fatalf("report: %d %v %v", rec.Code, err, book)
	}
	if again, _ := book.Rows(book.Sheets[0]); !reflect.DeepEqual(again, rows) {
		t.Errorf("report rows: %q", again)
	}

	for path, want := range map[string]int{
		"/api/v1/reports/" + runningLow + "/sections/2/xlsx": http.StatusNotFound,
		"/api/v1/reports/" + runningLow + "/sections/0/xlsx": http.StatusBadRequest,
		"/api/v1/reports/missing/xlsx":                       http.StatusNotFound,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodGet, path, ""); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}
