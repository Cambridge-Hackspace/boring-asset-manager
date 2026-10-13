package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateCategoryWithMissingParentIsBadRequest(t *testing.T) {
	srv, admin, _ := setupTestServer(t)

	sess, err := srv.store.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	adminCookie := &http.Cookie{Name: SessionCookieName, Value: sess.ID}

	cat, err := srv.store.CreateCategory("Existing", nil, "", false, false)
	if err != nil {
		t.Fatalf("creating category: %v", err)
	}

	missing := "00000000-0000-0000-0000-000000000000"
	body, _ := json.Marshal(UpdateCategoryRequest{Name: "Existing", ParentID: &missing})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/categories/"+cat.ID, bytes.NewReader(body))
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// The category being updated exists; only the requested parent is missing,
	// so this is a client error in the payload rather than a 404 on the resource.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for missing parent, got %d: %s", rec.Code, rec.Body.String())
	}
}
