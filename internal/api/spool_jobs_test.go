package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// codedTemplate is landscapeTemplate needing a Micro QR code, which assets
// without one cannot be labeled with.
const codedTemplate = `{"id":"coded","name":"Coded","units":"mm","width":90,"height":29,"dpi":300,"elements":[
  {"type":"generator","generator_type":"micro_qr","x":3,"y":3,"width":23,"height":23},
  {"type":"text","x":28,"y":4,"width":58,"height":10,"font_size":16,"content_binding":"{{asset.name}}"}]}`

// spoolTo adds assets to the spool for a printer with a template.
func spoolTo(t *testing.T, srv *Server, cookie *http.Cookie, printerID, templateID string, copies int, assetIDs ...string) []models.SpoolEntry {
	t.Helper()
	ids, _ := json.Marshal(assetIDs)
	body := `{"asset_ids":` + string(ids) + `,"template_id":"` + templateID + `","destination":"printer:` + printerID + `","copies":` + strconv.Itoa(copies) + `}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding to the spool: %d %s", rec.Code, rec.Body)
	}
	var resp SpoolAddResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Added
}

func printSpool(t *testing.T, srv *Server, cookie *http.Cookie, body string, want int) SpoolPrintResponse {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/print", body)
	if rec.Code != want {
		t.Fatalf("printing: %d, want %d: %s", rec.Code, want, rec.Body)
	}
	var resp SpoolPrintResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp
}

func spoolJobs(t *testing.T, srv *Server, cookie *http.Cookie) []models.SpoolJob {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/spool/jobs", "")
	var resp struct{ Jobs []models.SpoolJob }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Jobs
}

func TestPrintingFromTheSpool(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	coded := createTemplate(t, srv, cookie, codedTemplate)
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	saw, _ := srv.store.CreateAsset("Saw", nil, nil, nil)
	lathe, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)
	spoolTo(t, srv, cookie, printerID, ql90, 2, drill.Asset.ID, saw.Asset.ID)
	bad := spoolTo(t, srv, cookie, printerID, coded, 1, lathe.Asset.ID)
	// Something going to a download is not printed.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+lathe.Asset.ID+`"],"template_id":"`+ql90+`","destination":"download"}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	// Everything ready, one job per printer and template.
	resp := printSpool(t, srv, cookie, `{}`, http.StatusCreated)
	if len(resp.Jobs) != 2 || resp.Jobs[0].Labels+resp.Jobs[1].Labels != 5 || len(resp.Skipped) != 0 {
		t.Fatalf("printing: %+v", resp)
	}
	spool := spoolList(t, srv, cookie, "")
	if e := entryFor(spool, drill.Asset.ID); e.Job == nil || e.Job.State != models.SpoolWaiting || e.Job.Target != "QL dk-11201" || e.Ready {
		t.Errorf("an entry waiting to print: %+v", e)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/remove", `{"entry_ids":["`+entryFor(spool, drill.Asset.ID).ID+`"]}`); rec.Code != http.StatusConflict {
		t.Errorf("removing an entry being printed: %d", rec.Code)
	}
	if again := printSpool(t, srv, cookie, `{}`, http.StatusOK); len(again.Jobs) != 0 {
		t.Errorf("printing what is already printing: %+v", again)
	}

	srv.drainSpool(context.Background())
	jobs := ql.waitForJobs(t, 1, 3*time.Second)
	if len(jobs) != 1 {
		t.Fatalf("the printer received %d jobs, want 1", len(jobs))
	}
	pages, err := brotherql.DecodeJob(jobs[0])
	if err != nil || len(pages) != 4 {
		t.Fatalf("the job has %d pages (%v), want 4", len(pages), err)
	}
	spool = spoolList(t, srv, cookie, "")
	if e := entryFor(spool, lathe.Asset.ID); e == nil || e.Job != nil {
		t.Errorf("the lathe's labels: %+v", spool.Entries)
	}
	for _, e := range spool.Entries {
		if e.ID == bad[0].ID && (len(e.Problems) != 1 || !strings.HasPrefix(e.Problems[0], "Could not be produced")) {
			t.Errorf("the label that could not be produced: %+v", e)
		}
	}
	listed := spoolJobs(t, srv, cookie)
	states := map[string]string{}
	for _, j := range listed {
		states[j.TemplateName] = j.State
	}
	if states["QL 90x29"] != models.SpoolDone || states["Coded"] != models.SpoolFailed {
		t.Errorf("jobs: %+v", listed)
	}
	events, _, _ := srv.store.ListAssetEvents(drill.Asset.ID, "", 1)
	if len(events) != 1 || events[0].Payload.Label == nil || events[0].Payload.Label.Printer != "QL dk-11201" || events[0].Payload.Actor.ID != admin.ID {
		t.Errorf("the drill's history: %+v", events)
	}

	// Listed entries that cannot print are said so.
	var download string
	for _, e := range spool.Entries {
		if e.Destination == models.DestinationDownload {
			download = e.ID
		}
	}
	resp = printSpool(t, srv, cookie, `{"entry_ids":["`+download+`"]}`, http.StatusOK)
	if len(resp.Skipped) != 1 || !strings.Contains(resp.Skipped[0].Reason, "download") {
		t.Errorf("printing a download: %+v", resp)
	}
}

