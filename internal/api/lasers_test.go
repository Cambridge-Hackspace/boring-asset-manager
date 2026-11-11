package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// laserTag is a 50 × 20 mm tag naming its asset, its outline cut.
const laserTag = `{"name":"Tool tag","units":"mm","width":50,"height":20,"dpi":300,"elements":[
  {"type":"text","x":4,"y":4,"width":42,"height":12,"font_size":14,"content_binding":"{{asset.name}}"},
  {"type":"shape","shape":"rectangle","x":0,"y":0,"width":50,"height":20,"corner_radius":3,"stroke":"black","stroke_width":0.2,"fill":"none","cut":true}]}`

// newLaser makes a test laser of a model at an address of its own, which
// finishes a job in d, and has the server look at it often.
func newLaser(t *testing.T, srv *Server, cookie *http.Cookie, name, model, host string, d time.Duration) (string, *printers.SimulatedLaser) {
	t.Helper()
	printers.RegisterTestLaser()
	srv.lasers.poll = 5 * time.Millisecond
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"`+name+`","driver":"test-laser","model":"`+model+`","connection":{"type":"tcp","address":"`+host+`"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating the laser: %d %s", rec.Code, rec.Body)
	}
	var resp struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	sim := printers.TestLaserAt(resp.Printer.Connection.Address)
	sim.SetDuration(d)
	return resp.Printer.ID, sim
}

type laserAnswer struct {
	Job      models.LaserJob
	Held     bool
	Cuts     []printers.Polyline
	Warnings []string
	Preview  string
	Error    string
}

func prepareLaserJob(t *testing.T, srv *Server, cookie *http.Cookie, laser, body string) laserAnswer {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+laser+"/jobs", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("preparing: %d %s", rec.Code, rec.Body)
	}
	var a laserAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	return a
}

