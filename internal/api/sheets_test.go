package api

import (
	"bytes"
	"encoding/json"
	"image"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func images(pdf []byte) int { return bytes.Count(pdf, []byte("/Subtype /Image")) }

func TestAnAssetSheet(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	nothingPublic(t, srv)
	cookie := sessionCookie(t, srv, admin.ID)
	tag := createTemplate(t, srv, cookie, `{"name":"Tag","units":"mm","width":40,"height":20,"dpi":203,"elements":[
	  {"type":"text","x":2,"y":2,"width":36,"height":10,"font_size":10,"content_binding":"{{asset.name}}"}]}`)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools"}`)
	var cat struct{ Category struct{ ID string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat.Category.ID+"/label-template", `{"label_template_ids":["`+tag+`"]}`)
	drill, err := srv.store.CreateAsset("Cordless drill", []string{cat.Category.ID},
		[]models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "TOOL-1"}, {GeneratorType: "code128", AssignedCode: "TOOL-0001"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, ct := multipartBody(nil, false, map[string][]byte{"file": testJPEG(t, 20, 20)}, nil)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+drill.Asset.ID+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+drill.Asset.ID+"/sheet.pdf", "")
	pdf := rec.Body.Bytes()
	if rec.Code != http.StatusOK || !bytes.HasPrefix(pdf, []byte("%PDF")) ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="Cordless-drill.pdf"` {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	// Its photo, its two codes drawn, and its label: four pictures, on one
	// page.
	if got := images(pdf); got != 4 || pdfPages(pdf) != 1 {
		t.Errorf("%d pictures on %d pages", got, pdfPages(pdf))
	}

	// One with nothing to draw has none.
	hammer, _ := srv.store.CreateAsset("Hammer", nil, nil, nil)
	if pdf := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+hammer.Asset.ID+"/sheet.pdf", "").Body.Bytes(); images(pdf) != 0 {
		t.Errorf("a plain asset's sheet has %d pictures", images(pdf))
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/missing/sheet.pdf", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}

	// Many at once: a page each, for the list's filters.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/sheets.pdf", "")
	if rec.Code != http.StatusOK || pdfPages(rec.Body.Bytes()) != 2 || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="asset-sheets-`) {
		t.Errorf("sheets: %d, %d pages", rec.Code, pdfPages(rec.Body.Bytes()))
	}
	if pdf := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/sheets.pdf?search=hammer", "").Body.Bytes(); pdfPages(pdf) != 1 || images(pdf) != 0 {
		t.Errorf("searched: %d pages, %d pictures", pdfPages(pdf), images(pdf))
	}
}

func TestSheetsOnlyOfWhatSomeoneMaySee(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	nothingPublic(t, srv)
	st := srv.store
	scope, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(tools.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	drill, _ := st.CreateAsset("Drill", []string{tools.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "D-1"}}, nil)
	kettle, _ := st.CreateAsset("Kettle", []string{kitchen.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "K-1"}}, nil)

	reader := scopedReader(t, srv, "workshop")
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/"+kettle.Asset.ID+"/sheet.pdf", ""); rec.Code != http.StatusNotFound {
		t.Errorf("another scope's asset: %d", rec.Code)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/"+drill.Asset.ID+"/sheet.pdf", ""); rec.Code != http.StatusOK {
		t.Errorf("their own scope's: %d", rec.Code)
	}
	// The list's sheets hold theirs alone: one page, one code drawn.
	pdf := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets/sheets.pdf", "").Body.Bytes()
	if pdfPages(pdf) != 1 || images(pdf) != 1 {
		t.Errorf("the list's sheets: %d pages, %d pictures", pdfPages(pdf), images(pdf))
	}
	admins := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/assets/sheets.pdf", "").Body.Bytes()
	if pdfPages(admins) != 2 {
		t.Errorf("an admin's sheets: %d pages", pdfPages(admins))
	}
	nobody, _ := st.CreateUser("nobody@test.org", "NobodySecret123!", true)
	for _, path := range []string{"/api/v1/assets/sheets.pdf", "/api/v1/assets/stocktake.pdf", "/api/v1/assets/" + drill.Asset.ID + "/sheet.pdf"} {
		if rec := doJSON(t, srv, sessionCookie(t, srv, nobody.ID), http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s without assets.read: %d", path, rec.Code)
		}
	}
}

