package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// DefaultMaxAttachments is how many attachments an asset, a category or a
// vendor may have, unless the configuration says otherwise.
const DefaultMaxAttachments = 50

var (
	ErrAttachmentNotFound = errors.New("attachment not found")
	// ErrTooManyAttachments refuses attachments past an owner's limit.
	ErrTooManyAttachments = errors.New("too many attachments")
	// ErrAttachmentOrder refuses an order that is not exactly the owner's
	// attachments.
	ErrAttachmentOrder = errors.New("the order must name each of its attachments once")
	// ErrPhotoNotFound is an asset's image attachment not found, by the
	// photo routes.
	ErrPhotoNotFound = errors.New("photo not found")
	// ErrPhotoOrder refuses an order that is not exactly the asset's
	// photos.
	ErrPhotoOrder = errors.New("the order must name each of the asset's photos once")
	// ErrTooManyPhotos is ErrTooManyAttachments, as the photo routes knew
	// it.
	ErrTooManyPhotos = ErrTooManyAttachments
)

// attachmentOwner is where an owner's attachments are kept: a table of
// their own, by the owner's ID in column.
type attachmentOwner struct {
	kind, table, column, parent string
}

var attachmentOwners = map[string]attachmentOwner{
	models.OwnerAsset:    {models.OwnerAsset, "asset_attachments", "asset_id", "assets"},
	models.OwnerCategory: {models.OwnerCategory, "category_attachments", "category_id", "categories"},
	models.OwnerVendor:   {models.OwnerVendor, "vendor_attachments", "vendor_id", "vendors"},
}

// AttachmentTables are the tables attachments are kept in, by owner.
func AttachmentTables() map[string]string {
	out := map[string]string{}
	for k, o := range attachmentOwners {
		out[k] = o.table
	}
	return out
}

func ownerOf(kind string) (attachmentOwner, error) {
	o, ok := attachmentOwners[kind]
	if !ok {
		return o, fmt.Errorf("no attachments are kept for a %q", kind)
	}
	return o, nil
}

const attachmentColumns = "t.id, t.kind, t.name, t.description, t.filename, t.content_type, t.byte_size, t.thumb_id, t.width_px, t.height_px, t.added_by, t.created_at"

func attachmentSelect(o attachmentOwner) string {
	return "SELECT t." + o.column + ", " + attachmentColumns + ", u.name, u.email FROM " + o.table + " t LEFT JOIN users u ON u.id = t.added_by"
}

func scanAttachment(row interface{ Scan(...any) error }) (models.Attachment, string, error) {
	var (
		a                      models.Attachment
		ownerID                string
		thumb, by, name, email sql.NullString
		width, height          sql.NullInt64
	)
	err := row.Scan(&ownerID, &a.ID, &a.Kind, &a.Name, &a.Description, &a.Filename, &a.ContentType, &a.ByteSize, &thumb, &width, &height, &by, &a.CreatedAt, &name, &email)
	a.ThumbID, a.WidthPx, a.HeightPx, a.AddedBy = thumb.String, int(width.Int64), int(height.Int64), by.String
	a.AddedByName = name.String
	if a.AddedByName == "" {
		a.AddedByName = email.String
	}
	return a, ownerID, err
}

// readAttachments reads attachments in order, by owner.
func (s *Store) readAttachments(o attachmentOwner, where string, args ...any) (map[string][]models.Attachment, error) {
	rows, err := s.Query(attachmentSelect(o)+where+" ORDER BY t."+o.column+", t.position", args...)
	if err != nil {
		return nil, fmt.Errorf("reading attachments: %w", err)
	}
	defer rows.Close()
	out := map[string][]models.Attachment{}
	for rows.Next() {
		a, ownerID, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning attachment: %w", err)
		}
		out[ownerID] = append(out[ownerID], a)
	}
	return out, rows.Err()
}

