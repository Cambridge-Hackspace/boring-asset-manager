package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// A route is an endpoint to fuzz, with a valid request to start from. Paths
// name the kind of ID they take in braces; bodies are JSON unless kind says
// otherwise.
type route struct {
	method, path string
	kind         string // "", "csv" (an import) or "image" (an upload)
	body         string
	query        map[string][]string
}

// Sending to printers and to notification services is left out: the
// requests would leave the process. So is signing out, which would end the
// fuzzer's session.
var fuzzRoutes = []route{
	{method: "GET", path: "/healthz"},
	{method: "GET", path: "/api/v1/info"},
	{method: "POST", path: "/api/v1/auth/login", body: `{"email":"admin@test.org","password":"wrong"}`},
	{method: "GET", path: "/api/v1/auth/me"},
	{method: "PUT", path: "/api/v1/auth/name", body: `{"name":"Ada {n}"}`},
	{method: "PUT", path: "/api/v1/auth/password", body: `{"current_password":"wrong","new_password":"Another123!"}`},
	{method: "GET", path: "/api/v1/users"},
	{method: "POST", path: "/api/v1/users", body: `{"email":"someone{n}@test.org","name":"Someone {n}","password":"Secret123!","is_active":true}`},
	{method: "PATCH", path: "/api/v1/users/{user}/status", body: `{"is_active":false}`},
	{method: "DELETE", path: "/api/v1/users/{user}"},
	{method: "PUT", path: "/api/v1/users/{user}/name", body: `{"name":"Grace {n}"}`},
	{method: "PUT", path: "/api/v1/users/{user}/roles", body: `{"role_ids":["{role}"]}`},
	{method: "GET", path: "/api/v1/roles"},
	{method: "POST", path: "/api/v1/roles", body: `{"name":"Stores {n}","description":"Records stock","permissions":["assets:read","stock:record"]}`},
	{method: "PUT", path: "/api/v1/roles/{role}", body: `{"name":"Readers {n}","description":"","permissions":["assets:read"]}`},
	{method: "DELETE", path: "/api/v1/roles/{role}"},
	{method: "GET", path: "/api/v1/tokens"},
	{method: "POST", path: "/api/v1/tokens", body: `{"name":"Script {n}","role_ids":["{role}"],"expires_at":"2099-01-01T00:00:00Z"}`},
	{method: "DELETE", path: "/api/v1/tokens/{token}"},
	{method: "GET", path: "/api/v1/fields"},
	{method: "POST", path: "/api/v1/fields", body: `{"key":"k{n}","label":"Size","validation_type":"select","options":{"choices":[{"label":"Small","value":"s"},{"label":"Large","value":"l"}],"default":"s"}}`},
	{method: "GET", path: "/api/v1/fields/{field}"},
	{method: "PUT", path: "/api/v1/fields/{field}", body: `{"label":"Weight","validation_type":"number"}`},
	{method: "DELETE", path: "/api/v1/fields/{field}"},
	{method: "GET", path: "/api/v1/categories"},
	{method: "POST", path: "/api/v1/categories", body: `{"name":"Glue","parent_id":"{category}","color_hex":"#ff8800","is_consumable":true,"low_stock_threshold":2}`},
	{method: "GET", path: "/api/v1/categories/{category}"},
	{method: "PUT", path: "/api/v1/categories/{category}", body: `{"name":"Adhesives","parent_id":null,"is_consumable":true,"low_stock_threshold":5}`},
	{method: "DELETE", path: "/api/v1/categories/{category}"},
	{method: "PUT", path: "/api/v1/categories/{category}/running-low", body: `{"running_low":true}`},
	{method: "GET", path: "/api/v1/categories/{category}/fields"},
	{method: "GET", path: "/api/v1/categories/{category}/relation-types"},
	{method: "PUT", path: "/api/v1/categories/{category}/relation-types", body: `{"relation_type_ids":["{relation}"]}`},
	{method: "GET", path: "/api/v1/reports"},
	{method: "POST", path: "/api/v1/reports", body: `{"name":"Report {n}","spec":{"filter":{"running_low":true,"category_ids":["{category}"]},"sections":[{"type":"table","title":"Low","columns":[{"heading":"Asset","binding":"{{asset.name}} {{stock.quantity}}"}],"group_by":"category"}]}}`},
	{method: "POST", path: "/api/v1/reports/preview", body: `{"spec":{"filter":{"loan":"open"},"sections":[{"type":"table","columns":[{"heading":"Borrower","binding":"{{loan.borrower}}"}]}]}}`},
	{method: "POST", path: "/api/v1/reports/preview", body: `{"spec":{"filter":{"category_ids":["{category}"]},"sections":[{"type":"chart","chart":"bar","group_by":"category","measure":"quantity"},{"type":"chart","chart":"line","measure":"used","period":"week"}]}}`},
	{method: "GET", path: "/api/v1/reports/{report}"},
	{method: "PUT", path: "/api/v1/reports/{report}", body: `{"name":"Renamed {n}","spec":{"sections":[{"type":"table","columns":[{"heading":"Where","binding":"{{asset.location}}"}]}]}}`},
	{method: "GET", path: "/api/v1/reports/{report}/run"},
	{method: "GET", path: "/api/v1/reports/{report}/sections/1/csv"},
	{method: "DELETE", path: "/api/v1/reports/{report}"},
	{method: "GET", path: "/api/v1/relation-types"},
	{method: "POST", path: "/api/v1/relation-types", body: `{"name":"powers {n}","inverse_name":"powered by {n}","is_single":true}`},
	{method: "PUT", path: "/api/v1/relation-types/{relation}", body: `{"name":"feeds {n}","inverse_name":"fed by {n}"}`},
	{method: "DELETE", path: "/api/v1/relation-types/{relation}"},
	{method: "POST", path: "/api/v1/categories/effective-fields", body: `{"category_ids":["{category}","{category}"]}`},
	{method: "POST", path: "/api/v1/categories/{category}/fields", body: `{"field_id":"{field}","is_required":true}`},
	{method: "DELETE", path: "/api/v1/categories/{category}/fields/{field}"},
	{method: "GET", path: "/api/v1/assets", query: map[string][]string{"search": {"dri", "%", ""}, "category_id": {"{category}"}, "archived": {"true", "false"}, "condition": {"problem", "ok"}, "loan": {"open", "overdue"}}},
	{method: "GET", path: "/api/v1/assets/export.csv", query: map[string][]string{"search": {"a"}, "category_id": {"{category}"}}},
	{method: "POST", path: "/api/v1/assets/import/preview", kind: "csv"},
	{method: "POST", path: "/api/v1/assets/import", kind: "csv"},
	{method: "POST", path: "/api/v1/assets", body: `{"name":"Saw","category_ids":["{category}"],"generator_keys":[{"generator_type":"qr","assigned_code":"SAW-{n}"},{"generator_type":"code128","assigned_code":"SAW{n}"}],"field_values":{"size":"s"}}`},
	{method: "GET", path: "/api/v1/assets/{asset}"},
	{method: "PUT", path: "/api/v1/assets/{asset}", body: `{"name":"Saw","category_ids":["{category}"],"generator_keys":[{"generator_type":"qr","assigned_code":"SAW-{n}"},{"generator_type":"micro_qr","assigned_code":"{n}"}],"field_values":{"size":"l"}}`},
	{method: "POST", path: "/api/v1/assets", body: `{"name":"Screws {n}","stock":{"quantity":200,"unit":"pcs","low_stock_threshold":50}}`},
	{method: "PUT", path: "/api/v1/assets/{asset}", body: `{"stock":{"unit":"g","low_stock_threshold":300}}`},
	{method: "PUT", path: "/api/v1/assets/{asset}", body: `{"stock":null}`},
	{method: "POST", path: "/api/v1/assets/{asset}/quantity", body: `{"adjust":-60}`},
	{method: "POST", path: "/api/v1/assets/{asset}/quantity", body: `{"set":120}`},
	{method: "PUT", path: "/api/v1/assets/{asset}/running-low", body: `{"running_low":true}`},
	{method: "PUT", path: "/api/v1/assets/{asset}/condition", body: `{"condition":"needs_maintenance","note":"Blunt"}`},
	{method: "POST", path: "/api/v1/assets/{asset}/photos", kind: "image"},
	{method: "PUT", path: "/api/v1/assets/{asset}/photos", body: `{"photo_ids":[]}`},
	{method: "GET", path: "/api/v1/assets/{asset}/photos/{asset}", query: map[string][]string{"size": {"thumb", ""}}},
	{method: "DELETE", path: "/api/v1/assets/{asset}/photos/{asset}"},
	{method: "POST", path: "/api/v1/assets/{asset}/links", body: `{"type_id":"{relation}","asset_id":"{asset}","outgoing":true}`},
	{method: "DELETE", path: "/api/v1/assets/{asset}/links/{asset}"},
	{method: "POST", path: "/api/v1/assets/{asset}/checkout", body: `{"borrower_id":"{user}","due_at":"2099-01-01T00:00:00Z"}`},
	{method: "POST", path: "/api/v1/assets/{asset}/checkin", body: `{"left":250}`},
	{method: "GET", path: "/api/v1/loans", query: map[string][]string{"state": {"open", "overdue", ""}, "borrower_id": {"{user}"}}},
	{method: "GET", path: "/api/v1/people"},
	{method: "GET", path: "/api/v1/assets/{asset}/history"},
	{method: "DELETE", path: "/api/v1/assets/{asset}"},
	{method: "POST", path: "/api/v1/assets/{asset}/restore"},
	{method: "GET", path: "/api/v1/templates"},
	{method: "POST", path: "/api/v1/templates", body: fuzzTemplate},
	{method: "GET", path: "/api/v1/templates/{template}"},
	{method: "PUT", path: "/api/v1/templates/{template}", body: strings.Replace(fuzzTemplate, "tag{n}", "{template}", 1)},
	{method: "DELETE", path: "/api/v1/templates/{template}"},
	{method: "GET", path: "/api/v1/images"},
	{method: "POST", path: "/api/v1/images", kind: "image"},
	{method: "GET", path: "/api/v1/images/{image}"},
	{method: "GET", path: "/api/v1/images/{image}/content"},
	{method: "DELETE", path: "/api/v1/images/{image}"},
	{method: "GET", path: "/api/v1/assets/{asset}/label", query: map[string][]string{"template_id": {"{template}"}, "format": {"svg", "pdf", "png"}, "mono": {"true"}}},
	{method: "POST", path: "/api/v1/templates/preview", body: `{"template":` + fuzzTemplate + `,"asset_id":"{asset}"}`},
	{method: "POST", path: "/api/v1/labels/export", body: `{"template_id":"{template}","asset_ids":["{asset}"],"format":"zip","mono":true}`},
	{method: "GET", path: "/api/v1/generators"},
	{method: "GET", path: "/api/v1/printers/drivers"},
	{method: "GET", path: "/api/v1/printers"},
	{method: "GET", path: "/api/v1/printers/{printer}"},
	{method: "POST", path: "/api/v1/printers/{printer}/fit", body: `{"width_mm":62,"height_mm":29,"dpi":300}`},
	{method: "POST", path: "/api/v1/printers", body: `{"name":"Front desk {n}","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"10.0.0.9"},"media":"dk-22205"}`},
	{method: "PUT", path: "/api/v1/printers/{printer}", body: `{"name":"Back desk {n}","driver":"tspl","model":"Rollo X1038","connection":{"type":"ipp","address":"ipp://cups.lan/printers/x"},"media":"4x6"}`},
	{method: "DELETE", path: "/api/v1/printers/{printer}"},
	{method: "GET", path: "/api/v1/notifiers/drivers"},
	{method: "GET", path: "/api/v1/notifiers/deliveries"},
	{method: "GET", path: "/api/v1/notifiers"},
	{method: "POST", path: "/api/v1/notifiers", body: `{"name":"Stores {n}","driver":"discord","options":{"webhook_url":"https://discord.com/api/webhooks/1/abc"},"enabled":true,"events":["category.running_low","category.restocked","asset.running_low","asset.restocked"]}`},
	{method: "GET", path: "/api/v1/notifiers/{channel}"},
	{method: "PUT", path: "/api/v1/notifiers/{channel}", body: `{"name":"Stores {n}","driver":"discord","options":{},"enabled":false,"events":["category.restocked"]}`},
	{method: "DELETE", path: "/api/v1/notifiers/{channel}"},
	{method: "GET", path: "/assets/some/client/route"},
}

