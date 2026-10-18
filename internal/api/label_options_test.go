package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func labelOptions(t *testing.T, srv *Server, cookie *http.Cookie, assetIDs ...string) LabelOptionsResponse {
	t.Helper()
	ids, _ := json.Marshal(assetIDs)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/options", `{"asset_ids":`+string(ids)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("label options: %d %s", rec.Code, rec.Body)
	}
	var resp LabelOptionsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp
}

func TestLabelOptionsOfferWhatFitsAndSayWhyNot(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	// Named so the category's template does not come first by name.
	tag := createTemplate(t, srv, cookie, `{"name":"Tag","units":"mm","width":62,"height":29,"dpi":300,"elements":[
	  {"type":"generator","generator_type":"qr","x":2,"y":2,"width":25,"height":25}]}`)
	name := createTemplate(t, srv, cookie, `{"name":"Name","units":"mm","width":62,"height":29,"dpi":300,"elements":[
	  {"type":"text","x":2,"y":2,"width":58,"height":10,"font_size":12,"content_binding":"{{asset.name}}"}]}`)
	shipping := createTemplate(t, srv, cookie, `{"name":"Shipping","units":"in","width":4,"height":6,"dpi":203,"elements":[]}`)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools","color_hex":"#1d4ed8"}`)
	var cat struct{ Category struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat.Category.ID+"/label-template", `{"label_template_ids":["`+tag+`"]}`)
	drill, _ := srv.store.CreateAsset("Drill", []string{cat.Category.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "D1"}}, nil)
	screws, _ := srv.store.CreateAsset("Screws", nil, nil, nil)
	ql := createPrinter(t, srv, cookie, 9100, "dk-22205")

	// The drill's category's template comes first, and is where it starts.
	opts := labelOptions(t, srv, cookie, drill.Asset.ID)
	if len(opts.Templates) != 3 || opts.Templates[0].ID != tag || !opts.Templates[0].Preferred || opts.Templates[1].Preferred || opts.TemplateID != tag {
		t.Errorf("the drill's templates: %+v, starting with %s", opts.Templates, opts.TemplateID)
	}
	if f := opts.Fits[tag][ql]; !f.Fits || f.Reason != "" {
		t.Errorf("the tag on the QL: %+v", f)
	}
	// Shipping labels do not fit, for every reason at once.
	if f := opts.Fits[shipping][ql]; f.Fits || f.Reason != "Shipping can't print on QL dk-22205: it is laid out at 203 DPI, but Brother QL printers print at 300 DPI; "+
		"it is 101.6 × 152.4 mm, but the roll loaded, DK-22205 62 mm continuous, needs one side to be 62 mm." {
		t.Errorf("shipping on the QL: %+v", f)
	}

	// The screws have no code, so the tag cannot label them: it says so,
	// and starts with one that can.
	opts = labelOptions(t, srv, cookie, screws.Asset.ID)
	for _, o := range opts.Templates {
		want := 0
		if o.ID == tag {
			want = 1
		}
		if len(o.Problems) != want {
			t.Errorf("%s's problems with the screws: %+v", o.Name, o.Problems)
		}
		if o.ID == tag && o.Problems[0].Message != "This template prints a QR Code, and Screws has none." {
			t.Errorf("the tag's problem: %+v", o.Problems)
		}
	}
	if opts.TemplateID != name {
		t.Errorf("for the screws it starts with %s, not the name (%s)", opts.TemplateID, name)
	}

	// Someone who cannot print is offered templates, not printers.
	reader := userWithPermissions(t, srv, "reader@example.com", "assets.read")
	opts = labelOptions(t, srv, reader, drill.Asset.ID)
	if len(opts.Templates) != 3 || len(opts.Printers) != 0 || len(opts.Fits) != 0 || opts.TemplateID != tag {
		t.Errorf("for a reader: %+v", opts)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/options", `{"asset_ids":[]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no assets: %d", rec.Code)
	}
}

// An adaptive template planFor cannot lay out for a printer once named no
// template, and the spooler then failed on it.
func TestTheSpoolerSaysWhyAnAdaptiveTemplateNoLongerFits(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tag := createTemplate(t, srv, cookie, adaptiveTemplate)
	fake := newFakeQL(t)
	qlID := createPrinter(t, srv, cookie, fake.port(), "dk-22205")
	asset, _ := srv.store.CreateAsset("Drill", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "D1"}}, nil)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+asset.Asset.ID+`"],"template_id":"`+tag+`","destination":"printer:`+qlID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	printSpool(t, srv, cookie, `{}`, http.StatusCreated)

	// 12 mm tape is loaded before the job prints.
	p, err := srv.store.GetPrinter(qlID)
	if err != nil {
		t.Fatal(err)
	}
	p.Media = "dk-22214"
	if _, err := srv.store.UpdatePrinter(*p); err != nil {
		t.Fatal(err)
	}
	srv.drainSpool(context.Background())

	entries := spoolList(t, srv, cookie, "").Entries
	if len(entries) != 1 || !strings.Contains(entries[0].Problem, "Any tag can't print on QL dk-22205: it does not adapt to DK-22214 12 mm continuous") {
		t.Errorf("after printing on 12 mm tape: %+v", entries)
	}
}
