package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pdfPages counts a PDF's pages.
func pdfPages(body []byte) int {
	return bytes.Count(body, []byte("/Type /Page /Parent"))
}

func TestAReportAsAPDF(t *testing.T) {
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
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Blue PLA","category_ids":["`+cat.Category.ID+`"],"stock":{"quantity":200,"unit":"g"}}`)

	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+runningLow+"/export.pdf", "")
	body := rec.Body.Bytes()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(body, []byte("%PDF-1.4")) ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Running-low.pdf"`) {
		t.Fatalf("%d %v %q", rec.Code, rec.Header(), body[:min(len(body), 40)])
	}
	// Letter, by default; text in a font of its own, searchable; no
	// pictures unless asked for.
	if !bytes.Contains(body, []byte("/MediaBox [0 0 612 792]")) || !bytes.Contains(body, []byte("/Subtype /Type3")) || bytes.Contains(body, []byte("/Subtype /Image")) {
		t.Errorf("the PDF: %d bytes", len(body))
	}

	// The instance's page size, or one asked for, turned.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/documents", `{"instance_name":"Cambridge Hackspace","page_size":"a4"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	if body := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+runningLow+"/export.pdf", "").Body.Bytes(); !bytes.Contains(body, []byte("/MediaBox [0 0 595.3 841.9]")) {
		t.Error("the instance's A4 was not used")
	}
	if body := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/reports/"+runningLow+"/export.pdf?page_size=letter&orientation=landscape", "").Body.Bytes(); !bytes.Contains(body, []byte("/MediaBox [0 0 792 612]")) {
		t.Error("letter, landscape, was not used")
	}
	for path, want := range map[string]int{
		"/api/v1/reports/" + runningLow + "/export.pdf?page_size=legal":      http.StatusBadRequest,
		"/api/v1/reports/" + runningLow + "/export.pdf?orientation=sideways": http.StatusBadRequest,
		"/api/v1/reports/missing/export.pdf":                                 http.StatusNotFound,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodGet, path, ""); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}

func TestTheAssetListAsAPDF(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	nothingPublic(t, srv)
	cookie := sessionCookie(t, srv, admin.ID)
	serial, _ := srv.store.CreateField("serial", "Serial number", "text", nil)
	_ = serial
	// A label to preview, given by the category.
	tag := createTemplate(t, srv, cookie, `{"name":"Tag","units":"mm","width":40,"height":20,"dpi":203,"elements":[
	  {"type":"text","x":2,"y":2,"width":36,"height":10,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools","color_hex":"#1d4ed8"}`)
	var cat struct{ Category struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat.Category.ID+"/label-template", `{"label_template_ids":["`+tag+`"]}`)
	drill, _ := srv.store.CreateAsset("Drill", []string{cat.Category.ID}, nil, map[string]string{"serial": "SN-1"})
	if _, err := srv.store.CreateAsset("Hammer", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, ct := multipartBody(nil, false, map[string][]byte{"file": testJPEG(t, 20, 20)}, nil)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+drill.Asset.ID+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.pdf?columns=name,field.serial,code.qr", "")
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) || bytes.Contains(rec.Body.Bytes(), []byte("/Subtype /Image")) ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="assets-`) {
		t.Fatalf("plain: %d %s", rec.Code, rec.Header())
	}
	// The photo and the label, each an image, only when asked for.
	for query, images := range map[string]int{"photos=1": 1, "labels=1": 1, "photos=1&labels=1": 2} {
		rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.pdf?"+query, "")
		if got := bytes.Count(rec.Body.Bytes(), []byte("/Subtype /Image")); rec.Code != http.StatusOK || got != images {
			t.Errorf("%s: %d, %d images, want %d", query, rec.Code, got, images)
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.pdf?columns=name,nonsense", ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "nonsense is not a column") {
		t.Errorf("an unknown column: %d %s", rec.Code, rec.Body)
	}
	nobody, _ := srv.store.CreateUser("nobody@test.org", "NobodySecret123!", true)
	if rec := doJSON(t, srv, sessionCookie(t, srv, nobody.ID), http.MethodGet, "/api/v1/assets/export.pdf", ""); rec.Code != http.StatusForbidden {
		t.Errorf("without assets.read: %d", rec.Code)
	}
}

func TestPicturesAreDrawnForAtMostSoManyRows(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	for i := 0; i <= maxPictureRows; i++ {
		if _, err := srv.store.CreateAsset("Washer "+string(rune('A'+i%26))+strings.Repeat("x", i/26), nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.pdf?photos=1", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("with photos: %d", rec.Code)
	}
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.pdf", "")
	if rec.Code != http.StatusOK || pdfPages(rec.Body.Bytes()) < 10 {
		t.Errorf("without: %d, %d pages", rec.Code, pdfPages(rec.Body.Bytes()))
	}
}

func TestDocumentSettings(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/settings/documents", ""); !strings.Contains(rec.Body.String(), `"instance_name":"BAM!","page_size":"letter"`) {
		t.Errorf("defaults: %s", rec.Body)
	}
	for body, want := range map[string]int{
		`{"instance_name":"","page_size":"a4"}`:        http.StatusBadRequest,
		`{"instance_name":"Shop","page_size":"legal"}`: http.StatusBadRequest,
		`{"instance_name":" Shop ","page_size":"A4"}`:  http.StatusOK,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/documents", body); rec.Code != want {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/settings/documents", ""); !strings.Contains(rec.Body.String(), `"instance_name":"Shop","page_size":"a4"`) {
		t.Errorf("saved: %s", rec.Body)
	}
}

func TestPDFsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	if _, err := srv.store.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead))
	res := callTool(t, cs, "assets_pdf", map[string]any{"columns": []string{"name"}, "page_size": "a4", "orientation": "landscape"})
	var blob []byte
	for _, c := range res.Content {
		if e, ok := c.(*mcp.EmbeddedResource); ok && e.Resource.MIMEType == "application/pdf" {
			blob = e.Resource.Blob
		}
	}
	if res.IsError || !bytes.HasPrefix(blob, []byte("%PDF")) || !bytes.Contains(blob, []byte("/MediaBox [0 0 841.9 595.3]")) {
		t.Errorf("assets_pdf: %s, %d bytes", toolText(res), len(blob))
	}
	if res := callTool(t, cs, "report_pdf", map[string]any{"id": "missing"}); !res.IsError {
		t.Error("a report that is not there made a PDF")
	}
}
