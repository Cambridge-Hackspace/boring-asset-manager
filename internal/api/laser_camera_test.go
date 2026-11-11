package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/simcamera"
)

type measured struct {
	Workpiece   MeasuredWorkpiece
	View        string
	Error       string
	Recalibrate bool
}

func measure(t *testing.T, srv *Server, cookie *http.Cookie, laser string, z float64) (int, measured) {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+laser+"/camera/measure", fmt.Sprintf(`{"z_mm":%g}`, z))
	var m measured
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

// near checks a measured workpiece against where it really lies: within a
// millimeter and a degree.
func nearTruth(t *testing.T, got MeasuredWorkpiece, want simcamera.Truth) {
	t.Helper()
	if d := math.Hypot(got.Center.X-want.Center.X, got.Center.Y-want.Center.Y); d > 1 {
		t.Errorf("center %+v is %g mm from %+v", got.Center, d, want.Center)
	}
	if math.Abs(got.WidthMM-want.Width) > 1 || math.Abs(got.HeightMM-want.Height) > 1 || math.Abs(got.AngleDeg-want.AngleDeg) > 1 {
		t.Errorf("measured %g × %g mm turned %g°, want %g × %g turned %g°", got.WidthMM, got.HeightMM, got.AngleDeg, want.Width, want.Height, want.AngleDeg)
	}
	if math.Abs(got.XMM-(got.Center.X-got.WidthMM/2)) > 0.01 || math.Abs(got.YMM-(got.Center.Y-got.HeightMM/2)) > 0.01 {
		t.Errorf("corner %g, %g does not match the center", got.XMM, got.YMM)
	}
}

