package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mustID reads "id" from a JSON object nested under key, as the API answers.
func mustID(t *testing.T, body []byte, key string) string {
	t.Helper()
	var v map[string]json.RawMessage
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("%s: %s", err, body)
	}
	var inner struct {
		ID    string `json:"id"`
		Asset struct {
			ID string `json:"id"`
		} `json:"asset"`
	}
	_ = json.Unmarshal(v[key], &inner)
	if inner.Asset.ID != "" {
		return inner.Asset.ID
	}
	return inner.ID
}

// toolError calls a tool that must fail, and returns what it said.
func toolError(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res := callTool(t, cs, name, args)
	if !res.IsError {
		t.Fatalf("%s succeeded: %s", name, toolText(res))
	}
	return toolText(res)
}

type summary struct {
	ID         string
	Name       string
	Codes      map[string]string
	Where      string
	Condition  string
	Categories []string
	Stock      *struct {
		Quantity   int
		RunningLow bool `json:"running_low"`
	}
	Loan *struct{ Borrower string }
}

func TestMCPAssetTools(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	if _, err := srv.store.SetUserName(admin.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	_, secret := personalToken(t, srv, adminCookie, "Claude")
	cs := mcpSession(t, srv, secret)

	// A lendable category with a serial number, an Identify hook, and a shelf.
	cat := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools","is_lendable":true}`).Body.Bytes(), "category")
	field := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/fields", `{"key":"serial","label":"Serial","validation_type":"text"}`).Body.Bytes(), "field")
	doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories/"+cat+"/fields", `{"field_id":"`+field+`","is_required":false}`)
	hook := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/hooks", `{"key":"identify","label":"Identify"}`).Body.Bytes(), "hook")
	doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/categories/"+cat+"/hooks", `{"hooks":[{"hook_id":"`+hook+`","cascades":true}]}`)
	rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Shelf A","generator_keys":[{"generator_type":"qr","assigned_code":"HS-100"}]}`)
	shelf := mustID(t, rec.Body.Bytes(), "asset")

	// Adding: the next code, a field, and where it is; unknown fields refused
	// before anything is added.
	msg := toolError(t, cs, "create_asset", map[string]any{"assets": []any{
		map[string]any{"name": "Saw", "category_ids": []string{cat}},
		map[string]any{"name": "Plane", "category_ids": []string{cat}, "fields": map[string]string{"colour": "red"}},
	}})
	if !strings.Contains(msg, `no field "colour"`) || !strings.Contains(msg, "serial") || !strings.Contains(msg, "Nothing was added") {
		t.Errorf("unknown field: %s", msg)
	}
	var created struct{ Created []summary }
	structured(t, callTool(t, cs, "create_asset", map[string]any{"assets": []any{
		map[string]any{"name": "Drill", "category_ids": []string{cat}, "fields": map[string]string{"serial": "D-1"}, "assign_next_code": []string{"qr"}, "where": "HS-100"},
		map[string]any{"name": "M3 screws", "stock": map[string]any{"quantity": 100, "unit": "screws", "low_stock_threshold": 20}, "where": shelf},
	}}), &created)
	if len(created.Created) != 2 || created.Created[0].Codes["qr"] != "HS-101" || created.Created[0].Where != "Shelf A" || created.Created[1].Stock.Quantity != 100 {
		t.Fatalf("created: %+v", created)
	}
	drill, screws := created.Created[0].ID, created.Created[1].ID
	if res := callTool(t, cs, "search_assets", map[string]any{"search": "saw"}); !strings.Contains(toolText(res), "0 assets match") {
		t.Errorf("the refused list added something: %s", toolText(res))
	}

	// Finding: by the place's name, by code, and everything about one.
	var found struct {
		Total  int
		Assets []summary
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"search": "shelf a", "limit": 1}), &found)
	if found.Total != 3 || len(found.Assets) != 1 {
		t.Errorf("by place: %+v", found)
	}
	var detail struct {
		Contents []struct{ Name string }
		Fields   []struct{ Key, Value string }
		Hooks    []struct{ Key string }
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": "HS-100"}), &detail)
	if len(detail.Contents) != 2 {
		t.Errorf("shelf contents: %+v", detail)
	}
	structured(t, callTool(t, cs, "get_asset", map[string]any{"asset": "HS-101"}), &detail)
	if len(detail.Fields) != 1 || detail.Fields[0].Value != "D-1" || len(detail.Hooks) != 1 {
		t.Errorf("drill: %+v", detail)
	}
	if res := callTool(t, cs, "look_up_code", map[string]any{"code": "HS-101"}); res.IsError || !strings.Contains(toolText(res), "Drill") {
		t.Errorf("look_up_code: %s", toolText(res))
	}
	var next struct{ Code string }
	structured(t, callTool(t, cs, "next_code", map[string]any{"type": "qr"}), &next)
	if next.Code != "HS-102" {
		t.Errorf("next code: %s", next.Code)
	}

	// Changing keeps what is not named.
	var changed struct{ Asset summary }
	structured(t, callTool(t, cs, "update_asset", map[string]any{"asset": drill, "codes": []any{map[string]string{"type": "code128", "code": "DRL-1"}}}), &changed)
	if changed.Asset.Codes["qr"] != "HS-101" || changed.Asset.Codes["code128"] != "DRL-1" || len(changed.Asset.Categories) != 1 {
		t.Errorf("update: %+v", changed)
	}
	structured(t, callTool(t, cs, "move_asset", map[string]any{"asset": drill}), &changed)
	if changed.Asset.Where != "" {
		t.Errorf("taken out: %+v", changed)
	}
	structured(t, callTool(t, cs, "move_asset", map[string]any{"asset": drill, "where": "HS-100"}), &changed)
	if changed.Asset.Where != "Shelf A" {
		t.Errorf("put back: %+v", changed)
	}
	if res := callTool(t, cs, "link_assets", map[string]any{"asset": drill, "type": "used with", "other": screws}); res.IsError {
		t.Errorf("link: %s", toolText(res))
	}
	if msg := toolError(t, cs, "link_assets", map[string]any{"asset": drill, "type": "loves", "other": screws}); !strings.Contains(msg, "no link type") {
		t.Errorf("unknown link type: %s", msg)
	}

	// Stock and condition.
	structured(t, callTool(t, cs, "record_stock", map[string]any{"asset": screws, "use": 85}), &changed)
	if changed.Asset.Stock.Quantity != 15 || !changed.Asset.Stock.RunningLow {
		t.Errorf("used: %+v", changed.Asset.Stock)
	}
	if msg := toolError(t, cs, "record_stock", map[string]any{"asset": screws, "use": 1, "restock": 1}); !strings.Contains(msg, "exactly one") {
		t.Errorf("two changes: %s", msg)
	}
	structured(t, callTool(t, cs, "set_condition", map[string]any{"asset": drill, "condition": "needs_maintenance", "note": "chuck sticks"}), &changed)
	if changed.Asset.Condition != "needs_maintenance" {
		t.Errorf("condition: %+v", changed)
	}
	structured(t, callTool(t, cs, "set_condition", map[string]any{"asset": drill, "condition": "ok"}), &changed)

	// Lending, as the person.
	structured(t, callTool(t, cs, "borrow", map[string]any{"asset": drill, "due": "2030-01-01T17:00:00Z"}), &changed)
	if changed.Asset.Loan == nil || changed.Asset.Loan.Borrower != "Ada" {
		t.Errorf("borrowed: %+v", changed)
	}
	var loans struct{ Total int }
	structured(t, callTool(t, cs, "list_loans", map[string]any{"state": "open"}), &loans)
	if loans.Total != 1 {
		t.Errorf("loans: %+v", loans)
	}
	if msg := toolError(t, cs, "archive_asset", map[string]any{"asset": drill}); !strings.Contains(msg, "409") {
		t.Errorf("archiving a lent asset: %s", msg)
	}
	structured(t, callTool(t, cs, "return_asset", map[string]any{"asset": drill}), &changed)
	if changed.Asset.Loan != nil {
		t.Errorf("returned: %+v", changed)
	}
	var people struct {
		People []struct{ ID, Name, Email string }
	}
	structured(t, callTool(t, cs, "list_people", nil), &people)
	if len(people.People) != 1 || people.People[0].Name != "Ada" || people.People[0].Email != "" {
		t.Errorf("people: %+v", people)
	}

	// Hooks: triggered, with no device to answer.
	var pressed struct {
		EventID string `json:"event_id"`
		Replies []any
	}
	structured(t, callTool(t, cs, "trigger_hook", map[string]any{"asset": drill, "key": "identify", "wait_seconds": 0}), &pressed)
	if pressed.EventID == "" || len(pressed.Replies) != 0 {
		t.Errorf("pressed: %+v", pressed)
	}

	// Photos, seen as images.
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(3, 3, color.Black)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	if res := callTool(t, cs, "add_photo", map[string]any{"asset": drill, "data": base64.StdEncoding.EncodeToString(buf.Bytes()), "filename": "drill.png"}); res.IsError {
		t.Fatalf("add_photo: %s", toolText(res))
	}
	res := callTool(t, cs, "list_photos", map[string]any{"asset": drill})
	var photos struct{ Photos []struct{ ID string } }
	structured(t, res, &photos)
	images := 0
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok && len(ic.Data) > 0 && strings.HasPrefix(ic.MIMEType, "image/") {
			images++
		}
	}
	if len(photos.Photos) != 1 || images != 1 {
		t.Errorf("photos: %+v, %d images", photos, images)
	}
	if res := callTool(t, cs, "remove_photo", map[string]any{"asset": drill, "photo_id": photos.Photos[0].ID}); res.IsError {
		t.Errorf("remove_photo: %s", toolText(res))
	}

	// Archiving and restoring.
	structured(t, callTool(t, cs, "archive_asset", map[string]any{"asset": screws}), &changed)
	if res := callTool(t, cs, "search_assets", map[string]any{"archived": true}); !strings.Contains(toolText(res), "M3 screws") {
		t.Errorf("archived: %s", toolText(res))
	}
	structured(t, callTool(t, cs, "restore_asset", map[string]any{"asset": screws}), &changed)

	// History records the person, through the assistant.
	var history struct{ Events []map[string]any }
	structured(t, callTool(t, cs, "asset_history", map[string]any{"asset": drill}), &history)
	raw, _ := json.Marshal(history.Events)
	if !strings.Contains(string(raw), `"name":"Ada"`) || !strings.Contains(string(raw), `"via":"test client"`) {
		t.Errorf("history: %s", raw)
	}

	// Export, and import: previewed, then only what was previewed.
	res = callTool(t, cs, "export_assets", map[string]any{"search": "drill"})
	csv := ""
	for _, c := range res.Content {
		if er, ok := c.(*mcp.EmbeddedResource); ok {
			csv = er.Resource.Text
		}
	}
	if !strings.Contains(csv, "Drill") || !strings.Contains(csv, "HS-101") {
		t.Fatalf("export: %q", csv)
	}
	file := "name\nChisel\nMallet\n"
	var preview struct {
		Preview   map[string]any
		PreviewID string `json:"preview_id"`
	}
	structured(t, callTool(t, cs, "import_assets", map[string]any{"csv": file}), &preview)
	if preview.Preview["create"] != float64(2) || preview.PreviewID == "" {
		t.Fatalf("preview: %+v", preview)
	}
	mapping := []string{"name"}
	structured(t, callTool(t, cs, "import_assets", map[string]any{"csv": file, "mapping": mapping}), &preview)
	if msg := toolError(t, cs, "import_assets", map[string]any{"csv": file + "Saw\n", "mapping": mapping, "confirm": true, "preview_id": preview.PreviewID}); !strings.Contains(msg, "does not match") {
		t.Errorf("importing what was not previewed: %s", msg)
	}
	if res := callTool(t, cs, "import_assets", map[string]any{"csv": file, "mapping": mapping, "confirm": true, "preview_id": preview.PreviewID}); res.IsError {
		t.Errorf("import: %s", toolText(res))
	}
	if res := callTool(t, cs, "search_assets", map[string]any{"search": "mallet"}); !strings.Contains(toolText(res), "1 assets match") {
		t.Errorf("imported: %s", toolText(res))
	}
}

