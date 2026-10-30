package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type codeIn struct {
	Type string `json:"type" jsonschema:"the code type: qr, micro_qr or code128"`
	Code string `json:"code"`
}

type stockIn struct {
	Quantity          *int   `json:"quantity,omitempty" jsonschema:"how many there are now"`
	Unit              string `json:"unit,omitempty" jsonschema:"what it is counted in, such as screws or g"`
	LowStockThreshold *int   `json:"low_stock_threshold,omitempty" jsonschema:"flag it running low below this"`
}

type newAsset struct {
	Name           string            `json:"name"`
	CategoryIDs    []string          `json:"category_ids,omitempty" jsonschema:"its categories, by ID (list_categories)"`
	Fields         map[string]string `json:"fields,omitempty" jsonschema:"field values by field key, as get_category lists them"`
	Codes          []codeIn          `json:"codes,omitempty"`
	AssignNextCode []string          `json:"assign_next_code,omitempty" jsonschema:"code types to give it the next free code of, such as [\"qr\"]"`
	Where          string            `json:"where,omitempty" jsonschema:"the asset it is in, by ID or code, such as a shelf or bin"`
	Stock          *stockIn          `json:"stock,omitempty" jsonschema:"count it by quantity"`
	Icon           string            `json:"icon,omitempty" jsonschema:"an icon of its own, by name (icons_overview lists them); empty to be drawn with its categories'"`
}

type createIn struct {
	Assets []newAsset `json:"assets" jsonschema:"the assets to add, 1 to 50"`
}

type createOut struct {
	Created []assetSummary `json:"created"`
	Failed  *failure       `json:"failed,omitempty" jsonschema:"the asset that could not be added, and why; those after it were not tried"`
}

