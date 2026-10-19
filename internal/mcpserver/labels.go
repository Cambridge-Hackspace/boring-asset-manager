package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// minPreviewWidth is the narrowest a preview is shown to a model: smaller
// labels are scaled up, whole dots at a time, so their text can be read.
const minPreviewWidth = 640

// enlarge scales a small PNG up by a whole factor, each dot a square of
// pixels, so nothing is blurred; the label's real size is said in words.
func enlarge(data []byte) ([]byte, int) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() >= minPreviewWidth || img.Bounds().Dx() == 0 {
		return data, 1
	}
	k := min((minPreviewWidth+img.Bounds().Dx()-1)/img.Bounds().Dx(), 8)
	b := img.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx()*k, b.Dy()*k))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x/k, b.Min.Y+y/k))
		}
	}
	var buf bytes.Buffer
	if png.Encode(&buf, out) != nil {
		return data, 1
	}
	return buf.Bytes(), k
}

type templateRef struct {
	TemplateID string         `json:"template_id,omitempty" jsonschema:"a saved template"`
	Template   map[string]any `json:"template,omitempty" jsonschema:"or a template that need not be saved: a draft, as save_template takes it"`
}

// spec is the template a reference names, as JSON.
func (e *env) spec(ctx context.Context, in templateRef) (map[string]any, error) {
	if in.Template != nil {
		return inListOrder(in.Template), nil
	}
	if in.TemplateID == "" {
		return nil, fmt.Errorf("give template_id, or a template")
	}
	var got struct {
		Template map[string]any `json:"template"`
	}
	if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/templates/{id}", in.TemplateID), nil, &got); err != nil {
		return nil, err
	}
	return got.Template, nil
}

// inListOrder marks a template an assistant gives as drawn in list order, as
// these tools describe it, unless it says otherwise.
func inListOrder(spec map[string]any) map[string]any {
	if _, ok := spec["layering"]; !ok {
		spec["layering"] = "list"
	}
	return spec
}

// arrangeIn lines elements up, or spreads them out.
type arrangeIn struct {
	Elements   []int  `json:"elements" jsonschema:"the elements, by index"`
	Align      string `json:"align,omitempty" jsonschema:"left, center or right across; top, middle or bottom down"`
	Distribute string `json:"distribute,omitempty" jsonschema:"across or down: even gaps between three or more, the outermost staying put"`
	ToLabel    bool   `json:"to_label,omitempty" jsonschema:"align with the label, not with each other"`
}

// turned is a template turned a quarter turn, left or right, with
// everything on it.
func turned(spec map[string]any, way string) (map[string]any, error) {
	if way != "left" && way != "right" {
		return nil, fmt.Errorf(`turn must be "left" or "right"`)
	}
	return edited(spec, func(t *labels.Template) error {
		*t = *t.Turned(way == "right")
		return nil
	})
}

// edited is a template changed by f, read and written back as JSON.
func edited(spec map[string]any, f func(*labels.Template) error) (map[string]any, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var t labels.Template
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("the template cannot be read: %v", err)
	}
	if err := f(&t); err != nil {
		return nil, err
	}
	if raw, err = json.Marshal(&t); err != nil {
		return nil, err
	}
	out := map[string]any{}
	return out, json.Unmarshal(raw, &out)
}

// moveElements moves elements to new places in the drawing order, in turn;
// each index is one in elements as given.
func moveElements(elements []any, moves []elementMove) ([]any, []int, error) {
	// order[k] is the index, as given, of the element now at k.
	order := make([]int, len(elements))
	for i := range order {
		order[i] = i
	}
	for _, m := range moves {
		if m.Index < 0 || m.Index >= len(elements) {
			return nil, nil, fmt.Errorf("no element %d: there are %d", m.Index, len(elements))
		}
		at := slices.Index(order, m.Index)
		order = slices.Delete(order, at, at+1)
		to := min(max(m.To, 0), len(order))
		order = slices.Insert(order, to, m.Index)
	}
	moved := make([]any, len(elements))
	for k, i := range order {
		moved[k] = elements[i]
	}
	return moved, order, nil
}

type elementMove struct {
	Index int `json:"index" jsonschema:"the element, numbered as the template is now"`
	To    int `json:"to" jsonschema:"where it goes in the drawing order: 0 is at the back, under everything; a number past the last element puts it in front"`
}

