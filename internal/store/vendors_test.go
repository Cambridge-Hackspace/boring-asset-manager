package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// TestVendorsAreMadeAndEdited: a vendor is made with its contacts and
// scopes, edited, keeping the contacts sent back with their IDs, and its
// history says what changed; deleting it takes its contacts with it.
func TestVendorsAreMadeAndEdited(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateNamedUser("ada@hackspace.org", "Ada", "Secret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.As(user.ID).CreateVendor(VendorInput{Name: "  Acme  ", Website: "https://acme.example", AccountNumber: "A-1", Address: "1 Bolt Lane",
		Notes:    "**Net 30**",
		Contacts: []models.VendorContact{{Name: "Wile", Role: "sales", Email: "wile@acme.example"}, {Name: "Road", Role: "support", Phone: "123"}},
		ScopeIDs: []string{tools.ID, tools.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "Acme" || len(v.Contacts) != 2 || v.Contacts[0].Name != "Wile" || v.Contacts[1].Phone != "123" || !reflect.DeepEqual(v.ScopeIDs, []string{tools.ID}) {
		t.Errorf("made: %+v", v)
	}
	events, more, err := st.ListVendorEvents(v.ID, "", 10)
	if err != nil || more || len(events) != 1 || events[0].Kind != models.EventVendorCreated || events[0].Payload.Vendor == nil ||
		events[0].Payload.Vendor.Name != "Acme" || events[0].Payload.Actor == nil || events[0].Payload.Actor.ID != user.ID {
		t.Fatalf("history after making it: %+v %v", events, err)
	}

	// Wile stays, by ID, with a new role; Road goes; Bugs comes; the scope
	// is taken off.
	wile := v.Contacts[0]
	wile.Role = "accounts"
	v2, err := st.As(user.ID).UpdateVendor(v.ID, VendorInput{Name: "Acme Ltd", Website: "https://acme.example", AccountNumber: "A-1", Address: "1 Bolt Lane",
		Notes: "**Net 60**", Contacts: []models.VendorContact{{Name: "Bugs", Email: "bugs@acme.example"}, wile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(v2.Contacts) != 2 || v2.Contacts[0].Name != "Bugs" || v2.Contacts[1].ID != wile.ID || v2.Contacts[1].Role != "accounts" || len(v2.ScopeIDs) != 0 {
		t.Errorf("edited: %+v", v2)
	}
	events, _, err = st.ListVendorEvents(v.ID, "", 10)
	if err != nil || len(events) != 2 || events[0].Kind != models.EventVendorEdited {
		t.Fatalf("history after editing it: %+v %v", events, err)
	}
	whats := []string{}
	for _, c := range events[0].Payload.Changes {
		whats = append(whats, c.What+":"+c.From+">"+c.To)
	}
	want := []string{"name:Acme>Acme Ltd", "notes:>", "contact:>Bugs", "contact:Wile (sales)>Wile (accounts)", "contact:Road (support)>", "scope:tools>"}
	if !reflect.DeepEqual(whats, want) {
		t.Errorf("changes: %v, want %v", whats, want)
	}
	// Paged: the older event after the newer.
	older, more, err := st.ListVendorEvents(v.ID, events[0].ID, 10)
	if err != nil || more || len(older) != 1 || older[0].ID != events[1].ID {
		t.Errorf("the next page: %+v %v", older, err)
	}

	// Saving it as it is records nothing.
	if _, err := st.UpdateVendor(v.ID, VendorInput{Name: v2.Name, Website: v2.Website, AccountNumber: v2.AccountNumber, Address: v2.Address, Notes: v2.Notes, Contacts: v2.Contacts}); err != nil {
		t.Fatal(err)
	}
	if events, _, _ := st.ListVendorEvents(v.ID, "", 10); len(events) != 2 {
		t.Errorf("an unchanged save recorded %d events", len(events)-2)
	}

	if err := st.DeleteVendor(v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetVendor(v.ID); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("deleted: %v", err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM vendor_contacts").Scan(&n); err != nil || n != 0 {
		t.Errorf("%d contacts left: %v", n, err)
	}
	if err := st.DeleteVendor(v.ID); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
}

// TestVendorsAreChecked: what cannot be stored, or could be unsafe as a
// link, is refused.
func TestVendorsAreChecked(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	def := ""
	if err := st.QueryRow("SELECT id FROM scopes WHERE name = 'default'").Scan(&def); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]VendorInput{
		"no name":             {Name: " "},
		"a javascript: site":  {Name: "X", Website: "javascript:alert(1)"},
		"a data: site":        {Name: "X", Website: "data:text/html,hi"},
		"a site with no host": {Name: "X", Website: "https://"},
		"a bad email":         {Name: "X", Contacts: []models.VendorContact{{Name: "A", Email: "not an email"}}},
		"an empty contact":    {Name: "X", Contacts: []models.VendorContact{{Role: "sales"}}},
		"a long name":         {Name: strings.Repeat("x", 256)},
		"a NUL in notes":      {Name: "X", Notes: "a\x00b"},
		"too many contacts":   {Name: "X", Contacts: make([]models.VendorContact, MaxVendorContacts+1)},
	} {
		if _, err := st.CreateVendor(in); err == nil {
			t.Errorf("%s: made", name)
		}
	}
	if _, err := st.CreateVendor(VendorInput{Name: "X", ScopeIDs: []string{def}}); !errors.Is(err, ErrScopeReserved) {
		t.Errorf("in the default scope: %v", err)
	}
	if _, err := st.CreateVendor(VendorInput{Name: "X", ScopeIDs: []string{"nope"}}); !errors.Is(err, ErrScopeNotFound) {
		t.Errorf("in no scope there is: %v", err)
	}
	if n, _ := st.ListVendors(); len(n) != 0 {
		t.Errorf("%d vendors made", len(n))
	}
	if _, err := st.CreateVendor(VendorInput{Name: "Plain", Website: "http://plain.example/shop?a=1"}); err != nil {
		t.Errorf("an http site: %v", err)
	}
}

// TestCategoriesNameTheirVendors: a category names vendors in order, a copy
// of it names them too, a deleted vendor drops out, and a scope on a vendor
// cannot be deleted.
func TestCategoriesNameTheirVendors(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	sc, err := st.CreateScope("shop")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st.CreateVendor(VendorInput{Name: "A", ScopeIDs: []string{sc.ID}})
	b, _ := st.CreateVendor(VendorInput{Name: "B"})
	cat, err := st.CreateCategory("Screws", nil, "#123456", true, false)
	if err != nil || !reflect.DeepEqual(cat.VendorIDs, []string{}) {
		t.Fatalf("made: %+v %v", cat, err)
	}
	cat, err = st.SetCategoryVendors(cat.ID, []string{b.ID, a.ID, b.ID})
	if err != nil || !reflect.DeepEqual(cat.VendorIDs, []string{b.ID, a.ID}) {
		t.Fatalf("named: %+v %v", cat, err)
	}
	if _, err := st.SetCategoryVendors(cat.ID, []string{"nope"}); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("an unknown vendor: %v", err)
	}
	all, err := st.ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WithCategoryVendors(all); err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.ID == cat.ID && !reflect.DeepEqual(c.VendorIDs, []string{b.ID, a.ID}) {
			t.Errorf("listed: %+v", c)
		}
	}
	clone, err := st.CloneCategory(cat.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.CategoryVendorIDs(clone.ID); !reflect.DeepEqual(ids, []string{b.ID, a.ID}) {
		t.Errorf("copied: %+v", ids)
	}

	if err := st.DeleteScope(sc.ID); !errors.Is(err, ErrScopeInUse) {
		t.Errorf("deleting a vendor's scope: %v", err)
	}
	if got, _ := st.GetScope(sc.ID); got == nil || got.VendorCount != 1 {
		t.Errorf("the scope: %+v", got)
	}

	if err := st.DeleteVendor(b.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.CategoryVendorIDs(cat.ID); !reflect.DeepEqual(ids, []string{a.ID}) {
		t.Errorf("after deleting a vendor: %+v", ids)
	}
}

// TestVendorAttachments: a vendor's files are added and removed as a
// category's are, and its history says so.
func TestVendorAttachments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	v, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	doc := document("prices.pdf")
	added, err := st.AddAttachments(models.OwnerVendor, v.ID, []models.Attachment{doc}, 2)
	if err != nil || len(added) != 1 {
		t.Fatalf("added: %+v %v", added, err)
	}
	if _, err := st.RemoveAttachment(models.OwnerVendor, v.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	events, _, err := st.ListVendorEvents(v.ID, "", 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("history: %+v %v", events, err)
	}
	seen := map[models.EventChange]bool{}
	for _, e := range events {
		for _, c := range e.Payload.Changes {
			seen[c] = true
		}
	}
	if !seen[models.EventChange{What: "attachment", Key: doc.ID, To: "prices.pdf"}] || !seen[models.EventChange{What: "attachment", Key: doc.ID, From: "prices.pdf"}] {
		t.Errorf("history: %+v", events)
	}
	if _, err := st.AddAttachments(models.OwnerVendor, "nope", []models.Attachment{document("x.pdf")}, 2); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("to no vendor: %v", err)
	}
	files, err := st.StoredFiles()
	if err != nil {
		t.Fatal(err)
	}
	if files["photos/"+doc.ID] {
		t.Error("a removed file is still named")
	}
}
