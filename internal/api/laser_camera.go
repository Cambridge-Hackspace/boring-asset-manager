package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// Lasers' cameras (docs/printing.md#camera-alignment): calibration marks
// engraved through a job like any other, found in a photo and fitted to
// the bed at the height they were engraved at; then a workpiece measured
// wherever it lies, and the photo seen from above, for the person to check
// the plan over before framing. Nothing here moves a laser: taking a photo
// is all that reaches the machine.

// laserPhoto is a laser's latest photo, and the fit of its pixels to the
// bed, when its camera is calibrated.
type laserPhoto struct {
	img image.Image
	h   *vision.Homography
}

// laserPhotos holds each laser's latest photo, on the server that took it.
type laserPhotos struct {
	mu  sync.Mutex
	all map[string]*laserPhoto
}

func (l *laserPhotos) keep(printerID string, p *laserPhoto) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.all == nil {
		l.all = map[string]*laserPhoto{}
	}
	l.all[printerID] = p
}

func (l *laserPhotos) get(printerID string) *laserPhoto {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.all[printerID]
}

// laserCameraTimeout bounds taking a photo, black frames and all.
const laserCameraTimeout = 30 * time.Second

// viewSide is the longer side, in pixels, of the photo seen from above.
const viewSide = 1200

// handleLaserCameraSettings: GET /api/v1/settings/lasers (settings.manage).
func (s *Server) handleLaserCameraSettings(w http.ResponseWriter, _ *http.Request) {
	set, err := s.store.LaserCameraSettings()
	if err != nil {
		writeServerError(w, err, "read the lasers' settings")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// handleSetLaserCameraSettings: PUT /api/v1/settings/lasers (settings.manage).
func (s *Server) handleSetLaserCameraSettings(w http.ResponseWriter, r *http.Request) {
	var req store.LaserCameraSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.store.SetLaserCameraSettings(req); errors.Is(err, store.ErrInvalidSetting) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeServerError(w, err, "save the lasers' settings")
		return
	}
	s.handleLaserCameraSettings(w, r)
}

// loadCameraLaser reads a laser that must have a camera.
func (s *Server) loadCameraLaser(w http.ResponseWriter, r *http.Request) (*models.Printer, printers.OneShotModel, bool) {
	p, m, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return nil, m, false
	}
	if !m.Camera {
		writeJSONError(w, http.StatusConflict, p.Name+" has no camera BAM! can use")
		return nil, m, false
	}
	return p, m, true
}

// LaserCalibrationAnswer is a laser's camera as the API gives it.
type LaserCalibrationAnswer struct {
	Camera bool `json:"camera"`
	// Calibration is the latest, or null before there is one.
	Calibration *models.LaserCalibration `json:"calibration"`
	// Layout is where the calibration marks are engraved, and the scrap
	// they need.
	Layout *vision.MarkLayout `json:"layout,omitempty"`
	store.LaserCameraSettings
}