type previewIn struct {
	templateRef
	Assets       []string `json:"assets,omitempty" jsonschema:"assets to fill it from, by ID or code, up to 10; none for placeholders"`
	Width        float64  `json:"width,omitempty" jsonschema:"with height and dpi, lay an adaptive template out for this label size, in its units"`
	Height       float64  `json:"height,omitempty"`
	DPI          int      `json:"dpi,omitempty"`
	ForPrinter   string   `json:"for_printer,omitempty" jsonschema:"lay it out for the roll this printer has loaded, as printing there would"`
	CheckPrinter string   `json:"check_printer,omitempty" jsonschema:"say whether it prints on this printer, and which elements fall where it does not print"`
	Mono         bool     `json:"mono,omitempty" jsonschema:"render in black and white only, as thermal printers do"`
}

type previewLabel struct {
	Asset    string           `json:"asset,omitempty"`
	Elements []map[string]any `json:"elements" jsonschema:"each element: index, type, its box (x, y, width, height in the template's units), rotation and bounds when turned, value, what it prints, and for a code, its module and margins as drawn"`
	Warnings []string         `json:"warnings,omitempty" jsonschema:"what to know, which never stops it printing, such as a code's margin narrower than its standard quiet zone"`
	Problem  string           `json:"problem,omitempty" jsonschema:"why this label cannot be made"`
}

type adaptIn struct {
	Template  map[string]any `json:"template"`
	Width     float64        `json:"width,omitempty"`
	Height    float64        `json:"height,omitempty"`
	DPI       int            `json:"dpi,omitempty"`
	PrinterID string         `json:"printer_id,omitempty" jsonschema:"in place of a size and DPI: that printer's roll"`
}

type rangeIn struct {
	Template map[string]any `json:"template"`
	DPI      int            `json:"dpi"`
}

type fitIn struct {
	PrinterID string           `json:"printer_id"`
	WidthMM   float64          `json:"width_mm"`
	HeightMM  float64          `json:"height_mm"`
	DPI       int              `json:"dpi"`
	Content   []map[string]any `json:"content,omitempty" jsonschema:"the boxes printed on, each x, y, width, height in mm"`
	Feed      string           `json:"feed,omitempty" jsonschema:"for a label that fits either way: across (its width across the roll), along, or auto (as fits)"`
}

type previewOut struct {
	Width  float64         `json:"width"`
	Height float64         `json:"height"`
	Units  string          `json:"units"`
	DPI    int             `json:"dpi"`
	Labels []previewLabel  `json:"labels"`
	Fit    *map[string]any `json:"fit,omitempty" jsonschema:"the printer's check: whether it prints, why not, and warnings"`
	Scale  int             `json:"scale" jsonschema:"how many times larger than the printer's dots the images are shown"`
}

