package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// Packages (README §2.3, #144): part of an instance, chosen with what it
// needs, exported to share. A selection may be too long for a URL, so it is
// posted first, then downloaded by the browser as it is written, at any
// size. A package holds only what the requester may read: the selection is
// worked out again, with their permissions as they are, when it downloads.

// exportBodyLimit fits a selection and a preview image, base64.
const exportBodyLimit = 2 << 20

// exportTTL is how long a posted export waits to be downloaded.
const exportTTL = time.Hour

// pendingExport is an export posted and not yet downloaded.
type pendingExport struct {
	userID  string
	sel     archive.Selection
	info    archive.PackageInfo
	preview []byte
	made    time.Time
}

// exportsPending keeps exports posted in the last hour.
type exportsPending struct {
	mu sync.Mutex
	m  map[string]*pendingExport
}

func (e *exportsPending) add(p *pendingExport) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.m == nil {
		e.m = map[string]*pendingExport{}
	}
	for id, old := range e.m {
		if time.Since(old.made) > exportTTL {
			delete(e.m, id)
		}
	}
	id := uuid.NewString()
	e.m[id] = p
	return id
}

func (e *exportsPending) get(id, userID string) (*pendingExport, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, ok := e.m[id]
	if !ok || p.userID != userID || time.Since(p.made) > exportTTL {
		return nil, false
	}
	return p, true
}

// readerOf is what a request may read, for packaging.
func readerOf(r *http.Request) archive.Reader {
	perms := UserPermissions(r.Context())
	return func(perm string, scopes []string) bool {
		if scopes == nil {
			return rbac.HasPermission(perms, perm)
		}
		return rbac.Allows(perms, perm, scopes)
	}
}

// exportRequest is what POST /api/v1/exports takes; the plan takes its
// selection alone.
type exportRequest struct {
	Selection   archive.Selection `json:"selection"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	License     string            `json:"license"`
	Version     string            `json:"version"`
	// Preview is an image, PNG, JPEG or WebP, base64.
	Preview []byte `json:"preview"`
}

// writeSelectionError answers a selection that cannot be packaged.
func writeSelectionError(w http.ResponseWriter, err error, action string) {
	if errors.Is(err, archive.ErrSelection) {
		writeJSONError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), archive.ErrSelection.Error()+": "))
		return
	}
	writeServerError(w, err, action)
}

// signedIn answers 401 for a visitor who is not signed in: an export is
// kept for whoever made it, by their user or token.
func signedIn(w http.ResponseWriter, r *http.Request) bool {
	if actorOf(r) == "" {
		writeJSONError(w, http.StatusUnauthorized, "Sign in to export a package")
		return false
	}
	return true
}

// handleExportPlan says what a selection packages: what was chosen, what
// needs what, the assets, and what stays behind:
// POST /api/v1/exports/plan with {"selection"}.
func (s *Server) handleExportPlan(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if !signedIn(w, r) || !decodeJSON(w, r, &req) {
		return
	}
	// a large instance takes a while to work out
	takeLonger(w)
	plan, err := archive.Resolve(r.Context(), s.store, s.store.DB, req.Selection, readerOf(r))
	if err != nil {
		writeSelectionError(w, err, "work out the package")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// handleCreateExport checks a selection and keeps it to download, with
// what the package says of itself: POST /api/v1/exports. It answers 201
// with the export's ID, where to download it, and its plan.
func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if !signedIn(w, r) || !decodeJSON(w, r, &req) {
		return
	}
	takeLonger(w)
	info := archive.PackageInfo{
		Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), Author: strings.TrimSpace(req.Author),
		License: strings.TrimSpace(req.License), Version: strings.TrimSpace(req.Version),
	}
	if info.Name == "" || len(info.Name) > 200 {
		writeJSONError(w, http.StatusBadRequest, "A package needs a name, of at most 200 characters")
		return
	}
	if len(info.Description) > 4000 || len(info.Author) > 200 || len(info.License) > 200 || len(info.Version) > 64 {
		writeJSONError(w, http.StatusBadRequest, "A description is at most 4,000 characters; an author or license 200; a version 64")
		return
	}
	if len(req.Preview) > 0 {
		if _, err := archive.PreviewPath(req.Preview); err != nil {
			writeSelectionError(w, err, "check the preview")
			return
		}
	}
	plan, err := archive.Resolve(r.Context(), s.store, s.store.DB, req.Selection, readerOf(r))
	if err != nil {
		writeSelectionError(w, err, "work out the package")
		return
	}
	id := s.exports.add(&pendingExport{userID: actorOf(r), sel: req.Selection, info: info, preview: req.Preview, made: time.Now()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "download": "/api/v1/exports/" + id, "plan": plan})
}

// handleDownloadExport streams a package posted by the same person, as it
// is written: GET /api/v1/exports/{id}.
func (s *Server) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	w, r = transfer(w, r)
	if !signedIn(w, r) {
		return
	}
	p, ok := s.exports.get(r.PathValue("id"), actorOf(r))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "Export not found, or more than an hour old")
		return
	}
	name := sanitizeForHeader(strings.Map(func(c rune) rune {
		if c == '/' || c == '\\' || c == '"' {
			return '-'
		}
		return c
	}, p.info.Name))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.bam.tar.gz"`, name))
	w.Header().Set("Cache-Control", "no-store")
	started := false
	open := func(int) (io.WriteCloser, error) {
		started = true
		return nopWriteCloser{w}, nil
	}
	_, err := archive.Write(r.Context(), s.store, s.blobs, open, archive.Options{
		BamVersion: Version, Select: &p.sel, May: readerOf(r), Info: &p.info, Preview: p.preview,
	})
	if err != nil {
		if !started {
			writeSelectionError(w, err, "write the package")
			return
		}
		log.Printf("writing a package: %v", err)
		// Part written: the browser must see the download fail, not end.
		panic(http.ErrAbortHandler)
	}
}
