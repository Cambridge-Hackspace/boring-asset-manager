package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateAssetRequest defines the payload for registering a new asset.
type CreateAssetRequest struct {
	Name          string                     `json:"name"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
	// Stock counts the asset's stock by quantity; absent or null does not.
	Stock *StockRequest `json:"stock,omitempty"`
	// IconName is its own icon (docs/icons.md), one the build draws; absent or
	// empty, it is drawn with its categories'.
	IconName string `json:"icon_name,omitempty"`
}

// UpdateAssetRequest defines the payload for modifying an asset. Omitted parts
// are left as they are: an absent name, category_ids or generator_keys keeps
// the current value, while an empty list clears it. field_values only changes
// the keys it names; an empty value removes one. An absent stock keeps the
// asset's, null stops counting it by quantity, and an object replaces it.
// An absent icon_name keeps the asset's own icon; null or "" draws it with
// its categories', and a name gives it that one. It is the icon for
// icon_rendition, the screen's when absent.
type UpdateAssetRequest struct {
	Name          string                     `json:"name,omitempty"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
	Stock         json.RawMessage            `json:"stock,omitempty"`
	IconName      json.RawMessage            `json:"icon_name,omitempty"`
	IconRendition string                     `json:"icon_rendition,omitempty"`
}

// StockRequest is an asset's stock (docs/stock-and-lending.md). An absent quantity keeps
// the asset's current one, so an edit cannot undo a use made meanwhile; an
// asset whose stock was not counted starts at 0.
type StockRequest struct {
	Quantity          *int   `json:"quantity"`
	Unit              string `json:"unit"`
	LowStockThreshold *int   `json:"low_stock_threshold"`
}

func (r *StockRequest) input() *store.StockInput {
	if r == nil {
		return nil
	}
	return &store.StockInput{Quantity: r.Quantity, Unit: r.Unit, LowStockThreshold: r.LowStockThreshold}
}

// stockUpdate reads an update's stock: nil when absent.
func (r UpdateAssetRequest) stockUpdate() (*store.StockUpdate, error) {
	if len(r.Stock) == 0 {
		return nil, nil
	}
	var req *StockRequest
	if err := json.Unmarshal(r.Stock, &req); err != nil {
		return nil, err
	}
	return &store.StockUpdate{Stock: req.input()}, nil
}

// iconUpdate reads an update's icon: nil when absent.
func (r UpdateAssetRequest) iconUpdate() (*store.IconUpdate, error) {
	if len(r.IconName) == 0 {
		return nil, nil
	}
	var name *string
	if err := json.Unmarshal(r.IconName, &name); err != nil {
		return nil, err
	}
	if name == nil {
		return &store.IconUpdate{Rendition: r.IconRendition}, nil
	}
	return &store.IconUpdate{Name: *name, Rendition: r.IconRendition}, nil
}

// QuantityRequest changes an asset's quantity: by adjust (negative to use
// some, positive to restock) or to set. Exactly one is given.
type QuantityRequest struct {
	Adjust *int `json:"adjust"`
	Set    *int `json:"set"`
}

// historyPage is how many events one page of
// GET /api/v1/assets/{id}/history holds.
const historyPage = 50

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	assets, ok := s.filterAssets(w, r, r.URL.Query())
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"assets": assets,
	})
}