// waitForJob waits for a laser job to reach a state.
func waitForJob(t *testing.T, srv *Server, id, state string) *models.LaserJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, err := srv.store.GetLaserJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == state {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job is %s, not %s", j.State, state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestALaserJobIsPreparedFramedStartedAndRecorded(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, sim := newLaser(t, srv, cookie, "Workshop laser", printers.TestLaserPanel, "laser-flow.test", 50*time.Millisecond)
	tpl := createTemplate(t, srv, cookie, laserTag)
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)

	job := prepareLaserJob(t, srv, cookie, laser, `{"template_id":"`+tpl+`","asset_id":"`+drill.Asset.ID+`","material":"birch-plywood-3mm","workpiece":{"width_mm":60,"height_mm":30,"x_mm":10,"y_mm":10}}`)
	if job.Job.State != models.LaserJobPrepared || !job.Held || len(job.Cuts) != 1 || job.Job.Placement.Label != (printers.RectMM{X: 15, Y: 15, Width: 50, Height: 20}) {
		t.Fatalf("prepared: %+v", job)
	}
	if job.Job.Material.Engrave.Power != 30 || job.Job.AssetName != "Drill" || job.Job.PreparedBy != admin.ID {
		t.Errorf("recorded: %+v", job.Job)
	}
	// The engraving is the label at the material's 254 lines an inch.
	rec := doJSON(t, srv, cookie, http.MethodGet, job.Preview, "")
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("preview: %d %v", rec.Code, err)
	}
	if b := img.Bounds(); b.Dx() != 500 || b.Dy() != 200 {
		t.Errorf("engraving is %v", b)
	}

	path := "/api/v1/printers/" + laser + "/jobs/" + job.Job.ID
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/frame", ""); rec.Code != http.StatusOK || len(sim.Framed()) == 0 {
		t.Fatalf("framing: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/start", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"running"`) {
		t.Fatalf("starting: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/start", ""); rec.Code != http.StatusConflict {
		t.Errorf("starting twice: %d", rec.Code)
	}
	sent := sim.Jobs()[len(sim.Jobs())-1]
	if sent.Engrave.Bounds().Dx() != 500 || len(sent.Cuts) != 1 || sent.Cuts[0].Points[0].X < 15 {
		t.Errorf("the machine was sent %+v", sent)
	}

	done := waitForJob(t, srv, job.Job.ID, models.LaserJobDone)
	if done.StartedBy != admin.ID || done.StartedAt == nil || done.EndedAt == nil {
		t.Errorf("done: %+v", done)
	}
	history := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+drill.Asset.ID+"/history", "").Body.String()
	if !strings.Contains(history, `"material":"Birch plywood, 3 mm"`) || !strings.Contains(history, `"printer":"Workshop laser"`) {
		t.Errorf("history: %s", history)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+laser+"/jobs", ""); !strings.Contains(rec.Body.String(), `"state":"done"`) {
		t.Errorf("jobs: %s", rec.Body)
	}
}

func TestAButtonLaserIsSentReadyAndStartedAtTheMachine(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, sim := newLaser(t, srv, cookie, "F1", printers.TestLaserButton, "laser-button.test", time.Hour)
	tpl := createTemplate(t, srv, cookie, laserTag)
	job := prepareLaserJob(t, srv, cookie, laser, `{"template_id":"`+tpl+`","material":"clear-acrylic-3mm","workpiece":{"width_mm":50,"height_mm":20}}`)
	if !job.Job.Placement.Mirror || len(job.Warnings) == 0 {
		t.Errorf("acrylic is not mirrored: %+v", job)
	}
	path := "/api/v1/printers/" + laser + "/jobs/" + job.Job.ID
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/start", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"ready"`) {
		t.Fatalf("sending it ready: %d %s", rec.Code, rec.Body)
	}
	sim.PressButton()
	waitForJob(t, srv, job.Job.ID, models.LaserJobRunning)
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/pause", ""); rec.Code != http.StatusOK {
		t.Fatalf("pausing: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+laser+"/status", ""); !strings.Contains(rec.Body.String(), `"state":"paused"`) || !strings.Contains(rec.Body.String(), `"name":"lid","ok":true`) {
		t.Errorf("status: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/cancel", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"cancelled"`) {
		t.Fatalf("cancelling: %d %s", rec.Code, rec.Body)
	}
}

func TestStartingALaserIsRefused(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, sim := newLaser(t, srv, cookie, "Workshop laser", printers.TestLaserPanel, "laser-refused.test", time.Hour)
	tpl := createTemplate(t, srv, cookie, laserTag)
	prepare := func() string {
		return prepareLaserJob(t, srv, cookie, laser, `{"template_id":"`+tpl+`","material":"birch-plywood-3mm","workpiece":{"width_mm":50,"height_mm":20}}`).Job.ID
	}
	start := func(id string) string { return "/api/v1/printers/" + laser + "/jobs/" + id + "/start" }
	asToken := func(secret, path string, ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+secret)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	id := prepare()

	// Without lasers.run: someone who may print prepares a job, and may
	// not start it, nor frame it.
	printer := userWithPermissions(t, srv, "printer@example.com", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, printer, http.MethodPost, "/api/v1/printers/"+laser+"/jobs", `{"template_id":"`+tpl+`","material":"birch-plywood-3mm","workpiece":{"width_mm":50,"height_mm":20}}`); rec.Code != http.StatusCreated {
		t.Fatalf("preparing with labels.print: %d %s", rec.Code, rec.Body)
	} else {
		id = between(rec.Body.String(), `"id":"`, `"`)
	}
	for _, action := range []string{"start", "frame"} {
		if rec := doJSON(t, srv, printer, http.MethodPost, "/api/v1/printers/"+laser+"/jobs/"+id+"/"+action, ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "lasers.run") {
			t.Errorf("%s without lasers.run: %d %s", action, rec.Code, rec.Body)
		}
	}

	// A token without lasers.run is refused; one with it may start.
	plain := tokenWith(t, srv, admin.ID, "Printing script", rbac.PermLabelsPrint)
	if rec := asToken(plain, start(id), context.Background()); rec.Code != http.StatusForbidden {
		t.Errorf("a token without lasers.run: %d %s", rec.Code, rec.Body)
	}
	runner := tokenWith(t, srv, admin.ID, "Laser panel", rbac.PermLabelsPrint, rbac.PermLasersRun)

	// From an assistant over MCP, or a flow, which call the API in process
	// with their name on the request, whatever they hold.
	for _, via := range []string{"Claude Desktop", "flow “Engrave”"} {
		if rec := asToken(runner, start(id), mcpserver.WithVia(context.Background(), via)); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), via) {
			t.Errorf("through %s: %d %s", via, rec.Code, rec.Body)
		}
	}
	// From an incoming hook.
	hook := context.WithValue(context.Background(), incomingKey{}, &incomingCaller{hook: &models.IncomingHook{ID: "h"}})
	if rec := asToken(runner, start(id), hook); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "incoming hook") {
		t.Errorf("from an incoming hook: %d %s", rec.Code, rec.Body)
	}

	// While an interlock reports a problem.
	sim.SetInterlock(printers.InterlockLid, false, "The lid is open")
	if rec := asToken(runner, start(id), context.Background()); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "The lid is open") {
		t.Errorf("with the lid open: %d %s", rec.Code, rec.Body)
	}
	if j, _ := srv.store.GetLaserJob(id); j.State != models.LaserJobPrepared || len(sim.Jobs()) != 0 {
		t.Errorf("refused, the job is %s and the machine was sent %d", j.State, len(sim.Jobs()))
	}
	sim.SetInterlock(printers.InterlockLid, true, "")
	if rec := asToken(runner, start(id), context.Background()); rec.Code != http.StatusOK {
		t.Errorf("a token with lasers.run, interlocks fine: %d %s", rec.Code, rec.Body)
	}
	if j, _ := srv.store.GetLaserJob(id); !j.StartedByToken {
		t.Errorf("not recorded as the token's: %+v", j)
	}
	// Anyone who may print stops it.
	if rec := doJSON(t, srv, printer, http.MethodPost, "/api/v1/printers/"+laser+"/jobs/"+id+"/cancel", ""); rec.Code != http.StatusOK {
		t.Errorf("cancelling with labels.print: %d %s", rec.Code, rec.Body)
	}
}

func TestTheSpoolNeverOffersALaser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, _ := newLaser(t, srv, cookie, "Workshop laser", printers.TestLaserPanel, "laser-spool.test", time.Hour)
	tpl := createTemplate(t, srv, cookie, laserTag)
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)

	var spool SpoolResponse
	_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/spool", "").Body.Bytes(), &spool)
	for _, p := range spool.Printers {
		if p.ID == laser {
			t.Error("the spool offers the laser")
		}
	}
	for _, id := range spool.Fits[tpl] {
		if id == laser {
			t.Error("a template suits the laser in the spool")
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+laser+"/print", `{"template_id":"`+tpl+`","asset_ids":["`+drill.Asset.ID+`"]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("printing to the laser: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printer-pools", `{"name":"Lasers","batch_size":1,"members":[{"printer_id":"`+laser+`","share":1}]}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "never in a pool") {
		t.Errorf("a pool with the laser: %d %s", rec.Code, rec.Body)
	}
	// The template editor's fit check measures it against the work area.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+laser+"/fit", `{"width_mm":50,"height_mm":20,"dpi":300}`); !strings.Contains(rec.Body.String(), `"fits":true`) {
		t.Errorf("fit: %s", rec.Body)
	}
}

func TestALasersMaterialsAreTunedPerPrinter(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, _ := newLaser(t, srv, cookie, "Workshop laser", printers.TestLaserPanel, "laser-materials.test", time.Hour)
	other, _ := newLaser(t, srv, cookie, "Garage laser", printers.TestLaserPanel, "laser-materials-2.test", time.Hour)
	materials := func(id string) []models.LaserMaterial {
		var out struct{ Materials []models.LaserMaterial }
		_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+id+"/materials", "").Body.Bytes(), &out)
		return out.Materials
	}
	if list := materials(laser); len(list) != 3 || !list[0].Preset || list[0].Tuned {
		t.Fatalf("presets: %+v", list)
	}
	path := "/api/v1/printers/" + laser + "/materials/"
	if rec := doJSON(t, srv, cookie, http.MethodPut, path+"birch-plywood-3mm", `{"name":"Birch plywood, 3 mm","engrave":{"power":35,"speed":150,"passes":1,"lpi":254},"cut":{"power":100,"speed":7,"passes":2}}`); rec.Code != http.StatusOK {
		t.Fatalf("tuning: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, path+"oak-6mm", `{"name":"Oak, 6 mm","engrave":{"power":40,"speed":120,"passes":1,"lpi":300}}`); rec.Code != http.StatusOK {
		t.Fatalf("adding: %d %s", rec.Code, rec.Body)
	}
	list := materials(laser)
	if len(list) != 4 || !list[0].Tuned || list[0].Engrave.Power != 35 || list[3].ID != "oak-6mm" || list[3].Preset || list[3].Cut != nil {
		t.Errorf("after saving: %+v", list)
	}
	if theirs := materials(other); theirs[0].Tuned || len(theirs) != 3 {
		t.Errorf("another laser's changed: %+v", theirs)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, path+"bad", `{"name":"Bad","engrave":{"power":150,"speed":1,"passes":1,"lpi":254}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an invalid material: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, path+"birch-plywood-3mm", ""); rec.Code != http.StatusOK {
		t.Errorf("resetting: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, path+"oak-6mm", ""); rec.Code != http.StatusOK {
		t.Errorf("deleting: %d", rec.Code)
	}
	if list := materials(laser); len(list) != 3 || list[0].Tuned || list[0].Engrave.Power != 30 {
		t.Errorf("after reset: %+v", list)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, path+"oak-6mm", ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting again: %d", rec.Code)
	}
	// Only an admin changes them; anyone who may print reads them.
	printer := userWithPermissions(t, srv, "printer@example.com", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, printer, http.MethodPut, path+"oak-6mm", `{"name":"Oak","engrave":{"power":40,"speed":120,"passes":1,"lpi":300}}`); rec.Code != http.StatusForbidden {
		t.Errorf("tuning with labels.print: %d", rec.Code)
	}
	if _, err := srv.store.ListMaterials("nope"); err != store.ErrPrinterNotFound {
		t.Errorf("an unknown printer: %v", err)
	}
}
