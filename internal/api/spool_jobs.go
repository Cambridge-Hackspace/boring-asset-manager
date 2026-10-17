package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Printing from the spool (README §6.2): jobs are made here and sent by the
// spooler in the background.

// SpoolPrintRequest prints waiting entries: those listed, or every ready
// entry going to a printer when none are.
type SpoolPrintRequest struct {
	EntryIDs []string `json:"entry_ids,omitempty"`
}

// SpoolNotPrinted is an entry left out of printing, and why.
type SpoolNotPrinted struct {
	EntryID   string `json:"entry_id"`
	AssetName string `json:"asset_name"`
	Reason    string `json:"reason"`
}

// SpoolPrintResponse lists the jobs made and the entries left out.
type SpoolPrintResponse struct {
	Jobs    []models.SpoolJob `json:"jobs"`
	Skipped []SpoolNotPrinted `json:"skipped"`
}

// handlePrintSpool: POST /api/v1/spool/print
func (s *Server) handlePrintSpool(w http.ResponseWriter, r *http.Request) {
	var req SpoolPrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	var entries []models.SpoolEntry
	listed := req.EntryIDs != nil
	if listed {
		var ok bool
		if entries, ok = s.spoolEntriesFor(w, r, req.EntryIDs, false); !ok {
			return
		}
	} else {
		all, err := s.store.ListSpool(store.SpoolFilter{})
		if err != nil {
			writeServerError(w, err, "read the spool")
			return
		}
		entries = spoolReadable(r, all)
	}
	o, err := s.loadSpoolOptions()
	if err != nil {
		writeServerError(w, err, "read templates and printers")
		return
	}
	jobs, skipped := o.plan(entries, listed)
	resp := SpoolPrintResponse{Jobs: []models.SpoolJob{}, Skipped: skipped}
	if len(jobs) > 0 {
		ids, err := s.actingStore(r).CreateSpoolJobs(jobs)
		if err != nil {
			writeSpoolError(w, err, "make print jobs")
			return
		}
		s.wakeSpool()
		if resp.Jobs, err = s.store.ListSpoolJobs(time.Time{}, ids); err != nil {
			writeServerError(w, err, "read the print jobs")
			return
		}
	}
	status := http.StatusOK
	if len(resp.Jobs) > 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, resp)
}

// plan sorts entries into print jobs: one for each printer and template,
// in the order the entries were added, each holding at most
// MaxExportAssets labels. Entries that cannot print as they stand are left
// out, with why; with listed unset, those going to a download or already
// being printed are left out quietly.
func (o *spoolOptions) plan(entries []models.SpoolEntry, listed bool) ([]store.SpoolJobNew, []SpoolNotPrinted) {
	skipped := []SpoolNotPrinted{}
	skip := func(e models.SpoolEntry, why string) {
		skipped = append(skipped, SpoolNotPrinted{EntryID: e.ID, AssetName: e.AssetName, Reason: why})
	}
	type group struct {
		job    *store.SpoolJobNew
		labels int
	}
	var order []string
	groups := map[string][]*group{}
	var jobs []store.SpoolJobNew
	for _, e := range entries {
		switch {
		case e.Job != nil:
			if listed {
				skip(e, "Already being printed")
			}
			continue
		case e.Destination == models.DestinationDownload:
			if listed {
				skip(e, "Goes to a download; download it instead")
			}
			continue
		}
		if blockers := o.blockers(e); len(blockers) > 0 {
			skip(e, strings.Join(blockers, "; "))
			continue
		}
		printerID := strings.TrimPrefix(e.Destination, models.DestinationPrinterPrefix)
		key := printerID + "|" + *e.TemplateID
		list := groups[key]
		if len(list) == 0 || list[len(list)-1].labels+e.Copies > MaxExportAssets {
			if len(list) == 0 {
				order = append(order, key)
			}
			list = append(list, &group{job: &store.SpoolJobNew{
				TemplateID: *e.TemplateID, TemplateName: o.templates[*e.TemplateID].Name,
				PrinterID: printerID, PrinterName: o.printers[printerID].Name,
			}})
			groups[key] = list
		}
		g := list[len(list)-1]
		g.job.EntryIDs = append(g.job.EntryIDs, e.ID)
		g.labels += e.Copies
	}
	for _, key := range order {
		for _, g := range groups[key] {
			jobs = append(jobs, *g.job)
		}
	}
	return jobs, skipped
}

