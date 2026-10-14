package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Version is the current application version.
const Version = "0.1.0-alpha"

// Server wraps the HTTP server and dependencies.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	images     *filestore.Images
	transport  printers.Transport
	httpServer *http.Server
}

// NewServer initializes a new Server with route registration and middleware chaining.
func NewServer(cfg *config.Config, st *store.Store) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg:       cfg,
		store:     st,
		images:    filestore.NewImages(cfg.Storage.LocalPath),
		transport: printers.NewDispatcher(cfg.Printers.Devices),
	}

	// Unauthenticated public endpoints
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /api/v1/info", s.handleInfo)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Authenticated session endpoints
	mux.Handle("POST /api/v1/auth/logout", RequireAuth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/auth/me", RequireAuth(http.HandlerFunc(s.handleAuthMe)))

	// User administration endpoints (requires users:manage)
	mux.Handle("GET /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/v1/users", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("PATCH /api/v1/users/{id}/status", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleUpdateUserStatus)))
	mux.Handle("DELETE /api/v1/users/{id}", RequirePermission(rbac.PermUsersManage)(http.HandlerFunc(s.handleDeleteUser)))

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
	mux.Handle("GET /api/v1/categories/{id}/fields", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetCategoryEffectiveFields)))
	mux.Handle("POST /api/v1/categories/{id}/fields", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleAssignCategoryField)))
	mux.Handle("DELETE /api/v1/categories/{id}/fields/{field_id}", RequirePermission(rbac.PermCategoriesManage)(http.HandlerFunc(s.handleRemoveCategoryField)))

	// Asset Registry & Dynamic Field Values endpoints
	mux.Handle("GET /api/v1/assets", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleListAssets)))
	mux.Handle("POST /api/v1/assets", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleCreateAsset)))
	mux.Handle("GET /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleGetAsset)))
	mux.Handle("PUT /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsWrite)(http.HandlerFunc(s.handleUpdateAsset)))
	mux.Handle("DELETE /api/v1/assets/{id}", RequirePermission(rbac.PermAssetsDelete)(http.HandlerFunc(s.handleDeleteAsset)))

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
	mux.Handle("POST /api/v1/labels/export", RequirePermission(rbac.PermAssetsRead)(http.HandlerFunc(s.handleExportLabels)))

	// Network printers
	canSeePrinters := RequireAnyPermission(rbac.PermLabelsPrint, rbac.PermSettingsManage)
	mux.Handle("GET /api/v1/printers/drivers", canSeePrinters(http.HandlerFunc(s.handlePrinterDrivers)))
	mux.Handle("GET /api/v1/printers", canSeePrinters(http.HandlerFunc(s.handleListPrinters)))
	mux.Handle("GET /api/v1/printers/{id}", canSeePrinters(http.HandlerFunc(s.handleGetPrinter)))
	mux.Handle("POST /api/v1/printers", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleCreatePrinter)))
	mux.Handle("PUT /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleUpdatePrinter)))
	mux.Handle("DELETE /api/v1/printers/{id}", RequirePermission(rbac.PermSettingsManage)(http.HandlerFunc(s.handleDeletePrinter)))
	mux.Handle("POST /api/v1/printers/{id}/print", RequirePermission(rbac.PermLabelsPrint)(http.HandlerFunc(s.handlePrint)))

	// Wrap root handler with session extraction middleware
	rootHandler := LimitRequestBodies(Authenticate(st)(mux))

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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":     "BAM! (Boring Asset Manager)",
		"version":  Version,
		"base_url": s.cfg.Server.BaseURL,
		"dialect":  s.store.Dialect,
	})
}