// filterAssets lists the assets a query selects, as GET /api/v1/assets
// takes it, keeping those the request may read. On a bad query or a
// failure it answers the request itself.
func (s *Server) filterAssets(w http.ResponseWriter, r *http.Request, q url.Values) ([]models.AssetWithDetails, bool) {
	// The search is matched once the assets are as the request may see
	// them, so where they are counts only as far as it may read.
	search := q.Get("search")
	f := store.AssetFilter{CategoryID: q.Get("category_id"), Condition: q.Get("condition"), Loan: q.Get("loan"), Icon: q.Get("icon")}
	switch q.Get("conflict") {
	case "", "scan_template", "label_template":
	default:
		writeJSONError(w, http.StatusBadRequest, "conflict must be scan_template or label_template")
		return nil, false
	}
	switch q.Get("archived") {
	case "", "false":
	case "true":
		f.Archived = true
	default:
		writeJSONError(w, http.StatusBadRequest, "archived must be true or false")
		return nil, false
	}
	assets, err := s.store.FilterAssets(f)
	if errors.Is(err, store.ErrInvalidCondition) {
		writeJSONError(w, http.StatusBadRequest, "condition must be ok, needs_maintenance, out_of_service or problem")
		return nil, false
	}
	if errors.Is(err, store.ErrInvalidLoanFilter) {
		writeJSONError(w, http.StatusBadRequest, "loan must be open or overdue")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "list assets")
		return nil, false
	}
	if assets, err = s.readable(r, assets); err != nil {
		writeServerError(w, err, "read the assets' links")
		return nil, false
	}
	assets = searched(assets, search)
	// Those whose scan template (docs/scanning.md) or label template (docs/printing.md) was
	// chosen by a tie.
	if conflict := q.Get("conflict"); conflict != "" {
		kept := assets[:0]
		for _, a := range assets {
			if conflict == "scan_template" && a.ScanTemplate.Conflict || conflict == "label_template" && a.LabelTemplate.Conflict {
				kept = append(kept, a)
			}
		}
		assets = kept
	}
	return assets, true
}

// searched keeps the assets a search matches by name, code, or where they
// are, as the request may see them (readable has hidden the places it
// cannot read). An empty search keeps them all.
func searched(assets []models.AssetWithDetails, search string) []models.AssetWithDetails {
	if search == "" {
		return assets
	}
	kept := assets[:0]
	for i := range assets {
		if store.MatchesSearch(&assets[i], search) {
			kept = append(kept, assets[i])
		}
	}
	return kept
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	asset, ok := s.assetFor(w, r, id)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"asset": s.shown(r, asset),
	})
}

func (s *Server) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var req CreateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !s.mayPlace(w, r, rbac.PermAssetsWrite, req.CategoryIDs) {
		return
	}
	asset, err := s.actingStore(r).CreateAssetWithIcon(req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues, req.Stock.input(), req.IconName)
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "create the asset")
		return
	}
	asset = s.withIconParams(r, asset)

	writeJSON(w, http.StatusCreated, map[string]any{
		"asset": s.shown(r, asset),
	})
}

func (s *Server) handleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	var req UpdateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	stock, err := req.stockUpdate()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid stock")
		return
	}
	icon, err := req.iconUpdate()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "icon_name is an icon's name, or null")
		return
	}

	if _, ok := s.assetFor(w, r, id, rbac.PermAssetsWrite); !ok {
		return
	}
	// Moving it to other categories needs the permission in their scopes too.
	if req.CategoryIDs != nil && !s.mayPlace(w, r, rbac.PermAssetsWrite, req.CategoryIDs) {
		return
	}
	asset, err := s.actingStore(r).UpdateAssetWithIcon(id, req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues, stock, icon)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "update the asset")
		return
	}
	asset = s.withIconParams(r, asset)

	writeJSON(w, http.StatusOK, map[string]any{
		"asset": s.shown(r, asset),
	})
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	if _, ok := s.assetFor(w, r, id, rbac.PermAssetsDelete); !ok {
		return
	}
	err := s.actingStore(r).DeleteAsset(id)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if errors.Is(err, store.ErrAssetOnLoan) || errors.Is(err, store.ErrContainerNotEmpty) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "archive the asset")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Asset deleted successfully",
	})
}

