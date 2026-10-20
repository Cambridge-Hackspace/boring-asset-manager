package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

func sampleTemplate(id string) *labels.Template {
	return &labels.Template{
		ID: id, Name: "Shelf label", Units: labels.UnitsMM, Width: 62, Height: 29, DPI: 300, Layering: labels.LayeringList,
		Elements: []labels.Element{
			{Type: labels.ElementGenerator, GeneratorType: "qr", X: 2, Y: 2, Width: 25, Height: 25,
				ContentBinding: "{{base_url}}/a/{{asset.id}}"},
			{Type: labels.ElementText, X: 29, Y: 2, Width: 31, Height: 8, FontSize: 12.5,
				ContentBinding: "{{asset.name}}"},
		},
	}
}

func createTestImage(t *testing.T, st *Store) *models.Image {
	t.Helper()
	img := &models.Image{
		ID: uuid.NewString(), Filename: "logo.png", ContentType: "image/png",
		WidthPx: 64, HeightPx: 32, ByteSize: 1234, SHA256: "ab", CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateImage(img); err != nil {
		t.Fatalf("creating image: %v", err)
	}
	return img
}

func TestTemplateLifecycle(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	// Any ID the caller gives is replaced by a new UUID.
	created, err := st.CreateTemplate(sampleTemplate("shelf"))
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("created ID %q is not a UUID", created.ID)
	}
	shelf := created.ID
	if created.Elements[1].FontWeight != labels.WeightNormal {
		t.Errorf("defaults not applied before storing: %+v", created.Elements[1])
	}

	got, err := st.GetTemplate(shelf)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !reflect.DeepEqual(got.Elements, created.Elements) || got.Width != 62 || got.DPI != 300 {
		t.Errorf("round trip changed the template:\n got  %+v\n want %+v", got, created)
	}

	if _, err := st.GetTemplate("shelf"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("stored under the ID the caller gave: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	changed := sampleTemplate(shelf)
	changed.Name = "Renamed"
	changed.Elements = changed.Elements[:1]
	updated, err := st.UpdateTemplate(changed)
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if !updated.CreatedAt.Equal(got.CreatedAt) || !updated.UpdatedAt.After(got.UpdatedAt) {
		t.Errorf("timestamps: created %v -> %v, updated %v -> %v", got.CreatedAt, updated.CreatedAt, got.UpdatedAt, updated.UpdatedAt)
	}

	// The same template again is a second one, under another ID.
	another, err := st.CreateTemplate(sampleTemplate(""))
	if err != nil {
		t.Fatal(err)
	}
	if another.ID == shelf {
		t.Errorf("two templates share the ID %s", shelf)
	}
	list, err := st.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "Renamed" || len(list[0].Elements) != 1 {
		t.Errorf("ListTemplates() = %+v", list)
	}

	if err := st.DeleteTemplate(shelf); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := st.GetTemplate(shelf); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("after delete: got %v, want ErrTemplateNotFound", err)
	}
	if err := st.DeleteTemplate(shelf); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("second delete: got %v, want ErrTemplateNotFound", err)
	}
	if _, err := st.UpdateTemplate(sampleTemplate("missing")); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("update missing: got %v, want ErrTemplateNotFound", err)
	}
}

func TestTemplateReferencesMustExist(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	tpl := sampleTemplate("refs")
	tpl.Elements = append(tpl.Elements,
		labels.Element{Type: labels.ElementText, X: 29, Y: 12, Width: 31, Height: 6, FontSize: 9,
			ContentBinding: "{{field.location}} / {{field.bin}}"},
		labels.Element{Type: labels.ElementStaticImage, X: 50, Y: 20, Width: 10, Height: 8,
			ImageID: "7c9e6679-7425-40de-944b-e07fc1f90ae7"},
	)

	_, err := st.CreateTemplate(tpl)
	var ve *labels.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	want := []labels.Problem{
		{Path: "elements[2].content_binding", Message: `field "location" does not exist`},
		{Path: "elements[2].content_binding", Message: `field "bin" does not exist`},
		{Path: "elements[3].image_id", Message: "no uploaded image has this ID"},
	}
	if !reflect.DeepEqual(ve.Problems, want) {
		t.Errorf("problems = %+v, want %+v", ve.Problems, want)
	}

	for _, key := range []string{"location", "bin"} {
		if _, err := st.CreateField(key, key, fields.TypeText, nil); err != nil {
			t.Fatal(err)
		}
	}
	tpl.Elements[3].ImageID = createTestImage(t, st).ID
	if _, err := st.CreateTemplate(tpl); err != nil {
		t.Errorf("template with existing references rejected: %v", err)
	}
}

func TestInvalidTemplatesAreNotStored(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	tpl := sampleTemplate("bad")
	tpl.DPI = 72
	var ve *labels.ValidationError
	if _, err := st.CreateTemplate(tpl); !errors.As(err, &ve) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	if list, err := st.ListTemplates(); err != nil || len(list) != 0 {
		t.Errorf("invalid template was stored: %v %v", list, err)
	}
}

