package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Vendors (docs/vendors.md): the suppliers a space buys from. A vendor's
// scopes decide who sees it: those who may read assets, or manage vendors,
// in one of them, and everyone who reads assets when it has none.
// vendors.manage changes it, in the scopes it is in and those it ends up
// in; a vendor in no scope needs vendors.manage in every scope, since
// everyone sees it.

// VendorRequest is a vendor as created or replaced: every property, so an
// absent one is emptied, but for scope_ids, which stay as they are when
// absent, so that leaving them out never shows a vendor to everyone.
type VendorRequest struct {
	Name          string                 `json:"name"`
	Website       string                 `json:"website"`
	AccountNumber string                 `json:"account_number"`
	Address       string                 `json:"address"`
	Notes         string                 `json:"notes"`
	Contacts      []models.VendorContact `json:"contacts"`
	ScopeIDs      []string               `json:"scope_ids"`
}

func (req VendorRequest) input() store.VendorInput {
	return store.VendorInput{Name: req.Name, Website: req.Website, AccountNumber: req.AccountNumber, Address: req.Address,
		Notes: req.Notes, Contacts: req.Contacts, ScopeIDs: req.ScopeIDs}
}

// vendorScopes names a vendor's scopes, as permissions name them.
func vendorScopes(v *models.Vendor, names map[string]string) []string {
	out := make([]string, 0, len(v.ScopeIDs))
	for _, id := range v.ScopeIDs {
		if n, ok := names[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// seesVendor reports whether the request may see a vendor in these scopes.
func seesVendor(ctx context.Context, scopes []string) bool {
	if len(scopes) == 0 {
		return HasPermission(ctx, rbac.PermAssetsRead)
	}
	return Allows(ctx, rbac.PermAssetsRead, scopes) || Allows(ctx, rbac.PermVendorsManage, scopes)
}

// seesScopes reports whether the request may see which scopes things are
// in: those who manage users, categories or vendors.
func seesScopes(ctx context.Context) bool {
	return HasPermission(ctx, rbac.PermUsersManage) || HasPermission(ctx, rbac.PermCategoriesManage) || HasPermission(ctx, rbac.PermVendorsManage)
}

// shownVendor is a vendor as the requester may see it: its scopes only to
// those who see scopes.
func shownVendor(ctx context.Context, v models.Vendor) models.Vendor {
	if !seesScopes(ctx) {
		v.ScopeIDs = []string{}
	}
	return v
}

// visibleVendors reads every vendor the request may see, by ID, with their
// scopes' names.
func (s *Server) visibleVendors(ctx context.Context) (map[string]models.Vendor, map[string][]string, error) {
	all, err := s.store.ListVendors()
	if err != nil {
		return nil, nil, err
	}
	names, err := s.store.ScopeNames()
	if err != nil {
		return nil, nil, err
	}
	seen, scopes := map[string]models.Vendor{}, map[string][]string{}
	for i := range all {
		sc := vendorScopes(&all[i], names)
		if seesVendor(ctx, sc) {
			seen[all[i].ID], scopes[all[i].ID] = all[i], sc
		}
	}
	return seen, scopes, nil
}

// vendorFor reads the vendor a request names, answering 404 when the
// requester may not see it, and 403 when manage is set and they may not
// change it. It returns its scopes' names.
func (s *Server) vendorFor(w http.ResponseWriter, r *http.Request, id string, manage bool) (*models.Vendor, []string, bool) {
	v, err := s.store.GetVendor(id)
	if errors.Is(err, store.ErrVendorNotFound) {
		writeJSONError(w, http.StatusNotFound, "Vendor not found")
		return nil, nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the vendor")
		return nil, nil, false
	}
	names, err := s.store.ScopeNames()
	if err != nil {
		writeServerError(w, err, "read the vendor's scopes")
		return nil, nil, false
	}
	scopes := vendorScopes(v, names)
	if !seesVendor(r.Context(), scopes) {
		writeJSONError(w, http.StatusNotFound, "Vendor not found")
		return nil, nil, false
	}
	if manage && !Allows(r.Context(), rbac.PermVendorsManage, scopes) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for this vendor's scopes")
		return nil, nil, false
	}
	return v, scopes, true
}

// mayPutVendorIn checks the request may manage a vendor in these scopes,
// by ID, answering the request when it may not, or when one is unknown.
func (s *Server) mayPutVendorIn(w http.ResponseWriter, r *http.Request, ids []string) bool {
	names, err := s.store.ScopeNames()
	if err != nil {
		writeServerError(w, err, "read the scopes")
		return false
	}
	scopes := make([]string, 0, len(ids))
	for _, id := range ids {
		n, ok := names[id]
		if !ok {
			writeJSONError(w, http.StatusBadRequest, store.ErrScopeNotFound.Error()+": "+id)
			return false
		}
		scopes = append(scopes, n)
	}
	if !Allows(r.Context(), rbac.PermVendorsManage, scopes) {
		if len(scopes) == 0 {
			writeJSONError(w, http.StatusForbidden, "Forbidden: a vendor in no scope is seen by everyone, and needs vendors.manage in every scope; put it in a scope you manage vendors in")
		} else {
			writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for those scopes")
		}
		return false
	}
	return true
}

// writeVendorError answers a failure to save a vendor.
func writeVendorError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrVendorNotFound):
		writeJSONError(w, http.StatusNotFound, "Vendor not found")
	case errors.Is(err, store.ErrScopeNotFound):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrScopeReserved):
		writeJSONError(w, http.StatusConflict, "The default scope, where assets in no other scope are, cannot be put on a vendor; leave it in no scope instead")
	case errors.Is(err, store.ErrVendorHasContracts):
		writeJSONError(w, http.StatusConflict, "Contracts are with this vendor; delete them, or move them to another vendor, first")
	default:
		writeStoreError(w, err, action)
	}
}

