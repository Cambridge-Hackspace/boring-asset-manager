package archive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// A package leaves out person fields' values and their changes in
// history, as it leaves out people: it keeps the field itself.
func TestAPackageNamesNoOneInPersonFields(t *testing.T) {
	st := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	keeper := must[*models.Field](t)(st.CreateField("keeper", "Keeper", "person", nil))
	notes := must[*models.Field](t)(st.CreateField("notes", "Notes", "text", nil))
	cat := must[*models.Category](t)(st.CreateCategory("Machines", nil, "", false, false))
	for _, f := range []*models.Field{keeper, notes} {
		if err := st.AssignCategoryField(cat.ID, f.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	ada := must[*models.User](t)(st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true))
	must[*models.AssetWithDetails](t)(st.CreateAsset("Lathe", []string{cat.ID}, nil, map[string]string{"keeper": ada.ID, "notes": "Oil weekly"}))

	r := packageOf(t, st, blobs, Selection{Assets: &AssetsSelection{All: true, History: true}}, nil)
	if got := ids(t, r, "fields", "id"); !reflect.DeepEqual(got, sorted(keeper.ID, notes.ID)) {
		t.Errorf("fields: %v", got)
	}
	if got := ids(t, r, "asset_field_values", "field_id"); !reflect.DeepEqual(got, []string{notes.ID}) {
		t.Errorf("values: %v", got)
	}
	if rows := r.rows(t, DataDir+"asset_field_people/"); len(rows) != 0 {
		t.Errorf("whom fields name: %v", rows)
	}
	for _, row := range r.rows(t, DataDir+"events/") {
		raw, _ := json.Marshal(row)
		if strings.Contains(string(raw), "Ada") || strings.Contains(string(raw), ada.ID) {
			t.Errorf("history names Ada: %s", raw)
		}
	}
}
