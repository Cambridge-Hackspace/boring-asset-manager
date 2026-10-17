package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// The spool (README §6.2): labels gathered over time, sorted onto templates
// and printers in bulk, and printed or downloaded together. Everything here
// needs labels.print; each entry also needs its asset readable, and entries
// for assets the requester cannot read are not shown to them.

// printedShown is how far back the Printed list goes unless asked for all.
const printedShown = 30 * 24 * time.Hour

// SpoolEntryView is a spool entry with what keeps it from printing as it
// stands, for those waiting.
type SpoolEntryView struct {
	models.SpoolEntry
	Problems []string `json:"problems"`
}

// SpoolChoice names a template or printer an entry can take.
type SpoolChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SpoolResponse is the spool, with the templates and printers its entries
// can take and, for each template, the printers its labels suit.
type SpoolResponse struct {
	Entries   []SpoolEntryView    `json:"entries"`
	Templates []SpoolChoice       `json:"templates"`
	Printers  []SpoolChoice       `json:"printers"`
	Fits      map[string][]string `json:"fits"`
}

// spoolOptions are the templates and printers spool entries can take, and
// why each template does not suit each printer it does not.
type spoolOptions struct {
	templates map[string]*labels.Template
	printers  map[string]*models.Printer
	// unfit maps a template and a printer to why its labels do not suit
	// that printer; a pair missing from it fits.
	unfit map[string]map[string]string
	resp  SpoolResponse
}

// loadSpoolOptions reads the templates and printers, checking each template
// against each printer.
func (s *Server) loadSpoolOptions() (*spoolOptions, error) {
	templates, err := s.store.ListTemplates()
	if err != nil {
		return nil, err
	}
	printerList, err := s.store.ListPrinters()
	if err != nil {
		return nil, err
	}
	o := &spoolOptions{templates: map[string]*labels.Template{}, printers: map[string]*models.Printer{}, unfit: map[string]map[string]string{},
		resp: SpoolResponse{Templates: []SpoolChoice{}, Printers: []SpoolChoice{}, Fits: map[string][]string{}}}
	for i := range printerList {
		p := &printerList[i]
		o.printers[p.ID] = p
		o.resp.Printers = append(o.resp.Printers, SpoolChoice{ID: p.ID, Name: p.Name})
	}
	for i := range templates {
		t := &templates[i]
		o.templates[t.ID] = t
		o.resp.Templates = append(o.resp.Templates, SpoolChoice{ID: t.ID, Name: t.Name})
		o.unfit[t.ID] = map[string]string{}
		o.resp.Fits[t.ID] = []string{}
		for _, p := range printerList {
			if _, err := planFor(&p, t); err != nil {
				o.unfit[t.ID][p.ID] = err.Error()
			} else {
				o.resp.Fits[t.ID] = append(o.resp.Fits[t.ID], p.ID)
			}
		}
	}
	return o, nil
}

// onlyPrinter is where an entry with this template goes when nothing else
// says: the one printer its labels suit, when exactly one does.
func (o *spoolOptions) onlyPrinter(templateID *string) string {
	if templateID == nil || len(o.resp.Fits[*templateID]) != 1 {
		return ""
	}
	return models.DestinationPrinterPrefix + o.resp.Fits[*templateID][0]
}

// problems says what keeps a waiting entry from printing as it stands.
func (o *spoolOptions) problems(e models.SpoolEntry) []string {
	out := []string{}
	if e.AssetArchived {
		out = append(out, "Its asset is archived, and archived assets are never labeled")
	}
	t := (*labels.Template)(nil)
	if e.TemplateID == nil {
		out = append(out, "No template chosen")
	} else {
		t = o.templates[*e.TemplateID]
	}
	switch {
	case e.Destination == "":
		out = append(out, "No printer chosen")
	case strings.HasPrefix(e.Destination, models.DestinationPrinterPrefix) && t != nil:
		id := strings.TrimPrefix(e.Destination, models.DestinationPrinterPrefix)
		if why, ok := o.unfit[t.ID][id]; ok {
			out = append(out, fmt.Sprintf("%s does not suit %s: %s", t.Name, o.printers[id].Name, why))
		}
	}
	if e.Problem != "" {
		out = append(out, e.Problem)
	}
	return out
}

