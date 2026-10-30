package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Icons (README §3.7): an assistant can read, check and write isolex
// sources, and is told exactly what the compiler said.

type categoryIconIn struct {
	CategoryID string `json:"category_id"`
	Rendition  string `json:"rendition,omitempty" jsonschema:"which of the category's icons: screen (the default), drawn everywhere on screen, or label, drawn on labels in its place"`
	IconName   string `json:"icon_name" jsonschema:"the icon's name: 1 to 64 letters, digits, -, _ and ., not starting with .; set by the first save, and the same for every later one; unique across renditions, by convention <icon>-label for a label icon"`
	Source     string `json:"source" jsonschema:"isolex source (KDL 2.0) that draws an icon by that name; it may extend others' icons and use the library's bases and fragments"`
}

type categoryIDIn struct {
	CategoryID string `json:"category_id"`
	Rendition  string `json:"rendition,omitempty" jsonschema:"which of the category's icons: screen (the default), drawn everywhere on screen, or label, drawn on labels in its place"`
}

type renameIconIn struct {
	CategoryID string `json:"category_id"`
	Rendition  string `json:"rendition,omitempty" jsonschema:"which of the category's icons: screen (the default), drawn everywhere on screen, or label, drawn on labels in its place"`
	IconName   string `json:"icon_name" jsonschema:"the new name, which the published source must already draw"`
}

// withRendition is a category icon route's path for rendition, the
// screen's when empty.
func withRendition(path, rendition string) string {
	if rendition == "" {
		return path
	}
	return path + "?" + url.Values{"rendition": {rendition}}.Encode()
}

type libraryFileIn struct {
	ID     string `json:"id,omitempty" jsonschema:"the file to change; empty to add one"`
	Path   string `json:"path,omitempty" jsonschema:"a new file's path, such as shared/racks.ilx; to move one, use rename_icon_library_file"`
	Source string `json:"source" jsonschema:"isolex source (KDL 2.0): bases, fragments and axes icons share"`
}

type libraryPathIn struct {
	ID   string `json:"id"`
	Path string `json:"path" jsonschema:"such as shared/racks.ilx"`
}

type iconIRIn struct {
	Name string `json:"name,omitempty" jsonschema:"one icon's name; empty for every icon, which may be long"`
}

// saveSaid tells what a save came to: published, or a draft and why.
func saveSaid(subject string, out map[string]any) string {
	save, _ := out["save"].(map[string]any)
	if published, _ := save["published"].(bool); published {
		return subject + " is published."
	}
	return subject + " is kept as a draft and not published, because it does not compile with the rest of the icons: " + diagsSaid(save["errors"]) +
		". Fix it and save again, or discard the draft."
}

// diagsSaid lists diagnostics as file:line:col: message.
func diagsSaid(v any) string {
	list, _ := v.([]any)
	var out []string
	for _, d := range list {
		m, _ := d.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v:%v: %v", m["file"], m["line"], m["col"], m["message"]))
	}
	if len(out) == 0 {
		return "no errors"
	}
	return strings.Join(out, "; ")
}

