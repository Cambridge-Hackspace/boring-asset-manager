package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"maps"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/preview"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
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

// longRequestTimeout replaces the server's timeouts for work that can take
// far longer than an ordinary request: laying out hundreds of labels, or
// waiting while a printer prints them.
const longRequestTimeout = 30 * time.Minute

// takeLonger lets a request run for up to longRequestTimeout from now. The
// read deadline matters as much as the write: once the server's read
// timeout passes, it cancels the request's context, ending whatever the
// request was doing, though its response could still be written.
func takeLonger(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	until := time.Now().Add(longRequestTimeout)
	_ = rc.SetReadDeadline(until)
	_ = rc.SetWriteDeadline(until)
}

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
// request, and the icons labels draw (render.IconSource), reading the
// build and the palette once.
type labelImages struct {
	s     *Server
	ctx   context.Context
	cache map[string]image.Image
	// icons is the current build, read on first use; noIcons says it was
	// read and there is none, or it could not be read.
	icons   *ir.Document
	noIcons bool
	palette map[string]string
}

// LabelIcon is the icon a label draws by name: the unknown icon for a
// name the build does not draw, or with no build at all.
func (li *labelImages) LabelIcon(name string) *ir.Icon {
	if li.icons == nil && !li.noIcons {
		li.noIcons = true
		if b, err := li.s.store.CurrentIconBuild(); err == nil && b != nil {
			if doc, err := ir.Parse(b.IR); err == nil {
				li.icons, li.noIcons = doc, false
			}
		}
	}
	if li.icons != nil {
		if icon := li.icons.Icon(name); icon != nil {
			return icon
		}
	}
	unknown := isolex.Unknown()
	return &unknown
}

// IconColors are the palette's light colors, by token.
func (li *labelImages) IconColors() map[string]string {
	if li.palette == nil {
		li.palette = map[string]string{}
		if palette, err := li.s.store.GetIconPalette(); err == nil {
			for _, c := range palette {
				li.palette[c.Token] = c.Light
			}
		}
	}
	return li.palette
}

// newLabelImages loads label images for one rendering, as long as ctx
// lasts.
func (s *Server) newLabelImages(ctx context.Context) *labelImages {
	return &labelImages{s: s, ctx: ctx, cache: map[string]image.Image{}}
}

func (li *labelImages) LoadImage(id string) (image.Image, error) {
	if img, ok := li.cache[id]; ok {
		return img, nil
	}
	data, err := li.s.images.Read(li.ctx, id)
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
	return bindingData(a, s.cfg.Server.BaseURL)
}

// iconData is a resolved icon as a label draws it: its plain name, and
// the names it is drawn under with its params and its state.
func iconData(ic *models.ResolvedIcon) labels.IconData {
	d := labels.IconData{Icon: ic.Plain(), IconColor: ic.Color, BoundIcon: ic.Bound}
	if ic.State != "" {
		d.StateIcon = ic.Name
	}
	return d
}

// bindingData gathers what bindings resolve against for one asset, on a
// label or in a report.
func bindingData(a *models.AssetWithDetails, baseURL string) labels.Data {
	codes := make(map[string]string, len(a.GeneratorKeys))
	for _, gk := range a.GeneratorKeys {
		codes[gk.GeneratorType] = gk.AssignedCode
	}
	d := labels.Data{
		AssetID:   a.Asset.ID,
		AssetName: a.Asset.Name,
		BaseURL:   baseURL,
		Codes:     codes,
		Fields:    a.FieldValues,
		Condition: conditionText(a.Asset.Condition),
	}
	names := make([]string, 0, len(a.Categories))
	for _, c := range a.Categories {
		names = append(names, c.Name)
	}
	d.Categories = strings.Join(names, ", ")
	places := make([]string, 0, len(a.Location))
	for _, p := range a.Location {
		places = append(places, p.Name)
	}
	d.Location = strings.Join(places, " › ")
	if st := a.Asset.Stock; st != nil {
		d.Quantity, d.Unit = strconv.Itoa(st.Quantity), st.Unit
	}
	if ic := a.ResolvedIcon; ic != nil {
		screen := iconData(ic)
		d.Icon, d.IconColor, d.BoundIcon, d.StateIcon = screen.Icon, screen.IconColor, screen.BoundIcon, screen.StateIcon
		d.DrawnIcons = ic.Drawn
	}
	if ic := a.ResolvedIcons[models.RenditionLabel]; ic != nil && ic != a.ResolvedIcon {
		label := iconData(ic)
		d.LabelIcon = &label
		// the names differ, so both are drawn from one map
		if len(ic.Drawn) > 0 {
			drawn := make(map[string]json.RawMessage, len(d.DrawnIcons)+len(ic.Drawn))
			maps.Copy(drawn, d.DrawnIcons)
			maps.Copy(drawn, ic.Drawn)
			d.DrawnIcons = drawn
		}
	}
	if l := a.Loan; l != nil {
		// By name only; a borrower hidden from the request (no ID) is blank.
		d.Borrower = l.Borrower.Name
		if d.Borrower == "" && l.Borrower.ID != "" {
			d.Borrower = UnnamedUser
		}
		if l.DueAt != nil {
			d.Due = l.DueAt.UTC().Format("2006-01-02")
		}
	}
	return d
}

