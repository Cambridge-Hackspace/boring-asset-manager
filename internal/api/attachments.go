package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/attachments"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/photos"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// Attachments (docs/data-model.md#attachments): files on assets,
// categories, vendors, contracts and purchase orders, grown out of assets'
// photos, whose routes are
// kept as aliases (photos.go). An asset's follow assets.write to change and
// its read access, scopes and all, to see; a category's follow
// categories.manage to change, and assets.read, as the category itself
// does, to see; a vendor's follow vendors.manage in its scopes to change,
// and seeing the vendor to see; and a contract's and a purchase order's
// follow its vendor's.

// multipartSlack is the room a multipart form takes beyond its file.
const multipartSlack = 64 << 10

// attachmentBodyCeiling is the most an upload's body may be, whatever the
// configuration: the most attachments.max_file_mb allows. Each upload is
// then held to the configured limit.
const attachmentBodyCeiling = config.MaxAttachmentMB<<20 + multipartSlack

// isAttachmentUpload reports whether a POST to path uploads an attachment
// or a photo: to /api/v1/assets/{id}/attachments, /api/v1/assets/{id}/photos,
// /api/v1/categories/{id}/attachments, /api/v1/vendors/{id}/attachments or
// /api/v1/contracts/{id}/attachments, /api/v1/purchase-orders/{id}/attachments,
// or with a comment, to /api/v1/{assets,categories}/{id}/comments/attachments.
func isAttachmentUpload(path string) bool {
	for _, p := range []struct{ prefix, suffix string }{
		{"/api/v1/assets/", "/attachments"},
		{"/api/v1/assets/", "/photos"},
		{"/api/v1/categories/", "/attachments"},
		{"/api/v1/vendors/", "/attachments"},
		{"/api/v1/contracts/", "/attachments"},
		{"/api/v1/purchase-orders/", "/attachments"},
		{"/api/v1/assets/", "/comments/attachments"},
		{"/api/v1/categories/", "/comments/attachments"},
	} {
		if rest, ok := strings.CutPrefix(path, p.prefix); ok && strings.HasSuffix(rest, p.suffix) && !strings.Contains(strings.TrimSuffix(rest, p.suffix), "/") {
			return true
		}
	}
	return false
}

// attachmentOwner is what a request's attachments belong to, once the
// requester is known to see it, and to change it when that was asked.
type attachmentOwner struct {
	kind, id, name string
	// who is set when the requester may see who added each attachment:
	// those who track the asset, as its history shows them.
	who bool
}

// ownerFor reads the owner a request is about. An asset is checked with
// assetFor, for write when change is set, and a vendor with vendorFor; a
// category's permission is the route's. When it fails, it answers the
// request.
func (s *Server) ownerFor(w http.ResponseWriter, r *http.Request, kind string, change bool) (*attachmentOwner, bool) {
	id := r.PathValue("id")
	switch kind {
	case models.OwnerAsset:
		var perms []string
		if change {
			perms = []string{rbac.PermAssetsWrite}
		}
		a, ok := s.assetFor(w, r, id, perms...)
		if !ok {
			return nil, false
		}
		return &attachmentOwner{kind: kind, id: a.Asset.ID, name: a.Asset.Name, who: Allows(r.Context(), rbac.PermAssetsTrack, a.Scopes)}, true
	case models.OwnerVendor:
		v, scopes, ok := s.vendorFor(w, r, id, change)
		if !ok {
			return nil, false
		}
		return &attachmentOwner{kind: kind, id: v.ID, name: v.Name, who: tracksVendor(r.Context(), scopes)}, true
	case models.OwnerContract:
		c, scopes, ok := s.contractFor(w, r, id, change)
		if !ok {
			return nil, false
		}
		return &attachmentOwner{kind: kind, id: c.ID, name: c.Name, who: tracksVendor(r.Context(), scopes)}, true
	case models.OwnerOrder:
		o, scopes, ok := s.orderFor(w, r, id, change)
		if !ok {
			return nil, false
		}
		return &attachmentOwner{kind: kind, id: o.ID, name: o.Reference, who: tracksVendor(r.Context(), scopes)}, true
	default:
		c, err := s.store.GetCategoryByID(id)
		if errors.Is(err, store.ErrCategoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "Category not found")
			return nil, false
		}
		if err != nil {
			writeServerError(w, err, "query category")
			return nil, false
		}
		return &attachmentOwner{kind: kind, id: c.ID, name: c.Name, who: rbac.HasPermission(UserPermissions(r.Context()), rbac.PermAssetsTrack)}, true
	}
}

