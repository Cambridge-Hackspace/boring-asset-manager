package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// MaxCopies caps how many copies of each label one print request makes.
const MaxCopies = 20

// PrinterRequest creates or updates a printer.
type PrinterRequest struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Model  string `json:"model"`
	// Connection says how to reach the printer (docs/printing.md).
	Connection printers.Connection `json:"connection"`
	Media      string              `json:"media"`
	// Options are driver options (see GET /api/v1/printers/drivers).
	Options json.RawMessage `json:"options,omitempty"`
}

func (req PrinterRequest) printer(id string) models.Printer {
	return models.Printer{ID: id, Name: req.Name, Driver: req.Driver, Model: req.Model, Connection: req.Connection, Media: req.Media, Options: req.Options}
}

func (s *Server) handlePrinterDrivers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"drivers": printers.Drivers()})
}

func (s *Server) handleListPrinters(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPrinters()
	if err != nil {
		writeServerError(w, err, "list printers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": list})
}

func (s *Server) handleGetPrinter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printer": p})
}

func (s *Server) handleCreatePrinter(w http.ResponseWriter, r *http.Request) {
	var req PrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if writePrinterError(w, s.checkConnection(req.Connection)) {
		return
	}
	p, err := s.store.CreatePrinter(req.printer(""))
	if writePrinterError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"printer": p})
}

func (s *Server) handleUpdatePrinter(w http.ResponseWriter, r *http.Request) {
	var req PrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if writePrinterError(w, s.checkConnection(req.Connection)) {
		return
	}
	// What the change affects, said again in the answer for scripts.
	changes, moves, ok := s.changesFor(w, req.printer(r.PathValue("id")))
	if !ok {
		return
	}
	p, err := s.store.UpdatePrinter(req.printer(r.PathValue("id")))
	if writePrinterError(w, err) {
		return
	}
	// Labels whose categories offer a template that still fits move to it.
	if _, err := s.store.MoveSpoolTemplates(moves); err != nil {
		log.Printf("moving labels to templates that suit %s: %v", p.Name, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"printer": p, "changes": changes})
}

// handlePrinterChanges says what saving a printer as given would affect,
// without saving it: POST /api/v1/printers/{id}/changes
func (s *Server) handlePrinterChanges(w http.ResponseWriter, r *http.Request) {
	var req PrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	changes, _, ok := s.changesFor(w, req.printer(r.PathValue("id")))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes})
}

// changesFor checks a printer as it would be saved and works out what that
// would affect, and which labels would move, writing the error when it
// cannot.
func (s *Server) changesFor(w http.ResponseWriter, p models.Printer) (PrinterChanges, map[string]string, bool) {
	if _, err := s.store.GetPrinter(p.ID); writePrinterError(w, err) {
		return PrinterChanges{}, nil, false
	}
	if writePrinterError(w, s.checkConnection(p.Connection)) {
		return PrinterChanges{}, nil, false
	}
	next, err := s.store.PreviewPrinter(p)
	if writePrinterError(w, err) {
		return PrinterChanges{}, nil, false
	}
	changes, moves, err := s.printerChanges(next)
	if err != nil {
		writeServerError(w, err, "work out what the change affects")
		return PrinterChanges{}, nil, false
	}
	return changes, moves, true
}

func (s *Server) handleDeletePrinter(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeletePrinter(r.PathValue("id"))
	if writePrinterError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Printer deleted successfully"})
}

// checkConnection refuses connections this server may not use, such as a
// device outside its allowed paths.
func (s *Server) checkConnection(c printers.Connection) error {
	c, err := printers.NormalizeConnection(c)
	if err != nil {
		return err
	}
	if c.Type == printers.ConnWebhook {
		if _, err := s.webhookChannel(c.Address); err != nil {
			return fmt.Errorf("%w: connection: %v", printers.ErrInvalidPrinter, err)
		}
	}
	return s.transport.Check(c)
}

// sendToPrinter delivers a job to a printer, a Bluetooth LE one's
// connection taking its driver's profile where it sets none.
func (s *Server) sendToPrinter(ctx context.Context, p *models.Printer, job []byte) error {
	// A display that pulls, or is pushed through a webhook, is the
	// server's own to deliver to.
	if done, err := s.deliverToDisplay(ctx, p, job); done {
		return err
	}
	conn, err := printers.ConnectionFor(p.Driver, p.Connection)
	if err != nil {
		return fmt.Errorf("%w: %v", printers.ErrUnreachable, err)
	}
	return s.transport.Send(ctx, conn, job)
}

