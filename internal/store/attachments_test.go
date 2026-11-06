package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
	"github.com/google/uuid"
)

func document(name string) models.Attachment {
	return models.Attachment{ID: uuid.NewString(), Kind: models.AttachmentDocument, Name: name, Filename: name, ContentType: "application/pdf", ByteSize: 100, CreatedAt: time.Now()}
}

func attachmentIDs(as []models.Attachment) []string {
	out := []string{}
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

// TestCategoryAttachments: a category's attachments are added, in order, by
// whoever adds them, renamed, reordered and removed, and its history says
// what came and went.
func TestCategoryAttachments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	cat, err := st.CreateCategory("Lathes", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateNamedUser("ada@hackspace.org", "Ada", "Secret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.ListAttachments(models.OwnerCategory, cat.ID); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("none yet: %+v %v", got, err)
	}
	manual, guide := document("manual.pdf"), document("guide.pdf")
	guide.Name, guide.Description = "Guide", "For beginners"
	added, err := st.As(user.ID).AddAttachments(models.OwnerCategory, cat.ID, []models.Attachment{manual, guide}, 3)
	if err != nil || !reflect.DeepEqual(attachmentIDs(added), []string{manual.ID, guide.ID}) {
		t.Fatalf("added %+v: %v", added, err)
	}
	if a := added[1]; a.Name != "Guide" || a.Description != "For beginners" || a.AddedBy != user.ID || a.AddedByName != "Ada" || a.ThumbID != "" || a.WidthPx != 0 {
		t.Errorf("as added: %+v", a)
	}
	events, err := st.ListEvents(10)
	if err != nil || len(events) == 0 || events[0].Kind != models.EventCategoryEdited || events[0].CategoryID == nil || *events[0].CategoryID != cat.ID ||
		len(events[0].Payload.Changes) != 2 || events[0].Payload.Changes[1] != (models.EventChange{What: "attachment", Key: guide.ID, To: "Guide"}) {
		t.Errorf("history: %+v %v", events, err)
	}

	// No more than the limit, all or none.
	if _, err := st.AddAttachments(models.OwnerCategory, cat.ID, []models.Attachment{document("a.pdf"), document("b.pdf")}, 3); !errors.Is(err, ErrTooManyAttachments) {
		t.Errorf("past the limit: %v", err)
	}
	if _, err := st.AddAttachments(models.OwnerCategory, "nope", []models.Attachment{document("a.pdf")}, 3); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}

	renamed := " Lathe manual "
	got, err := st.UpdateAttachment(models.OwnerCategory, cat.ID, manual.ID, &renamed, nil)
	if err != nil || got.Name != "Lathe manual" || got.Filename != "manual.pdf" {
		t.Errorf("renamed: %+v %v", got, err)
	}
	empty := ""
	if _, err := st.UpdateAttachment(models.OwnerCategory, cat.ID, manual.ID, &empty, nil); !errors.Is(err, ErrAttachmentName) {
		t.Errorf("an empty name: %v", err)
	}
	if _, err := st.UpdateAttachment(models.OwnerAsset, cat.ID, manual.ID, &renamed, nil); err == nil {
		t.Error("renamed as an asset's")
	}

	reordered, err := st.ReorderAttachments(models.OwnerCategory, cat.ID, []string{guide.ID, manual.ID})
	if err != nil || !reflect.DeepEqual(attachmentIDs(reordered), []string{guide.ID, manual.ID}) {
		t.Errorf("reordered: %v %v", attachmentIDs(reordered), err)
	}
	if _, err := st.ReorderAttachments(models.OwnerCategory, cat.ID, []string{guide.ID}); !errors.Is(err, ErrAttachmentOrder) {
		t.Errorf("an order missing one: %v", err)
	}

	removed, err := st.RemoveAttachment(models.OwnerCategory, cat.ID, guide.ID)
	if err != nil || removed.ID != guide.ID {
		t.Fatalf("removed: %+v %v", removed, err)
	}
	if left, _ := st.ListAttachments(models.OwnerCategory, cat.ID); !reflect.DeepEqual(attachmentIDs(left), []string{manual.ID}) {
		t.Errorf("left: %v", attachmentIDs(left))
	}
	if _, err := st.RemoveAttachment(models.OwnerCategory, cat.ID, guide.ID); !errors.Is(err, ErrAttachmentNotFound) {
		t.Errorf("removing twice: %v", err)
	}

	// Deleting the category takes its attachments with it; their files
	// are then named by no row.
	named, _ := st.StoredFiles()
	if !named["photos/"+manual.ID] {
		t.Errorf("stored files: %v", named)
	}
	if err := st.DeleteCategory(cat.ID); err != nil {
		t.Fatal(err)
	}
	if named, _ := st.StoredFiles(); named["photos/"+manual.ID] {
		t.Error("a deleted category's attachment is still named")
	}
}

