package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/attachments"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/photos"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// The photo routes, from before attachments (docs/data-model.md#attachments),
// kept as they were: an asset's photos are its image attachments, and each
// route answers with the asset, as it always did. An asset's invoice is
// left out and refused as attachments' routes do, without purchases.read.

// handleUploadPhoto adds a photo to an asset, as an image attachment, from
// a multipart form with the photo in its "file" part:
// POST /api/v1/assets/{id}/photos
func (s *Server) handleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	data, filename, ok := readUpload(w, r, min(int64(photos.MaxBytes), s.cfg.Attachments.MaxFileBytes()), "Photos")
	if !ok {
		return
	}
	prepared, err := photos.Prepare(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	a := models.Attachment{Name: filename}
	p := &attachments.Prepared{Kind: models.AttachmentImage, ContentType: prepared.ContentType, Full: prepared.Full, Thumb: prepared.Thumb, Width: prepared.Width, Height: prepared.Height}
	if err := s.saveAttachment(r.Context(), &a, p, filename); err != nil {
		writeServerError(w, err, "store the photo")
		return
	}
	if _, err := s.actingStore(r).AddAttachments(models.OwnerAsset, r.PathValue("id"), []models.Attachment{a}, s.cfg.Attachments.MaxPerObject); err != nil {
		s.removeAttachmentFiles(r.Context(), a)
		writeAttachmentError(w, err, "add the photo")
		return
	}
	s.writeAsset(w, r, http.StatusCreated)
}

// writeAsset answers with the asset a photo route changed, as it is now.
func (s *Server) writeAsset(w http.ResponseWriter, r *http.Request, status int) {
	asset, err := s.store.GetAssetByID(r.PathValue("id"))
	if err != nil {
		writeAttachmentError(w, err, "read the asset")
		return
	}
	writeJSON(w, status, map[string]any{"asset": s.shown(r, asset)})
}

// handleGetPhoto serves a photo, or with ?size=thumb its thumbnail:
// GET /api/v1/assets/{id}/photos/{photo_id}
func (s *Server) handleGetPhoto(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetFor(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	a, err := s.store.GetAttachment(models.OwnerAsset, r.PathValue("id"), r.PathValue("photo_id"))
	if errors.Is(err, store.ErrAttachmentNotFound) || err == nil && (a.Kind != models.AttachmentImage || a.ID == hiddenInvoice(r.Context(), asset)) {
		err = store.ErrPhotoNotFound
	}
	if err != nil {
		writeAttachmentError(w, err, "read the photo")
		return
	}
	s.serveAttachment(w, r, *a)
}

// handleRemovePhoto removes a photo: DELETE /api/v1/assets/{id}/photos/{photo_id}
func (s *Server) handleRemovePhoto(w http.ResponseWriter, r *http.Request) {
	had, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite)
	if !ok {
		return
	}
	if r.PathValue("photo_id") == hiddenInvoice(r.Context(), had) {
		writeAttachmentError(w, store.ErrPhotoNotFound, "remove the photo")
		return
	}
	asset, removed, err := s.actingStore(r).RemovePhoto(r.PathValue("id"), r.PathValue("photo_id"))
	if err != nil {
		writeAttachmentError(w, err, "remove the photo")
		return
	}
	s.removeAttachmentFiles(r.Context(), models.Attachment{ID: removed.ID, ThumbID: removed.ThumbID})
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleReorderPhotos puts an asset's photos in order, the first being the
// one lists show, among its other attachments, which stay where they are:
// PUT /api/v1/assets/{id}/photos with {"photo_ids": [...]}
func (s *Server) handleReorderPhotos(w http.ResponseWriter, r *http.Request) {
	had, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite)
	if !ok {
		return
	}
	var req struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhotoIDs == nil {
		writeJSONError(w, http.StatusBadRequest, "photo_ids (a list) is required")
		return
	}
	order := req.PhotoIDs
	if hidden := hiddenInvoice(r.Context(), had); hidden != "" {
		have := make([]string, len(had.Photos))
		for i, p := range had.Photos {
			have[i] = p.ID
		}
		var err error
		if order, err = withHidden(order, have, hidden); err != nil {
			writeAttachmentError(w, store.ErrPhotoOrder, "order the photos")
			return
		}
	}
	asset, err := s.store.ReorderPhotos(r.PathValue("id"), order)
	if err != nil {
		writeAttachmentError(w, err, "order the photos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleCopyPhotos gives an asset copies of another's photos, after its
// attachments, each with files of its own, so that removing one leaves the
// other: POST /api/v1/assets/{id}/photos/copy with {"from_asset_id": "..."}.
// POST /api/v1/assets/{id}/attachments/copy copies every attachment.
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
	theirs, err := s.store.ListAttachments(models.OwnerAsset, from.Asset.ID)
	if err != nil {
		writeServerError(w, err, "list the photos")
		return
	}
	var images []models.Attachment
	for _, a := range withoutHidden(theirs, hiddenInvoice(r.Context(), from)) {
		if a.Kind == models.AttachmentImage {
			images = append(images, a)
		}
	}
	copies, ok := s.copyAttachmentFiles(w, r, images)
	if !ok {
		return
	}
	if _, err := s.actingStore(r).AddAttachments(models.OwnerAsset, r.PathValue("id"), copies, s.cfg.Attachments.MaxPerObject); err != nil {
		for _, c := range copies {
			s.removeAttachmentFiles(r.Context(), c)
		}
		writeAttachmentError(w, err, "copy the photos")
		return
	}
	s.writeAsset(w, r, http.StatusOK)
}
