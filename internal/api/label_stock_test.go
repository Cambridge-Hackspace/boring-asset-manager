package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestLabelStockThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/label-stock", `{"name":"Bin labels","width_mm":60,"length_mm":40,"gap_mm":2.5}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Stock models.LabelStock }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Stock.ID

	sink := newFakeQL(t)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"127.0.0.1:`+strconv.Itoa(sink.port())+`"},"media":"`+id+`"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"custom":true`) {
		t.Fatalf("a printer with custom stock: %d %s", rec.Code, rec.Body)
	}
	var printer struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &printer)

	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"a taken name", http.MethodPost, "/api/v1/label-stock", `{"name":"Bin labels","width_mm":50}`, http.StatusConflict},
		{"too narrow", http.MethodPost, "/api/v1/label-stock", `{"name":"Thin","width_mm":1}`, http.StatusBadRequest},
		{"too wide for the Rollo", http.MethodPut, "/api/v1/label-stock/" + id, `{"name":"Bin labels","width_mm":150,"length_mm":40}`, http.StatusBadRequest},
		{"on a Brother", http.MethodPost, "/api/v1/printers", `{"name":"QL","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"10.0.0.9"},"media":"` + id + `"}`, http.StatusBadRequest},
		{"deleting loaded stock", http.MethodDelete, "/api/v1/label-stock/" + id, ``, http.StatusConflict},
		{"updating a missing", http.MethodPut, "/api/v1/label-stock/nope", `{"name":"X","width_mm":50}`, http.StatusNotFound},
		{"deleting a missing", http.MethodDelete, "/api/v1/label-stock/nope", ``, http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}

	fit := func(body string) string {
		return doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printer.Printer.ID+"/fit", body).Body.String()
	}
	if got := fit(`{"width_mm":60,"height_mm":40,"dpi":203}`); !strings.Contains(got, `"fits":true`) {
		t.Errorf("the fit check: %s", got)
	}
	if got := fit(`{"width_mm":62,"height_mm":29,"dpi":203}`); !strings.Contains(got, `"fits":false`) || !strings.Contains(got, "takes 60 × 40 mm") {
		t.Errorf("a size it does not take: %s", got)
	}

	// Labels printed on it are sent at its size and gap.
	tag := createTemplate(t, srv, cookie, `{"name":"Bin tag","units":"mm","width":60,"height":40,"dpi":203,"elements":[
	  {"type":"text","x":2,"y":2,"width":56,"height":10,"font_size":14,"content_binding":"{{asset.name}}"}]}`)
	asset, _ := srv.store.CreateAsset("Resistors", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printer.Printer.ID+"/print", `{"template_id":"`+tag+`","asset_ids":["`+asset.Asset.ID+`"],"copies":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("printing: %d %s", rec.Code, rec.Body)
	}
	jobs := sink.waitForJobs(t, 1, 3*time.Second)
	if len(jobs) != 1 {
		t.Fatalf("the printer received %d jobs", len(jobs))
	}
	job, err := tspl.DecodeJob(jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	if setup := strings.Join(job.Setup, "; "); !strings.Contains(setup, "SIZE 60 mm,40 mm") || !strings.Contains(setup, "GAP 2.5 mm,0 mm") {
		t.Errorf("setup = %s", setup)
	}

	// Those who print see stock and the drivers' limits; only those who
	// manage settings change it.
	printing := userWithPermissions(t, srv, "printer@test.org", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, printing, http.MethodGet, "/api/v1/label-stock", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"printers":["Rollo"]`) {
		t.Errorf("listing as a printer: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, printing, http.MethodGet, "/api/v1/printers/drivers", ""); !strings.Contains(rec.Body.String(), `"custom_stock":{"max_width_mm":{`) {
		t.Errorf("drivers: %s", rec.Body)
	}
	if rec := doJSON(t, srv, printing, http.MethodPost, "/api/v1/label-stock", `{"name":"Mine","width_mm":50}`); rec.Code != http.StatusForbidden {
		t.Errorf("creating as a printer: %d", rec.Code)
	}
}
