package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
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
	cfg   *config.Config
	store *store.Store
	// blobs holds uploaded files (README §2); images and photos are kinds
	// of them.
	blobs  filestore.Blobs
	images *filestore.Images
	photos *filestore.Images
	// staging keeps archives uploaded through the web (#141), and
	// uploadLocks takes their parts one at a time.
	staging     filestore.Staging
	uploadLocks uploadLocks
	// backupJobs are the checks and replaces of backups under way (#142).
	backupJobs backupJobs
	// exports are packages posted to download (#144).
	exports    exportsPending
	transport  printers.Transport
	spool      *spooler
	presses    *pressLimiter
	live       *liveHub
	sample     sampleCache
	httpServer *http.Server
	// routes are every API route, for the test that MCP reaches each (#33).
	routes []string
	// registrations limits OAuth clients registering (README §6.6).
	registrations registrations
	// icons compiles icon sources (README §3.7).
	icons iconCompiler
	// iconsDirty is signalled after every change made through the API,
	// for RunIcons to build the icons again if their inputs changed:
	// those with params bound to asset fields follow the assets' values.
	iconsDirty chan struct{}
	// built is the current icon build's IR, parsed, for scenes.
	built builtIcons
}

// NewServer initializes a new Server with route registration and middleware chaining.
func NewServer(cfg *config.Config, st *store.Store) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:        cfg,
		store:      st,
		transport:  printers.NewDispatcher(cfg.Printers.Devices),
		spool:      newSpooler(),
		presses:    newPressLimiter(),
		live:       newLiveHub(),
		icons:      isolex.Default(),
		iconsDirty: make(chan struct{}, 1),
	}
	// The settings were checked as they loaded, so the store opens; one
	// that does not reach, or cannot be written, is found by CheckStorage.
	blobs, err := filestore.Open(cfg.Storage)
	if err != nil {
		panic(err)
	}
	s.blobs = blobs
	s.images, s.photos = filestore.NewImages(blobs), filestore.NewPhotos(blobs)
	s.staging = filestore.OpenStaging(cfg.Storage, blobs)
	handle := func(pattern string, h http.Handler) {
		s.routes = append(s.routes, pattern)
		if !strings.HasPrefix(pattern, "GET ") {
			h = s.markingIcons(h)
		}
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
	handle("PUT /api/v1/auth/theme", RequirePerson(http.HandlerFunc(s.handleSetOwnTheme)))
	// Personal tokens, for assistants (README §3.4): made, listed and revoked
	// by their own user, from a signed-in session.
	// A personal token lists and revokes its user's too (#43), but only a
	// session makes them.
	handle("GET /api/v1/auth/tokens", RequirePerson(http.HandlerFunc(s.handleListOwnTokens)))
	handle("POST /api/v1/auth/tokens", RequireAuth(http.HandlerFunc(s.handleCreateOwnToken)))
	handle("DELETE /api/v1/auth/tokens/{id}", RequirePerson(http.HandlerFunc(s.handleRevokeOwnToken)))
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

	// Archives uploaded in parts, resumably (README §2.3, #141), for
	// replacing an instance with a backup.
	manageSettings := RequirePermission(rbac.PermSettingsManage)
	handle("POST /api/v1/uploads", manageSettings(http.HandlerFunc(s.handleCreateUpload)))
	handle("GET /api/v1/uploads/{id}", manageSettings(http.HandlerFunc(s.handleGetUpload)))
	handle("PUT /api/v1/uploads/{id}", manageSettings(http.HandlerFunc(s.handlePutUploadPart)))
	handle("DELETE /api/v1/uploads/{id}", manageSettings(http.HandlerFunc(s.handleDeleteUpload)))
	handle("GET /api/v1/uploads/{id}/content", manageSettings(http.HandlerFunc(s.handleUploadContent)))
	// Backups from the web, and replacing the instance with one (#142):
	// people's and settings' managers both.
	manageBackups := func(h http.HandlerFunc) http.Handler {
		return RequirePermission(rbac.PermUsersManage)(RequirePermission(rbac.PermSettingsManage)(h))
	}
	handle("GET /api/v1/backup", manageBackups(s.handleDownloadBackup))
	handle("POST /api/v1/backup/check", manageBackups(s.handleCheckBackup))
	handle("POST /api/v1/backup/replace", manageBackups(s.handleReplaceBackup))
	handle("GET /api/v1/backup/jobs/{id}", manageBackups(s.handleBackupJob))
	// Packages (#144): what is packaged is what the requester may read.
	readAssets := RequirePermission(rbac.PermAssetsRead)
	handle("POST /api/v1/exports/plan", readAssets(http.HandlerFunc(s.handleExportPlan)))
	handle("POST /api/v1/exports", readAssets(http.HandlerFunc(s.handleCreateExport)))
	handle("GET /api/v1/exports/{id}", readAssets(http.HandlerFunc(s.handleDownloadExport)))
	// Importing one (#145), uploaded as backups are: what it needs beyond
	// is checked against what it holds.
	handle("POST /api/v1/import/check", manageSettings(http.HandlerFunc(s.handleCheckPackageImport)))
	handle("POST /api/v1/import", manageSettings(http.HandlerFunc(s.handlePackageImport)))
	handle("GET /api/v1/import/jobs/{id}", manageSettings(http.HandlerFunc(s.handleBackupJob)))

	// Scopes (README §3.3): who may see and change assets is users.manage's
	// to decide; category managers read them to see what labels are there.
	seeScopes := RequireAnyPermission(rbac.PermUsersManage, rbac.PermCategoriesManage)
	handle("GET /api/v1/scopes", seeScopes(http.HandlerFunc(s.handleListScopes)))
	handle("POST /api/v1/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	handle("PUT /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSaveScope)))
	handle("DELETE /api/v1/scopes/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteScope)))
	handle("GET /api/v1/categories/{id}/scopes", seeScopes(http.HandlerFunc(s.handleCategoryScopes)))
	handle("PUT /api/v1/categories/{id}/scopes", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleSetCategoryScopes)))

	// API tokens: made only from a signed-in session, never by a token, so
	// a token cannot leave itself a way back in; listed and revoked by
	// anyone holding users.manage, tokens too (#43).
	manageTokens := func(h http.HandlerFunc) http.Handler { return RequireAuth(RequirePermission(rbac.PermUsersManage)(h)) }
	seeTokens := func(h http.HandlerFunc) http.Handler { return RequirePermission(rbac.PermUsersManage)(h) }
	handle("GET /api/v1/tokens", seeTokens(s.handleListTokens))
	handle("POST /api/v1/tokens", manageTokens(s.handleCreateToken))
	handle("DELETE /api/v1/tokens/{id}", seeTokens(s.handleRevokeToken))
	handle("GET /api/v1/tokens/connections", seeTokens(s.handleLiveConnections))

	// Field Corpus endpoints
	handle("GET /api/v1/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListFields)))
	handle("POST /api/v1/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateField)))
	handle("GET /api/v1/fields/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetField)))
	handle("PUT /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateField)))
	handle("DELETE /api/v1/fields/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteField)))

	// Category Hierarchy & Field Inheritance endpoints
	handle("GET /api/v1/categories", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListCategories)))
	handle("POST /api/v1/categories", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCreateCategory)))
	handle("POST /api/v1/categories/{id}/clone", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCloneCategory)))
	handle("GET /api/v1/categories/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategory)))
	handle("PUT /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleUpdateCategory)))
	handle("DELETE /api/v1/categories/{id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteCategory)))
	// Icons (README §3.7): reading with assets.read; a category's own icon
	// with categories.manage, and what every icon is drawn with, the
	// library, palette and settings, with icons.manage.
	handle("GET /api/v1/icons", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIcons)))
	handle("GET /api/v1/icons/ir", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconIR)))
	handle("GET /api/v1/icons/image", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconImage)))
	handle("GET /api/v1/icons/palette", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconPalette)))
	handle("PUT /api/v1/icons/palette", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleSetIconPalette)))
	handle("GET /api/v1/icons/settings", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconSettings)))
	handle("PUT /api/v1/icons/settings", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleSetIconSettings)))
	handle("GET /api/v1/icons/states", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconStates)))
	handle("PUT /api/v1/icons/states", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleSetIconStates)))
	handle("GET /api/v1/category-icons", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListCategoryIcons)))
	handle("GET /api/v1/categories/{id}/icon", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategoryIcon)))
	handle("PUT /api/v1/categories/{id}/icon", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSaveCategoryIcon)))
	handle("POST /api/v1/categories/{id}/icon/check", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleCheckCategoryIcon)))
	handle("POST /api/v1/categories/{id}/icon/rename", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleRenameCategoryIcon)))
	handle("DELETE /api/v1/categories/{id}/icon", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDeleteCategoryIcon)))
	handle("DELETE /api/v1/categories/{id}/icon/draft", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleDiscardCategoryIconDraft)))
	handle("GET /api/v1/categories/{id}/icon/revisions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryIconRevisions)))
	handle("GET /api/v1/categories/{id}/icon/params", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleCategoryIconParams)))
	handle("PUT /api/v1/categories/{id}/icon/params", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleSetCategoryIconParams)))
	handle("GET /api/v1/icons/export", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportIcons)))
	handle("POST /api/v1/icon-library/import", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleImportIcons)))
	handle("GET /api/v1/icon-library", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListIconLibrary)))
	handle("POST /api/v1/icon-library", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleCreateIconLibraryFile)))
	handle("GET /api/v1/icon-library/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetIconLibraryFile)))
	handle("PUT /api/v1/icon-library/{id}", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleSaveIconLibraryFile)))
	handle("POST /api/v1/icon-library/{id}/rename", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleRenameIconLibraryFile)))
	handle("DELETE /api/v1/icon-library/{id}", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleDeleteIconLibraryFile)))
	handle("DELETE /api/v1/icon-library/{id}/draft", RequirePermission(rbac.PermIconsManage)(http.HandlerFunc(s.handleDiscardIconLibraryDraft)))
	handle("GET /api/v1/icon-library/{id}/revisions", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleIconLibraryRevisions)))
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
	// The same events, for those who poll, and answers to actions from them.
	handle("GET /api/v1/events", RequirePermission(rbac.PermEventsRead)(http.HandlerFunc(s.handleListEvents)))
	handle("POST /api/v1/events/{id}/reply", RequirePermission(rbac.PermEventsRead)(http.HandlerFunc(s.handleReplyToEvent)))

	// Flows (README §6.7): flows.manage makes them; flows.run runs them, in
	// the scopes of their categories, which each handler checks.
	seeFlows := RequireAnyPermission(rbac.PermFlowsManage, rbac.PermFlowsRun)
	manageFlows := RequirePermission(rbac.PermFlowsManage)
	runFlows := RequirePermission(rbac.PermFlowsRun)
	handle("GET /api/v1/flows", seeFlows(http.HandlerFunc(s.handleListFlows)))
	handle("POST /api/v1/flows", manageFlows(http.HandlerFunc(s.handleSaveFlow)))
	handle("GET /api/v1/flow-starters", manageFlows(http.HandlerFunc(s.handleFlowStarters)))
	handle("GET /api/v1/flows/{slug}", seeFlows(http.HandlerFunc(s.handleGetFlow)))
	handle("PUT /api/v1/flows/{slug}", manageFlows(http.HandlerFunc(s.handleSaveFlow)))
	handle("DELETE /api/v1/flows/{slug}", manageFlows(http.HandlerFunc(s.handleDeleteFlow)))
	handle("GET /api/v1/flows/{slug}/qr.svg", seeFlows(http.HandlerFunc(s.handleFlowQR)))
	handle("GET /api/v1/assets/{id}/flows", runFlows(http.HandlerFunc(s.handleAssetFlows)))
	handle("POST /api/v1/flows/{slug}/runs", runFlows(http.HandlerFunc(s.handleStartFlowRun)))
	handle("GET /api/v1/flows/{slug}/runs/{id}", runFlows(http.HandlerFunc(s.handleGetFlowRun)))
	handle("POST /api/v1/flows/{slug}/runs/{id}", runFlows(http.HandlerFunc(s.handleAnswerFlowRun)))
	handle("DELETE /api/v1/flows/{slug}/runs/{id}", runFlows(http.HandlerFunc(s.handleEndFlowRun)))

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
	handle("GET /api/v1/assets/{id}/scene", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleAssetScene)))
	handle("GET /api/v1/assets/{id}/scene/image", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleSceneImage)))
	handle("PUT /api/v1/assets/{id}/placement", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handlePlaceAsset)))
	handle("DELETE /api/v1/assets/{id}/placement", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handlePlaceAsset)))
	handle("GET /api/v1/assets/{id}/copy", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleCopyAsset)))
	handle("GET /api/v1/assets/{id}", OpenToPublic(RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetAsset))))
	handle("PUT /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUpdateAsset)))
	handle("DELETE /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleDeleteAsset)))
	handle("POST /api/v1/assets/{id}/restore", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleRestoreAsset)))
	handle("POST /api/v1/assets/{id}/quantity", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleAdjustQuantity)))
	handle("PUT /api/v1/assets/{id}/running-low", RequirePermission(rbac.PermStockRecord)(http.HandlerFunc(s.handleSetAssetRunningLow)))
	handle("PUT /api/v1/assets/{id}/condition", RequireAnyPermission(rbac.PermAssetsReport, rbac.PermAssetsWrite)(http.HandlerFunc(s.handleSetAssetCondition)))
	handle("POST /api/v1/assets/{id}/photos", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUploadPhoto)))
	handle("POST /api/v1/assets/{id}/photos/copy", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleCopyPhotos)))
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
	handle("POST /api/v1/templates/preview-data", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handlePreviewData)))
	handle("POST /api/v1/templates/adapt", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleAdaptTemplate)))
	handle("POST /api/v1/templates/range", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleTemplateRange)))
	handle("POST /api/v1/templates/import-svg", RequirePermission(rbac.PermTemplatesManage)(http.HandlerFunc(s.handleImportSVG)))
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
	// Signing assistants in by OAuth (README §6.6).
	s.oauthRoutes(mux, handle)

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
		ReadTimeout:  requestTimeout,
		WriteTimeout: requestTimeout,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// CheckStorage says which store holds uploaded files, and checks that it
// can be reached and written.
func (s *Server) CheckStorage(ctx context.Context) (string, error) {
	return fmt.Sprint(s.blobs), filestore.Check(ctx, s.blobs)
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
