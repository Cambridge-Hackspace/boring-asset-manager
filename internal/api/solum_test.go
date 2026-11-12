package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestASoluMLabelIsPushedThroughAIMS(t *testing.T) {
	srv, admin, _ := factorServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	var mu sync.Mutex
	var pushes []map[string]any
	var passwords []string
	aims := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/common/api/v2/token":
			passwords = append(passwords, body["password"].(string))
			_, _ = io.WriteString(w, `{"responseCode":"200","responseMessage":{"access_token":"a1","refresh_token":"r1"}}`)
		case "/common/api/v2/common/labels/type/info":
			_, _ = io.WriteString(w, `{"name":"Newton 4.2","displayWidth":400,"displayHeight":300,"color":"BWR"}`)
		case "/common/api/v2/common/labels/image":
			pushes = append(pushes, body)
			_, _ = io.WriteString(w, `{"responseCode":"200","responseMessage":"SUCCESS","customBatchId":"b1"}`)
		default:
			_, _ = io.WriteString(w, `{"updateHistoryList":[{"result":"SUCCESS"}]}`)
		}
	}))
	defer aims.Close()

	// The account is the instance's, its password sealed and never shown.
	put := func(body string) DisplaySettings {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", body)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") {
			t.Fatalf("settings: %d %s", rec.Code, rec.Body)
		}
		var s DisplaySettings
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		return s
	}
	if s := put(`{"refresh_window_seconds":30,"pull_refresh_seconds":900}`); s.SolumBaseURL != printers.DefaultSolumBase || s.SolumPasswordSet {
		t.Fatalf("defaults %+v", s)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":30,"pull_refresh_seconds":900,"solum_base_url":"ftp://x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an ftp URL: %d", rec.Code)
	}
	account := `"refresh_window_seconds":30,"pull_refresh_seconds":900,"solum_base_url":"` + aims.URL + `","solum_company":"TST","solum_store":"S001","solum_username":"ops@example.org"`
	if s := put(`{` + account + `,"solum_password":"s3cret"}`); !s.SolumPasswordSet || s.SolumCompany != "TST" || s.SolumStore != "S001" || s.SolumUsername != "ops@example.org" {
		t.Fatalf("set %+v", s)
	}
	if sealed, _ := srv.store.Setting(store.SolumPasswordSetting, ""); sealed == "" || strings.Contains(sealed, "s3cret") {
		t.Fatalf("kept as %q", sealed)
	}
	// Leaving the password out keeps it.
	if s := put(`{` + account + `}`); !s.SolumPasswordSet {
		t.Fatalf("kept %+v", s)
	}

	display := createDisplay(t, srv, cookie, "Bin A1", printers.DriverSolum, "4.2in-400x300-bwr", `{"type":"solum","address":"04507b0ac391"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Bin", 400, 300, 119))
	a, _ := srv.store.CreateAsset("M3 screws", nil, nil, nil)
	printTo(t, srv, cookie, display, tpl, a.Asset.ID)
	mu.Lock()
	if len(pushes) != 1 || pushes[0]["labelCode"] != "04507B0AC391" || len(passwords) != 1 || passwords[0] != "s3cret" {
		mu.Unlock()
		t.Fatalf("pushes %v, passwords %v", pushes, passwords)
	}
	mu.Unlock()

	// Without its password, printing says what to do.
	put(`{` + account + `,"solum_password":""}`)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+display+"/print", `{"template_id":"`+tpl+`","asset_ids":["`+a.Asset.ID+`"]}`)
	if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "not set up") {
		t.Fatalf("no password: %d %s", rec.Code, rec.Body)
	}
}

func TestTheSoluMPasswordNeedsASecretKey(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":30,"pull_refresh_seconds":900,"solum_password":"s3cret"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
