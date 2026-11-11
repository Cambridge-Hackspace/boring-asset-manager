package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Lasers (docs/printing.md#lasers): one-shot printers' materials, and their
// jobs, each prepared, framed, started, watched, paused and cancelled one
// at a time. Starting is for a person at the machine: its own button where
// it has one, or else Start from someone holding lasers.run, with the
// machine's interlocks checked first, and never from an assistant, a flow
// or an incoming hook.

// laserState is what the server holds of lasers' jobs: each prepared job's
// engraving and outlines, until it ends or another takes its place on its
// printer, which only the server that prepared it holds.
type laserState struct {
	mu   sync.Mutex
	jobs map[string]*laserPrepared
	// poll is how often a started job's machine is asked how it is going.
	poll time.Duration
	// lost is how long a machine may not answer before its job is taken
	// as failed.
	lost time.Duration
}

// laserPrepared is a prepared job as made for its machine.
type laserPrepared struct {
	printerID string
	job       *printers.Job
	warnings  []string
	// preview is the engraving as a PNG; nil when nothing is engraved.
	preview []byte
}

func newLaserState() *laserState {
	return &laserState{jobs: map[string]*laserPrepared{}, poll: time.Second, lost: 10 * time.Minute}
}

// keep holds a prepared job, in place of any other its printer had.
func (l *laserState) keep(id string, p *laserPrepared) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for other, q := range l.jobs {
		if q.printerID == p.printerID {
			delete(l.jobs, other)
		}
	}
	l.jobs[id] = p
}

func (l *laserState) get(id string) *laserPrepared {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.jobs[id]
}

func (l *laserState) drop(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.jobs, id)
}

// LaserJobRequest prepares a job on a one-shot printer.
type LaserJobRequest struct {
	TemplateID string `json:"template_id"`
	// AssetID is whose label it engraves; without one, the template's
	// placeholders, as a test print has.
	AssetID  string `json:"asset_id,omitempty"`
	Material string `json:"material"`
	// Workpiece is its size, and its top left corner's place on the bed,
	// in millimeters from the work area's top left corner.
	Workpiece struct {
		WidthMM  float64 `json:"width_mm"`
		HeightMM float64 `json:"height_mm"`
		XMM      float64 `json:"x_mm"`
		YMM      float64 `json:"y_mm"`
		// AngleDeg is how far it is turned clockwise about its center.
		AngleDeg float64 `json:"angle_deg,omitempty"`
		// ZMM is its surface's height above the bed.
		ZMM *float64 `json:"z_mm,omitempty"`
	} `json:"workpiece"`
	// Camera says the camera measured the workpiece: its calibration must
	// be good at the workpiece's z_mm, or calibrating again is asked for.
	Camera bool `json:"camera,omitempty"`
	// LabelAt is where the label's top left corner goes on the workpiece,
	// from its top left corner; left out, the label goes by Anchor.
	LabelAt *printers.PointMM `json:"label_at,omitempty"`
	// Anchor puts the label in a corner of the workpiece or centers it;
	// left out, as the template's workpiece_anchor says, or centered.
	Anchor *labels.WorkpieceAnchor `json:"anchor,omitempty"`
}

// laserJobAnswer is a laser job as the API gives it: its history, and while
// the server holds it prepared, what it engraves and cuts, and where.
func (s *Server) laserJobAnswer(p *models.Printer, j *models.LaserJob) map[string]any {
	out := map[string]any{"job": j}
	prepared := s.lasers.get(j.ID)
	if prepared == nil || models.LaserJobEnded(j.State) {
		out["held"] = false
		return out
	}
	cuts := prepared.job.Cuts
	if cuts == nil {
		cuts = []printers.Polyline{}
	}
	out["held"] = true
	out["cuts"] = cuts
	out["outline"] = prepared.job.Outline()
	out["warnings"] = nonNil(prepared.warnings)
	if prepared.preview != nil {
		out["preview"] = fmt.Sprintf("/api/v1/printers/%s/jobs/%s/preview", p.ID, j.ID)
	}
	return out
}