func labelTools() []tool {
	read := []string{rbac.PermAssetsRead}
	design := []string{rbac.PermTemplatesManage}
	settings := []string{rbac.PermSettingsManage}
	printing := []string{rbac.PermLabelsPrint}
	seePrinters := []string{rbac.PermLabelsPrint, rbac.PermSettingsManage, rbac.PermTemplatesManage}

	testPrint := define(&mcp.Tool{Name: "test_print", Description: "Print one label of a template, saved or a draft, on a printer, for an asset or with placeholders, to see it on paper. It is not recorded in any asset's history.",
		Annotations: changes("Test print", false)}, []string{"POST /api/v1/printers/{id}/test-print"}, printing,
		func(ctx context.Context, e *env, in struct {
			templateRef
			PrinterID string `json:"printer_id"`
			Asset     string `json:"asset,omitempty" jsonschema:"an asset to fill it from, by ID or code; none for placeholders"`
		}) (reply[map[string]any], error) {
			spec, err := e.spec(ctx, in.templateRef)
			if err != nil {
				return reply[map[string]any]{}, err
			}
			body := map[string]any{"template": spec}
			if in.Asset != "" {
				id, err := e.assetID(ctx, in.Asset)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				body["asset_id"] = id
			}
			out := map[string]any{}
			if err := e.call(ctx, http.MethodPost, pathOf("/api/v1/printers/{id}/test-print", in.PrinterID), body, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			return say(out, "Sent a test label to the printer.")
		})
	testPrint.needsAll = []string{rbac.PermLabelsPrint, rbac.PermTemplatesManage}

	return []tool{
		// Templates.
		simple(&mcp.Tool{Name: "list_templates", Description: "Every label template: its size, units and DPI, whether it fits several label sizes, and, for those who may see printers, which printers suit it now.",
			Annotations: readOnly("List label templates")}, "GET /api/v1/templates", read,
			func(none) (request, error) { return request{path: "/api/v1/templates"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d templates.", count(out, "templates")) }),
		simple(&mcp.Tool{Name: "get_template", Description: "One label template, whole: its size, and each element (text, codes and images) with its box and what it shows. Elements are numbered from 0 in this order.",
			Annotations: readOnly("Get a template")}, "GET /api/v1/templates/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/templates/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "template")) }),
		define(&mcp.Tool{Name: "save_template", Description: "Make a label template, or change one: give the whole template, or only changes: a new name, elements to change (by index, " +
			"each with the properties to set), elements to move in the drawing order, elements to remove (by index) and elements to add, in front. Elements are drawn in the order " +
			"they are listed, each over those before it. Every index is the element's number as the template is now. Then preview_template shows the result.",
			Annotations: changes("Save a template", true)}, []string{"POST /api/v1/templates", "PUT /api/v1/templates/{id}"}, design,
			func(ctx context.Context, e *env, in struct {
				ID       string         `json:"id,omitempty" jsonschema:"the template to change; empty to make one"`
				Template map[string]any `json:"template,omitempty" jsonschema:"the whole template: name, units (mm or in), width, height, dpi, sizing, and elements"`
				Name     string         `json:"name,omitempty"`
				Change   []struct {
					Index int            `json:"index"`
					Set   map[string]any `json:"set" jsonschema:"properties to set, such as x, y, width, height, rotation (degrees clockwise about the box's center), content_binding, font_size, group (the editor moves a group as one), locked; for a code margin (top, right, bottom, left; sides left out are standard), margin_unit (modules or length), align and vertical_align; an image mirror (across, down or both); a shape has shape (rectangle, ellipse, line or polygon), stroke and fill (black, white or none), stroke_width, corner_radius, flip, and points (fractions of its box) and closed"`
				} `json:"change_elements,omitempty"`
				Move    []elementMove    `json:"move_elements,omitempty" jsonschema:"elements to put elsewhere in the drawing order, moved in turn"`
				Turn    string           `json:"turn,omitempty" jsonschema:"left or right: turn the whole template a quarter turn with everything on it, as the editor's portrait and landscape switch does, after the other changes"`
				Arrange *arrangeIn       `json:"arrange_elements,omitempty" jsonschema:"line elements up or spread them out, as the editor's align and distribute buttons do, after the other changes and before turning"`
				Remove  []int            `json:"remove_elements,omitempty"`
				Add     []map[string]any `json:"add_elements,omitempty"`
			}) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Template == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new template needs the whole template")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/templates", inListOrder(in.Template), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the template %q.", nameOf(out, "template"))
				}
				spec := in.Template
				if spec == nil {
					var err error
					if spec, err = e.spec(ctx, templateRef{TemplateID: in.ID}); err != nil {
						return reply[map[string]any]{}, err
					}
					elements, _ := spec["elements"].([]any)
					for _, c := range in.Change {
						if c.Index < 0 || c.Index >= len(elements) {
							return reply[map[string]any]{}, fmt.Errorf("no element %d: there are %d", c.Index, len(elements))
						}
						el, _ := elements[c.Index].(map[string]any)
						for k, v := range c.Set {
							el[k] = v
						}
					}
					remove := map[int]bool{}
					for _, i := range in.Remove {
						if i < 0 || i >= len(elements) {
							return reply[map[string]any]{}, fmt.Errorf("no element %d: there are %d", i, len(elements))
						}
						remove[i] = true
					}
					moved, order, err := moveElements(elements, in.Move)
					if err != nil {
						return reply[map[string]any]{}, err
					}
					kept := []any{}
					for k, el := range moved {
						if !remove[order[k]] {
							kept = append(kept, el)
						}
					}
					for _, el := range in.Add {
						kept = append(kept, el)
					}
					spec["elements"] = kept
					if in.Name != "" {
						spec["name"] = in.Name
					}
				}
				spec["id"] = in.ID
				inListOrder(spec)
				if in.Arrange != nil {
					var err error
					if spec, err = edited(spec, func(t *labels.Template) error {
						return t.Arrange(in.Arrange.Elements, in.Arrange.Align, in.Arrange.Distribute, in.Arrange.ToLabel)
					}); err != nil {
						return reply[map[string]any]{}, err
					}
				}
				if in.Turn != "" {
					var err error
					if spec, err = turned(spec, in.Turn); err != nil {
						return reply[map[string]any]{}, err
					}
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/templates/{id}", in.ID), spec, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the template %q. Preview it with preview_template.", nameOf(out, "template"))
			}),
		define(&mcp.Tool{Name: "import_svg", Description: "Turn SVG artwork (a logo, symbol or icon) into shapes for a template: paths in black or white, sharp at any size. " +
			"Give the box on the template, in its units, to fit it into, keeping its proportions; nothing is saved: add the shapes with save_template's add_elements. " +
			"Text, images, scripts, links to other files, filters and the like are left out, and listed.",
			Annotations: readOnly("Import SVG artwork")}, []string{"POST /api/v1/templates/import-svg"}, design,
			func(ctx context.Context, e *env, in struct {
				SVG string `json:"svg" jsonschema:"the SVG file's text, at most 1 MB"`
				Box *struct {
					X      float64 `json:"x"`
					Y      float64 `json:"y"`
					Width  float64 `json:"width"`
					Height float64 `json:"height"`
				} `json:"box,omitempty" jsonschema:"where on the template to fit it, in the template's units; without one, the shapes are in the artwork's own units"`
			}) (reply[map[string]any], error) {
				out := map[string]any{}
				body := map[string]any{"svg": in.SVG}
				if in.Box != nil {
					body["box"] = in.Box
				}
				if err := e.call(ctx, http.MethodPost, "/api/v1/templates/import-svg", body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				skipped, _ := out["skipped"].([]any)
				summary := fmt.Sprintf("%d shapes, to add with save_template.", count(out, "elements"))
				if len(skipped) > 0 {
					summary += fmt.Sprintf(" Left out: %v.", skipped)
				}
				return say(out, "%s", summary)
			}),
		simple(&mcp.Tool{Name: "delete_template", Description: "Delete a label template no category names.",
			Annotations: destroys("Delete a template", true)}, "DELETE /api/v1/templates/{id}", design,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/templates/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the template." }),
		define(&mcp.Tool{Name: "preview_template", Description: "See a label template, saved or a draft, as it prints: an image of each label, filled from up to 10 assets or with placeholders, " +
			"and, for each, every element's box and exactly what it prints, to read rather than guess from pixels. Optionally laid out for another label size or a printer's roll, " +
			"and checked against a printer, which says which elements fall where it does not print.",
			Annotations: readOnly("Preview a template")},
			[]string{"POST /api/v1/templates/preview", "POST /api/v1/templates/adapt", "POST /api/v1/printers/{id}/fit"}, design,
			func(ctx context.Context, e *env, in previewIn) (reply[previewOut], error) {
				if len(in.Assets) > 10 {
					return reply[previewOut]{}, fmt.Errorf("at most 10 assets at a time")
				}
				spec, err := e.spec(ctx, in.templateRef)
				if err != nil {
					return reply[previewOut]{}, err
				}
				if in.ForPrinter != "" || in.Width > 0 {
					var adapted struct {
						Fits     bool           `json:"fits"`
						Template map[string]any `json:"template"`
						Problem  struct {
							Message string `json:"message"`
						} `json:"problem"`
					}
					body := map[string]any{"template": spec, "width": in.Width, "height": in.Height, "dpi": in.DPI, "printer_id": in.ForPrinter}
					if err := e.call(ctx, http.MethodPost, "/api/v1/templates/adapt", body, &adapted); err != nil {
						return reply[previewOut]{}, err
					}
					if !adapted.Fits {
						return reply[previewOut]{}, fmt.Errorf("it does not fit that label: %s", adapted.Problem.Message)
					}
					spec = adapted.Template
				}
				assets := in.Assets
				if len(assets) == 0 {
					assets = []string{""}
				}
				out := previewOut{Labels: []previewLabel{}, Scale: 1}
				var images []mcp.Content
				for _, a := range assets {
					label := previewLabel{Asset: a, Elements: []map[string]any{}}
					body := map[string]any{"template": spec}
					if a != "" {
						id, err := e.assetID(ctx, a)
						if err != nil {
							return reply[previewOut]{}, err
						}
						body["asset_id"] = id
					}
					var desc struct {
						Width    float64          `json:"width"`
						Height   float64          `json:"height"`
						Units    string           `json:"units"`
						DPI      int              `json:"dpi"`
						Elements []map[string]any `json:"elements"`
						Warnings []string         `json:"warnings"`
					}
					body["format"] = "json"
					if err := e.call(ctx, http.MethodPost, "/api/v1/templates/preview", body, &desc); err != nil {
						label.Problem = err.Error()
						out.Labels = append(out.Labels, label)
						continue
					}
					out.Width, out.Height, out.Units, out.DPI = desc.Width, desc.Height, desc.Units, desc.DPI
					label.Elements, label.Warnings = desc.Elements, desc.Warnings
					body["format"], body["mono"] = "png", in.Mono
					raw, _ := json.Marshal(body)
					res := e.api(ctx, http.MethodPost, "/api/v1/templates/preview", raw, "application/json")
					if res.Status != http.StatusOK {
						label.Problem = apiError(res).Error()
					} else {
						img, k := enlarge(res.Body)
						out.Scale = k
						images = append(images, &mcp.ImageContent{Data: img, MIMEType: "image/png"})
					}
					out.Labels = append(out.Labels, label)
				}
				if in.CheckPrinter != "" {
					mm := 1.0
					if out.Units == "in" {
						mm = 25.4
					}
					var content []map[string]float64
					for _, el := range out.Labels[0].Elements {
						f := func(k string) float64 { v, _ := el[k].(float64); return v * mm }
						content = append(content, map[string]float64{"x": f("x"), "y": f("y"), "width": f("width"), "height": f("height")})
					}
					fit := map[string]any{}
					body := map[string]any{"width_mm": out.Width * mm, "height_mm": out.Height * mm, "dpi": out.DPI, "content": content}
					if err := e.call(ctx, http.MethodPost, pathOf("/api/v1/printers/{id}/fit", in.CheckPrinter), body, &fit); err != nil {
						return reply[previewOut]{}, err
					}
					out.Fit = &fit
				}
				text := fmt.Sprintf("%g × %g %s at %d DPI", out.Width, out.Height, out.Units, out.DPI)
				if out.Scale > 1 {
					text += fmt.Sprintf(", shown %d times larger", out.Scale)
				}
				for _, l := range out.Labels {
					if l.Problem != "" {
						text += fmt.Sprintf(". %s cannot be made: %s", orPlaceholder(l.Asset), l.Problem)
					}
				}
				if out.Fit != nil {
					if fits, _ := (*out.Fit)["fits"].(bool); fits {
						text += ". It prints on that printer"
					} else {
						msg, _ := (*out.Fit)["message"].(string)
						text += ". " + msg
					}
				}
				r, _ := say(out, "%s.", text)
				r.extra = images
				return r, nil
			}),
		simple(&mcp.Tool{Name: "adapt_template", Description: "Lay an adaptive template out for another label size, or a printer's roll, and get the template as laid out, with notices of what moved or shrank.",
			Annotations: readOnly("Adapt a template")}, "POST /api/v1/templates/adapt", design,
			func(in adaptIn) (request, error) {
				return request{path: "/api/v1/templates/adapt", body: in}, nil
			},
			func(_ adaptIn, out map[string]any) string {
				if fits, _ := out["fits"].(bool); fits {
					return "It fits."
				}
				return "It does not fit that label."
			}),
		simple(&mcp.Tool{Name: "template_size_range", Description: "The label sizes an adaptive template fits, in its own shape, with the longest codes assigned.",
			Annotations: readOnly("Template size range")}, "POST /api/v1/templates/range", design,
			func(in rangeIn) (request, error) {
				return request{path: "/api/v1/templates/range", body: in}, nil
			},
			func(rangeIn, map[string]any) string {
				return "The sizes it fits."
			}),

		// Labels.
		define(&mcp.Tool{Name: "render_label", Description: "An asset's label from a template: as an image (png), or as a PDF or SVG file.",
			Annotations: readOnly("Render a label")}, []string{"GET /api/v1/assets/{id}/label"}, read,
			func(ctx context.Context, e *env, in struct {
				Asset      string `json:"asset" jsonschema:"the asset, by ID or code"`
				TemplateID string `json:"template_id"`
				Format     string `json:"format,omitempty" jsonschema:"png (the default), pdf or svg"`
				Mono       bool   `json:"mono,omitempty"`
			}) (reply[map[string]any], error) {
				id, err := e.assetID(ctx, in.Asset)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				format := in.Format
				if format == "" {
					format = "png"
				}
				q := url.Values{"template_id": {in.TemplateID}, "format": {format}}
				if in.Mono {
					q.Set("mono", "true")
				}
				res := e.api(ctx, http.MethodGet, assetPath(id, "/label?", q.Encode()), nil, "")
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				out := map[string]any{"format": format, "bytes": len(res.Body)}
				r, _ := say(out, "The label, as %s.", format)
				r.extra = []mcp.Content{fileContent("bam://labels/"+id+"."+format, res)}
				return r, nil
			}),
		define(&mcp.Tool{Name: "export_labels", Description: "Several assets' labels as one file: a PDF, a page a label, or a ZIP of PNGs.",
			Annotations: readOnly("Export labels")}, []string{"POST /api/v1/labels/export"}, read,
			func(ctx context.Context, e *env, in struct {
				TemplateID string   `json:"template_id"`
				Assets     []string `json:"assets" jsonschema:"by ID or code"`
				Format     string   `json:"format" jsonschema:"pdf or zip"`
				Mono       bool     `json:"mono,omitempty"`
			}) (reply[map[string]any], error) {
				ids, err := e.assetIDs(ctx, in.Assets)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				raw, _ := json.Marshal(map[string]any{"template_id": in.TemplateID, "asset_ids": ids, "format": in.Format, "mono": in.Mono})
				res := e.api(ctx, http.MethodPost, "/api/v1/labels/export", raw, "application/json")
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				out := map[string]any{"format": in.Format, "bytes": len(res.Body), "labels": len(ids)}
				r, _ := say(out, "%d labels, as %s.", len(ids), in.Format)
				r.extra = []mcp.Content{fileContent("bam://labels/export."+in.Format, res)}
				return r, nil
			}),
		define(&mcp.Tool{Name: "label_options", Description: "For some assets' labels: which templates suit them, which printers each can print on, and why the others cannot. Offer only what will print.",
			Annotations: readOnly("Label options")}, []string{"POST /api/v1/labels/options"}, read,
			func(ctx context.Context, e *env, in struct {
				Assets []string `json:"assets" jsonschema:"by ID or code"`
			}) (reply[map[string]any], error) {
				ids, err := e.assetIDs(ctx, in.Assets)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPost, "/api/v1/labels/options", map[string]any{"asset_ids": ids}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%d templates suit them.", count(out, "templates"))
			}),
		define(&mcp.Tool{Name: "print_labels", Description: "Print assets' labels now, on a printer, with a template; or use queue_labels to add them to the print queue.",
			Annotations: changes("Print labels", false)}, []string{"POST /api/v1/printers/{id}/print"}, printing,
			func(ctx context.Context, e *env, in struct {
				PrinterID  string   `json:"printer_id"`
				TemplateID string   `json:"template_id"`
				Assets     []string `json:"assets" jsonschema:"by ID or code, up to 50"`
				Copies     int      `json:"copies,omitempty"`
				Feed       string   `json:"feed,omitempty" jsonschema:"this time, which way labels that fit either way go through it: across (width across the roll), along, or auto"`
			}) (reply[map[string]any], error) {
				if err := checkItems(len(in.Assets)); err != nil {
					return reply[map[string]any]{}, err
				}
				ids, err := e.assetIDs(ctx, in.Assets)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPost, pathOf("/api/v1/printers/{id}/print", in.PrinterID), map[string]any{"template_id": in.TemplateID, "asset_ids": ids, "copies": in.Copies, "feed": in.Feed}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Sent %d labels to the printer.", len(ids))
			}),
		testPrint,

		// Template images.
		simple(&mcp.Tool{Name: "list_images", Description: "The images uploaded for label templates.", Annotations: readOnly("List images")}, "GET /api/v1/images", read,
			func(none) (request, error) { return request{path: "/api/v1/images"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d images.", count(out, "images")) }),
		define(&mcp.Tool{Name: "get_image", Description: "An image uploaded for label templates, and the image itself.", Annotations: readOnly("Get an image")},
			[]string{"GET /api/v1/images/{id}", "GET /api/v1/images/{id}/content"}, read,
			func(ctx context.Context, e *env, in idIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/images/{id}", in.ID), nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				res := e.api(ctx, http.MethodGet, pathOf("/api/v1/images/{id}/content", in.ID), nil, "")
				if res.Status != http.StatusOK {
					return reply[map[string]any]{}, apiError(res)
				}
				r, _ := say(out, "%q.", nameOf(out, "image"))
				r.extra = []mcp.Content{&mcp.ImageContent{Data: res.Body, MIMEType: res.Header.Get("Content-Type")}}
				return r, nil
			}),
		define(&mcp.Tool{Name: "upload_image", Description: "Upload a PNG or JPEG, base64-encoded, for label templates to place.",
			Annotations: changes("Upload an image", false)}, []string{"POST /api/v1/images"}, design,
			func(ctx context.Context, e *env, in addPhotoIn) (reply[map[string]any], error) {
				data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Data))
				if err != nil {
					return reply[map[string]any]{}, fmt.Errorf("data must be base64: %v", err)
				}
				name := in.Filename
				if name == "" {
					name = "image"
				}
				body, contentType, err := multipartBody("file", name, data, nil)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				res := e.api(ctx, http.MethodPost, "/api/v1/images", body, contentType)
				if res.Status < 200 || res.Status > 299 {
					return reply[map[string]any]{}, apiError(res)
				}
				out := map[string]any{}
				_ = json.Unmarshal(res.Body, &out)
				return say(out, "Uploaded %q.", nameOf(out, "image"))
			}),
		simple(&mcp.Tool{Name: "delete_image", Description: "Delete an uploaded image no template uses.", Annotations: destroys("Delete an image", true)}, "DELETE /api/v1/images/{id}", design,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/images/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the image." }),

		// Printers, rolls and pools.
		simple(&mcp.Tool{Name: "list_printer_drivers", Description: "The printer drivers: the models each supports, their rolls, and their options.",
			Annotations: readOnly("Printer drivers")}, "GET /api/v1/printers/drivers", seePrinters,
			func(none) (request, error) { return request{path: "/api/v1/printers/drivers"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d drivers.", count(out, "drivers")) }),
		simple(&mcp.Tool{Name: "list_printers", Description: "The label printers: model, how they are reached, and the roll loaded.",
			Annotations: readOnly("List printers")}, "GET /api/v1/printers", seePrinters,
			func(none) (request, error) { return request{path: "/api/v1/printers"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d printers.", count(out, "printers")) }),
		simple(&mcp.Tool{Name: "get_printer", Description: "One label printer.", Annotations: readOnly("Get a printer")}, "GET /api/v1/printers/{id}", seePrinters,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/printers/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "printer")) }),
		simple(&mcp.Tool{Name: "check_fit", Description: "Whether a label of a size prints on a printer as loaded, with the content's boxes in mm: why not, and what lies near the edge.",
			Annotations: readOnly("Check fit")}, "POST /api/v1/printers/{id}/fit", seePrinters,
			func(in fitIn) (request, error) {
				return request{path: pathOf("/api/v1/printers/{id}/fit", in.PrinterID), body: map[string]any{"width_mm": in.WidthMM, "height_mm": in.HeightMM, "dpi": in.DPI, "content": in.Content, "feed": in.Feed}}, need(in.PrinterID, "printer_id")
			},
			func(_ fitIn, out map[string]any) string {
				if fits, _ := out["fits"].(bool); fits {
					return fmt.Sprintf("It prints, fed with its width %s the roll.", out["feed"])
				}
				msg, _ := out["message"].(string)
				return msg
			}),
		define(&mcp.Tool{Name: "save_printer", Description: "Add a printer, or change one: only what is given changes. Changing one first says what it would affect (labels waiting for it), " +
			"and saves only with confirm.", Annotations: destroys("Save a printer", true)},
			[]string{"POST /api/v1/printers", "PUT /api/v1/printers/{id}", "POST /api/v1/printers/{id}/changes"}, settings,
			func(ctx context.Context, e *env, in struct {
				ID         string         `json:"id,omitempty" jsonschema:"the printer to change; empty to add one"`
				Name       *string        `json:"name,omitempty"`
				Driver     *string        `json:"driver,omitempty" jsonschema:"from list_printer_drivers"`
				Model      *string        `json:"model,omitempty"`
				Connection map[string]any `json:"connection,omitempty" jsonschema:"how it is reached, such as {\"type\": \"tcp\", \"address\": \"10.0.0.5:9100\"}"`
				Media      *string        `json:"media,omitempty" jsonschema:"the roll loaded: one of the driver's, or label stock by ID"`
				Options    map[string]any `json:"options,omitempty"`
				Confirm    bool           `json:"confirm,omitempty" jsonschema:"save a change even though it affects labels waiting"`
			}) (reply[map[string]any], error) {
				c := given(map[string]any{"name": in.Name, "driver": in.Driver, "model": in.Model, "media": in.Media})
				if in.Connection != nil {
					c["connection"] = in.Connection
				}
				if in.Options != nil {
					c["options"] = in.Options
				}
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/printers", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Added the printer %q.", nameOf(out, "printer"))
				}
				var got struct {
					Printer map[string]any `json:"printer"`
				}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/printers/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Printer, c)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				// What it would affect: labels waiting that would no longer
				// fit, categories no printer would suit, pools loaded unalike.
				var affects struct {
					Changes struct {
						Stranded   []any `json:"stranded"`
						Categories []any `json:"categories"`
						Pools      []any `json:"pools"`
					} `json:"changes"`
				}
				if err := e.call(ctx, http.MethodPost, pathOf("/api/v1/printers/{id}/changes", in.ID), body, &affects); err != nil {
					return reply[map[string]any]{}, err
				}
				hit := affects.Changes
				if n := len(hit.Stranded) + len(hit.Categories) + len(hit.Pools); n > 0 && !in.Confirm {
					out["changes"] = hit
					return say(out, "Not saved: it would leave %d groups of waiting labels where they no longer fit, %d categories with no printer for their labels, "+
						"and %d pools loaded unalike (listed under changes). Ask the person, and save again with confirm.", len(hit.Stranded), len(hit.Categories), len(hit.Pools))
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/printers/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the printer %q.", nameOf(out, "printer"))
			}),
		simple(&mcp.Tool{Name: "delete_printer", Description: "Delete a printer.", Annotations: destroys("Delete a printer", true)}, "DELETE /api/v1/printers/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/printers/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the printer." }),
		simple(&mcp.Tool{Name: "list_label_stock", Description: "The custom rolls printers can be loaded with: width, label length and gap, in mm.",
			Annotations: readOnly("List label stock")}, "GET /api/v1/label-stock", seePrinters,
			func(none) (request, error) { return request{path: "/api/v1/label-stock"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d rolls.", count(out, "stock")) }),
		define(&mcp.Tool{Name: "save_label_stock", Description: "Add a custom roll, or change one: name, width, label length (0 for continuous) and gap, in mm. Only what is given changes.",
			Annotations: changes("Save label stock", true)}, []string{"POST /api/v1/label-stock", "PUT /api/v1/label-stock/{id}"}, settings,
			func(ctx context.Context, e *env, in struct {
				ID       string   `json:"id,omitempty"`
				Name     *string  `json:"name,omitempty"`
				WidthMM  *float64 `json:"width_mm,omitempty"`
				LengthMM *float64 `json:"length_mm,omitempty"`
				GapMM    *float64 `json:"gap_mm,omitempty"`
			}) (reply[map[string]any], error) {
				c := map[string]any{}
				for k, v := range map[string]*float64{"width_mm": in.WidthMM, "length_mm": in.LengthMM, "gap_mm": in.GapMM} {
					if v != nil {
						c[k] = *v
					}
				}
				if in.Name != nil {
					c["name"] = *in.Name
				}
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/label-stock", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Added the roll.")
				}
				var list struct {
					Stock []map[string]any `json:"stock"`
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/label-stock", nil, &list); err != nil {
					return reply[map[string]any]{}, err
				}
				var current map[string]any
				for _, s := range list.Stock {
					if s["id"] == in.ID {
						current = s
					}
				}
				if current == nil {
					return reply[map[string]any]{}, fmt.Errorf("no roll has the ID %q", in.ID)
				}
				body, _ := merged(current, c)
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/label-stock/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the roll.")
			}),
		simple(&mcp.Tool{Name: "delete_label_stock", Description: "Delete a custom roll no printer has loaded.", Annotations: destroys("Delete label stock", true)}, "DELETE /api/v1/label-stock/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/label-stock/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the roll." }),
		simple(&mcp.Tool{Name: "list_printer_pools", Description: "Printer pools: printers that share the print queue's jobs.", Annotations: readOnly("List printer pools")}, "GET /api/v1/printer-pools", seePrinters,
			func(none) (request, error) { return request{path: "/api/v1/printer-pools"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d pools.", count(out, "pools")) }),
		simple(&mcp.Tool{Name: "get_printer_pool", Description: "One printer pool.", Annotations: readOnly("Get a printer pool")}, "GET /api/v1/printer-pools/{id}", seePrinters,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/printer-pools/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "pool")) }),
		define(&mcp.Tool{Name: "save_printer_pool", Description: "Make a printer pool, or replace one: its name, batch size, whether shares are fixed, and its members.",
			Annotations: changes("Save a printer pool", true)}, []string{"POST /api/v1/printer-pools", "PUT /api/v1/printer-pools/{id}"}, settings,
			func(ctx context.Context, e *env, in struct {
				ID          string           `json:"id,omitempty"`
				Name        string           `json:"name"`
				BatchSize   int              `json:"batch_size,omitempty"`
				FixedShares bool             `json:"fixed_shares,omitempty"`
				Members     []map[string]any `json:"members" jsonschema:"each {printer_id, share}"`
			}) (reply[map[string]any], error) {
				body := map[string]any{"name": in.Name, "batch_size": in.BatchSize, "fixed_shares": in.FixedShares, "members": in.Members}
				method, path := http.MethodPost, "/api/v1/printer-pools"
				if in.ID != "" {
					method, path = http.MethodPut, pathOf("/api/v1/printer-pools/{id}", in.ID)
				}
				out := map[string]any{}
				if err := e.call(ctx, method, path, body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the pool %q.", nameOf(out, "pool"))
			}),
		simple(&mcp.Tool{Name: "delete_printer_pool", Description: "Delete a printer pool.", Annotations: destroys("Delete a printer pool", true)}, "DELETE /api/v1/printer-pools/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/printer-pools/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the pool." }),
	}
}

func orPlaceholder(asset string) string {
	if asset == "" {
		return "The label with placeholders"
	}
	return asset
}

// fileContent is a file an API answered, for a client to keep: text as
// text, anything else as a blob.
func fileContent(uri string, res Response) mcp.Content {
	mime := res.Header.Get("Content-Type")
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	if strings.HasPrefix(mime, "image/png") {
		return &mcp.ImageContent{Data: res.Body, MIMEType: mime}
	}
	if strings.HasPrefix(mime, "text/") || mime == "image/svg+xml" {
		return &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: uri, MIMEType: mime, Text: string(res.Body)}}
	}
	return &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: uri, MIMEType: mime, Blob: res.Body}}
}

// assetIDs finds the assets references name.
func (e *env) assetIDs(ctx context.Context, refs []string) ([]string, error) {
	ids := []string{}
	for _, r := range refs {
		id, err := e.assetID(ctx, r)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
