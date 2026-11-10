package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// fakeModel stands in for the assistant's provider, Anthropic's Messages
// API: it answers each request with the next of its replies, and keeps
// what it was sent. Tests never call out.
type fakeModel struct {
	srv     *httptest.Server
	mu      sync.Mutex
	replies []string
	got     []map[string]any
}

func newFakeModel(t *testing.T) *fakeModel {
	t.Helper()
	m := &fakeModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.got = append(m.got, body)
		if r.Header.Get("x-api-key") != "sk-test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
			return
		}
		reply := `{"content":[{"type":"text","text":"(no more replies)"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		if len(m.replies) > 0 {
			reply, m.replies = m.replies[0], m.replies[1:]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

// says is a reply of text alone; uses one calling a tool.
func says(text string) string {
	raw, _ := json.Marshal(text)
	return `{"content":[{"type":"text","text":` + string(raw) + `}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":30}}`
}

func uses(id, name, input string) string {
	return `{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}],"stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":30}}`
}

func (m *fakeModel) queue(replies ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replies = append(m.replies, replies...)
}

// toolsSent are the tools the model was offered in request n.
func (m *fakeModel) toolsSent(n int) map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	tools, _ := m.got[n]["tools"].([]any)
	for _, tl := range tools {
		out[tl.(map[string]any)["name"].(string)] = true
	}
	return out
}

func (m *fakeModel) requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.got)
}

// turnOnAssistant turns the assistant on, talking to the fake model.
func turnOnAssistant(t *testing.T, srv *Server, m *fakeModel, edit func(*store.AssistantSettings)) {
	t.Helper()
	set := store.DefaultAssistantSettings()
	set.Enabled, set.Endpoint, set.APIKey, set.MaxTools = true, m.srv.URL, "sk-test-secret", 0
	if edit != nil {
		edit(&set)
	}
	if err := srv.store.SaveAssistantSettings(set, ""); err != nil {
		t.Fatal(err)
	}
}

// startConversation starts one, and returns its ID.
func startConversation(t *testing.T, srv *Server, who *http.Cookie, body string) string {
	t.Helper()
	rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("starting a conversation: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Conversation store.Conversation }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Conversation.ID
}

// streamed reads a turn's lines.
func streamed(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", rec.Code, rec.Body)
	}
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("not JSON: %s", sc.Text())
		}
		out = append(out, line)
	}
	return out
}

func send(t *testing.T, srv *Server, who *http.Cookie, conv, text string) []map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"text": text, "context": []ContextItem{{Kind: "asset", ID: "a1", Label: "Band saw"}}})
	return streamed(t, doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/messages", string(body)))
}

