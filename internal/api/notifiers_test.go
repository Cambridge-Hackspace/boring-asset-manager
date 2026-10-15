package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// testNotifier is a notifier driver for API tests: its "target" option is a
// secret, and a target of "fail" makes sending fail.
type testNotifier struct{}

var (
	testNotifierMu   sync.Mutex
	testNotifierSent []notify.Notification
)

func (testNotifier) Info() notify.DriverInfo {
	return notify.DriverInfo{Driver: "test-notifier", Label: "Test", Options: []notify.OptionInfo{
		{Name: "target", Label: "Target", Required: true, Secret: true},
		{Name: "note", Label: "Note"},
	}}
}

func (testNotifier) Validate(o map[string]string) error {
	if strings.Contains(o["target"], " ") {
		return errors.New("target may not contain spaces")
	}
	return nil
}

func (testNotifier) Send(ctx context.Context, o map[string]string, n notify.Notification) error {
	if o["target"] == "fail" {
		return errors.New("HTTP 404: no such target")
	}
	testNotifierMu.Lock()
	defer testNotifierMu.Unlock()
	testNotifierSent = append(testNotifierSent, n)
	return nil
}

func init() {
	if err := notify.Register(testNotifier{}); err != nil {
		panic(err)
	}
}

type channelResponse struct {
	Channel struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Options    map[string]string `json:"options"`
		SecretsSet map[string]bool   `json:"secrets_set"`
		Enabled    bool              `json:"enabled"`
		Events     []string          `json:"events"`
	} `json:"channel"`
}

func TestNotifierAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/notifiers/drivers", "")
	var drivers struct {
		Drivers []notify.DriverInfo
		Events  []notify.EventKind
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &drivers)
	names := map[string]bool{}
	for _, d := range drivers.Drivers {
		names[d.Driver] = true
	}
	if !names["discord"] || !names["test-notifier"] || len(drivers.Events) != 4 {
		t.Fatalf("drivers: %s", rec.Body)
	}

	body := `{"name":"Stores","driver":"test-notifier","options":{"target":"s3cret","note":"hi"},"events":["category.running_low"]}`
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("secret returned: %s", rec.Body)
	}
	var created channelResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	c := created.Channel
	if !c.Enabled || !c.SecretsSet["target"] || c.Options["note"] != "hi" || len(c.Events) != 1 {
		t.Fatalf("created %+v", c)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers", body); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d", rec.Code)
	}
	for _, bad := range []string{
		`{"name":"X","driver":"pager","options":{}}`,
		`{"name":"X","driver":"test-notifier","options":{}}`,
		`{"name":"X","driver":"test-notifier","options":{"target":"a b"}}`,
		`{"name":"X","driver":"test-notifier","options":{"target":"a"},"events":["asset.created"]}`,
		`{"name":"X","driver":"discord","options":{"webhook_url":"https://example.com/api/webhooks/1/x"}}`,
		`{"name":"","driver":"test-notifier","options":{"target":"a"}}`,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}

	// Updating without the secret keeps it.
	path := "/api/v1/notifiers/" + c.ID
	rec = doJSON(t, srv, cookie, http.MethodPut, path, `{"name":"Stores team","driver":"test-notifier","options":{"target":""},"enabled":false,"events":["category.restocked"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	stored, err := srv.store.GetChannel(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Options["target"] != "s3cret" || stored.Options["note"] != "" || stored.Enabled || stored.Name != "Stores team" || stored.Events[0] != models.EventRestocked {
		t.Errorf("stored %+v", stored)
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/notifiers", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") || !strings.Contains(rec.Body.String(), "Stores team") {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}

	// The test button sends straight away and reports failures.
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/test", ""); rec.Code != http.StatusOK {
		t.Errorf("test: %d %s", rec.Code, rec.Body)
	}
	testNotifierMu.Lock()
	last := testNotifierSent[len(testNotifierSent)-1]
	testNotifierMu.Unlock()
	if !last.Test || last.BaseURL != srv.cfg.Server.BaseURL {
		t.Errorf("test message %+v", last)
	}
	rec = doJSON(t, srv, cookie, http.MethodPut, path, `{"name":"Broken","driver":"test-notifier","options":{"target":"fail"}}`)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, path+"/test", ""); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no such target") {
		t.Errorf("failing test: %d %s", rec.Code, rec.Body)
	}

	// Deliveries are listed with their channel and event.
	doJSON(t, srv, cookie, http.MethodPut, path, `{"name":"Broken","driver":"test-notifier","options":{},"events":["category.running_low"]}`)
	cat, _ := srv.store.CreateCategory("Tape", nil, "", true, false)
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/categories/"+cat.ID+"/running-low", `{"running_low":true}`)
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/notifiers/deliveries?limit=5", "")
	var log struct {
		Deliveries []struct {
			Status       string `json:"status"`
			ChannelName  string `json:"channel_name"`
			EventKind    string `json:"event_kind"`
			CategoryName string `json:"category_name"`
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &log)
	if len(log.Deliveries) != 1 || log.Deliveries[0].ChannelName != "Broken" || log.Deliveries[0].Status != models.DeliveryPending ||
		log.Deliveries[0].CategoryName != "Tape" || log.Deliveries[0].EventKind != models.EventRunningLow {
		t.Errorf("deliveries: %s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/notifiers/deliveries?limit=500", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit 500: %d", rec.Code)
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, path, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: %d", rec.Code)
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		if rec := doJSON(t, srv, cookie, m, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s after delete: %d", m, rec.Code)
		}
	}
}

func TestNotifierPermissions(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	stores := userWithPermissions(t, srv, "stores@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermCategoriesManage)
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/notifiers"},
		{http.MethodGet, "/api/v1/notifiers/drivers"},
		{http.MethodGet, "/api/v1/notifiers/deliveries"},
		{http.MethodPost, "/api/v1/notifiers"},
		{http.MethodPut, "/api/v1/notifiers/x"},
		{http.MethodDelete, "/api/v1/notifiers/x"},
		{http.MethodPost, "/api/v1/notifiers/x/test"},
	} {
		if rec := doJSON(t, srv, stores, r.method, r.path, "{}"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", r.method, r.path, rec.Code)
		}
	}
}
