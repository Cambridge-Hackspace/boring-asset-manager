package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Catalogs (docs/vendors.md#catalogs): what each vendor sells. An entry is
// seen by those who see its vendor and what it is linked to: any category,
// or an asset they may read. Its price, currency and when it was checked
// need purchases.read in the vendor's scopes too, anywhere for a vendor in
// none. Changing it takes vendors.manage for its vendor, as changing the
// vendor does; those without purchases.read leave its price as it is.

// CatalogEntryRequest is an entry as created or replaced: every property,
// so an absent one is emptied.
type CatalogEntryRequest struct {
	VendorID     string `json:"vendor_id"`
	CategoryID   string `json:"category_id"`
	AssetID      string `json:"asset_id"`
	PartNumber   string `json:"part_number"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	PackSize     *int   `json:"pack_size"`
	PackUnit     string `json:"pack_unit"`
	Price        string `json:"price"`
	Currency     string `json:"currency"`
	PriceChecked string `json:"price_checked"`
	Preferred    bool   `json:"preferred"`
}

func (req CatalogEntryRequest) input() store.CatalogEntryInput {
	return store.CatalogEntryInput{VendorID: req.VendorID, CategoryID: req.CategoryID, AssetID: req.AssetID, PartNumber: req.PartNumber,
		Description: req.Description, URL: req.URL, PackSize: req.PackSize, PackUnit: req.PackUnit, Price: req.Price, Currency: req.Currency,
		PriceChecked: req.PriceChecked, Preferred: req.Preferred}
}

// seesVendorPrices reports whether the request may see the prices of a
// vendor in these scopes: purchases.read in one of them, or anywhere for
// one in none, as seeing the vendor is decided.
func seesVendorPrices(ctx context.Context, scopes []string) bool {
	if len(scopes) == 0 {
		return HasPermission(ctx, rbac.PermPurchasesRead)
	}
	return Allows(ctx, rbac.PermPurchasesRead, scopes)
}

// catalogView decides which entries a request sees, and how.
type catalogView struct {
	vendors     map[string]models.Vendor
	vendorScope map[string][]string
	assetScope  map[string][]string
}

// catalogView reads what deciding about entries needs: the vendors the
// request sees, and the scopes of the assets the entries name.
func (s *Server) catalogView(ctx context.Context, entries []models.CatalogEntry) (*catalogView, error) {
	seen, scopes, err := s.visibleVendors(ctx)
	if err != nil {
		return nil, err
	}
	var assets []string
	for _, e := range entries {
		if e.AssetID != "" {
			assets = append(assets, e.AssetID)
		}
	}
	assetScope, err := s.store.AssetScopes(assets)
	if err != nil {
		return nil, err
	}
	return &catalogView{vendors: seen, vendorScope: scopes, assetScope: assetScope}, nil
}

// shown is an entry as the request may see it, or false when it may not.
func (v *catalogView) shown(ctx context.Context, e models.CatalogEntry) (models.CatalogEntry, bool) {
	if _, ok := v.vendors[e.VendorID]; !ok {
		return e, false
	}
	if e.AssetID != "" {
		scopes, ok := v.assetScope[e.AssetID]
		if !ok || !Allows(ctx, rbac.PermAssetsRead, scopes) {
			return e, false
		}
	}
	scopes := v.vendorScope[e.VendorID]
	if !seesVendorPrices(ctx, scopes) {
		e.HidePrices()
	}
	e.CanManage = Allows(ctx, rbac.PermVendorsManage, scopes)
	return e, true
}

// shownCatalog is the entries the request sees, as it sees them.
func (s *Server) shownCatalog(ctx context.Context, entries []models.CatalogEntry) ([]models.CatalogEntry, error) {
	view, err := s.catalogView(ctx, entries)
	if err != nil {
		return nil, err
	}
	out := []models.CatalogEntry{}
	for _, e := range entries {
		if shown, ok := view.shown(ctx, e); ok {
			out = append(out, shown)
		}
	}
	return out, nil
}

// handleListCatalog lists the entries the request sees, by what they are
// for, then vendor: GET /api/v1/catalog, narrowed by vendor_id, category_id
// or asset_id, and to those preferred with preferred=true.
func (s *Server) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.CatalogFilter{VendorID: q.Get("vendor_id"), CategoryID: q.Get("category_id"), AssetID: q.Get("asset_id")}
	switch q.Get("preferred") {
	case "", "false":
	case "true":
		f.Preferred = true
	default:
		writeJSONError(w, http.StatusBadRequest, "preferred must be true or false")
		return
	}
	entries, err := s.store.ListCatalogEntries(f)
	if err == nil {
		entries, err = s.shownCatalog(r.Context(), entries)
	}
	if err != nil {
		writeServerError(w, err, "list the catalog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// catalogEntryFor reads the entry a request names, answering 404 when it
// may not see it.
func (s *Server) catalogEntryFor(w http.ResponseWriter, r *http.Request) (*models.CatalogEntry, bool) {
	e, err := s.store.GetCatalogEntry(r.PathValue("id"))
	if errors.Is(err, store.ErrCatalogEntryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Catalog entry not found")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the catalog entry")
		return nil, false
	}
	shown, err := s.shownCatalog(r.Context(), []models.CatalogEntry{*e})
	if err != nil {
		writeServerError(w, err, "read the catalog entry")
		return nil, false
	}
	if len(shown) == 0 {
		writeJSONError(w, http.StatusNotFound, "Catalog entry not found")
		return nil, false
	}
	return &shown[0], true
}

// handleGetCatalogEntry reads an entry: GET /api/v1/catalog/{id}
func (s *Server) handleGetCatalogEntry(w http.ResponseWriter, r *http.Request) {
	if e, ok := s.catalogEntryFor(w, r); ok {
		writeJSON(w, http.StatusOK, map[string]any{"entry": e})
	}
}

// catalogInput is what a request makes or replaces an entry with, keeping
// the price of had, the entry as stored or nil, where the request may not
// see prices. The vendor must be one it may manage, and an asset one it
// may read. When it fails, it answers the request.
func (s *Server) catalogInput(w http.ResponseWriter, r *http.Request, req CatalogEntryRequest, had *models.CatalogEntry) (store.CatalogEntryInput, bool) {
	in := req.input()
	if in.VendorID == "" {
		writeJSONError(w, http.StatusBadRequest, "vendor_id is required")
		return in, false
	}
	_, scopes, ok := s.vendorFor(w, r, in.VendorID, true)
	if !ok {
		return in, false
	}
	if !seesVendorPrices(r.Context(), scopes) {
		in.Price, in.Currency, in.PriceChecked = "", "", ""
		if had != nil {
			in.Price, in.Currency, in.PriceChecked = had.Price, had.Currency, had.PriceChecked
		}
	}
	if in.AssetID != "" && (had == nil || had.AssetID != in.AssetID) {
		found, err := s.store.AssetScopes([]string{in.AssetID})
		if err != nil {
			writeServerError(w, err, "read the asset")
			return in, false
		}
		if sc, ok := found[in.AssetID]; !ok || !Allows(r.Context(), rbac.PermAssetsRead, sc) {
			writeJSONError(w, http.StatusBadRequest, "asset not found: "+in.AssetID)
			return in, false
		}
	}
	return in, true
}

// writeCatalogError answers a failure to save an entry.
func writeCatalogError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrCatalogEntryNotFound):
		writeJSONError(w, http.StatusNotFound, "Catalog entry not found")
	case errors.Is(err, store.ErrInvalidCatalogEntry), errors.Is(err, store.ErrVendorNotFound), errors.Is(err, store.ErrCategoryNotFound),
		errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// answerCatalogEntry answers an entry saved, as the request sees it.
func (s *Server) answerCatalogEntry(w http.ResponseWriter, r *http.Request, status int, e *models.CatalogEntry) {
	shown, err := s.shownCatalog(r.Context(), []models.CatalogEntry{*e})
	if err != nil || len(shown) == 0 {
		writeServerError(w, fmt.Errorf("showing the entry saved: %v", err), "read the catalog entry")
		return
	}
	writeJSON(w, status, map[string]any{"entry": shown[0]})
}

// handleCreateCatalogEntry makes an entry: POST /api/v1/catalog
func (s *Server) handleCreateCatalogEntry(w http.ResponseWriter, r *http.Request) {
	var req CatalogEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	in, ok := s.catalogInput(w, r, req, nil)
	if !ok {
		return
	}
	e, err := s.actingStore(r).CreateCatalogEntry(in)
	if err != nil {
		writeCatalogError(w, err, "create the catalog entry")
		return
	}
	s.answerCatalogEntry(w, r, http.StatusCreated, e)
}

// handleUpdateCatalogEntry replaces an entry: PUT /api/v1/catalog/{id}. It
// may move to another vendor the requester manages.
func (s *Server) handleUpdateCatalogEntry(w http.ResponseWriter, r *http.Request) {
	var req CatalogEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	had, ok := s.catalogEntryFor(w, r)
	if !ok {
		return
	}
	if _, _, ok := s.vendorFor(w, r, had.VendorID, true); !ok {
		return
	}
	// what the requester may not see, as stored
	stored, err := s.store.GetCatalogEntry(had.ID)
	if err != nil {
		writeCatalogError(w, err, "read the catalog entry")
		return
	}
	in, ok := s.catalogInput(w, r, req, stored)
	if !ok {
		return
	}
	e, err := s.actingStore(r).UpdateCatalogEntry(had.ID, in)
	if err != nil {
		writeCatalogError(w, err, "update the catalog entry")
		return
	}
	s.answerCatalogEntry(w, r, http.StatusOK, e)
}

// handleDeleteCatalogEntry deletes an entry: DELETE /api/v1/catalog/{id}
func (s *Server) handleDeleteCatalogEntry(w http.ResponseWriter, r *http.Request) {
	e, ok := s.catalogEntryFor(w, r)
	if !ok {
		return
	}
	if _, _, ok := s.vendorFor(w, r, e.VendorID, true); !ok {
		return
	}
	if err := s.actingStore(r).DeleteCatalogEntry(e.ID); err != nil {
		writeCatalogError(w, err, "delete the catalog entry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Catalog entry deleted"})
}

// catalogCSV is what a vendor's catalog as CSV is read and written with:
// its entries the request sees, the categories, and the assets it may
// read, by name or code.
func (s *Server) catalogCSV(w http.ResponseWriter, r *http.Request, vendorID string) ([]models.CatalogEntry, *assetcsv.Categories, *assetcsv.AssetRefs, bool) {
	entries, err := s.store.ListCatalogEntries(store.CatalogFilter{VendorID: vendorID})
	if err == nil {
		entries, err = s.shownCatalog(r.Context(), entries)
	}
	if err != nil {
		writeServerError(w, err, "read the catalog")
		return nil, nil, nil, false
	}
	cats, err := s.store.ListAllCategories()
	if err != nil {
		writeServerError(w, err, "list categories")
		return nil, nil, nil, false
	}
	active, err := s.store.ListAssets("", "")
	if err == nil {
		active, err = s.readable(r, active)
	}
	if err != nil {
		writeServerError(w, err, "list assets")
		return nil, nil, nil, false
	}
	return entries, assetcsv.NewCategories(cats), assetcsv.NewAssetRefs(active), true
}

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// handleExportCatalog writes a vendor's catalog as CSV:
// GET /api/v1/vendors/{id}/catalog.csv
func (s *Server) handleExportCatalog(w http.ResponseWriter, r *http.Request) {
	v, _, ok := s.vendorFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	entries, cats, refs, ok := s.catalogCSV(w, r, v.ID)
	if !ok {
		return
	}
	name := unsafeFilename.ReplaceAllString(v.Name, "-")
	if name == "" || name == "-" {
		name = "vendor"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-catalog.csv"`, name))
	_ = assetcsv.ExportCatalog(w, entries, cats, refs)
}