type failure struct {
	Index int    `json:"index" jsonschema:"its place in the list, from 0"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

type copyIn struct {
	Asset    string            `json:"asset" jsonschema:"the asset to copy, by ID or code"`
	Name     string            `json:"name,omitempty" jsonschema:"the new asset's name; empty for the original's"`
	Fields   map[string]string `json:"fields,omitempty" jsonschema:"field values that differ, by key; an empty value leaves one blank. Others are the original's"`
	Codes    []codeIn          `json:"codes,omitempty" jsonschema:"codes to give it instead of the next in the series of the original's; an empty code gives it none of that type"`
	Quantity *int              `json:"quantity,omitempty" jsonschema:"for stock counted by quantity, how many the new one has; otherwise the original's"`
	Photos   bool              `json:"photos,omitempty" jsonschema:"true to give it copies of the original's photos"`
}

type copyOut struct {
	Asset     assetSummary `json:"asset"`
	Linked    []string     `json:"linked" jsonschema:"the links it was given, as its original has them"`
	NotLinked []string     `json:"not_linked,omitempty" jsonschema:"links it could not be given, and why"`
	Photos    int          `json:"photos,omitempty" jsonschema:"how many photos it was given"`
	NoPhotos  string       `json:"no_photos,omitempty" jsonschema:"why the photos could not be copied"`
}

type updateIn struct {
	Asset             string            `json:"asset" jsonschema:"the asset's ID, or one of its codes"`
	Name              string            `json:"name,omitempty" jsonschema:"a new name"`
	AddCategories     []string          `json:"add_categories,omitempty" jsonschema:"category IDs to put it in"`
	RemoveCategories  []string          `json:"remove_categories,omitempty" jsonschema:"category IDs to take it out of"`
	Fields            map[string]string `json:"fields,omitempty" jsonschema:"field values to set, by key; an empty value clears one. Others are kept"`
	Codes             []codeIn          `json:"codes,omitempty" jsonschema:"codes to set, replacing its code of the same type. Others are kept"`
	RemoveCodes       []string          `json:"remove_codes,omitempty" jsonschema:"code types to remove its codes of"`
	Stock             *stockIn          `json:"stock,omitempty" jsonschema:"count it by quantity, or change how"`
	StopCountingStock bool              `json:"stop_counting_stock,omitempty" jsonschema:"stop counting it by quantity"`
	Icon              string            `json:"icon,omitempty" jsonschema:"give it an icon of its own, by name (icons_overview lists them)"`
	CategoryIcon      bool              `json:"category_icon,omitempty" jsonschema:"draw it with its categories' icon again, dropping one of its own"`
	IconRendition     string            `json:"icon_rendition,omitempty" jsonschema:"which of its own icons icon or category_icon sets: screen (the default), or label, drawn on its labels; with its own screen icon and no label one, its labels draw its screen icon"`
}

type moveIn struct {
	Asset string `json:"asset" jsonschema:"the asset to move, by ID or code"`
	Where string `json:"where,omitempty" jsonschema:"the asset to put it in, by ID or code; empty to take it out of where it is"`
}

type linkIn struct {
	Asset string `json:"asset" jsonschema:"one asset, by ID or code"`
	Type  string `json:"type" jsonschema:"the link type by ID, or as read from the first asset: contains, contained in, has part, used with"`
	Other string `json:"other" jsonschema:"the other asset, by ID or code"`
}

type unlinkIn struct {
	Asset  string `json:"asset" jsonschema:"the asset, by ID or code"`
	LinkID string `json:"link_id" jsonschema:"the link's ID, from get_asset's links"`
}

type stockChangeIn struct {
	Asset   string `json:"asset" jsonschema:"the asset, by ID or code"`
	Use     int    `json:"use,omitempty" jsonschema:"how many were used"`
	Restock int    `json:"restock,omitempty" jsonschema:"how many were added"`
	Set     *int   `json:"set,omitempty" jsonschema:"how many there are, counted"`
}

type runningLowIn struct {
	Asset      string `json:"asset" jsonschema:"the asset, by ID or code"`
	RunningLow bool   `json:"running_low"`
}

type conditionIn struct {
	Asset     string `json:"asset" jsonschema:"the asset, by ID or code"`
	Condition string `json:"condition" jsonschema:"ok, needs_maintenance or out_of_service"`
	Note      string `json:"note,omitempty" jsonschema:"what is wrong, up to 255 characters"`
}

type borrowIn struct {
	Asset string     `json:"asset" jsonschema:"the asset, by ID or code"`
	Due   *time.Time `json:"due,omitempty" jsonschema:"when it is due back, an RFC 3339 time"`
}

type lendIn struct {
	Asset      string     `json:"asset" jsonschema:"the asset, by ID or code"`
	BorrowerID string     `json:"borrower_id" jsonschema:"who borrows it, from list_people"`
	Due        *time.Time `json:"due,omitempty" jsonschema:"when it is due back, an RFC 3339 time"`
}

type returnIn struct {
	Asset string `json:"asset" jsonschema:"the asset, by ID or code"`
	Left  *int   `json:"left,omitempty" jsonschema:"for stock counted by quantity, such as a spool, how much came back"`
}

type assetOut struct {
	Asset assetSummary `json:"asset"`
}

// apiAsset is an asset as the API answers a change to it.
type apiAsset struct {
	Asset models.AssetWithDetails `json:"asset"`
}

func (e *env) summaryOf(ctx context.Context, a *models.AssetWithDetails) assetOut {
	return assetOut{Asset: summarize(a, e.baseURL(ctx))}
}

// reread reads an asset after a change whose answer does not include it.
func (e *env) reread(ctx context.Context, id string) (assetOut, error) {
	a, err := e.asset(ctx, id)
	if err != nil {
		return assetOut{}, err
	}
	return e.summaryOf(ctx, a), nil
}

func assetPath(id string, rest ...string) string {
	return "/api/v1/assets/" + url.PathEscape(id) + strings.Join(rest, "")
}

func stockBody(s *stockIn) map[string]any {
	return map[string]any{"quantity": s.Quantity, "unit": s.Unit, "low_stock_threshold": s.LowStockThreshold}
}

// placeIn puts an asset in another, through the link type that says where
// things are, moving it from wherever it was.
func (e *env) placeIn(ctx context.Context, id, where string) error {
	container, err := e.assetID(ctx, where)
	if err != nil {
		return err
	}
	loc, err := e.locationType(ctx)
	if err != nil {
		return err
	}
	return e.call(ctx, http.MethodPost, assetPath(container, "/links"), map[string]any{"type_id": loc.ID, "asset_id": id, "outgoing": true}, nil)
}

func changingTools() []tool {
	return []tool{
		define(&mcp.Tool{
			Name: "create_asset",
			Description: "Add assets: each with its name, categories, field values, codes (or the next free code), the place it is in, and stock. " +
				"Up to 50 at a time; unknown field keys are refused, naming the field, before anything is added.",
			Annotations: changes("Add assets", false),
		}, []string{"POST /api/v1/assets", "POST /api/v1/categories/effective-fields"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in createIn) (reply[createOut], error) {
			if len(in.Assets) == 0 {
				return reply[createOut]{}, fmt.Errorf("give at least one asset")
			}
			if err := checkItems(len(in.Assets)); err != nil {
				return reply[createOut]{}, err
			}
			// Every field named must be one its categories give it.
			for i, a := range in.Assets {
				if len(a.Fields) == 0 {
					continue
				}
				var eff struct {
					Fields []models.EffectiveField `json:"fields"`
				}
				ids := a.CategoryIDs
				if ids == nil {
					ids = []string{}
				}
				if err := e.call(ctx, http.MethodPost, "/api/v1/categories/effective-fields", map[string]any{"category_ids": ids}, &eff); err != nil {
					return reply[createOut]{}, fmt.Errorf("asset %d (%q): %w", i, a.Name, err)
				}
				known := map[string]bool{}
				var keys []string
				for _, f := range eff.Fields {
					known[f.Field.Key] = true
					keys = append(keys, f.Field.Key)
				}
				for k := range a.Fields {
					if !known[k] {
						return reply[createOut]{}, fmt.Errorf("asset %d (%q): its categories give it no field %q; they give it %s. Nothing was added",
							i, a.Name, k, orNone(keys))
					}
				}
			}
			out := createOut{Created: []assetSummary{}}
			for i, a := range in.Assets {
				created, err := e.createOne(ctx, a)
				if err != nil {
					out.Failed = &failure{Index: i, Name: a.Name, Error: err.Error()}
					break
				}
				out.Created = append(out.Created, created)
			}
			var names []string
			for _, c := range out.Created {
				names = append(names, describe(c))
			}
			text := fmt.Sprintf("Added %d: %s.", len(out.Created), strings.Join(names, ", "))
			if out.Failed != nil {
				text += fmt.Sprintf(" Could not add %q: %s; the rest were not tried.", out.Failed.Name, out.Failed.Error)
			}
			return say(out, "%s", text)
		}),
		define(&mcp.Tool{
			Name: "copy_asset",
			Description: "Add an asset like another, such as one more spool like this one: the same categories, field values, stock, icon and place, " +
				"the next codes in the series of its own (after SP-014, SP-015), and its links, but not links that would take the other asset " +
				"from it (what a box contains), nor its condition, loans or history. Change what differs with name, fields, codes and quantity; " +
				"photos copies its photos too.",
			Annotations: changes("Copy an asset", false),
		}, []string{"GET /api/v1/assets/{id}/copy", "POST /api/v1/assets", "POST /api/v1/assets/{id}/links", "POST /api/v1/assets/{id}/photos/copy"}, []string{rbac.PermAssetsWrite},
			func(ctx context.Context, e *env, in copyIn) (reply[copyOut], error) {
				id, err := e.assetID(ctx, in.Asset)
				if err != nil {
					return reply[copyOut]{}, err
				}
				var got struct {
					Copy struct {
						Name          string                     `json:"name"`
						CategoryIDs   []string                   `json:"category_ids"`
						FieldValues   map[string]string          `json:"field_values"`
						Stock         *stockIn                   `json:"stock"`
						GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys"`
						IconName      string                     `json:"icon_name"`
						Links         []struct {
							TypeID   string             `json:"type_id"`
							Label    string             `json:"label"`
							Asset    models.LinkedAsset `json:"asset"`
							Outgoing bool               `json:"outgoing"`
						} `json:"links"`
					} `json:"copy"`
				}
				if err := e.call(ctx, http.MethodGet, assetPath(id, "/copy"), nil, &got); err != nil {
					return reply[copyOut]{}, err
				}
				c := got.Copy
				if in.Name != "" {
					c.Name = in.Name
				}
				for k, v := range in.Fields {
					c.FieldValues[k] = v
				}
				codes := map[string]string{}
				for _, k := range c.GeneratorKeys {
					codes[k.GeneratorType] = k.AssignedCode
				}
				for _, k := range in.Codes {
					codes[k.Type] = k.Code
				}
				keys := []map[string]string{}
				for _, t := range sortedKeys(codes) {
					if codes[t] != "" {
						keys = append(keys, map[string]string{"generator_type": t, "assigned_code": codes[t]})
					}
				}
				body := map[string]any{"name": c.Name, "category_ids": c.CategoryIDs, "generator_keys": keys, "field_values": c.FieldValues}
				if c.IconName != "" {
					body["icon_name"] = c.IconName
				}
				if c.Stock != nil {
					if in.Quantity != nil {
						c.Stock.Quantity = in.Quantity
					}
					body["stock"] = stockBody(c.Stock)
				}
				var res apiAsset
				if err := e.call(ctx, http.MethodPost, "/api/v1/assets", body, &res); err != nil {
					return reply[copyOut]{}, err
				}
				made := res.Asset.Asset.ID
				out := copyOut{Linked: []string{}}
				for _, l := range c.Links {
					what := l.Label + " " + l.Asset.Name
					err := e.call(ctx, http.MethodPost, assetPath(made, "/links"), map[string]any{"type_id": l.TypeID, "asset_id": l.Asset.ID, "outgoing": l.Outgoing}, nil)
					if err != nil {
						out.NotLinked = append(out.NotLinked, fmt.Sprintf("%s: %v", what, err))
						continue
					}
					out.Linked = append(out.Linked, what)
				}
				if in.Photos {
					var copied apiAsset
					if err := e.call(ctx, http.MethodPost, assetPath(made, "/photos/copy"), map[string]any{"from_asset_id": id}, &copied); err != nil {
						out.NoPhotos = err.Error()
					} else {
						out.Photos = len(copied.Asset.Photos)
					}
				}
				after, err := e.reread(ctx, made)
				if err != nil {
					return reply[copyOut]{}, err
				}
				out.Asset = after.Asset
				text := "Added " + describe(out.Asset) + "."
				if len(out.Linked) > 0 {
					text += " It is " + strings.Join(out.Linked, ", ") + "."
				}
				if len(out.NotLinked) > 0 {
					text += " Could not link it: " + strings.Join(out.NotLinked, "; ") + "."
				}
				switch {
				case out.Photos == 1:
					text += " It has a copy of the original's photo."
				case out.Photos > 1:
					text += fmt.Sprintf(" It has copies of the original's %d photos.", out.Photos)
				}
				if out.NoPhotos != "" {
					text += " Could not copy its photos: " + out.NoPhotos + "."
				}
				return say(out, "%s", text)
			}),
		define(&mcp.Tool{
			Name: "update_asset",
			Description: "Change an asset: its name, categories, field values, codes, stock or icon. Only what is given changes; " +
				"categories, fields and codes not named are kept.",
			Annotations: changes("Change an asset", true),
		}, []string{"PUT /api/v1/assets/{id}"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in updateIn) (reply[assetOut], error) {
			a, err := e.asset(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			body := map[string]any{}
			if in.Name != "" {
				body["name"] = in.Name
			}
			if len(in.AddCategories) > 0 || len(in.RemoveCategories) > 0 {
				ids := map[string]bool{}
				for _, c := range a.Categories {
					ids[c.ID] = true
				}
				for _, c := range in.AddCategories {
					ids[c] = true
				}
				for _, c := range in.RemoveCategories {
					delete(ids, c)
				}
				body["category_ids"] = sortedKeys(ids)
			}
			if len(in.Fields) > 0 {
				body["field_values"] = in.Fields
			}
			if len(in.Codes) > 0 || len(in.RemoveCodes) > 0 {
				codes := map[string]string{}
				for _, k := range a.GeneratorKeys {
					codes[k.GeneratorType] = k.AssignedCode
				}
				for _, t := range in.RemoveCodes {
					delete(codes, t)
				}
				for _, c := range in.Codes {
					codes[c.Type] = c.Code
				}
				keys := []map[string]string{}
				for _, t := range sortedKeys(codes) {
					keys = append(keys, map[string]string{"generator_type": t, "assigned_code": codes[t]})
				}
				body["generator_keys"] = keys
			}
			switch {
			case in.StopCountingStock:
				body["stock"] = nil
			case in.Stock != nil:
				body["stock"] = stockBody(in.Stock)
			}
			switch {
			case in.CategoryIcon && in.Icon != "":
				return reply[assetOut]{}, fmt.Errorf("give icon or category_icon, not both")
			case in.CategoryIcon:
				body["icon_name"] = nil
			case in.Icon != "":
				body["icon_name"] = in.Icon
			}
			if in.IconRendition != "" {
				if _, ok := body["icon_name"]; !ok {
					return reply[assetOut]{}, fmt.Errorf("icon_rendition says which icon icon or category_icon sets; give one of them")
				}
				body["icon_rendition"] = in.IconRendition
			}
			if len(body) == 0 {
				return reply[assetOut]{}, fmt.Errorf("nothing to change: give a name, categories, fields, codes, stock or icon")
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPut, assetPath(a.Asset.ID), body, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			return say(out, "Changed %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "move_asset",
			Description: "Put an asset in another, such as a bin on a shelf, moving it from wherever it was; or, with no where, take it out of where it is.",
			Annotations: changes("Move an asset", true),
		}, []string{"POST /api/v1/assets/{id}/links", "DELETE /api/v1/assets/{id}/links/{link_id}"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in moveIn) (reply[assetOut], error) {
			a, err := e.asset(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			if in.Where != "" {
				if err := e.placeIn(ctx, a.Asset.ID, in.Where); err != nil {
					return reply[assetOut]{}, err
				}
			} else {
				loc, err := e.locationType(ctx)
				if err != nil {
					return reply[assetOut]{}, err
				}
				removed := false
				for _, l := range a.Links {
					if l.TypeID == loc.ID && !l.Outgoing {
						if err := e.call(ctx, http.MethodDelete, assetPath(a.Asset.ID, "/links/", url.PathEscape(l.ID)), nil, nil); err != nil {
							return reply[assetOut]{}, err
						}
						removed = true
					}
				}
				if !removed {
					return reply[assetOut]{}, fmt.Errorf("%q is not in anything", a.Asset.Name)
				}
			}
			out, err := e.reread(ctx, a.Asset.ID)
			if err != nil {
				return reply[assetOut]{}, err
			}
			if out.Asset.Where == "" {
				return say(out, "%s is not in anything now.", describe(out.Asset))
			}
			return say(out, "%s is now in %s.", describe(out.Asset), out.Asset.Where)
		}),
		define(&mcp.Tool{
			Name: "link_assets",
			Description: "Link two assets by a link type, read from the first: this bin contains those screws, this drill is used with those bits. " +
				"A link type that says where things are moves the second asset there.",
			Annotations: changes("Link assets", true),
		}, []string{"POST /api/v1/assets/{id}/links"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in linkIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			other, err := e.assetID(ctx, in.Other)
			if err != nil {
				return reply[assetOut]{}, err
			}
			types, err := e.relationTypes(ctx)
			if err != nil {
				return reply[assetOut]{}, err
			}
			typeID, outgoing := "", true
			want := strings.ToLower(strings.TrimSpace(in.Type))
			var names []string
			for _, t := range types {
				names = append(names, t.Name)
				switch {
				case t.ID == in.Type || strings.ToLower(t.Name) == want:
					typeID, outgoing = t.ID, true
				case strings.ToLower(t.InverseName) == want:
					typeID, outgoing = t.ID, false
				}
			}
			if typeID == "" {
				sort.Strings(names)
				return reply[assetOut]{}, fmt.Errorf("no link type is %q; there are %s", in.Type, orNone(names))
			}
			if err := e.call(ctx, http.MethodPost, assetPath(id, "/links"), map[string]any{"type_id": typeID, "asset_id": other, "outgoing": outgoing}, nil); err != nil {
				return reply[assetOut]{}, err
			}
			out, err := e.reread(ctx, id)
			if err != nil {
				return reply[assetOut]{}, err
			}
			return say(out, "Linked %s: %s.", describe(out.Asset), in.Type)
		}),
		define(&mcp.Tool{
			Name:        "unlink_assets",
			Description: "Remove a link between two assets, by the link's ID from get_asset.",
			Annotations: destroys("Unlink assets", true),
		}, []string{"DELETE /api/v1/assets/{id}/links/{link_id}"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in unlinkIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			if err := e.call(ctx, http.MethodDelete, assetPath(id, "/links/", url.PathEscape(in.LinkID)), nil, nil); err != nil {
				return reply[assetOut]{}, err
			}
			out, err := e.reread(ctx, id)
			if err != nil {
				return reply[assetOut]{}, err
			}
			return say(out, "Removed the link from %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "record_stock",
			Description: "Record stock counted by quantity: some used, some added, or how many there are after counting. Give exactly one of use, restock or set.",
			Annotations: changes("Record stock", false),
		}, []string{"POST /api/v1/assets/{id}/quantity"}, []string{rbac.PermStockRecord}, func(ctx context.Context, e *env, in stockChangeIn) (reply[assetOut], error) {
			given := 0
			body := map[string]any{}
			if in.Use != 0 {
				given++
				body["adjust"] = -in.Use
			}
			if in.Restock != 0 {
				given++
				body["adjust"] = in.Restock
			}
			if in.Set != nil {
				given++
				body["set"] = *in.Set
			}
			if given != 1 {
				return reply[assetOut]{}, fmt.Errorf("give exactly one of use, restock or set")
			}
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPost, assetPath(id, "/quantity"), body, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			st := out.Asset.Stock
			if st == nil {
				return say(out, "Recorded.")
			}
			low := ""
			if st.RunningLow {
				low = ", running low"
			}
			return say(out, "%s: %d %s%s.", describe(out.Asset), st.Quantity, st.Unit, low)
		}),
		define(&mcp.Tool{
			Name:        "set_running_low",
			Description: "Flag an asset as running low, or clear the flag, by hand.",
			Annotations: changes("Flag running low", true),
		}, []string{"PUT /api/v1/assets/{id}/running-low"}, []string{rbac.PermStockRecord}, func(ctx context.Context, e *env, in runningLowIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPut, assetPath(id, "/running-low"), map[string]bool{"running_low": in.RunningLow}, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			if in.RunningLow {
				return say(out, "%s is flagged running low.", describe(out.Asset))
			}
			return say(out, "%s is no longer flagged running low.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "set_condition",
			Description: "Report an asset needing maintenance or out of service, with a note saying what is wrong; or mark it ok again.",
			Annotations: changes("Set condition", true),
		}, []string{"PUT /api/v1/assets/{id}/condition"}, []string{rbac.PermAssetsReport, rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in conditionIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPut, assetPath(id, "/condition"), map[string]string{"condition": in.Condition, "note": in.Note}, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			return say(out, "%s is %s.", describe(out.Asset), strings.ReplaceAll(out.Asset.Condition, "_", " "))
		}),
		define(&mcp.Tool{
			Name:        "borrow",
			Description: "Borrow an asset for the person you act for, optionally until a due date. Only a person can borrow; a token holding roles lends instead.",
			Annotations: changes("Borrow", false),
		}, []string{"POST /api/v1/assets/{id}/checkout"}, []string{rbac.PermAssetsBorrow}, func(ctx context.Context, e *env, in borrowIn) (reply[assetOut], error) {
			return e.checkOut(ctx, in.Asset, "", in.Due)
		}),
		define(&mcp.Tool{
			Name:        "lend",
			Description: "Lend an asset to someone (list_people), optionally until a due date.",
			Annotations: changes("Lend", false),
		}, []string{"POST /api/v1/assets/{id}/checkout"}, []string{rbac.PermAssetsLend}, func(ctx context.Context, e *env, in lendIn) (reply[assetOut], error) {
			if in.BorrowerID == "" {
				return reply[assetOut]{}, fmt.Errorf("name who borrows it, by borrower_id from list_people")
			}
			return e.checkOut(ctx, in.Asset, in.BorrowerID, in.Due)
		}),
		define(&mcp.Tool{
			Name:        "return_asset",
			Description: "Return a lent asset, ending its loan; for stock counted by quantity, say how much came back.",
			Annotations: changes("Return", false),
		}, []string{"POST /api/v1/assets/{id}/checkin"}, []string{rbac.PermAssetsBorrow, rbac.PermAssetsLend}, func(ctx context.Context, e *env, in returnIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			body := map[string]any{}
			if in.Left != nil {
				body["left"] = *in.Left
			}
			if err := e.call(ctx, http.MethodPost, assetPath(id, "/checkin"), body, nil); err != nil {
				return reply[assetOut]{}, err
			}
			out, err := e.reread(ctx, id)
			if err != nil {
				return reply[assetOut]{}, err
			}
			return say(out, "Returned %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "archive_asset",
			Description: "Archive an asset no longer in use: it leaves lists and searches, and can be restored. Something lent out, or holding other assets, cannot be archived.",
			Annotations: changes("Archive", true),
		}, []string{"DELETE /api/v1/assets/{id}"}, []string{rbac.PermAssetsDelete}, func(ctx context.Context, e *env, in assetIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			if err := e.call(ctx, http.MethodDelete, assetPath(id), nil, nil); err != nil {
				return reply[assetOut]{}, err
			}
			out, err := e.reread(ctx, id)
			if err != nil {
				return reply[assetOut]{}, err
			}
			return say(out, "Archived %s; restore_asset brings it back.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "restore_asset",
			Description: "Restore an archived asset, by its ID, back where it was.",
			Annotations: changes("Restore", true),
		}, []string{"POST /api/v1/assets/{id}/restore"}, []string{rbac.PermAssetsDelete}, func(ctx context.Context, e *env, in assetIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPost, assetPath(id, "/restore"), nil, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			return say(out, "Restored %s.", describe(out.Asset))
		}),
	}
}

func (e *env) checkOut(ctx context.Context, asset, borrower string, due *time.Time) (reply[assetOut], error) {
	id, err := e.assetID(ctx, asset)
	if err != nil {
		return reply[assetOut]{}, err
	}
	body := map[string]any{}
	if borrower != "" {
		body["borrower_id"] = borrower
	}
	if due != nil {
		body["due_at"] = due.UTC().Format(time.RFC3339)
	}
	if err := e.call(ctx, http.MethodPost, assetPath(id, "/checkout"), body, nil); err != nil {
		return reply[assetOut]{}, err
	}
	out, err := e.reread(ctx, id)
	if err != nil {
		return reply[assetOut]{}, err
	}
	text := "Lent out " + describe(out.Asset)
	if l := out.Asset.Loan; l != nil && l.Borrower != "" {
		text += " to " + l.Borrower
	}
	if due != nil {
		text += ", due " + due.Format("Monday 2 January")
	}
	return say(out, "%s.", text)
}

// createOne adds one asset, its next codes and its place.
func (e *env) createOne(ctx context.Context, a newAsset) (assetSummary, error) {
	keys := []map[string]string{}
	for _, c := range a.Codes {
		keys = append(keys, map[string]string{"generator_type": c.Type, "assigned_code": c.Code})
	}
	for _, t := range a.AssignNextCode {
		var next nextCodeOut
		if err := e.call(ctx, http.MethodGet, "/api/v1/generators/"+url.PathEscape(t)+"/next-code", nil, &next); err != nil {
			return assetSummary{}, err
		}
		if next.Code == "" {
			return assetSummary{}, fmt.Errorf("no %s code ends in a number to follow; give the code yourself", t)
		}
		keys = append(keys, map[string]string{"generator_type": t, "assigned_code": next.Code})
	}
	body := map[string]any{"name": a.Name, "category_ids": a.CategoryIDs, "generator_keys": keys, "field_values": a.Fields}
	if a.Stock != nil {
		body["stock"] = stockBody(a.Stock)
	}
	if a.Icon != "" {
		body["icon_name"] = a.Icon
	}
	var res apiAsset
	if err := e.call(ctx, http.MethodPost, "/api/v1/assets", body, &res); err != nil {
		return assetSummary{}, err
	}
	id := res.Asset.Asset.ID
	if a.Where != "" {
		if err := e.placeIn(ctx, id, a.Where); err != nil {
			return assetSummary{}, fmt.Errorf("added it (%s), but could not put it in %q: %w", id, a.Where, err)
		}
		out, err := e.reread(ctx, id)
		return out.Asset, err
	}
	return summarize(&res.Asset, e.baseURL(ctx)), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func orNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
