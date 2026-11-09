package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchIn struct {
	Search      string `json:"search,omitempty" jsonschema:"words to find in an asset's name, its codes, or the name of a place it is in at any depth"`
	CategoryID  string `json:"category_id,omitempty" jsonschema:"only assets in this category or below it"`
	Condition   string `json:"condition,omitempty" jsonschema:"ok, needs_maintenance, out_of_service, or problem for either of the last two"`
	Loan        string `json:"loan,omitempty" jsonschema:"open for assets lent out, overdue for those past due"`
	Icon        string `json:"icon,omitempty" jsonschema:"only assets with this icon as their own, by name, such as one in icons_overview's missing_icons"`
	VendorID    string `json:"vendor_id,omitempty" jsonschema:"only assets bought from this vendor (list_vendors gives their IDs)"`
	Review      string `json:"review,omitempty" jsonschema:"in categories using review states: draft, confirmed or verified; due or overdue for review; or proposed, with an assistant's changes waiting"`
	RunningLow  bool   `json:"running_low,omitempty" jsonschema:"only assets flagged running low, themselves or by a category"`
	Archived    bool   `json:"archived,omitempty" jsonschema:"list archived assets instead of active ones"`
	Favorite    bool   `json:"favorite,omitempty" jsonschema:"only assets the person you act for has starred"`
	Person      string `json:"person,omitempty" jsonschema:"only assets whose person fields name this person, by user ID, or me for the person you act for"`
	PersonField string `json:"person_field,omitempty" jsonschema:"with person, only this person field, by key"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many to return, 1 to 200 (25 if not given)"`
	Offset      int    `json:"offset,omitempty" jsonschema:"how many to skip, to page through"`
}

type searchOut struct {
	Total  int            `json:"total" jsonschema:"how many assets match"`
	Assets []assetSummary `json:"assets"`
	More   int            `json:"more" jsonschema:"how many more match after these; call again with a larger offset"`
}

