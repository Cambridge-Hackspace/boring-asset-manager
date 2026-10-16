package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// lending sets up a lendable category with a subcategory, a lender and a
// borrower.
func lending(t *testing.T, st *Store) (tools, drills *models.Category, lender, borrower *models.User) {
	t.Helper()
	tools, err := st.CreateCategoryFrom(CategoryInput{Name: "Tools", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	drills, err = st.CreateCategoryFrom(CategoryInput{Name: "Drills", ParentID: &tools.ID})
	if err != nil {
		t.Fatal(err)
	}
	if lender, err = st.CreateNamedUser("stores@hackspace.org", "Stores", "a long enough password", true); err != nil {
		t.Fatal(err)
	}
	if borrower, err = st.CreateNamedUser("bo@hackspace.org", "Bo", "a long enough password", true); err != nil {
		t.Fatal(err)
	}
	return tools, drills, lender, borrower
}

func TestLendingAndReturning(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	_, drills, lender, bo := lending(t, st)
	lent := st.As(lender.ID)

	// A subcategory of a lendable category lends too.
	drill := asset(t, st, "Drill", drills.ID)
	if a, _ := st.GetAssetByID(drill); !a.Lendable || a.Loan != nil {
		t.Fatalf("before lending: %+v", a)
	}
	due := time.Now().Add(48 * time.Hour)
	a, err := lent.CheckOut(drill, bo.ID, &due)
	if err != nil {
		t.Fatal(err)
	}
	if l := a.Loan; l == nil || l.Borrower.ID != bo.ID || l.Borrower.Name != "Bo" || l.DueAt == nil || l.Overdue || l.AssetName != "Drill" {
		t.Fatalf("loan %+v", a.Loan)
	}
	e := history(t, st, drill)[0]
	if e.Kind != models.EventAssetCheckedOut || e.Payload.Borrower == nil || e.Payload.Borrower.Name != "Bo" || e.Payload.DueAt == nil || e.Payload.Actor == nil || e.Payload.Actor.ID != lender.ID {
		t.Errorf("checked out: %+v", e.Payload)
	}

	// Lent, it cannot be lent again or archived.
	if _, err := lent.CheckOut(drill, lender.ID, nil); !errors.Is(err, ErrAlreadyLent) {
		t.Errorf("lending twice: %v", err)
	}
	if err := st.DeleteAsset(drill); !errors.Is(err, ErrAssetOnLoan) {
		t.Errorf("archiving a lent asset: %v", err)
	}
	if list, _ := st.FilterAssets(AssetFilter{Loan: LoansOpen}); len(list) != 1 {
		t.Errorf("lent out: %d", len(list))
	}
	if list, _ := st.FilterAssets(AssetFilter{Loan: LoansOverdue}); len(list) != 0 {
		t.Errorf("overdue: %d", len(list))
	}

	// Only the borrower, or a lender, checks it in.
	if _, err := st.CheckIn(drill, lender.ID, nil); !errors.Is(err, ErrNotYourLoan) {
		t.Errorf("someone else's loan: %v", err)
	}
	a, err = st.As(bo.ID).CheckIn(drill, bo.ID, nil)
	if err != nil || a.Loan != nil {
		t.Fatalf("checked in %+v, %v", a, err)
	}
	if e := history(t, st, drill)[0]; e.Kind != models.EventAssetCheckedIn || e.Payload.Borrower.ID != bo.ID || e.Payload.Actor.ID != bo.ID {
		t.Errorf("checked in: %+v", e.Payload)
	}
	if _, err := st.CheckIn(drill, "", nil); !errors.Is(err, ErrNotLent) {
		t.Errorf("checking in twice: %v", err)
	}

	// The loan stays in the borrower's history, as does a second one.
	if _, err := lent.CheckOut(drill, bo.ID, nil); err != nil {
		t.Fatal(err)
	}
	if loans, err := st.ListLoans(LoanFilter{BorrowerID: bo.ID}); err != nil || len(loans) != 2 || loans[0].CheckedInAt != nil || loans[1].CheckedInAt == nil {
		t.Errorf("bo's loans: %+v, %v", loans, err)
	}
	if loans, _ := st.ListLoans(LoanFilter{State: LoansOpen}); len(loans) != 1 {
		t.Errorf("open loans: %d", len(loans))
	}
	if loans, _ := st.ListLoans(LoanFilter{BorrowerID: lender.ID}); len(loans) != 0 {
		t.Errorf("the lender's loans: %d", len(loans))
	}

	// A borrower who leaves keeps their loan, marked as archived.
	if err := st.DeleteUser(bo.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.GetAssetByID(drill); a.Loan == nil || !a.Loan.Borrower.Archived {
		t.Errorf("after bo left: %+v", a.Loan)
	}
	if people, _ := st.ListPeople(); len(people) != 1 || people[0].ID != lender.ID {
		t.Errorf("people %+v", people)
	}
}

func TestWhatCannotBeLent(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tools, _, lender, bo := lending(t, st)
	plain := asset(t, st, "Bench")
	screws, err := st.CreateAssetWithStock("Screws", []string{tools.ID}, nil, nil, &StockInput{Quantity: intp(10)})
	if err != nil {
		t.Fatal(err)
	}
	broken := asset(t, st, "Saw", tools.ID)
	if _, _, err := st.SetAssetCondition(broken, models.ConditionOutOfService, "Blunt"); err != nil {
		t.Fatal(err)
	}
	gone := asset(t, st, "Old saw", tools.ID)
	archive(t, st, gone)
	fine := asset(t, st, "Drill", tools.ID)
	disabled, err := st.CreateUser("off@hackspace.org", "a long enough password", false)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)

	for what, c := range map[string]struct {
		asset, borrower string
		due             *time.Time
		want            error
	}{
		"not lendable":          {plain, bo.ID, nil, ErrNotLendable},
		"counted by quantity":   {screws.Asset.ID, bo.ID, nil, ErrLentByQuantity},
		"out of service":        {broken, bo.ID, nil, ErrOutOfService},
		"archived":              {gone, bo.ID, nil, ErrAssetArchived},
		"missing":               {"no-such-asset", bo.ID, nil, ErrAssetNotFound},
		"a disabled borrower":   {fine, disabled.ID, nil, ErrBorrowerNotFound},
		"an unknown borrower":   {fine, "no-such-user", nil, ErrBorrowerNotFound},
		"due before it is lent": {fine, bo.ID, &past, ErrDueInPast},
	} {
		if _, err := st.As(lender.ID).CheckOut(c.asset, c.borrower, c.due); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", what, err, c.want)
		}
	}
	if loans, _ := st.ListLoans(LoanFilter{}); len(loans) != 0 {
		t.Errorf("refusals left %d loans", len(loans))
	}
}

func TestOverdueLoansAreAnnouncedOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	tools, _, lender, bo := lending(t, st)
	drill := asset(t, st, "Drill", tools.ID)
	saw := asset(t, st, "Saw", tools.ID)
	ladder := asset(t, st, "Ladder", tools.ID)
	soon := time.Now().Add(time.Hour)
	for _, id := range []string{drill, saw} {
		if _, err := st.As(lender.ID).CheckOut(id, bo.ID, &soon); err != nil {
			t.Fatal(err)
		}
	}
	// No due date, never overdue; and one back before its time.
	if _, err := st.CheckOut(ladder, bo.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CheckIn(saw, "", nil); err != nil {
		t.Fatal(err)
	}

	if n, err := st.RecordOverdueLoans(time.Now()); err != nil || n != 0 {
		t.Fatalf("before it is due: %d, %v", n, err)
	}
	later := time.Now().Add(2 * time.Hour)
	// Several servers sweeping at once announce it once.
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := st.RecordOverdueLoans(later)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 {
		t.Errorf("announced %d times", total)
	}
	e := history(t, st, drill)[0]
	if e.Kind != models.EventAssetOverdue || e.Payload.Trigger != models.TriggerSchedule || e.Payload.Borrower.ID != bo.ID || e.Payload.DueAt == nil || e.Payload.Actor != nil {
		t.Errorf("overdue: %+v", e.Payload)
	}
	if models.HistoryOnly(e.Kind) {
		t.Error("channels cannot hear of overdue loans")
	}
	if n, _ := st.RecordOverdueLoans(later.Add(time.Hour)); n != 0 {
		t.Errorf("announced again: %d", n)
	}
}