// spoolReadable keeps the entries whose assets the request may read.
func spoolReadable(r *http.Request, entries []models.SpoolEntry) []models.SpoolEntry {
	kept := entries[:0]
	for _, e := range entries {
		if Allows(r.Context(), rbac.PermAssetsRead, e.AssetScopes) {
			kept = append(kept, e)
		}
	}
	return kept
}

// handleListSpool: GET /api/v1/spool. The waiting entries, or with
// ?printed=true the Printed list, from the last 30 days unless
// ?older=true; with ?mine=true, only those the requester added.
func (s *Server) handleListSpool(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.SpoolFilter{Printed: q.Get("printed") == "true"}
	if f.Printed && q.Get("older") != "true" {
		f.PrintedSince = time.Now().Add(-printedShown)
	}
	if q.Get("mine") == "true" {
		f.AddedBy = actorOf(r)
	}
	entries, err := s.store.ListSpool(f)
	if err != nil {
		writeServerError(w, err, "list the spool")
		return
	}
	o, err := s.loadSpoolOptions()
	if err != nil {
		writeServerError(w, err, "read templates and printers")
		return
	}
	resp := o.resp
	resp.Entries = []SpoolEntryView{}
	for _, e := range spoolReadable(r, entries) {
		v := SpoolEntryView{SpoolEntry: e, Problems: []string{}}
		if !f.Printed {
			v.Problems = o.problems(e)
		}
		resp.Entries = append(resp.Entries, v)
	}
	writeJSON(w, http.StatusOK, resp)
}

// actorOf is the ID of the user or API token making the request.
func actorOf(r *http.Request) string {
	if t := CurrentToken(r.Context()); t != nil {
		return t.ID
	}
	if u := CurrentUser(r.Context()); u != nil {
		return u.ID
	}
	return ""
}

// SpoolAddRequest adds labels to the spool, for the assets of exactly one
// of: AssetIDs; a category, CategoryID, with its subcategories' when
// Subcategories is set; or Filter, an asset filter as GET /api/v1/assets
// takes it. TemplateID, when given, is the template for all of them;
// otherwise each takes the one its categories name. Destination, when
// given, is where all of them print; otherwise each goes to the one printer
// its template suits, if only one does. Copies defaults to 1.
type SpoolAddRequest struct {
	AssetIDs      []string          `json:"asset_ids,omitempty"`
	CategoryID    string            `json:"category_id,omitempty"`
	Subcategories bool              `json:"subcategories,omitempty"`
	Filter        map[string]string `json:"filter,omitempty"`
	TemplateID    string            `json:"template_id,omitempty"`
	Destination   *string           `json:"destination,omitempty"`
	Copies        int               `json:"copies,omitempty"`
}

// SpoolSkipped is an asset not added, and why.
type SpoolSkipped struct {
	AssetID string `json:"asset_id"`
	Reason  string `json:"reason"`
}

// SpoolAddResponse says what adding did: the entries added, the assets
// whose labels were already waiting, and those skipped.
type SpoolAddResponse struct {
	Added          []models.SpoolEntry `json:"added"`
	AlreadyWaiting []string            `json:"already_waiting"`
	Skipped        []SpoolSkipped      `json:"skipped"`
}