func iconTools() []tool {
	read := []string{rbac.PermAssetsRead}
	manage := []string{rbac.PermCategoriesManage}
	icons := []string{rbac.PermIconsManage}
	return []tool{
		define(&mcp.Tool{Name: "icons_overview", Description: "How icons stand: whether this server can compile them, the current build (its icons, by name and isolex category, and its warnings), " +
			"the palette (the tokens icons may paint with, besides category, which paints in the category's color), the lexicon settings, " +
			"the variant each asset state is drawn with (an asset in a state is drawn as <icon>-<variant> when the build draws that), " +
			"and the icon each category is drawn with for each rendition (rendition_icons: screen, everywhere on screen, and label, on labels). " +
			"Icons are written in isolex, a KDL 2.0 language for isometric icons; the guide resource explains how bam uses it.",
			Annotations: readOnly("Icons overview")},
			[]string{"GET /api/v1/icons", "GET /api/v1/icons/palette", "GET /api/v1/icons/settings", "GET /api/v1/icons/states"}, read,
			func(ctx context.Context, e *env, _ none) (reply[map[string]any], error) {
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, "/api/v1/icons", nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				var palette, settings, states map[string]any
				if err := e.call(ctx, http.MethodGet, "/api/v1/icons/palette", nil, &palette); err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/icons/settings", nil, &settings); err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/icons/states", nil, &states); err != nil {
					return reply[map[string]any]{}, err
				}
				out["palette"], out["settings"] = palette["palette"], settings["settings"]
				out["state_variants"], out["states"] = states["variants"], states["states"]
				// for pages to draw; draw_icon draws it for an assistant
				delete(out, "unknown")
				build, _ := out["build"].(map[string]any)
				n := 0
				if list, ok := build["icons"].([]any); ok {
					n = len(list)
				}
				text := fmt.Sprintf("%d icons built.", n)
				if available, _ := out["available"].(bool); !available {
					text = fmt.Sprintf("%d icons built; this server cannot compile icons, so they cannot change.", n)
				}
				if missing, _ := out["missing_icons"].([]any); len(missing) > 0 {
					text += fmt.Sprintf(" %d icon names that assets have as their own are not drawn by the build (missing_icons); those assets show the unknown icon.", len(missing))
				}
				return say(out, "%s", text)
			}),
		define(&mcp.Tool{Name: "get_icon_ir", Description: "The current build's shape IR: each icon as polygons, paths and other shapes, every color a palette token. " +
			"Give a name for one icon; every icon at once may be long.",
			Annotations: readOnly("Get icon IR")}, []string{"GET /api/v1/icons/ir"}, read,
			func(ctx context.Context, e *env, in iconIRIn) (reply[map[string]any], error) {
				var doc struct {
					Icons []json.RawMessage `json:"icons"`
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/icons/ir", nil, &doc); err != nil {
					return reply[map[string]any]{}, err
				}
				if in.Name == "" {
					return say(map[string]any{"icons": doc.Icons}, "%d icons.", len(doc.Icons))
				}
				for _, raw := range doc.Icons {
					var icon struct{ Name string }
					if json.Unmarshal(raw, &icon) == nil && icon.Name == in.Name {
						return say(map[string]any{"icon": raw}, "The IR of %q.", in.Name)
					}
				}
				return reply[map[string]any]{}, fmt.Errorf("no icon named %q in the current build", in.Name)
			}),
		define(&mcp.Tool{Name: "draw_icon", Description: "Draw an icon as a PNG, to show the person: by name, a category's (its own or inherited) or an asset's (its own or its categories'), for a rendition. " +
			"A name the build does not draw comes back as the unknown icon, a question mark.",
			Annotations: readOnly("Draw an icon")}, []string{"GET /api/v1/icons/image"}, read,
			func(ctx context.Context, e *env, in drawIconIn) (reply[drawIconOut], error) {
				q := url.Values{"format": {"png"}}
				given := 0
				if in.Name != "" {
					q.Set("name", in.Name)
					given++
				}
				if in.Category != "" {
					q.Set("category", in.Category)
					given++
				}
				if in.Asset != "" {
					id, err := e.assetID(ctx, in.Asset)
					if err != nil {
						return reply[drawIconOut]{}, err
					}
					q.Set("asset", id)
					given++
				}
				if given != 1 {
					return reply[drawIconOut]{}, fmt.Errorf("give one of name, category or asset")
				}
				if in.Rendition != "" {
					if in.Name != "" {
						return reply[drawIconOut]{}, fmt.Errorf("a rendition is for a category's or an asset's icon; an icon by name is the one named")
					}
					q.Set("rendition", in.Rendition)
				}
				if in.Dark {
					q.Set("theme", "dark")
				}
				if in.Height != 0 {
					q.Set("height", strconv.Itoa(in.Height))
				}
				res := e.api(ctx, http.MethodGet, "/api/v1/icons/image?"+q.Encode(), nil, "")
				if res.Status != http.StatusOK {
					return reply[drawIconOut]{}, apiError(res)
				}
				out := drawIconOut{Missing: res.Header.Get("X-Icon-Missing")}
				r, _ := say(out, "The icon.")
				if out.Missing != "" {
					r, _ = say(out, "No icon is drawn by the name %q, so this is the unknown icon.", out.Missing)
				}
				r.extra = []mcp.Content{&mcp.ImageContent{Data: res.Body, MIMEType: "image/png"}}
				return r, nil
			}),
		define(&mcp.Tool{Name: "list_icon_sources", Description: "Every icon source: each category's, for each rendition it has one for (its rendition, its icon's name, published source, and any draft that did not compile, with its errors) " +
			"and the library's shared files.",
			Annotations: readOnly("List icon sources")}, []string{"GET /api/v1/category-icons", "GET /api/v1/icon-library"}, read,
			func(ctx context.Context, e *env, _ none) (reply[map[string]any], error) {
				var icons, files map[string]any
				if err := e.call(ctx, http.MethodGet, "/api/v1/category-icons", nil, &icons); err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/icon-library", nil, &files); err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{"category_icons": icons["icons"], "library": files["files"]}
				return say(out, "%d category icons and %d library files.", count(icons, "icons"), count(files, "files"))
			}),
		define(&mcp.Tool{Name: "get_category_icon", Description: "A category's icon for a rendition (screen unless given): its name, published source, any draft with its errors, every published version, newest first, " +
			"and its params bound to asset fields (params).",
			Annotations: readOnly("Get a category's icon")},
			[]string{"GET /api/v1/categories/{id}/icon", "GET /api/v1/categories/{id}/icon/revisions", "GET /api/v1/categories/{id}/icon/params"}, read,
			func(ctx context.Context, e *env, in categoryIDIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, withRendition(pathOf("/api/v1/categories/{id}/icon", in.CategoryID), in.Rendition), nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				var revs map[string]any
				if err := e.call(ctx, http.MethodGet, withRendition(pathOf("/api/v1/categories/{id}/icon/revisions", in.CategoryID), in.Rendition), nil, &revs); err != nil {
					return reply[map[string]any]{}, err
				}
				out["revisions"] = revs["revisions"]
				var params map[string]any
				if err := e.call(ctx, http.MethodGet, withRendition(pathOf("/api/v1/categories/{id}/icon/params", in.CategoryID), in.Rendition), nil, &params); err != nil {
					return reply[map[string]any]{}, err
				}
				out["params"] = params["params"]
				return say(out, "%d published versions.", count(revs, "revisions"))
			}),
		simple(&mcp.Tool{Name: "check_category_icon", Description: "Compile the icons with this source as a category's icon, and keep nothing: the errors and warnings, and the IR if it is clean. " +
			"Check as often as you like before saving.",
			Annotations: readOnly("Check a category's icon")}, "POST /api/v1/categories/{id}/icon/check", manage,
			func(in categoryIconIn) (request, error) {
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon/check", in.CategoryID), in.Rendition), body: map[string]string{"icon_name": in.IconName, "source": in.Source}}, need(in.CategoryID, "category_id")
			},
			func(_ categoryIconIn, out map[string]any) string {
				check, _ := out["check"].(map[string]any)
				if list, _ := check["errors"].([]any); len(list) > 0 {
					return "It does not compile: " + diagsSaid(list) + "."
				}
				return "It compiles."
			}),
		simple(&mcp.Tool{Name: "save_category_icon", Description: "Save a category's icon source, for a rendition: screen (the default), or label, drawn on labels in place of the screen icon, " +
			"best flat line art that prints clearly in black and white. It is published if the icons compile with it and it draws an icon by its name; " +
			"otherwise it is kept as a draft, with the errors, and the published icon stays.",
			Annotations: changes("Save a category's icon", true)}, "PUT /api/v1/categories/{id}/icon", manage,
			func(in categoryIconIn) (request, error) {
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon", in.CategoryID), in.Rendition), body: map[string]string{"icon_name": in.IconName, "source": in.Source}}, need(in.CategoryID, "category_id")
			},
			func(in categoryIconIn, out map[string]any) string {
				return saveSaid(fmt.Sprintf("%q", in.IconName), out)
			}),
		simple(&mcp.Tool{Name: "rename_category_icon", Description: "Rename a category's icon. Refused if any other icon still names it by its old name, " +
			"or the published source does not draw it by the new one.",
			Annotations: changes("Rename a category's icon", true)}, "POST /api/v1/categories/{id}/icon/rename", manage,
			func(in renameIconIn) (request, error) {
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon/rename", in.CategoryID), in.Rendition), body: map[string]string{"icon_name": in.IconName}}, need(in.CategoryID, "category_id")
			},
			func(in renameIconIn, _ map[string]any) string { return fmt.Sprintf("Renamed it %q.", in.IconName) }),
		simple(&mcp.Tool{Name: "bind_icon_params", Description: "Bind params of a category's icon, its own or inherited, to what each asset has, replacing those bound: " +
			"a number param to a number field by ID, so a server with 2 in Rack units is drawn 2 units high; a text param to \"name\" (the asset's name), " +
			"\"code\" (its first code) or a field by ID, so a tag is written with the asset's own name. " +
			"An asset with no value there is drawn with the param's default. Refused for a param the icon does not have, or bound to the other kind. " +
			"Bind none to unbind them all.",
			Annotations: changes("Bind an icon's params", true)}, "PUT /api/v1/categories/{id}/icon/params", manage,
			func(in iconParamsIn) (request, error) {
				params := in.Params
				if params == nil {
					params = map[string]string{}
				}
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon/params", in.CategoryID), in.Rendition), body: map[string]any{"params": params}}, need(in.CategoryID, "category_id")
			},
			func(_ iconParamsIn, out map[string]any) string {
				return fmt.Sprintf("%d params bound.", count(out, "params"))
			}),
		simple(&mcp.Tool{Name: "delete_category_icon", Description: "Delete a category's icon for a rendition (screen unless given) and its history. Refused if another icon uses it.",
			Annotations: destroys("Delete a category's icon", true)}, "DELETE /api/v1/categories/{id}/icon", manage,
			func(in categoryIDIn) (request, error) {
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon", in.CategoryID), in.Rendition)}, need(in.CategoryID, "category_id")
			},
			func(categoryIDIn, map[string]any) string { return "Deleted the icon." }),
		simple(&mcp.Tool{Name: "discard_category_icon_draft", Description: "Drop a category icon's draft, keeping what is published. An icon never published goes with it.",
			Annotations: destroys("Discard an icon draft", true)}, "DELETE /api/v1/categories/{id}/icon/draft", manage,
			func(in categoryIDIn) (request, error) {
				return request{path: withRendition(pathOf("/api/v1/categories/{id}/icon/draft", in.CategoryID), in.Rendition)}, need(in.CategoryID, "category_id")
			},
			func(categoryIDIn, map[string]any) string { return "Discarded the draft." }),
		define(&mcp.Tool{Name: "get_icon_library_file", Description: "A library file: its path, published source, any draft with its errors, and every published version, newest first.",
			Annotations: readOnly("Get an icon library file")},
			[]string{"GET /api/v1/icon-library/{id}", "GET /api/v1/icon-library/{id}/revisions"}, read,
			func(ctx context.Context, e *env, in idIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/icon-library/{id}", in.ID), nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				var revs map[string]any
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/icon-library/{id}/revisions", in.ID), nil, &revs); err != nil {
					return reply[map[string]any]{}, err
				}
				out["revisions"] = revs["revisions"]
				return say(out, "%d published versions.", count(revs, "revisions"))
			}),
		define(&mcp.Tool{Name: "save_icon_library_file", Description: "Add a library file, or change one's source: published if the icons compile with it, a draft with the errors otherwise.",
			Annotations: changes("Save an icon library file", true)},
			[]string{"POST /api/v1/icon-library", "PUT /api/v1/icon-library/{id}"}, icons,
			func(ctx context.Context, e *env, in libraryFileIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Path == "" {
						return reply[map[string]any]{}, fmt.Errorf("a new library file needs a path")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/icon-library", map[string]string{"path": in.Path, "source": in.Source}, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "%s", saveSaid(in.Path, out))
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/icon-library/{id}", in.ID), map[string]string{"source": in.Source}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%s", saveSaid("The file", out))
			}),
		simple(&mcp.Tool{Name: "rename_icon_library_file", Description: "Move a library file to another path, which changes only how errors name it.",
			Annotations: changes("Move an icon library file", true)}, "POST /api/v1/icon-library/{id}/rename", icons,
			func(in libraryPathIn) (request, error) {
				return request{path: pathOf("/api/v1/icon-library/{id}/rename", in.ID), body: map[string]string{"path": in.Path}}, need(in.ID, "id")
			},
			func(in libraryPathIn, _ map[string]any) string { return "Moved it to " + in.Path + "." }),
		simple(&mcp.Tool{Name: "delete_icon_library_file", Description: "Delete a library file and its history. Refused if any icon still uses what it defines.",
			Annotations: destroys("Delete an icon library file", true)}, "DELETE /api/v1/icon-library/{id}", icons,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/icon-library/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the file." }),
		simple(&mcp.Tool{Name: "discard_icon_library_draft", Description: "Drop a library file's draft, keeping what is published. A file never published goes with it.",
			Annotations: destroys("Discard a library draft", true)}, "DELETE /api/v1/icon-library/{id}/draft", icons,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/icon-library/{id}/draft", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Discarded the draft." }),
		simple(&mcp.Tool{Name: "set_icon_palette", Description: "Replace the palette: the whole list of tokens, in order, each with a light and a dark color (#rgb, #rrggbb or #rrggbbaa). " +
			"Tokens are what icons may paint with, so taking away one an icon uses is refused.",
			Annotations: changes("Set the icon palette", true)}, "PUT /api/v1/icons/palette", icons,
			func(in struct {
				Palette []models.IconColor `json:"palette" jsonschema:"the whole palette, in order"`
			}) (request, error) {
				if in.Palette == nil {
					return request{}, fmt.Errorf("palette is required")
				}
				return request{path: "/api/v1/icons/palette", body: map[string]any{"palette": in.Palette}}, nil
			},
			func(_ struct {
				Palette []models.IconColor `json:"palette" jsonschema:"the whole palette, in order"`
			}, out map[string]any) string {
				return fmt.Sprintf("The palette has %d tokens.", count(out, "palette"))
			}),
		simple(&mcp.Tool{Name: "set_icon_settings", Description: "Replace the lexicon settings: the stroke widths icons may draw at (none for any), the tokens only an icon that grants them may paint with, " +
			"and the compiler's limits (shape-limit, repeat-limit, insert-depth, icon-limit, expression-depth; 0 for no limit). Refused if the icons would not compile under them.",
			Annotations: changes("Set the icon settings", true)}, "PUT /api/v1/icons/settings", icons,
			func(in models.IconSettings) (request, error) {
				return request{path: "/api/v1/icons/settings", body: in}, nil
			},
			func(models.IconSettings, map[string]any) string { return "Saved the icon settings." }),
		simple(&mcp.Tool{Name: "set_icon_states", Description: "Replace the variant each asset state is drawn with: out_of_service, needs_maintenance, overdue, on_loan, out_of_stock and low_stock, " +
			"worst first. An asset in a state is drawn as <icon>-<variant>, an isolex axis variant, when the build draws it, and as its plain icon otherwise; " +
			"one in several states shows the worst its icon has. An empty variant, or a state left out, draws none.",
			Annotations: changes("Set the icon states", true)}, "PUT /api/v1/icons/states", icons,
			func(in iconStatesIn) (request, error) {
				return request{path: "/api/v1/icons/states", body: in}, nil
			},
			func(iconStatesIn, map[string]any) string { return "Saved the icon states." }),
	}
}

