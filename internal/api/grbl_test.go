package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/grbl/grblsim"
)

// A GRBL diode laser, simulated over TCP, through the laser API: refused
// while its door is open, then started by Start, as GRBL has no button,
// and streamed to the end.
func TestAGRBLLaserThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	srv.lasers.poll = 5 * time.Millisecond
	sim := grblsim.New()
	t.Cleanup(func() {
		printers.CloseGRBL()
		sim.Close()
	})

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Diode","driver":"grbl","model":"diode-10w","connection":{"type":"tcp","address":"`+sim.Addr()+`"},"options":{"work_width_mm":300,"work_height_mm":200}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating the laser: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	laser := created.Printer.ID

	// The work area is the printer's own.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+laser+"/status", "")
	if !strings.Contains(rec.Body.String(), `"work_width_mm":300`) {
		t.Fatalf("status: %s", rec.Body)
	}

	tpl := createTemplate(t, srv, cookie, laserTag)
	job := prepareLaserJob(t, srv, cookie, laser, `{"template_id":"`+tpl+`","material":"cardstock","workpiece":{"width_mm":60,"height_mm":30,"x_mm":10,"y_mm":10}}`)
	start := "/api/v1/printers/" + laser + "/jobs/" + job.Job.ID + "/start"

	sim.SetState("Door:0")
	if rec := doJSON(t, srv, cookie, http.MethodPost, start, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "door is open") {
		t.Fatalf("with the door open: %d %s", rec.Code, rec.Body)
	}
	sim.SetState("Idle")
	if len(sim.Lines()) != 0 {
		t.Fatal("the controller was sent the job while refused")
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, start, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"running"`) {
		t.Fatalf("starting: %d %s", rec.Code, rec.Body)
	}
	waitForJob(t, srv, job.Job.ID, models.LaserJobDone)
	if !strings.Contains(strings.Join(sim.Lines(), "\n"), "S600") { // cardstock cuts at 60%
		t.Errorf("the job run:\n%s", strings.Join(sim.Lines(), "\n"))
	}
}

// A laser's serial port is checked against [printers] devices each time
// it is reached, not only when it is saved.
func TestALaserSerialPortMustStillBeAllowed(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	srv.transport = printers.NewDispatcher([]string{"/dev/ttyUSB*"})
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Diode","driver":"grbl","model":"diode-10w","connection":{"type":"device","address":"/dev/ttyUSB7"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating the laser: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	srv.transport = printers.NewDispatcher(nil)
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/printers/"+created.Printer.ID+"/status", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "devices") {
		t.Fatalf("status of a port no longer allowed: %d %s", rec.Code, rec.Body)
	}
}
