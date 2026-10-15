package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolw"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
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
	body := `{"name":"QL ` + media + `","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(port) + `"},"media":"` + media + `"}`
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
	dup := `{"name":"QL dk-11201","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"10.0.0.9"},"media":"dk-22210"}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", dup); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: got %d", rec.Code)
	}
	bad := `{"name":"X","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"http://10.0.0.9"},"media":"dk-22210"}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", bad); rec.Code != http.StatusBadRequest {
		t.Errorf("bad host: got %d", rec.Code)
	}

	update := `{"name":"Front desk","driver":"brother_ql","model":"QL-820NWB","connection":{"type":"tcp","address":"10.0.0.9"},"media":"dk-22205"}`
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/printers/"+id, update)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"address":"10.0.0.9:9100"`) {
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
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	a, _ := srv.store.CreateAsset("Bench Drill Press", nil, nil, nil)
	b, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)

	body := `{"template_id":"` + ql90 + `","copies":2,"asset_ids":["` + a.Asset.ID + `","` + b.Asset.ID + `"]}`
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

	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	small := `{"id":"small","name":"Small","units":"mm","width":40,"height":20,"dpi":300,"elements":[
	  {"type":"generator","generator_type":"micro_qr","x":1,"y":1,"width":18,"height":18}]}`
	smallID := createTemplate(t, srv, cookie, small)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	id := `"` + asset.Asset.ID + `"`

	cases := []struct {
		name, body string
		want       int
		contains   string
	}{
		{"wrong size for the roll", `{"template_id":"` + smallID + `","asset_ids":[` + id + `]}`, http.StatusUnprocessableEntity, "needs a 29 × 90 mm template"},
		{"too many copies", `{"template_id":"` + ql90 + `","copies":21,"asset_ids":[` + id + `]}`, http.StatusBadRequest, "copies must be"},
		{"unknown template", `{"template_id":"nope","asset_ids":[` + id + `]}`, http.StatusNotFound, "Template not found"},
		{"no assets", `{"template_id":"` + ql90 + `","asset_ids":[]}`, http.StatusBadRequest, "asset_ids must list"},
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
	micro90 := createTemplate(t, srv, cookie, microTemplate)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+dieCut+"/print", `{"template_id":"`+micro90+`","asset_ids":[`+id+`]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "asset has no micro_qr code") {
		t.Errorf("layout failure: got %d %s", rec.Code, rec.Body.String())
	}

	// 26 assets × 20 copies is over the job limit.
	many := make([]string, 26)
	for i := range many {
		many[i] = strconv.Quote(uuid.NewString())
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+dieCut+"/print",
		`{"template_id":"`+ql90+`","copies":20,"asset_ids":[`+strings.Join(many, ",")+`]}`)
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
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+ql90+`","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "printer unreachable") {
		t.Errorf("got %d %s, want 502", rec.Code, rec.Body.String())
	}
}

// stubDriver is a second driver, registered only in tests, proving the print
// path depends on the Driver interface rather than on Brother.
type stubDriver struct{}

func (stubDriver) Info() printers.DriverInfo {
	return printers.DriverInfo{Driver: "stub", Models: []string{"Stub 1"}, Media: []printers.MediaInfo{{ID: "any", Name: "Any size"}}}
}

func (stubDriver) Validate(s printers.Settings) error {
	if s.Model != "Stub 1" || s.Media != "any" {
		return errors.New("model must be Stub 1 and media any")
	}
	return nil
}

func (stubDriver) Plan(s printers.Settings, label printers.LabelSpec) (printers.Plan, error) {
	return stubPlan{}, nil
}

type stubPlan struct{}

// Encode writes one line per label: its size and how many pixels are dark.
func (stubPlan) Encode(labels []*image.Gray) ([]byte, error) {
	var out strings.Builder
	for _, l := range labels {
		dark := 0
		for _, v := range l.Pix {
			if v == 0 {
				dark++
			}
		}
		fmt.Fprintf(&out, "STUB %dx%d %d\n", l.Bounds().Dx(), l.Bounds().Dy(), dark)
	}
	return []byte(out.String()), nil
}