const fuzzTemplate = `{"id":"tag{n}","name":"Tag","units":"mm","width":62,"height":29,"dpi":300,"elements":[` +
	`{"type":"generator","generator_type":"qr","x":1,"y":1,"width":20,"height":20,"content_binding":"{{asset.code.qr}}"},` +
	`{"type":"text","x":23,"y":1,"width":38,"height":10,"font_size":12,"wrap":true,"min_font_size":6,"content_binding":"{{asset.name}} {{field.size}}"},` +
	`{"type":"static_image","image_id":"{image}","x":23,"y":15,"width":5,"height":5}]}`

// fuzzer sends requests to a server and checks every response.
type fuzzer struct {
	tb      testing.TB
	st      *store.Store
	h       http.Handler
	cookie  *http.Cookie
	adminID string
	ids     map[string][]string // kind -> IDs that exist, or did
	fails   int
	n       int  // for {n}
	filling bool // while refilling a pool
	// keep holds IDs the seed requests depend on, which are never changed
	// or deleted.
	keep map[string]bool
	// While building a request: whether it is being mutated, whether it
	// deletes, and the ID chosen for each kind, used for every placeholder
	// of that kind.
	mutating, deleting bool
	chosen             map[string]string
	// outcomes counts responses by route and status class, for -v.
	outcomes map[string]*[6]int
}

