package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// maxTemplateBytes bounds a template document; MaxElements keeps real ones far
// smaller.
const maxTemplateBytes = 1 << 20

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.store.ListTemplates()
	if err != nil {
		writeServerError(w, err, "list templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.PathValue("id"))
	if errors.Is(err, store.ErrTemplateNotFound) {
		writeJSONError(w, http.StatusNotFound, "Template not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "query template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": t})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := readTemplate(w, r)
	if !ok {
		return
	}
	created, err := s.store.CreateTemplate(t)
	if errors.Is(err, store.ErrTemplateExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if writeTemplateError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": created})
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := readTemplate(w, r)
	if !ok {
		return
	}
	if t.ID != r.PathValue("id") {
		writeJSONError(w, http.StatusBadRequest, "Template id in the body must match the URL; ids cannot be changed")
		return
	}
	updated, err := s.store.UpdateTemplate(t)
	if errors.Is(err, store.ErrTemplateNotFound) {
		writeJSONError(w, http.StatusNotFound, "Template not found")
		return
	}
	if writeTemplateError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": updated})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteTemplate(r.PathValue("id"))
	if errors.Is(err, store.ErrTemplateNotFound) {
		writeJSONError(w, http.StatusNotFound, "Template not found")
		return
	}
	if err != nil {
		writeStoreError(w, err, "delete the template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Template deleted successfully"})
}

// readTemplate parses and validates a template document from the request
// body, writing the error response itself when that fails.
func readTemplate(w http.ResponseWriter, r *http.Request) (*labels.Template, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTemplateBytes))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Template documents are limited to %d bytes", maxTemplateBytes))
		return nil, false
	}
	t, err := labels.Parse(body)
	if writeTemplateError(w, err) {
		return nil, false
	}
	return t, true
}

// writeTemplateError writes the response for err, if any, and reports whether
// it did. Validation errors become 400 with a per-property list of problems.
func writeTemplateError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var ve *labels.ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Template is invalid", Details: ve.Problems})
		return true
	}
	writeStoreError(w, err, "save the template")
	return true
}