func init() {
	if err := printers.Register(stubDriver{}); err != nil {
		panic(err)
	}
}

func TestPrintThroughAnotherDriver(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/drivers", "")
	if !strings.Contains(rec.Body.String(), `"driver":"stub"`) || !strings.Contains(rec.Body.String(), `"driver":"brother_ql"`) {
		t.Errorf("drivers endpoint does not list both drivers: %s", rec.Body.String())
	}

	body := `{"name":"Stub","driver":"stub","model":"Stub 1","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(ql.port()) + `"},"media":"any"}`
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating stub printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", strings.Replace(body, `"Stub 1"`, `"QL-810W"`, 1)); rec.Code != http.StatusBadRequest {
		t.Errorf("stub driver validation not applied: got %d", rec.Code)
	}

	// Any template size suits the stub, including one no Brother roll takes.
	small := `{"id":"small","name":"Small","units":"mm","width":40,"height":20,"dpi":300,"elements":[
	  {"type":"text","x":1,"y":1,"width":38,"height":18,"font_size":20,"content_binding":"{{asset.name}}"}]}`
	smallID := createTemplate(t, srv, cookie, small)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print",
		`{"template_id":"`+smallID+`","copies":3,"asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("print: got %d %s", rec.Code, rec.Body.String())
	}

	jobs := ql.waitForJobs(t, 1, 3*time.Second)
	if len(jobs) != 1 {
		t.Fatalf("received %d jobs", len(jobs))
	}
	lines := strings.Split(strings.TrimSpace(string(jobs[0])), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "STUB 472x236 ") || strings.HasSuffix(lines[0], " 0") {
		t.Errorf("stub job = %q, want three non-blank 472x236 labels", jobs[0])
	}
}

func TestPrinterOptionsReachTheJob(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)

	body := `{"name":"Uncut","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(ql.port()) +
		`"},"media":"dk-11201","options":{"cut":false}}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"options":{"cut":false}`) {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	bad := strings.Replace(body, `{"cut":false}`, `{"cut":false,"speed":2}`, 1)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", strings.Replace(bad, `"Uncut"`, `"Other"`, 1)); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown option: got %d", rec.Code)
	}

	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+ql90+`","asset_ids":["`+asset.Asset.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body.String())
	}
	pages, err := brotherql.DecodeJob(ql.waitForJobs(t, 1, 3*time.Second)[0])
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].AutoCut {
		t.Error("printer configured not to cut, but the job switches the cutter on")
	}
}

func TestPrintToDymoLabelWriter(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	lw := newFakeQL(t)

	body := `{"name":"Dymo","driver":"dymo_labelwriter","model":"LabelWriter 450","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(lw.port()) +
		`"},"media":"30252","options":{"density":"dark"}}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	twin := strings.NewReplacer(`"Dymo"`, `"Dymo 2"`, `"density":"dark"`, `"roll":"left"`).Replace(body)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", twin); rec.Code != http.StatusBadRequest {
		t.Errorf("roll on a single-roll model: got %d", rec.Code)
	}

	tmpl := `{"id":"dymo89","name":"Dymo 89x28","units":"mm","width":89,"height":28,"dpi":300,"elements":[
  {"type":"generator","generator_type":"qr","x":3,"y":3,"width":22,"height":22,"content_binding":"{{base_url}}/a/{{asset.id}}"},
  {"type":"text","x":28,"y":4,"width":58,"height":10,"font_size":16,"font_weight":"bold","content_binding":"{{asset.name}}"}]}`
	dymo89 := createTemplate(t, srv, cookie, tmpl)
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	print := func(template string) *httptest.ResponseRecorder {
		return doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print",
			`{"template_id":"`+template+`","asset_ids":["`+asset.Asset.ID+`"],"copies":2}`)
	}
	if rec := print(ql90); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Brother-sized template on a Dymo address roll: got %d %s", rec.Code, rec.Body.String())
	}
	if rec := print(dymo89); rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body.String())
	}

	job, err := dymolw.DecodeJob(dymolw.ProtocolV1, lw.waitForJobs(t, 1, 3*time.Second)[0], 331)
	if err != nil {
		t.Fatal(err)
	}
	if job.Density != 'g' || len(job.Pages) != 2 || job.Pages[0].Length != 1051+36 {
		t.Fatalf("decoded density %q, %d pages, length %d", job.Density, len(job.Pages), job.Pages[0].Length)
	}
	dark := 0
	for _, v := range job.Pages[0].Image.Pix {
		if v < 0x80 {
			dark++
		}
	}
	if dark < 10000 {
		t.Errorf("label has only %d dark dots", dark)
	}
}

