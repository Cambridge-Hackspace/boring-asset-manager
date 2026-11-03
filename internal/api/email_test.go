package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

const emailChannel = `{"name": "Keepers", "driver": "email", "options": {"to": "keepers@example.org"}, "enabled": true, "events": ["asset.overdue"]}`

func TestAnEmailChannelNeedsEmailSetUp(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers", emailChannel)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "email is not set up") {
		t.Errorf("without [email]: %d %s", rec.Code, rec.Body)
	}
}

func TestAnEmailChannelIsSentThroughTheConfiguredServer(t *testing.T) {
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Email = config.EmailConfig{Host: "mail.test", TLS: config.EmailSTARTTLS, From: "bam@example.org"}
	st := setupTestStore(t)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	dialed := ""
	srv.mailer.Dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("no route to host")
	}
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers", emailChannel)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating: %d %s", rec.Code, rec.Body)
	}
	var created channelResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/notifiers/"+created.Channel.ID+"/test", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no route to host") || dialed != "mail.test:587" {
		t.Errorf("test message: %d %s, dialed %q", rec.Code, rec.Body, dialed)
	}
}

func TestPeopleTurnOffEmailSentToTheirRoles(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	me := func() bool {
		var resp struct {
			User struct {
				EmailNotifications bool `json:"email_notifications"`
			} `json:"user"`
		}
		_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", "").Body.Bytes(), &resp)
		return resp.User.EmailNotifications
	}
	if !me() {
		t.Fatal("email is off before anyone turned it off")
	}
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/email", `{"notifications": false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"email_notifications":false`) {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	if me() {
		t.Error("still on after turning it off")
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/email", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("without a value: %d", rec.Code)
	}
}