// TestPhotosAmongAttachments: an asset's photos are its images, in their
// order among its attachments, and the photo routes' store leaves the
// others where they are.
func TestPhotosAmongAttachments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := asset(t, st, "Drill")
	manual, front, notes, back := document("manual.pdf"), photo("front.jpg"), document("notes.pdf"), photo("back.jpg")
	all := []models.Attachment{manual, PhotoAttachments([]models.AssetPhoto{front})[0], notes, PhotoAttachments([]models.AssetPhoto{back})[0]}
	if _, err := st.AddAttachments(models.OwnerAsset, id, all, DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	a, _ := st.GetAssetByID(id)
	if !reflect.DeepEqual(photoNames(a), []string{"front.jpg", "back.jpg"}) || a.Photos[0].ThumbID != front.ThumbID {
		t.Errorf("photos: %+v", a.Photos)
	}
	list, _ := st.ListAssets("", "Drill")
	if len(list) != 1 || !reflect.DeepEqual(photoNames(&list[0]), []string{"front.jpg", "back.jpg"}) {
		t.Errorf("listed: %+v", list)
	}
	expectChanges(t, "added", history(t, st, id)[0], models.EventAssetEdited,
		models.EventChange{What: "attachment", Key: manual.ID, To: "manual.pdf"}, models.EventChange{What: "photo", Key: front.ID, To: "front.jpg"},
		models.EventChange{What: "attachment", Key: notes.ID, To: "notes.pdf"}, models.EventChange{What: "photo", Key: back.ID, To: "back.jpg"})

	if _, err := st.GetPhoto(id, manual.ID); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("a document as a photo: %v", err)
	}
	if _, _, err := st.RemovePhoto(id, manual.ID); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("removing a document as a photo: %v", err)
	}
	if _, err := st.ReorderPhotos(id, []string{back.ID, manual.ID}); !errors.Is(err, ErrPhotoOrder) {
		t.Errorf("a document among photos: %v", err)
	}
	a, err := st.ReorderPhotos(id, []string{back.ID, front.ID})
	if err != nil || !reflect.DeepEqual(photoNames(a), []string{"back.jpg", "front.jpg"}) {
		t.Fatalf("reordered photos: %v %v", photoNames(a), err)
	}
	got, _ := st.ListAttachments(models.OwnerAsset, id)
	if !reflect.DeepEqual(attachmentIDs(got), []string{manual.ID, back.ID, notes.ID, front.ID}) {
		t.Errorf("the documents moved: %v", attachmentIDs(got))
	}

	// Every file is named: each attachment's, and each image's thumbnail.
	named, err := st.StoredFiles()
	if err != nil || len(named) != 6 || !named["photos/"+back.ThumbID] || !named["photos/"+notes.ID] {
		t.Errorf("stored files: %v %v", named, err)
	}
}

// TestPhotosBecomeAttachments: migration 067 makes each asset's photos its
// image attachments, in the same order, keeping the files they name.
func TestPhotosBecomeAttachments(t *testing.T) {
	st := openBlank(t)
	if err := migrations.MigrateTo(st.DB, st.Dialect, 66); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, a := range []string{"a1", "a2"} {
		if _, err := st.Exec("INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", a, a, now, now); err != nil {
			t.Fatal(err)
		}
	}
	type photoRow struct {
		id, asset, thumb, filename string
		position                   int
	}
	rows := []photoRow{
		{"p-side", "a1", "t-side", "side.jpg", 1},
		{"p-front", "a1", "t-front", "front.jpg", 0},
		{"p-back", "a1", "t-back", "back.png", 2},
		{"p-only", "a2", "t-only", "only.jpg", 0},
	}
	for _, r := range rows {
		if _, err := st.Exec("INSERT INTO asset_photos (id, asset_id, thumb_id, position, filename, content_type, width_px, height_px, byte_size, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			r.id, r.asset, r.thumb, r.position, r.filename, "image/jpeg", 40, 30, 1234, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	got, err := st.ListAttachments(models.OwnerAsset, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attachmentIDs(got), []string{"p-front", "p-side", "p-back"}) {
		t.Errorf("order: %v", attachmentIDs(got))
	}
	a := got[0]
	if a.Kind != models.AttachmentImage || a.Name != "front.jpg" || a.Filename != "front.jpg" || a.ThumbID != "t-front" || a.ContentType != "image/jpeg" ||
		a.WidthPx != 40 || a.HeightPx != 30 || a.ByteSize != 1234 || a.Description != "" || a.AddedBy != "" || a.CreatedAt.Sub(now).Abs() > time.Second {
		t.Errorf("as migrated: %+v", a)
	}
	asset, err := st.GetAssetByID("a2")
	if err != nil || len(asset.Photos) != 1 || asset.Photos[0].ID != "p-only" || asset.Photos[0].ThumbID != "t-only" {
		t.Errorf("a2's photos: %+v %v", asset, err)
	}
	// The files they name are where they were: photos/<id> and photos/<thumb>.
	named, err := st.StoredFiles()
	if err != nil || len(named) != 8 {
		t.Fatalf("stored files: %v %v", named, err)
	}
	for _, r := range rows {
		if !named["photos/"+r.id] || !named["photos/"+r.thumb] {
			t.Errorf("%s's files are not named: %v", r.id, named)
		}
	}
	// The table is gone.
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM asset_photos").Scan(&n); err == nil {
		t.Error("asset_photos is still there")
	}
}