func located(name string, places ...string) models.AssetWithDetails {
	a := models.AssetWithDetails{Asset: models.Asset{Name: name}}
	for _, p := range places {
		a.Location = append(a.Location, models.LinkedAsset{Name: p})
	}
	return a
}

func TestAStocktakeIsGroupedByPlaceInOrder(t *testing.T) {
	counted := located("M3 screws", "Workshop", "Drawer 2")
	counted.Asset.Stock = &models.AssetStock{Quantity: 140, Unit: "pcs"}
	counted.GeneratorKeys = []models.AssetGeneratorKey{{AssignedCode: "SCR-3"}}
	table := stocktakeTable([]models.AssetWithDetails{
		located("Kettle", "Kitchen"),
		located("Saw", "workshop"),
		located("Loose tape"),
		counted,
		located("Drill", "Workshop"),
		located("Anvil", "Workshop"),
	})
	if !table.Checkbox || strings.Join(table.Columns, ",") != "Asset,Codes,Expected,Counted" {
		t.Fatalf("%+v", table)
	}
	var got []string
	for _, g := range table.Groups {
		var names []string
		for _, r := range g.Rows {
			names = append(names, r.Cells[0])
		}
		got = append(got, g.Title+": "+strings.Join(names, ", "))
	}
	// Places in order, outermost first, a place before those in it, and
	// none last; assets by name; places named alike kept apart only by
	// case.
	want := []string{
		"Kitchen: Kettle",
		"Workshop: Anvil, Drill",
		"workshop: Saw",
		"Workshop › Drawer 2: M3 screws",
		"Not in a place: Loose tape",
	}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("groups:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// What is expected: the quantity counted, else one; the count blank.
	screws := table.Groups[3].Rows[0].Cells
	if screws[1] != "SCR-3" || screws[2] != "140 pcs" || screws[3] != "" || table.Groups[0].Rows[0].Cells[2] != "1" {
		t.Errorf("cells %q", screws)
	}
}

func TestAStocktakeThroughTheAPIAndMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	for _, n := range []string{"Drill", "Saw"} {
		if _, err := srv.store.CreateAsset(n, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/stocktake.pdf?page_size=a4", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("/MediaBox [0 0 595.3 841.9]")) ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="stocktake-`+time.Now().UTC().Format("20060102")+`.pdf"` {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}

	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Reader", rbac.PermAssetsRead))
	for tool, args := range map[string]map[string]any{
		"stocktake_pdf":    {"search": "saw"},
		"asset_sheets_pdf": {"search": "drill", "orientation": "landscape"},
	} {
		res := callTool(t, cs, tool, args)
		var blob []byte
		for _, c := range res.Content {
			if e, ok := c.(*mcp.EmbeddedResource); ok && e.Resource.MIMEType == "application/pdf" {
				blob = e.Resource.Blob
			}
		}
		if res.IsError || !bytes.HasPrefix(blob, []byte("%PDF")) {
			t.Errorf("%s: %s", tool, toolText(res))
		}
	}
	if res := callTool(t, cs, "asset_sheets_pdf", map[string]any{"id": "missing"}); !res.IsError {
		t.Error("a sheet of an asset that is not there")
	}
}

func TestCodesAreDrawnInBlocks(t *testing.T) {
	for _, k := range []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "TOOL-1"}, {GeneratorType: "code128", AssignedCode: "TOOL-0001"}} {
		img, ok := codeImage(k).(*image.Gray)
		if !ok {
			t.Fatalf("%s: no image", k.GeneratorType)
		}
		// Every module is a 4 × 4 block of one shade, so a viewer scaling
		// it up keeps its edges sharp.
		b := img.Bounds()
		if b.Dx()%4 != 0 || b.Dy()%4 != 0 {
			t.Fatalf("%s: %v", k.GeneratorType, b)
		}
		dark := 0
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				v := img.Pix[y*img.Stride+x]
				if v != img.Pix[(y/4*4)*img.Stride+x/4*4] {
					t.Fatalf("%s: (%d, %d) differs from its block", k.GeneratorType, x, y)
				}
				if v == 0 {
					dark++
				}
			}
		}
		if dark == 0 {
			t.Errorf("%s: nothing dark", k.GeneratorType)
		}
	}
	if codeImage(models.AssetGeneratorKey{GeneratorType: "nonsense", AssignedCode: "x"}) != nil {
		t.Error("an unknown symbology was drawn")
	}
}