// handleAddToSpool: POST /api/v1/spool
func (s *Server) handleAddToSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	sources := 0
	for _, given := range []bool{req.AssetIDs != nil, req.CategoryID != "", req.Filter != nil} {
		if given {
			sources++
		}
	}
	if sources != 1 {
		writeJSONError(w, http.StatusBadRequest, "Give exactly one of asset_ids, category_id or filter")
		return
	}
	if len(req.AssetIDs) > store.MaxSpoolWaiting {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("asset_ids may list at most %d assets", store.MaxSpoolWaiting))
		return
	}

	skipped := []SpoolSkipped{}
	var assets []models.AssetWithDetails
	switch {
	case req.AssetIDs != nil:
		found, err := s.store.AssetsByIDs(req.AssetIDs)
		if err != nil {
			writeServerError(w, err, "read the assets")
			return
		}
		byID := map[string]models.AssetWithDetails{}
		for _, a := range found {
			byID[a.Asset.ID] = a
		}
		for _, id := range req.AssetIDs {
			a, ok := byID[id]
			switch {
			case !ok || !Allows(r.Context(), rbac.PermAssetsRead, a.Scopes):
				skipped = append(skipped, SpoolSkipped{AssetID: id, Reason: "not found"})
			case a.Asset.ArchivedAt != nil:
				skipped = append(skipped, SpoolSkipped{AssetID: id, Reason: "archived"})
			default:
				assets = append(assets, a)
			}
		}
	case req.CategoryID != "":
		ids, err := s.store.CategoryAssetIDs(req.CategoryID, req.Subcategories)
		if errors.Is(err, store.ErrCategoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "Category not found")
			return
		}
		if err != nil {
			writeServerError(w, err, "list the category's assets")
			return
		}
		found, err := s.store.AssetsByIDs(ids)
		if err != nil {
			writeServerError(w, err, "read the assets")
			return
		}
		for _, a := range found {
			if Allows(r.Context(), rbac.PermAssetsRead, a.Scopes) {
				assets = append(assets, a)
			}
		}
	default:
		q := url.Values{}
		for k, v := range req.Filter {
			q.Set(k, v)
		}
		if q.Get("archived") == "true" {
			writeJSONError(w, http.StatusBadRequest, "Archived assets are never labeled")
			return
		}
		found, ok := s.filterAssets(w, r, q)
		if !ok {
			return
		}
		assets = found
	}

	o, err := s.loadSpoolOptions()
	if err != nil {
		writeServerError(w, err, "read templates and printers")
		return
	}
	copies := req.Copies
	if copies == 0 {
		copies = 1
	}
	adds := make([]store.SpoolAdd, 0, len(assets))
	for _, a := range assets {
		add := store.SpoolAdd{AssetID: a.Asset.ID, Copies: copies}
		switch {
		case req.TemplateID != "":
			add.TemplateID = &req.TemplateID
		case a.LabelTemplate.TemplateID != "":
			id := a.LabelTemplate.TemplateID
			add.TemplateID = &id
		}
		if req.Destination != nil {
			add.Destination = *req.Destination
		} else {
			add.Destination = o.onlyPrinter(add.TemplateID)
		}
		adds = append(adds, add)
	}
	s.addToSpool(w, r, adds, skipped)
}

// addToSpool adds labels and answers with what was done: 201 when
// anything was added.
func (s *Server) addToSpool(w http.ResponseWriter, r *http.Request, adds []store.SpoolAdd, skipped []SpoolSkipped) {
	added, already, err := s.actingStore(r).AddToSpool(adds)
	if err != nil {
		writeSpoolError(w, err, "add to the spool")
		return
	}
	status := http.StatusOK
	if len(added) > 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, SpoolAddResponse{Added: added, AlreadyWaiting: already, Skipped: skipped})
}

// writeSpoolError answers a failure to change the spool.
func writeSpoolError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrSpoolFull):
		writeJSONError(w, http.StatusUnprocessableEntity, "The spool holds at most "+fmt.Sprint(store.MaxSpoolWaiting)+" labels waiting; print or remove some first")
	case errors.Is(err, store.ErrSpoolEntryNotFound):
		writeJSONError(w, http.StatusNotFound, "Spool entry not found")
	case errors.Is(err, store.ErrTemplateNotFound):
		writeJSONError(w, http.StatusBadRequest, "No template has that ID")
	case errors.Is(err, store.ErrPrinterNotFound):
		writeJSONError(w, http.StatusBadRequest, "No printer has that ID")
	case errors.Is(err, store.ErrInvalidDestination), errors.Is(err, store.ErrInvalidCopies):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	default:
		writeServerError(w, err, action)
	}
}

// SpoolEntriesRequest names spool entries.
type SpoolEntriesRequest struct {
	EntryIDs []string `json:"entry_ids"`
}

// SpoolUpdateRequest changes waiting entries in bulk: the template ("" for
// none), the destination ("printer:<id>", "download", or "" for none) and
// copies, each only when given. Setting a template on entries with no
// destination sends them to the one printer it suits, if only one does.
type SpoolUpdateRequest struct {
	EntryIDs    []string `json:"entry_ids"`
	TemplateID  *string  `json:"template_id,omitempty"`
	Destination *string  `json:"destination,omitempty"`
	Copies      *int     `json:"copies,omitempty"`
}

