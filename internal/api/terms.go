package api

import (
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/terms"
)

// Related terms (#386, docs/data-model.md#related-terms): the graph of
// what things are called that search expands through. Anyone who reads
// assets reads it, as search uses it; terms.manage changes it, from the
// web app or a person's own assistant over MCP, which each term and link
// records.

// TermsResponse is every term, by label, every link between them, and,
// for those who manage terms, the imports they came in.
type TermsResponse struct {
	Terms   []models.Term       `json:"terms"`
	Links   []models.TermLink   `json:"links"`
	Imports []models.TermImport `json:"imports"`
}

// ExpandResponse is what a search for some words also looks for, and the
// related terms suggested in its place.
type ExpandResponse struct {
	Search    string            `json:"search"`
	Expansion []terms.Expansion `json:"expansion"`
	Related   []string          `json:"related"`
}

// TermImportRequest is an import: what it is, and its terms, each naming
// the terms broader than it and related to it by label.
type TermImportRequest struct {
	Name  string                 `json:"name"`
	Terms []store.TermImportItem `json:"terms"`
}

// maxTermImportBody bounds an import's request body.
const maxTermImportBody = 32 << 20

// hideTermSources leaves out who added terms and links for those who do
// not manage them, who see only where each came from.
func hideTermSources(r *http.Request, out *TermsResponse) {
	if HasPermission(r.Context(), rbac.PermTermsManage) {
		return
	}
	for i := range out.Terms {
		out.Terms[i].AddedBy, out.Terms[i].AddedByName = "", ""
	}
	for i := range out.Links {
		out.Links[i].AddedBy, out.Links[i].AddedByName = "", ""
	}
	out.Imports = []models.TermImport{}
}

// handleListTerms is GET /api/v1/terms.
func (s *Server) handleListTerms(w http.ResponseWriter, r *http.Request) {
	list, links, err := s.store.ListTerms()
	if err != nil {
		writeServerError(w, err, "list the terms")
		return
	}
	imports, err := s.store.ListTermImports()
	if err != nil {
		writeServerError(w, err, "list the imports")
		return
	}
	out := TermsResponse{Terms: list, Links: links, Imports: imports}
	hideTermSources(r, &out)
	writeJSON(w, http.StatusOK, out)
}

// handleExpandTerms says what a search for q also looks for:
// GET /api/v1/terms/expand?q=…
func (s *Server) handleExpandTerms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	g, err := s.store.TermGraph()
	if err != nil {
		writeServerError(w, err, "read the terms")
		return
	}
	out := ExpandResponse{Search: q, Expansion: g.Expand(q), Related: g.Related(q)}
	if out.Expansion == nil {
		out.Expansion = []terms.Expansion{}
	}
	if out.Related == nil {
		out.Related = []string{}
	}
	writeJSON(w, http.StatusOK, out)
}

// writeTermError answers a change to terms that went wrong.
func writeTermError(w http.ResponseWriter, err error, doing string) {
	var problem *store.TermProblem
	var taken *store.TermLabelTaken
	switch {
	case errors.Is(err, store.ErrTermNotFound):
		writeJSONError(w, http.StatusNotFound, "Term not found")
	case errors.Is(err, store.ErrTermImportNotFound):
		writeJSONError(w, http.StatusNotFound, "Import not found")
	case errors.As(err, &taken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Another term is already called " + taken.Label, "term_id": taken.TermID})
	case errors.As(err, &problem):
		writeJSONError(w, http.StatusBadRequest, capitalize(problem.Message))
	default:
		writeServerError(w, err, doing)
	}
}

// handleCreateTerm is POST /api/v1/terms.
func (s *Server) handleCreateTerm(w http.ResponseWriter, r *http.Request) {
	var in store.TermInput
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := s.actingStore(r).CreateTerm(in)
	if err != nil {
		writeTermError(w, err, "add the term")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"term": t})
}

// handleGetTerm is GET /api/v1/terms/{id}.
func (s *Server) handleGetTerm(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTerm(r.PathValue("id"))
	if err != nil {
		writeTermError(w, err, "read the term")
		return
	}
	if !HasPermission(r.Context(), rbac.PermTermsManage) {
		t.AddedBy, t.AddedByName = "", ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"term": t})
}

// handleUpdateTerm is PUT /api/v1/terms/{id}: its labels and note.
func (s *Server) handleUpdateTerm(w http.ResponseWriter, r *http.Request) {
	var in store.TermInput
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := s.actingStore(r).UpdateTerm(r.PathValue("id"), in)
	if err != nil {
		writeTermError(w, err, "change the term")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"term": t})
}

// handleDeleteTerm is DELETE /api/v1/terms/{id}, with its links.
func (s *Server) handleDeleteTerm(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).DeleteTerm(r.PathValue("id")); err != nil {
		writeTermError(w, err, "remove the term")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Term removed"})
}

// handleLinkTerms is PUT /api/v1/terms/{id}/links/{relation}/{other}: the
// term broader than, narrower than, or related to the other.
func (s *Server) handleLinkTerms(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).LinkTerms(r.PathValue("id"), r.PathValue("relation"), r.PathValue("other")); err != nil {
		writeTermError(w, err, "link the terms")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Terms linked"})
}

// handleUnlinkTerms is DELETE /api/v1/terms/{id}/links/{relation}/{other}.
func (s *Server) handleUnlinkTerms(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).UnlinkTerms(r.PathValue("id"), r.PathValue("relation"), r.PathValue("other")); err != nil {
		writeTermError(w, err, "unlink the terms")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Terms unlinked"})
}

// handleImportTerms is POST /api/v1/terms/imports: terms and links added
// as one import, which can be removed as one.
func (s *Server) handleImportTerms(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTermImportBody)
	var in TermImportRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	imp, err := s.actingStore(r).ImportTerms(in.Name, in.Terms)
	if err != nil {
		writeTermError(w, err, "import the terms")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"import": imp})
}

// handleDeleteTermImport is DELETE /api/v1/terms/imports/{id}: the terms
// and links it added, and links to its terms.
func (s *Server) handleDeleteTermImport(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).DeleteTermImport(r.PathValue("id")); err != nil {
		writeTermError(w, err, "remove the import")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Import removed"})
}