// AttachmentLimits are an instance's limits on attachments, for the web to
// say before an upload is refused.
type AttachmentLimits struct {
	MaxFileBytes  int64 `json:"max_file_bytes"`
	MaxImageBytes int64 `json:"max_image_bytes"`
	MaxPerObject  int   `json:"max_per_object"`
	// AllowedTypes are the types beyond images and documents that may be
	// attached.
	AllowedTypes []string `json:"allowed_types"`
}

func (s *Server) attachmentLimits() AttachmentLimits {
	c := s.cfg.Attachments
	image := min(int64(photos.MaxBytes), c.MaxFileBytes())
	allowed := c.AllowedTypes
	if allowed == nil {
		allowed = []string{}
	}
	return AttachmentLimits{MaxFileBytes: c.MaxFileBytes(), MaxImageBytes: image, MaxPerObject: c.MaxPerObject, AllowedTypes: allowed}
}

// shownAttachments are attachments as the requester may see them: who
// added each only to those who may see it.
func shownAttachments(o *attachmentOwner, as []models.Attachment) []models.Attachment {
	out := make([]models.Attachment, len(as))
	for i, a := range as {
		if !o.who {
			a.AddedBy, a.AddedByName = "", ""
		}
		out[i] = a
	}
	return out
}

func (s *Server) writeAttachments(w http.ResponseWriter, status int, o *attachmentOwner, as []models.Attachment, added *models.Attachment) {
	body := map[string]any{"attachments": shownAttachments(o, as), "limits": s.attachmentLimits()}
	if added != nil {
		body["attachment"] = shownAttachments(o, []models.Attachment{*added})[0]
	}
	writeJSON(w, status, body)
}

// handleListAttachments lists an owner's attachments, in order, with the
// limits on them: GET /api/v1/{assets,categories}/{id}/attachments
func (s *Server) handleListAttachments(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, false)
		if !ok {
			return
		}
		as, err := s.store.ListAttachments(o.kind, o.id)
		if err != nil {
			writeServerError(w, err, "list the attachments")
			return
		}
		s.writeAttachments(w, http.StatusOK, o, as, nil)
	}
}

// readUpload reads the file of a multipart form's "file" part, held to
// limit bytes, answering the request when it cannot.
func readUpload(w http.ResponseWriter, r *http.Request, limit int64, what string) ([]byte, string, bool) {
	tooBig := fmt.Sprintf("%s are limited to %d MB", what, limit>>20)
	if r.ContentLength > limit+multipartSlack {
		writeJSONError(w, http.StatusRequestEntityTooLarge, tooBig)
		return nil, "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartSlack)
	file, header, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, tooBig)
			return nil, "", false
		}
		writeJSONError(w, http.StatusBadRequest, `Expected a multipart form with the file in a "file" field`)
		return nil, "", false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to read the uploaded file")
		return nil, "", false
	}
	if int64(len(data)) > limit {
		writeJSONError(w, http.StatusRequestEntityTooLarge, tooBig)
		return nil, "", false
	}
	return data, cleanFilename(header.Filename), true
}

// handleAddAttachment adds a file to an owner, after its others, from a
// multipart form with the file in its "file" part, and optionally a
// "name" and a "description": POST /api/v1/{assets,categories}/{id}/attachments
func (s *Server) handleAddAttachment(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, true)
		if !ok {
			return
		}
		as, added, ok := s.addAttachment(w, r, o)
		if ok {
			s.writeAttachments(w, http.StatusCreated, o, as, added)
		}
	}
}