// loadLaser reads a printer that must be a laser, and its model, writing
// the error when it cannot.
func (s *Server) loadLaser(w http.ResponseWriter, id string) (*models.Printer, printers.OneShotModel, bool) {
	p, ok := s.loadPrinter(w, id)
	if !ok {
		return nil, printers.OneShotModel{}, false
	}
	if !printers.IsOneShot(p.Driver) {
		writeJSONError(w, http.StatusBadRequest, p.Name+" is not a laser: its labels are printed, not engraved")
		return nil, printers.OneShotModel{}, false
	}
	m, err := printers.OneShotModelOf(p.Driver, store.PrinterSettings(p))
	if err != nil {
		writeServerError(w, err, "read the laser's model")
		return nil, printers.OneShotModel{}, false
	}
	return p, m, true
}

// loadLaserJob reads a laser and one of its jobs, writing the error when it
// cannot.
func (s *Server) loadLaserJob(w http.ResponseWriter, r *http.Request) (*models.Printer, printers.OneShotModel, *models.LaserJob, bool) {
	p, m, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return nil, m, nil, false
	}
	j, err := s.store.GetLaserJob(r.PathValue("job"))
	if err == nil && (j.PrinterID == nil || *j.PrinterID != p.ID) {
		err = store.ErrLaserJobNotFound
	}
	if writeLaserError(w, err, "read the laser job") {
		return nil, m, nil, false
	}
	return p, m, j, true
}

func writeLaserError(w http.ResponseWriter, err error, action string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrPrinterNotFound):
		writeJSONError(w, http.StatusNotFound, "Printer not found")
	case errors.Is(err, store.ErrNotOneShot):
		writeJSONError(w, http.StatusBadRequest, "That printer is not a laser")
	case errors.Is(err, store.ErrMaterialNotFound):
		writeJSONError(w, http.StatusNotFound, "The laser has no such material")
	case errors.Is(err, store.ErrLaserJobNotFound):
		writeJSONError(w, http.StatusNotFound, "Laser job not found")
	case errors.Is(err, store.ErrLaserJobState):
		writeJSONError(w, http.StatusConflict, "The job has already been started, or has ended")
	case errors.Is(err, printers.ErrInvalidMaterial):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeServerError(w, err, action)
	}
	return true
}

// handleListMaterials: GET /api/v1/printers/{id}/materials
func (s *Server) handleListMaterials(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMaterials(r.PathValue("id"))
	if writeLaserError(w, err, "list the materials") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"materials": list})
}

// handleSaveMaterial tunes a preset or saves one of the printer's own
// materials: PUT /api/v1/printers/{id}/materials/{material}
func (s *Server) handleSaveMaterial(w http.ResponseWriter, r *http.Request) {
	var m printers.Material
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	m.ID = r.PathValue("material")
	saved, err := s.store.SaveMaterial(r.PathValue("id"), m)
	if writeLaserError(w, err, "save the material") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"material": saved})
}

// handleDeleteMaterial deletes one of a printer's own materials, or puts a
// tuned preset back: DELETE /api/v1/printers/{id}/materials/{material}
func (s *Server) handleDeleteMaterial(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteMaterial(r.PathValue("id"), r.PathValue("material"))
	if writeLaserError(w, err, "delete the material") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Material deleted, or put back as the driver has it"})
}