func writePrinterError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrPrinterNotFound):
		writeJSONError(w, http.StatusNotFound, "Printer not found")
	case errors.Is(err, store.ErrPrinterNameExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, printers.ErrInvalidPrinter):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, "save the printer")
	}
	return true
}

func (s *Server) loadPrinter(w http.ResponseWriter, id string) (*models.Printer, bool) {
	p, err := s.store.GetPrinter(id)
	if writePrinterError(w, err) {
		return nil, false
	}
	return p, true
}

// PrintRequest prints labels for a batch of assets.
type PrintRequest struct {
	TemplateID string   `json:"template_id"`
	AssetIDs   []string `json:"asset_ids"`
	// Copies of each label, printed one asset after another; defaults to 1.
	Copies int `json:"copies,omitempty"`
	// Feed, when given, is which way the labels go through the printer
	// this time, in place of the template's.
	Feed string `json:"feed,omitempty"`
	// Mirror, when given, is how the labels print mirrored this time, in
	// place of the template's: across, down, both, or none for the right
	// way round.
	Mirror string `json:"mirror,omitempty"`
}

// handlePrint renders labels and sends them to a printer:
// POST /api/v1/printers/{id}/print
func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	var req PrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	job, ok := s.preparePrint(w, r, r.PathValue("id"), req)
	if !ok {
		return
	}
	p := job.printer

	// The printer's turn, so a job from the spool never interleaves. Once
	// begun, a job is sent whole even if its caller goes: one cut short
	// prints some labels and loses the rest.
	turn := s.printerTurn(p.ID)
	turn.Lock()
	err := s.sendToPrinter(context.WithoutCancel(r.Context()), p, job.data)
	turn.Unlock()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Could not deliver the job to %s: %v", p.Name, err))
		return
	}
	s.recordPrinted(r, job)
	writeJSON(w, http.StatusOK, map[string]any{
		"printer_id": p.ID,
		"labels":     job.labels,
		"bytes":      len(job.data),
		"warnings":   job.warnings,
	})
}

// preparedPrint is a print request's job, rendered for its printer.
type preparedPrint struct {
	req      PrintRequest
	printer  *models.Printer
	template *labels.Template
	data     []byte
	labels   int
	warnings []string
}

// recordPrinted records that a job's labels were printed and, on a
// display, shows the last of them.
func (s *Server) recordPrinted(r *http.Request, job *preparedPrint) {
	// The labels are printed whether or not their history records it.
	if err := s.actingStore(r).RecordLabelsPrinted(job.req.AssetIDs, job.template.Name, job.printer.Name, job.req.Copies); err != nil {
		log.Printf("recording labels printed on %s: %v", job.printer.Name, err)
	}
	s.rememberShown(s.actingStore(r), job.printer, job.req.TemplateID, job.req.AssetIDs[len(job.req.AssetIDs)-1])
}

// preparePrint checks a print request and renders its job for the printer
// it names, answering w itself when it cannot.
func (s *Server) preparePrint(w http.ResponseWriter, r *http.Request, printerID string, req PrintRequest) (*preparedPrint, bool) {
	if req.Copies == 0 {
		req.Copies = 1
	}
	if req.Copies < 1 || req.Copies > MaxCopies {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("copies must be 1-%d", MaxCopies))
		return nil, false
	}
	if !labels.ValidFeed(req.Feed) {
		writeJSONError(w, http.StatusBadRequest, `feed must be "auto", "across" or "along"`)
		return nil, false
	}
	if req.Mirror != labels.MirrorNone && !labels.ValidMirror(req.Mirror) {
		writeJSONError(w, http.StatusBadRequest, `mirror must be "across", "down", "both" or "none"`)
		return nil, false
	}
	if !checkAssetIDs(w, req.AssetIDs) {
		return nil, false
	}
	if total := len(req.AssetIDs) * req.Copies; total > MaxExportAssets {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("a print job is limited to %d labels (asked for %d)", MaxExportAssets, total))
		return nil, false
	}

	p, ok := s.loadPrinter(w, printerID)
	if !ok {
		return nil, false
	}
	t, ok := s.loadTemplateFor(w, req.TemplateID)
	if !ok {
		return nil, false
	}
	if req.Feed != "" || req.Mirror != "" {
		this := *t
		if req.Feed != "" {
			this.Feed = req.Feed
		}
		switch req.Mirror {
		case "":
		case labels.MirrorNone:
			this.PrintMirror = ""
		default:
			this.PrintMirror = req.Mirror
		}
		t = &this
	}
	// Check the template against the printer before laying out assets.
	plan, t, warnings, err := s.planFor(p, t)
	if errors.Is(err, printers.ErrDoesNotFit) {
		writeJSONError(w, http.StatusUnprocessableEntity, unfitMessage(t, p, err))
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "plan the print job")
		return nil, false
	}

	takeLonger(w)
	scenes, ok := s.layoutBatch(w, r, t, req.AssetIDs)
	if !ok {
		return nil, false
	}
	tags, err := s.tagWriterFor(plan)
	if err != nil {
		writeServerError(w, err, "read the RFID settings")
		return nil, false
	}
	rendered := newLabelJob(plan)
	for i, scene := range scenes {
		// layoutBatch answers itself unless every asset laid out, in order.
		epc, err := tags.epc(req.AssetIDs[i])
		if isTagError(err) {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return nil, false
		}
		if err != nil {
			writeServerError(w, err, "find the RFID tag's EPC")
			return nil, false
		}
		rendered.add(scene, req.Copies, epc)
	}
	data, err := rendered.encode()
	if err != nil {
		writeServerError(w, err, "encode print job")
		return nil, false
	}
	return &preparedPrint{req: req, printer: p, template: t, data: data, labels: rendered.len(), warnings: nonNil(warnings)}, true
}

