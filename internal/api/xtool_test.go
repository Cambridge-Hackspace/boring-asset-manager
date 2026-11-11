package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool/xtoolsim"
)

// An xTool F1, simulated over WS-V2, through the laser API: refused while
// its lid is open and while its firmware is unknown, then sent ready for
// its button, started there, and done.
func TestAnXToolLaserThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	srv.lasers.poll = 5 * time.Millisecond
	sim := xtoolsim.NewV2("40.51.020.04")
	t.Cleanup(func() {
		xtool.CloseV2()
		sim.Close()
	})

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"F1","driver":"xtool","model":"F1","connection":{"type":"tcp","address":"`+sim.Addr()+`"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating the laser: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	laser := created.Printer.ID

	tpl := createTemplate(t, srv, cookie, laserTag)
	job := prepareLaserJob(t, srv, cookie, laser, `{"template_id":"`+tpl+`","material":"cardstock","workpiece":{"width_mm":60,"height_mm":30,"x_mm":10,"y_mm":10}}`)
	start := "/api/v1/printers/" + laser + "/jobs/" + job.Job.ID + "/start"

	sim.Set(func(s *xtoolsim.V2) { s.Peripherals["gap"] = "off" })
	if rec := doJSON(t, srv, cookie, http.MethodPost, start, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "The lid is open") {
		t.Fatalf("with the lid open: %d %s", rec.Code, rec.Body)
	}
	sim.Set(func(s *xtoolsim.V2) {
		s.Peripherals["gap"] = "on"
		s.Firmware = "40.51.020.03" // V1 firmware, which WS-V2 does not know
	})
	xtool.CloseV2() // the firmware is read again on a new connection
	if rec := doJSON(t, srv, cookie, http.MethodPost, start, ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "40.51.020.03") {
		t.Fatalf("with unknown firmware: %d %s", rec.Code, rec.Body)
	}
	if sim.File("tmp.gcode") != nil {
		t.Fatal("the machine was sent the job while refused")
	}

	sim.Set(func(s *xtoolsim.V2) { s.Firmware = "40.51.020.04" })
	xtool.CloseV2()
	if rec := doJSON(t, srv, cookie, http.MethodPost, start, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"ready"`) {
		t.Fatalf("sending it ready: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(string(sim.File("tmp.gcode")), "S600") { // cardstock cuts at 60%
		t.Errorf("the job sent:\n%s", sim.File("tmp.gcode"))
	}
	sim.PressButton()
	waitForJob(t, srv, job.Job.ID, models.LaserJobRunning)
	sim.Finish()
	waitForJob(t, srv, job.Job.ID, models.LaserJobDone)
}