// handlePrepareLaserJob renders a label for a laser and holds it ready to
// frame and start: POST /api/v1/printers/{id}/jobs. Nothing reaches the
// machine yet.
func (s *Server) handlePrepareLaserJob(w http.ResponseWriter, r *http.Request) {
	var req LaserJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	p, model, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return
	}
	var z float64
	if req.Workpiece.ZMM != nil || req.Camera {
		if z, ok = zOf(w, req.Workpiece.ZMM); !ok {
			return
		}
	}
	if req.Camera {
		// Placed by the camera, it is only where the camera says at the
		// height it was calibrated at.
		if !model.Camera {
			writeJSONError(w, http.StatusConflict, p.Name+" has no camera BAM! can use")
			return
		}
		if _, ok := s.calibrationAt(w, p, z); !ok {
			return
		}
	}
	material, err := s.store.Material(p.ID, req.Material)
	if writeLaserError(w, err, "read the material") {
		return
	}
	t, ok := s.loadTemplateFor(w, req.TemplateID)
	if !ok {
		return
	}
	anchor := labels.WorkpieceAnchor{}
	switch {
	case req.Anchor != nil:
		anchor = *req.Anchor
		if !labels.ValidCorner(anchor.Corner) || !(anchor.InsetMM >= 0 && anchor.InsetMM <= labels.MaxInsetMM) {
			writeJSONError(w, http.StatusBadRequest, `anchor: corner must be "center", "top_left", "top_right", "bottom_left" or "bottom_right", and inset_mm from 0 to 1000`)
			return
		}
	case t.WorkpieceAnchor != nil:
		anchor = *t.WorkpieceAnchor
	}
	spec := labelSpec(t)
	if _, err := printers.FitLabel(printers.OneShotGeometry(model, p.Name), spec); err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, unfitMessage(t, p, err))
		return
	}

	// The label as engraved: the template without its cut outlines, drawn
	// by the same renderer as every printer, at the material's lines per
	// inch.
	engraved := *render.WithoutCuts(t)
	engraved.DPI = material.Engrave.LPI
	takeLonger(w)
	images := s.newLabelImages(r.Context())
	var scene *render.Scene
	var problem *LabelProblem
	assetName := ""
	if req.AssetID == "" {
		scene, err = render.Layout(&engraved, previewData(&engraved, s.cfg.Server.BaseURL), images)
		var le *render.LayoutError
		if errors.As(err, &le) {
			problem, err = &LabelProblem{Path: le.Path, Message: le.Message}, nil
		}
	} else {
		scene, problem, err = s.layoutAsset(r, &engraved, req.AssetID, images)
	}
	if problem != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered", Details: []LabelProblem{*problem}})
		return
	}
	if err != nil {
		writeServerError(w, err, "render the label")
		return
	}
	if req.AssetID != "" {
		if a, err := s.store.GetAssetByID(req.AssetID); err == nil {
			assetName = a.Asset.Name
		}
	}

	in := printers.JobInput{
		Material:     material.Material,
		Workpiece:    printers.RectMM{X: req.Workpiece.XMM, Y: req.Workpiece.YMM, Width: req.Workpiece.WidthMM, Height: req.Workpiece.HeightMM},
		AngleDeg:     req.Workpiece.AngleDeg,
		LabelAt:      req.LabelAt,
		Anchor:       anchor,
		LabelWidthMM: spec.WidthMM, LabelHeightMM: spec.HeightMM,
		Engrave: scene.Raster(true),
	}
	for _, c := range render.CutPaths(t) {
		pl := printers.Polyline{Closed: c.Closed}
		for _, q := range c.Points {
			pl.Points = append(pl.Points, printers.PointMM{X: q[0], Y: q[1]})
		}
		in.Cuts = append(in.Cuts, pl)
	}
	job, warnings, err := printers.MakeJob(model, in)
	if errors.Is(err, printers.ErrDoesNotFitWorkpiece) {
		writeJSONError(w, http.StatusUnprocessableEntity, strings.ToUpper(err.Error()[:1])+err.Error()[1:]+".")
		return
	}
	if err != nil {
		writeServerError(w, err, "make the laser job")
		return
	}
	prepared := &laserPrepared{printerID: p.ID, job: job, warnings: append(warnings, t.PrintWarnings()...)}
	if job.Engrave != nil {
		var buf bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, job.Engrave); err != nil {
			writeServerError(w, err, "draw the preview")
			return
		}
		prepared.preview = buf.Bytes()
	}

	rec := models.LaserJob{PrinterID: &p.ID, PrinterName: p.Name, TemplateID: &t.ID, TemplateName: t.Name, AssetName: assetName,
		Material: material.Material, Placement: models.LaserPlacement{Workpiece: job.Workpiece, AngleDeg: job.AngleDeg, Placed: job.Placed, Label: job.Label,
			Mirror: job.Mirror, Cuts: len(job.Cuts), Camera: req.Camera, ZMM: z}}
	if req.AssetID != "" {
		rec.AssetID = &req.AssetID
	}
	saved, err := s.actingStore(r).CreateLaserJob(rec)
	if err != nil {
		writeServerError(w, err, "record the laser job")
		return
	}
	s.lasers.keep(saved.ID, prepared)
	writeJSON(w, http.StatusCreated, s.laserJobAnswer(p, saved))
}

