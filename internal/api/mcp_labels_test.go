package api

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPreviewFormats renders an unsaved template as PNG, for assistants to
// see, and as JSON, saying what each element prints.
func TestPreviewFormats(t *testing.T) {
	srv, cookie, tagged, _, _ := labelFixture(t)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+labelTemplate+`,"asset_id":"`+tagged+`","format":"png"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("png: %d %s", rec.Code, rec.Header())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 472 { // 40 mm at 300 DPI
		t.Errorf("png: %v %v", err, img.Bounds())
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+labelTemplate+`,"asset_id":"`+tagged+`","format":"json"}`)
	var desc PreviewDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &desc); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("json: %d %s", rec.Code, rec.Body)
	}
	if desc.Width != 40 || desc.Units != "mm" || desc.DPI != 300 || len(desc.Elements) != 3 ||
		desc.Elements[0].Value != "BAM-0042" || desc.Elements[1].Value != "Drill" || desc.Elements[2].Value != "Shelf B3" || desc.Elements[2].Y != 11 {
		t.Errorf("json: %+v", desc)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+labelTemplate+`,"format":"gif"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("gif: %d", rec.Code)
	}
	// SVG stays the default, for the editor.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+labelTemplate+`}`)
	if !strings.HasPrefix(rec.Body.String(), "<?xml") {
		t.Errorf("default: %.40s", rec.Body)
	}
}

func imagesIn(res *mcp.CallToolResult) []*mcp.ImageContent {
	var out []*mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			out = append(out, ic)
		}
	}
	return out
}