// addAttachment adds the file a request uploads to an owner the requester
// may add to, after its others, answering the request when it cannot. It
// returns the owner's attachments, and the one added.
func (s *Server) addAttachment(w http.ResponseWriter, r *http.Request, o *attachmentOwner) ([]models.Attachment, *models.Attachment, bool) {
	data, filename, ok := readUpload(w, r, s.cfg.Attachments.MaxFileBytes(), "Attachments")
	if !ok {
		return nil, nil, false
	}
	prepared, err := attachments.Prepare(data, filename, s.cfg.Attachments.AllowedTypes)
	if errors.Is(err, attachments.ErrImageTooBig) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
		return nil, nil, false
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	a := models.Attachment{Name: strings.TrimSpace(r.FormValue("name")), Description: strings.TrimSpace(r.FormValue("description"))}
	if a.Name == "" {
		a.Name = filename
	}
	if len([]rune(a.Name)) > store.MaxAttachmentName {
		writeJSONError(w, http.StatusBadRequest, store.ErrAttachmentName.Error())
		return nil, nil, false
	}
	if len([]rune(a.Description)) > store.MaxAttachmentDescription {
		writeJSONError(w, http.StatusBadRequest, store.ErrAttachmentDescription.Error())
		return nil, nil, false
	}
	if err := s.saveAttachment(r.Context(), &a, prepared, filename); err != nil {
		writeServerError(w, err, "store the attachment")
		return nil, nil, false
	}
	as, err := s.actingStore(r).AddAttachments(o.kind, o.id, []models.Attachment{a}, s.cfg.Attachments.MaxPerObject)
	if err != nil {
		s.removeAttachmentFiles(r.Context(), a)
		writeAttachmentError(w, err, "add the attachment")
		return nil, nil, false
	}
	for i := range as {
		if as[i].ID == a.ID {
			return as, &as[i], true
		}
	}
	return as, nil, true
}

// saveAttachment stores a prepared upload's files, filling in a's file and
// type.
func (s *Server) saveAttachment(ctx context.Context, a *models.Attachment, p *attachments.Prepared, filename string) error {
	a.ID, a.Kind, a.Filename, a.ContentType = uuid.NewString(), p.Kind, filename, p.ContentType
	a.ByteSize, a.CreatedAt = int64(len(p.Full)), time.Now().UTC()
	if p.Thumb != nil {
		a.ThumbID, a.WidthPx, a.HeightPx = uuid.NewString(), p.Width, p.Height
	}
	if err := s.photos.Save(ctx, a.ID, p.Full); err != nil {
		return err
	}
	if p.Thumb != nil {
		if err := s.photos.Save(ctx, a.ThumbID, p.Thumb); err != nil {
			s.removeAttachmentFiles(ctx, *a)
			return err
		}
	}
	return nil
}

// handleGetAttachment serves an attachment: an image as photos always
// were, inline, or with ?size=thumb its thumbnail; anything else as a
// download, which a browser never shows, let alone runs:
// GET /api/v1/{assets,categories}/{id}/attachments/{attachment_id}
func (s *Server) handleGetAttachment(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, false)
		if !ok {
			return
		}
		a, err := s.store.GetAttachment(o.kind, o.id, r.PathValue("attachment_id"))
		if err != nil {
			writeAttachmentError(w, err, "read the attachment")
			return
		}
		s.serveAttachment(w, r, *a)
	}
}

// serveAttachment writes an attachment's file. Every one is served with
// nosniff and a sandboxing Content-Security-Policy, so that even a file
// opened directly runs nothing.
func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, a models.Attachment) {
	file := a.ID
	if r.URL.Query().Get("size") == "thumb" && a.ThumbID != "" {
		file = a.ThumbID
	}
	rc, err := s.photos.Open(r.Context(), file)
	if err != nil {
		writeServerError(w, err, "read the attachment's file")
		return
	}
	defer rc.Close()
	contentType, inline := attachments.Served(a)
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "private, max-age=86400, immutable")
	if !inline {
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})
		if disposition == "" {
			disposition = fmt.Sprintf(`attachment; filename="%s"`, sanitizeForHeader(a.Filename))
		}
		h.Set("Content-Disposition", disposition)
	}
	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("serving attachment %s: %v", a.ID, err)
	}
}

// AttachmentRequest renames or describes an attachment: each that is set.
type AttachmentRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// handleUpdateAttachment renames or describes an attachment:
// PUT /api/v1/{assets,categories}/{id}/attachments/{attachment_id}
func (s *Server) handleUpdateAttachment(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, true)
		if !ok {
			return
		}
		var req AttachmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		a, err := s.store.UpdateAttachment(o.kind, o.id, r.PathValue("attachment_id"), req.Name, req.Description)
		if err != nil {
			writeAttachmentError(w, err, "change the attachment")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"attachment": shownAttachments(o, []models.Attachment{*a})[0]})
	}
}

