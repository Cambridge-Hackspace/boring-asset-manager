package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// incomingShop is the wood shop (flows_engine_test.go), with a server that
// can seal signing secrets, a hook offered on its wood, fields to find and
// set, and a role for incoming hooks to act as, given in its scope.
type incomingShop struct {
	*woodShop
	identify *models.AssetHook
	acts     *models.RoleWithPermissions
	// spoon is in the kitchen, outside the wood shop's scope.
	spoon *models.AssetWithDetails
	n     int
}

func newIncomingShop(t *testing.T) *incomingShop {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey = testSecretKey
	st := setupTestStore(t)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	w := woodShopIn(t, st)
	w.srv, w.admin = srv, admin
	s := &incomingShop{woodShop: w}
	for _, f := range []struct{ key, label, typ string }{{"bin", "Bin", fields.TypeText}, {"weight", "Weight", fields.TypeNumber}} {
		field, err := st.CreateField(f.key, f.label, f.typ, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []string{w.wood.ID, w.kitchen.ID} {
			if err := st.AssignCategoryField(c, field.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s.identify, err = st.SaveHook("", store.HookInput{Key: "identify", Label: "Identify"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{w.wood.ID, w.kitchen.ID} {
		if _, err := st.SetCategoryHooks(c, []models.CategoryHook{{HookID: s.identify.ID, Cascades: true}}); err != nil {
			t.Fatal(err)
		}
	}
	if s.acts, err = st.CreateRole(store.RoleInput{Name: "Incoming", Permissions: []string{
		rbac.PermAssetsRead, rbac.PermAssetsHook, rbac.PermStockRecord, rbac.PermAssetsWrite, rbac.PermAssetsDelete,
		rbac.PermAssetsScan, rbac.PermFlowsRun,
	}}); err != nil {
		t.Fatal(err)
	}
	s.spoon = s.board(t, w.kitchen.ID, "Spoon")
	return s
}

// board files an asset counted by quantity, with a code and a bin of its
// own.
func (s *incomingShop) board(t *testing.T, category, name string) *models.AssetWithDetails {
	t.Helper()
	s.n++
	ten := 10
	values := map[string]string{"bin": fmt.Sprintf("B%d", s.n)}
	if category != s.kitchen.ID {
		values["owner"] = "Ada"
	}
	a, err := s.srv.store.CreateAssetWithStock(name, []string{category},
		[]models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: fmt.Sprintf("C-%d", s.n)}}, values, &store.StockInput{Quantity: &ten})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// hook makes an incoming hook acting as the role, in the wood shop's scope
// unless scopes are given, and returns it with its secret.
func (s *incomingShop) hook(t *testing.T, name, auth, action string, set models.IncomingSettings, scopes ...string) (*models.IncomingHook, string) {
	t.Helper()
	if scopes == nil {
		scopes = []string{s.scope.ID}
	}
	in := store.IncomingInput{Name: name, Enabled: true, Auth: auth, Action: action, Settings: set, RateLimit: store.MaxIncomingRate,
		Roles: []models.RoleAssignment{{RoleID: s.acts.ID, ScopeIDs: scopes}}}
	if auth == models.IncomingHMAC {
		in.Seal = func(id, secret string) (string, error) {
			return s.srv.sealer.Seal([]byte(secret), store.IncomingPurpose(id))
		}
	}
	h, secret, err := s.srv.store.CreateIncomingHook(in, s.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	return h, secret
}

// send posts body to an incoming hook with these headers.
func sendIncoming(t *testing.T, srv *Server, id, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incoming/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func bearer(secret string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + secret}
}

// signed are the headers of a request signed as webhooks are, at a time.
func signed(secret, body, delivery string, at time.Time) map[string]string {
	ts := strconv.FormatInt(at.Unix(), 10)
	return map[string]string{"BAM-Timestamp": ts, "BAM-Delivery": delivery, "BAM-Signature": notify.Signature(secret, ts, []byte(body))}
}

// accepted reads a 202's answer, failing on anything else.
func accepted(t *testing.T, what string, rec *httptest.ResponseRecorder) (eventIDs []string, assetID string) {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%s: %d %s", what, rec.Code, rec.Body)
	}
	var out struct {
		EventID  *string  `json:"event_id"`
		EventIDs []string `json:"event_ids"`
		AssetID  string   `json:"asset_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.EventIDs) > 0 && (out.EventID == nil || *out.EventID != out.EventIDs[0]) {
		t.Errorf("%s: event_id %v, event_ids %v", what, out.EventID, out.EventIDs)
	}
	return out.EventIDs, out.AssetID
}

// byHook checks that the events are about the asset and the hook's: of
// the kind, recorded as made by it, through via when given.
func (s *incomingShop) byHook(t *testing.T, what string, ids []string, assetID string, h *models.IncomingHook, kind, via string) {
	t.Helper()
	events, _, err := s.srv.store.ListAssetEvents(assetID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		for _, id := range ids {
			if e.ID != id {
				continue
			}
			a := e.Payload.Actor
			if a == nil || !a.Incoming || a.ID != h.ID || a.Name != h.Name || a.Token || a.Email != "" || a.Via != via {
				t.Errorf("%s: %s by %+v", what, e.Kind, a)
			}
			found = found || e.Kind == kind
		}
	}
	if !found {
		t.Errorf("%s: no %s among %v on %s", what, kind, ids, assetID)
	}
}

// ways are the ways an asset is found, each with the request that finds a.
func (s *incomingShop) ways(a *models.AssetWithDetails) []struct {
	asset models.IncomingAsset
	body  map[string]any
} {
	return []struct {
		asset models.IncomingAsset
		body  map[string]any
	}{
		{models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: a.Asset.ID}, map[string]any{}},
		{models.IncomingAsset{By: models.IncomingAssetID, Path: "thing.id"}, map[string]any{"thing": map[string]any{"id": a.Asset.ID}}},
		{models.IncomingAsset{By: models.IncomingAssetCode, Path: "$.scanned.0"}, map[string]any{"scanned": []any{a.GeneratorKeys[0].AssignedCode}}},
		{models.IncomingAsset{By: models.IncomingAssetField, Field: "bin", Path: "bin"}, map[string]any{"bin": a.FieldValues["bin"]}},
	}
}

func TestEachIncomingActionFindsItsAssetEachWay(t *testing.T) {
	s := newIncomingShop(t)
	st := s.srv.store
	for i := 0; i < 4; i++ {
		for _, action := range []string{models.IncomingTriggerHook, models.IncomingQuantity, models.IncomingFields} {
			a := s.board(t, s.short.ID, "Oak")
			way := s.ways(a)[i]
			what := action + " by " + way.asset.By
			set := models.IncomingSettings{Asset: way.asset}
			body := way.body
			switch action {
			case models.IncomingTriggerHook:
				set.Hook = "identify"
			case models.IncomingQuantity:
				body["adjust"] = -3
			case models.IncomingFields:
				// Mapped from the request, or the request's own field_values.
				if i%2 == 0 {
					set.Fields = map[string]string{"weight": "data.grams"}
					body["data"] = map[string]any{"grams": 250}
				} else {
					body["field_values"] = map[string]any{"weight": 250}
				}
			}
			auth := models.IncomingBearer
			if i%2 == 1 {
				auth = models.IncomingHMAC
			}
			h, secret := s.hook(t, what, auth, action, set)
			raw, _ := json.Marshal(body)
			headers := bearer(secret)
			if auth == models.IncomingHMAC {
				headers = signed(secret, string(raw), "d-"+what, time.Now())
			}
			ids, assetID := accepted(t, what, sendIncoming(t, s.srv, h.ID, string(raw), headers))
			if assetID != a.Asset.ID {
				t.Errorf("%s: acted on %s, not %s", what, assetID, a.Asset.ID)
			}
			after, err := st.GetAssetByID(a.Asset.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case models.IncomingTriggerHook:
				s.byHook(t, what, ids, a.Asset.ID, h, models.EventAssetHook, "")
			case models.IncomingQuantity:
				s.byHook(t, what, ids, a.Asset.ID, h, models.EventQuantityChanged, "")
				if after.Asset.Stock == nil || after.Asset.Stock.Quantity != 7 {
					t.Errorf("%s: stock %+v", what, after.Asset.Stock)
				}
			case models.IncomingFields:
				s.byHook(t, what, ids, a.Asset.ID, h, models.EventAssetEdited, "")
				if after.FieldValues["weight"] != "250" || after.FieldValues["owner"] != "Ada" {
					t.Errorf("%s: fields %v", what, after.FieldValues)
				}
			}
		}
	}

	// A flow that finds what it collects, found each way.
	collect := s.saveFlow(t, "collect", "Collect it", s.starter(t, 1, s.short.ID))
	for i := 0; i < 4; i++ {
		a := s.board(t, s.short.ID, "Pine")
		way := s.ways(a)[i]
		what := "collecting by " + way.asset.By
		h, secret := s.hook(t, what, models.IncomingBearer, models.IncomingFlow, models.IncomingSettings{FlowID: collect.ID, Asset: way.asset})
		raw, _ := json.Marshal(way.body)
		ids, assetID := accepted(t, what, sendIncoming(t, s.srv, h.ID, string(raw), bearer(secret)))
		if assetID != a.Asset.ID {
			t.Errorf("%s: collected %s, not %s", what, assetID, a.Asset.ID)
		}
		if after, err := st.GetAssetByID(a.Asset.ID); err != nil || after.Asset.ArchivedAt == nil {
			t.Errorf("%s: not archived: %v", what, err)
		}
		s.byHook(t, what, ids, a.Asset.ID, h, models.EventAssetArchived, "flow “Collect it”")
	}

	// A flow that files what the form elsewhere describes: no asset found
	// first. Its answers come from answers, or the request's own fields.
	leave := s.saveFlow(t, "leave", "Leave something", s.starter(t, 0, s.short.ID))
	h, secret := s.hook(t, "Form", models.IncomingBearer, models.IncomingFlow, models.IncomingSettings{FlowID: leave.ID})
	for _, body := range []string{`{"answers": {"name": "Walnut offcut", "owner": "Bea"}}`, `{"name": "Ash plank", "owner": "Cy"}`} {
		ids, assetID := accepted(t, "leaving", sendIncoming(t, s.srv, h.ID, body, bearer(secret)))
		made, err := st.GetAssetByID(assetID)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if (made.Asset.Name != "Walnut offcut" && made.Asset.Name != "Ash plank") || (made.FieldValues["owner"] != "Bea" && made.FieldValues["owner"] != "Cy") {
			t.Errorf("%s: filed %q %v", body, made.Asset.Name, made.FieldValues)
		}
		s.byHook(t, "leaving", ids, assetID, h, models.EventAssetCreated, "flow “Leave something”")
		filed, err := st.FiledBy(leave.ID)
		if err != nil || !filed[assetID] {
			t.Errorf("%s: not filed by the flow: %v", body, err)
		}
	}
	// A required answer left out stops it at its step, saying which.
	rec := sendIncoming(t, s.srv, h.ID, `{"answers": {"owner": "Bea"}}`, bearer(secret))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `Running the flow: Step \"`) {
		t.Errorf("an answer left out: %d %s", rec.Code, rec.Body)
	}

	// Form fields read as an object of text.
	a := s.board(t, s.short.ID, "Birch")
	h, secret = s.hook(t, "Form fields", models.IncomingBearer, models.IncomingQuantity, models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetCode, Path: "code"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incoming/"+h.ID, strings.NewReader("code="+a.GeneratorKeys[0].AssignedCode+"&set=4"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+secret)
	form := httptest.NewRecorder()
	s.srv.Handler().ServeHTTP(form, req)
	accepted(t, "form fields", form)
	if after, _ := st.GetAssetByID(a.Asset.ID); after.Asset.Stock.Quantity != 4 {
		t.Errorf("form fields: stock %+v", after.Asset.Stock)
	}
}

func TestIncomingHooksRefuse(t *testing.T) {
	s := newIncomingShop(t)
	a := s.board(t, s.short.ID, "Oak")
	fixed := models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: a.Asset.ID}}
	bh, bs := s.hook(t, "Bearer", models.IncomingBearer, models.IncomingQuantity, fixed)
	sh, ss := s.hook(t, "Signed", models.IncomingHMAC, models.IncomingQuantity, fixed)
	use := `{"adjust": -1}`
	refused := func(what string, rec *httptest.ResponseRecorder, status int, says string) {
		t.Helper()
		if rec.Code != status || !strings.Contains(rec.Body.String(), says) {
			t.Errorf("%s: %d %s, want %d saying %q", what, rec.Code, rec.Body, status, says)
		}
		for _, secret := range []string{bs, ss} {
			if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), store.IncomingSecretPrefix) {
				t.Errorf("%s: the answer names a secret: %s", what, rec.Body)
			}
		}
	}

	accepted(t, "the right secret", sendIncoming(t, s.srv, bh.ID, use, bearer(bs)))
	refused("no secret", sendIncoming(t, s.srv, bh.ID, use, nil), http.StatusUnauthorized, "Authorization: Bearer")
	refused("a wrong secret", sendIncoming(t, s.srv, bh.ID, use, bearer(bs+"0")), http.StatusUnauthorized, "not this hook's")
	refused("another hook's secret", sendIncoming(t, s.srv, bh.ID, use, bearer(ss)), http.StatusUnauthorized, "not this hook's")
	refused("an API token", sendIncoming(t, s.srv, bh.ID, use, bearer("bam_x")), http.StatusUnauthorized, "not this hook's")
	refused("an unknown hook", sendIncoming(t, s.srv, "nope", use, bearer(bs)), http.StatusNotFound, "No incoming hook")

	now := time.Now()
	accepted(t, "signed", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d1", now)))
	refused("a replay", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d1", now)), http.StatusConflict, "received already")
	refused("a bad signature", sendIncoming(t, s.srv, sh.ID, `{"adjust": -9}`, signed(ss, use, "d2", now)), http.StatusUnauthorized, "does not match")
	refused("signed with another secret", sendIncoming(t, s.srv, sh.ID, use, signed(bs, use, "d3", now)), http.StatusUnauthorized, "does not match")
	refused("too old", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d4", now.Add(-6*time.Minute))), http.StatusUnauthorized, "five minutes")
	refused("from the future", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d5", now.Add(6*time.Minute))), http.StatusUnauthorized, "five minutes")
	unsigned := signed(ss, use, "d6", now)
	delete(unsigned, "BAM-Delivery")
	refused("no delivery ID", sendIncoming(t, s.srv, sh.ID, use, unsigned), http.StatusUnauthorized, "BAM-Delivery")
	refused("a bearer secret to a signing hook", sendIncoming(t, s.srv, sh.ID, use, bearer(ss)), http.StatusUnauthorized, "BAM-Timestamp")
	// The refused were not taken: a delivery refused for its signature can
	// be sent again, signed.
	accepted(t, "d2 signed rightly", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d2", now)))

	// Too large a body is refused before anything is read into it.
	big := `{"adjust": -1, "pad": "` + strings.Repeat("x", incomingBodyLimit) + `"}`
	refused("too large", sendIncoming(t, s.srv, bh.ID, big, bearer(bs)), http.StatusRequestEntityTooLarge, "limited")
	refused("not JSON", sendIncoming(t, s.srv, bh.ID, `[1, 2]`, bearer(bs)), http.StatusBadRequest, "JSON object")

	// Too many requests a minute.
	in := store.IncomingInput{Name: "Slow", Enabled: true, Auth: models.IncomingBearer, Action: models.IncomingQuantity, Settings: fixed, RateLimit: 2,
		Roles: []models.RoleAssignment{{RoleID: s.acts.ID}}}
	slow, secret, err := s.srv.store.CreateIncomingHook(in, "")
	if err != nil {
		t.Fatal(err)
	}
	accepted(t, "first", sendIncoming(t, s.srv, slow.ID, use, bearer(secret)))
	accepted(t, "second", sendIncoming(t, s.srv, slow.ID, use, bearer(secret)))
	rec := sendIncoming(t, s.srv, slow.ID, use, bearer(secret))
	refused("a third", rec, http.StatusTooManyRequests, "2 requests a minute")
	if wait, _ := strconv.Atoi(rec.Header().Get("Retry-After")); wait < 1 || wait > 60 {
		t.Errorf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	// A request is authenticated before it is counted, so one without the
	// secret is refused as such, and uses up nothing; another hook's share
	// is its own.
	refused("unauthenticated, past its rate", sendIncoming(t, s.srv, slow.ID, use, nil), http.StatusUnauthorized, "Bearer")
	accepted(t, "another hook meanwhile", sendIncoming(t, s.srv, bh.ID, use, bearer(bs)))

	// Turned off.
	if _, _, err := s.srv.store.UpdateIncomingHook(bh.ID, store.IncomingInput{Name: "Bearer", Auth: models.IncomingBearer, Action: models.IncomingQuantity,
		Settings: fixed, Roles: []models.RoleAssignment{{RoleID: s.acts.ID, ScopeIDs: []string{s.scope.ID}}}}, false); err != nil {
		t.Fatal(err)
	}
	refused("turned off", sendIncoming(t, s.srv, bh.ID, use, bearer(bs)), http.StatusForbidden, "turned off")

	// What the endpoint behind it refuses, it refuses, saying so.
	byCode := models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetCode, Path: "code"}}
	ch, cs := s.hook(t, "By code", models.IncomingBearer, models.IncomingQuantity, byCode)
	refused("no code", sendIncoming(t, s.srv, ch.ID, use, bearer(cs)), http.StatusBadRequest, "no asset code at code")
	refused("an unknown code", sendIncoming(t, s.srv, ch.ID, `{"code": "nope", "adjust": -1}`, bearer(cs)), http.StatusNotFound, "No asset this hook can reach")
	refused("too little stock", sendIncoming(t, s.srv, ch.ID, `{"code": "`+a.GeneratorKeys[0].AssignedCode+`", "adjust": -100}`, bearer(cs)), http.StatusConflict, "Recording the use or restock: ")
	refused("neither adjust nor set", sendIncoming(t, s.srv, ch.ID, `{"code": "`+a.GeneratorKeys[0].AssignedCode+`"}`, bearer(cs)), http.StatusBadRequest, "Recording the use or restock: ")
	refused("not a number", sendIncoming(t, s.srv, ch.ID, `{"code": "`+a.GeneratorKeys[0].AssignedCode+`", "adjust": "lots"}`, bearer(cs)), http.StatusBadRequest, "whole number")

	// Restored without its secret, it refuses until given a new one.
	if _, err := s.srv.store.Exec("UPDATE incoming_hooks SET secret_hash = NULL WHERE id = ?", ch.ID); err != nil {
		t.Fatal(err)
	}
	refused("no secret kept", sendIncoming(t, s.srv, ch.ID, use, bearer(cs)), http.StatusUnauthorized, "no secret")
	// A signing secret this server's key cannot open.
	if _, err := s.srv.store.Exec("UPDATE incoming_hooks SET sealed_secret = ? WHERE id = ?", "v1:AAAA", sh.ID); err != nil {
		t.Fatal(err)
	}
	refused("an unopenable secret", sendIncoming(t, s.srv, sh.ID, use, signed(ss, use, "d9", now)), http.StatusServiceUnavailable, "new one")
}

func TestIncomingHooksReachOnlyTheirScopes(t *testing.T) {
	s := newIncomingShop(t)
	oak := s.board(t, s.short.ID, "Oak")
	byCode := models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetCode, Path: "code"}}
	use := func(code string) string { return `{"code": "` + code + `", "adjust": -1}` }

	// Narrowed to the wood shop: the spoon in the kitchen is not there.
	h, secret := s.hook(t, "Wood shop", models.IncomingBearer, models.IncomingQuantity, byCode)
	accepted(t, "in its scope", sendIncoming(t, s.srv, h.ID, use(oak.GeneratorKeys[0].AssignedCode), bearer(secret)))
	rec := sendIncoming(t, s.srv, h.ID, use(s.spoon.GeneratorKeys[0].AssignedCode), bearer(secret))
	if rec.Code != http.StatusNotFound {
		t.Errorf("outside its scope, by code: %d %s", rec.Code, rec.Body)
	}
	for _, way := range s.ways(s.spoon) {
		h, secret := s.hook(t, "Kitchen by "+way.asset.By, models.IncomingBearer, models.IncomingTriggerHook, models.IncomingSettings{Hook: "identify", Asset: way.asset})
		raw, _ := json.Marshal(way.body)
		if rec := sendIncoming(t, s.srv, h.ID, string(raw), bearer(secret)); rec.Code != http.StatusNotFound {
			t.Errorf("outside its scope, %s: %d %s", way.asset.By, rec.Code, rec.Body)
		}
	}
	if after, _ := s.srv.store.GetAssetByID(s.spoon.Asset.ID); after.Asset.Stock.Quantity != 10 {
		t.Errorf("the spoon was used: %+v", after.Asset.Stock)
	}
	events, _, _ := s.srv.store.ListAssetEvents(s.spoon.Asset.ID, "", 50)
	for _, e := range events {
		if e.Payload.Actor != nil && e.Payload.Actor.Incoming {
			t.Errorf("the spoon has %s by %s", e.Kind, e.Payload.Actor.Name)
		}
	}

	// In every scope, it reaches the kitchen too.
	everywhere, secret := s.hook(t, "Everywhere", models.IncomingBearer, models.IncomingQuantity, byCode, []string{}...)
	accepted(t, "everywhere", sendIncoming(t, s.srv, everywhere.ID, use(s.spoon.GeneratorKeys[0].AssignedCode), bearer(secret)))

	// A role without the permission its action needs reaches the asset to
	// read it, and is refused.
	readers, err := s.srv.store.CreateRole(store.RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	fixed := models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: oak.Asset.ID}}
	ro, roSecret, err := s.srv.store.CreateIncomingHook(store.IncomingInput{Name: "Reader", Enabled: true, Auth: models.IncomingBearer,
		Action: models.IncomingQuantity, Settings: fixed, Roles: []models.RoleAssignment{{RoleID: readers.ID}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := sendIncoming(t, s.srv, ro.ID, `{"adjust": -1}`, bearer(roSecret)); rec.Code != http.StatusForbidden {
		t.Errorf("without stock.record: %d %s", rec.Code, rec.Body)
	}
}

func TestManagingIncomingHooks(t *testing.T) {
	s := newIncomingShop(t)
	srv := s.srv
	admin := sessionCookie(t, srv, s.admin.ID)
	settings := userWithPermissions(t, srv, "settings@test.org", rbac.PermSettingsManage)
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	oak := s.board(t, s.short.ID, "Oak")
	body := func(extra string) string {
		return `{"name": "Scale", "action": "quantity", "asset": {"by": "code", "path": "code"},
			"roles": [{"role_id": "` + s.acts.ID + `", "scope_ids": ["` + s.scope.ID + `"]}]` + extra + `}`
	}
	var made struct {
		Hook   IncomingView `json:"incoming_hook"`
		Secret string       `json:"secret"`
	}
	rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/incoming", body(""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("making one: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	h := made.Hook
	if !strings.HasPrefix(made.Secret, store.IncomingSecretPrefix) || !strings.HasSuffix(h.URL, "/api/v1/incoming/"+h.ID) ||
		h.Auth != models.IncomingBearer || !h.Enabled || h.RateLimit != store.DefaultIncomingRate || !h.HasSecret {
		t.Fatalf("made %+v", made)
	}
	use := `{"code": "` + oak.GeneratorKeys[0].AssignedCode + `", "adjust": -1}`
	accepted(t, "using it", sendIncoming(t, srv, h.ID, use, bearer(made.Secret)))

	// Listed and read, never with its secret.
	for _, path := range []string{"/api/v1/incoming", "/api/v1/incoming/" + h.ID} {
		rec := doJSON(t, srv, admin, http.MethodGet, path, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), h.ID) || strings.Contains(rec.Body.String(), made.Secret) ||
			strings.Contains(rec.Body.String(), `"secret"`) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if rec := doJSON(t, srv, reader, http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s as a reader: %d", path, rec.Code)
		}
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/incoming", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}

	// A new secret: the old one stops working.
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/incoming/"+h.ID, body(`, "new_secret": true`))
	var renewed struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &renewed)
	if rec.Code != http.StatusOK || renewed.Secret == "" || renewed.Secret == made.Secret {
		t.Fatalf("a new secret: %d %s", rec.Code, rec.Body)
	}
	if rec := sendIncoming(t, srv, h.ID, use, bearer(made.Secret)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the old secret: %d", rec.Code)
	}
	accepted(t, "the new secret", sendIncoming(t, srv, h.ID, use, bearer(renewed.Secret)))
	// A change without one keeps it, and says none.
	rec = doJSON(t, srv, admin, http.MethodPut, "/api/v1/incoming/"+h.ID, body(`, "enabled": false`))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"secret"`) || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Errorf("a change: %d %s", rec.Code, rec.Body)
	}

	// What is refused.
	for what, c := range map[string]struct {
		body string
		want int
	}{
		"an unknown action":      {`{"name": "x", "action": "delete", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an unknown hook":        {`{"name": "x", "action": "hook", "hook": "nope", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an unknown flow":        {`{"name": "x", "action": "flow", "flow_id": "nope", "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an unknown field":       {`{"name": "x", "action": "fields", "fields": {"nope": "a"}, "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"no path":                {`{"name": "x", "action": "quantity", "asset": {"by": "code"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an empty part":          {`{"name": "x", "action": "quantity", "asset": {"by": "code", "path": "a..b"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"no asset":               {`{"name": "x", "action": "quantity", "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an unknown fixed asset": {`{"name": "x", "action": "quantity", "asset": {"by": "fixed", "asset_id": "nope"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"no role":                {`{"name": "x", "action": "quantity", "asset": {"by": "code", "path": "c"}}`, http.StatusBadRequest},
		"an unknown role":        {`{"name": "x", "action": "quantity", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "nope"}]}`, http.StatusBadRequest},
		"an unknown scope":       {`{"name": "x", "action": "quantity", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "` + s.acts.ID + `", "scope_ids": ["nope"]}]}`, http.StatusBadRequest},
		"too fast":               {`{"name": "x", "action": "quantity", "asset": {"by": "code", "path": "c"}, "rate_limit": 601, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
		"an unknown auth":        {`{"name": "x", "auth": "basic", "action": "quantity", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "` + s.acts.ID + `"}]}`, http.StatusBadRequest},
	} {
		if rec := doJSON(t, srv, admin, http.MethodPost, "/api/v1/incoming", c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}

	// It can do no more than whoever saves it: settings.manage alone
	// cannot give it the role's asset permissions.
	rec = doJSON(t, srv, settings, http.MethodPost, "/api/v1/incoming", body(""))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "woodshop:"+rbac.PermStockRecord) {
		t.Errorf("more than its maker holds: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/incoming", body("")); rec.Code != http.StatusForbidden {
		t.Errorf("as a reader: %d", rec.Code)
	}

	// Signing needs [auth] secret_key.
	plain, plainAdmin, _ := setupTestServer(t)
	role, err := plain.store.CreateRole(store.RoleInput{Name: "Counters", Permissions: []string{rbac.PermStockRecord}})
	if err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, plain, sessionCookie(t, plain, plainAdmin.ID), http.MethodPost, "/api/v1/incoming",
		`{"name": "x", "auth": "hmac", "action": "quantity", "asset": {"by": "code", "path": "c"}, "roles": [{"role_id": "`+role.ID+`"}]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key") {
		t.Errorf("signing without a key: %d %s", rec.Code, rec.Body)
	}

	// Deleted, its address is gone.
	if rec := doJSON(t, srv, admin, http.MethodDelete, "/api/v1/incoming/"+h.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("deleting: %d %s", rec.Code, rec.Body)
	}
	if rec := sendIncoming(t, srv, h.ID, use, bearer(renewed.Secret)); rec.Code != http.StatusNotFound {
		t.Errorf("after deleting: %d", rec.Code)
	}
	if rec := doJSON(t, srv, admin, http.MethodDelete, "/api/v1/incoming/"+h.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting again: %d", rec.Code)
	}
}

func TestIncomingHookAddressesIgnoreSessionsAndTokens(t *testing.T) {
	s := newIncomingShop(t)
	oak := s.board(t, s.short.ID, "Oak")
	h, secret := s.hook(t, "Scale", models.IncomingBearer, models.IncomingQuantity,
		models.IncomingSettings{Asset: models.IncomingAsset{By: models.IncomingAssetFixed, AssetID: oak.Asset.ID}})
	// An admin's session alone is no secret.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incoming/"+h.ID, strings.NewReader(`{"adjust": -1}`))
	req.AddCookie(sessionCookie(t, s.srv, s.admin.ID))
	rec := httptest.NewRecorder()
	s.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a session: %d %s", rec.Code, rec.Body)
	}
	// Nor is an API token, even the admin's.
	tok, tokSecret, err := s.srv.store.CreateToken("Admin's", []string{s.acts.ID}, nil, s.admin.ID)
	if err != nil || tok == nil {
		t.Fatal(err)
	}
	if rec := sendIncoming(t, s.srv, h.ID, `{"adjust": -1}`, bearer(tokSecret)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a token: %d %s", rec.Code, rec.Body)
	}
	// And the hook's secret is no API token.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	rec = httptest.NewRecorder()
	s.srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("its secret as a token: %d", rec.Code)
	}
}

func TestIncomingLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newIncomingLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if wait := l.allow("a", 3); wait != 0 {
			t.Fatalf("request %d: wait %v", i, wait)
		}
		now = now.Add(10 * time.Second)
	}
	if wait := l.allow("a", 3); wait != 30*time.Second {
		t.Errorf("a fourth: wait %v", wait)
	}
	if wait := l.allow("b", 3); wait != 0 {
		t.Errorf("another hook: wait %v", wait)
	}
	now = now.Add(30 * time.Second)
	if wait := l.allow("a", 3); wait != 0 {
		t.Errorf("a minute after the first: wait %v", wait)
	}
}

func TestLookupPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	doc, err := incomingPayload(r, []byte(`{"a": {"b": [{"c": "x"}, 2]}, "n": 1.5, "t": true}`))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"a.b.0.c": "x", "$.a.b.0.c": "x", "n": "1.5", "t": "true"} {
		v, ok := lookupPath(doc, path)
		text, isText := scalarText(v)
		if !ok || !isText || text != want {
			t.Errorf("%s: %v %v", path, v, ok)
		}
	}
	for _, path := range []string{"a.b.2", "a.x", "n.x", "a.b.-1"} {
		if v, ok := lookupPath(doc, path); ok {
			t.Errorf("%s: found %v", path, v)
		}
	}
	if v, _ := lookupPath(doc, "a"); v != nil {
		if _, ok := scalarText(v); ok {
			t.Errorf("an object read as text")
		}
	}
	for _, bad := range []string{"", " ", "a..b", "a.", strings.Repeat("a", 256)} {
		if checkIncomingPath(bad) == nil {
			t.Errorf("%q passed", bad)
		}
	}
}
