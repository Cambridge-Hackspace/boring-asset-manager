package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
)

// postLinkImport sends a CSV with the suggested mapping, and remove_links
// when it is not empty.
func postLinkImport(t *testing.T, srv *Server, cookie *http.Cookie, path, file, removeLinks string) *bytes.Buffer {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "links.csv")
	_, _ = fw.Write([]byte(file))
	mapping, _ := json.Marshal(assetcsv.Suggest(strings.Split(strings.SplitN(file, "\n", 2)[0], ","), nil))
	_ = mw.WriteField("mapping", string(mapping))
	if removeLinks != "" {
		_ = mw.WriteField("remove_links", removeLinks)
	}
	_ = mw.Close()
	rec := do(t, srv, cookie, http.MethodPost, path, mw.FormDataContentType(), &body)
	if rec.Code != http.StatusOK {
		return bytes.NewBufferString(rec.Result().Status + " " + rec.Body.String())
	}
	return rec.Body
}

func TestLinksThroughCSV(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	// A bin and what is in it, made in one file.
	got := postLinkImport(t, srv, cookie, "/api/v1/assets/import", "name,links\nBin A3,contains:M3 screws\nM3 screws,\n", "")
	var result map[string]any
	if err := json.Unmarshal(got.Bytes(), &result); err != nil || result["created"] != float64(2) || result["links_added"] != float64(1) {
		t.Fatalf("import: %s", got)
	}

	// The export writes the link from both ends.
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/export.csv", "")
	rows := readCSV(t, rec.Body.String())
	col := -1
	for i, h := range rows[0] {
		if h == "links" {
			col = i
		}
	}
	cells := map[string]string{}
	for _, r := range rows[1:] {
		cells[r[1]] = r[col]
	}
	if col < 0 || cells["Bin A3"] != "contains:M3 screws" || cells["M3 screws"] != "contained_in:Bin A3" {
		t.Fatalf("export: %v", rows)
	}

	// Previewing the screws' row alone, removing what no row states.
	var screws string
	for _, r := range rows[1:] {
		if r[1] == "M3 screws" {
			screws = r[0]
		}
	}
	// Without the option nothing changes, and the list is empty, not null:
	// the page reads its length.
	got = postLinkImport(t, srv, cookie, "/api/v1/assets/import/preview", "id,links\n"+screws+",\n", "")
	if !strings.Contains(got.String(), `"link_changes":[]`) {
		t.Errorf("preview without the option: %s", got)
	}
	// A problem leaves nothing listed either.
	got = postLinkImport(t, srv, cookie, "/api/v1/assets/import/preview", "id,links\n"+screws+",contained_in:Nowhere\n", "")
	if !strings.Contains(got.String(), `"link_changes":[]`) || !strings.Contains(got.String(), `"problem_count":1`) {
		t.Errorf("preview with a problem: %s", got)
	}

	got = postLinkImport(t, srv, cookie, "/api/v1/assets/import/preview", "id,links\n"+screws+",\n", "true")
	var plan assetcsv.Plan
	if err := json.Unmarshal(got.Bytes(), &plan); err != nil || !plan.RemoveLinks || plan.LinksRemoved != 1 || plan.LinkChanges[0].Change != "remove" {
		t.Errorf("preview: %s", got)
	}
	if got := postLinkImport(t, srv, cookie, "/api/v1/assets/import/preview", "id,links\n"+screws+",\n", "maybe"); !strings.HasPrefix(got.String(), "400") {
		t.Errorf("remove_links=maybe: %s", got)
	}
}