func TestMCPLabelTools(t *testing.T) {
	srv, cookie, tagged, untagged, tag := labelFixture(t)
	admin, _ := srv.store.GetUserByEmail("admin@test.org")
	ql := newFakeQL(t)
	printer := createPrinter(t, srv, cookie, ql.port(), "dk-22205")
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Designer", rbac.PermAssetsRead, rbac.PermAssetsScan, rbac.PermTemplatesManage, rbac.PermLabelsPrint, rbac.PermSettingsManage))

	// A template as wide as the printer's roll, to print on it.
	wide := createTemplate(t, srv, cookie, strings.Replace(labelTemplate, `"width":40,"height":20`, `"width":62,"height":29`, 1))

	// Seeing a template as it prints, and reading what it prints.
	res := callTool(t, cs, "preview_template", map[string]any{"template_id": tag, "assets": []string{tagged, "BAM-0042"}})
	var preview struct {
		Width  float64
		Labels []struct {
			Elements []struct {
				Value string
				Name  string
			}
		}
		Fit   map[string]any
		Scale int
	}
	structured(t, res, &preview)
	images := imagesIn(res)
	if len(images) != 2 || len(preview.Labels) != 2 || preview.Labels[1].Elements[1].Value != "Drill" || preview.Fit != nil {
		t.Fatalf("preview: %+v, %d images: %s", preview, len(images), toolText(res))
	}
	// Small labels are shown larger, whole dots at a time.
	img, err := png.Decode(bytes.NewReader(images[0].Data))
	if err != nil || img.Bounds().Dx() != 472*preview.Scale || preview.Scale != 2 {
		t.Errorf("enlarged: %v %v ×%d", err, img.Bounds(), preview.Scale)
	}

	// Checked against the printer: the narrow one does not fit its roll,
	// the wide one does.
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag, "check_printer": printer}), &preview)
	if preview.Fit["fits"] != false || !strings.Contains(preview.Fit["message"].(string), "62 mm") {
		t.Errorf("the narrow one: %+v", preview.Fit)
	}
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": wide, "check_printer": printer}), &preview)
	if preview.Fit["fits"] != true {
		t.Errorf("the wide one: %+v", preview.Fit)
	}

	// Changing an element, and seeing the change.
	var saved map[string]any
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "change_elements": []any{map[string]any{"index": 2, "set": map[string]any{"content_binding": "Fragile", "name": "Warning"}}}}), &saved)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &preview)
	if got := preview.Labels[0].Elements[2]; got.Value != "Fragile" || got.Name != "Warning" {
		t.Errorf("after the change: %+v", got)
	}
	// The name is as written in the template, read raw.
	var named struct {
		Template struct{ Elements []map[string]any }
	}
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &named)
	if named.Template.Elements[2]["name"] != "Warning" {
		t.Errorf("get_template: %+v", named.Template.Elements[2])
	}
	// Text filling its box, and the size it was drawn at.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "change_elements": []any{map[string]any{"index": 1, "set": map[string]any{"fit": "fill", "max_font_size": 30}}}}), &saved)
	var sized struct {
		Labels []struct {
			Elements []struct {
				FontSize      float64 `json:"font_size"`
				DrawnFontSize float64 `json:"drawn_font_size"`
			}
		}
	}
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &sized)
	filled := sized.Labels[0].Elements[1].DrawnFontSize
	if !(filled > 6 && filled <= 30) {
		t.Errorf("filling its box: drawn at %v pt", filled)
	}
	// Held to 6 pt at most, it is drawn no larger.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "change_elements": []any{map[string]any{"index": 1, "set": map[string]any{"max_font_size": 6}}}}), &saved)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &sized)
	if held := sized.Labels[0].Elements[1].DrawnFontSize; held != 6 {
		t.Errorf("held to 6 pt: drawn at %v pt", held)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "remove_elements": []int{2}}), &saved)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &preview)
	if len(preview.Labels[0].Elements) != 2 {
		t.Errorf("after removing: %+v", preview)
	}
	// Bringing the code to the front: elements are drawn as listed.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "move_elements": []any{map[string]any{"index": 0, "to": 9}}}), &saved)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &preview)
	if got := preview.Labels[0].Elements; len(got) != 2 || got[0].Value != "Sample asset" || got[1].Value == "Sample asset" {
		t.Errorf("after moving: %+v", got)
	}
	if res := callTool(t, cs, "save_template", map[string]any{"id": tag, "move_elements": []any{map[string]any{"index": 2, "to": 0}}}); !res.IsError {
		t.Errorf("moved an element that is not there: %s", toolText(res))
	}
	// Turning the code upside down, and reading where it lies turned.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "change_elements": []any{map[string]any{"index": 1, "set": map[string]any{"rotation": 180}}}}), &saved)
	var turned struct {
		Labels []struct {
			Elements []struct {
				Rotation float64
				Bounds   *struct{ X, Width float64 }
			}
		}
	}
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &turned)
	if e := turned.Labels[0].Elements[1]; e.Rotation != 180 || e.Bounds == nil || e.Bounds.Width != 18 {
		t.Errorf("turned: %+v", turned.Labels[0].Elements)
	}
	// Drawing a frame round the label, behind the rest, and reading it back.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag,
		"add_elements": []any{map[string]any{"type": "shape", "shape": "rectangle", "x": 0, "y": 0, "width": 40, "height": 20, "corner_radius": 2}}}), &saved)
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "move_elements": []any{map[string]any{"index": 2, "to": 0}}}), &saved)
	var framed struct {
		Labels []struct {
			Elements []struct {
				Type, Shape, Stroke string
				CornerRadius        float64 `json:"corner_radius"`
			}
		}
	}
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &framed)
	if e := framed.Labels[0].Elements; len(e) != 3 || e[0].Shape != "rectangle" || e[0].Stroke != "black" || e[0].CornerRadius != 2 {
		t.Errorf("framed: %+v", e)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "remove_elements": []int{0}}), &saved)

	// No margin on the code's left, and where that leaves it.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "change_elements": []any{map[string]any{"index": 1,
		"set": map[string]any{"margin": map[string]any{"left": 0}, "align": "left"}}}}), &saved)
	var coded struct {
		Labels []struct {
			Elements []struct {
				Code *struct {
					MarginDots struct{ Left float64 } `json:"margin_dots"`
					Narrow     []string
				}
			}
			Warnings []string
		}
	}
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &coded)
	if c := coded.Labels[0].Elements[1].Code; c == nil || c.MarginDots.Left != 0 || !slices.Contains(c.Narrow, "left") || len(coded.Labels[0].Warnings) != 1 {
		t.Errorf("margins: %+v", coded.Labels[0])
	}
	// Artwork imported into a box, then added.
	var art struct {
		Elements []map[string]any
		Skipped  []string
	}
	structured(t, callTool(t, cs, "import_svg", map[string]any{"svg": `<svg viewBox="0 0 10 10"><circle cx="5" cy="5" r="5"/></svg>`,
		"box": map[string]any{"x": 30, "y": 10, "width": 8, "height": 8}}), &art)
	if len(art.Elements) != 1 || art.Elements[0]["shape"] != "path" || art.Elements[0]["x"] != 30.0 {
		t.Fatalf("imported: %+v", art)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "add_elements": []any{art.Elements[0]}}), &saved)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template_id": tag}), &framed)
	if e := framed.Labels[0].Elements; len(e) != 3 || e[2].Shape != "path" {
		t.Errorf("with the artwork: %+v", e)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "remove_elements": []int{2}}), &saved)

	// The text lined up with the code's left edge.
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag,
		"arrange_elements": map[string]any{"elements": []int{0, 1}, "align": "left"}}), &saved)
	var lined struct {
		Template struct{ Elements []struct{ X float64 } }
	}
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &lined)
	if e := lined.Template.Elements; e[0].X != 1 || e[1].X != 1 {
		t.Errorf("lined up: %+v", e)
	}
	if res := callTool(t, cs, "save_template", map[string]any{"id": tag, "arrange_elements": map[string]any{"elements": []int{0, 1}, "distribute": "across"}}); !res.IsError {
		t.Errorf("spread out two: %s", toolText(res))
	}

	// Turned upright with everything on it, then back.
	var got struct {
		Template struct {
			Width, Height float64
			Elements      []struct{ Rotation float64 }
		}
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "turn": "right"}), &saved)
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &got)
	if got.Template.Width != 20 || got.Template.Height != 40 || got.Template.Elements[0].Rotation != 90 || got.Template.Elements[1].Rotation != 270 {
		t.Errorf("turned right: %+v", got.Template)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "turn": "left"}), &saved)
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &got)
	if got.Template.Width != 40 || got.Template.Elements[0].Rotation != 0 || got.Template.Elements[1].Rotation != 180 {
		t.Errorf("and back: %+v", got.Template)
	}
	if res := callTool(t, cs, "save_template", map[string]any{"id": tag, "turn": "around"}); !res.IsError {
		t.Errorf("an unknown turn: %s", toolText(res))
	}
	// Printing mirrored and turned, and back as designed.
	var printing struct {
		Template struct {
			PrintMirror   string  `json:"print_mirror"`
			PrintRotation float64 `json:"print_rotation"`
		}
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "print_mirror": "across", "print_rotation": 15}), &saved)
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &printing)
	if printing.Template.PrintMirror != "across" || printing.Template.PrintRotation != 15 {
		t.Errorf("printing mirrored and turned: %+v", printing.Template)
	}
	structured(t, callTool(t, cs, "save_template", map[string]any{"id": tag, "print_mirror": "none", "print_rotation": 0}), &saved)
	printing.Template.PrintMirror, printing.Template.PrintRotation = "?", -1
	structured(t, callTool(t, cs, "get_template", map[string]any{"id": tag}), &printing)
	if printing.Template.PrintMirror != "" || printing.Template.PrintRotation != 0 {
		t.Errorf("printing as designed: %+v", printing.Template)
	}
	// A draft never saved, too, and one that cannot be made for an asset says why.
	draft := map[string]any{}
	_ = json.Unmarshal([]byte(labelTemplate), &draft)
	structured(t, callTool(t, cs, "preview_template", map[string]any{"template": draft, "assets": []string{untagged}}), &preview)
	if res := callTool(t, cs, "preview_template", map[string]any{"template": draft, "assets": []string{untagged}}); !strings.Contains(toolText(res), "cannot be made") {
		t.Errorf("an asset without a code: %s", toolText(res))
	}

	// A label for an asset, as an image and as a PDF.
	if res := callTool(t, cs, "render_label", map[string]any{"asset": tagged, "template_id": tag}); len(imagesIn(res)) != 1 {
		t.Errorf("render_label: %s", toolText(res))
	}
	res = callTool(t, cs, "export_labels", map[string]any{"template_id": tag, "assets": []string{tagged}, "format": "pdf"})
	pdf := false
	for _, c := range res.Content {
		if er, ok := c.(*mcp.EmbeddedResource); ok && er.Resource.MIMEType == "application/pdf" && bytes.HasPrefix(er.Resource.Blob, []byte("%PDF")) {
			pdf = true
		}
	}
	if !pdf {
		t.Errorf("export_labels: %s", toolText(res))
	}

	// Printing, now and through the queue.
	if res := callTool(t, cs, "label_options", map[string]any{"assets": []string{tagged}}); res.IsError {
		t.Errorf("label_options: %s", toolText(res))
	}
	if res := callTool(t, cs, "print_labels", map[string]any{"printer_id": printer, "template_id": wide, "assets": []string{tagged}}); res.IsError {
		t.Fatalf("print_labels: %s", toolText(res))
	}
	if jobs := ql.waitForJobs(t, 1, 5*time.Second); len(jobs) != 1 {
		t.Errorf("printed %d jobs", len(jobs))
	}
	// Mirrored this time, it prints otherwise.
	if res := callTool(t, cs, "print_labels", map[string]any{"printer_id": printer, "template_id": wide, "assets": []string{tagged}, "mirror": "across"}); res.IsError {
		t.Fatalf("print_labels mirrored: %s", toolText(res))
	}
	if jobs := ql.waitForJobs(t, 2, 5*time.Second); len(jobs) != 2 || bytes.Equal(jobs[0], jobs[1]) {
		t.Errorf("printed %d jobs, mirrored the same as not", len(jobs))
	}
	if res := callTool(t, cs, "print_labels", map[string]any{"printer_id": printer, "template_id": wide, "assets": []string{tagged}, "mirror": "sideways"}); !res.IsError {
		t.Errorf("an unknown mirror: %s", toolText(res))
	}
	draft["width"], draft["height"] = 62, 29
	if res := callTool(t, cs, "test_print", map[string]any{"printer_id": printer, "template": draft}); res.IsError {
		t.Errorf("test_print: %s", toolText(res))
	}
	if res := callTool(t, cs, "queue_labels", map[string]any{"assets": []string{tagged}, "template_id": wide, "destination": "printer:" + printer}); res.IsError {
		t.Fatalf("queue_labels: %s", toolText(res))
	}
	var queue struct {
		Entries []struct{ ID string }
	}
	structured(t, callTool(t, cs, "list_print_queue", nil), &queue)
	if len(queue.Entries) != 1 {
		t.Fatalf("queue: %+v", queue)
	}
	if res := callTool(t, cs, "update_print_queue", map[string]any{"entry_ids": []string{queue.Entries[0].ID}, "copies": 2}); res.IsError {
		t.Errorf("update_print_queue: %s", toolText(res))
	}
	if res := callTool(t, cs, "print_queue", nil); res.IsError || !strings.Contains(toolText(res), "1 print jobs") {
		t.Errorf("print_queue: %s", toolText(res))
	}
	for _, name := range []string{"list_print_jobs", "list_templates", "list_printers", "list_printer_drivers", "list_label_stock", "list_printer_pools", "list_images"} {
		if res := callTool(t, cs, name, nil); res.IsError {
			t.Errorf("%s: %s", name, toolText(res))
		}
	}

	// Changing a printer says first what it would affect.
	if res := callTool(t, cs, "save_printer", map[string]any{"id": printer, "name": "Front desk"}); res.IsError || !strings.Contains(toolText(res), "Saved") {
		t.Errorf("renaming: %s", toolText(res))
	}
	var p struct{ Printer struct{ Name, Media string } }
	_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+printer, "").Body.Bytes(), &p)
	if p.Printer.Name != "Front desk" || p.Printer.Media != "dk-22205" {
		t.Errorf("saved printer: %+v", p)
	}
	var stock map[string]any
	structured(t, callTool(t, cs, "save_label_stock", map[string]any{"name": "Bin labels", "width_mm": 60, "length_mm": 40, "gap_mm": 3}), &stock)
	if res := callTool(t, cs, "save_label_stock", map[string]any{"id": stock["stock"].(map[string]any)["id"], "gap_mm": 2}); res.IsError {
		t.Errorf("changing stock: %s", toolText(res))
	}

	// Only those who design see the design tools.
	names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Printer", rbac.PermAssetsRead, rbac.PermLabelsPrint)))
	if slices.Contains(names, "save_template") || slices.Contains(names, "test_print") || !slices.Contains(names, "print_labels") {
		t.Errorf("a printer lists %v", names)
	}
}
