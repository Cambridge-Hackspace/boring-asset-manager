package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// attachedFixture is an asset and a category, each with a document beside
// the seed's photo, and their files.
type attachedFixture struct {
	seeded
	cat                   *models.Category
	asset                 string
	assetDoc, categoryDoc models.Attachment
}

func seedAttachments(t *testing.T, st *store.Store, blobs filestore.Blobs) attachedFixture {
	t.Helper()
	f := attachedFixture{seeded: seed(t, st, blobs)}
	if err := st.QueryRow("SELECT asset_id FROM asset_attachments WHERE id = ?", f.photo.ID).Scan(&f.asset); err != nil {
		t.Fatal(err)
	}
	if err := st.QueryRow("SELECT category_id FROM asset_categories WHERE asset_id = ?", f.asset).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	var catID string
	_ = st.QueryRow("SELECT category_id FROM asset_categories WHERE asset_id = ?", f.asset).Scan(&catID)
	cat, err := st.GetCategoryByID(catID)
	if err != nil {
		t.Fatal(err)
	}
	f.cat = cat
	doc := func(name string) models.Attachment {
		return models.Attachment{ID: uuid.NewString(), Kind: models.AttachmentDocument, Name: name, Description: "About " + name, Filename: name,
			ContentType: "application/pdf", ByteSize: 9, CreatedAt: time.Now()}
	}
	f.assetDoc, f.categoryDoc = doc("manual.pdf"), doc("guide.pdf")
	for _, a := range []models.Attachment{f.assetDoc, f.categoryDoc} {
		if err := blobs.Put(context.Background(), "photos/"+a.ID, strings.NewReader("%PDF-"+a.Name), -1, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.As(f.user.ID).AddAttachments(models.OwnerAsset, f.asset, []models.Attachment{f.assetDoc}, store.DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(f.user.ID).AddAttachments(models.OwnerCategory, f.cat.ID, []models.Attachment{f.categoryDoc}, store.DefaultMaxAttachments); err != nil {
		t.Fatal(err)
	}
	return f
}

func rowIDs(rows []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r["id"].(string)] = true
	}
	return out
}

// TestBackupsCarryAttachments: a backup holds assets' and categories'
// attachments and their files, and a restore puts them back; one without
// documents holds the images alone.
func TestBackupsCarryAttachments(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	f := seedAttachments(t, src, blobs)

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	if got := rowIDs(r.rows(t, DataDir+"asset_attachments/")); len(got) != 2 || !got[f.photo.ID] || !got[f.assetDoc.ID] {
		t.Errorf("assets' attachments: %v", got)
	}
	if got := rowIDs(r.rows(t, DataDir+"category_attachments/")); len(got) != 1 || !got[f.categoryDoc.ID] {
		t.Errorf("categories' attachments: %v", got)
	}
	for _, id := range []string{f.photo.ID, f.photo.ThumbID, f.assetDoc.ID, f.categoryDoc.ID} {
		if _, ok := r.members[FilesDir+"photos/"+id]; !ok {
			t.Errorf("no file photos/%s", id)
		}
	}

	dst := emptyStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Restore(context.Background(), dst, dstBlobs, opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.ListAttachments(models.OwnerCategory, f.cat.ID)
	if err != nil || len(got) != 1 || got[0].Description != "About guide.pdf" || got[0].AddedBy != f.user.ID {
		t.Errorf("the category's, restored: %+v %v", got, err)
	}
	if rc, err := dstBlobs.Get(context.Background(), "photos/"+f.categoryDoc.ID); err != nil {
		t.Errorf("its file: %v", err)
	} else {
		rc.Close()
	}

	// Without documents: the images, and none of the rest, rows or files.
	r = readArchive(t, backup(t, src, blobs, Options{NoDocuments: true}))
	if got := rowIDs(r.rows(t, DataDir+"asset_attachments/")); len(got) != 1 || !got[f.photo.ID] {
		t.Errorf("without documents, assets' attachments: %v", got)
	}
	if got := r.rows(t, DataDir+"category_attachments/"); len(got) != 0 {
		t.Errorf("without documents, categories' attachments: %v", got)
	}
	for _, id := range []string{f.assetDoc.ID, f.categoryDoc.ID} {
		if _, ok := r.members[FilesDir+"photos/"+id]; ok {
			t.Errorf("without documents, photos/%s is held", id)
		}
	}
	if _, ok := r.members[FilesDir+"photos/"+f.photo.ThumbID]; !ok || len(r.manifest.Missing) != 1 {
		t.Errorf("without documents, the photo's thumbnail is not held, or missing: %v", r.manifest.Missing)
	}
}

// TestPackagesCarryAttachments: a package holds its categories'
// attachments, and its assets' with their photos, without who added them;
// importing them gives each file a copy of its own. One without documents
// holds the images alone.
func TestPackagesCarryAttachments(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	f := seedAttachments(t, src, blobs)
	sel := Selection{Subtrees: []string{f.cat.ID}, Assets: &AssetsSelection{Photos: true}}
	r := packageOf(t, src, blobs, sel, nil)
	assets, cats := r.rows(t, DataDir+"asset_attachments/"), r.rows(t, DataDir+"category_attachments/")
	if len(assets) != 2 || len(cats) != 1 || cats[0]["added_by"] != nil || assets[1]["added_by"] != nil {
		t.Errorf("packaged: %v %v", assets, cats)
	}
	if _, ok := r.members[FilesDir+"photos/"+f.categoryDoc.ID]; !ok {
		t.Error("the category's document's file is not packaged")
	}
	// Without its assets' photos, the category's still come.
	r = packageOf(t, src, blobs, Selection{Subtrees: []string{f.cat.ID}}, nil)
	if got := r.rows(t, DataDir+"category_attachments/"); len(got) != 1 || len(r.rows(t, DataDir+"asset_attachments/")) != 0 {
		t.Errorf("a category alone: %v", got)
	}
	noDocs := sel
	noDocs.NoDocuments = true
	r = packageOf(t, src, blobs, noDocs, nil)
	if got := rowIDs(r.rows(t, DataDir+"asset_attachments/")); len(got) != 1 || !got[f.photo.ID] || len(r.rows(t, DataDir+"category_attachments/")) != 0 {
		t.Errorf("without documents: %v", got)
	}
	if _, ok := r.members[FilesDir+"photos/"+f.assetDoc.ID]; ok {
		t.Error("without documents, the document's file is packaged")
	}

	// Imported, with files of their own.
	pkg := exportPackage(t, src, blobs, sel)
	dst := migratedStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Import(ctx, dst, dstBlobs, opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var catID string
	if err := dst.QueryRow("SELECT id FROM categories WHERE name = ?", f.cat.Name).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	got, err := dst.ListAttachments(models.OwnerCategory, catID)
	if err != nil || len(got) != 1 || got[0].ID == f.categoryDoc.ID || got[0].Name != "guide.pdf" || got[0].AddedBy != "" {
		t.Fatalf("the category's, imported: %+v %v", got, err)
	}
	if rc, err := dstBlobs.Get(ctx, "photos/"+got[0].ID); err != nil {
		t.Errorf("its file: %v", err)
	} else {
		data, _ := io.ReadAll(rc)
		rc.Close()
		if string(data) != "%PDF-guide.pdf" {
			t.Errorf("its file holds %q", data)
		}
	}
	if n := count(t, dst, "asset_attachments"); n != 2 {
		t.Errorf("%d assets' attachments imported", n)
	}
	// Every file stored is one a row names.
	named, err := dst.StoredFiles()
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	if err := dstBlobs.List(ctx, "photos/", func(o filestore.Object) error {
		stored++
		if !named[o.Key] {
			t.Errorf("%s is stored, and no row names it", o.Key)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if stored != 4 {
		t.Errorf("%d files stored", stored)
	}
}

// beforeAttachments rewrites an archive as one written before attachments
// (migration 067): its assets' images as asset_photos, and no documents,
// nor categories' attachments, nor vendors, which came after.
func beforeAttachments(t *testing.T, b []byte) []byte {
	t.Helper()
	b = beforeVendors(t, b)
	var out []member
	tables := map[string]int{}
	for _, m := range unpack(t, b) {
		switch {
		case strings.HasPrefix(m.name, DataDir+"category_attachments/"):
			continue
		case strings.HasPrefix(m.name, DataDir+"asset_attachments/"):
			var buf bytes.Buffer
			for _, line := range strings.Split(strings.TrimSpace(string(m.data)), "\n") {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				if row["kind"] != "image" {
					continue
				}
				old := map[string]any{}
				for _, c := range retiredTable("asset_photos").Columns {
					old[c.Name] = row[c.Name]
				}
				data, _ := json.Marshal(old)
				buf.Write(append(data, '\n'))
				tables["asset_photos"]++
			}
			m.name = DataDir + "asset_photos/" + m.name[strings.LastIndex(m.name, "/")+1:]
			m.data = buf.Bytes()
		}
		out = append(out, m)
	}
	return pack(t, reseal(t, out, func(h *Header, man *Manifest) {
		h.SchemaVersion = AttachmentsVersion - 1
		delete(man.Tables, "asset_attachments")
		delete(man.Tables, "category_attachments")
		if _, ok := man.Tables["assets"]; ok {
			man.Tables["asset_photos"] = tables["asset_photos"]
		}
		man.Header.SchemaVersion = h.SchemaVersion
	}))
}

// TestArchivesFromBeforeAttachments: a backup or a package written before
// attachments, holding asset_photos, still restores and imports, its
// photos becoming image attachments; at this schema, asset_photos is no
// table an archive may hold.
func TestArchivesFromBeforeAttachments(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	f := seedAttachments(t, src, blobs)

	old := beforeAttachments(t, backup(t, src, blobs, Options{}))
	if _, err := Check(bytes.NewReader(old)); err != nil {
		t.Fatalf("an archive from before attachments: %v", err)
	}
	dst := emptyStore(t, envDialect())
	dstBlobs := filestore.NewLocal(t.TempDir())
	sum, err := Restore(ctx, dst, dstBlobs, opener(old), RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Migrated {
		t.Errorf("not migrated: %+v", sum)
	}
	got, err := dst.ListAttachments(models.OwnerAsset, f.asset)
	if err != nil || len(got) != 1 || got[0].ID != f.photo.ID || got[0].Kind != models.AttachmentImage || got[0].ThumbID != f.photo.ThumbID || got[0].Name != f.photo.Filename {
		t.Errorf("restored: %+v %v", got, err)
	}
	for _, id := range []string{f.photo.ID, f.photo.ThumbID} {
		if rc, err := dstBlobs.Get(ctx, "photos/"+id); err != nil {
			t.Errorf("photos/%s: %v", id, err)
		} else {
			rc.Close()
		}
	}

	// At this schema, asset_photos is refused.
	now := pack(t, reseal(t, unpack(t, old), func(h *Header, man *Manifest) {
		h.SchemaVersion = AttachmentsVersion
		man.Header.SchemaVersion = h.SchemaVersion
	}))
	if _, err := Check(bytes.NewReader(now)); err == nil {
		t.Error("asset_photos at this schema checked out")
	}

	// A package from before: its photos are imported as images.
	pkg := beforeAttachments(t, exportPackage(t, src, blobs, Selection{Subtrees: []string{f.cat.ID}, Assets: &AssetsSelection{Photos: true}}))
	into := migratedStore(t, envDialect())
	intoBlobs := filestore.NewLocal(t.TempDir())
	if _, err := Import(ctx, into, intoBlobs, opener(pkg), ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var id, thumb, kind, name string
	if err := into.QueryRow("SELECT id, thumb_id, kind, name FROM asset_attachments").Scan(&id, &thumb, &kind, &name); err != nil {
		t.Fatal(err)
	}
	if id == f.photo.ID || kind != models.AttachmentImage || name != f.photo.Filename {
		t.Errorf("imported: %s %s %s", id, kind, name)
	}
	for _, k := range []string{id, thumb} {
		if rc, err := intoBlobs.Get(ctx, "photos/"+k); err != nil {
			t.Errorf("photos/%s: %v", k, err)
		} else {
			rc.Close()
		}
	}
}
