package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
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
	resp := map[string]any{"templates": templates}
	// Those who may see printers are told which suit each template, as
	// loaded now.
	if HasPermission(r.Context(), rbac.PermLabelsPrint) || HasPermission(r.Context(), rbac.PermSettingsManage) || HasPermission(r.Context(), rbac.PermTemplatesManage) {
		list, err := s.store.ListPrinters()
		if err != nil {
			writeServerError(w, err, "list printers")
			return
		}
		suits := map[string][]SpoolChoice{}
		for i := range templates {
			t := &templates[i]
			suits[t.ID] = []SpoolChoice{}
			for j := range list {
				if _, _, err := planFor(&list[j], t); err == nil {
					suits[t.ID] = append(suits[t.ID], SpoolChoice{ID: list[j].ID, Name: list[j].Name})
				}
			}
		}
		resp["suits"] = suits
	}
	writeJSON(w, http.StatusOK, resp)
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

// handleCreateTemplate stores a new template under an ID the server assigns;
// an id in the body is ignored.
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := readTemplate(w, r, "")
	if !ok {
		return
	}
	created, err := s.store.CreateTemplate(t)
	if writeTemplateError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": created})
}

// handleUpdateTemplate replaces a template. The id in the body may be left
// out, but cannot differ from the URL's.
func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := readTemplate(w, r, r.PathValue("id"))
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
	if errors.Is(err, store.ErrTemplateInUse) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "delete the template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Template deleted successfully"})
}

// readTemplate parses and validates a template document from the request
// body, writing the error response itself when that fails. id is the ID of
// the template being replaced, or empty for a new one (see withTemplateID).
func readTemplate(w http.ResponseWriter, r *http.Request, id string) (*labels.Template, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTemplateBytes))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Template documents are limited to %d bytes", maxTemplateBytes))
		return nil, false
	}
	t, err := labels.Parse(withTemplateID(body, id))
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

// withTemplateID gives a template document the id the request is about: id
// when the document has none, and none at all when id is empty. A document
// that is not a JSON object is returned as it is, for Parse to describe.
func withTemplateID(doc []byte, id string) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return doc
	}
	if id == "" {
		delete(obj, "id")
	} else if raw, ok := obj["id"]; !ok || string(raw) == `""` || string(raw) == "null" {
		obj["id"], _ = json.Marshal(id)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return doc
	}
	return out
}
