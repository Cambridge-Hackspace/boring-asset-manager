package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// importBodyLimit allows for a full-size file plus the multipart framing
// and the mapping.
const importBodyLimit = assetcsv.MaxBytes + 1<<20

// readImport reads a multipart import request: the CSV in "file" and, if
// given, a JSON array of column targets in "mapping". It writes the error
// response itself when it fails.
func readImport(w http.ResponseWriter, r *http.Request) (data []byte, mapping []string, ok bool) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Files are limited to %d bytes", assetcsv.MaxBytes))
			return nil, nil, false
		}
		writeJSONError(w, http.StatusBadRequest, "Send the CSV as multipart/form-data in a field named file")
		return nil, nil, false
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Send the CSV as multipart/form-data in a field named file")
		return nil, nil, false
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, assetcsv.MaxBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Could not read the file")
		return nil, nil, false
	}
	if len(data) > assetcsv.MaxBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Files are limited to %d bytes", assetcsv.MaxBytes))
		return nil, nil, false
	}
	if raw := r.FormValue("mapping"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &mapping); err != nil || mapping == nil {
			writeJSONError(w, http.StatusBadRequest, "mapping must be a JSON array with one target per column")
			return nil, nil, false
		}
	}
	return data, mapping, true
}

// prepareImport plans an import, writing the error response if the file
// cannot be planned at all.
func (s *Server) prepareImport(w http.ResponseWriter, data []byte, mapping []string) (*assetcsv.Plan, bool) {
	plan, err := assetcsv.Prepare(s.store, data, mapping)
	if errors.Is(err, assetcsv.ErrUnreadable) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check the import: %v", err))
		return nil, false
	}
	return plan, true
}

// handleImportPreview checks an import without writing anything:
// POST /api/v1/assets/import/preview
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	data, mapping, ok := readImport(w, r)
	if !ok {
		return
	}
	if plan, ok := s.prepareImport(w, data, mapping); ok {
		writeJSON(w, http.StatusOK, plan)
	}
}

// handleImport checks an import again and applies it in one transaction:
// POST /api/v1/assets/import
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	data, mapping, ok := readImport(w, r)
	if !ok {
		return
	}
	if mapping == nil {
		writeJSONError(w, http.StatusBadRequest, "mapping is required: confirm where each column goes")
		return
	}
	plan, ok := s.prepareImport(w, data, mapping)
	if !ok {
		return
	}
	if plan.ProblemCount > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(ErrorResponse{
			Error:   fmt.Sprintf("The file has %d problems; nothing was imported", plan.ProblemCount),
			Details: plan,
		})
		return
	}
	created, err := s.actingStore(r).ImportAssets(plan.Changes)
	if errors.Is(err, store.ErrGeneratorCodeExists) || errors.Is(err, store.ErrAssetNotFound) {
		// Something changed since the check; nothing was imported.
		writeJSONError(w, http.StatusConflict, "Assets changed while importing, and nothing was imported: "+err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to import: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created":     plan.Create,
		"updated":     plan.Update,
		"unchanged":   plan.Unchanged,
		"created_ids": created,
	})
}
