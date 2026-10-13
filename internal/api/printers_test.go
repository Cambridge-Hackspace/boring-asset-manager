package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/google/uuid"
)

// fakeQL is a TCP server standing in for a Brother QL printer.
type fakeQL struct {
	ln   net.Listener
	mu   sync.Mutex
	jobs [][]byte
}

func newFakeQL(t *testing.T) *fakeQL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeQL{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			data, _ := io.ReadAll(conn)
			conn.Close()
			f.mu.Lock()
			f.jobs = append(f.jobs, data)
			f.mu.Unlock()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeQL) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

// waitForJobs waits up to timeout for n jobs to arrive; the fake may still be
// reading after the client returns.
func (f *fakeQL) waitForJobs(t *testing.T, n int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		f.mu.Lock()
		jobs := append([][]byte(nil), f.jobs...)
		f.mu.Unlock()
		if len(jobs) >= n || time.Now().After(deadline) {
			return jobs
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func createPrinter(t *testing.T, srv *Server, cookie *http.Cookie, port int, media string) string {
	t.Helper()
	body := `{"name":"QL ` + media + `","driver":"brother_ql","model":"QL-810W","host":"127.0.0.1","port":` + strconv.Itoa(port) + `,"media":"` + media + `"}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Printer.ID
}

func TestPrinterAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/drivers", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"dk-11201"`) {
		t.Errorf("drivers: got %d %s", rec.Code, rec.Body.String())
	}

	id := createPrinter(t, srv, cookie, 9100, "dk-11201")
	dup := `{"name":"QL dk-11201","driver":"brother_ql","model":"QL-810W","host":"10.0.0.9","media":"dk-22210"}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", dup); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: got %d", rec.Code)
	}
	bad := `{"name":"X","driver":"brother_ql","model":"QL-810W","host":"http://10.0.0.9","media":"dk-22210"}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", bad); rec.Code != http.StatusBadRequest {
		t.Errorf("bad host: got %d", rec.Code)
	}

	update := `{"name":"Front desk","driver":"brother_ql","model":"QL-820NWB","host":"10.0.0.9","media":"dk-22205"}`
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/printers/"+id, update)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"port":9100`) {
		t.Errorf("update: got %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Front desk"`) {
		t.Errorf("list: got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/printers/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: got %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+id, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d", rec.Code)
	}
}

// userWithPermissions creates a user holding exactly perms through a new role.
func userWithPermissions(t *testing.T, srv *Server, email string, perms ...string) *http.Cookie {
	t.Helper()
	user, err := srv.store.CreateUser(email, "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	roleID := uuid.NewString()
	if _, err := srv.store.Exec("INSERT INTO roles (id, name, description) VALUES (?, ?, ?)", roleID, "role-"+email, "test"); err != nil {
		t.Fatal(err)
	}
	for _, perm := range perms {
		if _, err := srv.store.Exec(`INSERT INTO role_permissions (role_id, permission_id)
			SELECT ?, id FROM permissions WHERE permission_key = ?`, roleID, perm); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.store.AssignUserRole(user.ID, roleID); err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, srv, user.ID)
}

func TestPrinterPermissions(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	id := createPrinter(t, srv, sessionCookie(t, srv, admin.ID), 9100, "dk-11201")

	printer := userWithPermissions(t, srv, "printer@test.org", rbac.PermLabelsPrint)
	nobody := userWithPermissions(t, srv, "nobody@test.org")

	cases := []struct {
		cookie       *http.Cookie
		method, path string
		want         int
	}{
		{printer, http.MethodGet, "/api/v1/printers", http.StatusOK},
		{printer, http.MethodGet, "/api/v1/printers/" + id, http.StatusOK},
		{printer, http.MethodPost, "/api/v1/printers", http.StatusForbidden},
		{printer, http.MethodDelete, "/api/v1/printers/" + id, http.StatusForbidden},
		{nobody, http.MethodGet, "/api/v1/printers", http.StatusForbidden},
		{nobody, http.MethodGet, "/api/v1/printers/drivers", http.StatusForbidden},
		{nobody, http.MethodPost, "/api/v1/printers/" + id + "/print", http.StatusForbidden},
	}
	for _, c := range cases {
		if rec := doJSON(t, srv, c.cookie, c.method, c.path, "{}"); rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}

const landscapeTemplate = `{"id":"ql90","name":"QL 90x29","units":"mm","width":90,"height":29,"dpi":300,"elements":[
  {"type":"generator","generator_type":"qr","x":3,"y":3,"width":23,"height":23,"content_binding":"{{base_url}}/a/{{asset.id}}"},
  {"type":"text","x":28,"y":4,"width":58,"height":10,"font_size":16,"font_weight":"bold","content_binding":"{{asset.name}}"}]}`

func TestPrintSendsRotatedLabelsToThePrinter(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", landscapeTemplate); rec.Code != http.StatusCreated {
		t.Fatalf("template: %d %s", rec.Code, rec.Body.String())
	}
	a, _ := srv.store.CreateAsset("Bench Drill Press", nil, nil, nil)
	b, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)

	body := `{"template_id":"ql90","copies":2,"asset_ids":["` + a.Asset.ID + `","` + b.Asset.ID + `"]}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"labels":4`) {
		t.Fatalf("print: got %d %s", rec.Code, rec.Body.String())
	}

	jobs := ql.waitForJobs(t, 1, 3*time.Second)
	if len(jobs) != 1 {
		t.Fatalf("printer received %d jobs, want 1", len(jobs))
	}
	pages, err := brotherql.DecodeJob(jobs[0])
	if err != nil {
		t.Fatalf("printer could not decode the job: %v", err)
	}
	if len(pages) != 4 {
		t.Fatalf("job has %d pages, want 4", len(pages))
	}

	// Each page must be exactly the 1-bit label, rotated onto the 29 mm tape
	// and cropped to the printable area.
	media, _ := brotherql.LookupMedia("dk-11201")
	tpl, err := labels.Parse([]byte(landscapeTemplate))
	if err != nil {
		t.Fatal(err)
	}
	for i, assetID := range []string{a.Asset.ID, a.Asset.ID, b.Asset.ID, b.Asset.ID} {
		asset, _ := srv.store.GetAssetByID(assetID)
		scene, err := render.Layout(tpl, srv.labelData(asset), nil)
		if err != nil {
			t.Fatal(err)
		}
		want := media.Prepare(scene.Raster(true), brotherql.Placement{Rotate: true})
		got := brotherql.PageImage(media, pages[i])
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Errorf("page %d does not match the rendered label for %s", i, assetID)
		}
	}
}

func TestPrintRefusesBeforeSendingAnything(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	dieCut := createPrinter(t, srv, cookie, ql.port(), "dk-11201")

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", landscapeTemplate); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	small := `{"id":"small","name":"Small","units":"mm","width":40,"height":20,"dpi":300,"elements":[
	  {"type":"generator","generator_type":"micro_qr","x":1,"y":1,"width":18,"height":18}]}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", small); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	id := `"` + asset.Asset.ID + `"`

	cases := []struct {
		name, body string
		want       int
		contains   string
	}{
		{"wrong size for the roll", `{"template_id":"small","asset_ids":[` + id + `]}`, http.StatusUnprocessableEntity, "needs a 29 × 90 mm template"},
		{"too many copies", `{"template_id":"ql90","copies":21,"asset_ids":[` + id + `]}`, http.StatusBadRequest, "copies must be"},
		{"unknown template", `{"template_id":"nope","asset_ids":[` + id + `]}`, http.StatusNotFound, "Template not found"},
		{"no assets", `{"template_id":"ql90","asset_ids":[]}`, http.StatusBadRequest, "asset_ids must list"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+dieCut+"/print", c.body)
		if rec.Code != c.want || !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s: got %d %s", c.name, rec.Code, rec.Body.String())
		}
	}

	// An asset whose code is missing fails layout; nothing reaches the printer.
	microTemplate := `{"id":"micro90","name":"Micro","units":"mm","width":29,"height":90,"dpi":300,"elements":[
	  {"type":"generator","generator_type":"micro_qr","x":2,"y":2,"width":20,"height":20}]}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", microTemplate); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+dieCut+"/print", `{"template_id":"micro90","asset_ids":[`+id+`]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "asset has no micro_qr code") {
		t.Errorf("layout failure: got %d %s", rec.Code, rec.Body.String())
	}

	// 26 assets × 20 copies is over the job limit.
	many := make([]string, 26)
	for i := range many {
		many[i] = strconv.Quote(uuid.NewString())
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+dieCut+"/print",
		`{"template_id":"ql90","copies":20,"asset_ids":[`+strings.Join(many, ",")+`]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "limited to 500 labels") {
		t.Errorf("job limit: got %d %s", rec.Code, rec.Body.String())
	}

	if jobs := ql.waitForJobs(t, 1, 200*time.Millisecond); len(jobs) != 0 {
		t.Errorf("printer received %d jobs from refused requests", len(jobs))
	}
}

func TestPrintToAnUnreachablePrinter(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	printerID := createPrinter(t, srv, cookie, port, "dk-11201")
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", landscapeTemplate); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"ql90","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "printer unreachable") {
		t.Errorf("got %d %s, want 502", rec.Code, rec.Body.String())
	}
}