// handleExportAssets writes the assets the list would show as CSV
// (docs/csv.md): GET /api/v1/assets/export.csv?category_id=&search=
func (s *Server) handleExportAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.ListAssets(r.URL.Query().Get("category_id"), "")
	if err != nil {
		writeServerError(w, err, "list assets")
		return
	}
	if assets, err = s.readable(r, assets); err != nil {
		writeServerError(w, err, "read the assets' links")
		return
	}
	assets = searched(assets, r.URL.Query().Get("search"))
	cats, err := s.store.ListAllCategories()
	if err != nil {
		writeServerError(w, err, "list categories")
		return
	}
	// Links name other assets as an import would find them: among all
	// active assets the requester can read, not only those exported.
	active, err := s.store.ListAssets("", "")
	if err == nil {
		active, err = s.readable(r, active)
	}
	if err != nil {
		writeServerError(w, err, "list assets")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="assets-%s.csv"`, time.Now().UTC().Format("20060102")))
	if err := assetcsv.Export(w, assets, assetcsv.NewCategories(cats), generators.AllowedGeneratorTypes(), assetcsv.NewAssetRefs(active)); err != nil {
		// Headers are gone; all that is left is to stop.
		return
	}
}

// handleAdjustQuantity uses up or restocks an asset counted by quantity:
// POST /api/v1/assets/{id}/quantity
func (s *Server) handleAdjustQuantity(w http.ResponseWriter, r *http.Request) {
	var req QuantityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Adjust == nil) == (req.Set == nil) {
		writeJSONError(w, http.StatusBadRequest, "Give either adjust or set, as a whole number")
		return
	}
	delta := 0
	if req.Adjust != nil {
		delta = *req.Adjust
	}
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermStockRecord); !ok {
		return
	}
	asset, err := s.actingStore(r).AdjustQuantity(r.PathValue("id"), delta, req.Set)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrNotEnoughStock):
		writeJSONError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeStoreError(w, err, "change the quantity")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
	}
}

// handleSetAssetRunningLow flags or clears an asset counted by quantity by
// hand: PUT /api/v1/assets/{id}/running-low
func (s *Server) handleSetAssetRunningLow(w http.ResponseWriter, r *http.Request) {
	var req RunningLowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RunningLow == nil {
		writeJSONError(w, http.StatusBadRequest, "running_low (true or false) is required")
		return
	}
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermStockRecord); !ok {
		return
	}
	asset, changed, err := s.actingStore(r).SetAssetRunningLow(r.PathValue("id"), *req.RunningLow)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case err != nil:
		writeStoreError(w, err, "update the asset")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset), "changed": changed})
	}
}

// handleAssetHistory lists an asset's events a page at a time, newest first:
// GET /api/v1/assets/{id}/history[?before=<event id>]. People show by name,
// and only with assets.track.
func (s *Server) handleAssetHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, ok := s.assetFor(w, r, id)
	if !ok {
		return
	}
	events, more, err := s.store.ListAssetEvents(id, r.URL.Query().Get("before"), historyPage)
	if errors.Is(err, store.ErrEventNotFound) {
		writeJSONError(w, http.StatusBadRequest, "before must name one of this asset's events")
		return
	}
	if err != nil {
		writeServerError(w, err, "list the asset's history")
		return
	}
	track := Allows(r.Context(), rbac.PermAssetsTrack, a.Scopes)
	for i := range events {
		events[i] = redactEvent(events[i], track)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// handleRestoreAsset brings an archived asset back, with the codes archiving
// released that no other asset has taken since:
// POST /api/v1/assets/{id}/restore
func (s *Server) handleRestoreAsset(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsDelete); !ok {
		return
	}
	asset, skipped, err := s.actingStore(r).RestoreAsset(r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrAssetNotArchived):
		writeJSONError(w, http.StatusConflict, "The asset is not archived")
	case errors.Is(err, store.ErrGeneratorCodeExists):
		writeJSONError(w, http.StatusConflict, "Another asset took one of its codes at the same moment; try again")
	case err != nil:
		writeStoreError(w, err, "restore the asset")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset), "skipped_codes": skipped})
	}
}

// ConditionRequest sets an asset's condition, with a note saying what is
// wrong.
type ConditionRequest struct {
	Condition string `json:"condition"`
	Note      string `json:"note"`
}

// handleSetAssetCondition reports a problem with an asset, or marks it
// working again: PUT /api/v1/assets/{id}/condition. Reporting needs
// assets.report or assets.write; marking an asset ok needs assets.write.
func (s *Server) handleSetAssetCondition(w http.ResponseWriter, r *http.Request) {
	var req ConditionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !models.ValidCondition(req.Condition) {
		writeJSONError(w, http.StatusBadRequest, "condition must be ok, needs_maintenance or out_of_service")
		return
	}
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsReport, rbac.PermAssetsWrite)
	if !ok {
		return
	}
	if req.Condition == models.ConditionOK && !Allows(r.Context(), rbac.PermAssetsWrite, a.Scopes) {
		writeJSONError(w, http.StatusForbidden, "Marking an asset working again needs assets.write")
		return
	}
	asset, changed, err := s.actingStore(r).SetAssetCondition(r.PathValue("id"), req.Condition, req.Note)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case err != nil:
		writeStoreError(w, err, "set the condition")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset), "changed": changed})
	}
}
