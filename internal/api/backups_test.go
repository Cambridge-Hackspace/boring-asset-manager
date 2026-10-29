package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// members lists an archive's members and their bytes.
func archiveMembers(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name], _ = io.ReadAll(tr)
	}
}

// uploadArchive uploads b as the person, in one part, and returns its ID.
func uploadArchive(t *testing.T, srv *Server, cookie *http.Cookie, b []byte) string {
	t.Helper()
	rec, u := beginUpload(t, srv, cookie, int64(len(b)), sha256Of(b))
	if rec.Code != http.StatusCreated {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	for first := int64(0); first < int64(len(b)); first += u.PartSize {
		end := min(first+u.PartSize, int64(len(b)))
		if rec := putPart(t, srv, cookie, u.ID, b[first:end], first, end-first, int64(len(b))); rec.Code != http.StatusOK {
			t.Fatalf("part: %d %s", rec.Code, rec.Body)
		}
	}
	return u.ID
}

// awaitJob asks after a job until it is done.
func awaitJob(t *testing.T, srv *Server, cookie *http.Cookie, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("starting a job: %d %s", rec.Code, rec.Body)
	}
	var job struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		r := do(t, srv, cookie, http.MethodGet, "/api/v1/backup/jobs/"+job.ID, "", nil)
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		if out["done"] == true {
			return out
		}
	}
	t.Fatal("the job did not finish")
	return nil
}

func postJSON(t *testing.T, srv *Server, cookie *http.Cookie, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	return do(t, srv, cookie, http.MethodPost, path, "application/json", bytes.NewReader(b))
}