// handleListSpoolJobs lists print jobs, newest first, from the last 30 days
// unless ?older=true: GET /api/v1/spool/jobs
func (s *Server) handleListSpoolJobs(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if r.URL.Query().Get("older") != "true" {
		since = time.Now().Add(-printedShown)
	}
	jobs, err := s.store.ListSpoolJobs(since, nil)
	if err != nil {
		writeServerError(w, err, "list print jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// handleCancelSpoolJob cancels a job not yet started; its labels wait in
// the spool again: POST /api/v1/spool/jobs/{id}/cancel
func (s *Server) handleCancelSpoolJob(w http.ResponseWriter, r *http.Request) {
	if err := s.store.CancelSpoolJob(r.PathValue("id")); err != nil {
		writeSpoolError(w, err, "cancel the job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"canceled": r.PathValue("id")})
}

// SpoolResendRequest sends a past job's labels again: to Destination when
// given, else where the job went.
type SpoolResendRequest struct {
	Destination *string `json:"destination,omitempty"`
}

// SpoolResendResponse says what sending again did: the labels put back in
// the spool, as adding does, and the jobs made to print them.
type SpoolResendResponse struct {
	SpoolAddResponse
	Jobs       []models.SpoolJob `json:"jobs"`
	NotPrinted []SpoolNotPrinted `json:"not_printed"`
}

// handleResendSpoolJob puts the labels a job printed back in the spool,
// and prints them at once when they go to a printer:
// POST /api/v1/spool/jobs/{id}/resend
func (s *Server) handleResendSpoolJob(w http.ResponseWriter, r *http.Request) {
	var req SpoolResendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	found, err := s.store.ListSpoolJobs(time.Time{}, []string{id})
	if err != nil {
		writeServerError(w, err, "read the job")
		return
	}
	if len(found) == 0 {
		writeJSONError(w, http.StatusNotFound, "Print job not found")
		return
	}
	job := found[0]
	destination := ""
	switch {
	case req.Destination != nil:
		destination = *req.Destination
	case job.Download:
		destination = models.DestinationDownload
	case job.PrinterID != nil:
		destination = models.DestinationPrinterPrefix + *job.PrinterID
	default:
		writeJSONError(w, http.StatusBadRequest, "Its printer was deleted; choose where to send it")
		return
	}
	printed, err := s.store.ListSpool(store.SpoolFilter{Printed: true, JobID: id})
	if err != nil {
		writeServerError(w, err, "read the job's labels")
		return
	}
	skipped := []SpoolSkipped{}
	adds := []store.SpoolAdd{}
	for _, e := range spoolReadable(r, printed) {
		if e.AssetArchived {
			skipped = append(skipped, SpoolSkipped{AssetID: e.AssetID, Reason: "archived"})
			continue
		}
		adds = append(adds, store.SpoolAdd{AssetID: e.AssetID, TemplateID: e.TemplateID, Destination: destination, Copies: e.Copies})
	}
	if len(adds) == 0 && len(skipped) == 0 {
		writeJSONError(w, http.StatusConflict, "Nothing of this job was printed; its labels are waiting in the spool")
		return
	}
	added, already, err := s.actingStore(r).AddToSpool(adds)
	if err != nil {
		writeSpoolError(w, err, "send the job again")
		return
	}
	resp := SpoolResendResponse{SpoolAddResponse: SpoolAddResponse{Added: added, AlreadyWaiting: already, Skipped: skipped}, Jobs: []models.SpoolJob{}, NotPrinted: []SpoolNotPrinted{}}
	if strings.HasPrefix(destination, models.DestinationPrinterPrefix) && len(added) > 0 {
		o, err := s.loadSpoolOptions()
		if err != nil {
			writeServerError(w, err, "read templates and printers")
			return
		}
		var jobs []store.SpoolJobNew
		jobs, resp.NotPrinted = o.plan(added, true)
		if len(jobs) > 0 {
			ids, err := s.actingStore(r).CreateSpoolJobs(jobs)
			if err != nil {
				writeSpoolError(w, err, "make print jobs")
				return
			}
			s.wakeSpool()
			if resp.Jobs, err = s.store.ListSpoolJobs(time.Time{}, ids); err != nil {
				writeServerError(w, err, "read the print jobs")
				return
			}
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

// spoolJobError answers the errors of printing from the spool, for
// writeSpoolError.
func spoolJobError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, store.ErrSpoolEntryBusy):
		return http.StatusConflict, "A job is printing some of those labels; wait for it, or cancel it", true
	case errors.Is(err, store.ErrSpoolJobNotFound):
		return http.StatusNotFound, "Print job not found", true
	case errors.Is(err, store.ErrSpoolJobStarted):
		return http.StatusConflict, "The job has started, and what is sent cannot be called back", true
	}
	return 0, "", false
}