// newFuzzer starts a server with one of everything to aim at.
func newFuzzer(tb testing.TB) *fuzzer {
	tb.Helper()
	cfg := config.DefaultConfig()
	cfg.Storage.LocalPath = tb.TempDir()
	st := setupTestStore(tb)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		tb.Fatal(err)
	}
	sess, err := st.CreateSession(admin.ID, time.Hour)
	if err != nil {
		tb.Fatal(err)
	}
	fz := &fuzzer{tb: tb, st: st, h: NewServer(cfg, st).Handler(), cookie: &http.Cookie{Name: SessionCookieName, Value: sess.ID}, adminID: admin.ID, ids: map[string][]string{}, keep: map[string]bool{}, chosen: map[string]string{}}
	imageBody, imageType := multipartBody(nil, false, map[string][]byte{"file": tinyPNG()}, nil)
	roles, err := st.ListRoles()
	if err != nil {
		tb.Fatal(err)
	}
	for _, r := range roles {
		fz.ids["role"] = append(fz.ids["role"], r.ID)
	}
	relations, err := st.ListRelationTypes()
	if err != nil {
		tb.Fatal(err)
	}
	for _, r := range relations {
		fz.ids["relation"] = append(fz.ids["relation"], r.ID)
	}
	reports, err := st.ListReports()
	if err != nil {
		tb.Fatal(err)
	}
	for _, r := range reports {
		fz.ids["report"] = append(fz.ids["report"], r.ID)
	}
	// A category to be the parent of others, then each create once, as it
	// is written, so every kind of ID exists.
	fz.send(fuzzRequest{method: "POST", target: "/api/v1/categories", contentType: "application/json", body: []byte(`{"name":"Tools"}`), create: "category"})
	fz.send(fuzzRequest{method: "POST", target: "/api/v1/fields", contentType: "application/json", body: []byte(`{"key":"size","label":"Size","validation_type":"select","options":{"choices":[{"label":"Small","value":"s"},{"label":"Large","value":"l"}]}}`), create: "field"})
	fz.send(fuzzRequest{method: "POST", target: "/api/v1/images", contentType: imageType, body: imageBody, create: "image"})
	fz.keep[fz.ids["field"][0]], fz.keep[fz.ids["image"][0]] = true, true
	for _, path := range []string{"/api/v1/fields", "/api/v1/categories", "/api/v1/images", "/api/v1/assets", "/api/v1/templates", "/api/v1/printers", "/api/v1/notifiers", "/api/v1/users", "/api/v1/tokens"} {
		for _, r := range fuzzRoutes {
			if r.method == "POST" && r.path == path {
				fz.send(fz.request(r, rand.New(rand.NewSource(0)), false))
			}
		}
	}
	for _, kind := range []string{"field", "category", "image", "asset", "template", "printer", "channel", "user", "token"} {
		if len(fz.ids[kind]) == 0 {
			tb.Fatalf("setting up: no %s was created", kind)
		}
	}
	return fz
}