// handleListLaserJobs is a laser's history: GET /api/v1/printers/{id}/jobs
func (s *Server) handleListLaserJobs(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return
	}
	jobs, err := s.store.ListLaserJobs(p.ID, "", 50)
	if writeLaserError(w, err, "list the laser's jobs") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// handleGetLaserJob: GET /api/v1/printers/{id}/jobs/{job}
func (s *Server) handleGetLaserJob(w http.ResponseWriter, r *http.Request) {
	p, _, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.laserJobAnswer(p, j))
}

// handleLaserJobPreview is a prepared job's engraving, as the machine is
// given it: GET /api/v1/printers/{id}/jobs/{job}/preview
func (s *Server) handleLaserJobPreview(w http.ResponseWriter, r *http.Request) {
	_, _, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	prepared := s.lasers.get(j.ID)
	if prepared == nil || prepared.preview == nil {
		writeJSONError(w, http.StatusNotFound, "This server holds no engraving for the job")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(prepared.preview)
}

// LaserStatus is what a laser is doing: its machine's status, as its
// driver reports it, and its job under way or prepared, if any.
type LaserStatus struct {
	Status *printers.MachineStatus `json:"status"`
	// Unreachable says why the machine did not answer.
	Unreachable string                `json:"unreachable,omitempty"`
	Model       printers.OneShotModel `json:"model"`
	Job         map[string]any        `json:"job"`
}

// handleLaserStatus: GET /api/v1/printers/{id}/status
func (s *Server) handleLaserStatus(w http.ResponseWriter, r *http.Request) {
	p, model, ok := s.loadLaser(w, r.PathValue("id"))
	if !ok {
		return
	}
	out := LaserStatus{Model: model}
	if st, err := s.machineStatus(r.Context(), p); err != nil {
		out.Unreachable = err.Error()
	} else {
		out.Status = &st
	}
	jobs, err := s.store.ListLaserJobs(p.ID, "", 1)
	if writeLaserError(w, err, "read the laser's job") {
		return
	}
	if len(jobs) > 0 && jobs[0].State != models.LaserJobDiscarded {
		out.Job = s.laserJobAnswer(p, &jobs[0])
	}
	writeJSON(w, http.StatusOK, out)
}

// machine reaches a laser's machine.
func (s *Server) machine(p *models.Printer) (printers.Machine, error) {
	return printers.MachineFor(p.Driver, p.Connection, store.PrinterSettings(p))
}

// machineStatus asks a laser's machine how it is, a few seconds at most.
func (s *Server) machineStatus(ctx context.Context, p *models.Printer) (printers.MachineStatus, error) {
	m, err := s.machine(p)
	if err != nil {
		return printers.MachineStatus{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.Status(ctx)
}

// atTheMachine refuses a request to move a laser that does not come from a
// person who may (docs/printing.md#lasers): one holding lasers.run, or a
// token given it, and never an assistant over MCP, a flow or an incoming
// hook, whatever they hold.
func atTheMachine(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if via := mcpserver.ViaFrom(r.Context()); via != "" {
			writeJSONError(w, http.StatusForbidden, fmt.Sprintf("Only a person at the laser starts it or traces its outline; %s cannot", via))
			return
		}
		if currentIncoming(r.Context()) != nil {
			writeJSONError(w, http.StatusForbidden, "Only a person at the laser starts it or traces its outline; an incoming hook cannot")
			return
		}
		if !HasPermission(r.Context(), rbac.PermLasersRun) {
			if !authenticated(r.Context()) {
				writeJSONError(w, http.StatusUnauthorized, "Authentication required")
				return
			}
			writeJSONError(w, http.StatusForbidden, "Starting a laser, or tracing its outline, needs lasers.run")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// heldJob is a laser job the server holds prepared, writing the error when
// it does not.
func (s *Server) heldJob(w http.ResponseWriter, j *models.LaserJob) (*laserPrepared, bool) {
	prepared := s.lasers.get(j.ID)
	if prepared == nil {
		writeJSONError(w, http.StatusConflict, "This server no longer holds the job ready: it was prepared on another server, before a restart, or another job took its place. Prepare it again.")
		return nil, false
	}
	return prepared, true
}

// handleFrameLaserJob has the machine trace a prepared job's outline with
// its low-power pointer: POST /api/v1/printers/{id}/jobs/{job}/frame
// (lasers.run, a person).
func (s *Server) handleFrameLaserJob(w http.ResponseWriter, r *http.Request) {
	p, model, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	if !model.Frames {
		writeJSONError(w, http.StatusConflict, p.Name+" cannot trace a job's outline")
		return
	}
	if j.State != models.LaserJobPrepared {
		writeJSONError(w, http.StatusConflict, "Only a job not yet started is framed")
		return
	}
	prepared, ok := s.heldJob(w, j)
	if !ok {
		return
	}
	m, err := s.machine(p)
	if err == nil {
		turn := s.printerTurn(p.ID)
		turn.Lock()
		err = m.Frame(context.WithoutCancel(r.Context()), prepared.job)
		turn.Unlock()
	}
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s did not frame the job: %v", p.Name, err))
		return
	}
	writeJSON(w, http.StatusOK, s.laserJobAnswer(p, j))
}

// handleStartLaserJob starts a prepared job, or sends it ready for the
// machine's own button, or resumes a paused one: POST
// /api/v1/printers/{id}/jobs/{job}/start (lasers.run, a person). The
// machine's interlocks are checked first, and any problem refuses it.
func (s *Server) handleStartLaserJob(w http.ResponseWriter, r *http.Request) {
	p, model, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	if j.State != models.LaserJobPrepared && j.State != models.LaserJobPaused {
		writeJSONError(w, http.StatusConflict, "Only a prepared or paused job is started")
		return
	}
	var prepared *laserPrepared
	if j.State == models.LaserJobPrepared {
		if prepared, ok = s.heldJob(w, j); !ok {
			return
		}
	}
	m, err := s.machine(p)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Could not reach %s: %v", p.Name, err))
		return
	}
	ctx := context.WithoutCancel(r.Context())
	turn := s.printerTurn(p.ID)
	turn.Lock()
	defer turn.Unlock()

	// The interlocks first, as the machine reports them now.
	st, err := m.Status(ctx)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Could not read %s's status, so it was not started: %v", p.Name, err))
		return
	}
	if problems := st.Problems(); len(problems) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("%s was not started: %s.", p.Name, strings.Join(problems, "; ")), "status": st})
		return
	}

	if j.State == models.LaserJobPaused {
		if err := m.Resume(ctx); err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s did not resume: %v", p.Name, err))
			return
		}
		if _, err := s.store.SetLaserJobState(j.ID, models.LaserJobRunning, ""); err != nil {
			writeServerError(w, err, "record the job resumed")
			return
		}
		j.State = models.LaserJobRunning
		writeJSON(w, http.StatusOK, s.laserJobAnswer(p, j))
		return
	}

	if st.Busy() {
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("%s is busy with another job", p.Name))
		return
	}
	state := models.LaserJobRunning
	if model.StartButton {
		state = models.LaserJobReady
	}
	// Recorded first, so the job is started once however many ask.
	started, err := s.actingStore(r).StartLaserJob(j.ID, state)
	if writeLaserError(w, err, "record the job started") {
		return
	}
	if err := m.Start(ctx, prepared.job); err != nil {
		msg := fmt.Sprintf("%s did not take the job: %v", p.Name, err)
		if _, err := s.store.SetLaserJobState(j.ID, models.LaserJobFailed, msg); err != nil {
			log.Printf("recording laser job %s failed: %v", j.ID, err)
		}
		s.lasers.drop(j.ID)
		writeJSONError(w, http.StatusBadGateway, msg)
		return
	}
	go s.watchLaserJob(p, m, started)
	writeJSON(w, http.StatusOK, s.laserJobAnswer(p, started))
}