func TestSpoolJobsWaitTheirTurnAndPrintersWorkInParallel(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	front, back := newFakeQL(t), newFakeQL(t)
	frontID := createPrinter(t, srv, cookie, front.port(), "dk-11201")
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Back room","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:`+strconv.Itoa(back.port())+`"},"media":"dk-11201"}`)
	var created struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	b, _ := srv.store.CreateAsset("Saw", nil, nil, nil)
	spoolTo(t, srv, cookie, frontID, ql90, 1, a.Asset.ID)
	spoolTo(t, srv, cookie, created.Printer.ID, ql90, 1, b.Asset.ID)
	printSpool(t, srv, cookie, `{}`, http.StatusCreated)

	// Someone is printing straight to the front desk: its job waits, and
	// the back room's goes out meanwhile.
	turn := srv.printerTurn(frontID)
	turn.Lock()
	done := make(chan struct{})
	go func() {
		srv.drainSpool(context.Background())
		close(done)
	}()
	if got := back.waitForJobs(t, 1, 3*time.Second); len(got) != 1 {
		t.Fatal("the back room printed nothing while the front desk was busy")
	}
	time.Sleep(50 * time.Millisecond)
	if got := front.waitForJobs(t, 1, 0); len(got) != 0 {
		t.Fatal("the front desk printed during someone else's turn")
	}
	turn.Unlock()
	<-done
	if got := front.waitForJobs(t, 1, 3*time.Second); len(got) != 1 {
		t.Fatal("the front desk never printed")
	}
}

func TestFailedSpoolJobsWaitToBeTriedAgain(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	printerID := createPrinter(t, srv, cookie, port, "dk-11201")
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	entries := spoolTo(t, srv, cookie, printerID, ql90, 1, a.Asset.ID)

	// A waiting job can be canceled; its label waits again.
	resp := printSpool(t, srv, cookie, `{}`, http.StatusCreated)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+resp.Jobs[0].ID+"/cancel", ""); rec.Code != http.StatusOK {
		t.Fatalf("canceling: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/nope/cancel", ""); rec.Code != http.StatusNotFound {
		t.Errorf("canceling a missing job: %d", rec.Code)
	}
	if e := spoolList(t, srv, cookie, "").Entries[0]; e.Job != nil || !e.Ready {
		t.Errorf("after canceling: %+v", e)
	}

	// The printer cannot be reached: the label waits with why, and can be
	// printed again.
	resp = printSpool(t, srv, cookie, `{}`, http.StatusCreated)
	srv.drainSpool(context.Background())
	e := spoolList(t, srv, cookie, "").Entries[0]
	if e.ID != entries[0].ID || !e.Ready || len(e.Problems) != 1 || !strings.Contains(e.Problems[0], "Could not deliver these to QL dk-11201") {
		t.Errorf("after failing: %+v", e)
	}
	if jobs := spoolJobs(t, srv, cookie); jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "Could not deliver") {
		t.Errorf("the failed job: %+v", jobs[0])
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+resp.Jobs[0].ID+"/cancel", ""); rec.Code != http.StatusConflict {
		t.Errorf("canceling a finished job: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+resp.Jobs[0].ID+"/resend", `{}`); rec.Code != http.StatusConflict {
		t.Errorf("sending again a job that printed nothing: %d %s", rec.Code, rec.Body)
	}
	if again := printSpool(t, srv, cookie, `{}`, http.StatusCreated); len(again.Jobs) != 1 {
		t.Errorf("trying again: %+v", again)
	}
}