// kindOf names the kind of thing a create route makes.
func kindOf(path string) string {
	switch path {
	case "/api/v1/fields":
		return "field"
	case "/api/v1/categories":
		return "category"
	case "/api/v1/assets":
		return "asset"
	case "/api/v1/templates":
		return "template"
	case "/api/v1/images":
		return "image"
	case "/api/v1/printers":
		return "printer"
	case "/api/v1/notifiers":
		return "channel"
	case "/api/v1/roles":
		return "role"
	case "/api/v1/tokens":
		return "token"
	case "/api/v1/relation-types":
		return "relation"
	case "/api/v1/reports":
		return "report"
	case "/api/v1/users":
		return "user"
	}
	return ""
}

// junkIDs are IDs that name nothing, or try to name something else.
var junkIDs = []string{"x", "00000000-0000-0000-0000-000000000000", "..", "%2e%2e%2fetc", "uncategorized", strings.Repeat("a", 300), "%00", "%F0%9F%94%A7", "1;DROP TABLE assets"}

// id picks an ID of a kind: usually one that exists, sometimes junk. The
// fuzzer's own account is never a target, so it keeps its session. A pool
// running low is refilled with a valid create.
func (fz *fuzzer) id(kind string, rng *rand.Rand) string {
	if len(fz.ids[kind]) < 3 && !fz.filling {
		for _, r := range fuzzRoutes {
			if r.method == "POST" && kindOf(r.path) == kind {
				fz.filling = true
				fz.send(fz.request(r, rng, false))
				fz.filling = false
			}
		}
	}
	if id, ok := fz.chosen[kind]; ok {
		return id
	}
	var ids []string
	for _, id := range fz.ids[kind] {
		if !(fz.deleting && fz.keep[id]) && !(kind == "user" && id == fz.adminID) {
			ids = append(ids, id)
		}
	}
	id := junkIDs[rng.Intn(len(junkIDs))]
	if len(ids) > 0 && !(fz.mutating && rng.Intn(8) == 0) {
		id = ids[rng.Intn(len(ids))]
	}
	fz.chosen[kind] = id
	return id
}