type iconParamsIn struct {
	CategoryID string            `json:"category_id" jsonschema:"the category whose own icon it is"`
	Rendition  string            `json:"rendition,omitempty" jsonschema:"which of the category's icons: screen (the default) or label"`
	Params     map[string]string `json:"params,omitempty" jsonschema:"each param's name, to what sets it: a field's ID, or for a text param name or code"`
}

type iconStatesIn struct {
	Variants map[string]string `json:"variants" jsonschema:"each state's variant name by state, such as down for out_of_service"`
}

type drawIconIn struct {
	Name     string `json:"name,omitempty" jsonschema:"an icon by name, as the build draws it"`
	Category string `json:"category,omitempty" jsonschema:"a category by ID: the icon it is drawn with"`
	Asset    string `json:"asset,omitempty" jsonschema:"an asset by ID or code: the icon it is drawn with"`
	// a category's or an asset's icon for a rendition
	Rendition string `json:"rendition,omitempty" jsonschema:"for a category or an asset, the icon for screen (the default) or label; one with no label icon is drawn on labels with its screen icon"`
	Height    int    `json:"height,omitempty" jsonschema:"how high, in pixels, 1 to 1024; 64 unless given"`
	Dark      bool   `json:"dark,omitempty" jsonschema:"draw it in the palette's dark colors"`
}

type drawIconOut struct {
	Missing string `json:"missing,omitempty" jsonschema:"set to the name when the build draws no icon by it, so the unknown icon was drawn"`
}