func TestSendingAPrintJobAgain(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	spoolTo(t, srv, cookie, printerID, ql90, 2, a.Asset.ID)
	job := printSpool(t, srv, cookie, `{}`, http.StatusCreated).Jobs[0]
	srv.drainSpool(context.Background())
	ql.waitForJobs(t, 1, 3*time.Second)

	// Again on the same printer, at once.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+job.ID+"/resend", `{}`)
	var again SpoolResendResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if rec.Code != http.StatusCreated || len(again.Added) != 1 || again.Added[0].Copies != 2 || len(again.Jobs) != 1 {
		t.Fatalf("sending again: %d %s", rec.Code, rec.Body)
	}
	srv.drainSpool(context.Background())
	if got := ql.waitForJobs(t, 2, 3*time.Second); len(got) != 2 {
		t.Fatalf("the printer received %d jobs, want 2", len(got))
	}
	// Or as a download, to fetch from the spool.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+job.ID+"/resend", `{"destination":"download"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if rec.Code != http.StatusCreated || len(again.Added) != 1 || again.Added[0].Destination != "download" || len(again.Jobs) != 0 {
		t.Fatalf("sending again as a download: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/nope/resend", `{}`); rec.Code != http.StatusNotFound {
		t.Errorf("sending again a missing job: %d", rec.Code)
	}
	// Downloads are jobs too.
	id := again.Added[0].ID
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+id+`"],"format":"pdf"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if jobs := spoolJobs(t, srv, cookie); len(jobs) != 3 || !jobs[0].Download || jobs[0].State != models.SpoolDone {
		t.Errorf("jobs: %+v", jobs)
	}
}

// TestSpoolJobsPrintAsWhoAskedMaySee checks labels are laid out with the
// permissions of whoever asked for the job, as they are when it is sent.
func TestSpoolJobsPrintAsWhoAskedMaySee(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ql := newFakeQL(t)
	printerID := createPrinter(t, srv, cookie, ql.port(), "dk-11201")
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	printerUser, _ := srv.store.CreateNamedUser("printer@test.org", "Printer", "UserSecret123!", true)
	role, _ := srv.store.CreateRole(store.RoleInput{Name: "Printers", Permissions: []string{"assets.read", "labels.print"}})
	_ = srv.store.SetUserRoles(printerUser.ID, []string{role.ID})
	theirs := sessionCookie(t, srv, printerUser.ID)
	spoolTo(t, srv, theirs, printerID, ql90, 1, a.Asset.ID)
	printSpool(t, srv, theirs, `{}`, http.StatusCreated)

	// They leave before it is sent: nothing is printed for them.
	if err := srv.store.UpdateUserStatus(printerUser.ID, false); err != nil {
		t.Fatal(err)
	}
	srv.drainSpool(context.Background())
	if got := ql.waitForJobs(t, 1, 100*time.Millisecond); len(got) != 0 {
		t.Error("printed for someone no longer active")
	}
	if jobs := spoolJobs(t, srv, cookie); jobs[0].State != models.SpoolFailed || !strings.Contains(jobs[0].Error, "no longer") {
		t.Errorf("the job: %+v", jobs[0])
	}
}

// TestPlanningSplitsBigJobs checks a job holds at most MaxExportAssets
// labels, and the rest go in another.
func TestPlanningSplitsBigJobs(t *testing.T) {
	tmpl, printer := "t", "p"
	o := &spoolOptions{templates: map[string]*labels.Template{}, printers: map[string]*models.Printer{printer: {ID: printer, Name: "Front"}}, unfit: map[string]map[string]string{tmpl: {}}}
	o.templates[tmpl] = &labels.Template{ID: tmpl, Name: "Tag"}
	var entries []models.SpoolEntry
	for i := 0; i < 30; i++ {
		entries = append(entries, models.SpoolEntry{ID: strconv.Itoa(i), TemplateID: &tmpl, Destination: "printer:" + printer, Copies: 20})
	}
	jobs, skipped := o.plan(entries, true)
	if len(jobs) != 2 || len(jobs[0].EntryIDs) != 25 || len(jobs[1].EntryIDs) != 5 || len(skipped) != 0 {
		t.Errorf("planned %d jobs of %d and %d", len(jobs), len(jobs[0].EntryIDs), len(jobs[len(jobs)-1].EntryIDs))
	}
}
