package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Tests for the API additions the web interface needs (README Phase 4.2).

func TestEffectiveFieldsForANewAsset(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	serial, _ := srv.store.CreateField("serial", "Serial", fields.TypeText, nil)
	tools, _ := srv.store.CreateCategory("Tools", nil, "#112233", false, false)
	if err := srv.store.AssignCategoryField(tools.ID, serial.ID, true); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories/effective-fields", `{"category_ids":["`+tools.ID+`"]}`)
	var resp struct {
		Fields []models.EffectiveField `json:"fields"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || len(resp.Fields) != 1 || resp.Fields[0].Field.Key != "serial" || !resp.Fields[0].IsRequired {
		t.Errorf("effective fields: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories/effective-fields", `{"category_ids":[]}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"fields":[]`) {
		t.Errorf("no categories: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/categories/effective-fields", `{"category_ids":["missing"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown category: %d %s", rec.Code, rec.Body.String())
	}

	nobody, _ := srv.store.CreateUser("nobody@test.org", "Pass1234!", true)
	if rec := doJSON(t, srv, sessionCookie(t, srv, nobody.ID), http.MethodPost, "/api/v1/categories/effective-fields", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("without assets.read: %d", rec.Code)
	}
}

func TestRolesAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/roles", "")
	var roles struct {
		Roles []models.RoleWithPermissions `json:"roles"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &roles)
	var adminRole string
	for _, r := range roles.Roles {
		if r.Name == "admin" {
			adminRole = r.ID
		}
	}
	if rec.Code != http.StatusOK || adminRole == "" {
		t.Fatalf("roles: %d %s", rec.Code, rec.Body.String())
	}

	user, _ := srv.store.CreateUser("helper@test.org", "Pass1234!", true)
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/users/"+user.ID+"/roles", `{"role_ids":["`+adminRole+`"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), adminRole) {
		t.Fatalf("granting admin: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/users", "")
	if !strings.Contains(rec.Body.String(), `"role_ids":["`+adminRole+`"]`) {
		t.Errorf("user list lacks role_ids: %s", rec.Body.String())
	}

	for _, c := range []struct {
		why, user, body string
		status          int
	}{
		{"missing role_ids", user.ID, `{}`, http.StatusBadRequest},
		{"unknown role", user.ID, `{"role_ids":["nope"]}`, http.StatusBadRequest},
		{"unknown user", "nope", `{"role_ids":[]}`, http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/users/"+c.user+"/roles", c.body); rec.Code != c.status {
			t.Errorf("%s: %d %s", c.why, rec.Code, rec.Body.String())
		}
	}

	// The admin may drop their role while the helper holds it; then the
	// helper, now the last admin, may not drop theirs.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/users/"+admin.ID+"/roles", `{"role_ids":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("dropping admin with another left: %d %s", rec.Code, rec.Body.String())
	}
	helper := sessionCookie(t, srv, user.ID)
	if rec := doJSON(t, srv, helper, http.MethodPut, "/api/v1/users/"+user.ID+"/roles", `{"role_ids":[]}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "anti-lockout") {
		t.Errorf("last admin dropping their role: %d %s", rec.Code, rec.Body.String())
	}

	// The former admin can no longer manage users.
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/roles", ""); rec.Code != http.StatusForbidden {
		t.Errorf("roles without users.manage: %d", rec.Code)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	srv, admin, adminPassword := setupTestServer(t)
	current := sessionCookie(t, srv, admin.ID)
	other := sessionCookie(t, srv, admin.ID)

	for _, c := range []struct {
		why, body string
	}{
		{"wrong current password", `{"current_password":"nope","new_password":"NewSecret123!"}`},
		{"too short", `{"current_password":"` + adminPassword + `","new_password":"short"}`},
	} {
		if rec := doJSON(t, srv, current, http.MethodPut, "/api/v1/auth/password", c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", c.why, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, srv, current, http.MethodPut, "/api/v1/auth/password", `{"current_password":"`+adminPassword+`","new_password":"NewSecret123!"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("changing password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, current, http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusOK {
		t.Errorf("the session that changed the password ended: %d", rec.Code)
	}
	if rec := doJSON(t, srv, other, http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("another session survived: %d", rec.Code)
	}
	login := `{"email":"` + admin.Email + `","password":"NewSecret123!"}`
	if rec := doJSON(t, srv, nil, http.MethodPost, "/api/v1/auth/login", login); rec.Code != http.StatusOK {
		t.Errorf("signing in with the new password: %d", rec.Code)
	}
	if rec := doJSON(t, srv, nil, http.MethodPut, "/api/v1/auth/password", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
}

func TestCreateUserEnforcesPasswordLength(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodPost, "/api/v1/users", `{"email":"x@test.org","name":"X","password":"short"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "8-72") {
		t.Errorf("short password: %d %s", rec.Code, rec.Body.String())
	}
}

func TestListGenerators(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/generators", "")
	var resp struct {
		Generators []struct {
			Type   string   `json:"type"`
			Levels []string `json:"error_correction_levels"`
		} `json:"generators"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || len(resp.Generators) != 3 || resp.Generators[0].Type != "qr" ||
		strings.Join(resp.Generators[0].Levels, "") != "MLQH" || resp.Generators[2].Levels == nil {
		t.Errorf("generators: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPrinterFitCheck(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	id := createPrinter(t, srv, cookie, 9100, "dk-11201") // 29 × 90 mm die-cut, 300 DPI

	fit := func(body string) (int, string) {
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+id+"/fit", body)
		return rec.Code, rec.Body.String()
	}
	if code, body := fit(`{"width_mm":90,"height_mm":29,"dpi":300}`); code != 200 || !strings.Contains(body, `"fits":true`) {
		t.Errorf("fitting template: %d %s", code, body)
	}
	if code, body := fit(`{"width_mm":62,"height_mm":29,"dpi":300}`); code != 200 || !strings.Contains(body, `"fits":false`) || !strings.Contains(body, "29 × 90 mm") {
		t.Errorf("wrong size: %d %s", code, body)
	}
	if code, _ := fit(`{"width_mm":0,"height_mm":29,"dpi":300}`); code != http.StatusBadRequest {
		t.Errorf("zero width: %d", code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/nope/fit", `{"width_mm":1,"height_mm":1,"dpi":300}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown printer: %d", rec.Code)
	}

	// Template designers can read printers and check fit, but not print.
	dc := userWithPermissions(t, srv, "designer@test.org", "templates.manage")
	if rec := doJSON(t, srv, dc, http.MethodPost, "/api/v1/printers/"+id+"/fit", `{"width_mm":90,"height_mm":29,"dpi":300}`); rec.Code != http.StatusOK {
		t.Errorf("designer fit check: %d", rec.Code)
	}
	if rec := doJSON(t, srv, dc, http.MethodPost, "/api/v1/printers/"+id+"/print", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("designer printing: %d", rec.Code)
	}
}

func TestDriversListModelDPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/printers/drivers", "")
	for _, want := range []string{`"QL-810W":300`, `"LabelWriter 450":300`, `"Rollo X1038":203`, `"Generic TSPL 300 DPI":300`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("drivers list lacks %s", want)
		}
	}
}