// TestABackupFromTheWebHoldsNoAuthentication: downloaded by the people who
// manage users and settings, it holds everything but authentication.
func TestABackupFromTheWebHoldsNoAuthentication(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/backup", "", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="bam-`) {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	var hash string
	if err := srv.store.QueryRow("SELECT password_hash FROM users WHERE id = ?", admin.ID).Scan(&hash); err != nil || hash == "" {
		t.Fatalf("the admin's hash: %q, %v", hash, err)
	}
	members := archiveMembers(t, rec.Body.Bytes())
	var hdr archive.Header
	if err := json.Unmarshal(members[archive.HeaderPath], &hdr); err != nil || hdr.Secrets {
		t.Fatalf("header: %+v, %v", hdr, err)
	}
	for path, data := range members {
		if strings.HasPrefix(path, archive.AuthDir) {
			t.Errorf("%s is in a backup from the web", path)
		}
		if bytes.Contains(data, []byte(hash)) {
			t.Errorf("%s holds a password hash", path)
		}
	}
	if _, ok := members[archive.DataDir+"users/0001.jsonl"]; !ok {
		t.Error("the backup holds no users")
	}

	// Both permissions are needed.
	for _, perm := range []string{rbac.PermUsersManage, rbac.PermSettingsManage} {
		u := userWith(t, srv, perm)
		if rec := do(t, srv, sessionCookie(t, srv, u.ID), http.MethodGet, "/api/v1/backup", "", nil); rec.Code != http.StatusForbidden {
			t.Errorf("a backup with %s alone: %d", perm, rec.Code)
		}
	}
}

// userWith is someone holding one permission.
func userWith(t *testing.T, srv *Server, perm string) *models.User {
	t.Helper()
	role, err := srv.store.CreateRole(store.RoleInput{Name: "only-" + perm, Permissions: []string{perm}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := srv.store.CreateUser(perm+"@test.org", "OnlySecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, role.ID); err != nil {
		t.Fatal(err)
	}
	return u
}

// TestReplacingFromTheWeb: a backup downloaded, the instance changed,
// the backup uploaded, checked and its assets replaced: they are as they
// were, the rest as changed, and the person still signed in; a backup of
// what was replaced was taken first, and puts it back.
func TestReplacingFromTheWeb(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	cat, err := srv.store.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateAsset("Drill", []string{cat.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	b := do(t, srv, cookie, http.MethodGet, "/api/v1/backup", "", nil).Body.Bytes()

	if _, err := srv.store.CreateAsset("Saw", []string{cat.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.CreateCategory("Added since", nil, "#000000", false, false); err != nil {
		t.Fatal(err)
	}
	id := uploadArchive(t, srv, cookie, b)

	checked := awaitJob(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/backup/check", map[string]any{"upload_id": id}))
	if checked["error"] != nil {
		t.Fatalf("check: %v", checked["error"])
	}
	res := checked["result"].(map[string]any)
	if res["schema_current"] != true || res["instance_host"] != "localhost:8080" || res["header"].(map[string]any)["secrets"] != false {
		t.Errorf("check: %v", res)
	}

	refused := func(name string, body map[string]any, code int) {
		t.Helper()
		if rec := postJSON(t, srv, cookie, "/api/v1/backup/replace", body); rec.Code != code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	refused("unconfirmed", map[string]any{"upload_id": id, "kinds": []string{"assets"}, "confirm": "yes"}, http.StatusBadRequest)
	refused("no kinds", map[string]any{"upload_id": id, "kinds": []string{}, "confirm": "localhost:8080"}, http.StatusBadRequest)
	refused("authentication", map[string]any{"upload_id": id, "kinds": []string{"auth"}, "confirm": "localhost:8080"}, http.StatusBadRequest)
	refused("someone else's", map[string]any{"upload_id": "4b9c4a1e-5b2a-4a52-9a0e-6d3c2f1e0a9b", "kinds": []string{"assets"}, "confirm": "localhost:8080"}, http.StatusNotFound)

	done := awaitJob(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/backup/replace", map[string]any{"upload_id": id, "kinds": []string{"assets"}, "confirm": "localhost:8080"}))
	if done["error"] != nil {
		t.Fatalf("replace: %v", done["error"])
	}
	var n int
	srv.store.QueryRow("SELECT COUNT(*) FROM assets").Scan(&n)
	if n != 1 {
		t.Errorf("%d assets after replacing, not the backup's 1", n)
	}
	srv.store.QueryRow("SELECT COUNT(*) FROM categories WHERE name = 'Added since'").Scan(&n)
	if n != 1 {
		t.Error("a category made since was replaced, though categories were not")
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", "", nil); rec.Code != http.StatusOK {
		t.Errorf("signed out by a replace: %d", rec.Code)
	}

	// The backup taken first puts the saw back.
	before := done["result"].(map[string]any)["before_upload_id"].(string)
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/uploads/"+before+"/content", "", nil); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("downloading the backup taken first: %d", rec.Code)
	}
	undo := awaitJob(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/backup/replace", map[string]any{"upload_id": before, "kinds": []string{"assets"}, "confirm": "localhost:8080"}))
	if undo["error"] != nil {
		t.Fatalf("undo: %v", undo["error"])
	}
	srv.store.QueryRow("SELECT COUNT(*) FROM assets").Scan(&n)
	if n != 2 {
		t.Errorf("%d assets after putting back what was replaced", n)
	}
}

// TestReplacingAnArchiveWithAuthentication: one from bam backup, holding
// authentication, is replaced without it, saying so.
func TestReplacingAnArchiveWithAuthentication(t *testing.T) {
	srv, admin, password := setupTestServer(t)
	var parts bytes.Buffer
	if _, err := archive.Write(t.Context(), srv.store, srv.blobs, func(int) (io.WriteCloser, error) { return nopWriteCloser{&parts}, nil }, archive.Options{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SetPassword(admin.ID, "AnotherSecret456!", ""); err != nil {
		t.Fatal(err)
	}
	// Signed in after, as a new password signs people out.
	cookie := sessionCookie(t, srv, admin.ID)
	id := uploadArchive(t, srv, cookie, parts.Bytes())
	checked := awaitJob(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/backup/check", map[string]any{"upload_id": id}))
	if w := checked["result"].(map[string]any)["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "never imported through the web") {
		t.Errorf("warnings: %v", w)
	}
	done := awaitJob(t, srv, cookie, postJSON(t, srv, cookie, "/api/v1/backup/replace", map[string]any{"upload_id": id, "kinds": []string{"people"}, "confirm": "localhost:8080"}))
	if done["error"] != nil {
		t.Fatalf("replace: %v", done["error"])
	}
	u, _ := srv.store.GetUserByEmail(admin.Email)
	if auth.CheckPassword(password, u.PasswordHash) || !auth.CheckPassword("AnotherSecret456!", u.PasswordHash) {
		t.Error("the password was replaced through the web")
	}
}
