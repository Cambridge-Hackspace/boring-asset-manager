package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
)

// postWorkbook sends a workbook to an import route, naming a sheet unless
// it is "".
func postWorkbook(t *testing.T, srv *Server, cookie *http.Cookie, path string, data []byte, sheet string, mapping []string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "assets.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	if sheet != "" {
		_ = mw.WriteField("sheet", sheet)
	}
	if mapping != nil {
		raw, _ := json.Marshal(mapping)
		_ = mw.WriteField("mapping", string(raw))
	}
	_ = mw.Close()
	return do(t, srv, cookie, http.MethodPost, path, mw.FormDataContentType(), &body)
}

func TestImportingAWorkbookASheetAtATime(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	book, err := os.ReadFile("../xlsx/testdata/excel.xlsx")
	if err != nil {
		t.Fatal(err)
	}

	// Without a sheet, the preview lists them.
	rec := postWorkbook(t, srv, cookie, "/api/v1/assets/import/preview", book, "", nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"sheets":["Tools","Bins"]}` {
		t.Fatalf("sheets: %d %s", rec.Code, rec.Body)
	}
	if rec := postWorkbook(t, srv, cookie, "/api/v1/assets/import", book, "", []string{"name"}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sheet is required") {
		t.Errorf("importing with no sheet: %d %s", rec.Code, rec.Body)
	}

	// A sheet previews as its CSV does, its problems on the sheet's rows.
	mapping := []string{"name", "ignore", "ignore", "id", "ignore"}
	rec = postWorkbook(t, srv, cookie, "/api/v1/assets/import/preview", book, "Tools", mapping)
	var plan assetcsv.Plan
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	csvRec := postImport(t, srv, cookie, "/api/v1/assets/import/preview", "\nName,Quantity,Bought,Working,Total\nSaw,0.1,2024-03-05,true,0.2\nClamp,5,2023-12-31,false,10\n", mapping)
	var fromCSV assetcsv.Plan
	_ = json.Unmarshal(csvRec.Body.Bytes(), &fromCSV)
	if rec.Code != http.StatusOK || plan.ProblemCount != 2 || plan.Problems[0].Row != 3 || plan.Problems[1].Row != 4 {
		t.Errorf("Tools: %d %s", rec.Code, rec.Body)
	}
	if got, want := rec.Body.String(), csvRec.Body.String(); got != want {
		t.Errorf("Tools and its CSV differ:\n%s\n%s", got, want)
	}

	// And imports.
	if rec := postWorkbook(t, srv, cookie, "/api/v1/assets/import", book, "Bins", []string{"name"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"created":1`) {
		t.Fatalf("Bins: %d %s", rec.Code, rec.Body)
	}
	list, _ := srv.store.ListAssets("", "Parts bin A3 by the lathe")
	if len(list) != 1 {
		t.Errorf("imported: %+v", list)
	}

	// A sheet it has not, and the old binary format, are refused.
	if rec := postWorkbook(t, srv, cookie, "/api/v1/assets/import/preview", book, "Nope", nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `no sheet called \"Nope\"`) {
		t.Errorf("a sheet it has not: %d %s", rec.Code, rec.Body)
	}
	xls := append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), make([]byte, 504)...)
	if rec := postWorkbook(t, srv, cookie, "/api/v1/assets/import/preview", xls, "", nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "(.xls) are not taken") {
		t.Errorf("an .xls: %d %s", rec.Code, rec.Body)
	}
}