func confirm(t *testing.T, srv *Server, who *http.Cookie, conv string, approve bool) []map[string]any {
	t.Helper()
	return streamed(t, doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/confirm", fmt.Sprintf(`{"approve":%v}`, approve)))
}

func lineOf(lines []map[string]any, kind string) map[string]any {
	for _, l := range lines {
		if l["type"] == kind {
			return l
		}
	}
	return nil
}

func countAssets(t *testing.T, srv *Server, name string) int {
	t.Helper()
	var n int
	if err := srv.store.QueryRow("SELECT COUNT(*) FROM assets WHERE name = ? AND archived_at IS NULL", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestTheAssistantOffersOnlyThePersonsTools: the model is offered the tools
// the person may use, and no others; a role taken away mid-conversation is
// gone at the next call, and a change it allowed is not made.
func TestTheAssistantOffersOnlyThePersonsTools(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	m := newFakeModel(t)
	turnOnAssistant(t, srv, m, nil)
	cat, err := srv.store.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	user, err := srv.store.CreateUser("maker@test.org", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	use, err := srv.store.CreateRole(store.RoleInput{Name: "talker", Permissions: []string{rbac.PermAssistantUse, rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := srv.store.CreateRole(store.RoleInput{Name: "editor", Permissions: []string{rbac.PermAssetsWrite}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{use.ID, edit.ID} {
		if err := srv.store.AssignUserRole(user.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	who := sessionCookie(t, srv, user.ID)
	conv := startConversation(t, srv, who, `{}`)

	m.queue(uses("c1", "create_asset", `{"assets":[{"name":"Lathe","category_ids":["`+cat.ID+`"]}]}`))
	lines := send(t, srv, who, conv, "Add a lathe")
	offered := m.toolsSent(0)
	if !offered["search_assets"] || !offered["create_asset"] || offered["create_user"] || offered["save_role"] || offered["delete_category"] {
		t.Errorf("offered %v", offered)
	}
	if lineOf(lines, "confirm") == nil {
		t.Fatalf("no confirmation asked: %v", lines)
	}
	if countAssets(t, srv, "Lathe") != 0 {
		t.Fatal("the change was made before it was confirmed")
	}

	// The editor role is taken away before the person confirms.
	if _, err := srv.store.Exec("DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", user.ID, edit.ID); err != nil {
		t.Fatal(err)
	}
	m.queue(says("It is not something you can do now."))
	lines = confirm(t, srv, who, conv, true)
	if countAssets(t, srv, "Lathe") != 0 {
		t.Error("a change was made with a role taken away")
	}
	if n := m.requests(); n != 2 || m.toolsSent(1)["create_asset"] {
		t.Errorf("after the role went: %d requests, offered %v", n, m.toolsSent(n-1))
	}
	var answered bool
	for _, l := range lines {
		if msg, ok := l["message"].(map[string]any); ok && strings.Contains(fmt.Sprint(msg["blocks"]), "not available to you now") {
			answered = true
		}
	}
	if !answered {
		t.Errorf("the model was not told the tool is gone: %v", lines)
	}

	// Without assistant.use, the panel is gone too.
	if _, err := srv.store.Exec("DELETE FROM user_roles WHERE user_id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/messages", `{"text":"hello"}`); rec.Code != http.StatusForbidden {
		t.Errorf("without assistant.use: %d", rec.Code)
	}
}

// TestNothingChangesWithoutConfirmation: where review states are off, a
// change waits for the person; declined, nothing is done, and confirmed,
// it is made as theirs, through the assistant.
func TestNothingChangesWithoutConfirmation(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	who := sessionCookie(t, srv, admin.ID)
	m := newFakeModel(t)
	turnOnAssistant(t, srv, m, nil)
	conv := startConversation(t, srv, who, `{}`)

	m.queue(uses("c1", "create_asset", `{"assets":[{"name":"Drill press"}]}`), says("Left as it is."))
	lines := send(t, srv, who, conv, "Add a drill press")
	c := lineOf(lines, "confirm")
	if c == nil || c["pending"].(map[string]any)["name"] != "create_asset" {
		t.Fatalf("no confirmation: %v", lines)
	}
	if done := lineOf(lines, "done"); done["pending"] == nil {
		t.Errorf("the turn did not end waiting: %v", done)
	}
	confirm(t, srv, who, conv, false)
	if countAssets(t, srv, "Drill press") != 0 {
		t.Fatal("a declined change was made")
	}

	m.queue(uses("c2", "create_asset", `{"assets":[{"name":"Drill press"}]}`), says("Added."))
	send(t, srv, who, conv, "Add it after all")
	if rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/confirm", `{"approve":true,"call":"other"}`); rec.Code != http.StatusConflict {
		t.Errorf("confirming another call: %d", rec.Code)
	}
	lines = confirm(t, srv, who, conv, true)
	if countAssets(t, srv, "Drill press") != 1 {
		t.Fatalf("the confirmed change was not made: %v", lines)
	}
	if lineOf(lines, "changed") == nil {
		t.Error("the page was not told something changed")
	}
	var n int
	if err := srv.store.QueryRow("SELECT COUNT(*) FROM events WHERE payload LIKE ?", `%"via":"BAM! assistant"%`).Scan(&n); err != nil || n == 0 {
		t.Errorf("history does not say it was through the assistant (%d, %v)", n, err)
	}
	// A new message leaves a change waiting undone.
	m.queue(uses("c3", "create_asset", `{"assets":[{"name":"Bench grinder"}]}`), says("Fine."))
	send(t, srv, who, conv, "Add a grinder")
	send(t, srv, who, conv, "No, never mind")
	if countAssets(t, srv, "Bench grinder") != 0 {
		t.Error("a change left waiting was made")
	}
}

// TestChangesLandAsDraftsWhereReviewIsOn: in a category using review
// states, the assistant's new asset is made at once, as a draft for the
// person to accept or reject, without asking first.
func TestChangesLandAsDraftsWhereReviewIsOn(t *testing.T) {
	f := newReviewFixture(t)
	m := newFakeModel(t)
	turnOnAssistant(t, f.srv, m, nil)
	conv := startConversation(t, f.srv, f.admin, `{}`)
	m.queue(uses("c1", "create_asset", `{"assets":[{"name":"Band saw","category_ids":["`+f.tools.ID+`"]}]}`), says("Added as a draft."))
	lines := send(t, f.srv, f.admin, conv, "There's a band saw")
	if lineOf(lines, "confirm") != nil {
		t.Fatalf("a draft was asked to be confirmed: %v", lines)
	}
	var id string
	if err := f.srv.store.QueryRow("SELECT id FROM assets WHERE name = 'Band saw'").Scan(&id); err != nil {
		t.Fatalf("no draft made: %v (%v)", err, lines)
	}
	if r := reviewOf(t, f.srv, f.admin, id); r == nil || r.State != models.ReviewDraft || r.Proposal == nil || !r.Proposal.New {
		t.Errorf("review %+v", r)
	}
	// Outside review, the same asks first.
	m.queue(uses("c2", "create_asset", `{"assets":[{"name":"Kettle"}]}`))
	if lines := send(t, f.srv, f.admin, conv, "And a kettle"); lineOf(lines, "confirm") == nil {
		t.Errorf("a change outside review was not asked: %v", lines)
	}
}

// TestTheProvidersKeyIsNeverReturned: the key is written, kept when left
// blank, and never in an answer or what the panel is sent; turning the
// assistant on says first what goes where.
func TestTheProvidersKeyIsNeverReturned(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	who := sessionCookie(t, srv, admin.ID)
	m := newFakeModel(t)
	rec := doJSON(t, srv, who, http.MethodPut, "/api/v1/settings/assistant", `{"enabled":true,"provider":"anthropic","endpoint":"`+m.srv.URL+`","api_key":"sk-test-secret","max_tokens":1000,"max_steps":5,"keep_days":30,"name":"Helper"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), m.srv.URL) {
		t.Fatalf("turned on without saying where things go: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, who, http.MethodPut, "/api/v1/settings/assistant", `{"enabled":true,"acknowledge":true,"provider":"anthropic","endpoint":"`+m.srv.URL+`","api_key":"sk-test-secret","max_tokens":1000,"max_steps":5,"keep_days":30,"name":"Helper"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "sk-test-secret") || !strings.Contains(rec.Body.String(), `"key_set":true`) {
		t.Fatalf("saving: %d %s", rec.Code, rec.Body)
	}
	// Left blank, the key is kept.
	rec = doJSON(t, srv, who, http.MethodPut, "/api/v1/settings/assistant", `{"enabled":true,"provider":"anthropic","endpoint":"`+m.srv.URL+`","max_tokens":1000,"max_steps":5,"keep_days":30,"name":"Helper"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("saving again: %d %s", rec.Code, rec.Body)
	}
	if set, _ := srv.store.AssistantSettings(); set.APIKey != "sk-test-secret" || set.AcknowledgedBy == nil || *set.AcknowledgedBy != admin.ID {
		t.Errorf("stored %+v", set)
	}
	for _, path := range []string{"/api/v1/settings/assistant", "/api/v1/assistant"} {
		if rec := doJSON(t, srv, who, http.MethodGet, path, ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "sk-test-secret") {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	conv := startConversation(t, srv, who, `{}`)
	m.queue(uses("c1", "server_info", `{}`), says("All well."))
	rec = doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/messages", `{"text":"How are things?"}`)
	if strings.Contains(rec.Body.String(), "sk-test-secret") || !strings.Contains(rec.Body.String(), "All well.") {
		t.Errorf("the turn: %s", rec.Body)
	}
	if rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assistant/conversations/"+conv, ""); strings.Contains(rec.Body.String(), "sk-test-secret") {
		t.Errorf("the conversation: %s", rec.Body)
	}
	// Set in the configuration, it is Admin's no longer.
	srv.cfg.Assistant.Model = "configured-model"
	rec = doJSON(t, srv, who, http.MethodGet, "/api/v1/settings/assistant", "")
	if !strings.Contains(rec.Body.String(), `"model":"configured-model"`) || !strings.Contains(rec.Body.String(), `"fixed":{"model":true}`) {
		t.Errorf("configured: %s", rec.Body)
	}
}

// TestLimitsStopTheConversation: a person past their messages a day is
// refused, and one past their tokens is stopped part way, each told when
// the limit resets.
func TestLimitsStopTheConversation(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	who := sessionCookie(t, srv, admin.ID)
	m := newFakeModel(t)
	turnOnAssistant(t, srv, m, func(s *store.AssistantSettings) { s.PersonMessages, s.PersonTokens = 2, 40 })
	conv := startConversation(t, srv, who, `{}`)
	// Each reply counts 50 tokens: the second call to the model is past
	// the limit.
	m.queue(uses("c1", "server_info", `{}`), says("never sent"))
	lines := send(t, srv, who, conv, "What version is this?")
	limit := lineOf(lines, "limit")
	if limit == nil || limit["resets_at"] == nil || !strings.Contains(limit["error"].(string), "resets at") {
		t.Fatalf("no limit: %v", lines)
	}
	if m.requests() != 1 {
		t.Errorf("%d requests went to the model past the limit", m.requests())
	}
	srv.store.Exec("UPDATE assistant_usage SET tokens = 0")
	send(t, srv, who, conv, "Again")
	rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/conversations/"+conv+"/messages", `{"text":"And again"}`)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "resets_at") {
		t.Errorf("past the messages a day: %d %s", rec.Code, rec.Body)
	}
	var status AssistantStatus
	_ = json.Unmarshal(doJSON(t, srv, who, http.MethodGet, "/api/v1/assistant", "").Body.Bytes(), &status)
	if status.Used.Messages != 2 || !status.ResetsAt.After(time.Now()) || !status.Available {
		t.Errorf("status %+v", status)
	}
}

// TestConversationsAreTheirsAlone: no one else reads a person's
// conversations, admins included, and they are deleted once older than the
// instance keeps them.
func TestConversationsAreTheirsAlone(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	m := newFakeModel(t)
	turnOnAssistant(t, srv, m, nil)
	member := userWithPermissions(t, srv, "member@test.org", rbac.PermAssistantUse)
	conv := startConversation(t, srv, member, `{}`)
	m.queue(says("Hello."))
	send(t, srv, member, conv, "Hi")
	adminCookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assistant/conversations/"+conv, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an admin read it: %d", rec.Code)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assistant/conversations", ""); strings.Contains(rec.Body.String(), conv) {
		t.Errorf("an admin listed it: %s", rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/assistant/conversations/"+conv, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an admin deleted it: %d", rec.Code)
	}
	if rec := doJSON(t, srv, member, http.MethodGet, "/api/v1/assistant/conversations/"+conv, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello.") {
		t.Errorf("its person: %d %s", rec.Code, rec.Body)
	}
	if n, err := srv.SweepConversations(time.Now().AddDate(0, 0, 29)); err != nil || n != 0 {
		t.Errorf("swept early: %d %v", n, err)
	}
	if n, err := srv.SweepConversations(time.Now().AddDate(0, 0, 31)); err != nil || n != 1 {
		t.Errorf("swept %d, %v", n, err)
	}
	if rec := doJSON(t, srv, member, http.MethodGet, "/api/v1/assistant/conversations/"+conv, ""); rec.Code != http.StatusNotFound {
		t.Errorf("kept past its time: %d", rec.Code)
	}
	// Deleted by its person.
	conv = startConversation(t, srv, member, `{}`)
	if rec := doJSON(t, srv, member, http.MethodDelete, "/api/v1/assistant/conversations/"+conv, ""); rec.Code != http.StatusOK {
		t.Errorf("deleting: %d", rec.Code)
	}
}

// TestAGuideStartsAConversationAndKeepsItsProgress: a guide starts a
// conversation with its purpose, has the model record answers to its
// questions, and has a poster's code.
func TestAGuideStartsAConversationAndKeepsItsProgress(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	who := sessionCookie(t, srv, admin.ID)
	m := newFakeModel(t)
	turnOnAssistant(t, srv, m, nil)
	cat, err := srv.store.CreateCategory("Machines", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"slug":"workshop","name":"Record the workshop","purpose":"Walk through the equipment in your area.","enabled":true,"category_ids":["` + cat.ID + `"],"questions":["What is there?","What does each need to run?"]}`
	if rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/guides", body); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"url":"http://localhost:8080/g/workshop"`) || !strings.Contains(rec.Body.String(), `"name":"Machines"`) {
		t.Fatalf("saving the guide: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/assistant/guides", body); rec.Code != http.StatusConflict {
		t.Errorf("a second guide at the address: %d", rec.Code)
	}
	if rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assistant/guides/workshop/qr.svg", ""); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "<svg") {
		t.Errorf("its code: %d", rec.Code)
	}
	conv := startConversation(t, srv, who, `{"guide":"workshop"}`)
	m.queue(uses("c1", "record_answer", `{"question":1,"answer":"A band saw and an extractor"}`), says("Next: what does each need?"))
	lines := send(t, srv, who, conv, "There's a band saw, and it only runs when the extractor's on")
	if p := lineOf(lines, "progress"); p == nil || p["progress"].(map[string]any)["1"] != "A band saw and an extractor" {
		t.Errorf("progress: %v", lines)
	}
	m.mu.Lock()
	system := fmt.Sprint(m.got[0]["system"])
	m.mu.Unlock()
	if !strings.Contains(system, "Walk through the equipment") || !strings.Contains(system, "What does each need to run?") || !strings.Contains(system, `"Machines"`) {
		t.Errorf("the model was not told the guide: %s", system)
	}
	rec := doJSON(t, srv, who, http.MethodGet, "/api/v1/assistant/conversations/"+conv, "")
	if !strings.Contains(rec.Body.String(), `"progress":{"1":"A band saw and an extractor"}`) || !strings.Contains(rec.Body.String(), `"guide":{`) {
		t.Errorf("the conversation: %s", rec.Body)
	}
	// Someone without settings.manage sees only guides turned on.
	if rec := doJSON(t, srv, who, http.MethodPut, "/api/v1/assistant/guides/workshop", strings.Replace(body, `"enabled":true`, `"enabled":false`, 1)); rec.Code != http.StatusOK {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	member := userWithPermissions(t, srv, "member@test.org", rbac.PermAssistantUse)
	if rec := doJSON(t, srv, member, http.MethodGet, "/api/v1/assistant/guides", ""); strings.Contains(rec.Body.String(), "workshop") {
		t.Errorf("a guide turned off was listed: %s", rec.Body)
	}
}