// matchesVendor reports whether a vendor matches a search: every word of
// it in its name, website, account number, address, notes, or a
// contact's name, role, email or phone, regardless of case.
func matchesVendor(v models.Vendor, q string) bool {
	parts := []string{v.Name, v.Website, v.AccountNumber, v.Address, v.Notes}
	for _, c := range v.Contacts {
		parts = append(parts, c.Name, c.Role, c.Email, c.Phone)
	}
	text := strings.ToLower(strings.Join(parts, "\n"))
	for _, word := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// handleListVendors lists the vendors the requester may see, by name,
// matching ?q= when given: GET /api/v1/vendors
func (s *Server) handleListVendors(w http.ResponseWriter, r *http.Request) {
	all, err := s.store.ListVendors()
	if err != nil {
		writeServerError(w, err, "list vendors")
		return
	}
	names, err := s.store.ScopeNames()
	if err != nil {
		writeServerError(w, err, "list vendors")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []models.Vendor{}
	for i := range all {
		if !seesVendor(r.Context(), vendorScopes(&all[i], names)) || (q != "" && !matchesVendor(all[i], q)) {
			continue
		}
		out = append(out, shownVendor(r.Context(), all[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendors": out})
}

// handleGetVendor reads a vendor, with whether the requester may change it:
// GET /api/v1/vendors/{id}
func (s *Server) handleGetVendor(w http.ResponseWriter, r *http.Request) {
	v, scopes, ok := s.vendorFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendor": shownVendor(r.Context(), *v), "can_manage": Allows(r.Context(), rbac.PermVendorsManage, scopes)})
}

// handleCreateVendor makes a vendor: POST /api/v1/vendors
func (s *Server) handleCreateVendor(w http.ResponseWriter, r *http.Request) {
	var req VendorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !s.mayPutVendorIn(w, r, req.ScopeIDs) {
		return
	}
	v, err := s.actingStore(r).CreateVendor(req.input())
	if err != nil {
		writeVendorError(w, err, "create the vendor")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"vendor": shownVendor(r.Context(), *v)})
}

// handleUpdateVendor replaces a vendor's details, contacts and scopes:
// PUT /api/v1/vendors/{id}
func (s *Server) handleUpdateVendor(w http.ResponseWriter, r *http.Request) {
	var req VendorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	v, _, ok := s.vendorFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	// Those who do not see scopes leave them as they are.
	if req.ScopeIDs == nil || !seesScopes(r.Context()) {
		req.ScopeIDs = v.ScopeIDs
	}
	if !s.mayPutVendorIn(w, r, req.ScopeIDs) {
		return
	}
	updated, err := s.actingStore(r).UpdateVendor(v.ID, req.input())
	if err != nil {
		writeVendorError(w, err, "update the vendor")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendor": shownVendor(r.Context(), *updated)})
}

// handleDeleteVendor deletes a vendor: DELETE /api/v1/vendors/{id}
func (s *Server) handleDeleteVendor(w http.ResponseWriter, r *http.Request) {
	v, _, ok := s.vendorFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	if err := s.actingStore(r).DeleteVendor(v.ID); err != nil {
		writeVendorError(w, err, "delete the vendor")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Vendor deleted"})
}

// handleVendorHistory lists a vendor's history, newest first, a page at a
// time, as an asset's is: GET /api/v1/vendors/{id}/history?before=
func (s *Server) handleVendorHistory(w http.ResponseWriter, r *http.Request) {
	v, scopes, ok := s.vendorFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	events, more, err := s.store.ListVendorEvents(v.ID, r.URL.Query().Get("before"), historyPage)
	if errors.Is(err, store.ErrEventNotFound) {
		writeJSONError(w, http.StatusBadRequest, "before must name one of this vendor's events")
		return
	}
	if err != nil {
		writeServerError(w, err, "list the vendor's history")
		return
	}
	track := tracksVendor(r.Context(), scopes)
	for i := range events {
		events[i] = redactEvent(events[i], track)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// tracksVendor reports whether the request may see who changed a vendor in
// these scopes: those who track assets in one of them, or anywhere when it
// has none.
func tracksVendor(ctx context.Context, scopes []string) bool {
	if len(scopes) == 0 {
		return HasPermission(ctx, rbac.PermAssetsTrack)
	}
	return Allows(ctx, rbac.PermAssetsTrack, scopes)
}

// CategoryVendorsRequest names the vendors a category is bought from, in
// order of preference.
type CategoryVendorsRequest struct {
	VendorIDs []string `json:"vendor_ids"`
}

// handleSetCategoryVendors replaces the vendors a category is bought from:
// PUT /api/v1/categories/{id}/vendors. Each must be one the requester sees.
// Those they do not see stay where they were, after them.
func (s *Server) handleSetCategoryVendors(w http.ResponseWriter, r *http.Request) {
	var req CategoryVendorsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cat, err := s.store.GetCategoryByID(r.PathValue("id"))
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the category")
		return
	}
	seen, _, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "read the vendors")
		return
	}
	had, err := s.store.CategoryVendorIDs(cat.ID)
	if err != nil {
		writeServerError(w, err, "read the category's vendors")
		return
	}
	ids := []string{}
	for _, id := range req.VendorIDs {
		if _, ok := seen[id]; !ok {
			writeJSONError(w, http.StatusBadRequest, "vendor not found: "+id)
			return
		}
		ids = append(ids, id)
	}
	for _, id := range had {
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
		}
	}
	cat, err = s.actingStore(r).SetCategoryVendors(cat.ID, ids)
	if err != nil {
		writeVendorError(w, err, "set the category's vendors")
		return
	}
	shown, err := s.shownCategories(r.Context(), []models.Category{*cat})
	if err != nil {
		writeServerError(w, err, "read the vendors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": shown[0]})
}

// shownCategories are categories as the requester may see them: naming
// only the vendors they see.
func (s *Server) shownCategories(ctx context.Context, cats []models.Category) ([]models.Category, error) {
	if err := s.store.WithCategoryVendors(cats); err != nil {
		return nil, err
	}
	named := false
	for _, c := range cats {
		named = named || len(c.VendorIDs) > 0
	}
	if !named {
		return cats, nil
	}
	seen, _, err := s.visibleVendors(ctx)
	if err != nil {
		return nil, err
	}
	for i := range cats {
		ids := []string{}
		for _, id := range cats[i].VendorIDs {
			if _, ok := seen[id]; ok {
				ids = append(ids, id)
			}
		}
		cats[i].VendorIDs = ids
	}
	return cats, nil
}
