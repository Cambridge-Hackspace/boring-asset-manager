package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func newPrinter(t *testing.T, st *Store, name, conn string) *models.Printer {
	t.Helper()
	address := "10.0.0.9"
	if conn == printers.ConnIPP {
		address = "ipp://cups.lan/printers/" + strings.ToLower(name)
	}
	p, err := st.CreatePrinter(models.Printer{Name: name, Driver: "brother_ql", Model: "QL-810W", Connection: printers.Connection{Type: conn, Address: address}, Media: "dk-22205"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrinterPools(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, b := newPrinter(t, st, "A", printers.ConnTCP), newPrinter(t, st, "B", printers.ConnTCP)
	pool, err := st.CreatePool(PoolInput{Name: " Bench ", Members: []models.PoolMember{{PrinterID: b.ID, Share: 2}, {PrinterID: a.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if pool.Name != "Bench" || pool.BatchSize != 10 || pool.FixedShares || !reflect.DeepEqual(pool.Members, []models.PoolMember{{PrinterID: a.ID, Share: 1}, {PrinterID: b.ID, Share: 2}}) {
		t.Errorf("created: %+v", pool)
	}
	for name, in := range map[string]PoolInput{
		"no name":         {Members: []models.PoolMember{{PrinterID: a.ID}}},
		"no printers":     {Name: "X"},
		"a printer twice": {Name: "X", Members: []models.PoolMember{{PrinterID: a.ID}, {PrinterID: a.ID}}},
		"a huge batch":    {Name: "X", BatchSize: MaxPoolBatch + 1, Members: []models.PoolMember{{PrinterID: a.ID}}},
		"a huge share":    {Name: "X", Members: []models.PoolMember{{PrinterID: a.ID, Share: MaxPoolShare + 1}}},
	} {
		if _, err := st.CreatePool(in); !errors.Is(err, ErrInvalidPool) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := st.CreatePool(PoolInput{Name: "X", Members: []models.PoolMember{{PrinterID: "nope"}}}); !errors.Is(err, ErrPrinterNotFound) {
		t.Errorf("a missing printer: %v", err)
	}
	if _, err := st.CreatePool(PoolInput{Name: "Bench", Members: []models.PoolMember{{PrinterID: a.ID}}}); !errors.Is(err, ErrPoolNameExists) {
		t.Errorf("a taken name: %v", err)
	}
	updated, err := st.UpdatePool(pool.ID, PoolInput{Name: "Bench", BatchSize: 3, FixedShares: true, Members: []models.PoolMember{{PrinterID: a.ID}}})
	if err != nil || updated.BatchSize != 3 || !updated.FixedShares || len(updated.Members) != 1 {
		t.Errorf("updated: %+v, %v", updated, err)
	}
	if _, err := st.UpdatePool("nope", PoolInput{Name: "Y", Members: []models.PoolMember{{PrinterID: a.ID}}}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("updating a missing pool: %v", err)
	}
	// A deleted printer leaves its pools.
	if err := st.DeletePrinter(a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetPool(pool.ID); len(got.Members) != 0 {
		t.Errorf("after deleting its printer: %+v", got)
	}
	if err := st.DeletePool(pool.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePool(pool.ID); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestCuttingAndSharingOutBatches(t *testing.T) {
	entries := []SpoolBatchEntry{{ID: "a", Copies: 4}, {ID: "b", Copies: 4}, {ID: "c", Copies: 20}, {ID: "d", Copies: 1}}
	var sizes [][]string
	for _, b := range cutBatches(entries, 10) {
		var ids []string
		for _, e := range b {
			ids = append(ids, e.ID)
		}
		sizes = append(sizes, ids)
	}
	// A label's copies stay together, even past the size.
	if !reflect.DeepEqual(sizes, [][]string{{"a", "b"}, {"c"}, {"d"}}) {
		t.Errorf("batches: %v", sizes)
	}
	members := []PoolJobMember{{PrinterName: "B", Share: 2}, {PrinterName: "A", Share: 1}}
	if got := shareOut(members, 6); !reflect.DeepEqual(got, []int{0, 1, 0, 0, 1, 0}) {
		t.Errorf("shares 2:1 over six batches: %v", got)
	}
}

// poolFixture adds labels for a pool and makes its job, as the API would.
func poolFixture(t *testing.T, st *Store, pool *models.PrinterPool, members []PoolJobMember, labels int) (string, []models.SpoolEntry) {
	t.Helper()
	tmpl, _ := st.CreateTemplate(sampleTemplate(""))
	var adds []SpoolAdd
	for i := 0; i < labels; i++ {
		a, _ := st.CreateAsset("Bin", nil, nil, nil)
		adds = append(adds, SpoolAdd{AssetID: a.Asset.ID, TemplateID: &tmpl.ID, Destination: models.DestinationPoolPrefix + pool.ID, Copies: 1})
	}
	added, _, err := st.AddToSpool(adds)
	if err != nil {
		t.Fatal(err)
	}
	if added[0].Destination != models.DestinationPoolPrefix+pool.ID {
		t.Fatalf("an entry for a pool: %+v", added[0])
	}
	ids, err := st.CreateSpoolJobs([]SpoolJobNew{{TemplateID: tmpl.ID, TemplateName: "Shelf label", PoolID: pool.ID, PoolName: pool.Name, Members: members, BatchSize: pool.BatchSize, EntryIDs: idsOf(added)}})
	if err != nil {
		t.Fatal(err)
	}
	return ids[0], added
}

func TestPoolMembersTakeBatchesAsTheyFreeUp(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, b, c := newPrinter(t, st, "A", printers.ConnTCP), newPrinter(t, st, "B", printers.ConnTCP), newPrinter(t, st, "C", printers.ConnIPP)
	pool, _ := st.CreatePool(PoolInput{Name: "Bench", BatchSize: 1, Members: []models.PoolMember{{PrinterID: a.ID}, {PrinterID: b.ID}, {PrinterID: c.ID}}})
	// C is behind a spooler: it is given its share, a third, at once.
	members := []PoolJobMember{{PrinterID: a.ID, PrinterName: "A", Share: 1, Pulls: true}, {PrinterID: b.ID, PrinterName: "B", Share: 1, Pulls: true}, {PrinterID: c.ID, PrinterName: "C", Share: 1}}
	job, _ := poolFixture(t, st, pool, members, 6)

	claim := func(p *models.Printer) *SpoolBatch {
		t.Helper()
		got, err := st.ClaimSpoolBatch(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	finish := func(got *SpoolBatch, printer string) {
		t.Helper()
		if err := st.FinishSpoolBatch(got, SpoolOutcome{Printed: []string{got.Entries[0].ID}, Printer: printer}); err != nil {
			t.Fatal(err)
		}
	}
	// A is fast: it finishes each batch before B finishes its first.
	fromB := claim(b)
	took := 0
	for got := claim(a); got != nil; got = claim(a) {
		if got.PoolID != pool.ID {
			t.Fatalf("a pool batch: %+v", got)
		}
		finish(got, "A")
		took++
	}
	fromC := 0
	for got := claim(c); got != nil; got = claim(c) {
		finish(got, "C")
		fromC++
	}
	finish(fromB, "B")
	if took != 3 || fromC != 2 {
		t.Errorf("A took %d batches and C %d; want 3 and 2", took, fromC)
	}
	jobs, _ := st.ListSpoolJobs(time.Time{}, []string{job})
	if jobs[0].State != models.SpoolDone || jobs[0].Target != "Bench" || jobs[0].PoolID == nil || *jobs[0].PoolID != pool.ID {
		t.Fatalf("the job: %+v", jobs[0])
	}
	printed := map[string]int{}
	for _, m := range jobs[0].Members {
		printed[m.Printer] = m.Printed
	}
	if !reflect.DeepEqual(printed, map[string]int{"A": 3, "B": 1, "C": 2}) {
		t.Errorf("progress: %+v", jobs[0].Members)
	}
	if got, _ := st.ListSpool(SpoolFilter{Printed: true, JobID: job}); len(got) != 6 {
		t.Errorf("printed: %d", len(got))
	}
}

func TestAPoolMemberThatFailsHandsItsBatchBack(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, b := newPrinter(t, st, "A", printers.ConnTCP), newPrinter(t, st, "B", printers.ConnTCP)
	pool, _ := st.CreatePool(PoolInput{Name: "Bench", BatchSize: 1, Members: []models.PoolMember{{PrinterID: a.ID}, {PrinterID: b.ID}}})
	members := []PoolJobMember{{PrinterID: a.ID, PrinterName: "A", Share: 1, Pulls: true}, {PrinterID: b.ID, PrinterName: "B", Share: 1, Pulls: true}}
	job, _ := poolFixture(t, st, pool, members, 2)

	got, _ := st.ClaimSpoolBatch(a.ID)
	if err := st.FinishSpoolBatch(got, SpoolOutcome{Failure: "Could not deliver these to A: refused", HandBack: true}); err != nil {
		t.Fatal(err)
	}
	// A is out of this job; B prints both.
	if again, _ := st.ClaimSpoolBatch(a.ID); again != nil {
		t.Errorf("a dropped printer took a batch: %+v", again)
	}
	for n := 0; n < 2; n++ {
		got, _ := st.ClaimSpoolBatch(b.ID)
		if got == nil {
			t.Fatalf("B took %d batches", n)
		}
		if err := st.FinishSpoolBatch(got, SpoolOutcome{Printed: []string{got.Entries[0].ID}, Printer: "B"}); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := st.ListSpoolJobs(time.Time{}, []string{job})
	if jobs[0].State != models.SpoolDone || jobs[0].Printed != 2 {
		t.Errorf("the job: %+v", jobs[0])
	}
	for _, m := range jobs[0].Members {
		if m.Printer == "A" && !m.Dropped {
			t.Errorf("A did not drop out: %+v", m)
		}
	}

	// With every member out, the job fails and its labels wait again.
	job, entries := poolFixture(t, st, pool, members, 1)
	for _, p := range []*models.Printer{a, b} {
		got, _ := st.ClaimSpoolBatch(p.ID)
		if got == nil {
			t.Fatalf("%s took nothing", p.Name)
		}
		if err := st.FinishSpoolBatch(got, SpoolOutcome{Failure: "Could not deliver these to " + p.Name + ": refused", HandBack: true}); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ = st.ListSpoolJobs(time.Time{}, []string{job})
	if jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "No printer of the pool could print these") {
		t.Errorf("a job none could print: %+v", jobs[0])
	}
	if left, _ := st.ListSpool(SpoolFilter{IDs: idsOf(entries)}); left[0].Job != nil || !strings.Contains(left[0].Problem, "Could not deliver these to B") {
		t.Errorf("its label: %+v", left[0])
	}
}

func TestFixedSharesAndDeletingAPool(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	a, b := newPrinter(t, st, "A", printers.ConnTCP), newPrinter(t, st, "B", printers.ConnTCP)
	pool, _ := st.CreatePool(PoolInput{Name: "Bench", BatchSize: 1, FixedShares: true, Members: []models.PoolMember{{PrinterID: a.ID}, {PrinterID: b.ID}}})
	// Fixed shares: nothing is left to pull, each is given its half.
	members := []PoolJobMember{{PrinterID: a.ID, PrinterName: "A", Share: 1}, {PrinterID: b.ID, PrinterName: "B", Share: 1}}
	poolFixture(t, st, pool, members, 4)
	took := 0
	for got, _ := st.ClaimSpoolBatch(a.ID); got != nil; got, _ = st.ClaimSpoolBatch(a.ID) {
		if err := st.FinishSpoolBatch(got, SpoolOutcome{Printed: []string{got.Entries[0].ID}, Printer: "A"}); err != nil {
			t.Fatal(err)
		}
		took++
	}
	if took != 2 {
		t.Errorf("A took %d of 4 batches with equal fixed shares", took)
	}

	// Pulling members' batches fail when their pool goes; its labels
	// waiting are left with nowhere to go.
	pulls := []PoolJobMember{{PrinterID: a.ID, PrinterName: "A", Share: 1, Pulls: true}}
	job, _ := poolFixture(t, st, pool, pulls, 2)
	tmpl, _ := st.CreateTemplate(sampleTemplate(""))
	asset, _ := st.CreateAsset("Bin", nil, nil, nil)
	waiting, _, _ := st.AddToSpool([]SpoolAdd{{AssetID: asset.Asset.ID, TemplateID: &tmpl.ID, Destination: models.DestinationPoolPrefix + pool.ID, Copies: 1}})
	if err := st.DeletePool(pool.ID); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := st.ListSpoolJobs(time.Time{}, []string{job}); jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "Bench was deleted") {
		t.Errorf("the pool's job: %+v", jobs[0])
	}
	if got, _ := st.ListSpool(SpoolFilter{IDs: idsOf(waiting)}); got[0].Destination != "" {
		t.Errorf("a label for the deleted pool: %+v", got[0])
	}
	if _, _, err := st.AddToSpool([]SpoolAdd{{AssetID: asset.Asset.ID, Destination: models.DestinationPoolPrefix + pool.ID, Copies: 1}}); !errors.Is(err, ErrPoolNotFound) {
		t.Errorf("adding for a missing pool: %v", err)
	}
}
