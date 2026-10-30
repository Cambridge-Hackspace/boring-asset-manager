package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
)

// Importing a package, or parts of a backup, uploaded as backups are
// (README §2.3, #145): checked first, saying what it would do, then
// imported as checked. Both take as long as the archive does, so each runs
// as a job. The importer's permissions follow what it holds, and
// authentication never comes in through the web.

// packageImportRequest is what POST /api/v1/import/check and POST
// /api/v1/import take: the upload, the category to import under, what to
// do with what clashes, the kinds of a backup to merge, and, to import,
// the check's fingerprint.
type packageImportRequest struct {
	UploadID    string                    `json:"upload_id"`
	Anchor      string                    `json:"anchor"`
	Choices     map[string]archive.Choice `json:"choices"`
	Kinds       []archive.Kind            `json:"kinds"`
	Fingerprint string                    `json:"fingerprint"`
}

// packageImportResult is what a check or an import came to: its plan,
// even when refused, so what it needs is shown.
type packageImportResult struct {
	Plan *archive.ImportPlan `json:"plan"`
}

// packageImportRequested reads an import's request and its upload,
// answering for one that cannot be.
func (s *Server) packageImportRequested(w http.ResponseWriter, r *http.Request) (*packageImportRequest, archive.ImportOptions, string, bool) {
	var req packageImportRequest
	if CurrentUser(r.Context()) == nil {
		writeJSONError(w, http.StatusForbidden, "An upload is a person's own: import as one")
		return nil, archive.ImportOptions{}, "", false
	}
	if !decodeJSON(w, r, &req) {
		return nil, archive.ImportOptions{}, "", false
	}
	for _, k := range req.Kinds {
		if k == archive.KindAuth {
			writeJSONError(w, http.StatusBadRequest, "Authentication is never imported through the web")
			return nil, archive.ImportOptions{}, "", false
		}
		if !kindKnown(k) {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a kind of information", k))
			return nil, archive.ImportOptions{}, "", false
		}
	}
	u, code, msg := s.stagedUpload(r, req.UploadID)
	if u == nil {
		writeJSONError(w, code, msg)
		return nil, archive.ImportOptions{}, "", false
	}
	opts := archive.ImportOptions{Anchor: req.Anchor, Choices: req.Choices, Kinds: req.Kinds, May: readerOf(r), Web: true, Icons: s.icons, Fingerprint: req.Fingerprint}
	return &req, opts, u.ID, true
}

// handleCheckPackageImport checks an uploaded package or backup and says
// what importing it would do, as a job: POST /api/v1/import/check.
func (s *Server) handleCheckPackageImport(w http.ResponseWriter, r *http.Request) {
	_, opts, upload, ok := s.packageImportRequested(w, r)
	if !ok {
		return
	}
	job := s.backupJobs.start("import-check", CurrentUser(r.Context()).ID, func() (any, error) {
		plan, err := archive.CheckImport(context.Background(), s.store, s.openUpload(upload), opts)
		return packageImportResult{Plan: plan}, err
	})
	writeJSON(w, http.StatusAccepted, job)
}

// handlePackageImport imports an uploaded package or backup as its check
// said, as a job: POST /api/v1/import, with the check's fingerprint. One
// import or replace runs at a time.
func (s *Server) handlePackageImport(w http.ResponseWriter, r *http.Request) {
	req, opts, upload, ok := s.packageImportRequested(w, r)
	if !ok {
		return
	}
	if req.Fingerprint == "" {
		writeJSONError(w, http.StatusBadRequest, "Check the import first, and import what the check said")
		return
	}
	if !s.backupJobs.replacing.TryLock() {
		writeJSONError(w, http.StatusConflict, "Another import or replace is under way; wait for it to finish")
		return
	}
	acting := s.actingStore(r)
	job := s.backupJobs.start("import", CurrentUser(r.Context()).ID, func() (any, error) {
		defer s.backupJobs.replacing.Unlock()
		plan, err := archive.Import(context.Background(), acting, s.blobs, s.openUpload(upload), opts)
		if err == nil {
			// The icons are built again from what is there now.
			select {
			case s.iconsDirty <- struct{}{}:
			default:
			}
		}
		return packageImportResult{Plan: plan}, err
	})
	writeJSON(w, http.StatusAccepted, job)
}