// conditionText is how a condition reads on a label or in a report.
func conditionText(c string) string {
	switch c {
	case models.ConditionNeedsMaintenance:
		return "Needs maintenance"
	case models.ConditionOutOfService:
		return "Out of service"
	}
	return "Working"
}

// layoutAsset loads an asset and lays out its label. One the request cannot
// read is not found, and its location shows only places it can read. A non-nil problem means
// the asset itself cannot be labeled; err is reserved for server failures.
func (s *Server) layoutAsset(r *http.Request, t *labels.Template, assetID string, images render.ImageSource) (*render.Scene, *LabelProblem, error) {
	scene, _, problem, err := s.layoutAssetData(r, t, assetID, images)
	return scene, problem, err
}

// layoutAssetData is layoutAsset, with the data the label was filled from.
func (s *Server) layoutAssetData(r *http.Request, t *labels.Template, assetID string, images render.ImageSource) (*render.Scene, labels.Data, *LabelProblem, error) {
	a, err := s.store.GetAssetByID(assetID)
	if errors.Is(err, store.ErrAssetNotFound) || (err == nil && !Allows(r.Context(), rbac.PermAssetsRead, a.Scopes)) {
		return nil, labels.Data{}, &LabelProblem{AssetID: assetID, Message: "asset not found"}, nil
	}
	if err != nil {
		return nil, labels.Data{}, nil, err
	}
	if a.Asset.ArchivedAt != nil {
		return nil, labels.Data{}, &LabelProblem{AssetID: assetID, Message: "asset is archived"}, nil
	}
	data := s.labelData(s.shown(r, a))
	scene, err := render.Layout(t, data, images)
	var le *render.LayoutError
	if errors.As(err, &le) {
		return nil, data, &LabelProblem{AssetID: assetID, Path: le.Path, Message: le.Message}, nil
	}
	return scene, data, nil, err
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
	scene, problem, err := s.layoutAsset(r, t, assetID, s.newLabelImages(r.Context()))
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
	setDownloadHeaders(w, contentType, fmt.Sprintf("%s-%s.%s", assetID, fileStem(t), format), "inline")
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
	// Format is how a preview answers: svg (the default), png (grayscale,
	// or 1-bit with Mono), or json, which describes each element as laid
	// out: its box and what it prints.
	Format string `json:"format,omitempty"`
	Mono   bool   `json:"mono,omitempty"`
}

// The preview's description, shared with the editor's live renderer.
type (
	PreviewElement     = preview.Element
	PreviewCode        = preview.Code
	PreviewSides       = preview.Sides
	PreviewBox         = preview.Box
	PreviewDescription = preview.Description
)

func describePreview(t *labels.Template, data labels.Data, scene *render.Scene) PreviewDescription {
	return preview.Describe(t, data, scene)
}

// previewData stands in for an asset when previewing without one.
func previewData(t *labels.Template, baseURL string) labels.Data { return preview.Data(t, baseURL) }

func withPreviewDefaults(doc json.RawMessage) ([]byte, error) { return preview.WithDefaults(doc) }

// PreviewDataResponse is what the editor's live renderer draws a label
// with: an asset's data, or the sample data, whose fields the renderer
// fills with placeholders for each template it draws.
type PreviewDataResponse struct {
	Data         labels.Data `json:"data"`
	Placeholders bool        `json:"placeholders"`
}

// handlePreviewData gives the editor's live renderer the data a preview of
// an asset, or of no asset, is drawn with: POST /api/v1/templates/preview-data
func (s *Server) handlePreviewData(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, `Expected {"asset_id": "optional"}`)
		return
	}
	if req.AssetID == "" {
		writeJSON(w, http.StatusOK, PreviewDataResponse{Data: preview.Sample(s.cfg.Server.BaseURL), Placeholders: true})
		return
	}
	a, err := s.store.GetAssetByID(req.AssetID)
	if errors.Is(err, store.ErrAssetNotFound) || (err == nil && !Allows(r.Context(), rbac.PermAssetsRead, a.Scopes)) {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered",
			Details: []LabelProblem{{AssetID: req.AssetID, Message: "asset not found"}}})
		return
	}
	if err != nil {
		writeServerError(w, err, "query asset")
		return
	}
	if a.Asset.ArchivedAt != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered",
			Details: []LabelProblem{{AssetID: req.AssetID, Message: "asset is archived"}}})
		return
	}
	writeJSON(w, http.StatusOK, PreviewDataResponse{Data: s.labelData(s.shown(r, a))})
}

