package store

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

func photo(name string) models.AssetPhoto {
	return models.AssetPhoto{ID: uuid.NewString(), ThumbID: uuid.NewString(), Filename: name, ContentType: "image/jpeg", WidthPx: 4, HeightPx: 3, ByteSize: 10, CreatedAt: time.Now()}
}

func photoNames(a *models.AssetWithDetails) []string {
	out := []string{}
	for _, p := range a.Photos {
		out = append(out, p.Filename)
	}
	return out
}

func TestAssetPhotos(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := asset(t, st, "Drill")
	if a, _ := st.GetAssetByID(id); a.Photos == nil || len(a.Photos) != 0 {
		t.Fatalf("no photos: %+v", a.Photos)
	}
	front, side, back := photo("front.jpg"), photo("side.jpg"), photo("back.jpg")
	for _, p := range []models.AssetPhoto{front, side, back} {
		if _, err := st.AddPhoto(id, p); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := st.GetAssetByID(id)
	if got := photoNames(a); !reflect.DeepEqual(got, []string{"front.jpg", "side.jpg", "back.jpg"}) {
		t.Errorf("added in order: %v", got)
	}
	expectChanges(t, "added", history(t, st, id)[0], models.EventAssetEdited, models.EventChange{What: "photo", Key: back.ID, To: "back.jpg"})
	if got, err := st.GetPhoto(id, side.ID); err != nil || got.ThumbID != side.ThumbID {
		t.Errorf("get: %+v, %v", got, err)
	}
	if _, err := st.GetPhoto(asset(t, st, "Saw"), side.ID); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("another asset's photo: %v", err)
	}

	// Reordered: the first is what lists show.
	a, err := st.ReorderPhotos(id, []string{back.ID, front.ID, side.ID})
	if err != nil || !reflect.DeepEqual(photoNames(a), []string{"back.jpg", "front.jpg", "side.jpg"}) {
		t.Errorf("reordered %v, %v", photoNames(a), err)
	}
	for _, bad := range [][]string{{back.ID, front.ID}, {back.ID, front.ID, front.ID}, {back.ID, front.ID, "other"}} {
		if _, err := st.ReorderPhotos(id, bad); !errors.Is(err, ErrPhotoOrder) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	list, _ := st.ListAssets("", "Drill")
	if len(list) != 1 || !reflect.DeepEqual(photoNames(&list[0]), []string{"back.jpg", "front.jpg", "side.jpg"}) {
		t.Errorf("listed: %+v", list)
	}

	// Removing one closes the gap and says which it was.
	a, removed, err := st.RemovePhoto(id, front.ID)
	if err != nil || removed.ThumbID != front.ThumbID || !reflect.DeepEqual(photoNames(a), []string{"back.jpg", "side.jpg"}) {
		t.Fatalf("removed %+v: %v, %v", removed, photoNames(a), err)
	}
	expectChanges(t, "removed", history(t, st, id)[0], models.EventAssetEdited, models.EventChange{What: "photo", Key: front.ID, From: "front.jpg"})
	if _, _, err := st.RemovePhoto(id, front.ID); !errors.Is(err, ErrPhotoNotFound) {
		t.Errorf("removing twice: %v", err)
	}

	// At most MaxPhotos, and none for archived or missing assets.
	for i := len(a.Photos); i < MaxPhotos; i++ {
		if _, err := st.AddPhoto(id, photo(fmt.Sprintf("%d.jpg", i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddPhoto(id, photo("one too many.jpg")); !errors.Is(err, ErrTooManyPhotos) {
		t.Errorf("one too many: %v", err)
	}
	if _, err := st.AddPhoto("no-such-asset", photo("x.jpg")); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("a missing asset: %v", err)
	}
	gone := asset(t, st, "Old saw")
	archive(t, st, gone)
	if _, err := st.AddPhoto(gone, photo("x.jpg")); !errors.Is(err, ErrAssetArchived) {
		t.Errorf("an archived asset: %v", err)
	}
}

func TestAddingPhotosTogether(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	id := asset(t, st, "Drill")
	if _, err := st.AddPhoto(id, photo("own.jpg")); err != nil {
		t.Fatal(err)
	}
	front, side := photo("front.jpg"), photo("side.jpg")
	a, err := st.AddPhotos(id, []models.AssetPhoto{front, side})
	if err != nil || !reflect.DeepEqual(photoNames(a), []string{"own.jpg", "front.jpg", "side.jpg"}) {
		t.Fatalf("after its own, in order: %v, %v", photoNames(a), err)
	}
	// One entry in its history for the lot.
	expectChanges(t, "added together", history(t, st, id)[0], models.EventAssetEdited,
		models.EventChange{What: "photo", Key: front.ID, To: "front.jpg"}, models.EventChange{What: "photo", Key: side.ID, To: "side.jpg"})

	// All or none: too many for what room is left adds none of them.
	more := []models.AssetPhoto{}
	for i := len(a.Photos); i <= MaxPhotos; i++ {
		more = append(more, photo(fmt.Sprintf("%d.jpg", i)))
	}
	if _, err := st.AddPhotos(id, more); !errors.Is(err, ErrTooManyPhotos) {
		t.Errorf("one too many: %v", err)
	}
	if again, _ := st.GetAssetByID(id); len(again.Photos) != 3 {
		t.Errorf("a refused lot left %d photos", len(again.Photos))
	}
	// Nothing to add changes nothing.
	before := len(history(t, st, id))
	if _, err := st.AddPhotos(id, nil); err != nil || len(history(t, st, id)) != before {
		t.Errorf("adding none: %v, history %d then %d", err, before, len(history(t, st, id)))
	}
}
