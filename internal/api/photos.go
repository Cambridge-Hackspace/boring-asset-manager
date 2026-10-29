package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/photos"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// photoUploadLimit leaves headroom over photos.MaxBytes for multipart
// framing.
const photoUploadLimit = photos.MaxBytes + 64<<10

// handleUploadPhoto adds a photo to an asset, from a multipart form with the
// photo in its "file" part: POST /api/v1/assets/{id}/photos
func (s *Server) handleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	tooBig := fmt.Sprintf("Photos are limited to %d MB", photos.MaxBytes>>20)
	file, header, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, tooBig)
			return
		}
		writeJSONError(w, http.StatusBadRequest, `Expected a multipart form with the photo in a "file" field`)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, photos.MaxBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to read the uploaded file")
		return
	}
	if len(data) > photos.MaxBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, tooBig)
		return
	}
	prepared, err := photos.Prepare(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := models.AssetPhoto{
		ID:          uuid.NewString(),
		ThumbID:     uuid.NewString(),
		Filename:    cleanFilename(header.Filename),
		ContentType: prepared.ContentType,
		WidthPx:     prepared.Width,
		HeightPx:    prepared.Height,
		ByteSize:    int64(len(prepared.Full)),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.photos.Save(r.Context(), p.ID, prepared.Full); err != nil {
		writeServerError(w, err, "store the photo")
		return
	}
	if err := s.photos.Save(r.Context(), p.ThumbID, prepared.Thumb); err != nil {
		s.removePhotoFiles(r.Context(), p)
		writeServerError(w, err, "store the photo")
		return
	}
	asset, err := s.actingStore(r).AddPhoto(r.PathValue("id"), p)
	if err != nil {
		s.removePhotoFiles(r.Context(), p)
		writePhotoError(w, err, "add the photo")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"asset": s.shown(r, asset)})
}

// handleGetPhoto serves a photo, or with ?size=thumb its thumbnail:
// GET /api/v1/assets/{id}/photos/{photo_id}
func (s *Server) handleGetPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id")); !ok {
		return
	}
	p, err := s.store.GetPhoto(r.PathValue("id"), r.PathValue("photo_id"))
	if err != nil {
		writePhotoError(w, err, "read the photo")
		return
	}
	file := p.ID
	if r.URL.Query().Get("size") == "thumb" {
		file = p.ThumbID
	}
	data, err := s.photos.Read(r.Context(), file)
	if err != nil {
		writeServerError(w, err, "read the photo file")
		return
	}
	w.Header().Set("Content-Type", p.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	_, _ = w.Write(data)
}

// handleRemovePhoto removes a photo: DELETE /api/v1/assets/{id}/photos/{photo_id}
func (s *Server) handleRemovePhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	asset, removed, err := s.actingStore(r).RemovePhoto(r.PathValue("id"), r.PathValue("photo_id"))
	if err != nil {
		writePhotoError(w, err, "remove the photo")
		return
	}
	s.removePhotoFiles(r.Context(), *removed)
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleReorderPhotos puts an asset's photos in order, the first being the
// one lists show: PUT /api/v1/assets/{id}/photos with {"photo_ids": [...]}
func (s *Server) handleReorderPhotos(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	var req struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhotoIDs == nil {
		writeJSONError(w, http.StatusBadRequest, "photo_ids (a list) is required")
		return
	}
	asset, err := s.store.ReorderPhotos(r.PathValue("id"), req.PhotoIDs)
	if err != nil {
		writePhotoError(w, err, "order the photos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleCopyPhotos gives an asset copies of another's photos, after its own,
// each with files of its own, so that removing one leaves the other:
// POST /api/v1/assets/{id}/photos/copy with {"from_asset_id": "..."}
func (s *Server) handleCopyPhotos(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	var req struct {
		FromAssetID string `json:"from_asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromAssetID == "" {
		writeJSONError(w, http.StatusBadRequest, "from_asset_id is required")
		return
	}
	from, ok := s.assetFor(w, r, req.FromAssetID)
	if !ok {
		return
	}
	copies := make([]models.AssetPhoto, 0, len(from.Photos))
	undo := func() {
		for _, p := range copies {
			s.removePhotoFiles(r.Context(), p)
		}
	}
	for _, p := range from.Photos {
		c := p
		c.ID, c.ThumbID, c.CreatedAt = uuid.NewString(), uuid.NewString(), time.Now().UTC()
		for to, of := range map[string]string{c.ID: p.ID, c.ThumbID: p.ThumbID} {
			data, err := s.photos.Read(r.Context(), of)
			if err == nil {
				err = s.photos.Save(r.Context(), to, data)
			}
			if err != nil {
				s.removePhotoFiles(r.Context(), c)
				undo()
				writeServerError(w, err, "copy the photo files")
				return
			}
		}
		copies = append(copies, c)
	}
	asset, err := s.actingStore(r).AddPhotos(r.PathValue("id"), copies)
	if err != nil {
		undo()
		writePhotoError(w, err, "copy the photos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// removePhotoFiles removes a photo's files. A file left behind wastes space
// but harms nothing, so a failure is logged rather than reported.
func (s *Server) removePhotoFiles(ctx context.Context, p models.AssetPhoto) {
	for _, id := range []string{p.ID, p.ThumbID} {
		if err := s.photos.Remove(ctx, id); err != nil {
			log.Printf("removing photo file %s: %v", id, err)
		}
	}
}

func writePhotoError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrPhotoNotFound):
		writeJSONError(w, http.StatusNotFound, "Photo not found")
	case errors.Is(err, store.ErrTooManyPhotos), errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrPhotoOrder):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
