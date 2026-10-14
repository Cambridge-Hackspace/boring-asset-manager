package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// MaxExportAssets caps one batch export or print job.
const MaxExportAssets = 500

// maxBatchParts bounds the shapes (path segments, barcode rectangles and
// images) a batch of labels may hold in all: room for 500 ordinary labels
// of about a thousand each, or a few very detailed ones, in a few hundred
// megabytes. A variable so tests can lower it.
var maxBatchParts = 5_000_000

// exportWriteTimeout replaces the server's write timeout for batch exports,
// which can take longer than an ordinary request.
const exportWriteTimeout = 2 * time.Minute

var labelContentTypes = map[string]string{
	"svg": "image/svg+xml",
	"png": "image/png",
	"pdf": "application/pdf",
}

// LabelProblem explains why one asset's label could not be rendered.
type LabelProblem struct {
	AssetID string `json:"asset_id,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// labelImages loads uploaded images for rendering, decoding each once per
// request.
type labelImages struct {
	s     *Server
	cache map[string]image.Image
}

func (s *Server) newLabelImages() *labelImages {
	return &labelImages{s: s, cache: map[string]image.Image{}}
}

func (li *labelImages) LoadImage(id string) (image.Image, error) {
	if img, ok := li.cache[id]; ok {
		return img, nil
	}
	data, err := li.s.images.Read(id)
	if err != nil {
		return nil, errors.New("image is missing from storage")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	li.cache[id] = img
	return img, nil
}

// labelData gathers what bindings resolve against for one asset.
func (s *Server) labelData(a *models.AssetWithDetails) labels.Data {
	codes := make(map[string]string, len(a.GeneratorKeys))
	for _, gk := range a.GeneratorKeys {
		codes[gk.GeneratorType] = gk.AssignedCode
	}
	return labels.Data{
		AssetID:   a.Asset.ID,
		AssetName: a.Asset.Name,
		BaseURL:   s.cfg.Server.BaseURL,
		Codes:     codes,
		Fields:    a.FieldValues,
	}
}

// layoutAsset loads an asset and lays out its label. A non-nil problem means
// the asset itself cannot be labeled; err is reserved for server failures.
func (s *Server) layoutAsset(t *labels.Template, assetID string, images render.ImageSource) (*render.Scene, *LabelProblem, error) {
	a, err := s.store.GetAssetByID(assetID)
	if errors.Is(err, store.ErrAssetNotFound) {
		return nil, &LabelProblem{AssetID: assetID, Message: "asset not found"}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if a.Asset.ArchivedAt != nil {
		return nil, &LabelProblem{AssetID: assetID, Message: "asset is archived"}, nil
	}
	scene, err := render.Layout(t, s.labelData(a), images)
	var le *render.LayoutError
	if errors.As(err, &le) {
		return nil, &LabelProblem{AssetID: assetID, Path: le.Path, Message: le.Message}, nil
	}
	return scene, nil, err
}

// writeLabel renders one scene in format: svg, png (grayscale, or 1-bit when
// mono) or pdf.
func writeLabel(w io.Writer, scene *render.Scene, format string, mono bool) error {
	switch format {
	case "svg":
		return render.WriteSVG(w, scene)
	case "png":
		return render.WritePNG(w, scene, mono)
	default:
		return render.WritePDF(w, scene)
	}
}

func setDownloadHeaders(w http.ResponseWriter, contentType, filename, disposition string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, sanitizeForHeader(filename)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}

// handleAssetLabel renders one asset's label:
// GET /api/v1/assets/{id}/label?template_id=…&format=svg|png|pdf[&mono=true]
func (s *Server) handleAssetLabel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "svg"
	}
	contentType, ok := labelContentTypes[format]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, `format must be "svg", "png" or "pdf"`)
		return
	}
	mono := q.Get("mono") == "true" || q.Get("mono") == "1"
	t, ok := s.loadTemplateFor(w, q.Get("template_id"))
	if !ok {
		return
	}

	assetID := r.PathValue("id")
	scene, problem, err := s.layoutAsset(t, assetID, s.newLabelImages())
	if err != nil {
		writeServerError(w, err, "render label")
		return
	}
	if problem != nil {
		status := http.StatusUnprocessableEntity
		if problem.Message == "asset not found" {
			status = http.StatusNotFound
		}
		writeJSON(w, status, ErrorResponse{Error: "Label cannot be rendered", Details: []LabelProblem{*problem}})
		return
	}

	var buf bytes.Buffer
	if err := writeLabel(&buf, scene, format, mono); err != nil {
		writeServerError(w, err, "render label")
		return
	}
	setDownloadHeaders(w, contentType, fmt.Sprintf("%s-%s.%s", assetID, t.ID, format), "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// loadTemplateFor fetches a template by ID, writing the error response when
// it cannot.
func (s *Server) loadTemplateFor(w http.ResponseWriter, id string) (*labels.Template, bool) {
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "template_id is required")
		return nil, false
	}
	t, err := s.store.GetTemplate(id)
	if errors.Is(err, store.ErrTemplateNotFound) {
		writeJSONError(w, http.StatusNotFound, "Template not found")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "query template")
		return nil, false
	}
	return t, true
}

// PreviewRequest renders a template that need not be saved yet.
type PreviewRequest struct {
	Template json.RawMessage `json:"template"`
	// AssetID selects real data; without it placeholders are used.
	AssetID string `json:"asset_id,omitempty"`
}

// previewData stands in for an asset when previewing without one. Every
// generator gets a code that each symbology accepts.
func previewData(t *labels.Template, baseURL string) labels.Data {
	d := labels.Data{
		AssetID:   "00000000-0000-4000-8000-000000000000",
		AssetName: "Sample asset",
		BaseURL:   baseURL,
		Codes:     map[string]string{},
		Fields:    map[string]string{},
	}
	for _, genType := range generators.AllowedGeneratorTypes() {
		d.Codes[genType] = "BAM-0001"
	}
	for _, key := range t.FieldKeys() {
		d.Fields[key] = "[" + key + "]"
	}
	return d
}

// handlePreviewTemplate renders an unsaved template as SVG for the editor:
// POST /api/v1/templates/preview
func (s *Server) handlePreviewTemplate(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Template) == 0 {
		writeJSONError(w, http.StatusBadRequest, `Expected {"template": {...}, "asset_id": "optional"}`)
		return
	}
	doc, err := withPreviewID(req.Template)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "template must be a JSON object")
		return
	}
	t, err := labels.Parse(doc)
	if writeTemplateError(w, err) {
		return
	}

	images := s.newLabelImages()
	var scene *render.Scene
	if req.AssetID == "" {
		scene, err = render.Layout(t, previewData(t, s.cfg.Server.BaseURL), images)
		var le *render.LayoutError
		if errors.As(err, &le) {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered",
				Details: []LabelProblem{{Path: le.Path, Message: le.Message}}})
			return
		}
	} else {
		var problem *LabelProblem
		scene, problem, err = s.layoutAsset(t, req.AssetID, images)
		if problem != nil {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered", Details: []LabelProblem{*problem}})
			return
		}
	}
	if err != nil {
		writeServerError(w, err, "render preview")
		return
	}

	var buf bytes.Buffer
	if err := render.WriteSVG(&buf, scene); err != nil {
		writeServerError(w, err, "render preview")
		return
	}
	setDownloadHeaders(w, labelContentTypes["svg"], "preview.svg", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// withPreviewID gives an unsaved template a placeholder ID so it passes
// validation; previews do not need a real one.
func withPreviewID(doc json.RawMessage) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, errors.New("not an object")
	}
	if id, ok := obj["id"]; !ok || string(id) == `""` || string(id) == "null" {
		obj["id"] = json.RawMessage(`"preview"`)
	}
	return json.Marshal(obj)
}

// ExportRequest selects the labels for a batch export.
type ExportRequest struct {
	TemplateID string   `json:"template_id"`
	AssetIDs   []string `json:"asset_ids"`
	// Format is "pdf" (one page per label) or "zip" (one PNG per label).
	Format string `json:"format"`
	// Mono renders the PNGs in a ZIP export as 1-bit images.
	Mono bool `json:"mono,omitempty"`
}

// checkAssetIDs enforces the batch size and rejects repeated IDs.
func checkAssetIDs(w http.ResponseWriter, assetIDs []string) bool {
	if len(assetIDs) == 0 || len(assetIDs) > MaxExportAssets {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("asset_ids must list 1-%d assets", MaxExportAssets))
		return false
	}
	seen := make(map[string]bool, len(assetIDs))
	for _, id := range assetIDs {
		if seen[id] {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("asset %s is listed more than once", id))
			return false
		}
		seen[id] = true
	}
	return true
}

// layoutBatch lays out every requested asset before anything is written, so
// a batch either renders completely or not at all.
func (s *Server) layoutBatch(w http.ResponseWriter, t *labels.Template, assetIDs []string) ([]*render.Scene, bool) {

	images := s.newLabelImages()
	scenes := make([]*render.Scene, 0, len(assetIDs))
	var problems []LabelProblem
	parts := 0
	for _, id := range assetIDs {
		scene, problem, err := s.layoutAsset(t, id, images)
		if err != nil {
			writeServerError(w, err, "render labels")
			return nil, false
		}
		if problem != nil {
			problems = append(problems, *problem)
			continue
		}
		// Every scene is held until the batch is written, so their size
		// together is bounded, and the batch stopped as soon as it is over.
		if parts += scene.Parts(); parts > maxBatchParts {
			writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"These labels are too detailed to produce together (over %d shapes); produce fewer at a time", maxBatchParts))
			return nil, false
		}
		scenes = append(scenes, scene)
	}
	if len(problems) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{
			Error:   fmt.Sprintf("%d of %d labels cannot be rendered; nothing was produced", len(problems), len(assetIDs)),
			Details: problems,
		})
		return nil, false
	}
	return scenes, true
}

// handleExportLabels renders a batch as one PDF or a ZIP of PNGs:
// POST /api/v1/labels/export
func (s *Server) handleExportLabels(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Format != "pdf" && req.Format != "zip" {
		writeJSONError(w, http.StatusBadRequest, `format must be "pdf" or "zip"`)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteTimeout))

	if !checkAssetIDs(w, req.AssetIDs) {
		return
	}
	t, ok := s.loadTemplateFor(w, req.TemplateID)
	if !ok {
		return
	}
	scenes, ok := s.layoutBatch(w, t, req.AssetIDs)
	if !ok {
		return
	}

	if req.Format == "pdf" {
		var buf bytes.Buffer
		if err := render.WritePDF(&buf, scenes...); err != nil {
			writeServerError(w, err, "render labels")
			return
		}
		setDownloadHeaders(w, labelContentTypes["pdf"], "labels-"+t.ID+".pdf", "attachment")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
		return
	}

	// Every label is already laid out, so only I/O can fail from here on.
	setDownloadHeaders(w, "application/zip", "labels-"+t.ID+".zip", "attachment")
	w.WriteHeader(http.StatusOK)
	zw := zip.NewWriter(w)
	for i, scene := range scenes {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: req.AssetIDs[i] + ".png", Method: zip.Store})
		if err != nil {
			return
		}
		if err := render.WritePNG(f, scene, req.Mono); err != nil {
			return
		}
	}
	_ = zw.Close()
}

// sanitizeForHeader keeps a filename safe inside a quoted header value.
func sanitizeForHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r > 0x7E {
			return '_'
		}
		return r
	}, s)
}

// handleListGenerators describes the barcode generators: GET /api/v1/generators
func (s *Server) handleListGenerators(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"generators": generators.Describe()})
}
