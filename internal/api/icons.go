package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Icons (README §3.7): category icons, the library, the palette, the
// settings and the build they make, all compiled by isolex in process.

// iconCompiler is what the server compiles icons with: the process's
// isolex, which a binary built without it does not have.
type iconCompiler interface {
	store.IconCompiler
	Available() bool
}

// iconBodyLimit admits a source of MaxIconSource characters, each up to
// four bytes, escaped in JSON.
const iconBodyLimit = 4 << 20

// isIconSourcePath reports whether a request carries an icon source.
func isIconSourcePath(path string) bool {
	// an archive of them has its own, larger limit
	return (strings.HasPrefix(path, "/api/v1/icon-library") && path != "/api/v1/icon-library/import") ||
		(strings.HasPrefix(path, "/api/v1/categories/") && (strings.HasSuffix(path, "/icon") || strings.HasSuffix(path, "/icon/check")))
}

// RunIcons warms the icon compiler, which takes seconds to compile to
// machine code, so that the first save does not wait for it; then builds
// the icons if what is published was last built by another isolex, or
// not at all.
func (s *Server) RunIcons(ctx context.Context) {
	if !s.icons.Available() {
		log.Printf("icons: the compiler is not built into this binary; icons are off")
		return
	}
	v, err := s.icons.Version()
	if err != nil {
		log.Printf("icons: the compiler does not start: %v", err)
		return
	}
	save, err := s.store.RebuildIcons(ctx, s.icons)
	switch {
	case err != nil:
		log.Printf("icons: building with isolex %s: %v", v, err)
	case !save.Published:
		log.Printf("icons: isolex %s does not compile what is published (%d errors, the first %s:%d:%d: %s); the last good build stays",
			v, len(save.Errors), save.Errors[0].File, save.Errors[0].Line, save.Errors[0].Col, save.Errors[0].Message)
	}
}

// writeIconError answers a failed icon change.
func writeIconError(w http.ResponseWriter, err error, action string) {
	var be *store.IconBuildError
	switch {
	case errors.As(err, &be):
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "The icons would not compile after this change", Details: be.Errors})
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrCategoryIconNotFound):
		writeJSONError(w, http.StatusNotFound, "This category has no icon")
	case errors.Is(err, store.ErrIconLibraryFileNotFound):
		writeJSONError(w, http.StatusNotFound, "Icon library file not found")
	case errors.Is(err, store.ErrIconNameTaken), errors.Is(err, store.ErrIconPathTaken), errors.Is(err, store.ErrIconsChanging):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, isolex.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "The icon compiler is not built into this server, so icons cannot change")
	case errors.Is(err, store.ErrIconCompile):
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// decodeJSON reads a request body into v, answering 400 if it cannot.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// handleIcons says whether icons can change here, what the current build
// is, which icon each category is drawn with, its own or its nearest
// ancestor's, the unknown icon, drawn for a name the build does not draw,
// and the names assets have as their own that it does not draw, with how
// many have each: GET /api/v1/icons
func (s *Server) handleIcons(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.CurrentIconBuild()
	if err != nil {
		writeServerError(w, err, "read the icon build")
		return
	}
	shown, err := s.store.ShownCategoryIcons()
	if err != nil {
		writeServerError(w, err, "read the category icons")
		return
	}
	missing, err := s.store.MissingAssetIcons()
	if err != nil {
		writeServerError(w, err, "read the assets' icons")
		return
	}
	body := map[string]any{"available": s.icons.Available(), "isolex_version": isolex.Pinned().Version, "build": nil,
		"category_icons": shown, "unknown": isolex.UnknownIR(), "missing_icons": missing}
	if b != nil {
		var doc struct {
			Icons []struct {
				Name     string `json:"name"`
				Category string `json:"category"`
			} `json:"icons"`
		}
		if err := json.Unmarshal(b.IR, &doc); err != nil {
			writeServerError(w, err, "read the icon build")
			return
		}
		body["build"] = map[string]any{"hash": b.Hash, "isolex_version": b.IsolexVersion, "built_at": b.BuiltAt,
			"warnings": b.Warnings, "icons": doc.Icons}
	}
	writeJSON(w, http.StatusOK, body)
}

