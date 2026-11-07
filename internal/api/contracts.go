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

// Contracts (docs/contracts.md): warranties, service and maintenance
// agreements, leases and licenses, each with a vendor. Reading one follows
// its vendor's scopes: whoever sees the vendor sees its contracts.
// vendors.manage in the vendor's scopes creates, changes and deletes them.
// Their cost, its currency and how often it is paid need purchases.read
// there too. Of the assets a contract covers, each requester sees those
// they may read; those they may not stay covered when they change it.

// ContractRequest is a contract as created or replaced: every property, so
// an absent one is emptied, but for asset_ids and category_ids, which stay
// as they are when absent.
type ContractRequest struct {
	VendorID    string    `json:"vendor_id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Reference   string    `json:"reference"`
	StartDate   string    `json:"start_date"`
	EndDate     string    `json:"end_date"`
	AutoRenews  bool      `json:"auto_renews"`
	NoticeDays  int       `json:"notice_days"`
	Cost        string    `json:"cost"`
	Currency    string    `json:"currency"`
	Billing     string    `json:"billing"`
	Notes       string    `json:"notes"`
	AssetIDs    *[]string `json:"asset_ids"`
	CategoryIDs *[]string `json:"category_ids"`
}

// seesCosts reports whether the request may see the cost of a contract
// with a vendor in these scopes: purchases.read in one of them, or
// in every scope for a vendor in none, which everyone who reads assets
// sees, the public role among them.
func seesCosts(ctx context.Context, scopes []string) bool {
	if len(scopes) == 0 {
		return Allows(ctx, rbac.PermPurchasesRead, nil)
	}
	return Allows(ctx, rbac.PermPurchasesRead, scopes)
}

// contractFor reads the contract a request names, answering 404 when the
// requester may not see its vendor, and 403 when manage is set and they
// may not manage it. It returns its vendor's scopes' names.
func (s *Server) contractFor(w http.ResponseWriter, r *http.Request, id string, manage bool) (*models.Contract, []string, bool) {
	c, err := s.store.GetContract(id)
	if errors.Is(err, store.ErrContractNotFound) {
		writeJSONError(w, http.StatusNotFound, "Contract not found")
		return nil, nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the contract")
		return nil, nil, false
	}
	seen, scopes, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "read the contract's vendor")
		return nil, nil, false
	}
	if _, ok := seen[c.VendorID]; !ok {
		writeJSONError(w, http.StatusNotFound, "Contract not found")
		return nil, nil, false
	}
	if manage && !Allows(r.Context(), rbac.PermVendorsManage, scopes[c.VendorID]) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for this contract's vendor's scopes")
		return nil, nil, false
	}
	return c, scopes[c.VendorID], true
}

// shownContracts are contracts as the request may see them: without the
// assets it may not read, and without their costs where it may not see
// them. scopes are each vendor's scopes' names, by ID. A failure to read
// the assets' scopes hides every asset.
func (s *Server) shownContracts(ctx context.Context, cs []models.Contract, scopes map[string][]string) []models.Contract {
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.AssetIDs...)
	}
	assetScopes, err := s.store.AssetScopes(ids)
	if err != nil {
		assetScopes = map[string][]string{}
	}
	out := make([]models.Contract, len(cs))
	for i, c := range cs {
		if !seesCosts(ctx, scopes[c.VendorID]) {
			c.HideCosts()
		}
		kept, named := []string{}, []models.NamedThing{}
		for _, a := range c.Assets {
			if sc, ok := assetScopes[a.ID]; ok && Allows(ctx, rbac.PermAssetsRead, sc) {
				kept, named = append(kept, a.ID), append(named, a)
			}
		}
		c.AssetIDs, c.Assets = kept, named
		out[i] = c
	}
	return out
}

// redactContracts leaves out of each asset the contracts covering it whose
// vendor the request may not see, and their costs where it may not see
// them. A failure to read the vendors hides every contract.
func (s *Server) redactContracts(ctx context.Context, assets []models.AssetWithDetails) {
	var (
		seen   map[string]models.Vendor
		scopes map[string][]string
	)
	for i := range assets {
		a := &assets[i]
		if len(a.Contracts) == 0 {
			continue
		}
		if seen == nil {
			var err error
			if seen, scopes, err = s.visibleVendors(ctx); err != nil {
				seen = map[string]models.Vendor{}
			}
		}
		kept := make([]models.ContractCover, 0, len(a.Contracts))
		for _, c := range a.Contracts {
			if _, ok := seen[c.VendorID]; !ok {
				continue
			}
			if !seesCosts(ctx, scopes[c.VendorID]) {
				c.HideCosts()
			}
			kept = append(kept, c)
		}
		a.Contracts = kept
	}
}

// contractInput is what a request replaces a contract with, keeping what
// the request may not see as had has it, nil for one being created: its
// cost unless costs says the request sees it (purchases.read), the assets
// it may not read, and what it
// covers when the request leaves that out. When it fails, it answers the
// request.
func (s *Server) contractInput(w http.ResponseWriter, r *http.Request, req ContractRequest, had *models.Contract, costs bool) (store.ContractInput, bool) {
	in := store.ContractInput{VendorID: req.VendorID, Kind: req.Kind, Name: req.Name, Reference: req.Reference, StartDate: req.StartDate,
		EndDate: req.EndDate, AutoRenews: req.AutoRenews, NoticeDays: req.NoticeDays, Cost: req.Cost, Currency: req.Currency,
		Billing: req.Billing, Notes: req.Notes}
	var kept models.Contract
	if had != nil {
		kept = *had
	}
	if !costs {
		in.Cost, in.Currency, in.Billing = kept.Cost, kept.Currency, kept.Billing
	}
	if req.CategoryIDs != nil {
		in.CategoryIDs = *req.CategoryIDs
	} else {
		in.CategoryIDs = kept.CategoryIDs
	}
	if req.AssetIDs == nil {
		in.AssetIDs = kept.AssetIDs
		return in, true
	}
	// Each asset named must be one the request reads; those covered that
	// it does not read stay.
	ids := append([]string{}, *req.AssetIDs...)
	ids = append(ids, kept.AssetIDs...)
	assetScopes, err := s.store.AssetScopes(ids)
	if err != nil {
		writeServerError(w, err, "read the assets' scopes")
		return in, false
	}
	reads := func(id string) bool {
		sc, ok := assetScopes[id]
		return ok && Allows(r.Context(), rbac.PermAssetsRead, sc)
	}
	in.AssetIDs = []string{}
	for _, id := range *req.AssetIDs {
		if !reads(id) {
			writeJSONError(w, http.StatusBadRequest, "asset not found: "+id)
			return in, false
		}
		in.AssetIDs = append(in.AssetIDs, id)
	}
	for _, id := range kept.AssetIDs {
		if !reads(id) {
			in.AssetIDs = append(in.AssetIDs, id)
		}
	}
	return in, true
}

// writeContractError answers a failure to save a contract.
func writeContractError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrContractNotFound):
		writeJSONError(w, http.StatusNotFound, "Contract not found")
	case errors.Is(err, store.ErrVendorNotFound), errors.Is(err, store.ErrAssetNotFound), errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// matchesContract reports whether a contract matches a search: every word
// of it in its name, kind, reference, vendor or notes, regardless of case.
func matchesContract(c models.Contract, q string) bool {
	text := strings.ToLower(strings.Join([]string{c.Name, c.Kind, c.Reference, c.VendorName, c.Notes}, "\n"))
	for _, word := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// handleListContracts lists the contracts the requester may see, by when
// they end, soonest first: GET /api/v1/contracts, with ?vendor_id= those
// with a vendor, ?category_id= those naming a category, ?asset_id= those
// covering an asset the requester reads, and ?q= those matching a search.
func (s *Server) handleListContracts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	all, err := s.store.ListContracts()
	if err != nil {
		writeServerError(w, err, "list contracts")
		return
	}
	seen, scopes, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "list contracts")
		return
	}
	var covering map[string]bool
	if id := q.Get("asset_id"); id != "" {
		a, ok := s.assetFor(w, r, id)
		if !ok {
			return
		}
		covering = map[string]bool{}
		for _, c := range a.Contracts {
			covering[c.ID] = true
		}
	}
	vendor, category, search := q.Get("vendor_id"), q.Get("category_id"), strings.TrimSpace(q.Get("q"))
	kept := []models.Contract{}
	for _, c := range all {
		if _, ok := seen[c.VendorID]; !ok {
			continue
		}
		switch {
		case vendor != "" && c.VendorID != vendor,
			category != "" && !contains(c.CategoryIDs, category),
			covering != nil && !covering[c.ID],
			search != "" && !matchesContract(c, search):
			continue
		}
		kept = append(kept, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"contracts": s.shownContracts(r.Context(), kept, scopes)})
}

// handleGetContract reads a contract, with whether the requester may
// change it: GET /api/v1/contracts/{id}
func (s *Server) handleGetContract(w http.ResponseWriter, r *http.Request) {
	c, scopes, ok := s.contractFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	shown := s.shownContracts(r.Context(), []models.Contract{*c}, map[string][]string{c.VendorID: scopes})[0]
	writeJSON(w, http.StatusOK, map[string]any{"contract": shown, "can_manage": Allows(r.Context(), rbac.PermVendorsManage, scopes)})
}

// contractVendor checks the request may manage contracts with the vendor
// a request names, answering it when it may not. It returns the vendor's
// scopes' names.
func (s *Server) contractVendor(w http.ResponseWriter, r *http.Request, id string) ([]string, bool) {
	if strings.TrimSpace(id) == "" {
		writeJSONError(w, http.StatusBadRequest, "a contract needs a vendor")
		return nil, false
	}
	_, scopes, ok := s.vendorFor(w, r, id, true)
	return scopes, ok
}

// handleCreateContract makes a contract: POST /api/v1/contracts
func (s *Server) handleCreateContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	scopes, ok := s.contractVendor(w, r, req.VendorID)
	if !ok {
		return
	}
	in, ok := s.contractInput(w, r, req, nil, seesCosts(r.Context(), scopes))
	if !ok {
		return
	}
	c, err := s.actingStore(r).CreateContract(in)
	if err != nil {
		writeContractError(w, err, "create the contract")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"contract": s.shownContracts(r.Context(), []models.Contract{*c}, map[string][]string{c.VendorID: scopes})[0]})
}

// handleUpdateContract replaces a contract: PUT /api/v1/contracts/{id}.
// Moving it to another vendor needs vendors.manage for both.
func (s *Server) handleUpdateContract(w http.ResponseWriter, r *http.Request) {
	var req ContractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	had, scopes, ok := s.contractFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	costs := seesCosts(r.Context(), scopes)
	if req.VendorID != had.VendorID {
		if scopes, ok = s.contractVendor(w, r, req.VendorID); !ok {
			return
		}
	}
	in, ok := s.contractInput(w, r, req, had, costs && seesCosts(r.Context(), scopes))
	if !ok {
		return
	}
	c, err := s.actingStore(r).UpdateContract(had.ID, in)
	if err != nil {
		writeContractError(w, err, "update the contract")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract": s.shownContracts(r.Context(), []models.Contract{*c}, map[string][]string{c.VendorID: scopes})[0]})
}

// handleDeleteContract deletes a contract: DELETE /api/v1/contracts/{id}
func (s *Server) handleDeleteContract(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.contractFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	if err := s.actingStore(r).DeleteContract(c.ID); err != nil {
		writeContractError(w, err, "delete the contract")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Contract deleted"})
}

// handleContractHistory lists a contract's history, newest first, a page
// at a time, as a vendor's is: GET /api/v1/contracts/{id}/history?before=
func (s *Server) handleContractHistory(w http.ResponseWriter, r *http.Request) {
	c, scopes, ok := s.contractFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	events, more, err := s.store.ListContractEvents(c.ID, r.URL.Query().Get("before"), historyPage)
	if errors.Is(err, store.ErrEventNotFound) {
		writeJSONError(w, http.StatusBadRequest, "before must name one of this contract's events")
		return
	}
	if err != nil {
		writeServerError(w, err, "list the contract's history")
		return
	}
	track, costs := tracksVendor(r.Context(), scopes), seesCosts(r.Context(), scopes)
	for i := range events {
		events[i] = redactEvent(events[i], track)
		if !costs {
			events[i] = withoutContractCosts(events[i])
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// withoutContractCosts leaves out of a contract's event its changes to
// what only purchases.read sees.
func withoutContractCosts(e models.Event) models.Event {
	if len(e.Payload.Changes) == 0 {
		return e
	}
	kept := make([]models.EventChange, 0, len(e.Payload.Changes))
	for _, c := range e.Payload.Changes {
		if !store.ContractChanges[c.What] {
			kept = append(kept, c)
		}
	}
	e.Payload.Changes = kept
	return e
}