// fill replaces {kind} placeholders with IDs, and {n} with a number not
// used before. It returns the first ID it chose, and its kind.
func (fz *fuzzer) fill(s string, rng *rand.Rand, escape bool) (string, string, string) {
	firstKind, firstID, first := "", "", len(s)
	for strings.Contains(s, "{n}") {
		fz.n++
		s = strings.Replace(s, "{n}", strconv.Itoa(fz.n), 1)
	}
	for _, kind := range []string{"user", "role", "token", "relation", "report", "field", "category", "asset", "template", "image", "printer", "channel"} {
		for strings.Contains(s, "{"+kind+"}") {
			id := fz.id(kind, rng)
			if !escape {
				// Junk IDs are written for URLs; in bodies they are text.
				if u, err := url.PathUnescape(id); err == nil {
					id = u
				}
			}
			if i := strings.Index(s, "{"+kind+"}"); i < first {
				firstKind, firstID, first = kind, id, i
			}
			s = strings.Replace(s, "{"+kind+"}", id, 1)
		}
	}
	return s, firstKind, firstID
}

// fuzzRequest is a request ready to send, kept for reporting.
type fuzzRequest struct {
	method, target, contentType string
	body                        []byte
	create                      string // the kind a successful request creates
	deleteKind, deleteID        string // what a successful DELETE removes
	route                       string // the route it was made from
}

func (r fuzzRequest) String() string {
	body := r.body
	if len(body) > 600 {
		body = append(body[:600:600], "..."...)
	}
	return fmt.Sprintf("%s %s (%s) %q", r.method, r.target, r.contentType, body)
}

// request builds a request for a route, mutated when mutate is set.
func (fz *fuzzer) request(r route, rng *rand.Rand, mutate bool) fuzzRequest {
	saved := [3]any{fz.mutating, fz.deleting, fz.chosen}
	defer func() {
		fz.mutating, fz.deleting, fz.chosen = saved[0].(bool), saved[1].(bool), saved[2].(map[string]string)
	}()
	fz.mutating, fz.deleting, fz.chosen = mutate, r.method != "GET" && r.method != "POST", map[string]string{}
	target, kind, id := fz.fill(r.path, rng, true)
	fz.deleting = false
	req := fuzzRequest{method: r.method, target: target, route: r.path}
	if r.method == "DELETE" {
		req.deleteKind, req.deleteID = kind, id
	}
	if r.method == "POST" {
		req.create = kindOf(r.path)
	}
	q := url.Values{}
	for k, vs := range r.query {
		if !mutate || rng.Intn(3) > 0 {
			v, _, _ := fz.fill(vs[rng.Intn(len(vs))], rng, false)
			q.Set(k, v)
		}
		if mutate && rng.Intn(4) == 0 {
			q.Set(k, fuzzString(rng))
		}
	}
	if mutate && rng.Intn(10) == 0 {
		q.Set(fuzzString(rng), fuzzString(rng))
	}
	if len(q) > 0 {
		req.target += "?" + q.Encode()
	}
	switch r.kind {
	case "csv":
		file, mapping := fz.csv(rng, mutate)
		req.body, req.contentType = multipartBody(rng, mutate, map[string][]byte{"file": file}, map[string]string{"mapping": mapping})
	case "image":
		data := tinyPNG()
		if mutate {
			data = mutateBytes(rng, data)
		}
		req.body, req.contentType = multipartBody(rng, mutate, map[string][]byte{"file": data}, nil)
	default:
		if r.body == "" && !(mutate && rng.Intn(10) == 0) {
			break
		}
		body, _, _ := fz.fill(r.body, rng, false)
		if mutate {
			body = mutateJSON(rng, body)
		}
		req.body, req.contentType = []byte(body), "application/json"
		if mutate && rng.Intn(20) == 0 {
			req.contentType = []string{"", "text/plain", "multipart/form-data", "application/json; charset=latin1"}[rng.Intn(4)]
		}
	}
	return req
}

