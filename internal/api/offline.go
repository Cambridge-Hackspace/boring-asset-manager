package api

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Using BAM! offline on a phone (#214, docs/installing.md#offline): the
// shopping list of what is running low, the copy of what a person can see
// that the installed app keeps, and the instance's settings for it.

// The offline copy's settings: whether installed apps keep a copy at all,
// how often an open app refreshes it, and whether people may keep their
// assets' thumbnails in it.
const (
	OfflineEnabledSetting    = "offline.enabled"
	OfflineRefreshSetting    = "offline.refresh_minutes"
	OfflineThumbnailsSetting = "offline.thumbnails"
)

// Defaults and limits on how often the copy is refreshed, in minutes: an
// hour, and from five minutes to a week.
const (
	defaultOfflineRefresh = 60
	minOfflineRefresh     = 5
	maxOfflineRefresh     = 7 * 24 * 60
)

// OfflineSettings are the instance's settings for the offline copy.
type OfflineSettings struct {
	Enabled        bool `json:"enabled"`
	RefreshMinutes int  `json:"refresh_minutes"`
	Thumbnails     bool `json:"thumbnails"`
}

// offlineSettings reads them, with the defaults for those unset: copies
// kept, refreshed hourly, thumbnails allowed.
func (s *Server) offlineSettings() (OfflineSettings, error) {
	o := OfflineSettings{Enabled: true, RefreshMinutes: defaultOfflineRefresh, Thumbnails: true}
	enabled, err := s.store.Setting(OfflineEnabledSetting, "true")
	if err != nil {
		return o, err
	}
	refresh, err := s.store.Setting(OfflineRefreshSetting, strconv.Itoa(defaultOfflineRefresh))
	if err != nil {
		return o, err
	}
	thumbs, err := s.store.Setting(OfflineThumbnailsSetting, "true")
	if err != nil {
		return o, err
	}
	o.Enabled, o.Thumbnails = enabled != "false", thumbs != "false"
	if n, err := strconv.Atoi(refresh); err == nil && n >= minOfflineRefresh && n <= maxOfflineRefresh {
		o.RefreshMinutes = n
	}
	return o, nil
}

// handleOfflineSettings reads them: GET /api/v1/settings/offline, for
// anyone who reads assets, whose app follows them.
func (s *Server) handleOfflineSettings(w http.ResponseWriter, r *http.Request) {
	o, err := s.offlineSettings()
	if err != nil {
		writeServerError(w, err, "read the offline settings")
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// handleSetOfflineSettings sets them: PUT /api/v1/settings/offline.
func (s *Server) handleSetOfflineSettings(w http.ResponseWriter, r *http.Request) {
	var req OfflineSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshMinutes < minOfflineRefresh || req.RefreshMinutes > maxOfflineRefresh {
		writeJSONError(w, http.StatusBadRequest, "refresh_minutes is from 5 to 10080, a week")
		return
	}
	for key, value := range map[string]string{
		OfflineEnabledSetting:    strconv.FormatBool(req.Enabled),
		OfflineRefreshSetting:    strconv.Itoa(req.RefreshMinutes),
		OfflineThumbnailsSetting: strconv.FormatBool(req.Thumbnails),
	} {
		if err := s.store.SetSetting(key, value); err != nil {
			writeServerError(w, err, "save the offline settings")
			return
		}
	}
	s.handleOfflineSettings(w, r)
}

// ShoppingList is what is running low, of what the requester can see, to
// buy: each thing with where it is and its preferred catalog entry.
type ShoppingList struct {
	// MadeAt is when it was read, by the server's clock: a restock checked
	// off it is refused if the thing was restocked since.
	MadeAt time.Time      `json:"made_at"`
	Items  []ShoppingItem `json:"items"`
	// CanOrder is whether the requester may start purchase orders from it
	// (vendors.manage in some scope).
	CanOrder bool `json:"can_order"`
}

// ShoppingItem is one thing running low: an asset counted by quantity
// (AssetID), or a consumable category (CategoryID), counted by its assets
// or weighed in its unit.
type ShoppingItem struct {
	AssetID    string `json:"asset_id,omitempty"`
	CategoryID string `json:"category_id,omitempty"`
	Name       string `json:"name"`
	// Group is the category it is listed under: an asset's first, a
	// category itself, as a path from the top ("Stock › Fasteners");
	// empty for an asset in none.
	Group string `json:"group"`
	// Level is an asset's quantity, or a category's count of assets, or
	// total in its unit, over those the requester can see.
	Level     int    `json:"level"`
	Threshold *int   `json:"threshold"`
	Unit      string `json:"unit,omitempty"`
	// Where an asset is, outermost first, as far as the requester can see.
	Where string `json:"where,omitempty"`
	// CanRestock is whether checking it off records a restock: an asset,
	// with stock.record in its scopes.
	CanRestock bool `json:"can_restock"`
	// Vendor is its preferred catalog entry, if the requester sees one.
	Vendor *ShoppingVendor `json:"vendor,omitempty"`
	// OnOrder is the open purchase order it is already on, if any.
	OnOrder *OnOrder `json:"on_order,omitempty"`
}

// ShoppingVendor is where a thing is bought: its preferred catalog entry,
// with its price only for those who may see the vendor's prices.
type ShoppingVendor struct {
	VendorID    string `json:"vendor_id"`
	VendorName  string `json:"vendor_name"`
	PartNumber  string `json:"part_number,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	PackSize    *int   `json:"pack_size,omitempty"`
	PackUnit    string `json:"pack_unit,omitempty"`
	Price       string `json:"price,omitempty"`
	Currency    string `json:"currency,omitempty"`
}

// handleShoppingList: GET /api/v1/shopping.
func (s *Server) handleShoppingList(w http.ResponseWriter, r *http.Request) {
	list, err := s.shoppingList(r, nil)
	if err != nil {
		writeServerError(w, err, "make the shopping list")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// categoryPaths names each category by its path from the top.
func categoryPaths(cats []models.Category) map[string]string {
	byID := make(map[string]models.Category, len(cats))
	for _, c := range cats {
		byID[c.ID] = c
	}
	paths := make(map[string]string, len(cats))
	var path func(id string, depth int) string
	path = func(id string, depth int) string {
		if p, ok := paths[id]; ok {
			return p
		}
		c, ok := byID[id]
		if !ok {
			return ""
		}
		p := c.Name
		// Deep enough for any tree; a loop, which the store refuses, stops.
		if c.ParentID != nil && depth < 64 {
			if above := path(*c.ParentID, depth+1); above != "" {
				p = above + " › " + c.Name
			}
		}
		paths[id] = p
		return p
	}
	for _, c := range cats {
		path(c.ID, 0)
	}
	return paths
}

// whereOf is where an asset is, outermost first, as the request sees it.
func whereOf(a *models.AssetWithDetails) string {
	names := make([]string, len(a.Location))
	for i, l := range a.Location {
		names[i] = l.Name
	}
	return strings.Join(names, " › ")
}

// firstGroup is the path of an asset's first category, by path.
func firstGroup(a *models.AssetWithDetails, paths map[string]string) string {
	group := ""
	for _, c := range a.Categories {
		if p := paths[c.ID]; p != "" && (group == "" || p < group) {
			group = p
		}
	}
	return group
}

// shoppingList makes the list for the request. readable is the assets it
// may read, as readable leaves them, when the caller has them already;
// nil reads them.
func (s *Server) shoppingList(r *http.Request, readable []models.AssetWithDetails) (*ShoppingList, error) {
	ctx := r.Context()
	list := &ShoppingList{MadeAt: time.Now().UTC(), Items: []ShoppingItem{}, CanOrder: HasPermission(ctx, rbac.PermVendorsManage)}
	if readable == nil {
		all, err := s.store.FilterAssets(store.AssetFilter{})
		if err == nil {
			readable, err = s.readable(r, all)
		}
		if err != nil {
			return nil, err
		}
	}
	cats, err := s.store.ListCategories()
	if err != nil {
		return nil, err
	}
	paths := categoryPaths(cats)

	// Where each thing is bought, and the open orders it is on.
	preferred, err := s.store.ListCatalogEntries(store.CatalogFilter{Preferred: true})
	if err != nil {
		return nil, err
	}
	view, err := s.catalogView(ctx, preferred)
	if err != nil {
		return nil, err
	}
	vendorOf := map[string]*ShoppingVendor{}
	for _, e := range preferred {
		shown, ok := view.shown(ctx, e)
		if !ok {
			continue
		}
		vendorOf[e.CategoryID+"/"+e.AssetID] = &ShoppingVendor{VendorID: shown.VendorID, VendorName: shown.VendorName, PartNumber: shown.PartNumber,
			Description: shown.Description, URL: shown.URL, PackSize: shown.PackSize, PackUnit: shown.PackUnit, Price: shown.Price, Currency: shown.Currency}
	}
	orders, err := s.store.ListPurchaseOrders()
	if err != nil {
		return nil, err
	}
	onOrder := map[string]*OnOrder{}
	// Newest first, so the oldest open order is kept.
	for _, o := range orders {
		if _, ok := view.vendors[o.VendorID]; !ok || !models.OrderOpen(o.State) {
			continue
		}
		for _, l := range o.Lines {
			on := &OnOrder{ID: o.ID, Reference: o.Reference, State: o.State}
			onOrder[l.CategoryID+"/"+l.AssetID] = on
		}
	}

	for i := range readable {
		a := &readable[i]
		st := a.Asset.Stock
		if a.Asset.ArchivedAt != nil || st == nil || !st.IsRunningLow {
			continue
		}
		key := "/" + a.Asset.ID
		list.Items = append(list.Items, ShoppingItem{AssetID: a.Asset.ID, Name: a.Asset.Name, Group: firstGroup(a, paths), Level: st.Quantity,
			Threshold: st.LowStockThreshold, Unit: st.Unit, Where: whereOf(a), CanRestock: Allows(ctx, rbac.PermStockRecord, a.Scopes),
			Vendor: vendorOf[key], OnOrder: onOrder[key]})
	}
	scopesOf, err := s.store.ScopeResolver()
	if err != nil {
		return nil, err
	}
	for i := range cats {
		c := &cats[i]
		if !c.IsConsumable || !c.IsRunningLow || !Allows(ctx, rbac.PermAssetsRead, scopesOf([]string{c.ID})) {
			continue
		}
		count, total, err := s.categoryLevel(r, c)
		if err != nil {
			return nil, err
		}
		level := count
		if c.Unit != "" {
			level = total
		}
		key := c.ID + "/"
		list.Items = append(list.Items, ShoppingItem{CategoryID: c.ID, Name: c.Name, Group: paths[c.ID], Level: level, Threshold: c.LowStockThreshold,
			Unit: c.Unit, Vendor: vendorOf[key], OnOrder: onOrder[key]})
	}
	sort.SliceStable(list.Items, func(i, j int) bool {
		a, b := list.Items[i], list.Items[j]
		if a.Group != b.Group {
			// Those in no category last.
			if a.Group == "" || b.Group == "" {
				return b.Group == ""
			}
			return a.Group < b.Group
		}
		// A category before the assets in it, then by name.
		if (a.CategoryID != "") != (b.CategoryID != "") {
			return a.CategoryID != ""
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return list, nil
}

// OfflineCopy is what the installed app keeps of what a person can see, to
// look things up and shop with no signal (docs/installing.md#offline).
type OfflineCopy struct {
	// MadeAt is when it was made, by the server's clock: what it shows is
	// as it was then, and a restock recorded from it is refused if the
	// thing was restocked since.
	MadeAt time.Time `json:"made_at"`
	// UserID is whose it is: the app keeps it only for them.
	UserID     string            `json:"user_id"`
	Settings   OfflineSettings   `json:"settings"`
	Categories []OfflineCategory `json:"categories"`
	Assets     []OfflineAsset    `json:"assets"`
	Shopping   *ShoppingList     `json:"shopping"`
}

// OfflineCategory is a category an asset in the copy is in, named by its
// path.
type OfflineCategory struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// OfflineAsset is an asset as the copy keeps it: enough to find it by its
// code, name, category or place, and to see how many there are, and
// little else, so a few thousand fit in a few megabytes.
type OfflineAsset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Codes are the codes on its labels, which scanning looks up.
	Codes      []string      `json:"codes,omitempty"`
	Categories []string      `json:"categories,omitempty"`
	Where      string        `json:"where,omitempty"`
	Stock      *OfflineStock `json:"stock,omitempty"`
	// Condition is left out when it is ok.
	Condition string       `json:"condition,omitempty"`
	Loan      *OfflineLoan `json:"loan,omitempty"`
	// Review is its review state, when its categories use them.
	Review string `json:"review,omitempty"`
	// Photo is its first photo, whose thumbnail the app may keep: only
	// when the instance allows thumbnails.
	Photo string `json:"photo,omitempty"`
	// Record is whether the person may use and restock it: stock.record in
	// its scopes.
	Record bool `json:"record,omitempty"`
}

// OfflineStock is an asset's stock, counted by quantity.
type OfflineStock struct {
	Quantity  int    `json:"quantity"`
	Unit      string `json:"unit,omitempty"`
	Threshold *int   `json:"threshold,omitempty"`
	Low       bool   `json:"low,omitempty"`
}

// OfflineLoan is an asset's open loan: who has it only for those who track
// it, or the borrower themselves, as everywhere else.
type OfflineLoan struct {
	Borrower string     `json:"borrower,omitempty"`
	Due      *time.Time `json:"due,omitempty"`
}

// handleOfflineCopy makes the requester's copy: GET /api/v1/offline. It
// holds only what they can read now, as readable leaves it, so a copy
// made after they lose access to something no longer has it; one made
// after they lose assets.read, or while copies are off, is refused, and
// the app clears what it kept.
func (s *Server) handleOfflineCopy(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusForbidden, "An offline copy is a person's: sign in")
		return
	}
	settings, err := s.offlineSettings()
	if err != nil {
		writeServerError(w, err, "read the offline settings")
		return
	}
	if !settings.Enabled {
		writeJSONError(w, http.StatusForbidden, "Offline copies are turned off on this instance")
		return
	}
	kept, err := s.offlineCopy(r, user.ID, settings)
	if err != nil {
		writeServerError(w, err, "make the offline copy")
		return
	}
	writeJSON(w, http.StatusOK, kept)
}

func (s *Server) offlineCopy(r *http.Request, userID string, settings OfflineSettings) (*OfflineCopy, error) {
	ctx := r.Context()
	madeAt := time.Now().UTC()
	all, err := s.store.FilterAssets(store.AssetFilter{})
	if err != nil {
		return nil, err
	}
	readable, err := s.readable(r, all)
	if err != nil {
		return nil, err
	}
	cats, err := s.store.ListCategories()
	if err != nil {
		return nil, err
	}
	paths := categoryPaths(cats)
	out := &OfflineCopy{MadeAt: madeAt, UserID: userID, Settings: settings, Categories: []OfflineCategory{}, Assets: make([]OfflineAsset, 0, len(readable))}
	used := map[string]bool{}
	for i := range readable {
		a := offlineAsset(&readable[i], settings.Thumbnails, Allows(ctx, rbac.PermStockRecord, readable[i].Scopes))
		for _, id := range a.Categories {
			used[id] = true
		}
		out.Assets = append(out.Assets, a)
	}
	for _, c := range cats {
		if used[c.ID] {
			out.Categories = append(out.Categories, OfflineCategory{ID: c.ID, Path: paths[c.ID]})
		}
	}
	if out.Shopping, err = s.shoppingList(r, readable); err != nil {
		return nil, err
	}
	out.Shopping.MadeAt = madeAt
	return out, nil
}

// offlineAsset is an asset, as readable left it, as the copy keeps it.
func offlineAsset(a *models.AssetWithDetails, thumbnails, record bool) OfflineAsset {
	o := OfflineAsset{ID: a.Asset.ID, Name: a.Asset.Name, Where: whereOf(a), Record: record && a.Asset.Stock != nil}
	for _, k := range a.GeneratorKeys {
		if k.AssignedCode != "" && !slices.Contains(o.Codes, k.AssignedCode) {
			o.Codes = append(o.Codes, k.AssignedCode)
		}
	}
	for _, c := range a.Categories {
		o.Categories = append(o.Categories, c.ID)
	}
	if st := a.Asset.Stock; st != nil {
		o.Stock = &OfflineStock{Quantity: st.Quantity, Unit: st.Unit, Threshold: st.LowStockThreshold, Low: st.IsRunningLow}
	}
	if a.Asset.Condition != "" && a.Asset.Condition != models.ConditionOK {
		o.Condition = a.Asset.Condition
	}
	if l := a.Loan; l != nil {
		o.Loan = &OfflineLoan{Borrower: l.Borrower.Name, Due: l.DueAt}
	}
	if a.Review != nil {
		o.Review = a.Review.State
	}
	if thumbnails && len(a.Photos) > 0 {
		o.Photo = a.Photos[0].ID
	}
	return o
}
