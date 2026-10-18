package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

func TestTheEditorTestPrintsATemplateNotYetSaved(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	fake := newFakeQL(t)
	ql := createPrinter(t, srv, cookie, fake.port(), "dk-22205")
	const doc = `{"units":"mm","width":62,"height":29,"dpi":300,"elements":[
	  {"type":"text","x":2,"y":2,"width":58,"height":10,"font_size":12,"content_binding":"{{asset.name}}"}]}`

	// With placeholders: one label, nothing recorded.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+ql+"/test-print", `{"template":`+doc+`}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"labels":1`) {
		t.Fatalf("test print: %d %s", rec.Code, rec.Body)
	}
	pages, err := brotherql.DecodeJob(fake.waitForJobs(t, 1, 3*time.Second)[0])
	if err != nil || len(pages) != 1 {
		t.Errorf("the job: %v, %d pages", err, len(pages))
	}

	// For an asset, which its history does not record.
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+ql+"/test-print", `{"template":`+doc+`,"asset_id":"`+asset.Asset.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("test print for an asset: %d %s", rec.Code, rec.Body)
	}
	fake.waitForJobs(t, 2, 3*time.Second)
	events, _, _ := srv.store.ListAssetEvents(asset.Asset.ID, "", 50)
	for _, e := range events {
		if e.Kind == models.EventAssetLabelPrinted {
			t.Errorf("a test print was recorded: %+v", e)
		}
	}

	// A template the printer does not take says why.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+ql+"/test-print", `{"template":{"name":"Shipping","units":"in","width":4,"height":6,"dpi":203,"elements":[]}}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Shipping can't print on QL dk-22205: it is laid out at 203 DPI") {
		t.Errorf("an unsuited template: %d %s", rec.Code, rec.Body)
	}

	// It takes both designing templates and printing labels.
	for _, perms := range [][]string{{"labels.print"}, {"templates.manage"}} {
		other := userWithPermissions(t, srv, perms[0]+"@example.com", perms...)
		if rec := doJSON(t, srv, other, http.MethodPost, "/api/v1/printers/"+ql+"/test-print", `{"template":`+doc+`}`); rec.Code != http.StatusForbidden {
			t.Errorf("with only %v: %d", perms, rec.Code)
		}
	}
}

func TestTheFitCheckSaysWhereThePrinterPrints(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := createPrinter(t, srv, cookie, 9100, "dk-22205")
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+ql+"/fit",
		`{"width_mm":62,"height_mm":29,"dpi":300,"content":[{"x":0,"y":0,"width":10,"height":10},{"x":10,"y":5,"width":20,"height":10}]}`)
	var fit struct {
		Fits      bool
		Printable struct{ X, Y, Width, Height float64 }
		Outside   []int
		NearEdge  []int `json:"near_edge"`
		EdgeMM    float64
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &fit)
	if !fit.Fits || fit.Printable.X <= 0 || fit.Printable.Height != 29 || len(fit.Outside) != 1 || fit.Outside[0] != 0 || fit.NearEdge == nil {
		t.Errorf("the fit check: %s", rec.Body)
	}
}