// handleGetLaserCalibration: GET /api/v1/printers/{id}/calibration
func (s *Server) handleGetLaserCalibration(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return
	}
	set, err := s.store.LaserCameraSettings()
	if err != nil {
		writeServerError(w, err, "read the lasers' settings")
		return
	}
	out := LaserCalibrationAnswer{Camera: m.Camera, LaserCameraSettings: set}
	if m.Camera {
		l := printers.MarkLayout(m)
		out.Layout = &l
		c, err := s.store.LaserCalibration(p.ID)
		if err != nil && !errors.Is(err, store.ErrNoCalibration) {
			writeServerError(w, err, "read the calibration")
			return
		}
		out.Calibration = c
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteLaserCalibration forgets a laser's calibration: DELETE
// /api/v1/printers/{id}/calibration
func (s *Server) handleDeleteLaserCalibration(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.store.DeleteLaserCalibration(p.ID); err != nil {
		writeServerError(w, err, "delete the calibration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Calibration deleted: calibrate the camera again before placing a label by it"})
}

// handlePrepareCalibrationMarks prepares the job that engraves a laser's
// calibration marks on scrap: POST /api/v1/printers/{id}/calibration/marks
// (labels.print). It is framed and started as any job is, by a person at
// the machine.
func (s *Server) handlePrepareCalibrationMarks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Material string `json:"material"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	p, model, ok := s.loadCameraLaser(w, r)
	if !ok {
		return
	}
	material, err := s.store.Material(p.ID, req.Material)
	if writeLaserError(w, err, "read the material") {
		return
	}
	job, layout, err := printers.CalibrationJob(model, material.Material)
	if err != nil {
		writeServerError(w, err, "lay out the calibration marks")
		return
	}
	prepared := &laserPrepared{printerID: p.ID, job: job,
		warnings: []string{"Lay light scrap, such as plywood or card, over the area marked, and engrave the marks on it. Leave it where it is to calibrate."}}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, job.Engrave); err != nil {
		writeServerError(w, err, "draw the preview")
		return
	}
	prepared.preview = buf.Bytes()
	rec := models.LaserJob{PrinterID: &p.ID, PrinterName: p.Name, TemplateName: "Calibration marks", Material: job.Material,
		Placement: models.LaserPlacement{Workpiece: job.Workpiece, Placed: job.Placed, Label: job.Label, Calibration: true}}
	saved, err := s.actingStore(r).CreateLaserJob(rec)
	if err != nil {
		writeServerError(w, err, "record the laser job")
		return
	}
	s.lasers.keep(saved.ID, prepared)
	out := s.laserJobAnswer(p, saved)
	out["layout"] = layout
	writeJSON(w, http.StatusCreated, out)
}

// photograph takes a photo with a laser's camera.
func (s *Server) photograph(ctx context.Context, p *models.Printer) (image.Image, error) {
	m, err := s.machine(p)
	if err != nil {
		return nil, err
	}
	cam, ok := m.(printers.Camera)
	if !ok {
		return nil, fmt.Errorf("%s's driver cannot take a photo", p.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, laserCameraTimeout)
	defer cancel()
	data, err := cam.Photo(ctx)
	if err != nil {
		return nil, err
	}
	return vision.Decode(data)
}

// viewPath is where a laser's latest photo is seen from above.
func viewPath(p *models.Printer) string {
	return fmt.Sprintf("/api/v1/printers/%s/camera/view", p.ID)
}

// zOf checks a height above the bed, in mm, writing the error when it is
// not one.
func zOf(w http.ResponseWriter, z *float64) (float64, bool) {
	if z == nil || math.IsNaN(*z) || *z < 0 || *z > 500 {
		writeJSONError(w, http.StatusBadRequest, "z_mm, the height of the surface above the bed, must be from 0 to 500 mm")
		return 0, false
	}
	return *z, true
}

// handleCalibrateLaserCamera photographs the calibration marks a laser
// engraved, finds them, and fits its camera to the bed at the height the
// scrap stands: POST /api/v1/printers/{id}/calibration (labels.print). A
// fit worse than the instance allows is refused.
func (s *Server) handleCalibrateLaserCamera(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ZMM *float64 `json:"z_mm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	z, ok := zOf(w, req.ZMM)
	if !ok {
		return
	}
	p, model, ok := s.loadCameraLaser(w, r)
	if !ok {
		return
	}
	set, err := s.store.LaserCameraSettings()
	if err != nil {
		writeServerError(w, err, "read the lasers' settings")
		return
	}
	takeLonger(w)
	img, err := s.photograph(r.Context(), p)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s's camera did not take a photo: %v", p.Name, err))
		return
	}
	s.bedPhotos.keep(p.ID, &laserPhoto{img: img})
	cal, err := vision.Calibrate(img, printers.MarkLayout(model))
	if errors.Is(err, vision.ErrMarksNotFound) || errors.Is(err, vision.ErrDegenerate) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": upperFirst(err.Error()) + ". Check the scrap is light, the marks dark and all in view, and the bed darker than the scrap."})
		return
	}
	if err != nil {
		writeServerError(w, err, "find the calibration marks")
		return
	}
	if cal.RMS > set.MaxErrorMM {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  fmt.Sprintf("The marks fit only within %g mm (RMS; %g mm at worst), more than the %g mm allowed: the camera's lens may bend the picture too much, or a mark was mistaken. Engrave fresh marks and try again.", cal.RMS, cal.Max, set.MaxErrorMM),
			"rms_mm": cal.RMS, "max_mm": cal.Max, "marks": cal.Marks,
		})
		return
	}
	saved, err := s.actingStore(r).SaveLaserCalibration(models.LaserCalibration{PrinterID: p.ID, Homography: cal.H, ImageWidth: cal.Width, ImageHeight: cal.Height,
		ZMM: z, Marks: cal.Marks, RMSMM: cal.RMS, MaxMM: cal.Max})
	if err != nil {
		writeServerError(w, err, "save the calibration")
		return
	}
	h := cal.H
	s.bedPhotos.keep(p.ID, &laserPhoto{img: img, h: &h})
	writeJSON(w, http.StatusOK, map[string]any{"calibration": saved, "view": viewPath(p)})
}

// calibrationAt is a laser's calibration, when it is good for a surface z
// mm above the bed, writing the error when it is not: 409, with
// recalibrate, when there is none or it was made at another height.
func (s *Server) calibrationAt(w http.ResponseWriter, p *models.Printer, z float64) (*models.LaserCalibration, bool) {
	c, err := s.store.LaserCalibration(p.ID)
	if errors.Is(err, store.ErrNoCalibration) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": p.Name + "'s camera has not been calibrated: engrave the calibration marks, then calibrate.", "recalibrate": true})
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the calibration")
		return nil, false
	}
	set, err := s.store.LaserCameraSettings()
	if err != nil {
		writeServerError(w, err, "read the lasers' settings")
		return nil, false
	}
	if math.Abs(z-c.ZMM) > set.HeightToleranceMM+1e-9 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       fmt.Sprintf("%s's camera was calibrated for a surface %g mm above the bed, and this one is %g mm: calibrate again at this height, since the camera sees the bed differently at each.", p.Name, c.ZMM, z),
			"recalibrate": true, "calibrated_z_mm": c.ZMM,
		})
		return nil, false
	}
	return c, true
}