// spoolEntriesFor reads the entries a request names, every one of which
// must be in the spool, in the state wanted (printed or waiting), and
// readable by the request. Otherwise it answers the request itself.
func (s *Server) spoolEntriesFor(w http.ResponseWriter, r *http.Request, ids []string, printed bool) ([]models.SpoolEntry, bool) {
	if len(ids) == 0 || len(ids) > store.MaxSpoolWaiting {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("entry_ids must list 1-%d entries", store.MaxSpoolWaiting))
		return nil, false
	}
	entries, err := s.store.ListSpool(store.SpoolFilter{Printed: printed, IDs: ids})
	if err != nil {
		writeServerError(w, err, "read the spool")
		return nil, false
	}
	if len(spoolReadable(r, entries)) != len(ids) {
		writeJSONError(w, http.StatusNotFound, "Spool entry not found")
		return nil, false
	}
	return entries, true
}

// handleUpdateSpool: PATCH /api/v1/spool
func (s *Server) handleUpdateSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	entries, ok := s.spoolEntriesFor(w, r, req.EntryIDs, false)
	if !ok {
		return
	}
	u := store.SpoolUpdate{Copies: req.Copies}
	if req.TemplateID != nil {
		u.SetTemplate = true
		if *req.TemplateID != "" {
			u.TemplateID = req.TemplateID
		}
	}
	if req.Destination != nil {
		u.SetDestination, u.Destination = true, *req.Destination
	}
	if err := s.store.UpdateSpool(req.EntryIDs, u); err != nil {
		writeSpoolError(w, err, "update the spool")
		return
	}
	if u.TemplateID != nil && !u.SetDestination {
		o, err := s.loadSpoolOptions()
		if err != nil {
			writeServerError(w, err, "read templates and printers")
			return
		}
		var unsent []string
		for _, e := range entries {
			if e.Destination == "" {
				unsent = append(unsent, e.ID)
			}
		}
		if only := o.onlyPrinter(u.TemplateID); only != "" && len(unsent) > 0 {
			if err := s.store.UpdateSpool(unsent, store.SpoolUpdate{SetDestination: true, Destination: only}); err != nil {
				writeSpoolError(w, err, "update the spool")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(req.EntryIDs)})
}

// handleRemoveFromSpool: POST /api/v1/spool/remove
func (s *Server) handleRemoveFromSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolEntriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if _, ok := s.spoolEntriesFor(w, r, req.EntryIDs, false); !ok {
		return
	}
	if err := s.store.RemoveFromSpool(req.EntryIDs); err != nil {
		writeSpoolError(w, err, "remove from the spool")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": len(req.EntryIDs)})
}

// handleRequeueSpool puts printed entries back in the spool to print
// again, with the template, destination and copies they had:
// POST /api/v1/spool/requeue
func (s *Server) handleRequeueSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolEntriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	entries, ok := s.spoolEntriesFor(w, r, req.EntryIDs, true)
	if !ok {
		return
	}
	skipped := []SpoolSkipped{}
	adds := make([]store.SpoolAdd, 0, len(entries))
	for _, e := range entries {
		if e.AssetArchived {
			skipped = append(skipped, SpoolSkipped{AssetID: e.AssetID, Reason: "archived"})
			continue
		}
		adds = append(adds, store.SpoolAdd{AssetID: e.AssetID, TemplateID: e.TemplateID, Destination: e.Destination, Copies: e.Copies})
	}
	s.addToSpool(w, r, adds, skipped)
}

// SpoolDownloadRequest downloads waiting entries' labels, all of one
// template, as one PDF or a ZIP of PNGs (with Mono, 1-bit).
type SpoolDownloadRequest struct {
	EntryIDs []string `json:"entry_ids"`
	Format   string   `json:"format"`
	Mono     bool     `json:"mono,omitempty"`
}