// TestALasersCameraPlacesTheLabelOnTheWorkpiece runs the whole flow on the
// simulated laser: the marks engraved by a job a person starts, the camera
// calibrated from them at a height, the workpiece measured, seen from
// above, and the label placed on it, turned with it; a job at another
// height asks for calibrating again.
func TestALasersCameraPlacesTheLabelOnTheWorkpiece(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	laser, sim := newLaser(t, srv, cookie, "Camera laser", printers.TestLaserPanel, "camera-flow.test", 20*time.Millisecond)
	tpl := createTemplate(t, srv, cookie, laserTag)
	base := "/api/v1/printers/" + laser

	var status LaserCalibrationAnswer
	rec := doJSON(t, srv, cookie, http.MethodGet, base+"/calibration", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if rec.Code != http.StatusOK || !status.Camera || status.Calibration != nil || status.Layout == nil || len(status.Layout.Marks) != 9 || status.MaxErrorMM != 0.5 || status.HeightToleranceMM != 1 {
		t.Fatalf("before calibrating: %d %s", rec.Code, rec.Body)
	}
	// Measuring needs a calibration.
	if code, m := measure(t, srv, cookie, laser, 3); code != http.StatusConflict || !m.Recalibrate {
		t.Fatalf("measuring uncalibrated: %d %+v", code, m)
	}

	// The marks are a job like any other, started by a person at the
	// machine.
	rec = doJSON(t, srv, cookie, http.MethodPost, base+"/calibration/marks", `{"material":"birch-plywood-3mm"}`)
	var marks laserAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &marks)
	if rec.Code != http.StatusCreated || !marks.Job.Placement.Calibration || marks.Job.TemplateName != "Calibration marks" || !marks.Held {
		t.Fatalf("preparing the marks: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, base+"/jobs/"+marks.Job.ID+"/start", ""); rec.Code != http.StatusOK {
		t.Fatalf("starting the marks: %d %s", rec.Code, rec.Body)
	}
	waitForJob(t, srv, marks.Job.ID, models.LaserJobDone)
	if jobs := sim.Jobs(); len(jobs) != 1 || !jobs[0].Calibration || jobs[0].Engrave == nil {
		t.Fatalf("the machine was sent %+v", jobs)
	}

	// Calibrated at 3 mm, from the photo of the marks.
	rec = doJSON(t, srv, cookie, http.MethodPost, base+"/calibration", `{"z_mm":3}`)
	var cal struct{ Calibration models.LaserCalibration }
	_ = json.Unmarshal(rec.Body.Bytes(), &cal)
	if rec.Code != http.StatusOK || cal.Calibration.Marks != 9 || cal.Calibration.RMSMM > 0.3 || cal.Calibration.ZMM != 3 || cal.Calibration.CalibratedBy != admin.ID {
		t.Fatalf("calibrating: %d %s", rec.Code, rec.Body)
	}

	// The workpiece, measured within a millimeter and a degree; within the
	// tolerance of the calibration's height.
	code, m := measure(t, srv, cookie, laser, 3.5)
	if code != http.StatusOK {
		t.Fatalf("measuring: %d %+v", code, m)
	}
	nearTruth(t, m.Workpiece, simcamera.Pieces[simcamera.Workpiece])

	// Seen from above, the work area at 6 pixels a millimeter.
	rec = doJSON(t, srv, cookie, http.MethodGet, m.View, "")
	view, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("view: %d %v", rec.Code, err)
	}
	if b := view.Bounds(); b.Dx() != 1200 || b.Dy() != 900 {
		t.Errorf("view is %v", b)
	}

	// The label placed in the measured piece's top left corner, turned
	// with it.
	w := m.Workpiece
	body := fmt.Sprintf(`{"template_id":%q,"material":"birch-plywood-3mm","camera":true,"anchor":{"corner":"top_left","inset_mm":3},
		"workpiece":{"width_mm":%g,"height_mm":%g,"x_mm":%g,"y_mm":%g,"angle_deg":%g,"z_mm":3.5}}`, tpl, w.WidthMM, w.HeightMM, w.XMM, w.YMM, w.AngleDeg)
	job := prepareLaserJob(t, srv, cookie, laser, body)
	pl := job.Job.Placement
	if !pl.Camera || pl.ZMM != 3.5 || pl.AngleDeg != w.AngleDeg || pl.Placed.X != w.XMM+3 || pl.Placed.Y != w.YMM+3 || pl.Placed.Width != 50 {
		t.Errorf("placement %+v", pl)
	}
	// Turned, the engraving covers the square around the label.
	if len(job.Cuts) != 1 || pl.Label.Width <= 50 || pl.Label.Height <= 20 {
		t.Errorf("cuts %d, engraving over %+v", len(job.Cuts), pl.Label)
	}

	// At another height, beyond the tolerance, calibrating again is asked
	// for, both measuring and preparing.
	if code, m := measure(t, srv, cookie, laser, 8); code != http.StatusConflict || !m.Recalibrate || !strings.Contains(m.Error, "calibrate again") {
		t.Errorf("measuring at 8 mm: %d %+v", code, m)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, base+"/jobs", strings.Replace(body, `"z_mm":3.5`, `"z_mm":8`, 1))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"recalibrate":true`) {
		t.Errorf("preparing at 8 mm: %d %s", rec.Code, rec.Body)
	}
	// Placed by hand, the height does not matter.
	prepareLaserJob(t, srv, cookie, laser, strings.Replace(strings.Replace(body, `"z_mm":3.5`, `"z_mm":8`, 1), `"camera":true`, `"camera":false`, 1))

	// A photo of another size, of the same view, is measured too.
	sim.SetPhoto(simcamera.Photo(simcamera.WorkpieceLarge))
	if code, m = measure(t, srv, cookie, laser, 3); code != http.StatusOK {
		t.Fatalf("measuring the larger photo: %d %+v", code, m)
	}
	nearTruth(t, m.Workpiece, simcamera.Pieces[simcamera.WorkpieceLarge])

	// A photo without the marks, or a fit worse than allowed, is refused,
	// and the calibration kept is the one before.
	if rec := doJSON(t, srv, cookie, http.MethodPost, base+"/calibration", `{"z_mm":3}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "marks were not found") {
		t.Errorf("calibrating without marks: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/lasers", `{"max_error_mm":0.01,"height_tolerance_mm":1}`); rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	sim.SetPhoto(simcamera.Photo(simcamera.Calibration))
	if rec := doJSON(t, srv, cookie, http.MethodPost, base+"/calibration", `{"z_mm":6}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"rms_mm"`) {
		t.Errorf("calibrating beyond the largest error: %d %s", rec.Code, rec.Body)
	}
	if c, err := srv.store.LaserCalibration(laser); err != nil || c.ZMM != 3 {
		t.Errorf("kept %+v %v", c, err)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/lasers", `{"max_error_mm":0,"height_tolerance_mm":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a largest error of 0: %d", rec.Code)
	}

	// A laser without a camera has none to calibrate.
	button, _ := newLaser(t, srv, cookie, "Buttoned laser", printers.TestLaserButton, "camera-none.test", time.Second)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+button+"/camera/measure", `{"z_mm":3}`); rec.Code != http.StatusConflict {
		t.Errorf("measuring without a camera: %d %s", rec.Code, rec.Body)
	}
}
