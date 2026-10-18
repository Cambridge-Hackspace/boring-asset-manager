package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestChangingARollSaysWhatItAffects(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a := namedPrinter(t, srv, cookie, "A", 9100, "dk-11201")
	b := namedPrinter(t, srv, cookie, "B", 9100, "dk-11201")
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printer-pools", `{"name":"Bench","members":[{"printer_id":"`+a+`"},{"printer_id":"`+b+`"}]}`)
	var pool struct{ Pool struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &pool)
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools","color_hex":"#1d4ed8"}`)
	var cat struct{ Category struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat.Category.ID+"/label-template", `{"label_template_id":"`+ql90+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("naming the category's template: %d %s", rec.Code, rec.Body)
	}
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	saw, _ := srv.store.CreateAsset("Saw", nil, nil, nil)
	spoolTo(t, srv, cookie, a, ql90, 2, drill.Asset.ID)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+saw.Asset.ID+`"],"template_id":"`+ql90+`","destination":"pool:`+pool.Pool.ID+`"}`); rec.Code != http.StatusCreated {
		t.Fatalf("spooling for the pool: %d %s", rec.Code, rec.Body)
	}

	change := func(id, name, media string) PrinterChanges {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+id+"/changes", `{"name":"`+name+`","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:9100"},"media":"`+media+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("changes: %d %s", rec.Code, rec.Body)
		}
		var resp struct{ Changes PrinterChanges }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Changes
	}

	// Nothing changes, nothing is affected.
	if c := change(a, "A", "dk-11201"); !c.Empty() {
		t.Errorf("keeping the roll: %+v", c)
	}
	// Loading 62 mm tape in A strands its own labels, and sets the pool
	// apart; B still takes the pool's labels and the category's template.
	c := change(a, "A", "dk-22205")
	if len(c.Stranded) != 1 || c.Stranded[0].Destination != "printer:"+a || c.Stranded[0].Labels != 2 || c.Stranded[0].TemplateName != "QL 90x29" || c.Stranded[0].Target != "A" {
		t.Errorf("stranded: %+v", c.Stranded)
	}
	if len(c.Categories) != 0 || len(c.Pools) != 1 || c.Pools[0].Name != "Bench" {
		t.Errorf("categories %+v, pools %+v", c.Categories, c.Pools)
	}
	// Saving says the same.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/printers/"+a, `{"name":"A","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:9100"},"media":"dk-22205"}`)
	var saved struct{ Changes PrinterChanges }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusOK || len(saved.Changes.Stranded) != 1 || len(saved.Changes.Pools) != 1 {
		t.Errorf("saving: %d %s", rec.Code, rec.Body)
	}

	// Then loading it in B too strands the pool's labels and leaves the
	// category's template with no printer.
	c = change(b, "B", "dk-22205")
	if len(c.Stranded) != 1 || c.Stranded[0].Destination != "pool:"+pool.Pool.ID || c.Stranded[0].Target != "Bench" || c.Stranded[0].Labels != 1 {
		t.Errorf("stranded in the pool: %+v", c.Stranded)
	}
	if len(c.Categories) != 1 || c.Categories[0].Name != "Tools" || len(c.Pools) != 0 {
		t.Errorf("categories %+v, pools %+v", c.Categories, c.Pools)
	}

	// The template list says which printers suit each template.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates", "")
	var list struct{ Suits map[string][]SpoolChoice }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if s := list.Suits[ql90]; len(s) != 1 || s[0].Name != "B" {
		t.Errorf("suits: %+v", list.Suits)
	}

	for _, c := range []struct {
		name, id, body string
		want           int
	}{
		{"a missing printer", "nope", `{"name":"X","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"10.0.0.1"},"media":"dk-22205"}`, http.StatusNotFound},
		{"a roll it does not take", a, `{"name":"A","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"10.0.0.1"},"media":"4x6"}`, http.StatusBadRequest},
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+c.id+"/changes", c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}

	// Only those who manage printers ask; those who only read assets are
	// not told which printers suit templates.
	printing := userWithPermissions(t, srv, "printer@test.org", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, printing, http.MethodPost, "/api/v1/printers/"+a+"/changes", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("asking as a printer: %d", rec.Code)
	}
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/templates", ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"suits"`) {
		t.Errorf("listing templates as a reader: %d %s", rec.Code, rec.Body)
	}
}