// handleImportCatalog makes and replaces a vendor's entries from CSV, or
// an Excel workbook's sheet, all or none: POST /api/v1/vendors/{id}/catalog.csv.
// With check=true it only says what it would do.
func (s *Server) handleImportCatalog(w http.ResponseWriter, r *http.Request) {
	v, scopes, ok := s.vendorFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	data, sheets, _, _, ok := readImport(w, r)
	if !ok {
		return
	}
	if sheets != nil {
		writeJSON(w, http.StatusOK, map[string]any{"sheets": sheets})
		return
	}
	entries, cats, refs, ok := s.catalogCSV(w, r, v.ID)
	if !ok {
		return
	}
	byID := map[string]models.CatalogEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	stored, err := s.store.ListCatalogEntries(store.CatalogFilter{VendorID: v.ID})
	if err != nil {
		writeServerError(w, err, "read the catalog")
		return
	}
	// the prices as stored, which an importer who may not see them keeps
	for _, e := range stored {
		if _, ok := byID[e.ID]; ok {
			byID[e.ID] = e
		}
	}
	plan, err := assetcsv.PlanCatalog(data, assetcsv.CatalogImport{VendorID: v.ID, Entries: byID, Categories: cats, Assets: refs,
		Prices: seesVendorPrices(r.Context(), scopes)})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.FormValue("check") == "true" {
		writeJSON(w, http.StatusOK, plan)
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
	if len(plan.Writes) > 0 {
		if _, err := s.actingStore(r).WriteCatalogEntries(plan.Writes); err != nil {
			if errors.Is(err, store.ErrCatalogEntryNotFound) || errors.Is(err, store.ErrCategoryNotFound) || errors.Is(err, store.ErrAssetNotFound) {
				writeJSONError(w, http.StatusConflict, "The catalog changed while importing, and nothing was imported: "+err.Error())
				return
			}
			writeCatalogError(w, err, "import the catalog")
			return
		}
	}
	writeJSON(w, http.StatusOK, plan)
}