func (in searchIn) query() string {
	q := url.Values{}
	for k, v := range map[string]string{"search": in.Search, "category_id": in.CategoryID, "condition": in.Condition, "loan": in.Loan, "icon": in.Icon, "vendor_id": in.VendorID,
		"person": in.Person, "person_field": in.PersonField, "review": in.Review} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if in.Archived {
		q.Set("archived", "true")
	}
	if in.Favorite {
		q.Set("favorite", "true")
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

type graphIn struct {
	Asset      string   `json:"asset,omitempty" jsonschema:"the asset whose links to follow: its ID, or one of its codes; leave it out for a map"`
	CategoryID string   `json:"category_id,omitempty" jsonschema:"for a map: the assets in this category or below it"`
	Search     string   `json:"search,omitempty" jsonschema:"for a map: the assets whose name or codes hold these words"`
	Types      []string `json:"types,omitempty" jsonschema:"the followed link types to follow, by ID (list_link_types marks them is_followed); all of them if not given"`
	Direction  string   `json:"direction,omitempty" jsonschema:"from an asset: out for what it needs, in for what needs it, or both (the default)"`
	Depth      int      `json:"depth,omitempty" jsonschema:"from an asset: how many hops to follow, 1 to 10 (5 if not given)"`
}

type assetIn struct {
	Asset string `json:"asset" jsonschema:"the asset's ID, or one of its codes"`
}

type historyIn struct {
	Asset  string `json:"asset" jsonschema:"the asset's ID, or one of its codes"`
	Before string `json:"before,omitempty" jsonschema:"an event's ID, for the events before it: the next page"`
}

type historyOut struct {
	Events []map[string]any `json:"events" jsonschema:"newest first: each has a kind, when, the actor (with via, the assistant it came through) and what changed"`
	More   bool             `json:"more" jsonschema:"whether there are older events; pass the last event's ID as before"`
}

type loansIn struct {
	State      string `json:"state,omitempty" jsonschema:"open, overdue, or empty for every loan, returned ones too"`
	BorrowerID string `json:"borrower_id,omitempty" jsonschema:"only this person's loans"`
	Limit      int    `json:"limit,omitempty" jsonschema:"how many to return, 1 to 200 (25 if not given)"`
	Offset     int    `json:"offset,omitempty"`
}

type loansOut struct {
	Total int           `json:"total"`
	Loans []models.Loan `json:"loans" jsonschema:"newest first"`
	More  int           `json:"more"`
}

type peopleOut struct {
	People []personView `json:"people" jsonschema:"the active users a loan can name"`
}

type personView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty" jsonschema:"given only to tell apart two people of the same name"`
}

type nextCodeIn struct {
	Type string `json:"type" jsonschema:"a code type: qr, micro_qr or code128"`
}

type nextCodeOut struct {
	Code string `json:"code" jsonschema:"one more than the highest code of that type ending in a number, or empty when none does"`
}

type exportOut struct {
	Rows  int `json:"rows" jsonschema:"assets in the file"`
	Bytes int `json:"bytes"`
}

func findingTools() []tool {
	return []tool{
		define(&mcp.Tool{
			Name: "search_assets",
			Description: "Find assets: by words in their name, codes or the place they are in, by category, condition, loan, stock or review state. " +
				"Each comes with its codes, where it is, its condition, stock and loan. Use get_asset for everything about one.",
			Annotations: readOnly("Search assets"),
		}, []string{"GET /api/v1/assets"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in searchIn) (reply[searchOut], error) {
			var res struct {
				Assets []models.AssetWithDetails `json:"assets"`
			}
			if err := e.call(ctx, http.MethodGet, "/api/v1/assets"+in.query(), nil, &res); err != nil {
				return reply[searchOut]{}, err
			}
			found := res.Assets
			if in.RunningLow {
				found = found[:0]
				for i := range res.Assets {
					if runningLow(&res.Assets[i]) {
						found = append(found, res.Assets[i])
					}
				}
			}
			shown, more, err := page(found, in.Offset, in.Limit)
			if err != nil {
				return reply[searchOut]{}, err
			}
			out := searchOut{Total: len(found), More: more, Assets: []assetSummary{}}
			base := e.baseURL(ctx)
			var names []string
			for i := range shown {
				s := summarize(&shown[i], base)
				out.Assets = append(out.Assets, s)
				if len(names) < 10 {
					names = append(names, describe(s))
				}
			}
			text := fmt.Sprintf("%d assets match", len(found))
			if len(names) > 0 {
				text += ": " + strings.Join(names, ", ")
				if len(found) > len(names) {
					text += ", …"
				}
			}
			return say(out, "%s.", text)
		}),
		define(&mcp.Tool{
			Name: "get_asset",
			Description: "Everything about one asset, by its ID or any of its codes: its fields, codes, categories, where it is and what is in it, " +
				"its links, condition, stock, loan, photos, the hooks it offers, the icon it is drawn with, and where it came from: " +
				"its vendor, dates, warranty end, and, with purchases.read, its price, order number and invoice.",
			Annotations: readOnly("Get an asset"),
		}, []string{"GET /api/v1/assets/{id}", "GET /api/v1/scan/{code}"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in assetIn) (reply[assetDetail], error) {
			a, err := e.asset(ctx, in.Asset)
			if err != nil {
				return reply[assetDetail]{}, err
			}
			d, err := e.detail(ctx, a)
			if err != nil {
				return reply[assetDetail]{}, err
			}
			text := describe(d.assetSummary)
			if d.Where != "" {
				text += ", in " + d.Where
			}
			if d.Condition != "ok" {
				text += ", " + strings.ReplaceAll(d.Condition, "_", " ")
			}
			if d.Loan != nil {
				text += ", lent out"
			}
			return say(d, "%s. Its page: %s", text, d.URL)
		}),
		define(&mcp.Tool{
			Name: "get_scene",
			Description: "What is in a location, as its scene lays it out: each asset in it directly, where it stands on the floor in tiles " +
				"(x, y, z, and the room it takes, size), its code, condition, icon, and how many things are in it (holds), drawn back to front; " +
				"and the way down to the location, outermost first. Contents are packed in rows, by name, around those placed (arranged); " +
				"overlaps lists the IDs of the others one shares space with.",
			Annotations: readOnly("Get a scene"),
		}, []string{"GET /api/v1/assets/{id}/scene"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in assetIn) (reply[map[string]any], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[map[string]any]{}, err
			}
			out := map[string]any{}
			if err := e.call(ctx, http.MethodGet, assetPath(id, "/scene"), nil, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			// for drawing it, which a person's page does
			delete(out, "viewbox")
			items, _ := out["items"].([]any)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					delete(m, "tx")
					delete(m, "ty")
					delete(m, "shapes")
				}
			}
			loc, _ := out["location"].(map[string]any)
			return say(out, "%d things in %v.", len(items), loc["name"])
		}),
		define(&mcp.Tool{
			Name: "asset_graph",
			Description: "Follow links through several hops: what an asset needs (direction out), what needs it (in: what stops working if it does), " +
				"or both, through the link types marked is_followed, such as depends on / needed by. Without an asset, the map of a category or a search: " +
				"its assets and the followed links among them. Each asset comes with its name, categories, icon, condition, whether it is archived, " +
				"and its hops from the asset; each link with its type, from the asset that needs to the one needed. At most 500 assets: past that, " +
				"truncated is set, at truncated_at hops, and cut marks the assets with links not followed. Assets the person may not read are only " +
				"counted (hidden), and never followed through. loops lists each loop's assets: following stops where it comes back round.",
			Annotations: readOnly("Follow links"),
		}, []string{"GET /api/v1/assets/{id}/graph", "GET /api/v1/assets/graph"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in graphIn) (reply[map[string]any], error) {
			q := url.Values{}
			if len(in.Types) > 0 {
				q.Set("types", strings.Join(in.Types, ","))
			}
			path := "/api/v1/assets/graph"
			if strings.TrimSpace(in.Asset) != "" {
				if in.CategoryID != "" || in.Search != "" {
					return reply[map[string]any]{}, fmt.Errorf("give an asset, or a category or search for a map, not both")
				}
				id, err := e.assetID(ctx, in.Asset)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				path = assetPath(id, "/graph")
				if in.Direction != "" {
					q.Set("direction", in.Direction)
				}
				if in.Depth != 0 {
					q.Set("depth", strconv.Itoa(in.Depth))
				}
			} else {
				if in.Direction != "" || in.Depth != 0 {
					return reply[map[string]any]{}, fmt.Errorf("direction and depth follow links from an asset: name one, or leave them out for a map")
				}
				if in.CategoryID != "" {
					q.Set("category_id", in.CategoryID)
				}
				if in.Search != "" {
					q.Set("search", in.Search)
				}
			}
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var res struct {
				Graph map[string]any `json:"graph"`
			}
			if err := e.call(ctx, http.MethodGet, path, nil, &res); err != nil {
				return reply[map[string]any]{}, err
			}
			out := res.Graph
			// where a person's page draws them
			assets, _ := out["assets"].([]any)
			for _, a := range assets {
				if m, ok := a.(map[string]any); ok {
					delete(m, "layer")
					delete(m, "row")
				}
			}
			links, _ := out["links"].([]any)
			text := fmt.Sprintf("%d assets and %d links", len(assets), len(links))
			if n, _ := out["hidden"].(float64); n > 0 {
				text += fmt.Sprintf("; %d not shown, which the person may not read", int(n))
			}
			if loops, _ := out["loops"].([]any); len(loops) > 0 {
				text += fmt.Sprintf("; %d loops", len(loops))
			}
			if cut, _ := out["truncated"].(bool); cut {
				text += "; cut short at 500 assets"
			}
			return say(out, "%s.", text)
		}),
		define(&mcp.Tool{
			Name: "render_scene",
			Description: "Draw a location's scene as a PNG, to show the person where things are: what is in it, standing where it stands, " +
				"each with its icon, as its page draws it. Give show, an asset in it, to outline that one: " +
				"\"where is the spare switch?\" is answered by its location's scene with it shown.",
			Annotations: readOnly("Draw a scene"),
		}, []string{"GET /api/v1/assets/{id}/scene/image"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in struct {
			Asset  string `json:"asset" jsonschema:"the location: an asset's ID, or one of its codes"`
			Show   string `json:"show,omitempty" jsonschema:"an asset in it to outline: its ID, or one of its codes"`
			Dark   bool   `json:"dark,omitempty" jsonschema:"draw it in the dark theme's colors"`
			Height int    `json:"height,omitempty" jsonschema:"how high to draw it, in pixels, 1 to 2048; 512 unless given"`
		}) (reply[map[string]any], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[map[string]any]{}, err
			}
			q := url.Values{"format": {"png"}}
			if in.Show != "" {
				shown, err := e.assetID(ctx, in.Show)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				q.Set("show", shown)
			}
			if in.Dark {
				q.Set("theme", "dark")
			}
			if in.Height != 0 {
				q.Set("height", strconv.Itoa(in.Height))
			}
			res := e.api(ctx, http.MethodGet, assetPath(id, "/scene/image")+"?"+q.Encode(), nil, "")
			if res.Status != http.StatusOK {
				return reply[map[string]any]{}, apiError(res)
			}
			r, _ := say(map[string]any{"location": id}, "The scene.")
			if in.Show != "" {
				r, _ = say(map[string]any{"location": id, "shown": q.Get("show")}, "The scene, with the one asked about outlined.")
			}
			r.extra = []mcp.Content{&mcp.ImageContent{Data: res.Body, MIMEType: "image/png"}}
			return r, nil
		}),
		define(&mcp.Tool{
			Name: "place_asset",
			Description: "Place an asset in its location's scene, x, y and z tiles from the floor's corner (get_scene), " +
				"or with clear, leave it to be packed with the rest again. Things may be placed on others, or even through them; " +
				"get_scene says what overlaps. Answers with the scene.",
			Annotations: changes("Place an asset", true),
		}, []string{"PUT /api/v1/assets/{id}/placement", "DELETE /api/v1/assets/{id}/placement"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in struct {
			Asset string  `json:"asset" jsonschema:"the asset's ID, or one of its codes"`
			X     float64 `json:"x,omitempty" jsonschema:"tiles along the floor's width"`
			Y     float64 `json:"y,omitempty" jsonschema:"tiles along the floor's depth"`
			Z     float64 `json:"z,omitempty" jsonschema:"tiles above the floor"`
			Clear bool    `json:"clear,omitempty" jsonschema:"pack it with the rest again"`
		}) (reply[map[string]any], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[map[string]any]{}, err
			}
			out := map[string]any{}
			if in.Clear {
				err = e.call(ctx, http.MethodDelete, assetPath(id, "/placement"), nil, &out)
			} else {
				err = e.call(ctx, http.MethodPut, assetPath(id, "/placement"), map[string]any{"x": in.X, "y": in.Y, "z": in.Z}, &out)
			}
			if err != nil {
				return reply[map[string]any]{}, err
			}
			delete(out, "viewbox")
			var overlaps []any
			items, _ := out["items"].([]any)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					delete(m, "tx")
					delete(m, "ty")
					delete(m, "shapes")
					if m["id"] == id {
						overlaps, _ = m["overlaps"].([]any)
					}
				}
			}
			loc, _ := out["location"].(map[string]any)
			if in.Clear {
				return say(out, "Packed with the rest in %v again.", loc["name"])
			}
			if len(overlaps) > 0 {
				return say(out, "Placed in %v at %v, %v, %v; it overlaps %d other things.", loc["name"], in.X, in.Y, in.Z, len(overlaps))
			}
			return say(out, "Placed in %v at %v, %v, %v.", loc["name"], in.X, in.Y, in.Z)
		}),
		define(&mcp.Tool{
			Name: "look_up_code",
			Description: "Look up a code as scanning a label does, and get the asset's scan page: the details, contents and quick actions " +
				"its scan template lays out for you.",
			Annotations: readOnly("Look up a code"),
		}, []string{"GET /api/v1/scan/{code}"}, []string{rbac.PermAssetsScan}, func(ctx context.Context, e *env, in struct {
			Code string `json:"code" jsonschema:"a code from a label, or an asset's ID"`
		}) (reply[map[string]any], error) {
			var out map[string]any
			if err := e.call(ctx, http.MethodGet, "/api/v1/scan/"+url.PathEscape(strings.TrimSpace(in.Code)), nil, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			name := ""
			if a, ok := out["asset"].(map[string]any); ok {
				name, _ = a["name"].(string)
			}
			return say(out, "%q has that code.", name)
		}),
		define(&mcp.Tool{
			Name:        "asset_history",
			Description: "An asset's history, newest first, 50 at a time: who did what to it and when, and through which assistant.",
			Annotations: readOnly("Asset history"),
		}, []string{"GET /api/v1/assets/{id}/history"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in historyIn) (reply[historyOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[historyOut]{}, err
			}
			path := "/api/v1/assets/" + url.PathEscape(id) + "/history"
			if in.Before != "" {
				path += "?before=" + url.QueryEscape(in.Before)
			}
			var out historyOut
			if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
				return reply[historyOut]{}, err
			}
			if out.Events == nil {
				out.Events = []map[string]any{}
			}
			more := ""
			if out.More {
				more = " There are older ones."
			}
			return say(out, "%d events.%s", len(out.Events), more)
		}),
		define(&mcp.Tool{
			Name:        "list_loans",
			Description: "Loans of the assets you track: open, overdue, or all, newest first, with who borrowed what and when it is due.",
			Annotations: readOnly("List loans"),
		}, []string{"GET /api/v1/loans"}, []string{rbac.PermAssetsTrack}, func(ctx context.Context, e *env, in loansIn) (reply[loansOut], error) {
			q := url.Values{}
			if in.State != "" {
				q.Set("state", in.State)
			}
			if in.BorrowerID != "" {
				q.Set("borrower_id", in.BorrowerID)
			}
			path := "/api/v1/loans"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			var res struct {
				Loans []models.Loan `json:"loans"`
			}
			if err := e.call(ctx, http.MethodGet, path, nil, &res); err != nil {
				return reply[loansOut]{}, err
			}
			shown, more, err := page(res.Loans, in.Offset, in.Limit)
			if err != nil {
				return reply[loansOut]{}, err
			}
			for i := range shown {
				shown[i].Borrower.Email = "" // never shown on loans (docs/access.md)
			}
			if shown == nil {
				shown = []models.Loan{}
			}
			return say(loansOut{Total: len(res.Loans), Loans: shown, More: more}, "%d loans.", len(res.Loans))
		}),
		define(&mcp.Tool{
			Name:        "list_people",
			Description: "The people a loan can name, by name, to lend to them.",
			Annotations: readOnly("List people"),
		}, []string{"GET /api/v1/people"}, []string{rbac.PermAssetsLend, rbac.PermUsersManage}, func(ctx context.Context, e *env, _ none) (reply[peopleOut], error) {
			var res struct {
				People []models.Person `json:"people"`
			}
			if err := e.call(ctx, http.MethodGet, "/api/v1/people", nil, &res); err != nil {
				return reply[peopleOut]{}, err
			}
			// Emails only to tell apart two of the same name, as the lending
			// form does.
			counts := map[string]int{}
			for _, p := range res.People {
				counts[personName(p.Name)]++
			}
			out := peopleOut{People: []personView{}}
			for _, p := range res.People {
				v := personView{ID: p.ID, Name: personName(p.Name)}
				if counts[v.Name] > 1 {
					v.Email = p.Email
				}
				out.People = append(out.People, v)
			}
			return say(out, "%d people.", len(out.People))
		}),
		define(&mcp.Tool{
			Name:        "next_code",
			Description: "The next free code of a type, one more than the highest ending in a number, keeping its prefix and padding (after HS-105, HS-106).",
			Annotations: readOnly("Next code"),
		}, []string{"GET /api/v1/generators/{type}/next-code"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in nextCodeIn) (reply[nextCodeOut], error) {
			var out nextCodeOut
			if err := e.call(ctx, http.MethodGet, "/api/v1/generators/"+url.PathEscape(in.Type)+"/next-code", nil, &out); err != nil {
				return reply[nextCodeOut]{}, err
			}
			if out.Code == "" {
				return say(out, "No %s code ends in a number yet; choose the first yourself.", in.Type)
			}
			return say(out, "The next %s code is %s.", in.Type, out.Code)
		}),
		define(&mcp.Tool{
			Name:        "export_assets",
			Description: "The assets a search selects as a CSV file, as people download it, to read or to change and import again with import_assets.",
			Annotations: readOnly("Export assets"),
		}, []string{"GET /api/v1/assets/export.csv"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in searchIn) (reply[exportOut], error) {
			res := e.api(ctx, http.MethodGet, "/api/v1/assets/export.csv"+in.query(), nil, "")
			if res.Status != http.StatusOK {
				return reply[exportOut]{}, apiError(res)
			}
			rows := strings.Count(strings.TrimRight(string(res.Body), "\r\n"), "\n")
			out := exportOut{Rows: rows, Bytes: len(res.Body)}
			r, _ := say(out, "%d assets, as CSV.", rows)
			r.extra = []mcp.Content{&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "bam://export/assets.csv", MIMEType: "text/csv", Text: string(res.Body)}}}
			return r, nil
		}),
	}
}

func personName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Unnamed user"
	}
	return name
}
