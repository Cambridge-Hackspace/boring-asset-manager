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
		ID: id, Name: "Shelf label", Units: labels.UnitsMM, Width: 62, Height: 29, DPI: 300,
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

	created, err := st.CreateTemplate(sampleTemplate("shelf"))
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.Elements[1].FontWeight != labels.WeightNormal {
		t.Errorf("defaults not applied before storing: %+v", created.Elements[1])
	}

	got, err := st.GetTemplate("shelf")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !reflect.DeepEqual(got.Elements, created.Elements) || got.Width != 62 || got.DPI != 300 {
		t.Errorf("round trip changed the template:\n got  %+v\n want %+v", got, created)
	}

	if _, err := st.CreateTemplate(sampleTemplate("shelf")); !errors.Is(err, ErrTemplateExists) {
		t.Errorf("duplicate id: got %v, want ErrTemplateExists", err)
	}

	time.Sleep(10 * time.Millisecond)
	changed := sampleTemplate("shelf")
	changed.Name = "Renamed"
	changed.Elements = changed.Elements[:1]
	updated, err := st.UpdateTemplate(changed)
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if !updated.CreatedAt.Equal(got.CreatedAt) || !updated.UpdatedAt.After(got.UpdatedAt) {
		t.Errorf("timestamps: created %v -> %v, updated %v -> %v", got.CreatedAt, updated.CreatedAt, got.UpdatedAt, updated.UpdatedAt)
	}

	if _, err := st.CreateTemplate(sampleTemplate("another")); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "Renamed" || len(list[0].Elements) != 1 {
		t.Errorf("ListTemplates() = %+v", list)
	}

	if err := st.DeleteTemplate("shelf"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := st.GetTemplate("shelf"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("after delete: got %v, want ErrTemplateNotFound", err)
	}
	if err := st.DeleteTemplate("shelf"); !errors.Is(err, ErrTemplateNotFound) {
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
	if _, err := st.GetTemplate("bad"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("invalid template was stored")
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
	if _, err := st.CreateTemplate(tpl); err != nil {
		t.Fatal(err)
	}

	var inUse *ImageInUseError
	if err := st.DeleteImage(img.ID); !errors.As(err, &inUse) || !reflect.DeepEqual(inUse.TemplateIDs, []string{"logo"}) {
		t.Fatalf("deleting a used image: got %v, want ImageInUseError naming logo", err)
	}

	if err := st.DeleteTemplate("logo"); err != nil {
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
