package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/webui"
)

// Version is the version of this build: a release's tag, such as v1.2.0, or
// what git describe says of the commit it was built from. Release builds set
// it with -ldflags "-X" (tools/release/build.sh); other builds say "dev".
var Version = "dev"

// Server wraps the HTTP server and dependencies.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	images     *filestore.Images
	photos     *filestore.Images
	transport  printers.Transport
	spool      *spooler
	presses    *pressLimiter
	live       *liveHub
	sample     sampleCache
	httpServer *http.Server
}

// NewServer initializes a new Server with route registration and middleware chaining.
func NewServer(cfg *config.Config, st *store.Store) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:       cfg,
		store:     st,
		images:    filestore.NewImages(cfg.Storage.LocalPath),
		photos:    filestore.NewPhotos(cfg.Storage.LocalPath),
		transport: printers.NewDispatcher(cfg.Printers.Devices),
		spool:     newSpooler(),
		presses:   newPressLimiter(),
		live:      newLiveHub(),
	}

	// Unauthenticated public endpoints
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /api/v1/info", s.handleInfo)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Signed-out visitors hold the public role's permissions on the routes
	// wrapped in OpenToPublic only: one asset's page, never the catalog.

	// Authenticated session endpoints
	mux.Handle("POST /api/v1/auth/logout", RequireAuth(http.HandlerFunc(s.handleLogout)))
	mux.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	mux.Handle("PUT /api/v1/auth/name", RequireAuth(http.HandlerFunc(s.handleSetOwnName)))
	mux.Handle("PUT /api/v1/auth/password", RequireAuth(http.HandlerFunc(s.handleChangePassword)))

	// User administration endpoints (requires users.manage)
	mux.Handle("GET /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("PATCH /api/v1/users/{id}/status", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleUpdateUserStatus)))
	mux.Handle("DELETE /api/v1/users/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteUser)))
	mux.Handle("PUT /api/v1/users/{id}/name", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserName)))
	mux.Handle("PUT /api/v1/users/{id}/password", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserPassword)))
	mux.Handle("PUT /api/v1/users/{id}/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserRoles)))
	mux.Handle("GET /api/v1/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleListRoles)))
	mux.Handle("POST /api/v1/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleCreateRole)))
	mux.Handle("PUT /api/v1/roles/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleUpdateRole)))
	mux.Handle("DELETE /api/v1/roles/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteRole)))

	// Scopes (README §3.3): who may see and change assets is users.manage's
	// to decide; category managers read them to see what labels are there.
	seeScopes := RequireAnyPermission(rbac.PermUsersManage, rbac.PermCategoriesManage)
	mux.Handle("GET /api/v1/scopes", seeScopes(http.HandlerFunc(s.handleListScopes)))
	mux.Handle("POST /api/v1/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	mux.Handle("PUT /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	mux.Handle("DELETE /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteScope)))
	mux.Handle("GET /api/v1/categories/{id}/scopes", seeScopes(http.HandlerFunc(s.handleCategoryScopes)))
	mux.Handle("PUT /api/v1/categories/{id}/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetCategoryScopes)))

	// API tokens: managed only from a signed-in session, never by a token.
	manageTokens := func(h http.HandlerFunc) http.Handler { return RequireAuth(RequirePermission(rbac.PermUsersManage)(h)) }
	mux.Handle("GET /api/v1/tokens", manageTokens(s.handleListTokens))
	mux.Handle("POST /api/v1/tokens", manageTokens(s.handleCreateToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", manageTokens(s.handleRevokeToken))
	mux.Handle("GET /api/v1/tokens/connections", manageTokens(s.handleLiveConnections))

	// Field Corpus endpoints
	mux.Handle("GET /api/v1/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListFields)))
	mux.Handle("POST /api/v1/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateField)))
	mux.Handle("GET /api/v1/fields/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetField)))
	mux.Handle("PUT /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateField)))
	mux.Handle("DELETE /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteField)))

	// Category Hierarchy & Field Inheritance endpoints
	mux.Handle("GET /api/v1/categories", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListCategories)))
	mux.Handle("POST /api/v1/categories", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateCategory)))
	mux.Handle("GET /api/v1/categories/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategory)))
	mux.Handle("PUT /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateCategory)))
	mux.Handle("DELETE /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteCategory)))
	mux.Handle("PUT /api/v1/categories/{id}/running-low", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleSetRunningLow)))
	mux.Handle("GET /api/v1/categories/{id}/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategoryEffectiveFields)))
	mux.Handle("POST /api/v1/categories/effective-fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleEffectiveFieldsForAsset)))
	mux.Handle("POST /api/v1/categories/{id}/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleAssignCategoryField)))
	mux.Handle("DELETE /api/v1/categories/{id}/fields/{field_id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleRemoveCategoryField)))
	mux.Handle("GET /api/v1/categories/{id}/relation-types", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryRelationTypes)))
	mux.Handle("PUT /api/v1/categories/{id}/relation-types", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryRelationTypes)))
	mux.Handle("PUT /api/v1/categories/{id}/scan-template", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryScanTemplate)))
	mux.Handle("GET /api/v1/scan/{code}", OpenToPublic(RequirePermission(rbac.PermAssetsScan)(http.HandlerFunc(s.handleScan))))
	mux.Handle("GET /api/v1/categories/scan-conflicts", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleScanConflicts)))
	mux.Handle("PUT /api/v1/categories/{id}/label-template", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryLabelTemplate)))
	mux.Handle("GET /api/v1/categories/label-conflicts", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleLabelConflicts)))

	// Actions on assets (README §5.4)
	mux.Handle("GET /api/v1/actions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListActions)))
	mux.Handle("POST /api/v1/actions", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSaveAction)))
	mux.Handle("PUT /api/v1/actions/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSaveAction)))
	mux.Handle("DELETE /api/v1/actions/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteAction)))
	mux.Handle("GET /api/v1/categories/{id}/actions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryActions)))
	mux.Handle("PUT /api/v1/categories/{id}/actions", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryActions)))
	mux.Handle("POST /api/v1/assets/{id}/actions/{key}", RequirePermission(rbac.PermAssetsAct)(http.HandlerFunc(s.handlePressAction)))
	mux.Handle("GET /api/v1/assets/{id}/actions/replies", RequirePermission(rbac.PermAssetsAct)(http.HandlerFunc(s.handleActionReplies)))
	mux.Handle("GET /api/v1/events/live", RequirePermission(rbac.PermEventsRead)(http.HandlerFunc(s.handleLiveEvents)))

	// Reports: anyone who reads assets runs them; reports.manage edits them.
	mux.Handle("GET /api/v1/reports", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListReports)))
	mux.Handle("POST /api/v1/reports", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSaveReport)))
	mux.Handle("POST /api/v1/reports/preview", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handlePreviewReport)))
	mux.Handle("GET /api/v1/reports/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetReport)))
	mux.Handle("PUT /api/v1/reports/{id}", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSaveReport)))
	mux.Handle("DELETE /api/v1/reports/{id}", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleDeleteReport)))
	mux.Handle("POST /api/v1/reports/{id}/default", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSetDefaultScanTemplate)))
	mux.Handle("GET /api/v1/reports/{id}/run", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleRunReport)))
	mux.Handle("GET /api/v1/reports/{id}/sections/{n}/csv", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleReportCSV)))

	// Relation types, for links between assets
	mux.Handle("GET /api/v1/relation-types", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListRelationTypes)))
	mux.Handle("POST /api/v1/relation-types", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateRelationType)))
	mux.Handle("PUT /api/v1/relation-types/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateRelationType)))
	mux.Handle("DELETE /api/v1/relation-types/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteRelationType)))

	// Asset Registry & Dynamic Field Values endpoints
	mux.Handle("GET /api/v1/assets", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListAssets)))
	mux.Handle("GET /api/v1/assets/export.csv", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportAssets)))
	mux.Handle("POST /api/v1/assets/import/preview", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleImportPreview)))
	mux.Handle("POST /api/v1/assets/import", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleImport)))
	mux.Handle("POST /api/v1/assets", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleCreateAsset)))
	mux.Handle("GET /api/v1/assets/{id}", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetAsset))))
	mux.Handle("PUT /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUpdateAsset)))
	mux.Handle("DELETE /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleDeleteAsset)))
	mux.Handle("POST /api/v1/assets/{id}/restore", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleRestoreAsset)))
	mux.Handle("POST /api/v1/assets/{id}/quantity", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleAdjustQuantity)))
	mux.Handle("PUT /api/v1/assets/{id}/running-low", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleSetAssetRunningLow)))
	mux.Handle("PUT /api/v1/assets/{id}/condition", RequireAnyPermission(rbac.PermAssetsReport, rbac.PermAssetsWrite)(http.HandlerFunc(s.handleSetAssetCondition)))
	mux.Handle("POST /api/v1/assets/{id}/photos", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUploadPhoto)))
	mux.Handle("PUT /api/v1/assets/{id}/photos", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleReorderPhotos)))
	mux.Handle("GET /api/v1/assets/{id}/photos/{photo_id}", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetPhoto))))
	mux.Handle("DELETE /api/v1/assets/{id}/photos/{photo_id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleRemovePhoto)))
	mux.Handle("POST /api/v1/assets/{id}/links", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleAddLink)))
	mux.Handle("DELETE /api/v1/assets/{id}/links/{link_id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleRemoveLink)))
	lending := RequireAnyPermission(rbac.PermAssetsBorrow, rbac.PermAssetsLend)
	mux.Handle("POST /api/v1/assets/{id}/checkout", lending(http.HandlerFunc(s.handleCheckOut)))
	mux.Handle("POST /api/v1/assets/{id}/checkin", lending(http.HandlerFunc(s.handleCheckIn)))
	mux.Handle("GET /api/v1/loans", RequirePermission(rbac.PermAssetsTrack)(http.HandlerFunc(s.handleListLoans)))
	mux.Handle("GET /api/v1/people", RequireAnyPermission(rbac.PermAssetsLend, rbac.PermUsersManage)(http.HandlerFunc(s.handleListPeople)))
	mux.Handle("GET /api/v1/assets/{id}/history", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleAssetHistory))))

	// Label templates
	mux.Handle("GET /api/v1/templates", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListTemplates)))
	mux.Handle("POST /api/v1/templates", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleCreateTemplate)))
	mux.Handle("GET /api/v1/templates/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetTemplate)))
	mux.Handle("PUT /api/v1/templates/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleUpdateTemplate)))
	mux.Handle("DELETE /api/v1/templates/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleDeleteTemplate)))

	// Images placed on labels
	mux.Handle("GET /api/v1/images", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListImages)))
	mux.Handle("POST /api/v1/images", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleUploadImage)))
	mux.Handle("GET /api/v1/images/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetImage)))
	mux.Handle("GET /api/v1/images/{id}/content", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetImageContent)))
	mux.Handle("DELETE /api/v1/images/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleDeleteImage)))

	// Label rendering and export
	mux.Handle("GET /api/v1/assets/{id}/label", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleAssetLabel)))
	mux.Handle("POST /api/v1/templates/preview", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handlePreviewTemplate)))
	mux.Handle("POST /api/v1/templates/adapt", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleAdaptTemplate)))
	mux.Handle("POST /api/v1/templates/range", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleTemplateRange)))
	mux.Handle("POST /api/v1/labels/export", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportLabels)))
	mux.Handle("POST /api/v1/labels/options", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleLabelOptions)))
	mux.Handle("GET /api/v1/generators", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListGenerators))))
	mux.Handle("GET /api/v1/generators/{type}/next-code", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleNextCode)))

	// Network printers
	// Template designers read printers too, for their media size and DPI.
	canSeePrinters := RequireAnyPermission(rbac.PermLabelsPrint, rbac.PermSettingsManage, rbac.PermTemplatesManage)
	mux.Handle("GET /api/v1/printers/drivers", canSeePrinters(http.HandlerFunc(s.handlePrinterDrivers)))
	mux.Handle("GET /api/v1/printers", canSeePrinters(http.HandlerFunc(s.handleListPrinters)))
	mux.Handle("GET /api/v1/printers/{id}", canSeePrinters(http.HandlerFunc(s.handleGetPrinter)))
	mux.Handle("POST /api/v1/printers/{id}/fit", canSeePrinters(http.HandlerFunc(s.handlePrinterFit)))
	mux.Handle("POST /api/v1/printers", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleCreatePrinter)))
	mux.Handle("PUT /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleUpdatePrinter)))
	mux.Handle("POST /api/v1/printers/{id}/changes", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handlePrinterChanges)))
	mux.Handle("DELETE /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeletePrinter)))
	mux.Handle("POST /api/v1/printers/{id}/print", RequirePermission(rbac.PermLabelsPrint)(http.HandlerFunc(s.handlePrint)))
	mux.Handle("POST /api/v1/printers/{id}/test-print", RequirePermission(rbac.PermLabelsPrint)(RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleTestPrint))))
	mux.Handle("GET /api/v1/label-stock", canSeePrinters(http.HandlerFunc(s.handleListLabelStock)))
	mux.Handle("POST /api/v1/label-stock", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleCreateLabelStock)))
	mux.Handle("PUT /api/v1/label-stock/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleUpdateLabelStock)))
	mux.Handle("DELETE /api/v1/label-stock/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeleteLabelStock)))
	mux.Handle("GET /api/v1/printer-pools", canSeePrinters(http.HandlerFunc(s.handleListPools)))
	mux.Handle("GET /api/v1/printer-pools/{id}", canSeePrinters(http.HandlerFunc(s.handleGetPool)))
	mux.Handle("POST /api/v1/printer-pools", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleSavePool)))
	mux.Handle("PUT /api/v1/printer-pools/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleSavePool)))
	mux.Handle("DELETE /api/v1/printer-pools/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeletePool)))

	// The spool, a print queue for labels (README §6.2)
	printing := func(h http.HandlerFunc) http.Handler { return RequirePermission(rbac.PermLabelsPrint)(h) }
	mux.Handle("GET /api/v1/spool", printing(s.handleListSpool))
	mux.Handle("POST /api/v1/spool", printing(s.handleAddToSpool))
	mux.Handle("PATCH /api/v1/spool", printing(s.handleUpdateSpool))
	mux.Handle("POST /api/v1/spool/remove", printing(s.handleRemoveFromSpool))
	mux.Handle("POST /api/v1/spool/requeue", printing(s.handleRequeueSpool))
	mux.Handle("POST /api/v1/spool/download", printing(s.handleDownloadSpool))
	mux.Handle("POST /api/v1/spool/print", printing(s.handlePrintSpool))
	mux.Handle("GET /api/v1/spool/jobs", printing(s.handleListSpoolJobs))
	mux.Handle("POST /api/v1/spool/jobs/{id}/cancel", printing(s.handleCancelSpoolJob))
	mux.Handle("POST /api/v1/spool/jobs/{id}/resend", printing(s.handleResendSpoolJob))

	// Notification channels (README §5.2)
	manage := func(h http.HandlerFunc) http.Handler { return RequirePermission(rbac.PermSettingsManage)(h) }
	mux.Handle("GET /api/v1/notifiers/drivers", manage(s.handleNotifierDrivers))
	mux.Handle("GET /api/v1/notifiers/deliveries", manage(s.handleListDeliveries))
	mux.Handle("GET /api/v1/notifiers", manage(s.handleListChannels))
	mux.Handle("POST /api/v1/notifiers", manage(s.handleCreateChannel))
	mux.Handle("GET /api/v1/notifiers/{id}", manage(s.handleGetChannel))
	mux.Handle("PUT /api/v1/notifiers/{id}", manage(s.handleUpdateChannel))
	mux.Handle("DELETE /api/v1/notifiers/{id}", manage(s.handleDeleteChannel))
	mux.Handle("POST /api/v1/notifiers/{id}/test", manage(s.handleTestChannel))

	// The web interface: every other GET, including client-side routes
	mux.Handle("GET /", webui.Handler())

	// Wrap root handler with session extraction middleware
	rootHandler := LimitRequestBodies(RefuseUnstorableText(Authenticate(st)(s.PokeLive(mux))))

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      rootHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Handler returns the underlying http.Handler (useful for httptest).
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start begins listening and serving HTTP requests.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	dbStatus := "connected"
	httpCode := http.StatusOK

	if err := s.store.Ping(); err != nil {
		status = "degraded"
		dbStatus = fmt.Sprintf("disconnected: %v", err)
		httpCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   status,
		"database": dbStatus,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ready": true,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	commit, modified := buildCommit()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":     "BAM! (Boring Asset Manager)",
		"version":  Version,
		"commit":   commit,
		"modified": modified,
		"base_url": s.cfg.Server.BaseURL,
		"dialect":  s.store.Dialect,
	})
}

// actingStore is the store with changes attributed to the signed-in user, or
// the API token, for the events they cause.
func (s *Server) actingStore(r *http.Request) *store.Store {
	if t := CurrentToken(r.Context()); t != nil {
		return s.store.AsToken(t.ID)
	}
	if u := CurrentUser(r.Context()); u != nil {
		return s.store.As(u.ID)
	}
	return s.store
}

// buildCommit is the commit this binary was built from, shortened, and
// whether the working tree had changes, as Go records when building in a
// git checkout; empty when it was not.
func buildCommit() (commit string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return commit, modified
}