func TestImageLifecycleAndInUseGuard(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	img := createTestImage(t, st)
	got, err := st.GetImage(img.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Filename != "logo.png" || got.WidthPx != 64 || got.ByteSize != 1234 {
		t.Errorf("GetImage() = %+v", got)
	}

	tpl := sampleTemplate("logo")
	tpl.Elements = append(tpl.Elements, labels.Element{Type: labels.ElementStaticImage, X: 50, Y: 20, Width: 10, Height: 8, ImageID: img.ID})
	logo, err := st.CreateTemplate(tpl)
	if err != nil {
		t.Fatal(err)
	}

	var inUse *ImageInUseError
	if err := st.DeleteImage(img.ID); !errors.As(err, &inUse) || !reflect.DeepEqual(inUse.TemplateIDs, []string{logo.ID}) {
		t.Fatalf("deleting a used image: got %v, want ImageInUseError naming logo", err)
	}

	if err := st.DeleteTemplate(logo.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteImage(img.ID); err != nil {
		t.Fatalf("deleting an unused image: %v", err)
	}
	if _, err := st.GetImage(img.ID); !errors.Is(err, ErrImageNotFound) {
		t.Errorf("after delete: got %v, want ErrImageNotFound", err)
	}
	if err := st.DeleteImage(img.ID); !errors.Is(err, ErrImageNotFound) {
		t.Errorf("second delete: got %v, want ErrImageNotFound", err)
	}

	list, err := st.ListImages()
	if err != nil || len(list) != 0 {
		t.Errorf("ListImages() = %v, %v", list, err)
	}
}

// TestTemplatesWithChosenIDs covers templates from versions where authors
// chose the ID: they keep it, and are read, changed and deleted by it.
func TestTemplatesWithChosenIDs(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	now := time.Now().UTC()
	if _, err := st.Exec(`INSERT INTO templates (`+templateColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"shelf-tag", "Shelf tag", "mm", 62, 29, 300, `{"elements":[]}`, now, now); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetTemplate("shelf-tag"); err != nil || got.Name != "Shelf tag" {
		t.Fatalf("GetTemplate: %+v %v", got, err)
	}
	changed := sampleTemplate("shelf-tag")
	if _, err := st.UpdateTemplate(changed); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if got, err := st.GetTemplate("shelf-tag"); err != nil || len(got.Elements) != 2 {
		t.Errorf("after update: %+v %v", got, err)
	}
	if err := st.DeleteTemplate("shelf-tag"); err != nil {
		t.Errorf("DeleteTemplate: %v", err)
	}
}

// TestStoredTemplatesAreLayered runs migration 033 again on a template
// stored before elements were drawn in list order: they are put in the
// order they were drawn then, and running it once more changes nothing.
func TestStoredTemplatesAreLayered(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	// Given without layering, a template is stored in the order it was drawn.
	legacyTemplate := sampleTemplate("")
	legacyTemplate.Layering = ""
	created, err := st.CreateTemplate(legacyTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if created.Layering != labels.LayeringList || created.Elements[0].Type != labels.ElementText {
		t.Fatalf("created: %+v", created)
	}
	const legacy = `{"elements":[{"type":"generator","x":1,"y":1,"width":5,"height":5,"generator_type":"qr"},` +
		`{"type":"text","x":7,"y":1,"width":5,"height":5,"font_size":8,"content_binding":"A"},` +
		`{"type":"static_image","x":13,"y":1,"width":5,"height":5,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"},` +
		`{"type":"text","x":19,"y":1,"width":5,"height":5,"font_size":8,"content_binding":"B"}],"sizing":"adaptive"}`
	if _, err := st.Exec("UPDATE templates SET layout_json = ? WHERE id = ?", legacy, created.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = 33"); err != nil {
			t.Fatal(err)
		}
		if err := st.Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetTemplate(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range got.Elements {
		order = append(order, e.Type+":"+e.ContentBinding)
	}
	if want := []string{"static_image:", "text:A", "text:B", "generator:"}; !reflect.DeepEqual(order, want) || got.Sizing != "adaptive" || got.Layering != labels.LayeringList {
		t.Errorf("after migrating: %v, sizing %q, layering %q", order, got.Sizing, got.Layering)
	}
}

// TestTemplateFeedAndTurnable keeps which way a template's labels are fed,
// whether an adaptive one takes turned labels, its guides, and how it
// prints mirrored and turned.
func TestTemplateFeedAndTurnable(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tmpl := sampleTemplate("")
	tmpl.Feed, tmpl.Turnable, tmpl.Sizing = labels.FeedAlong, true, labels.SizingAdaptive
	tmpl.Guides = &labels.Guides{X: []float64{10, 31}, Y: []float64{14.5}}
	tmpl.Grid = &labels.Grid{X: 2, Y: 1.5, OffsetX: 0.5, Subdivisions: 4, Visible: true}
	tmpl.PrintMirror, tmpl.PrintRotation = labels.MirrorAcross, 12.5
	tmpl.Adapt = &labels.Adapt{MinWidth: 30, MaxWidth: 100, MinHeight: 20, MaxHeight: 50, ContinuousLength: labels.LengthFixed}
	created, err := st.CreateTemplate(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTemplate(created.ID)
	if err != nil || got.Feed != labels.FeedAlong || !got.Turnable || !reflect.DeepEqual(got.Guides, tmpl.Guides) || !reflect.DeepEqual(got.Grid, tmpl.Grid) ||
		got.PrintMirror != labels.MirrorAcross || got.PrintRotation != 12.5 {
		t.Fatalf("stored: %+v %v", got, err)
	}
	got.Feed, got.Turnable, got.PrintMirror, got.PrintRotation = "", false, "", 0
	if _, err := st.UpdateTemplate(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.GetTemplate(created.ID); again.Feed != "" || again.Turnable || again.PrintMirror != "" || again.PrintRotation != 0 {
		t.Errorf("updated: %+v", again)
	}
}