// send makes a request and checks the response: never a server error,
// errors from the API always a JSON object with a message, and JSON
// responses always valid. A successful create adds its ID to the pool.
func (fz *fuzzer) send(req fuzzRequest) {
	rec, u := fz.serve(req)

	fail := func(format string, args ...any) {
		fz.fails++
		fz.tb.Errorf("%s\n  -> %d %.400s\n  %s", req, rec.Code, rec.Body.Bytes(), fmt.Sprintf(format, args...))
		if fz.fails >= 10 {
			fz.tb.FailNow()
		}
	}
	if fz.outcomes != nil {
		key := req.method + " " + req.route
		if fz.outcomes[key] == nil {
			fz.outcomes[key] = &[6]int{}
		}
		fz.outcomes[key][min(rec.Code/100, 5)]++
	}
	ct := rec.Header().Get("Content-Type")
	switch {
	case rec.Code >= 500:
		fail("server error")
	case rec.Code >= 400 && strings.HasPrefix(u.Path, "/api/"):
		var e struct {
			Error string `json:"error"`
		}
		if !strings.HasPrefix(ct, "application/json") || json.Unmarshal(rec.Body.Bytes(), &e) != nil || e.Error == "" {
			fail("an error without a JSON message (Content-Type %q)", ct)
		}
	case strings.HasPrefix(ct, "application/json") && !json.Valid(rec.Body.Bytes()):
		fail("invalid JSON")
	}
	if req.deleteKind != "" && rec.Code/100 == 2 {
		ids := fz.ids[req.deleteKind][:0]
		for _, id := range fz.ids[req.deleteKind] {
			if id != req.deleteID {
				ids = append(ids, id)
			}
		}
		fz.ids[req.deleteKind] = ids
	}
	if req.create != "" && rec.Code == http.StatusCreated {
		if id := firstID(rec.Body.Bytes()); id != "" {
			fz.ids[req.create] = append(fz.ids[req.create], id)
		}
	}
}

// serve makes a request and returns the response, unchecked.
func (fz *fuzzer) serve(req fuzzRequest) (*httptest.ResponseRecorder, *url.URL) {
	httpReq := httptest.NewRequest(req.method, "/", bytes.NewReader(req.body))
	u, err := url.Parse(req.target)
	if err != nil {
		fz.tb.Fatalf("building %s: %v", req, err)
	}
	httpReq.URL = u
	httpReq.RequestURI = req.target
	if req.contentType != "" {
		httpReq.Header.Set("Content-Type", req.contentType)
	}
	httpReq.AddCookie(fz.cookie)
	rec := httptest.NewRecorder()
	fz.h.ServeHTTP(rec, httpReq)
	return rec, u
}

// alive checks the server still answers, and still knows the session.
func (fz *fuzzer) alive() {
	for _, path := range []string{"/healthz", "/api/v1/auth/me"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(fz.cookie)
		fz.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			fz.tb.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.Bytes())
		}
	}
}

// firstID finds the first "id" in a JSON document, breadth first.
func firstID(doc []byte) string {
	var v any
	if json.Unmarshal(doc, &v) != nil {
		return ""
	}
	queue := []any{v}
	for len(queue) > 0 {
		m, ok := queue[0].(map[string]any)
		queue = queue[1:]
		if !ok {
			continue
		}
		if id, ok := m["id"].(string); ok {
			return id
		}
		for _, e := range m {
			queue = append(queue, e)
		}
	}
	return ""
}

// fuzzStrings are awkward values for text and IDs.
var fuzzStrings = []string{
	"", " ", "a", "Drill", "drill ", "0", "-1", "1e309", "NaN", "true", "null", "{}", "[]",
	"🔧", "e" + string(rune(0x301)), "a" + string(rune(0)) + "b", string([]byte{0xff, 0xfe}),
	"%", "_", `\`, "'; DROP TABLE assets; --", `"`, "<script>alert(1)</script>", "../../etc/passwd",
	"=1+1", "{{asset.name}}", "{{", "Tools > Power", "Tools;Paint", "#ff8800", "#zzz",
	"category.running_low", "qr", "code128", "micro_qr", "select", "number", "date", "url", "text",
	"tcp", "ipp", "device", "windows", "/dev/usb/lp0", "10.0.0.9:9100", "https://discord.com/api/webhooks/1/abc",
	"2026-02-30", "9999-12-31", "mm", "in", "svg", "pdf", "png", "zip",
}

func fuzzString(rng *rand.Rand) string {
	switch rng.Intn(10) {
	case 0:
		return strings.Repeat(fuzzStrings[rng.Intn(len(fuzzStrings))], 1+rng.Intn(400))
	case 1:
		b := make([]byte, rng.Intn(40))
		rng.Read(b)
		return string(b)
	}
	return fuzzStrings[rng.Intn(len(fuzzStrings))]
}