// handleDownloadSpool lays out the entries' labels, each as many times as
// its copies, and answers with them as one file. Entries whose labels
// cannot be produced stay in the spool with the reason; the rest count as
// printed. POST /api/v1/spool/download
func (s *Server) handleDownloadSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Format != "pdf" && req.Format != "zip" {
		writeJSONError(w, http.StatusBadRequest, `format must be "pdf" or "zip"`)
		return
	}
	entries, ok := s.spoolEntriesFor(w, r, req.EntryIDs, false)
	if !ok {
		return
	}
	templateID := ""
	for _, e := range entries {
		if e.TemplateID == nil {
			writeJSONError(w, http.StatusBadRequest, "Choose a template for every label first")
			return
		}
		if templateID != "" && *e.TemplateID != templateID {
			writeJSONError(w, http.StatusBadRequest, "Download labels of one template at a time")
			return
		}
		templateID = *e.TemplateID
	}
	t, ok := s.loadTemplateFor(w, templateID)
	if !ok {
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteTimeout))

	images := s.newLabelImages()
	type laidOut struct {
		entry models.SpoolEntry
		scene *render.Scene
	}
	var done []laidOut
	problems, details := map[string]string{}, []LabelProblem{}
	parts := 0
	for _, e := range entries {
		scene, problem, err := s.layoutAsset(r, t, e.AssetID, images)
		if err != nil {
			writeServerError(w, err, "render labels")
			return
		}
		if problem != nil {
			msg := problem.Message
			if problem.Path != "" {
				msg = problem.Path + ": " + msg
			}
			problems[e.ID] = "Could not be produced: " + msg
			details = append(details, *problem)
			continue
		}
		if parts += scene.Parts() * e.Copies; parts > maxBatchParts {
			writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"These labels are too detailed to produce together (over %d shapes); download fewer at a time", maxBatchParts))
			return
		}
		done = append(done, laidOut{e, scene})
	}
	if err := s.store.SetSpoolProblems(problems); err != nil {
		writeServerError(w, err, "record the spool's problems")
		return
	}
	if len(done) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "None of these labels can be produced", Details: details})
		return
	}

	var buf bytes.Buffer
	if req.Format == "pdf" {
		var scenes []*render.Scene
		for _, d := range done {
			for c := 0; c < d.entry.Copies; c++ {
				scenes = append(scenes, d.scene)
			}
		}
		if err := render.WritePDF(&buf, scenes...); err != nil {
			writeServerError(w, err, "render labels")
			return
		}
	} else {
		zw := zip.NewWriter(&buf)
		for _, d := range done {
			for c := 1; c <= d.entry.Copies; c++ {
				name := d.entry.AssetID + ".png"
				if c > 1 {
					name = fmt.Sprintf("%s-%d.png", d.entry.AssetID, c)
				}
				f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
				if err == nil {
					err = render.WritePNG(f, d.scene, req.Mono)
				}
				if err != nil {
					writeServerError(w, err, "render labels")
					return
				}
			}
		}
		if err := zw.Close(); err != nil {
			writeServerError(w, err, "render labels")
			return
		}
	}

	ids := make([]string, len(done))
	for i, d := range done {
		ids[i] = d.entry.ID
	}
	if err := s.actingStore(r).MarkSpoolPrinted(ids, t.Name, ""); err != nil {
		writeSpoolError(w, err, "mark the labels printed")
		return
	}
	contentType := labelContentTypes["pdf"]
	if req.Format == "zip" {
		contentType = "application/zip"
	}
	setDownloadHeaders(w, contentType, "spool-"+fileStem(t)+"."+req.Format, "attachment")
	w.Header().Set("X-Spool-Problems", fmt.Sprint(len(problems)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// CategoryLabelTemplateRequest names a category's label template, or with
// null or "", none.
type CategoryLabelTemplateRequest struct {
	LabelTemplateID *string `json:"label_template_id"`
}

// handleSetCategoryLabelTemplate: PUT /api/v1/categories/{id}/label-template
func (s *Server) handleSetCategoryLabelTemplate(w http.ResponseWriter, r *http.Request) {
	var req CategoryLabelTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.LabelTemplateID != nil && *req.LabelTemplateID == "" {
		req.LabelTemplateID = nil
	}
	cat, err := s.store.SetCategoryLabelTemplate(r.PathValue("id"), req.LabelTemplateID)
	switch {
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrTemplateNotFound):
		writeJSONError(w, http.StatusBadRequest, "No label template has that ID")
	case err != nil:
		writeStoreError(w, err, "set the label template")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": cat})
	}
}

// handleLabelConflicts lists the categories in label template conflicts,
// as handleScanConflicts does for scan templates:
// GET /api/v1/categories/label-conflicts[?category_id=&label_template_id=]
func (s *Server) handleLabelConflicts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var template *string
	if t := q.Get("label_template_id"); t != "" {
		template = &t
	}
	involved, err := s.store.LabelConflicts(q.Get("category_id"), template)
	if err != nil {
		writeServerError(w, err, "find label template conflicts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": involved})
}
