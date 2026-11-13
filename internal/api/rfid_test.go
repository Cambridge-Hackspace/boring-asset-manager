package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestRFIDSettings(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/settings/rfid", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scheme":"text"`) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/rfid", `{"scheme":"giai-96","company_prefix":"0614141","filter":3,"code_type":"code128"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"company_prefix":"0614141","filter":3,"code_type":"code128"`) {
		t.Fatalf("saving: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{
		`{"scheme":"sgtin-96"}`,
		`{"scheme":"giai-96","company_prefix":"123"}`,
		`{"scheme":"giai-202","company_prefix":"0614141","filter":9}`,
		`{"scheme":"text","code_type":"aztec"}`,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/rfid", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	// The text scheme keeps no prefix.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/rfid", `{"scheme":"text","company_prefix":"0614141","filter":3}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"company_prefix":"","filter":0`) {
		t.Errorf("text: %d %s", rec.Code, rec.Body)
	}
	nobody := userWithPermissions(t, srv, "nobody@example.com")
	if rec := doJSON(t, srv, nobody, http.MethodGet, "/api/v1/settings/rfid", ""); rec.Code != http.StatusForbidden {
		t.Errorf("without settings.manage: %d", rec.Code)
	}
}

// A ZPL printer set to write tags writes each label's tag with its
// asset's EPC; one with no code, or one the scheme cannot write, stops the
// job before anything is sent.
func TestPrintWritesEachLabelsRFIDTag(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	zt := newFakeQL(t)
	body := `{"name":"ZT411 RFID","driver":"zpl","model":"Zebra ZT411 203 DPI","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(zt.port()) +
		`"},"media":"2x1","options":{"rfid":true,"rfid_on_failure":"pause"}}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	tmpl := createTemplate(t, srv, cookie, `{"id":"zt2x1","name":"ZT 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"text","x":0.1,"y":0.2,"width":1.8,"height":0.4,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	drill, _ := srv.store.CreateAsset("Drill", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "HS-105"}}, nil)
	saw, _ := srv.store.CreateAsset("Saw", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "4711"}, {GeneratorType: "code128", AssignedCode: "SAW"}}, nil)
	bare, _ := srv.store.CreateAsset("Bare", nil, nil, nil)
	printIDs := func(ids ...string) *httptest.ResponseRecorder {
		return doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print",
			`{"template_id":"`+tmpl+`","asset_ids":["`+strings.Join(ids, `","`)+`"]}`)
	}

	// The text scheme, by default, from each asset's first code.
	if rec := printIDs(drill.Asset.ID, saw.Asset.ID); rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body)
	}
	job := string(zt.waitForJobs(t, 1, 3*time.Second)[0])
	for _, want := range []string{"^RS8,,,,P\n^RFW,H,,,A^FD48532D313035^FS", "^RS8,,,,P\n^RFW,H,,,A^FD34373131^FS"} {
		if !strings.Contains(job, want) {
			t.Errorf("job lacks %q", want)
		}
	}

	if rec := printIDs(drill.Asset.ID, bare.Asset.ID); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Bare has no code to write to its RFID tag") {
		t.Errorf("an asset with no code: %d %s", rec.Code, rec.Body)
	}
	// GIAI-96 writes only numbers: the saw's QR code is one, the drill's
	// code is not.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/rfid", `{"scheme":"giai-96","company_prefix":"000123456789"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := printIDs(drill.Asset.ID); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Drill's RFID tag: GIAI-96 writes only codes that are whole numbers") {
		t.Errorf("a code GIAI-96 cannot write: %d %s", rec.Code, rec.Body)
	}
	if rec := printIDs(saw.Asset.ID); rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body)
	}
	jobs := zt.waitForJobs(t, 2, 3*time.Second)
	if want := "^RFW,H^FD3400001D6F34540000001267^FS"; !strings.Contains(string(jobs[1]), want) {
		t.Errorf("GIAI-96 job lacks %q:\n%s", want, jobs[1][:min(len(jobs[1]), 300)])
	}
	if len(jobs) != 2 {
		t.Errorf("%d jobs sent", len(jobs))
	}

	// A reader typing the saw's EPC finds it.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/scan/3400001D6F34540000001267", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Saw"`) {
		t.Errorf("scanning the EPC: %d %s", rec.Code, rec.Body)
	}
	// A code type chooses which code a tag holds.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/rfid", `{"scheme":"text","code_type":"code128"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := printIDs(saw.Asset.ID); rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body)
	}
	if job := string(zt.waitForJobs(t, 3, 3*time.Second)[2]); !strings.Contains(job, "^FD53415700^FS") {
		t.Errorf("the saw's Code 128 code was not written")
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/scan/48532d313035", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Drill"`) {
		t.Errorf("scanning a text EPC in lowercase: %d %s", rec.Code, rec.Body)
	}
}

// From the print queue, a label whose tag cannot be written stays, saying
// why, and the rest print with theirs.
func TestSpooledLabelsWriteTheirRFIDTags(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	zt := newFakeQL(t)
	body := `{"name":"ZD621R","driver":"zpl","model":"Zebra ZD621 203 DPI","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(zt.port()) +
		`"},"media":"2x1","options":{"rfid":true}}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	tmpl := createTemplate(t, srv, cookie, `{"id":"zd2x1","name":"ZD 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"text","x":0.1,"y":0.2,"width":1.8,"height":0.4,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	drill, _ := srv.store.CreateAsset("Drill", nil, []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "HS-105"}}, nil)
	bare, _ := srv.store.CreateAsset("Bare", nil, nil, nil)
	added := spoolTo(t, srv, cookie, created.Printer.ID, tmpl, 1, drill.Asset.ID, bare.Asset.ID)
	printSpool(t, srv, cookie, `{}`, http.StatusCreated)
	srv.drainSpool(context.Background())
	jobs := zt.waitForJobs(t, 1, 3*time.Second)
	if len(jobs) != 1 || !strings.Contains(string(jobs[0]), "^RS8\n^RFW,H,,,A^FD48532D313035^FS") || strings.Count(string(jobs[0]), "^XA") != 1 {
		t.Fatalf("jobs: %q", jobs)
	}
	found := false
	for _, e := range spoolList(t, srv, cookie, "").Entries {
		if e.ID == added[1].ID {
			found = true
			if len(e.Problems) != 1 || e.Problems[0] != "Could not be produced: Bare has no code to write to its RFID tag" {
				t.Errorf("the bare asset's label: %+v", e)
			}
		}
	}
	if !found {
		t.Error("the bare asset's label left the queue")
	}
}