// fuzzValue is an awkward JSON value.
func fuzzValue(rng *rand.Rand, depth int) any {
	switch rng.Intn(14) {
	case 0:
		return nil
	case 1:
		return rng.Intn(2) == 0
	case 2:
		return []float64{0, -1, 1, 0.5, -0.0001, 1e9, 1e300, -1e300, 1 << 53, 9007199254740993, 2147483648}[rng.Intn(11)]
	case 3:
		if depth > 0 {
			n := rng.Intn(4)
			if rng.Intn(20) == 0 {
				n = 600
			}
			a := make([]any, n)
			for i := range a {
				a[i] = fuzzValue(rng, depth-1)
			}
			return a
		}
	case 4:
		if depth > 0 {
			return map[string]any{fuzzString(rng): fuzzValue(rng, depth-1)}
		}
	case 5:
		// Deep nesting.
		var v any = "deep"
		for i := 0; i < 2000; i++ {
			v = []any{v}
		}
		return v
	}
	return fuzzString(rng)
}

// mutateJSON changes a few values in a JSON document, or breaks it.
func mutateJSON(rng *rand.Rand, doc string) string {
	switch rng.Intn(20) {
	case 0:
		return string(mutateBytes(rng, []byte(doc)))
	case 1:
		if len(doc) > 0 {
			return doc[:rng.Intn(len(doc))]
		}
	case 2:
		raw, _ := json.Marshal(fuzzValue(rng, 3))
		return string(raw)
	}
	var v any
	if json.Unmarshal([]byte(doc), &v) != nil {
		return doc
	}
	for n := 1 + rng.Intn(3)/2; n > 0; n-- {
		v = mutateValue(rng, v)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return doc
	}
	return string(raw)
}

// mutateValue changes one place in a decoded JSON value.
func mutateValue(rng *rand.Rand, v any) any {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 || rng.Intn(6) == 0 {
			x[fuzzString(rng)] = fuzzValue(rng, 2)
			return x
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		// Map order is random; sort for a repeatable choice.
		sort.Strings(keys)
		k := keys[rng.Intn(len(keys))]
		switch rng.Intn(4) {
		case 0:
			delete(x, k)
		case 1:
			x[k] = fuzzValue(rng, 2)
		default:
			x[k] = mutateValue(rng, x[k])
		}
		return x
	case []any:
		if len(x) == 0 || rng.Intn(5) == 0 {
			return append(x, fuzzValue(rng, 2))
		}
		i := rng.Intn(len(x))
		if rng.Intn(4) == 0 {
			return append(x[:i], x[i+1:]...)
		}
		x[i] = mutateValue(rng, x[i])
		return x
	}
	return fuzzValue(rng, 2)
}

// mutateBytes flips, inserts and drops bytes.
func mutateBytes(rng *rand.Rand, b []byte) []byte {
	b = append([]byte(nil), b...)
	for n := 1 + rng.Intn(8); n > 0 && len(b) > 0; n-- {
		i := rng.Intn(len(b))
		switch rng.Intn(3) {
		case 0:
			b[i] = byte(rng.Intn(256))
		case 1:
			b = append(b[:i], append([]byte{byte(rng.Intn(256))}, b[i:]...)...)
		default:
			b = append(b[:i], b[i+1:]...)
		}
	}
	return b
}

// csvTargets are the columns an import can map to.
var csvTargets = []string{"id", "name", "categories", "code.qr", "code.code128", "code.micro_qr", "field.size", "field.weight", "ignore", "field.nope", "code.nope", "colour", ""}