// handlePreviewTemplate renders an unsaved template for the editor, as SVG,
// PNG or a description of its elements: POST /api/v1/templates/preview
func (s *Server) handlePreviewTemplate(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Template) == 0 {
		writeJSONError(w, http.StatusBadRequest, `Expected {"template": {...}, "asset_id": "optional"}`)
		return
	}
	switch req.Format {
	case "":
		req.Format = "svg"
	case "svg", "png", "json":
	default:
		writeJSONError(w, http.StatusBadRequest, `format must be "svg", "png" or "json"`)
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

	images := s.newLabelImages(r.Context())
	var (
		scene *render.Scene
		data  labels.Data
	)
	if req.AssetID == "" {
		data = previewData(t, s.cfg.Server.BaseURL)
		scene, err = render.Layout(t, data, images)
		var le *render.LayoutError
		if errors.As(err, &le) {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered",
				Details: []LabelProblem{{Path: le.Path, Message: le.Message}}})
			return
		}
	} else {
		var problem *LabelProblem
		scene, data, problem, err = s.layoutAssetData(r, t, req.AssetID, images)
		if problem != nil {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "Label cannot be rendered", Details: []LabelProblem{*problem}})
			return
		}
	}
	if err != nil {
		writeServerError(w, err, "render preview")
		return
	}
	if req.Format == "json" {
		writeJSON(w, http.StatusOK, describePreview(t, data, scene))
		return
	}

	var buf bytes.Buffer
	if err := writeLabel(&buf, scene, req.Format, req.Mono); err != nil {
		writeServerError(w, err, "render preview")
		return
	}
	setDownloadHeaders(w, labelContentTypes[req.Format], "preview."+req.Format, "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
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
func (s *Server) layoutBatch(w http.ResponseWriter, r *http.Request, t *labels.Template, assetIDs []string) ([]*render.Scene, bool) {

	images := s.newLabelImages(r.Context())
	scenes := make([]*render.Scene, 0, len(assetIDs))
	var problems []LabelProblem
	parts := 0
	for _, id := range assetIDs {
		scene, problem, err := s.layoutAsset(r, t, id, images)
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
	takeLonger(w)

	if !checkAssetIDs(w, req.AssetIDs) {
		return
	}
	t, ok := s.loadTemplateFor(w, req.TemplateID)
	if !ok {
		return
	}
	scenes, ok := s.layoutBatch(w, r, t, req.AssetIDs)
	if !ok {
		return
	}

	if req.Format == "pdf" {
		var buf bytes.Buffer
		if err := render.WritePDF(&buf, scenes...); err != nil {
			writeServerError(w, err, "render labels")
			return
		}
		setDownloadHeaders(w, labelContentTypes["pdf"], "labels-"+fileStem(t)+".pdf", "attachment")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
		return
	}

	// Every label is already laid out, so only I/O can fail from here on.
	setDownloadHeaders(w, "application/zip", "labels-"+fileStem(t)+".zip", "attachment")
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

// fileStem names a template in a download's filename: its name, with each run
// of spaces or of anything a filesystem or header might object to turned into
// one hyphen. A name with nothing usable left falls back to the ID.
func fileStem(t *labels.Template) string {
	return nameStem(t.Name, t.ID)
}

// nameStem makes a name safe in a file name, or gives fallback when nothing
// of it is.
func nameStem(name, fallback string) string {
	stem := strings.Trim(unsafeFileChars.ReplaceAllString(name, "-"), "-.")
	if stem == "" {
		return fallback
	}
	return stem
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

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
// handleNextCode suggests the next code of a generator type, one more than
// the highest ending in a number: GET /api/v1/generators/{type}/next-code.
// The code is "" when none ends in a number.
func (s *Server) handleNextCode(w http.ResponseWriter, r *http.Request) {
	genType := r.PathValue("type")
	if !generators.IsValidGeneratorType(genType) {
		writeJSONError(w, http.StatusNotFound, "Unknown code type")
		return
	}
	code, err := s.store.NextCode(genType)
	if err != nil {
		writeServerError(w, err, "suggest a code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) handleListGenerators(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"generators": generators.Describe()})
}