func TestPrintToRollo(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rollo := newFakeQL(t)

	body := `{"name":"Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"tcp","address":"127.0.0.1:` + strconv.Itoa(rollo.port()) +
		`"},"media":"2x1","options":{"density":10}}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	tmpl := `{"id":"rollo2x1","name":"Rollo 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"generator","generator_type":"qr","x":0.1,"y":0.1,"width":0.8,"height":0.8,"content_binding":"{{base_url}}/a/{{asset.id}}"},
  {"type":"text","x":1,"y":0.2,"width":0.9,"height":0.4,"font_size":10,"font_weight":"bold","content_binding":"{{asset.name}}"}]}`
	rollo2x1 := createTemplate(t, srv, cookie, tmpl)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	b, _ := srv.store.CreateAsset("Saw", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print",
		`{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`","`+b.Asset.ID+`"],"copies":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body.String())
	}

	job, err := tspl.DecodeJob(rollo.waitForJobs(t, 1, 3*time.Second)[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Labels) != 2 || job.Labels[0].Copies != 2 || job.Labels[1].Copies != 2 {
		t.Fatalf("decoded %d labels: %+v", len(job.Labels), job.Labels)
	}
	if setup := strings.Join(job.Setup, "|"); setup != "SIZE 50.8 mm,25.4 mm|GAP 3.2 mm,0 mm|DIRECTION 0,0|REFERENCE 0,0|DENSITY 10" {
		t.Errorf("setup = %q", setup)
	}
	dark := 0
	for _, v := range job.Labels[0].Image.Pix {
		if v < 0x80 {
			dark++
		}
	}
	if dark < 5000 {
		t.Errorf("label has only %d dark dots", dark)
	}
}

func TestPrintToADevice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("device connections take Unix paths")
	}
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	dir := filepath.ToSlash(t.TempDir())
	device := dir + "/lp0"
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"USB Rollo","driver":"tspl","model":"Rollo X1038","connection":{"type":"device","address":"` + device + `"},"media":"2x1"}`

	// The default configuration allows no devices.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "devices") {
		t.Fatalf("device printer without allowed devices: %d %s", rec.Code, rec.Body.String())
	}

	srv.transport = printers.NewDispatcher([]string{dir + "/lp*"})
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating printer: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Printer models.Printer `json:"printer"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	tmpl := `{"id":"rollo2x1","name":"Rollo 2x1","units":"in","width":2,"height":1,"dpi":203,"elements":[
  {"type":"text","x":0.1,"y":0.2,"width":1.8,"height":0.4,"font_size":10,"content_binding":"{{asset.name}}"}]}`
	rollo2x1 := createTemplate(t, srv, cookie, tmpl)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("print: %d %s", rec.Code, rec.Body.String())
	}
	written, _ := os.ReadFile(device)
	if job, err := tspl.DecodeJob(written); err != nil || len(job.Labels) != 1 {
		t.Errorf("device received %d bytes that decode to %+v, %v", len(written), job, err)
	}

	// A device later dropped from the configuration is refused at print time.
	srv.transport = printers.NewDispatcher(nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+created.Printer.ID+"/print", `{"template_id":"`+rollo2x1+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("print to a no longer allowed device: %d %s", rec.Code, rec.Body.String())
	}
}
