package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/wordnet/wordnettest"
)

// withWordNet has srv seed from the test database, downloading it with a
// fetcher that counts what it was asked for, serving body.
func withWordNet(srv *Server, body func() []byte) *[]string {
	_, src := wordnettest.Archive()
	srv.thesaurus = src
	var asked []string
	srv.fetchThesaurus = func(_ context.Context, url string) (io.ReadCloser, error) {
		asked = append(asked, url)
		b := body()
		if b == nil {
			return nil, errors.New("connection refused")
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return &asked
}

func seed(t *testing.T, srv *Server, cookie *http.Cookie, senses int) (int, map[string]any) {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/terms/wordnet/seed", `{"senses":`+jsonInt(senses)+`}`)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func uploadWordNet(t *testing.T, srv *Server, cookie *http.Cookie, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, srv, cookie, http.MethodPut, "/api/v1/terms/wordnet/file", "application/octet-stream", bytes.NewReader(file))
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestSeedingFromWordNetDownloadsOnceAndImportsTheNounsInUse(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	archive, _ := wordnettest.Archive()
	asked := withWordNet(srv, func() []byte { return archive })
	st := srv.store
	tools, _ := st.CreateCategory("Power tools", nil, "", false, false)
	for _, name := range []string{"Cordless drills", "Head torch"} {
		if _, err := st.CreateAsset(name, []string{tools.ID}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var status WordNetStatus
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms/wordnet", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if rec.Code != http.StatusOK || status.Cached || status.SourceURL != "https://wordnet.test/dict.tar.gz" || !strings.Contains(status.Notice, "Princeton University") {
		t.Fatalf("before: %d %+v", rec.Code, status)
	}

	code, out := seed(t, srv, cookie, 0)
	if code != http.StatusCreated || out["downloaded"] != true {
		t.Fatalf("seeding: %d %v", code, out)
	}
	imp := out["import"].(map[string]any)
	if imp["name"] != "WordNet test" || imp["terms"].(float64) < 5 {
		t.Errorf("imported %v", imp)
	}
	if len(*asked) != 1 || (*asked)[0] != "https://wordnet.test/dict.tar.gz" {
		t.Errorf("downloaded from %v", *asked)
	}
	// A search for what the torch is also called finds it, and says why.
	rec = doJSON(t, srv, cookie, http.MethodGet, searchPath("flashlight"), "")
	var found struct {
		Assets  []models.AssetWithDetails
		Matched map[string]string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &found)
	if len(found.Assets) != 1 || found.Assets[0].Asset.Name != "Head torch" || !strings.Contains(found.Matched[found.Assets[0].Asset.ID], "torch") {
		t.Errorf("flashlight found %s", rec.Body)
	}
	// The cordless drill is narrower than the drill that is a tool, so a
	// search for that drill finds it.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms/expand?q="+strings.ReplaceAll("drill (tool)", " ", "%20"), "")
	if !strings.Contains(rec.Body.String(), "cordless drill") {
		t.Errorf("drill (tool) expands to %s", rec.Body)
	}

	// Kept: seeding again downloads nothing, and with nothing new, adds
	// no empty import.
	code, out = seed(t, srv, cookie, 0)
	if code != http.StatusOK || out["import"] != nil || out["downloaded"] != false {
		t.Errorf("seeding again: %d %v", code, out)
	}
	if len(*asked) != 1 {
		t.Errorf("downloaded again: %v", *asked)
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms", "")
	if n := strings.Count(rec.Body.String(), `"name":"WordNet test"`); n != 1 {
		t.Errorf("%d WordNet imports: %s", n, rec.Body)
	}
	// A name added since brings its own import.
	if _, err := st.CreateAsset("Soldering iron", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if code, out = seed(t, srv, cookie, 0); code != http.StatusCreated {
		t.Errorf("seeding after a new name: %d %v", code, out)
	}
}

func TestOnlyWordNetItselfIsKept(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	archive, _ := wordnettest.Archive()
	var serve []byte
	asked := withWordNet(srv, func() []byte { return serve })
	if _, err := srv.store.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Unreachable, or another file: refused, and nothing kept.
	if code, out := seed(t, srv, cookie, 0); code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "couldn't be downloaded") {
		t.Errorf("unreachable: %d %v", code, out)
	}
	serve = append(append([]byte{}, archive...), 'x')
	if code, out := seed(t, srv, cookie, 0); code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "SHA-256") {
		t.Errorf("another file: %d %v", code, out)
	}
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms/wordnet", "")
	if !strings.Contains(rec.Body.String(), `"cached":false`) {
		t.Errorf("kept a file that isn't WordNet: %s", rec.Body)
	}

	// Uploaded by hand: refused unless it is the same file.
	rec = uploadWordNet(t, srv, cookie, archive[:len(archive)-1])
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a file cut short: %d %s", rec.Code, rec.Body)
	}
	rec = uploadWordNet(t, srv, cookie, append(append([]byte{}, archive...), "more"...))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a longer file: %d %s", rec.Code, rec.Body)
	}
	rec = uploadWordNet(t, srv, cookie, archive)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cached":true`) {
		t.Fatalf("the file itself: %d %s", rec.Code, rec.Body)
	}
	before := len(*asked)
	if code, out := seed(t, srv, cookie, 1); code != http.StatusCreated {
		t.Errorf("seeding from the upload: %d %v", code, out)
	}
	if len(*asked) != before {
		t.Errorf("downloaded though it was uploaded")
	}
	if code, _ := seed(t, srv, cookie, maxSeedSenses+1); code != http.StatusBadRequest {
		t.Errorf("too many meanings: %d", code)
	}
}

func TestWordNetsSourceIsAnAdminsSetting(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	archive, _ := wordnettest.Archive()
	asked := withWordNet(srv, func() []byte { return archive })
	if _, err := srv.store.CreateAsset("Drill", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	keeper := userWithPermissions(t, srv, "keeper@hackspace.org", rbac.PermAssetsRead, rbac.PermTermsManage)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/terms/wordnet", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader saw it: %d", rec.Code)
	}
	if rec := doJSON(t, srv, keeper, http.MethodPut, "/api/v1/terms/wordnet", `{"source_url":"http://mirror.lan/wn.tar.gz"}`); rec.Code != http.StatusForbidden {
		t.Errorf("one who manages terms but not settings set it: %d", rec.Code)
	}
	for _, bad := range []string{"mirror.lan/wn.tar.gz", "file:///etc/passwd", "ftp://mirror.lan/wn.tar.gz"} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/wordnet", `{"source_url":"`+bad+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/wordnet", `{"source_url":" http://mirror.lan/wn.tar.gz "}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source_url":"http://mirror.lan/wn.tar.gz"`) {
		t.Fatalf("setting a mirror: %d %s", rec.Code, rec.Body)
	}
	// Those who manage terms seed from it.
	if code, out := seed(t, srv, keeper, 0); code != http.StatusCreated {
		t.Errorf("seeding: %d %v", code, out)
	}
	if len(*asked) != 1 || (*asked)[0] != "http://mirror.lan/wn.tar.gz" {
		t.Errorf("downloaded from %v", *asked)
	}
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/wordnet", `{"source_url":""}`)
	if !strings.Contains(rec.Body.String(), `"source_url":"https://wordnet.test/dict.tar.gz"`) {
		t.Errorf("cleared: %s", rec.Body)
	}
}

func TestSeedingWithNoNounsSaysSo(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	archive, _ := wordnettest.Archive()
	withWordNet(srv, func() []byte { return archive })
	if _, err := srv.store.CreateAsset("M3 x 10", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if code, out := seed(t, srv, cookie, 0); code != http.StatusUnprocessableEntity || !strings.Contains(out["error"].(string), "None of the words") {
		t.Errorf("%d %v", code, out)
	}
}