// labelSpec is a template's size in millimeters, with its elements' boxes
// as its content.
func labelSpec(t *labels.Template) printers.LabelSpec {
	mm := 1.0
	if t.Units == labels.UnitsIn {
		mm = 25.4
	}
	label := printers.LabelSpec{WidthMM: t.Width * mm, HeightMM: t.Height * mm, DPI: t.DPI, Feed: t.Feed, Inks: t.Inks()}
	for _, e := range t.Elements {
		x, y, w, h := e.Bounds()
		label.Content = append(label.Content, printers.RectMM{X: x * mm, Y: y * mm, Width: w * mm, Height: h * mm})
	}
	return label
}

// FitRequest asks whether labels of a physical size suit a printer, and
// optionally where they have content, for warnings.
type FitRequest struct {
	WidthMM  float64           `json:"width_mm"`
	HeightMM float64           `json:"height_mm"`
	DPI      int               `json:"dpi"`
	Content  []printers.RectMM `json:"content,omitempty"`
	// Feed is which way the labels go through the printer when they fit
	// either way; empty or "auto" is as fits.
	Feed string `json:"feed,omitempty"`
	// Inks are the inks the labels draw in, for a warning when the
	// printer lacks one.
	Inks []string `json:"inks,omitempty"`
}

// handlePrinterFit runs a printer's fit check without printing, for the
// template editor: POST /api/v1/printers/{id}/fit
func (s *Server) handlePrinterFit(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req FitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WidthMM <= 0 || req.HeightMM <= 0 || req.DPI <= 0 {
		writeJSONError(w, http.StatusBadRequest, "width_mm, height_mm and dpi must be positive")
		return
	}
	if !labels.ValidFeed(req.Feed) {
		writeJSONError(w, http.StatusBadRequest, `feed must be "auto", "across" or "along"`)
		return
	}
	for _, ink := range req.Inks {
		if !labels.ValidInk(ink) {
			writeJSONError(w, http.StatusBadRequest, "inks must name inks: "+strings.Join(labels.Inks, ", "))
			return
		}
	}
	label := printers.LabelSpec{WidthMM: req.WidthMM, HeightMM: req.HeightMM, DPI: req.DPI, Content: req.Content, Feed: req.Feed, Inks: req.Inks}
	var fit printers.Fit
	var err error
	if printers.IsOneShot(p.Driver) {
		// A laser's labels fit its work area (docs/printing.md#lasers).
		var g printers.Geometry
		if g, err = printers.GeometryOf(p.Driver, store.PrinterSettings(p)); err == nil {
			fit, err = printers.FitLabel(g, label)
		}
	} else {
		_, fit, err = printers.PlanLabel(p.Driver, store.PrinterSettings(p), label)
	}
	switch {
	case errors.Is(err, printers.ErrDoesNotFit):
		writeJSON(w, http.StatusOK, map[string]any{"fits": false, "message": fmt.Sprintf("This template can't print on %s: %s.", p.Name, printers.Why(err)), "warnings": []string{}})
	case err != nil:
		writeServerError(w, err, "check the fit")
	default:
		// Which way it goes through the printer: its width across the roll, or along it.
		feed := labels.FeedAcross
		if fit.Rotate {
			feed = labels.FeedAlong
		}
		writeJSON(w, http.StatusOK, map[string]any{"fits": true, "warnings": nonNil(fit.Warnings), "feed": feed, "palette": fit.Palette,
			"printable": fit.Printable, "edge_mm": fit.EdgeMM, "outside": nonNilInts(fit.Outside), "near_edge": nonNilInts(fit.NearEdge)})
	}
}