// fitFor is a calibration's fit for a photo of a size: scaled, for a
// camera that sends photos of different sizes of the same view, and
// refused for one of another shape.
func fitFor(c *models.LaserCalibration, b image.Rectangle) (vision.Homography, error) {
	h := vision.Homography(c.Homography)
	if b.Dx() == c.ImageWidth && b.Dy() == c.ImageHeight {
		return h, nil
	}
	if math.Abs(float64(b.Dx())*float64(c.ImageHeight)-float64(b.Dy())*float64(c.ImageWidth)) > 0.01*float64(b.Dx()*c.ImageHeight) {
		return h, fmt.Errorf("the camera's photo is %d × %d, a different shape from the %d × %d it was calibrated with: calibrate again", b.Dx(), b.Dy(), c.ImageWidth, c.ImageHeight)
	}
	return h.Scaled(c.ImageWidth, c.ImageHeight, b.Dx(), b.Dy()), nil
}

// MeasuredWorkpiece is a workpiece as the camera measured it: its size,
// where its top left corner lies before it is turned, as a job takes it,
// and how far it is turned, with its center.
type MeasuredWorkpiece struct {
	WidthMM  float64      `json:"width_mm"`
	HeightMM float64      `json:"height_mm"`
	XMM      float64      `json:"x_mm"`
	YMM      float64      `json:"y_mm"`
	AngleDeg float64      `json:"angle_deg"`
	Center   vision.Point `json:"center"`
}

// handleMeasureWorkpiece photographs a laser's bed and measures the
// workpiece on it: POST /api/v1/printers/{id}/camera/measure
// (labels.print), with z_mm, its surface's height above the bed.
func (s *Server) handleMeasureWorkpiece(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ZMM *float64 `json:"z_mm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	z, ok := zOf(w, req.ZMM)
	if !ok {
		return
	}
	p, model, ok := s.loadCameraLaser(w, r)
	if !ok {
		return
	}
	c, ok := s.calibrationAt(w, p, z)
	if !ok {
		return
	}
	takeLonger(w)
	img, err := s.photograph(r.Context(), p)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s's camera did not take a photo: %v", p.Name, err))
		return
	}
	h, err := fitFor(c, img.Bounds())
	if err != nil {
		s.bedPhotos.keep(p.ID, &laserPhoto{img: img})
		writeJSON(w, http.StatusConflict, map[string]any{"error": upperFirst(err.Error()) + ".", "recalibrate": true})
		return
	}
	s.bedPhotos.keep(p.ID, &laserPhoto{img: img, h: &h})
	found, err := vision.FindWorkpiece(img, h, model.WorkWidthMM, model.WorkHeightMM)
	if errors.Is(err, vision.ErrNoWorkpiece) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": upperFirst(err.Error()) + ".", "view": viewPath(p)})
		return
	}
	if err != nil {
		writeServerError(w, err, "measure the workpiece")
		return
	}
	piece := MeasuredWorkpiece{WidthMM: found.Width, HeightMM: found.Height, AngleDeg: found.AngleDeg, Center: found.Center,
		XMM: math.Round((found.Center.X-found.Width/2)*100) / 100, YMM: math.Round((found.Center.Y-found.Height/2)*100) / 100}
	writeJSON(w, http.StatusOK, map[string]any{"workpiece": piece, "z_mm": z, "view": viewPath(p)})
}

// handleLaserCameraView is a laser's latest photo as seen from straight
// above its work area, a pixel for each RectifyScale of a millimeter, for
// the plan to be drawn over: GET /api/v1/printers/{id}/camera/view
// (labels.print).
func (s *Server) handleLaserCameraView(w http.ResponseWriter, r *http.Request) {
	p, model, ok := s.loadCameraLaser(w, r)
	if !ok {
		return
	}
	photo := s.bedPhotos.get(p.ID)
	if photo == nil || photo.h == nil {
		writeJSONError(w, http.StatusNotFound, "This server holds no photo of the bed fitted to it: measure the workpiece, or calibrate")
		return
	}
	out, err := vision.Rectify(photo.img, *photo.h, model.WorkWidthMM, model.WorkHeightMM, vision.RectifyScale(model.WorkWidthMM, model.WorkHeightMM, viewSide))
	if err != nil {
		writeServerError(w, err, "draw the photo from above")
		return
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); err != nil {
		writeServerError(w, err, "draw the photo from above")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// upperFirst is a message with its first letter capitalized.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
