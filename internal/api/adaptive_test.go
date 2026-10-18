package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
)

// adaptiveTemplate is a 62 × 29 mm tag that adapts from 25 to 102 mm by 20
// to 100 mm: its code pinned to the left, its name stretching beside it.
const adaptiveTemplate = `{"name":"Any tag","units":"mm","width":62,"height":29,"dpi":300,"sizing":"adaptive",
  "adapt":{"min_width":25,"max_width":102,"min_height":20,"max_height":100},"elements":[
  {"type":"generator","generator_type":"qr","x":3,"y":3,"width":23,"height":23,"anchor_x":"start","anchor_y":"start"},
  {"type":"text","x":28,"y":4,"width":31,"height":10,"font_size":12,"min_font_size":6,"content_binding":"{{asset.name}}","anchor_x":"both","anchor_y":"start"}]}`

func TestAnAdaptiveTemplatePrintsOnEveryRollItTakes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tag := createTemplate(t, srv, cookie, adaptiveTemplate)
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates/"+tag, "")
	if !strings.Contains(rec.Body.String(), `"sizing":"adaptive"`) || !strings.Contains(rec.Body.String(), `"continuous_length":"proportional"`) || !strings.Contains(rec.Body.String(), `"anchor_x":"both"`) {
		t.Fatalf("the template as saved: %s", rec.Body)
	}
	asset, _ := srv.store.CreateAsset("Drill", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "BIN-1"}}, nil)

	// On 29 × 90 mm die-cut labels, 62 mm tape, and a 203 DPI Rollo.
	dieCut := newFakeQL(t)
	tape := newFakeQL(t)
	rollo := newFakeQL(t)
	dieCutID := createPrinter(t, srv, cookie, dieCut.port(), "dk-11201")
	tapeID := createPrinter(t, srv, cookie, tape.port(), "dk-22205")
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"127.0.0.1:`+strconv.Itoa(rollo.port())+`"},"media":"3x2"}`)
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	rolloID := created.Printer.ID

	print := func(printerID string) {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+tag+`","asset_ids":["`+asset.Asset.ID+`"],"copies":1}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("printing on %s: %d %s", printerID, rec.Code, rec.Body)
		}
	}
	print(dieCutID)
	pages, err := brotherql.DecodeJob(dieCut.waitForJobs(t, 1, 3*time.Second)[0])
	// 90 × 29 mm, turned onto the 29 mm tape: 991 lines of the label.
	if err != nil || len(pages) != 1 || len(pages[0].Lines) != 991 {
		t.Errorf("on die-cut labels: %v, %d pages", err, len(pages))
	}
	print(tapeID)
	pages, err = brotherql.DecodeJob(tape.waitForJobs(t, 1, 3*time.Second)[0])
	// 62 mm across and, in proportion, 29 mm long: 343 lines.
	if err != nil || len(pages) != 1 || len(pages[0].Lines) != 343 {
		t.Errorf("on tape: %v, %d lines", err, len(pages[0].Lines))
	}
	print(rolloID)
	job, err := tspl.DecodeJob(rollo.waitForJobs(t, 1, 3*time.Second)[0])
	// 76.2 × 50.8 mm at the Rollo's 203 DPI: 609 × 406 dots, sent in whole
	// bytes, so 616 wide.
	if err != nil || len(job.Labels) != 1 || job.Labels[0].Image.Bounds().Dx() != 616 || job.Labels[0].Image.Bounds().Dy() != 406 {
		t.Errorf("on the Rollo: %v, %+v", err, job.Labels)
	}

	// The spool and the template list know it suits all three.
	spool := spoolList(t, srv, cookie, "")
	if fits := spool.Fits[tag]; len(fits) != 3 {
		t.Errorf("the spool's fits: %v", fits)
	}

	// 12 mm tape is narrower than it takes; saying why.
	narrow := createPrinter(t, srv, cookie, 9100, "dk-22214")
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+narrow+"/print", `{"template_id":"`+tag+`","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Any tag does not adapt to DK-22214 12 mm continuous") || !strings.Contains(rec.Body.String(), "it takes labels 25-102") {
		t.Errorf("on 12 mm tape: %d %s", rec.Code, rec.Body)
	}

	// A small code box holds the longest code assigned at 300 DPI but not
	// at the Rollo's 203, so the template stays off the Rollo.
	small := createTemplate(t, srv, cookie, `{"name":"Small code","units":"mm","width":62,"height":29,"dpi":300,"sizing":"adaptive",
	  "adapt":{"min_width":25,"max_width":102,"min_height":20,"max_height":100},"elements":[
	  {"type":"generator","generator_type":"qr","x":2,"y":2,"width":10,"height":10,"anchor_x":"start","anchor_y":"start"}]}`)
	if fits := spoolList(t, srv, cookie, "").Fits[small]; len(fits) != 3 {
		t.Errorf("with short codes, the small code's fits: %v", fits)
	}
	srv.store.CreateAsset("Reel", nil, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: strings.Repeat("reel-00123-", 6)}}, nil)
	srv.sample = sampleCache{}
	fits := spoolList(t, srv, cookie, "").Fits[small]
	if len(fits) != 2 || fits[0] == rolloID || fits[1] == rolloID {
		t.Errorf("with a long code, the small code's fits: %v (the Rollo is %s)", fits, rolloID)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+rolloID+"/print", `{"template_id":"`+small+`","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "at 203 DPI, for the code") {
		t.Errorf("printing it on the Rollo: %d %s", rec.Code, rec.Body)
	}
}

func TestAFixedTemplateStaysAtItsOwnDPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	fixed := createTemplate(t, srv, cookie, `{"name":"Fixed 2x1","units":"in","width":2,"height":1,"dpi":300,"elements":[]}`)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"10.0.0.1"},"media":"2x1"}`)
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+fixed+`","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "prints at 203 DPI, not 300") {
		t.Errorf("a 300 DPI fixed template on a 203 DPI printer: %d %s", rec.Code, rec.Body)
	}
}
