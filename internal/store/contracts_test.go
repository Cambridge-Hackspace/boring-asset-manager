package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// coverIDs lists the contracts covering an asset, as ID@category.
func coverIDs(cs []models.ContractCover) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID+"@"+c.CategoryID)
	}
	return strings.Join(out, " ")
}

// TestContractsSaveShowAndCover: a contract is made with its vendor, dates,
// cost and coverage, which read back, the cost exactly; it covers the
// assets it names and every asset in the categories it names or below
// them, once each, by when it ends; editing it records what changed, and
// deleting it leaves what it covered.
func TestContractsSaveShowAndCover(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateCategory("Tools", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	drills, err := st.CreateCategory("Drills", &tools.ID, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateCategory("Other", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	drill, err := st.CreateAsset("Drill", []string{drills.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	saw, err := st.CreateAsset("Saw", []string{other.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	service, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: " Maintenance ", Name: " Workshop service ", Reference: "AG-1",
		StartDate: "2026-01-01", EndDate: "2027-06-30", AutoRenews: true, NoticeDays: 30, Cost: "1200.5", Currency: "gbp", Billing: "yearly",
		Notes: "**Annual**", CategoryIDs: []string{tools.ID}, AssetIDs: []string{saw.Asset.ID, saw.Asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if service.Kind != "maintenance" || service.Name != "Workshop service" || service.VendorName != "Acme" || service.Cost != "1200.50" || service.Currency != "GBP" ||
		service.NoticeDeadline != "2027-05-31" || len(service.AssetIDs) != 1 || len(service.CategoryIDs) != 1 || service.Categories[0].Name != "Tools" {
		t.Errorf("made: %+v", service)
	}
	warranty, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "warranty", Name: "Drill warranty", EndDate: "2026-12-31", AssetIDs: []string{drill.Asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	license, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "license", Name: "CAD", CategoryIDs: []string{drills.ID}})
	if err != nil {
		t.Fatal(err)
	}

	// The drill: its own warranty, then the service through Tools, above
	// Drills, and the license, with no end, last.
	a, err := st.GetAssetByID(drill.Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := coverIDs(a.Contracts), warranty.ID+"@ "+service.ID+"@"+tools.ID+" "+license.ID+"@"+drills.ID; got != want {
		t.Errorf("the drill is covered by %s, want %s", got, want)
	}
	listed, err := st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range listed {
		switch l.Asset.ID {
		case drill.Asset.ID:
			if len(l.Contracts) != 3 {
				t.Errorf("the drill, listed: %+v", l.Contracts)
			}
		case saw.Asset.ID:
			if coverIDs(l.Contracts) != service.ID+"@" || l.Contracts[0].Cost != "1200.50" || l.Contracts[0].VendorName != "Acme" {
				t.Errorf("the saw, listed: %+v", l.Contracts)
			}
		}
	}
	all, err := st.ListContracts()
	if err != nil || len(all) != 3 || all[0].ID != warranty.ID || all[1].ID != service.ID || all[2].ID != license.ID {
		t.Errorf("listed by end: %+v %v", all, err)
	}

	// Edited: what changed is in its history, costs by name only for those
	// who see them, and coverage by name.
	in := ContractInputOf(*service)
	in.Cost, in.AutoRenews, in.NoticeDays, in.Notes = "1300", false, 0, "Changed"
	in.AssetIDs = nil
	edited, err := st.UpdateContract(service.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if edited.NoticeDeadline != "" || edited.Cost != "1300.00" || len(edited.AssetIDs) != 0 {
		t.Errorf("edited: %+v", edited)
	}
	events, _, err := st.ListContractEvents(service.ID, "", 10)
	if err != nil || len(events) != 2 || events[0].Kind != models.EventContractEdited || events[1].Kind != models.EventContractCreated {
		t.Fatalf("history: %+v %v", events, err)
	}
	if got := changesOf(events[0]); got != "auto_renews:yes>no notice_days:30>0 cost:1200.50 GBP>1300.00 GBP notes:> asset:Saw>" {
		t.Errorf("edited, in its history: %s", got)
	}
	if got := changesOf(events[1]); !strings.Contains(got, "kind:>maintenance") || !strings.Contains(got, "auto_renews:no>yes") ||
		!strings.Contains(got, "notice_days:0>30") || !strings.Contains(got, "category:>Tools") || !strings.Contains(got, "asset:>Saw") {
		t.Errorf("made, in its history: %s", got)
	}
	if events[0].Payload.Contract == nil || events[0].Payload.Contract.VendorName != "Acme" {
		t.Errorf("the event names %+v", events[0].Payload.Contract)
	}

	// A vendor with contracts is not deleted; deleting them leaves the
	// assets and categories they covered.
	if err := st.DeleteVendor(acme.ID); !errors.Is(err, ErrVendorHasContracts) {
		t.Errorf("deleting the vendor: %v", err)
	}
	for _, id := range []string{service.ID, warranty.ID, license.ID} {
		if err := st.DeleteContract(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteContract(service.ID); !errors.Is(err, ErrContractNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	if a, err = st.GetAssetByID(drill.Asset.ID); err != nil || len(a.Contracts) != 0 {
		t.Errorf("the drill after: %+v %v", a, err)
	}
	if err := st.DeleteVendor(acme.ID); err != nil {
		t.Errorf("deleting the vendor after: %v", err)
	}
}

// TestContractsRefuseWhatCannotBeKept: bad kinds, dates, costs and
// coverage are refused, and nothing is kept.
func TestContractsRefuseWhatCannotBeKept(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	ok := ContractInput{VendorID: acme.ID, Kind: "lease", Name: "Printer lease"}
	for name, change := range map[string]func(*ContractInput){
		"no name":                 func(in *ContractInput) { in.Name = " " },
		"no vendor":               func(in *ContractInput) { in.VendorID = "" },
		"an unknown kind":         func(in *ContractInput) { in.Kind = "rental" },
		"a bad date":              func(in *ContractInput) { in.EndDate = "30/06/2027" },
		"an end before its start": func(in *ContractInput) { in.StartDate, in.EndDate = "2027-01-02", "2027-01-01" },
		"renewing with no end":    func(in *ContractInput) { in.AutoRenews = true },
		"a negative notice":       func(in *ContractInput) { in.NoticeDays = -1 },
		"a cost with no currency": func(in *ContractInput) { in.Cost = "10" },
		"too many places":         func(in *ContractInput) { in.Cost, in.Currency = "10.001", "GBP" },
		"an unknown billing":      func(in *ContractInput) { in.Billing = "weekly" },
	} {
		in := ok
		change(&in)
		if _, err := st.CreateContract(in); !errors.Is(err, ErrInvalidContract) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := st.CreateContract(ContractInput{VendorID: "nope", Kind: "lease", Name: "x"}); !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("an unknown vendor: %v", err)
	}
	if _, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "lease", Name: "x", AssetIDs: []string{"nope"}}); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("an unknown asset: %v", err)
	}
	if _, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "lease", Name: "x", CategoryIDs: []string{"nope"}}); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("an unknown category: %v", err)
	}
	if _, err := st.UpdateContract("nope", ok); !errors.Is(err, ErrContractNotFound) {
		t.Errorf("updating none: %v", err)
	}
	if all, err := st.ListContracts(); err != nil || len(all) != 0 {
		t.Errorf("kept: %+v %v", all, err)
	}
}

// remindersOf lists the contract reminders recorded, oldest first, as
// kind:contract name@date.
func remindersOf(t *testing.T, st *Store) []string {
	t.Helper()
	all, err := st.ListContracts()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range all {
		events, _, err := st.ListContractEvents(c.ID, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			if e.Kind != models.EventContractExpiring && e.Kind != models.EventContractExpired || seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			out = append(out, e.Kind+":"+e.Payload.Contract.Name+"@"+e.Payload.DueAt.Format(DateLayout))
		}
	}
	return out
}

// TestContractRemindersFireOnce: contract.expiring is recorded once, the
// lead time before a contract ends, or before its notice deadline when it
// renews automatically, and again for a new end; contract.expired once,
// on the day it ends. Servers polling at once record each once.
func TestContractRemindersFireOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	acme, err := st.CreateVendor(VendorInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		at, _ := time.Parse(DateLayout, s)
		return at.Add(9 * time.Hour)
	}
	lease, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "lease", Name: "Lease", EndDate: "2027-06-30"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "service", Name: "Service", EndDate: "2027-06-30", AutoRenews: true, NoticeDays: 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "license", Name: "Forever"}); err != nil {
		t.Fatal(err)
	}

	// Before the lead time, nothing.
	if n, err := st.RecordContractReminders(day("2027-02-28"), 30); err != nil || n != 0 {
		t.Errorf("early: %d %v", n, err)
	}
	// 30 days before the service's notice deadline, 1 May, it is reminded,
	// by servers polling at once, once.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.RecordContractReminders(day("2027-04-01"), 30); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := strings.Join(remindersOf(t, st), " "); got != "contract.expiring:Service@2027-05-01" {
		t.Errorf("at the notice deadline's lead time: %s", got)
	}
	// 30 days before the lease ends, it is; again later, nothing new.
	for _, d := range []string{"2027-05-31", "2027-06-15", "2027-06-29"} {
		if _, err := st.RecordContractReminders(day(d), 30); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(remindersOf(t, st), " "); got != "contract.expiring:Lease@2027-06-30 contract.expiring:Service@2027-05-01" {
		t.Errorf("at the end's lead time: %s", got)
	}
	// On the day both end, each is recorded as ended, once.
	for i := 0; i < 2; i++ {
		if _, err := st.RecordContractReminders(day("2027-06-30"), 30); err != nil {
			t.Fatal(err)
		}
	}
	got := remindersOf(t, st)
	if strings.Join(got, " ") != "contract.expiring:Lease@2027-06-30 contract.expired:Lease@2027-06-30 contract.expiring:Service@2027-05-01 contract.expired:Service@2027-06-30" {
		t.Errorf("on the day: %v", got)
	}
	// A new end is a new reminder, from its own lead time.
	in := ContractInputOf(*lease)
	in.EndDate = "2028-06-30"
	if _, err := st.UpdateContract(lease.ID, in); err != nil {
		t.Fatal(err)
	}
	if n, err := st.RecordContractReminders(day("2027-07-01"), 30); err != nil || n != 0 {
		t.Errorf("long before the new end: %d %v", n, err)
	}
	if n, err := st.RecordContractReminders(day("2028-06-01"), 30); err != nil || n != 1 {
		t.Errorf("before the new end: %d %v", n, err)
	}
	// One whose date passed before it was first seen is not announced.
	if _, err := st.CreateContract(ContractInput{VendorID: acme.ID, Kind: "other", Name: "Old", EndDate: "2028-01-01"}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.RecordContractReminders(day("2028-06-02"), 30); err != nil || n != 0 {
		t.Errorf("one long ended: %d %v", n, err)
	}
}
