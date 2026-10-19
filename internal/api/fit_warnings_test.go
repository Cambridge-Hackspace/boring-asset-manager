package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// edgeTemplate is landscapeTemplate with its name at the very top of the
// label, where a QL does not print on 29 mm tape.
const edgeTemplate = `{"name":"Edge","units":"mm","width":90,"height":29,"dpi":300,"elements":[
  {"type":"generator","generator_type":"qr","x":3,"y":3,"width":23,"height":23,"content_binding":"{{base_url}}/a/{{asset.id}}"},
  {"type":"text","x":28,"y":0,"width":58,"height":10,"font_size":16,"content_binding":"{{asset.name}}"}]}`

func TestFitWarningsReachThePersonPrinting(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	edge := createTemplate(t, srv, cookie, edgeTemplate)
	clean := createTemplate(t, srv, cookie, landscapeTemplate)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)

	// The editor's fit check, with and without content where it does not print.
	var fit struct {
		Fits     bool
		Warnings []string
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/fit", `{"width_mm":90,"height_mm":29,"dpi":300,"content":[{"x":28,"y":0,"width":58,"height":10}]}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &fit)
	if !fit.Fits || len(fit.Warnings) != 1 || !strings.Contains(fit.Warnings[0], "does not print") {
		t.Errorf("fit check with content at the edge: %s", rec.Body)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/fit", `{"width_mm":90,"height_mm":29,"dpi":300}`)
	if !strings.Contains(rec.Body.String(), `"warnings":[]`) {
		t.Errorf("fit check without content: %s", rec.Body)
	}

	// Printing says so too, and prints.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+edge+`","asset_ids":["`+asset.Asset.ID+`"],"copies":1}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "does not print") {
		t.Errorf("printing: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+clean+`","asset_ids":["`+asset.Asset.ID+`"],"copies":1}`)
	if !strings.Contains(rec.Body.String(), `"warnings":[]`) {
		t.Errorf("printing a template that fits cleanly: %s", rec.Body)
	}

	// The spool lists them by template and printer, and printing from it
	// repeats them, naming the printer.
	entries := spoolTo(t, srv, cookie, printerID, edge, 1, asset.Asset.ID)
	spool := spoolList(t, srv, cookie, "")
	if w := spool.FitWarnings[edge]["printer:"+printerID]; len(w) != 1 || len(spool.FitWarnings[clean]) != 0 {
		t.Errorf("the spool's warnings: %+v", spool.FitWarnings)
	}
	if len(spool.Entries) != 1 || len(spool.Entries[0].Problems) != 0 {
		t.Errorf("a warning made a problem: %+v", spool.Entries)
	}
	// A pool's warnings name the printer each is about.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printer-pools", `{"name":"Bench","members":[{"printer_id":"`+printerID+`"}]}`)
	var pool struct{ Pool struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &pool)
	spool = spoolList(t, srv, cookie, "")
	if w := spool.FitWarnings[edge]["pool:"+pool.Pool.ID]; len(w) != 1 || !strings.HasPrefix(w[0], "QL dk-11201: ") {
		t.Errorf("the pool's warnings: %+v", spool.FitWarnings)
	}

	resp := printSpool(t, srv, cookie, `{"entry_ids":["`+entries[0].ID+`"]}`, http.StatusCreated)
	if len(resp.Jobs) != 1 || len(resp.Warnings) != 1 || !strings.HasPrefix(resp.Warnings[0], "QL dk-11201: ") {
		t.Errorf("printing from the spool: %+v", resp)
	}
}

// TestTurnedElementsAreWhereTheyLie checks a turned element where it lies
// once turned: the JSON preview gives its bounds, and a printer warns about
// it reaching where it does not print, though its box does not.
func TestTurnedElementsAreWhereTheyLie(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	// A 29 × 6 mm name in the middle of the label, turned upright: it then
	// runs from the top edge to the bottom one.
	const doc = `{"name":"Upright","units":"mm","width":90,"height":29,"dpi":300,"layering":"list","elements":[
	  {"type":"text","x":45.5,"y":11.5,"width":29,"height":6,"font_size":10,"rotation":90,"content_binding":"{{asset.name}}"}]}`

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+doc+`,"format":"json"}`)
	var desc PreviewDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &desc); err != nil || len(desc.Elements) != 1 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if e := desc.Elements[0]; e.Rotation != 90 || e.X != 45.5 || e.Bounds == nil || *e.Bounds != (PreviewBox{X: 57, Y: 0, Width: 6, Height: 29}) {
		t.Errorf("described: %+v %+v", e, e.Bounds)
	}

	for _, tc := range []struct {
		doc  string
		warn bool
	}{{doc, true}, {strings.Replace(doc, `"rotation":90,`, "", 1), false}} {
		tmpl := createTemplate(t, srv, cookie, tc.doc)
		rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+tmpl+`","asset_ids":["`+asset.Asset.ID+`"],"copies":1}`)
		if warned := strings.Contains(rec.Body.String(), "does not print"); warned != tc.warn {
			t.Errorf("turned %v: %s", tc.warn, rec.Body)
		}
	}
}

// TestPreviewDescribesCodes says how each code is laid out: its module and
// the margins drawn, warning where one is narrower than the standard.
func TestPreviewDescribesCodes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	// HELLO is a 21-module QR Code; in a 20 mm box at 300 DPI, 236 dots,
	// 29 modules high with its margins, so 8 dots each, at the box's left.
	const doc = `{"name":"Codes","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[
	  {"type":"generator","generator_type":"qr","x":0,"y":0,"width":20,"height":20,"content_binding":"HELLO","margin":{"left":0},"align":"left"},
	  {"type":"text","x":21,"y":0,"width":19,"height":10,"font_size":8,"content_binding":"A"}]}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+doc+`,"format":"json"}`)
	var desc PreviewDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &desc); err != nil || len(desc.Elements) != 2 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	c := desc.Elements[0].Code
	if c == nil || c.ModuleDots != 8 || c.Module != 0.677 || c.MarginDots.Left != 0 || c.MarginDots.Right != 236-21*8 || c.MarginDots.Top != 34 || c.Margin.Top != 2.879 ||
		len(c.Narrow) != 1 || c.Narrow[0] != "left" {
		t.Errorf("the code: %+v", c)
	}
	if desc.Elements[1].Code != nil || len(desc.Warnings) != 1 || !strings.Contains(desc.Warnings[0], "on its left side") {
		t.Errorf("warnings: %v", desc.Warnings)
	}
}
