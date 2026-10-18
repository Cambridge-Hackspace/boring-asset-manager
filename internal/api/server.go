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
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/mcpserver"
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
	// routes are every API route, for the test that MCP reaches each (#33).
	routes []string
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
	handle := func(pattern string, h http.Handler) {
		s.routes = append(s.routes, pattern)
		mux.Handle(pattern, h)
	}
	handleFunc := func(pattern string, h http.HandlerFunc) { handle(pattern, h) }

	// Unauthenticated public endpoints
	handleFunc("GET /healthz", s.handleHealthz)
	handleFunc("GET /readyz", s.handleReadyz)
	handleFunc("GET /api/v1/info", s.handleInfo)
	handleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Signed-out visitors hold the public role's permissions on the routes
	// wrapped in OpenToPublic only: one asset's page, never the catalog.

	// Authenticated session endpoints
	handle("POST /api/v1/auth/logout", RequireAuth(http.HandlerFunc(s.handleLogout)))
	handleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	handle("PUT /api/v1/auth/name", RequirePerson(http.HandlerFunc(s.handleSetOwnName)))
	// Personal tokens, for assistants (README §3.4): made, listed and revoked
	// by their own user, from a signed-in session.
	handle("GET /api/v1/auth/tokens", RequireAuth(http.HandlerFunc(s.handleListOwnTokens)))
	handle("POST /api/v1/auth/tokens", RequireAuth(http.HandlerFunc(s.handleCreateOwnToken)))
	handle("DELETE /api/v1/auth/tokens/{id}", RequireAuth(http.HandlerFunc(s.handleRevokeOwnToken)))
	handle("PUT /api/v1/auth/password", RequireAuth(http.HandlerFunc(s.handleChangePassword)))

	// User administration endpoints (requires users.manage)
	handle("GET /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleListUsers)))
	handle("POST /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleCreateUser)))
	handle("PATCH /api/v1/users/{id}/status", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleUpdateUserStatus)))
	handle("DELETE /api/v1/users/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteUser)))
	handle("PUT /api/v1/users/{id}/name", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserName)))
	handle("PUT /api/v1/users/{id}/password", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserPassword)))
	handle("PUT /api/v1/users/{id}/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetUserRoles)))
	handle("GET /api/v1/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleListRoles)))
	handle("POST /api/v1/roles", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleCreateRole)))
	handle("PUT /api/v1/roles/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleUpdateRole)))
	handle("DELETE /api/v1/roles/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteRole)))

	// Scopes (README §3.3): who may see and change assets is users.manage's
	// to decide; category managers read them to see what labels are there.
	seeScopes := RequireAnyPermission(rbac.PermUsersManage, rbac.PermCategoriesManage)
	handle("GET /api/v1/scopes", seeScopes(http.HandlerFunc(s.handleListScopes)))
	handle("POST /api/v1/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	handle("PUT /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	handle("DELETE /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteScope)))
	handle("GET /api/v1/categories/{id}/scopes", seeScopes(http.HandlerFunc(s.handleCategoryScopes)))
	handle("PUT /api/v1/categories/{id}/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetCategoryScopes)))

	// API tokens: managed only from a signed-in session, never by a token.
	manageTokens := func(h http.HandlerFunc) http.Handler { return RequireAuth(RequirePermission(rbac.PermUsersManage)(h)) }
	handle("GET /api/v1/tokens", manageTokens(s.handleListTokens))
	handle("POST /api/v1/tokens", manageTokens(s.handleCreateToken))
	handle("DELETE /api/v1/tokens/{id}", manageTokens(s.handleRevokeToken))
	handle("GET /api/v1/tokens/connections", manageTokens(s.handleLiveConnections))

	// Field Corpus endpoints
	handle("GET /api/v1/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListFields)))
	handle("POST /api/v1/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateField)))
	handle("GET /api/v1/fields/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetField)))
	handle("PUT /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateField)))
	handle("DELETE /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteField)))

	// Category Hierarchy & Field Inheritance endpoints
	handle("GET /api/v1/categories", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListCategories)))
	handle("POST /api/v1/categories", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateCategory)))
	handle("GET /api/v1/categories/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategory)))
	handle("PUT /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateCategory)))
	handle("DELETE /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteCategory)))
	handle("PUT /api/v1/categories/{id}/running-low", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleSetRunningLow)))
	handle("GET /api/v1/categories/{id}/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategoryEffectiveFields)))
	handle("POST /api/v1/categories/effective-fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleEffectiveFieldsForAsset)))
	handle("POST /api/v1/categories/{id}/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleAssignCategoryField)))
	handle("DELETE /api/v1/categories/{id}/fields/{field_id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleRemoveCategoryField)))
	handle("GET /api/v1/categories/{id}/relation-types", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryRelationTypes)))
	handle("PUT /api/v1/categories/{id}/relation-types", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryRelationTypes)))
	handle("PUT /api/v1/categories/{id}/scan-template", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryScanTemplate)))
	handle("GET /api/v1/scan/{code}", OpenToPublic(RequirePermission(rbac.PermAssetsScan)(http.HandlerFunc(s.handleScan))))
	handle("GET /api/v1/categories/scan-conflicts", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleScanConflicts)))
	handle("PUT /api/v1/categories/{id}/label-template", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryLabelTemplate)))
	handle("GET /api/v1/categories/label-conflicts", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleLabelConflicts)))

	// Actions on assets (README §5.4)
	handle("GET /api/v1/actions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListActions)))
	handle("POST /api/v1/actions", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSaveAction)))
	handle("PUT /api/v1/actions/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSaveAction)))
	handle("DELETE /api/v1/actions/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteAction)))
	handle("GET /api/v1/categories/{id}/actions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryActions)))
	handle("PUT /api/v1/categories/{id}/actions", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryActions)))
	handle("POST /api/v1/assets/{id}/actions/{key}", RequirePermission(rbac.PermAssetsAct)(http.HandlerFunc(s.handlePressAction)))
	handle("GET /api/v1/assets/{id}/actions/replies", RequirePermission(rbac.PermAssetsAct)(http.HandlerFunc(s.handleActionReplies)))
	handle("GET /api/v1/events/live", RequirePermission(rbac.PermEventsRead)(http.HandlerFunc(s.handleLiveEvents)))

	// Reports: anyone who reads assets runs them; reports.manage edits them.
	handle("GET /api/v1/reports", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListReports)))
	handle("POST /api/v1/reports", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSaveReport)))
	handle("POST /api/v1/reports/preview", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handlePreviewReport)))
	handle("GET /api/v1/reports/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetReport)))
	handle("PUT /api/v1/reports/{id}", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSaveReport)))
	handle("DELETE /api/v1/reports/{id}", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleDeleteReport)))
	handle("POST /api/v1/reports/{id}/default", RequirePermission(rbac.PermReportsManage)(http.HandlerFunc(s.handleSetDefaultScanTemplate)))
	handle("GET /api/v1/reports/{id}/run", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleRunReport)))
	handle("GET /api/v1/reports/{id}/sections/{n}/csv", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleReportCSV)))

	// Relation types, for links between assets
	handle("GET /api/v1/relation-types", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListRelationTypes)))
	handle("POST /api/v1/relation-types", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateRelationType)))
	handle("PUT /api/v1/relation-types/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateRelationType)))
	handle("DELETE /api/v1/relation-types/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteRelationType)))

	// Asset Registry & Dynamic Field Values endpoints
	handle("GET /api/v1/assets", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListAssets)))
	handle("GET /api/v1/assets/export.csv", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportAssets)))
	handle("POST /api/v1/assets/import/preview", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleImportPreview)))
	handle("POST /api/v1/assets/import", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleImport)))
	handle("POST /api/v1/assets", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleCreateAsset)))
	handle("GET /api/v1/assets/{id}", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetAsset))))
	handle("PUT /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUpdateAsset)))
	handle("DELETE /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleDeleteAsset)))
	handle("POST /api/v1/assets/{id}/restore", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleRestoreAsset)))
	handle("POST /api/v1/assets/{id}/quantity", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleAdjustQuantity)))
	handle("PUT /api/v1/assets/{id}/running-low", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleSetAssetRunningLow)))
	handle("PUT /api/v1/assets/{id}/condition", RequireAnyPermission(rbac.PermAssetsReport, rbac.PermAssetsWrite)(http.HandlerFunc(s.handleSetAssetCondition)))
	handle("POST /api/v1/assets/{id}/photos", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUploadPhoto)))
	handle("PUT /api/v1/assets/{id}/photos", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleReorderPhotos)))
	handle("GET /api/v1/assets/{id}/photos/{photo_id}", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetPhoto))))
	handle("DELETE /api/v1/assets/{id}/photos/{photo_id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleRemovePhoto)))
	handle("POST /api/v1/assets/{id}/links", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleAddLink)))
	handle("DELETE /api/v1/assets/{id}/links/{link_id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleRemoveLink)))
	lending := RequireAnyPermission(rbac.PermAssetsBorrow, rbac.PermAssetsLend)
	handle("POST /api/v1/assets/{id}/checkout", lending(http.HandlerFunc(s.handleCheckOut)))
	handle("POST /api/v1/assets/{id}/checkin", lending(http.HandlerFunc(s.handleCheckIn)))
	handle("GET /api/v1/loans", RequirePermission(rbac.PermAssetsTrack)(http.HandlerFunc(s.handleListLoans)))
	handle("GET /api/v1/people", RequireAnyPermission(rbac.PermAssetsLend, rbac.PermUsersManage)(http.HandlerFunc(s.handleListPeople)))
	handle("GET /api/v1/assets/{id}/history", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleAssetHistory))))

	// Label templates
	handle("GET /api/v1/templates", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListTemplates)))
	handle("POST /api/v1/templates", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleCreateTemplate)))
	handle("GET /api/v1/templates/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetTemplate)))
	handle("PUT /api/v1/templates/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleUpdateTemplate)))
	handle("DELETE /api/v1/templates/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleDeleteTemplate)))

	// Images placed on labels
	handle("GET /api/v1/images", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListImages)))
	handle("POST /api/v1/images", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleUploadImage)))
	handle("GET /api/v1/images/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetImage)))
	handle("GET /api/v1/images/{id}/content", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetImageContent)))
	handle("DELETE /api/v1/images/{id}", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleDeleteImage)))

	// Label rendering and export
	handle("GET /api/v1/assets/{id}/label", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleAssetLabel)))
	handle("POST /api/v1/templates/preview", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handlePreviewTemplate)))
	handle("POST /api/v1/templates/adapt", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleAdaptTemplate)))
	handle("POST /api/v1/templates/range", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleTemplateRange)))
	handle("POST /api/v1/labels/export", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportLabels)))
	handle("POST /api/v1/labels/options", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleLabelOptions)))
	handle("GET /api/v1/generators", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListGenerators))))
	handle("GET /api/v1/generators/{type}/next-code", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleNextCode)))

	// Network printers
	// Template designers read printers too, for their media size and DPI.
	canSeePrinters := RequireAnyPermission(rbac.PermLabelsPrint, rbac.PermSettingsManage, rbac.PermTemplatesManage)
	handle("GET /api/v1/printers/drivers", canSeePrinters(http.HandlerFunc(s.handlePrinterDrivers)))
	handle("GET /api/v1/printers", canSeePrinters(http.HandlerFunc(s.handleListPrinters)))
	handle("GET /api/v1/printers/{id}", canSeePrinters(http.HandlerFunc(s.handleGetPrinter)))
	handle("POST /api/v1/printers/{id}/fit", canSeePrinters(http.HandlerFunc(s.handlePrinterFit)))
	handle("POST /api/v1/printers", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleCreatePrinter)))
	handle("PUT /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleUpdatePrinter)))
	handle("POST /api/v1/printers/{id}/changes", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handlePrinterChanges)))
	handle("DELETE /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeletePrinter)))
	handle("POST /api/v1/printers/{id}/print", RequirePermission(rbac.PermLabelsPrint)(http.HandlerFunc(s.handlePrint)))
	handle("POST /api/v1/printers/{id}/test-print", RequirePermission(rbac.PermLabelsPrint)(RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleTestPrint))))
	handle("GET /api/v1/label-stock", canSeePrinters(http.HandlerFunc(s.handleListLabelStock)))
	handle("POST /api/v1/label-stock", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleCreateLabelStock)))
	handle("PUT /api/v1/label-stock/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleUpdateLabelStock)))
	handle("DELETE /api/v1/label-stock/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeleteLabelStock)))
	handle("GET /api/v1/printer-pools", canSeePrinters(http.HandlerFunc(s.handleListPools)))
	handle("GET /api/v1/printer-pools/{id}", canSeePrinters(http.HandlerFunc(s.handleGetPool)))
	handle("POST /api/v1/printer-pools", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleSavePool)))
	handle("PUT /api/v1/printer-pools/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleSavePool)))
	handle("DELETE /api/v1/printer-pools/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeletePool)))

	// The spool, a print queue for labels (README §6.2)
	printing := func(h http.HandlerFunc) http.Handler { return RequirePermission(rbac.PermLabelsPrint)(h) }
	handle("GET /api/v1/spool", printing(s.handleListSpool))
	handle("POST /api/v1/spool", printing(s.handleAddToSpool))
	handle("PATCH /api/v1/spool", printing(s.handleUpdateSpool))
	handle("POST /api/v1/spool/remove", printing(s.handleRemoveFromSpool))
	handle("POST /api/v1/spool/requeue", printing(s.handleRequeueSpool))
	handle("POST /api/v1/spool/download", printing(s.handleDownloadSpool))
	handle("POST /api/v1/spool/print", printing(s.handlePrintSpool))
	handle("GET /api/v1/spool/jobs", printing(s.handleListSpoolJobs))
	handle("POST /api/v1/spool/jobs/{id}/cancel", printing(s.handleCancelSpoolJob))
	handle("POST /api/v1/spool/jobs/{id}/resend", printing(s.handleResendSpoolJob))

	// Notification channels (README §5.2)
	manage := func(h http.HandlerFunc) http.Handler { return RequirePermission(rbac.PermSettingsManage)(h) }
	handle("GET /api/v1/notifiers/drivers", manage(s.handleNotifierDrivers))
	handle("GET /api/v1/notifiers/deliveries", manage(s.handleListDeliveries))
	handle("GET /api/v1/notifiers", manage(s.handleListChannels))
	handle("POST /api/v1/notifiers", manage(s.handleCreateChannel))
	handle("GET /api/v1/notifiers/{id}", manage(s.handleGetChannel))
	handle("PUT /api/v1/notifiers/{id}", manage(s.handleUpdateChannel))
	handle("DELETE /api/v1/notifiers/{id}", manage(s.handleDeleteChannel))
	handle("POST /api/v1/notifiers/{id}/test", manage(s.handleTestChannel))

	// The web interface: every other GET, including client-side routes
	// MCP (README §6.6): the API again, as tools for assistants.
	mcp := s.mcpHandler()
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		mux.Handle(method+" /api/v1/mcp", mcp)
	}

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
		"mcp":      s.cfg.MCP.Enabled,
	})
}

// actingStore is the store with changes attributed to the signed-in user, or
// the API token, for the events they cause, and to the assistant they were
// made through, if an MCP tool made them.
func (s *Server) actingStore(r *http.Request) *store.Store {
	acting := s.actor(r)
	if via := mcpserver.ViaFrom(r.Context()); via != "" {
		acting = acting.Via(via)
	}
	return acting
}

func (s *Server) actor(r *http.Request) *store.Store {
	// A personal token acts as its user, so the user comes first.
	if u := CurrentUser(r.Context()); u != nil {
		return s.store.As(u.ID)
	}
	if t := CurrentToken(r.Context()); t != nil {
		return s.store.AsToken(t.ID)
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