// csv makes an import file and its mapping.
func (fz *fuzzer) csv(rng *rand.Rand, mutate bool) ([]byte, string) {
	cols := []string{"name", "categories", "code.qr"}
	if mutate {
		cols = nil
		for n := 1 + rng.Intn(6); n > 0; n-- {
			cols = append(cols, csvTargets[rng.Intn(len(csvTargets))])
		}
	}
	cell := func(target string) string {
		if !mutate {
			switch target {
			case "name":
				return "Chisel " + strconv.Itoa(rng.Intn(1e6))
			case "categories":
				return ""
			default:
				return "CH-" + strconv.Itoa(rng.Intn(1e9))
			}
		}
		switch rng.Intn(6) {
		case 0:
			return fz.id("asset", rng)
		case 1:
			return fz.id("category", rng)
		case 2:
			return []string{"s", "l", "2.5", "Tools", "Tools > Power", "Nope > Nothing", "Paint;Tools", ""}[rng.Intn(8)]
		}
		return fuzzString(rng)
	}
	var b bytes.Buffer
	if mutate && rng.Intn(4) == 0 {
		b.WriteString(string(rune(0xFEFF)))
	}
	writeRow := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(',')
			}
			if mutate && rng.Intn(10) == 0 {
				b.WriteString(c) // unquoted, whatever it holds
				continue
			}
			b.WriteString(`"` + strings.ReplaceAll(c, `"`, `""`) + `"`)
		}
		b.WriteString([]string{"\n", "\r\n"}[rng.Intn(2)])
	}
	writeRow(cols)
	for n := rng.Intn(6); n > 0; n-- {
		row := make([]string, len(cols))
		for i := range row {
			row[i] = cell(cols[i])
		}
		if mutate && rng.Intn(10) == 0 {
			row = row[:rng.Intn(len(row)+1)]
		}
		writeRow(row)
	}
	data := b.Bytes()
	if mutate && rng.Intn(8) == 0 {
		data = mutateBytes(rng, data)
	}
	mapping := cols
	if mutate && rng.Intn(4) == 0 {
		mapping = append([]string(nil), cols...)
		if len(mapping) > 0 {
			mapping[rng.Intn(len(mapping))] = csvTargets[rng.Intn(len(csvTargets))]
		}
		if rng.Intn(3) == 0 {
			mapping = mapping[:rng.Intn(len(mapping)+1)]
		}
	}
	raw, _ := json.Marshal(mapping)
	if mutate && rng.Intn(10) == 0 {
		raw = []byte(mutateJSON(rng, string(raw)))
	}
	return data, string(raw)
}

// multipartBody builds a form, sometimes damaged.
func multipartBody(rng *rand.Rand, mutate bool, files map[string][]byte, fields map[string]string) ([]byte, string) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for name, data := range files {
		if mutate && rng.Intn(15) == 0 {
			continue
		}
		filename := "upload.csv"
		if mutate && rng.Intn(5) == 0 {
			filename = fuzzString(rng)
		}
		w, _ := mw.CreateFormFile(name, filename)
		_, _ = w.Write(data)
	}
	for name, v := range fields {
		if mutate && rng.Intn(15) == 0 {
			continue
		}
		_ = mw.WriteField(name, v)
	}
	_ = mw.Close()
	body := b.Bytes()
	if mutate && rng.Intn(15) == 0 {
		body = mutateBytes(rng, body)
	}
	return body, mw.FormDataContentType()
}

func tinyPNG() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 4)))
	return b.Bytes()
}

// envInt reads a positive integer from the environment.
func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}
	return def
}

// TestSeededFuzz sends requests to every endpoint made by mutating valid
// ones: values of the wrong type, out of range or awkward, missing and
// extra keys, broken JSON, damaged forms and files, junk IDs, and imports
// of generated CSV. The sequence is fixed by its seed, so a failure repeats;
// BAM_FUZZ_SEED and BAM_FUZZ_REQUESTS choose another seed and length.
func TestSeededFuzz(t *testing.T) {
	seed := int64(envInt("BAM_FUZZ_SEED", 1))
	n := envInt("BAM_FUZZ_REQUESTS", 1500)
	if testing.Short() {
		n = 200
	}
	fz := newFuzzer(t)
	fz.outcomes = map[string]*[6]int{}
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		r := fuzzRoutes[rng.Intn(len(fuzzRoutes))]
		fz.send(fz.request(r, rng, rng.Intn(8) > 0))
		if i%100 == 99 {
			fz.alive()
		}
	}
	fz.alive()
	if testing.Verbose() {
		keys := make([]string, 0, len(fz.outcomes))
		for k := range fz.outcomes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			o := fz.outcomes[k]
			t.Logf("%-55s 2xx %4d  3xx %4d  4xx %4d", k, o[2], o[3], o[4])
		}
	}
}

// FuzzAPI lets Go's fuzzer write the request body for any endpoint, for
// longer runs than TestSeededFuzz: go test -fuzz=FuzzAPI ./internal/api/
func FuzzAPI(f *testing.F) {
	fz := newFuzzer(f)
	rng := rand.New(rand.NewSource(1))
	for i, r := range fuzzRoutes {
		req := fz.request(r, rng, false)
		f.Add(uint8(i), uint8(0), req.body, "")
	}
	f.Fuzz(func(t *testing.T, routeIndex, idSeed uint8, body []byte, query string) {
		r := fuzzRoutes[int(routeIndex)%len(fuzzRoutes)]
		req := fz.request(r, rand.New(rand.NewSource(int64(idSeed))), false)
		req.body = body
		if q, err := url.ParseQuery(query); err == nil && len(q) > 0 {
			req.target = strings.SplitN(req.target, "?", 2)[0] + "?" + q.Encode()
		}
		fz.tb = t
		fz.fails = 0
		fz.send(req)
	})
}
