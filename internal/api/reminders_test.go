package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestLoanReminderSettingsThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	// Off by default, and refused on while email cannot carry them.
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/settings/loan-reminders", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"on":false,"days_before":1,"overdue_times":4,"can_email":false`) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/loan-reminders", `{"on":true,"days_before":1,"overdue_times":4}`); rec.Code != http.StatusConflict {
		t.Errorf("on without email: %d %s", rec.Code, rec.Body)
	}
	// Off, they can still be timed.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/loan-reminders", `{"on":false,"days_before":2,"overdue_times":3}`); rec.Code != http.StatusOK {
		t.Errorf("timed while off: %d %s", rec.Code, rec.Body)
	}

	catchMail(srv)
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/loan-reminders", `{"on":true,"days_before":2,"overdue_times":3}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"on":true,"days_before":2,"overdue_times":3,"can_email":true`) {
		t.Errorf("on: %d %s", rec.Code, rec.Body)
	}
	if set, _ := srv.store.LoanReminders(); !set.On || set.DaysBefore != 2 || set.OverdueTimes != 3 {
		t.Errorf("saved %+v", set)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/loan-reminders", `{"on":true,"days_before":40,"overdue_times":3}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "days before must be 0 to 30") {
		t.Errorf("too many days: %d %s", rec.Code, rec.Body)
	}

	// Only those who manage settings see or set them.
	member, _ := srv.store.CreateUser("member@test.org", "MemberSecret123!", true)
	if rec := doJSON(t, srv, sessionCookie(t, srv, member.ID), http.MethodGet, "/api/v1/settings/loan-reminders", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d", rec.Code)
	}
}

func TestABorrowerTurnsTheirLoanRemindersOff(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	bo, _ := srv.store.CreateUser("bo@test.org", "BoSecret12345!", true)
	cookie := sessionCookie(t, srv, bo.ID)
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", ""); !strings.Contains(rec.Body.String(), `"loan_reminders":true`) {
		t.Fatalf("on by default: %s", rec.Body)
	}
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/loan-reminders", `{"reminders":false}`)
	var out struct {
		User struct {
			LoanReminders bool `json:"loan_reminders"`
		} `json:"user"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.User.LoanReminders {
		t.Fatalf("off: %d %s", rec.Code, rec.Body)
	}
	if u, _ := srv.store.GetUserByID(bo.ID); u.LoanReminders {
		t.Error("not saved")
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/loan-reminders", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("without reminders: %d", rec.Code)
	}
}

func TestLoanRemindersOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	catchMail(srv)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Settings", rbac.PermSettingsManage))
	res := callTool(t, cs, "set_loan_reminder_settings", map[string]any{"on": true, "days_before": 3, "overdue_times": 2})
	if res.IsError || !strings.Contains(toolText(res), "3 days before") {
		t.Fatalf("set: %s", toolText(res))
	}
	if res := callTool(t, cs, "get_loan_reminder_settings", map[string]any{}); res.IsError || !strings.Contains(toolText(res), "Reminders on: true") {
		t.Errorf("get: %s", toolText(res))
	}
}