// TestMCPAssetToolsKeepToScopes answers as the API does: an asset out of the
// caller's scopes is not found, one it can read but not change is refused.
func TestMCPAssetToolsKeepToScopes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	scope := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/scopes", `{"name":"tools"}`).Body.Bytes(), "scope")
	cat := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Tools"}`).Body.Bytes(), "category")
	doJSON(t, srv, adminCookie, http.MethodPut, "/api/v1/categories/"+cat+"/scopes", `{"scope_ids":["`+scope+`"]}`)
	drill := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill","category_ids":["`+cat+`"]}`).Body.Bytes(), "asset")
	secretSaw := mustID(t, doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Secret saw"}`).Body.Bytes(), "asset")

	reader := tokenWith(t, srv, admin.ID, "Reader", "tools:assets.read")
	cs := mcpSession(t, srv, reader)
	if names := toolNames(t, cs); strings.Contains(strings.Join(names, ","), "create_asset") {
		t.Errorf("a reader lists create_asset: %v", names)
	}
	if res := callTool(t, cs, "get_asset", map[string]any{"asset": drill}); res.IsError {
		t.Errorf("in scope: %s", toolText(res))
	}
	for _, args := range []map[string]any{{"asset": secretSaw}} {
		msg := toolError(t, cs, "get_asset", args)
		viaAPI := withBearer(t, srv, reader, http.MethodGet, "/api/v1/assets/"+secretSaw, "")
		if !strings.Contains(msg, "HTTP 404") || viaAPI.Code != http.StatusNotFound {
			t.Errorf("out of scope: %s; the API %d", msg, viaAPI.Code)
		}
	}

	writer := tokenWith(t, srv, admin.ID, "Writer", "assets.read", "tools:assets.write")
	cs = mcpSession(t, srv, writer)
	if res := callTool(t, cs, "update_asset", map[string]any{"asset": drill, "name": "Big drill"}); res.IsError {
		t.Errorf("writing in scope: %s", toolText(res))
	}
	msg := toolError(t, cs, "update_asset", map[string]any{"asset": secretSaw, "name": "Saw"})
	viaAPI := withBearer(t, srv, writer, http.MethodPut, "/api/v1/assets/"+secretSaw, `{"name":"Saw"}`)
	if !strings.Contains(msg, "HTTP 403") || viaAPI.Code != http.StatusForbidden {
		t.Errorf("read-only: %s; the API %d", msg, viaAPI.Code)
	}
	// A token holding roles cannot borrow, and says so.
	lender := tokenWith(t, srv, admin.ID, "Desk", rbac.PermAssetsRead, rbac.PermAssetsBorrow)
	if msg := toolError(t, mcpSession(t, srv, lender), "borrow", map[string]any{"asset": drill}); !strings.Contains(msg, "cannot borrow") {
		t.Errorf("a token borrowing: %s", msg)
	}
}