// handlePauseLaserJob: POST /api/v1/printers/{id}/jobs/{job}/pause
func (s *Server) handlePauseLaserJob(w http.ResponseWriter, r *http.Request) {
	p, _, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	if j.State != models.LaserJobRunning {
		writeJSONError(w, http.StatusConflict, "Only a running job is paused")
		return
	}
	m, err := s.machine(p)
	if err == nil {
		err = m.Pause(context.WithoutCancel(r.Context()))
	}
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s did not pause: %v", p.Name, err))
		return
	}
	if _, err := s.store.SetLaserJobState(j.ID, models.LaserJobPaused, ""); err != nil {
		writeServerError(w, err, "record the job paused")
		return
	}
	j.State = models.LaserJobPaused
	writeJSON(w, http.StatusOK, s.laserJobAnswer(p, j))
}

// handleCancelLaserJob cancels a job: one under way on its machine, or one
// prepared and not started: POST /api/v1/printers/{id}/jobs/{job}/cancel
func (s *Server) handleCancelLaserJob(w http.ResponseWriter, r *http.Request) {
	p, _, j, ok := s.loadLaserJob(w, r)
	if !ok {
		return
	}
	if models.LaserJobEnded(j.State) {
		writeJSONError(w, http.StatusConflict, "The job has already ended")
		return
	}
	if j.State != models.LaserJobPrepared {
		m, err := s.machine(p)
		if err == nil {
			err = m.Cancel(context.WithoutCancel(r.Context()))
		}
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("%s did not cancel the job: %v", p.Name, err))
			return
		}
	}
	if _, err := s.store.SetLaserJobState(j.ID, models.LaserJobCancelled, ""); err != nil {
		writeServerError(w, err, "record the job cancelled")
		return
	}
	s.lasers.drop(j.ID)
	j, err := s.store.GetLaserJob(j.ID)
	if writeLaserError(w, err, "read the laser job") {
		return
	}
	writeJSON(w, http.StatusOK, s.laserJobAnswer(p, j))
}

