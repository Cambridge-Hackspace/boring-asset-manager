package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// importBodyLimit allows for a full-size file plus the multipart framing
// and the mapping.
const importBodyLimit = assetcsv.MaxBytes + 1<<20

// readImport reads a multipart import request: the CSV in "file" and, if
// given, a JSON array of column targets in "mapping". It writes the error
// response itself when it fails.
func readImport(w http.ResponseWriter, r *http.Request) (data []byte, mapping []string, opts assetcsv.Options, ok bool) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Files are limited to %d bytes", assetcsv.MaxBytes))
			return nil, nil, opts, false
		}
		writeJSONError(w, http.StatusBadRequest, "Send the CSV as multipart/form-data in a field named file")
		return nil, nil, opts, false
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Send the CSV as multipart/form-data in a field named file")
		return nil, nil, opts, false
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, assetcsv.MaxBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Could not read the file")
		return nil, nil, opts, false
	}
	if len(data) > assetcsv.MaxBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Files are limited to %d bytes", assetcsv.MaxBytes))
		return nil, nil, opts, false
	}
	if raw := r.FormValue("mapping"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &mapping); err != nil || mapping == nil {
			writeJSONError(w, http.StatusBadRequest, "mapping must be a JSON array with one target per column")
			return nil, nil, opts, false
		}
	}
	switch r.FormValue("remove_links") {
	case "", "false":
	case "true":
		opts.RemoveLinks = true
	default:
		writeJSONError(w, http.StatusBadRequest, "remove_links must be true or false")
		return nil, nil, opts, false
	}
	return data, mapping, opts, true
}

// readableSource is the store as an import by this request sees it: assets
// it cannot read are not there (docs/access.md).
type readableSource struct {
	*store.Store
	s *Server
	r *http.Request
}

func (rs readableSource) ListAssets(categoryID, search string) ([]models.AssetWithDetails, error) {
	assets, err := rs.Store.ListAssets(categoryID, search)
	if err != nil {
		return nil, err
	}
	return rs.s.readable(rs.r, assets)
}

func (rs readableSource) GetAssetByID(id string) (*models.AssetWithDetails, error) {
	a, err := rs.Store.GetAssetByID(id)
	if err != nil {
		return nil, err
	}
	if !Allows(rs.r.Context(), rbac.PermAssetsRead, a.Scopes) {
		return nil, store.ErrAssetNotFound
	}
	return rs.s.shown(rs.r, a), nil
}

// prepareImport plans an import, writing the error response if the file
// cannot be planned at all. The importer may change only assets in scopes
// they can write in.
func (s *Server) prepareImport(w http.ResponseWriter, r *http.Request, data []byte, mapping []string, opts assetcsv.Options) (*assetcsv.Plan, bool) {
	scopesOf, err := s.store.ScopeResolver()
	if err != nil {
		writeServerError(w, err, "read the categories' scopes")
		return nil, false
	}
	opts.ScopesOf = scopesOf
	opts.May = func(scopes []string) bool { return Allows(r.Context(), rbac.PermAssetsWrite, scopes) }
	if opts.Icons, err = s.store.BuiltIconNames(); err != nil {
		writeServerError(w, err, "read the icon build")
		return nil, false
	}
	plan, err := assetcsv.PrepareWith(readableSource{s.store, s, r}, data, mapping, opts)
	if errors.Is(err, assetcsv.ErrUnreadable) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "check the import")
		return nil, false
	}
	return plan, true
}

// handleImportPreview checks an import without writing anything:
// POST /api/v1/assets/import/preview
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	data, mapping, opts, ok := readImport(w, r)
	if !ok {
		return
	}
	if plan, ok := s.prepareImport(w, r, data, mapping, opts); ok {
		writeJSON(w, http.StatusOK, plan)
	}
}

// handleImport checks an import again and applies it in one transaction:
// POST /api/v1/assets/import
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	data, mapping, opts, ok := readImport(w, r)
	if !ok {
		return
	}
	if mapping == nil {
		writeJSONError(w, http.StatusBadRequest, "mapping is required: confirm where each column goes")
		return
	}
	plan, ok := s.prepareImport(w, r, data, mapping, opts)
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
	created, err := s.actingStore(r).ImportAssets(plan.Changes, plan.Links)
	if errors.Is(err, store.ErrGeneratorCodeExists) || errors.Is(err, store.ErrAssetNotFound) || errors.Is(err, store.ErrImportOutdated) {
		// Something changed since the check; nothing was imported.
		writeJSONError(w, http.StatusConflict, "Assets changed while importing, and nothing was imported: "+err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "import the assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created":       plan.Create,
		"updated":       plan.Update,
		"unchanged":     plan.Unchanged,
		"links_added":   plan.LinksAdded,
		"links_moved":   plan.LinksMoved,
		"links_removed": plan.LinksRemoved,
		"created_ids":   created,
	})
}
