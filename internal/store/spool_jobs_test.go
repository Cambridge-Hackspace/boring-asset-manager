package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// spoolFixture is a store with a printer, a template and waiting entries
// for it, one per name.
func spoolFixture(t *testing.T, names ...string) (*Store, *models.Printer, string, []models.SpoolEntry) {
	t.Helper()
	st := setupTestStore(t)
	t.Cleanup(func() { st.Close() })
	tmpl, _ := st.CreateTemplate(sampleTemplate(""))
	p, err := st.CreatePrinter(models.Printer{Name: "Front desk", Driver: "brother_ql", Model: "QL-810W", Connection: printers.Connection{Type: printers.ConnTCP, Address: "10.0.0.9"}, Media: "dk-22205"})
	if err != nil {
		t.Fatal(err)
	}
	var adds []SpoolAdd
	for _, n := range names {
		a, _ := st.CreateAsset(n, nil, nil, nil)
		adds = append(adds, SpoolAdd{AssetID: a.Asset.ID, TemplateID: &tmpl.ID, Destination: models.DestinationPrinterPrefix + p.ID, Copies: 2})
	}
	added, _, err := st.AddToSpool(adds)
	if err != nil {
		t.Fatal(err)
	}
	return st, p, tmpl.ID, added
}

func idsOf(entries []models.SpoolEntry) []string {
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestSpoolJobsAreClaimedOneAtATimePerPrinter(t *testing.T) {
	st, p, tmpl, entries := spoolFixture(t, "Saw", "Drill", "Lathe")
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	as := st.As(admin.ID)
	jobIDs, err := as.CreateSpoolJobs([]SpoolJobNew{
		{TemplateID: tmpl, TemplateName: "Shelf label", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: idsOf(entries[:2])},
		{TemplateID: tmpl, TemplateName: "Shelf label", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: idsOf(entries[2:])},
	})
	if err != nil || len(jobIDs) != 2 {
		t.Fatalf("making jobs: %v, %v", jobIDs, err)
	}
	// Entries in a job show it, and cannot be changed or printed twice.
	waiting, _ := st.ListSpool(SpoolFilter{})
	if waiting[0].Job == nil || waiting[0].Job.State != models.SpoolWaiting || waiting[0].Job.Target != "Front desk" {
		t.Errorf("an entry in a job: %+v", waiting[0].Job)
	}
	copies := 3
	if err := st.UpdateSpool([]string{entries[0].ID}, SpoolUpdate{Copies: &copies}); !errors.Is(err, ErrSpoolEntryBusy) {
		t.Errorf("changing an entry in a job: %v", err)
	}
	if err := st.RemoveFromSpool([]string{entries[0].ID}); !errors.Is(err, ErrSpoolEntryBusy) {
		t.Errorf("removing an entry in a job: %v", err)
	}
	if _, err := st.CreateSpoolJobs([]SpoolJobNew{{TemplateID: tmpl, TemplateName: "x", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: []string{entries[0].ID}}}); !errors.Is(err, ErrSpoolEntryBusy) {
		t.Errorf("printing an entry twice: %v", err)
	}
	jobs, _ := st.ListSpoolJobs(time.Time{}, nil)
	if len(jobs) != 2 || jobs[1].ID != jobIDs[0] || jobs[1].Labels != 4 || jobs[1].Entries != 2 || jobs[1].State != models.SpoolWaiting || jobs[1].CreatedBy.ID != admin.ID {
		t.Fatalf("jobs, newest first: %+v", jobs)
	}

	// The oldest first; not a second while one is being sent.
	b, err := st.ClaimSpoolBatch(p.ID)
	if err != nil || b == nil || b.JobID != jobIDs[0] || len(b.Entries) != 2 || b.Entries[0].Copies != 2 || b.CreatedBy != admin.ID || b.CreatedByToken {
		t.Fatalf("claiming: %+v, %v", b, err)
	}
	if again, err := st.ClaimSpoolBatch(p.ID); err != nil || again != nil {
		t.Errorf("claiming while sending: %+v, %v", again, err)
	}
	if err := st.CancelSpoolJob(jobIDs[0]); !errors.Is(err, ErrSpoolJobStarted) {
		t.Errorf("canceling a job being sent: %v", err)
	}
	if err := st.HeartbeatSpoolBatch(b.ID); err != nil {
		t.Fatal(err)
	}
	// One printed, one could not be produced.
	if err := as.FinishSpoolBatch(b, SpoolOutcome{Printed: []string{entries[0].ID}, Printer: "Front desk", Problems: map[string]string{entries[1].ID: "Could not be produced: no code"}}); err != nil {
		t.Fatal(err)
	}
	jobs, _ = st.ListSpoolJobs(time.Time{}, []string{jobIDs[0]})
	if jobs[0].State != models.SpoolDone || jobs[0].Printed != 2 || jobs[0].FinishedAt == nil || jobs[0].StartedAt == nil {
		t.Errorf("a finished job: %+v", jobs[0])
	}
	left, _ := st.ListSpool(SpoolFilter{IDs: []string{entries[1].ID}})
	if left[0].Job != nil || left[0].Problem != "Could not be produced: no code" {
		t.Errorf("the label not produced: %+v", left[0])
	}
	if printed, _ := st.ListSpool(SpoolFilter{Printed: true, JobID: jobIDs[0]}); len(printed) != 1 || printed[0].PrintedOn != "Front desk" || printed[0].PrintedBy.ID != admin.ID {
		t.Errorf("printed in the job: %+v", printed)
	}

	// The second job waits until canceled, and its label waits again.
	if err := st.CancelSpoolJob(jobIDs[1]); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.ClaimSpoolBatch(p.ID); b != nil {
		t.Errorf("claimed a canceled job: %+v", b)
	}
	if err := st.UpdateSpool([]string{entries[2].ID}, SpoolUpdate{Copies: &copies}); err != nil {
		t.Errorf("changing a canceled job's label: %v", err)
	}
	if jobs, _ := st.ListSpoolJobs(time.Time{}, []string{jobIDs[1]}); jobs[0].State != models.SpoolCanceled {
		t.Errorf("canceled: %+v", jobs[0])
	}
	if err := st.CancelSpoolJob("nope"); !errors.Is(err, ErrSpoolJobNotFound) {
		t.Errorf("canceling a missing job: %v", err)
	}
}

func TestFailedAndAbandonedSpoolJobs(t *testing.T) {
	st, p, tmpl, entries := spoolFixture(t, "Saw", "Drill")
	ids, _ := st.CreateSpoolJobs([]SpoolJobNew{{TemplateID: tmpl, TemplateName: "Shelf label", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: idsOf(entries[:1])}})
	b, _ := st.ClaimSpoolBatch(p.ID)
	if err := st.FinishSpoolBatch(b, SpoolOutcome{Failure: "Could not deliver these to Front desk: refused"}); err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.ListSpoolJobs(time.Time{}, ids)
	if jobs[0].State != models.SpoolFailed || jobs[0].Error != "Could not deliver these to Front desk: refused" {
		t.Errorf("a failed job: %+v", jobs[0])
	}
	got, _ := st.ListSpool(SpoolFilter{IDs: idsOf(entries[:1])})
	if got[0].Job != nil || got[0].Problem != "Could not deliver these to Front desk: refused" {
		t.Errorf("its label, waiting again: %+v", got[0])
	}

	// A batch whose server stopped beating is put back, maybe printed.
	ids, _ = st.CreateSpoolJobs([]SpoolJobNew{{TemplateID: tmpl, TemplateName: "Shelf label", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: idsOf(entries)}})
	b, _ = st.ClaimSpoolBatch(p.ID)
	if n, err := st.SweepSpool(time.Now().Add(-time.Minute)); err != nil || n != 0 {
		t.Errorf("sweeping a live batch: %d, %v", n, err)
	}
	if _, err := st.Exec("UPDATE spool_batches SET heartbeat_at = ? WHERE id = ?", time.Now().Add(-time.Hour).UTC(), b.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := st.SweepSpool(time.Now().Add(-time.Minute)); err != nil || n != 1 {
		t.Errorf("sweeping an abandoned batch: %d, %v", n, err)
	}
	jobs, _ = st.ListSpoolJobs(time.Time{}, ids)
	if jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "may have printed") {
		t.Errorf("an abandoned job: %+v", jobs[0])
	}
	if got, _ := st.ListSpool(SpoolFilter{}); len(got) != 2 || !strings.Contains(got[0].Problem, "may have printed") || got[0].Job != nil {
		t.Errorf("its labels: %+v", got)
	}
	// Its own server finishing it late changes nothing.
	if err := st.FinishSpoolBatch(b, SpoolOutcome{Printed: idsOf(entries), Printer: p.Name}); !errors.Is(err, ErrSpoolBatchGiven) {
		t.Errorf("finishing late: %v", err)
	}
	if printed, _ := st.ListSpool(SpoolFilter{Printed: true}); len(printed) != 0 {
		t.Errorf("printed late: %+v", printed)
	}

	// A job whose printer is deleted before it starts fails.
	var err error
	if ids, err = st.CreateSpoolJobs([]SpoolJobNew{{TemplateID: tmpl, TemplateName: "Shelf label", PrinterID: p.ID, PrinterName: p.Name, EntryIDs: idsOf(entries[:1])}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := st.SweepSpool(time.Now().Add(-time.Minute)); err != nil || n != 1 {
		t.Errorf("sweeping an orphaned job: %d, %v", n, err)
	}
	if jobs, _ := st.ListSpoolJobs(time.Time{}, ids); jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "Front desk was deleted") {
		t.Errorf("an orphaned job: %+v", jobs[0])
	}
}

// TestDownloadsAreJobs checks a download is recorded as a finished job,
// whose labels are found again by it.
func TestDownloadsAreJobs(t *testing.T) {
	st, _, tmpl, entries := spoolFixture(t, "Saw")
	if err := st.RecordSpoolDownload(idsOf(entries), tmpl, "Shelf label"); err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.ListSpoolJobs(time.Now().Add(-time.Hour), nil)
	if len(jobs) != 1 || !jobs[0].Download || jobs[0].Target != "Download" || jobs[0].State != models.SpoolDone || jobs[0].Printed != 2 {
		t.Fatalf("a download: %+v", jobs)
	}
	if printed, _ := st.ListSpool(SpoolFilter{Printed: true, JobID: jobs[0].ID}); len(printed) != 1 || printed[0].PrintedOn != "Download" {
		t.Errorf("its labels: %+v", printed)
	}
	if later, _ := st.ListSpoolJobs(time.Now().Add(time.Hour), nil); len(later) != 0 {
		t.Errorf("jobs since an hour from now: %+v", later)
	}
}

// TestTokenPermissionsByID checks a token's permissions are read by ID
// only while it works.
func TestTokenPermissionsByID(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	roles, _ := st.ListRoles()
	tok, _, err := st.CreateToken("Script", []string{roles[0].ID}, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, perms, err := st.TokenPermissions(tok.ID); err != nil || got.ID != tok.ID || len(perms) == 0 {
		t.Errorf("a working token: %+v, %v, %v", got, perms, err)
	}
	if _, err := st.RevokeToken(tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.TokenPermissions(tok.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("a revoked token: %v", err)
	}
}