// ListAttachments returns an owner's attachments in order. An owner that
// does not exist has none.
func (s *Store) ListAttachments(owner, ownerID string) ([]models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	all, err := s.readAttachments(o, " WHERE t."+o.column+" = ?", ownerID)
	if err != nil {
		return nil, err
	}
	if all[ownerID] == nil {
		return []models.Attachment{}, nil
	}
	return all[ownerID], nil
}

// GetAttachment returns one of an owner's attachments, or
// ErrAttachmentNotFound.
func (s *Store) GetAttachment(owner, ownerID, id string) (*models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	a, _, err := scanAttachment(s.QueryRow(attachmentSelect(o)+" WHERE t.id = ? AND t."+o.column+" = ?", id, ownerID))
	if err == sql.ErrNoRows {
		return nil, ErrAttachmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading attachment: %w", err)
	}
	return &a, nil
}

// attachmentRow is an attachment's ID and kind, in order.
type attachmentRow struct{ id, kind, name string }

// editableAttachmentsTx locks an owner that may be changed, an active asset,
// a category or a vendor, and returns its attachments in order.
func (s *Store) editableAttachmentsTx(tx *sql.Tx, o attachmentOwner, ownerID string) ([]attachmentRow, error) {
	switch o.kind {
	case models.OwnerAsset:
		if err := s.lockAssetTx(tx, ownerID); err != nil {
			return nil, err
		}
		var archived sql.NullTime
		if err := s.TxQueryRow(tx, "SELECT archived_at FROM assets WHERE id = ?", ownerID).Scan(&archived); err != nil {
			return nil, fmt.Errorf("reading asset: %w", err)
		}
		if archived.Valid {
			return nil, ErrAssetArchived
		}
	case models.OwnerCategory, models.OwnerVendor:
		res, err := s.TxExec(tx, "UPDATE "+o.parent+" SET updated_at = updated_at WHERE id = ?", ownerID)
		if err != nil {
			return nil, fmt.Errorf("locking %s: %w", o.kind, err)
		}
		// MariaDB counts rows changed, not matched, so check it exists.
		if n, _ := res.RowsAffected(); n == 0 {
			var exists int
			if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM "+o.parent+" WHERE id = ?", ownerID).Scan(&exists); err != nil {
				return nil, fmt.Errorf("reading %s: %w", o.kind, err)
			}
			if exists == 0 && o.kind == models.OwnerVendor {
				return nil, ErrVendorNotFound
			}
			if exists == 0 {
				return nil, ErrCategoryNotFound
			}
		}
	}
	rows, err := s.TxQuery(tx, "SELECT id, kind, name FROM "+o.table+" WHERE "+o.column+" = ? ORDER BY position", ownerID)
	if err != nil {
		return nil, fmt.Errorf("reading attachments: %w", err)
	}
	defer rows.Close()
	var out []attachmentRow
	for rows.Next() {
		var r attachmentRow
		if err := rows.Scan(&r.id, &r.kind, &r.name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// attachmentChange is how an owner's history records an attachment: an
// image as a photo, as it always was, and the rest as attachments.
func attachmentChange(kind, id, name string, added bool) models.EventChange {
	c := models.EventChange{What: "attachment", Key: id}
	if kind == models.AttachmentImage {
		c.What = "photo"
	}
	if added {
		c.To = name
	} else {
		c.From = name
	}
	return c
}

// recordAttachmentsTx records attachments added or removed in their
// owner's history.
func (s *Store) recordAttachmentsTx(tx *sql.Tx, o attachmentOwner, ownerID string, changes []models.EventChange) error {
	if len(changes) == 0 {
		return nil
	}
	payload := models.EventPayload{Trigger: models.TriggerManual, Changes: changes}
	switch o.kind {
	case models.OwnerCategory:
		return s.recordEvent(tx, models.EventCategoryEdited, ownerID, payload)
	case models.OwnerVendor:
		var name string
		if err := s.TxQueryRow(tx, "SELECT name FROM vendors WHERE id = ?", ownerID).Scan(&name); err != nil {
			return fmt.Errorf("reading vendor for event: %w", err)
		}
		return s.recordVendorEvent(tx, models.EventVendorEdited, ownerID, name, payload)
	}
	return s.recordAssetEvent(tx, models.EventAssetEdited, ownerID, payload)
}

// AddAttachments adds attachments, whose files are already stored, after
// an owner's others, in order, as the acting user, and records them in its
// history: all of them, or none if that would take it past max (none when
// max is 0 or less).
func (s *Store) AddAttachments(owner, ownerID string, as []models.Attachment, max int) ([]models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	have, err := s.editableAttachmentsTx(tx, o, ownerID)
	if err != nil {
		return nil, err
	}
	if len(have)+len(as) > max {
		return nil, fmt.Errorf("%w: a%s %s can have at most %d", ErrTooManyAttachments, map[bool]string{true: "n", false: ""}[o.kind == models.OwnerAsset], o.kind, max)
	}
	var addedBy *string
	if s.actor != "" {
		addedBy = &s.actor
	}
	changes := make([]models.EventChange, 0, len(as))
	for i, a := range as {
		var thumb *string
		var width, height *int
		if a.ThumbID != "" {
			thumb, width, height = &a.ThumbID, &a.WidthPx, &a.HeightPx
		}
		if a.Name == "" {
			a.Name = a.Filename
		}
		_, err = s.TxExec(tx, "INSERT INTO "+o.table+" (id, "+o.column+", kind, position, name, description, filename, content_type, byte_size, thumb_id, width_px, height_px, added_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			a.ID, ownerID, a.Kind, len(have)+i, a.Name, a.Description, a.Filename, a.ContentType, a.ByteSize, thumb, width, height, addedBy, dbTime(a.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("adding attachment: %w", err)
		}
		changes = append(changes, attachmentChange(a.Kind, a.ID, a.Name, true))
	}
	if err := s.recordAttachmentsTx(tx, o, ownerID, changes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.ListAttachments(owner, ownerID)
}

// MaxAttachmentName and MaxAttachmentDescription bound an attachment's
// name and description, in characters.
const (
	MaxAttachmentName        = 255
	MaxAttachmentDescription = 4000
)

// ErrAttachmentName refuses a name that is empty or too long, and
// ErrAttachmentDescription a description too long.
var (
	ErrAttachmentName        = fmt.Errorf("an attachment's name is from 1 to %d characters", MaxAttachmentName)
	ErrAttachmentDescription = fmt.Errorf("an attachment's description is at most %d characters", MaxAttachmentDescription)
)

// UpdateAttachment renames or describes an attachment: each of name and
// description that is not nil.
func (s *Store) UpdateAttachment(owner, ownerID, id string, name, description *string) (*models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	var sets []string
	var args []any
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len([]rune(n)) > MaxAttachmentName {
			return nil, ErrAttachmentName
		}
		sets, args = append(sets, "name = ?"), append(args, n)
	}
	if description != nil {
		d := strings.TrimSpace(*description)
		if len([]rune(d)) > MaxAttachmentDescription {
			return nil, ErrAttachmentDescription
		}
		sets, args = append(sets, "description = ?"), append(args, d)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	have, err := s.editableAttachmentsTx(tx, o, ownerID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, r := range have {
		found = found || r.id == id
	}
	if !found {
		return nil, ErrAttachmentNotFound
	}
	if len(sets) > 0 {
		if _, err := s.TxExec(tx, "UPDATE "+o.table+" SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, id)...); err != nil {
			return nil, fmt.Errorf("updating attachment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAttachment(owner, ownerID, id)
}

// RemoveAttachment removes one of an owner's attachments and records it,
// returning it so its caller can remove its files.
func (s *Store) RemoveAttachment(owner, ownerID, id string) (*models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	have, err := s.editableAttachmentsTx(tx, o, ownerID)
	if err != nil {
		return nil, err
	}
	a, _, err := scanAttachment(s.TxQueryRow(tx, attachmentSelect(o)+" WHERE t.id = ? AND t."+o.column+" = ?", id, ownerID))
	if err == sql.ErrNoRows {
		return nil, ErrAttachmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading attachment: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM "+o.table+" WHERE id = ?", id); err != nil {
		return nil, fmt.Errorf("removing attachment: %w", err)
	}
	rest := make([]string, 0, len(have))
	for _, r := range have {
		if r.id != id {
			rest = append(rest, r.id)
		}
	}
	if err := s.positionAttachmentsTx(tx, o, rest); err != nil {
		return nil, err
	}
	if err := s.recordAttachmentsTx(tx, o, ownerID, []models.EventChange{attachmentChange(a.Kind, a.ID, a.Name, false)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return &a, nil
}

// ReorderAttachments puts an owner's attachments in the order given, which
// must name each of them once. An asset's first image is the one lists
// show.
func (s *Store) ReorderAttachments(owner, ownerID string, order []string) ([]models.Attachment, error) {
	o, err := ownerOf(owner)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	have, err := s.editableAttachmentsTx(tx, o, ownerID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(have))
	for i, r := range have {
		ids[i] = r.id
	}
	if !sameIDs(ids, order) {
		return nil, ErrAttachmentOrder
	}
	if err := s.positionAttachmentsTx(tx, o, order); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.ListAttachments(owner, ownerID)
}

// sameIDs reports whether order names each of ids once.
func sameIDs(ids, order []string) bool {
	have, want := append([]string{}, ids...), append([]string{}, order...)
	sort.Strings(have)
	sort.Strings(want)
	if len(have) != len(want) {
		return false
	}
	for i := range have {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}

func (s *Store) positionAttachmentsTx(tx *sql.Tx, o attachmentOwner, ids []string) error {
	for i, id := range ids {
		if _, err := s.TxExec(tx, "UPDATE "+o.table+" SET position = ? WHERE id = ?", i, id); err != nil {
			return fmt.Errorf("ordering attachments: %w", err)
		}
	}
	return nil
}

// Photos: an asset's image attachments, as the photo routes, which came
// before attachments, see them.

// photoDetails fills in an asset's photos: from every asset's, read once,
// when ctx serves a list, and otherwise its own.
func (s *Store) photoDetails(a *models.AssetWithDetails, ctx *assetContext) error {
	o := attachmentOwners[models.OwnerAsset]
	var images map[string][]models.Attachment
	if ctx.list {
		if ctx.photos == nil {
			all, err := s.readAttachments(o, " WHERE t.kind = ?", models.AttachmentImage)
			if err != nil {
				return err
			}
			ctx.photos = all
		}
		images = ctx.photos
	} else {
		own, err := s.readAttachments(o, " WHERE t.asset_id = ? AND t.kind = ?", a.Asset.ID, models.AttachmentImage)
		if err != nil {
			return err
		}
		images = own
	}
	a.Photos = make([]models.AssetPhoto, 0, len(images[a.Asset.ID]))
	for _, img := range images[a.Asset.ID] {
		a.Photos = append(a.Photos, img.Photo())
	}
	return nil
}

// GetPhoto returns one of an asset's image attachments, or
// ErrPhotoNotFound.
func (s *Store) GetPhoto(assetID, photoID string) (*models.AssetPhoto, error) {
	a, err := s.GetAttachment(models.OwnerAsset, assetID, photoID)
	if errors.Is(err, ErrAttachmentNotFound) || err == nil && a.Kind != models.AttachmentImage {
		return nil, ErrPhotoNotFound
	}
	if err != nil {
		return nil, err
	}
	p := a.Photo()
	return &p, nil
}

// AddPhoto adds a photo, whose files are already stored, as an image
// attachment after an asset's others, and records it in its history.
func (s *Store) AddPhoto(assetID string, p models.AssetPhoto) (*models.AssetWithDetails, error) {
	return s.AddPhotos(assetID, []models.AssetPhoto{p})
}

// AddPhotos adds photos, whose files are already stored, as image
// attachments after an asset's others, in order: all of them, or none if
// that would take it past DefaultMaxAttachments.
func (s *Store) AddPhotos(assetID string, ps []models.AssetPhoto) (*models.AssetWithDetails, error) {
	if _, err := s.AddAttachments(models.OwnerAsset, assetID, PhotoAttachments(ps), DefaultMaxAttachments); err != nil {
		return nil, err
	}
	return s.GetAssetByID(assetID)
}

// PhotoAttachments are photos as image attachments, named by their files.
func PhotoAttachments(ps []models.AssetPhoto) []models.Attachment {
	out := make([]models.Attachment, len(ps))
	for i, p := range ps {
		created := p.CreatedAt
		if created.IsZero() {
			created = time.Now().UTC()
		}
		out[i] = models.Attachment{ID: p.ID, Kind: models.AttachmentImage, Name: p.Filename, Filename: p.Filename, ContentType: p.ContentType,
			ByteSize: p.ByteSize, ThumbID: p.ThumbID, WidthPx: p.WidthPx, HeightPx: p.HeightPx, CreatedAt: created}
	}
	return out
}

// RemovePhoto removes one of an asset's image attachments and records it,
// returning it so its caller can remove its files.
func (s *Store) RemovePhoto(assetID, photoID string) (*models.AssetWithDetails, *models.AssetPhoto, error) {
	if _, err := s.GetPhoto(assetID, photoID); err != nil {
		if errors.Is(err, ErrPhotoNotFound) {
			// An asset that is not there is not found as itself.
			if _, err := s.GetAssetByID(assetID); err != nil {
				return nil, nil, err
			}
		}
		return nil, nil, err
	}
	removed, err := s.RemoveAttachment(models.OwnerAsset, assetID, photoID)
	if errors.Is(err, ErrAttachmentNotFound) {
		return nil, nil, ErrPhotoNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	a, err := s.GetAssetByID(assetID)
	p := removed.Photo()
	return a, &p, err
}

// ReorderPhotos puts an asset's images in the order given, which must name
// each of them once, in the places among its attachments that images
// have: its other attachments stay where they are. The first is the one
// lists show.
func (s *Store) ReorderPhotos(assetID string, order []string) (*models.AssetWithDetails, error) {
	o := attachmentOwners[models.OwnerAsset]
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	have, err := s.editableAttachmentsTx(tx, o, assetID)
	if err != nil {
		return nil, err
	}
	var images []string
	for _, r := range have {
		if r.kind == models.AttachmentImage {
			images = append(images, r.id)
		}
	}
	if !sameIDs(images, order) {
		return nil, ErrPhotoOrder
	}
	all, next := make([]string, len(have)), 0
	for i, r := range have {
		if r.kind == models.AttachmentImage {
			all[i] = order[next]
			next++
		} else {
			all[i] = r.id
		}
	}
	if err := s.positionAttachmentsTx(tx, o, all); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// StoredFiles lists the keys of the files rows name in the file store
// (filestore.Images): each attachment, and an image's thumbnail, as
// photos/<id>, and each label image, as images/<id>.
func (s *Store) StoredFiles() (map[string]bool, error) {
	out := map[string]bool{}
	queries := map[string]string{"SELECT id, id FROM images": "images/"}
	for _, o := range attachmentOwners {
		queries["SELECT id, thumb_id FROM "+o.table] = "photos/"
	}
	for query, prefix := range queries {
		rows, err := s.Query(query)
		if err != nil {
			return nil, fmt.Errorf("reading stored files: %w", err)
		}
		for rows.Next() {
			var a string
			var b sql.NullString
			if err := rows.Scan(&a, &b); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning a stored file: %w", err)
			}
			out[prefix+a] = true
			if b.Valid && b.String != "" {
				out[prefix+b.String] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
