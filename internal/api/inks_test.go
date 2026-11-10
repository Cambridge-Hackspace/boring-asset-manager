package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
)

// redTemplate is a 62 × 29 mm label of black text and a red bar.
const redTemplate = `{"name":"Red","units":"mm","width":62,"height":29,"dpi":300,"layering":"list","elements":[
  {"type":"text","x":4,"y":2,"width":54,"height":10,"font_size":16,"content_binding":"{{asset.name}}"},
  {"type":"shape","shape":"rectangle","x":4,"y":16,"width":54,"height":8,"stroke":"none","fill":"red"}]}`

// TestPrintingInRedAndBlack prints a template in red and black on a QL
// with DK-22251 loaded, and in black alone, with a warning, on one with
// black-and-white tape.
func TestPrintingInRedAndBlack(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	fake := newFakeQL(t)
	red := createPrinter(t, srv, cookie, fake.port(), "dk-22251")
	plain := createPrinter(t, srv, cookie, 9100, "dk-22205")

	// The printer says what it prints in.
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+red, "")
	if !strings.Contains(rec.Body.String(), `"palette":"bwr"`) {
		t.Errorf("the printer: %s", rec.Body)
	}

	// The editor's fit check: the palette, and a warning of an ink it lacks.
	var fit struct {
		Fits     bool
		Palette  string
		Warnings []string
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+red+"/fit", `{"width_mm":62,"height_mm":29,"dpi":300,"inks":["black","red"]}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &fit)
	if !fit.Fits || fit.Palette != "bwr" || len(fit.Warnings) != 0 {
		t.Errorf("fit in red: %s", rec.Body)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+plain+"/fit", `{"width_mm":62,"height_mm":29,"dpi":300,"inks":["black","red"]}`)
	fit.Warnings = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &fit)
	if fit.Palette != "bw" || len(fit.Warnings) != 1 || fit.Warnings[0] != "The printer prints only black and white, so red prints black." {
		t.Errorf("fit in black and white: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+red+"/fit", `{"width_mm":62,"height_mm":29,"dpi":300,"inks":["purple"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an ink that is not one: %d", rec.Code)
	}

	// Printed, the bar is red and the name black.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+red+"/test-print", `{"template":`+redTemplate+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("test print: %d %s", rec.Code, rec.Body)
	}
	pages, err := brotherql.DecodeJob(fake.waitForJobs(t, 1, 3*time.Second)[0])
	if err != nil || len(pages) != 1 || !pages[0].TwoColor {
		t.Fatalf("the job: %v", err)
	}
	m, _ := brotherql.LookupMedia("dk-22251")
	dark := func(img *image.Gray, y0, y1 int) int {
		n := 0
		for y := y0; y < y1; y++ {
			for _, v := range img.Pix[y*img.Stride : y*img.Stride+img.Rect.Dx()] {
				if v == 0 {
					n++
				}
			}
		}
		return n
	}
	black, redDots := brotherql.PageImage(m, pages[0]), brotherql.PageImage(m, brotherql.Page{Lines: pages[0].RedLines})
	// The bar is 16-24 mm down: dots 189-283; the name above it.
	if dark(redDots, 195, 278) < 40000 || dark(redDots, 0, 150) != 0 || dark(black, 195, 278) != 0 || dark(black, 0, 150) == 0 {
		t.Errorf("red %d over the bar, %d above; black %d over the bar, %d above",
			dark(redDots, 195, 278), dark(redDots, 0, 150), dark(black, 195, 278), dark(black, 0, 150))
	}

	// A preview in a printer's inks, and a palette that is not one.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+redTemplate+`,"format":"png","palette":"bwr"}`)
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("preview: %d %v", rec.Code, err)
	}
	if p, ok := img.(*image.Paletted); !ok || len(p.Palette) != len(labels.PaletteBWR.Inks()) {
		t.Errorf("preview in bwr: %T", img)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+redTemplate+`,"format":"png","palette":"cmyk"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a palette that is not one: %d", rec.Code)
	}
}