// laserJobState is the job state a machine's state means for a job in
// state current, which it keeps while the machine says nothing new.
func laserJobState(machine, current string) string {
	switch machine {
	case printers.MachineReady:
		return models.LaserJobReady
	case printers.MachineRunning:
		return models.LaserJobRunning
	case printers.MachinePaused:
		return models.LaserJobPaused
	case printers.MachineDone:
		return models.LaserJobDone
	case printers.MachineCancelled:
		return models.LaserJobCancelled
	case printers.MachineFailed:
		return models.LaserJobFailed
	case printers.MachineIdle:
		// Idle after running is finished; a job sent ready waits on.
		if current == models.LaserJobRunning || current == models.LaserJobPaused {
			return models.LaserJobDone
		}
	}
	return current
}

// watchLaserJob follows a started job on its machine until it ends,
// recording its state as it changes, and in its asset's history that its
// label was engraved when it is done.
func (s *Server) watchLaserJob(p *models.Printer, m printers.Machine, j *models.LaserJob) {
	defer s.lasers.drop(j.ID)
	ctx := context.Background()
	var failing time.Time
	for {
		time.Sleep(s.lasers.poll)
		now, err := s.store.GetLaserJob(j.ID)
		if err != nil || models.LaserJobEnded(now.State) {
			return
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := m.Status(sctx)
		cancel()
		if err != nil {
			if failing.IsZero() {
				failing = time.Now()
			}
			if time.Since(failing) > s.lasers.lost {
				msg := fmt.Sprintf("Lost touch with %s: %v", p.Name, err)
				if _, err := s.store.SetLaserJobState(j.ID, models.LaserJobFailed, msg); err != nil {
					log.Printf("recording laser job %s failed: %v", j.ID, err)
				}
				return
			}
			continue
		}
		failing = time.Time{}
		state := laserJobState(st.State, now.State)
		if state == now.State {
			continue
		}
		changed, err := s.store.SetLaserJobState(j.ID, state, st.Message)
		if err != nil {
			log.Printf("recording laser job %s's state: %v", j.ID, err)
			continue
		}
		if changed && state == models.LaserJobDone && now.AssetID != nil {
			acting := s.store.As(now.StartedBy)
			if now.StartedByToken {
				acting = s.store.AsToken(now.StartedBy)
			}
			if err := acting.RecordLabelEngraved(*now.AssetID, now.TemplateName, now.PrinterName, now.Material.Name); err != nil {
				log.Printf("recording a label engraved on %s: %v", p.Name, err)
			}
		}
		if models.LaserJobEnded(state) {
			return
		}
	}
}