// handleIconIR serves the current build's IR, which pages draw icons
// from; its ETag is the build's hash: GET /api/v1/icons/ir
func (s *Server) handleIconIR(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.CurrentIconBuild()
	if err != nil {
		writeServerError(w, err, "read the icon build")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if b == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ir_version": 1, "icons": []any{}})
		return
	}
	etag := `"` + b.Hash + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b.IR)
}

// GET /api/v1/icons/palette
func (s *Server) handleIconPalette(w http.ResponseWriter, r *http.Request) {
	palette, err := s.store.GetIconPalette()
	if err != nil {
		writeServerError(w, err, "read the icon palette")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"palette": palette})
}

// PUT /api/v1/icons/palette with {"palette": [{"token", "light", "dark"}]}
func (s *Server) handleSetIconPalette(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Palette []models.IconColor `json:"palette"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Palette == nil {
		writeJSONError(w, http.StatusBadRequest, "palette is required")
		return
	}
	save, err := s.actingStore(r).SetIconPalette(r.Context(), s.icons, req.Palette)
	if err != nil {
		writeIconError(w, err, "change the icon palette")
		return
	}
	palette, err := s.store.GetIconPalette()
	if err != nil {
		writeServerError(w, err, "read the icon palette")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"palette": palette, "save": save})
}

// GET /api/v1/icons/settings
func (s *Server) handleIconSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetIconSettings()
	if err != nil {
		writeServerError(w, err, "read the icon settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

// PUT /api/v1/icons/settings with {"stroke_widths", "reserved", "limits"}
func (s *Server) handleSetIconSettings(w http.ResponseWriter, r *http.Request) {
	var req models.IconSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	save, err := s.actingStore(r).SetIconSettings(r.Context(), s.icons, req)
	if err != nil {
		writeIconError(w, err, "change the icon settings")
		return
	}
	settings, err := s.store.GetIconSettings()
	if err != nil {
		writeServerError(w, err, "read the icon settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "save": save})
}

// GET /api/v1/category-icons
func (s *Server) handleListCategoryIcons(w http.ResponseWriter, r *http.Request) {
	icons, err := s.store.ListCategoryIcons()
	if err != nil {
		writeServerError(w, err, "list category icons")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"icons": icons})
}

// GET /api/v1/categories/{id}/icon
func (s *Server) handleGetCategoryIcon(w http.ResponseWriter, r *http.Request) {
	icon, err := s.store.GetCategoryIcon(r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "read the category's icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"icon": icon})
}

type iconSourceRequest struct {
	IconName string `json:"icon_name"`
	Source   string `json:"source"`
}

// PUT /api/v1/categories/{id}/icon with {"icon_name", "source"}: published
// if the icons compile with it, a draft otherwise.
func (s *Server) handleSaveCategoryIcon(w http.ResponseWriter, r *http.Request) {
	var req iconSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	save, err := s.actingStore(r).SaveCategoryIcon(r.Context(), s.icons, id, req.IconName, req.Source)
	if err != nil {
		writeIconError(w, err, "save the category's icon")
		return
	}
	icon, err := s.store.GetCategoryIcon(id)
	if err != nil {
		writeIconError(w, err, "read the category's icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"icon": icon, "save": save})
}

// POST /api/v1/categories/{id}/icon/check with {"icon_name", "source"}:
// compiles as a save would, and keeps nothing.
func (s *Server) handleCheckCategoryIcon(w http.ResponseWriter, r *http.Request) {
	var req iconSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.GetCategoryByID(id); err != nil {
		writeIconError(w, err, "check the category's icon")
		return
	}
	check, err := s.store.CheckCategoryIcon(r.Context(), s.icons, id, req.IconName, req.Source)
	if err != nil {
		writeIconError(w, err, "check the category's icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"check": check})
}

// POST /api/v1/categories/{id}/icon/rename with {"icon_name"}
func (s *Server) handleRenameCategoryIcon(w http.ResponseWriter, r *http.Request) {
	var req iconSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	save, err := s.actingStore(r).RenameCategoryIcon(r.Context(), s.icons, id, req.IconName)
	if err != nil {
		writeIconError(w, err, "rename the category's icon")
		return
	}
	icon, err := s.store.GetCategoryIcon(id)
	if err != nil {
		writeIconError(w, err, "read the category's icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"icon": icon, "save": save})
}

// DELETE /api/v1/categories/{id}/icon
func (s *Server) handleDeleteCategoryIcon(w http.ResponseWriter, r *http.Request) {
	save, err := s.actingStore(r).DeleteCategoryIcon(r.Context(), s.icons, r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "delete the category's icon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"save": save})
}

// DELETE /api/v1/categories/{id}/icon/draft
func (s *Server) handleDiscardCategoryIconDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).DiscardCategoryIconDraft(r.PathValue("id")); err != nil {
		writeIconError(w, err, "discard the draft")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/categories/{id}/icon/revisions
func (s *Server) handleCategoryIconRevisions(w http.ResponseWriter, r *http.Request) {
	revs, err := s.store.ListCategoryIconRevisions(r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "list the icon's versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

// GET /api/v1/icon-library
func (s *Server) handleListIconLibrary(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.ListIconLibrary()
	if err != nil {
		writeServerError(w, err, "list the icon library")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

type libraryRequest struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// POST /api/v1/icon-library with {"path", "source"}
func (s *Server) handleCreateIconLibraryFile(w http.ResponseWriter, r *http.Request) {
	var req libraryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	file, save, err := s.actingStore(r).CreateIconLibraryFile(r.Context(), s.icons, req.Path, req.Source)
	if err != nil {
		writeIconError(w, err, "add the library file")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": file, "save": save})
}

// GET /api/v1/icon-library/{id}
func (s *Server) handleGetIconLibraryFile(w http.ResponseWriter, r *http.Request) {
	file, err := s.store.GetIconLibraryFile(r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "read the library file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": file})
}

// PUT /api/v1/icon-library/{id} with {"source"}
func (s *Server) handleSaveIconLibraryFile(w http.ResponseWriter, r *http.Request) {
	var req libraryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	save, err := s.actingStore(r).SaveIconLibraryFile(r.Context(), s.icons, id, req.Source)
	if err != nil {
		writeIconError(w, err, "save the library file")
		return
	}
	s.answerLibraryFile(w, id, save)
}

// POST /api/v1/icon-library/{id}/rename with {"path"}
func (s *Server) handleRenameIconLibraryFile(w http.ResponseWriter, r *http.Request) {
	var req libraryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	save, err := s.actingStore(r).RenameIconLibraryFile(r.Context(), s.icons, id, req.Path)
	if err != nil {
		writeIconError(w, err, "rename the library file")
		return
	}
	s.answerLibraryFile(w, id, save)
}

func (s *Server) answerLibraryFile(w http.ResponseWriter, id string, save *models.IconSave) {
	file, err := s.store.GetIconLibraryFile(id)
	if err != nil {
		writeIconError(w, err, "read the library file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": file, "save": save})
}

// DELETE /api/v1/icon-library/{id}
func (s *Server) handleDeleteIconLibraryFile(w http.ResponseWriter, r *http.Request) {
	save, err := s.actingStore(r).DeleteIconLibraryFile(r.Context(), s.icons, r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "delete the library file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"save": save})
}

// DELETE /api/v1/icon-library/{id}/draft
func (s *Server) handleDiscardIconLibraryDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.actingStore(r).DiscardIconLibraryDraft(r.PathValue("id")); err != nil {
		writeIconError(w, err, "discard the draft")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/icon-library/{id}/revisions
func (s *Server) handleIconLibraryRevisions(w http.ResponseWriter, r *http.Request) {
	revs, err := s.store.ListIconLibraryRevisions(r.PathValue("id"))
	if err != nil {
		writeIconError(w, err, "list the file's versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

// removeCategoryIcon takes a category's icon away before the category
// goes, refused if other icons still use it. Without the compiler, icons
// are off and the icon simply goes with the category.
func (s *Server) removeCategoryIcon(r *http.Request, id string) error {
	if id == store.UncategorizedCategoryID || !s.icons.Available() {
		return nil
	}
	if _, err := s.store.GetCategoryIcon(id); errors.Is(err, store.ErrCategoryIconNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := s.actingStore(r).DeleteCategoryIcon(r.Context(), s.icons, id)
	return err
}
