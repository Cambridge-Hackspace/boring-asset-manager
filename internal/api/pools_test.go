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

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// namedPrinter adds a Brother printer on a local port.
func namedPrinter(t *testing.T, srv *Server, cookie *http.Cookie, name string, port int, media string) string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"`+name+`","driver":"brother_ql","model":"QL-810W","connection":{"type":"tcp","address":"127.0.0.1:`+strconv.Itoa(port)+`"},"media":"`+media+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding %s: %d %s", name, rec.Code, rec.Body)
	}
	var resp struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Printer.ID
}

func TestPoolsThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a := namedPrinter(t, srv, cookie, "A", 9100, "dk-11201")
	b := namedPrinter(t, srv, cookie, "B", 9100, "dk-11201")
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printer-pools", `{"name":"Bench","batch_size":2,"members":[{"printer_id":"`+a+`"},{"printer_id":"`+b+`","share":3}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating: %d %s", rec.Code, rec.Body)
	}
	var created struct{ Pool models.PrinterPool }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Pool.ID
	// In order, since renaming frees the name the first case finds taken.
	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"a taken name", http.MethodPost, "/api/v1/printer-pools", `{"name":"Bench","members":[{"printer_id":"` + a + `"}]}`, http.StatusConflict},
		{"no printers", http.MethodPost, "/api/v1/printer-pools", `{"name":"Empty","members":[]}`, http.StatusBadRequest},
		{"a missing printer", http.MethodPost, "/api/v1/printer-pools", `{"name":"Gone","members":[{"printer_id":"nope"}]}`, http.StatusBadRequest},
		{"renaming", http.MethodPut, "/api/v1/printer-pools/" + id, `{"name":"Workbench","batch_size":2,"members":[{"printer_id":"` + a + `"},{"printer_id":"` + b + `"}]}`, http.StatusOK},
		{"reading", http.MethodGet, "/api/v1/printer-pools/" + id, ``, http.StatusOK},
		{"reading a missing", http.MethodGet, "/api/v1/printer-pools/nope", ``, http.StatusNotFound},
		{"updating a missing", http.MethodPut, "/api/v1/printer-pools/nope", `{"name":"X","members":[{"printer_id":"` + a + `"}]}`, http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}

	// Those who print see pools; only those who manage settings change them.
	printer := userWithPermissions(t, srv, "printer@test.org", rbac.PermLabelsPrint)
	if rec := doJSON(t, srv, printer, http.MethodGet, "/api/v1/printer-pools", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Workbench") {
		t.Errorf("listing as a printer: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, printer, http.MethodDelete, "/api/v1/printer-pools/"+id, ""); rec.Code != http.StatusForbidden {
		t.Errorf("deleting as a printer: %d", rec.Code)
	}

	// The pool takes a template both its printers suit: labels go to it by
	// themselves, since every printer that suits them is in it.
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	asset, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+asset.Asset.ID+`"],"template_id":"`+ql90+`"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"destination":"pool:`+id+`"`) {
		t.Errorf("a label going to the pool by itself: %s", rec.Body)
	}
	spool := spoolList(t, srv, cookie, "")
	if len(spool.Pools) != 1 || len(spool.PoolFits[ql90]) != 1 || spool.PoolFits[ql90][0] != id {
		t.Errorf("the spool's pools: %+v, %+v", spool.Pools, spool.PoolFits)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/printer-pools/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("deleting: %d %s", rec.Code, rec.Body)
	}
	if e := spoolList(t, srv, cookie, "").Entries[0]; e.Destination != "" {
		t.Errorf("a label for a deleted pool: %+v", e)
	}
}

func TestPrintingThroughAPool(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	fast := newFakeQL(t)
	// One that cannot be reached, and one whose media does not suit.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	a := namedPrinter(t, srv, cookie, "Fast", fast.port(), "dk-11201")
	b := namedPrinter(t, srv, cookie, "Dead", dead, "dk-11201")
	c := namedPrinter(t, srv, cookie, "Wide", 9100, "dk-22205")
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printer-pools", `{"name":"Bench","batch_size":2,"members":[{"printer_id":"`+a+`"},{"printer_id":"`+b+`"},{"printer_id":"`+c+`"}]}`)
	var created struct{ Pool models.PrinterPool }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	ql90 := createTemplate(t, srv, cookie, landscapeTemplate)
	var ids []string
	for _, name := range []string{"Drill", "Saw", "Lathe", "Vice"} {
		asset, _ := srv.store.CreateAsset(name, nil, nil, nil)
		ids = append(ids, asset.Asset.ID)
	}
	idsJSON, _ := json.Marshal(ids)
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":`+string(idsJSON)+`,"template_id":"`+ql90+`","destination":"pool:`+created.Pool.ID+`"}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	resp := printSpool(t, srv, cookie, `{}`, http.StatusCreated)
	if len(resp.Jobs) != 1 || resp.Jobs[0].Target != "Bench" || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "Wide sits out Bench") {
		t.Fatalf("printing to the pool: %+v", resp)
	}

	// The dead printer hands back what it takes; the fast one prints it all.
	srv.drainSpool(context.Background())
	jobs := fast.waitForJobs(t, 2, 3*time.Second)
	pages := 0
	for _, j := range jobs {
		decoded, err := brotherql.DecodeJob(j)
		if err != nil {
			t.Fatal(err)
		}
		pages += len(decoded)
	}
	if len(jobs) != 2 || pages != 4 {
		t.Fatalf("the fast printer received %d jobs of %d labels, want 2 of 4 in all", len(jobs), pages)
	}
	job := spoolJobs(t, srv, cookie)[0]
	if job.State != models.SpoolDone || job.Printed != 4 || len(job.Members) != 2 {
		t.Fatalf("the job: %+v", job)
	}
	for _, m := range job.Members {
		if m.Printer == "Dead" && (!m.Dropped || m.Printed != 0) || m.Printer == "Fast" && (m.Dropped || m.Printed != 4) {
			t.Errorf("a member: %+v", m)
		}
	}
	printed := spoolList(t, srv, cookie, "?printed=true")
	if len(printed.Entries) != 4 || printed.Entries[0].PrintedOn != "Fast" {
		t.Errorf("printed: %+v", printed.Entries)
	}
	// Sent again, it goes to the pool once more.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/jobs/"+job.ID+"/resend", `{}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"target":"Bench"`) {
		t.Errorf("sending again: %d %s", rec.Code, rec.Body)
	}
}