// handleRemoveAttachment removes an attachment, and its files:
// DELETE /api/v1/{assets,categories}/{id}/attachments/{attachment_id}
func (s *Server) handleRemoveAttachment(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, true)
		if !ok {
			return
		}
		removed, err := s.actingStore(r).RemoveAttachment(o.kind, o.id, r.PathValue("attachment_id"))
		if err != nil {
			writeAttachmentError(w, err, "remove the attachment")
			return
		}
		s.removeAttachmentFiles(r.Context(), *removed)
		as, err := s.store.ListAttachments(o.kind, o.id)
		if err != nil {
			writeServerError(w, err, "list the attachments")
			return
		}
		s.writeAttachments(w, http.StatusOK, o, as, nil)
	}
}

// handleReorderAttachments puts an owner's attachments in order: PUT
// /api/v1/{assets,categories}/{id}/attachments with {"attachment_ids": [...]}
func (s *Server) handleReorderAttachments(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.ownerFor(w, r, kind, true)
		if !ok {
			return
		}
		var req struct {
			AttachmentIDs []string `json:"attachment_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AttachmentIDs == nil {
			writeJSONError(w, http.StatusBadRequest, "attachment_ids (a list) is required")
			return
		}
		as, err := s.store.ReorderAttachments(o.kind, o.id, req.AttachmentIDs)
		if err != nil {
			writeAttachmentError(w, err, "order the attachments")
			return
		}
		s.writeAttachments(w, http.StatusOK, o, as, nil)
	}
}

// handleCopyAttachments gives an asset copies of another's attachments,
// after its own, each with files of its own, so that removing one leaves
// the other: POST /api/v1/assets/{id}/attachments/copy with
// {"from_asset_id": "..."}
func (s *Server) handleCopyAttachments(w http.ResponseWriter, r *http.Request) {
	o, ok := s.ownerFor(w, r, models.OwnerAsset, true)
	if !ok {
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
		writeServerError(w, err, "list the attachments")
		return
	}
	copies, ok := s.copyAttachmentFiles(w, r, theirs)
	if !ok {
		return
	}
	as, err := s.actingStore(r).AddAttachments(o.kind, o.id, copies, s.cfg.Attachments.MaxPerObject)
	if err != nil {
		for _, c := range copies {
			s.removeAttachmentFiles(r.Context(), c)
		}
		writeAttachmentError(w, err, "copy the attachments")
		return
	}
	s.writeAttachments(w, http.StatusOK, o, as, nil)
}

// copyAttachmentFiles copies attachments' files, as new attachments with
// files of their own. When it fails, it removes what it copied and answers
// the request.
func (s *Server) copyAttachmentFiles(w http.ResponseWriter, r *http.Request, from []models.Attachment) ([]models.Attachment, bool) {
	copies := make([]models.Attachment, 0, len(from))
	undo := func() {
		for _, c := range copies {
			s.removeAttachmentFiles(r.Context(), c)
		}
	}
	for _, a := range from {
		c := a
		c.ID, c.CreatedAt = uuid.NewString(), time.Now().UTC()
		files := map[string]string{c.ID: a.ID}
		if a.ThumbID != "" {
			c.ThumbID = uuid.NewString()
			files[c.ThumbID] = a.ThumbID
		}
		for to, of := range files {
			data, err := s.photos.Read(r.Context(), of)
			if err == nil {
				err = s.photos.Save(r.Context(), to, data)
			}
			if err != nil {
				s.removeAttachmentFiles(r.Context(), c)
				undo()
				writeServerError(w, err, "copy the attachments' files")
				return nil, false
			}
		}
		copies = append(copies, c)
	}
	return copies, true
}

// removeAttachmentFiles removes an attachment's files. A file left behind
// wastes space but harms nothing, and bam storage clean removes it, so a
// failure is logged rather than reported.
func (s *Server) removeAttachmentFiles(ctx context.Context, a models.Attachment) {
	for _, id := range []string{a.ID, a.ThumbID} {
		if id == "" {
			continue
		}
		if err := s.photos.Remove(ctx, id); err != nil {
			log.Printf("removing attachment file %s: %v", id, err)
		}
	}
}

func writeAttachmentError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrVendorNotFound):
		writeJSONError(w, http.StatusNotFound, "Vendor not found")
	case errors.Is(err, store.ErrAttachmentNotFound):
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
	case errors.Is(err, store.ErrPhotoNotFound):
		writeJSONError(w, http.StatusNotFound, "Photo not found")
	case errors.Is(err, store.ErrTooManyAttachments), errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrAttachmentOrder), errors.Is(err, store.ErrPhotoOrder),
		errors.Is(err, store.ErrAttachmentName), errors.Is(err, store.ErrAttachmentDescription):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
