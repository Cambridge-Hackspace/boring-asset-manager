package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

type commentsBody struct {
	Comments []models.Comment `json:"comments"`
	Comment  models.Comment   `json:"comment"`
}

func decodeComments(t *testing.T, rec *httptest.ResponseRecorder) commentsBody {
	t.Helper()
	var out commentsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out
}

// comment writes a comment through the API, failing unless it is made.
func comment(t *testing.T, srv *Server, who *http.Cookie, path, body string) models.Comment {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"body": body})
	rec := doJSON(t, srv, who, http.MethodPost, path, string(b))
	if rec.Code != http.StatusCreated {
		t.Fatalf("commenting on %s: %d %s", path, rec.Code, rec.Body)
	}
	return decodeComments(t, rec).Comment
}

// TestCommentsThroughTheAPI: comments are written, rendered safely, listed
// with their categories', edited by their authors only, deleted by them or
// a moderator, and found by search, by those who may.
func TestCommentsThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools"})
	drills, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Drills", ParentID: &tools.ID})
	a, err := srv.store.CreateAsset("Drill", []string{drills.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assetPath := "/api/v1/assets/" + a.Asset.ID + "/comments"
	writer := holderOf(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsTrack, rbac.PermCommentsWrite)
	reader := holderOf(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	mod := holderOf(t, srv, "mod@hackspace.org", rbac.PermAssetsRead, rbac.PermCommentsModerate)

	onTools := comment(t, srv, cookie, "/api/v1/categories/"+tools.ID+"/comments", "Wear goggles")
	mine := comment(t, srv, writer, assetPath, "The left motor makes a noise above 50%\n\n<script>alert(1)</script>\n[x](javascript:alert(1))\n- [x] belt ordered")
	if !mine.Mine || !mine.CanEdit || !mine.CanDelete || mine.AuthorName != "Someone" {
		t.Errorf("one's own: %+v", mine)
	}
	if strings.Contains(mine.HTML, "<script") || strings.Contains(mine.HTML, "javascript:") || !strings.Contains(mine.HTML, `aria-label="Done"`) {
		t.Errorf("rendered: %s", mine.HTML)
	}

	// The asset lists its own, then its categories' and their ancestors'.
	list := decodeComments(t, doJSON(t, srv, reader, http.MethodGet, assetPath, "")).Comments
	if len(list) != 2 || list[0].ID != mine.ID || list[1].ID != onTools.ID || list[1].Category == nil || list[1].Category.Name != "Tools" {
		t.Fatalf("the asset's comments: %+v", list)
	}
	// Without assets.track, who wrote them is not shown, and the reader
	// may do nothing with them.
	if list[0].AuthorID != "" || list[0].AuthorName != "" || list[0].Mine || list[0].CanEdit || list[0].CanDelete {
		t.Errorf("as the reader sees it: %+v", list[0])
	}
	own := decodeComments(t, doJSON(t, srv, reader, http.MethodGet, assetPath+"?own=1", "")).Comments
	if len(own) != 1 {
		t.Errorf("its own only: %+v", own)
	}

	// Writing needs comments.write; editing is the author's alone.
	if rec := doJSON(t, srv, reader, http.MethodPost, assetPath, `{"body":"hi"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader comments: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/comments/"+mine.ID, `{"body":"not mine"}`); rec.Code != http.StatusForbidden {
		t.Errorf("an admin edits another's comment: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, writer, http.MethodPut, "/api/v1/comments/"+mine.ID, `{"body":"**Fixed**"}`)
	if edited := decodeComments(t, rec).Comment; rec.Code != http.StatusOK || edited.EditedAt == nil || !strings.Contains(edited.HTML, "<strong>Fixed</strong>") {
		t.Errorf("edited: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, writer, http.MethodPut, "/api/v1/comments/"+mine.ID, `{"body":" "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("emptied: %d", rec.Code)
	}

	// A moderator deletes anyone's; it leaves who deleted it, and its
	// text in history.
	if rec := doJSON(t, srv, writer, http.MethodDelete, "/api/v1/comments/"+onTools.ID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("deleting another's without moderating: %d", rec.Code)
	}
	rec = doJSON(t, srv, mod, http.MethodDelete, "/api/v1/comments/"+mine.ID, "")
	if gone := decodeComments(t, rec).Comment; rec.Code != http.StatusOK || gone.DeletedAt == nil || gone.Body != "" || gone.HTML != "" {
		t.Errorf("deleted: %d %s", rec.Code, rec.Body)
	}
	seen := decodeComments(t, doJSON(t, srv, writer, http.MethodGet, "/api/v1/comments/"+mine.ID, "")).Comment
	if seen.DeletedByName != "Someone" || seen.CanEdit || seen.CanDelete {
		t.Errorf("deleted, as its author sees it: %+v", seen)
	}
	rec = doJSON(t, srv, writer, http.MethodGet, "/api/v1/comments/"+mine.ID+"/history", "")
	var history struct{ Events []models.Event }
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if len(history.Events) != 3 || history.Events[1].Payload.Changes[0].To != "**Fixed**" || history.Events[2].Payload.Changes[0].From != "**Fixed**" {
		t.Errorf("history: %s", rec.Body)
	}

	// Search finds comments, and assets by them.
	comment(t, srv, writer, assetPath, "ordered a replacement BELT")
	found := decodeComments(t, doJSON(t, srv, reader, http.MethodGet, "/api/v1/comments?q=belt", "")).Comments
	if len(found) != 1 || found[0].Place == nil || found[0].Place.Name != "Drill" {
		t.Errorf("search: %+v", found)
	}
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets?search=replacement", "")
	if !strings.Contains(rec.Body.String(), a.Asset.ID) {
		t.Errorf("assets by their comments: %s", rec.Body)
	}

	// Files attached while commenting land on the asset.
	rec = attach(t, srv, writer, assetPath+"/attachments", "belt.png", pngBytes(t, 8, 8), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attaching: %d %s", rec.Code, rec.Body)
	}
	var attachedOut struct {
		Attachment models.Attachment
		Markdown   string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &attachedOut)
	if !strings.HasPrefix(attachedOut.Markdown, "![belt.png](/api/v1/assets/"+a.Asset.ID+"/attachments/") {
		t.Errorf("its Markdown: %q", attachedOut.Markdown)
	}
	as, _ := srv.store.ListAttachments(models.OwnerAsset, a.Asset.ID)
	if len(as) != 1 || as[0].ID != attachedOut.Attachment.ID {
		t.Errorf("the asset's attachments: %+v", as)
	}
	withImage := comment(t, srv, writer, assetPath, "Here: "+attachedOut.Markdown)
	if !strings.Contains(withImage.HTML, `<img src="/api/v1/assets/`) {
		t.Errorf("an attached image: %s", withImage.HTML)
	}
	if rec := attach(t, srv, reader, assetPath+"/attachments", "belt.png", pngBytes(t, 8, 8), nil); rec.Code != http.StatusForbidden {
		t.Errorf("a reader attaches: %d", rec.Code)
	}

	// The preview renders as comments are.
	rec = doJSON(t, srv, reader, http.MethodPost, "/api/v1/markdown", `{"body":"# Hi <img src=x onerror=alert(1)>"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "onerror") || !strings.Contains(rec.Body.String(), "\\u003ch1\\u003e") {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}
}

// TestCommentScopes: who sees and writes comments follows the scopes of
// what they are on.
func TestCommentScopes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	lab, err := srv.store.CreateScope("lab")
	if err != nil {
		t.Fatal(err)
	}
	lasers, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Lasers"})
	if _, err := srv.store.SetCategoryScopes(lasers.ID, []string{lab.ID}); err != nil {
		t.Fatal(err)
	}
	pens, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Pens"})
	laser, _ := srv.store.CreateAsset("Laser", []string{lasers.ID}, nil, nil)
	pen, _ := srv.store.CreateAsset("Pen", []string{pens.ID}, nil, nil)
	laserPath := "/api/v1/assets/" + laser.Asset.ID + "/comments"
	penPath := "/api/v1/assets/" + pen.Asset.ID + "/comments"
	secret := comment(t, srv, cookie, laserPath, "the lab door code is 1234")
	comment(t, srv, cookie, penPath, "out of ink")

	// Reading pens only, the laser and its comments are not there.
	outsider := holderOf(t, srv, "out@hackspace.org", rbac.DefaultScope+":"+rbac.PermAssetsRead, rbac.DefaultScope+":"+rbac.PermCommentsWrite)
	if rec := doJSON(t, srv, outsider, http.MethodGet, laserPath, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another scope's comments: %d", rec.Code)
	}
	if rec := doJSON(t, srv, outsider, http.MethodGet, "/api/v1/comments/"+secret.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another scope's comment: %d", rec.Code)
	}
	if found := decodeComments(t, doJSON(t, srv, outsider, http.MethodGet, "/api/v1/comments?q=o", "")).Comments; len(found) != 1 || found[0].Body != "out of ink" {
		t.Errorf("search across scopes: %+v", found)
	}
	if rec := doJSON(t, srv, outsider, http.MethodPost, laserPath, `{"body":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("commenting in another scope: %d", rec.Code)
	}
	comment(t, srv, outsider, penPath, "refilled")

	// Reading the lab, but writing only in default, it reads and may not
	// write.
	labReader := holderOf(t, srv, "lab@hackspace.org", "lab:"+rbac.PermAssetsRead, rbac.DefaultScope+":"+rbac.PermCommentsWrite)
	if list := decodeComments(t, doJSON(t, srv, labReader, http.MethodGet, laserPath, "")).Comments; len(list) != 1 {
		t.Errorf("the lab's comments: %+v", list)
	}
	if rec := doJSON(t, srv, labReader, http.MethodPost, laserPath, `{"body":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("commenting where it may only read: %d", rec.Code)
	}
	// A category's comments are written in its scopes.
	if rec := doJSON(t, srv, labReader, http.MethodPost, "/api/v1/categories/"+lasers.ID+"/comments", `{"body":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("commenting on a category in another scope: %d", rec.Code)
	}
	labMod := holderOf(t, srv, "labmod@hackspace.org", "lab:"+rbac.PermAssetsRead, rbac.DefaultScope+":"+rbac.PermCommentsModerate)
	if rec := doJSON(t, srv, labMod, http.MethodDelete, "/api/v1/comments/"+secret.ID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("moderating in another scope: %d", rec.Code)
	}
}

// TestLoanAndFilingComments: loans and filings take comments, read as
// their asset is.
func TestLoanAndFilingComments(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true})
	a, _ := srv.store.CreateAsset("Saw", []string{tools.ID}, nil, nil)
	lent, err := srv.store.As(admin.ID).CheckOut(a.Asset.ID, admin.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	loanPath := "/api/v1/loans/" + lent.Loan.ID + "/comments"
	c := comment(t, srv, cookie, loanPath, "Lent with the blade guard")
	if c.On != models.CommentOnLoan || c.AssetID != a.Asset.ID {
		t.Errorf("on the loan: %+v", c)
	}
	if list := decodeComments(t, doJSON(t, srv, cookie, http.MethodGet, loanPath, "")).Comments; len(list) != 1 {
		t.Errorf("the loan's comments: %+v", list)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/loans/nope/comments", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no loan: %d", rec.Code)
	}

	f, err := srv.store.CreateFlow(store.FlowInput{Slug: "leave", Name: "Leave it", Enabled: true, Definition: flows.Definition{CategoryIDs: []string{tools.ID}}}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	flowID := f.ID
	filingPath := "/api/v1/assets/" + a.Asset.ID + "/filings/" + flowID + "/comments"
	if rec := doJSON(t, srv, cookie, http.MethodPost, filingPath, `{"body":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("before it was filed: %d %s", rec.Code, rec.Body)
	}
	if err := srv.store.RecordFiling(flowID, a.Asset.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	comment(t, srv, cookie, filingPath, "On the shelf by the door")
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+a.Asset.ID+"/filings", "")
	var filings struct{ Filings []models.Filing }
	_ = json.Unmarshal(rec.Body.Bytes(), &filings)
	if len(filings.Filings) != 1 || filings.Filings[0].Comments != 1 {
		t.Errorf("filings: %s", rec.Body)
	}
}