// nonNilInts is list, or an empty one, so it is written as [] rather than null.
func nonNilInts(list []int) []int {
	if list == nil {
		return []int{}
	}
	return list
}

// handleTestPrint prints one label of a template that need not be saved,
// for an asset or with placeholders, for the template editor:
// POST /api/v1/printers/{id}/test-print. Its labels are not recorded in
// the asset's history.
func (s *Server) handleTestPrint(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Template) == 0 {
		writeJSONError(w, http.StatusBadRequest, `Expected {"template": {...}, "asset_id": "optional"}`)
		return
	}
	doc, err := withPreviewDefaults(req.Template)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "template must be a JSON object")
		return
	}
	t, err := labels.Parse(doc)
	if writeTemplateError(w, err) {
		return
	}
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	plan, laid, warnings, err := s.planFor(p, t)
	if errors.Is(err, printers.ErrDoesNotFit) {
		writeJSONError(w, http.StatusUnprocessableEntity, unfitMessage(t, p, err))
		return
	}
	if err != nil {
		writeServerError(w, err, "plan the test print")
		return
	}

	images := s.newLabelImages(r.Context())
	var scene *render.Scene
	var problem *LabelProblem
	if req.AssetID == "" {
		scene, err = render.Layout(laid, previewData(laid, s.cfg.Server.BaseURL), images)
		var le *render.LayoutError
		if errors.As(err, &le) {
			problem, err = &LabelProblem{Path: le.Path, Message: le.Message}, nil
		}
	} else {
		scene, problem, err = s.layoutAsset(r, laid, req.AssetID, images)
	}
	if problem != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered", Details: []LabelProblem{*problem}})
		return
	}
	if err != nil {
		writeServerError(w, err, "render the test print")
		return
	}
	tags, err := s.tagWriterFor(plan)
	if err != nil {
		writeServerError(w, err, "read the RFID settings")
		return
	}
	epc, err := tags.epc(req.AssetID)
	if isTagError(err) {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "find the RFID tag's EPC")
		return
	}
	one := newLabelJob(plan)
	one.add(scene, 1, epc)
	job, err := one.encode()
	if err != nil {
		writeServerError(w, err, "encode the test print")
		return
	}
	// It may wait for the printer behind a long job, and is then sent whole.
	takeLonger(w)
	turn := s.printerTurn(p.ID)
	turn.Lock()
	err = s.sendToPrinter(context.WithoutCancel(r.Context()), p, job)
	turn.Unlock()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Could not deliver the job to %s: %v", p.Name, err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printer_id": p.ID, "labels": 1, "bytes": len(job), "warnings": nonNil(warnings)})
}

// labelJob gathers a print job's labels as its plan takes them: drawn in
// the printer's inks when it prints more than black and white
// (docs/labels.md#inks), 1-bit otherwise.
type labelJob struct {
	plan  printers.Plan
	inks  printers.InkPlan
	gray  []*image.Gray
	color []*image.Paletted
	// epcs are each gray label's RFID tag's EPC (docs/rfid.md), "" for
	// none.
	epcs []string
}

func newLabelJob(plan printers.Plan) *labelJob {
	j := &labelJob{plan: plan}
	if p, ok := plan.(printers.InkPlan); ok && p.Palette() != labels.PaletteBW {
		j.inks = p
	}
	return j
}

// add draws a label, to print copies of, each with its RFID tag written
// with epc where the printer writes tags.
func (j *labelJob) add(scene *render.Scene, copies int, epc string) {
	if j.inks != nil {
		img := scene.RasterInks(j.inks.Palette())
		for range copies {
			j.color = append(j.color, img)
		}
		return
	}
	img := scene.Raster(true)
	for range copies {
		j.gray = append(j.gray, img)
		j.epcs = append(j.epcs, epc)
	}
}

// len is how many labels it prints.
func (j *labelJob) len() int { return len(j.gray) + len(j.color) }

// encode is the print job.
func (j *labelJob) encode() ([]byte, error) {
	if j.inks != nil {
		return j.inks.EncodeInks(j.color)
	}
	if p, ok := j.plan.(printers.TagPlan); ok && p.EncodesTags() {
		return p.EncodeTagged(j.gray, j.epcs)
	}
	return j.plan.Encode(j.gray)
}

// nonNil is list, or an empty one, so it is written as [] rather than null.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
