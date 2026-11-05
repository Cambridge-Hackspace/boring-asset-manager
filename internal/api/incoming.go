package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Incoming hooks (docs/hooks.md#incoming-hooks): URLs that let something
// outside BAM! act on it without an API client, such as a button on a
// cabinet, a Home Assistant automation, a form on another site or a scale.
// settings.manage makes them. A request to one is authenticated by its
// secret, then does what the hook does by asking the API's own handlers,
// in process, as the hook: its roles, narrowed to their scopes. So it is
// held to every rule they keep, and its changes are recorded in history as
// the hook's.

const (
	// incomingPrefix starts each incoming hook's address.
	incomingPrefix = "/api/v1/incoming/"
	// incomingBodyLimit caps what is sent to one.
	incomingBodyLimit = 64 << 10
	// incomingWindow is how far a signed request's timestamp may be from
	// this server's time.
	incomingWindow = 5 * time.Minute
	// maxIncomingPath is the longest path into a request's JSON.
	maxIncomingPath = 255
)

// incomingDelivery reports whether r is a request to an incoming hook's
// address, from outside: POST /api/v1/incoming/{id}.
func incomingDelivery(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	id, ok := strings.CutPrefix(r.URL.Path, incomingPrefix)
	return ok && id != "" && !strings.Contains(id, "/")
}

// incomingCaller is an incoming hook acting, and the events it records.
type incomingCaller struct {
	hook *models.IncomingHook
	log  *store.EventLog
}

type incomingKey struct{}

// currentIncoming is the incoming hook a request acts as, or nil.
func currentIncoming(ctx context.Context) *incomingCaller {
	c, _ := ctx.Value(incomingKey{}).(*incomingCaller)
	return c
}

// IncomingRequest makes or changes an incoming hook. A change replaces
// everything; Enabled left out keeps it as it was, and a new hook on.
type IncomingRequest struct {
	Name      string                  `json:"name"`
	Enabled   *bool                   `json:"enabled"`
	Auth      string                  `json:"auth"`
	Action    string                  `json:"action"`
	Hook      string                  `json:"hook"`
	FlowID    string                  `json:"flow_id"`
	Fields    map[string]string       `json:"fields"`
	Asset     models.IncomingAsset    `json:"asset"`
	RateLimit int                     `json:"rate_limit"`
	Roles     []models.RoleAssignment `json:"roles"`
	// NewSecret replaces the secret; the old one stops working at once.
	NewSecret bool `json:"new_secret"`
}

// IncomingView is an incoming hook with its address.
type IncomingView struct {
	models.IncomingHook
	URL string `json:"url"`
}

func (s *Server) incomingView(r *http.Request, h *models.IncomingHook) IncomingView {
	return IncomingView{IncomingHook: *h, URL: s.baseURL(r) + incomingPrefix + h.ID}
}

// baseURL is this server's address: its configured base URL, or the one
// the request came to.
func (s *Server) baseURL(r *http.Request) string {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base
}

// handleListIncoming: GET /api/v1/incoming
func (s *Server) handleListIncoming(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.store.ListIncomingHooks()
	if err != nil {
		writeServerError(w, err, "list incoming hooks")
		return
	}
	views := make([]IncomingView, 0, len(hooks))
	for i := range hooks {
		views = append(views, s.incomingView(r, &hooks[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"incoming_hooks": views})
}

// handleGetIncoming: GET /api/v1/incoming/{id}
func (s *Server) handleGetIncoming(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetIncomingHook(r.PathValue("id"))
	if err != nil {
		writeIncomingError(w, err, "read the incoming hook")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incoming_hook": s.incomingView(r, h)})
}

// handleSaveIncoming: POST /api/v1/incoming and PUT /api/v1/incoming/{id}.
// The secret is in the answer when it is new, the only time it is shown.
func (s *Server) handleSaveIncoming(w http.ResponseWriter, r *http.Request) {
	var req IncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	var was *models.IncomingHook
	if id != "" {
		var err error
		if was, err = s.store.GetIncomingHook(id); err != nil {
			writeIncomingError(w, err, "save the incoming hook")
			return
		}
	}
	in, status, err := s.incomingInput(r, req, was)
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}
	var (
		h      *models.IncomingHook
		secret string
	)
	if id == "" {
		by := ""
		if u := CurrentUser(r.Context()); u != nil {
			by = u.ID
		}
		h, secret, err = s.store.CreateIncomingHook(in, by)
		status = http.StatusCreated
	} else {
		h, secret, err = s.store.UpdateIncomingHook(id, in, req.NewSecret)
		status = http.StatusOK
	}
	if err != nil {
		writeIncomingError(w, err, "save the incoming hook")
		return
	}
	out := map[string]any{"incoming_hook": s.incomingView(r, h)}
	if secret != "" {
		out["secret"] = secret
	}
	writeJSON(w, status, out)
}

// handleDeleteIncoming: DELETE /api/v1/incoming/{id}
func (s *Server) handleDeleteIncoming(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteIncomingHook(r.PathValue("id")); err != nil {
		writeIncomingError(w, err, "delete the incoming hook")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Incoming hook deleted"})
}

func writeIncomingError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrIncomingNotFound):
		writeJSONError(w, http.StatusNotFound, "Incoming hook not found")
	case errors.Is(err, store.ErrInvalidIncoming), errors.Is(err, store.ErrRoleNotFound),
		errors.Is(err, store.ErrScopeNotFound), errors.Is(err, store.ErrInvalidAssignment):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// incomingPerm is the permission an action needs on the asset.
func incomingPerm(action string) string {
	switch action {
	case models.IncomingTriggerHook:
		return rbac.PermAssetsHook
	case models.IncomingQuantity:
		return rbac.PermStockRecord
	case models.IncomingFields:
		return rbac.PermAssetsWrite
	}
	return rbac.PermFlowsRun
}

// incomingInput checks a request to save an incoming hook, saying what is
// wrong with it and with which status.
func (s *Server) incomingInput(r *http.Request, req IncomingRequest, was *models.IncomingHook) (store.IncomingInput, int, error) {
	bad := func(format string, args ...any) (store.IncomingInput, int, error) {
		return store.IncomingInput{}, http.StatusBadRequest, fmt.Errorf(format, args...)
	}
	in := store.IncomingInput{Name: req.Name, Enabled: true, Auth: req.Auth, Action: req.Action, RateLimit: req.RateLimit, Roles: req.Roles}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	} else if was != nil {
		in.Enabled = was.Enabled
	}
	if in.Auth == "" {
		in.Auth = models.IncomingBearer
	}
	// Settings for its action only, so nothing unused is kept.
	set := models.IncomingSettings{}
	switch req.Action {
	case models.IncomingTriggerHook:
		hooks, err := s.store.ListHooks()
		if err != nil {
			return store.IncomingInput{}, http.StatusInternalServerError, errors.New("could not read the hooks")
		}
		found := false
		for _, h := range hooks {
			found = found || h.Key == req.Hook
		}
		if !found {
			return bad("Choose the hook to trigger: %q is no hook's key", req.Hook)
		}
		set.Hook = req.Hook
	case models.IncomingFlow:
		if _, err := s.store.GetFlowByID(req.FlowID); err != nil {
			return bad("Choose the flow to run: %q is no flow", req.FlowID)
		}
		set.FlowID = req.FlowID
	case models.IncomingFields:
		for key, path := range req.Fields {
			if _, err := s.store.GetFieldByKey(key); err != nil {
				return bad("No field has the key %q", key)
			}
			if err := checkIncomingPath(path); err != nil {
				return bad("The path for %s: %v", key, err)
			}
		}
		if len(req.Fields) > 0 {
			set.Fields = req.Fields
		}
	case models.IncomingQuantity:
	default:
		return bad("The action is hook, quantity, fields or flow, not %q", req.Action)
	}
	a := req.Asset
	switch a.By {
	case models.IncomingAssetFixed:
		if _, err := s.store.GetAssetByID(a.AssetID); err != nil {
			return bad("The asset to act on: %q is no asset", a.AssetID)
		}
		set.Asset = models.IncomingAsset{By: a.By, AssetID: a.AssetID}
	case models.IncomingAssetID, models.IncomingAssetCode:
		if err := checkIncomingPath(a.Path); err != nil {
			return bad("Where the asset's %s is in the request: %v", a.By, err)
		}
		set.Asset = models.IncomingAsset{By: a.By, Path: a.Path}
	case models.IncomingAssetField:
		if _, err := s.store.GetFieldByKey(a.Field); err != nil {
			return bad("Finding the asset by a field: no field has the key %q", a.Field)
		}
		if err := checkIncomingPath(a.Path); err != nil {
			return bad("Where the field's value is in the request: %v", err)
		}
		set.Asset = models.IncomingAsset{By: a.By, Field: a.Field, Path: a.Path}
	case "":
		if req.Action != models.IncomingFlow {
			return bad("Say how the asset is found: fixed, or by its id, code or a field from the request")
		}
	default:
		return bad("The asset is found fixed, or by id, code or field, not %q", a.By)
	}
	in.Settings = set

	// It may do no more than whoever makes it.
	keys, err := s.store.AssignmentKeys(req.Roles)
	if errors.Is(err, store.ErrScopeNotFound) {
		return bad("%v", err)
	}
	if err != nil {
		return store.IncomingInput{}, http.StatusInternalServerError, errors.New("could not read the roles' permissions")
	}
	if missing := ungrantable(r, keys); len(missing) > 0 {
		return store.IncomingInput{}, http.StatusForbidden, fmt.Errorf("An incoming hook can do no more than whoever saves it, and you do not hold %s", strings.Join(missing, ", "))
	}

	if in.Auth == models.IncomingHMAC {
		if !s.sealer.HasKey() {
			return store.IncomingInput{}, http.StatusConflict, errors.New("a signing secret is kept sealed with [auth] secret_key, and the configuration has none: ask whoever runs BAM! to add one, or use a bearer secret")
		}
		in.Seal = func(id, secret string) (string, error) {
			return s.sealer.Seal([]byte(secret), store.IncomingPurpose(id))
		}
	}
	return in, 0, nil
}

// ungrantable lists the permission keys the request does not hold: a key
// for every scope needs it in every scope, and scope:key in that scope.
func ungrantable(r *http.Request, keys map[string]bool) []string {
	held := UserPermissions(r.Context())
	var out []string
	for k := range keys {
		scope, perm := rbac.SplitKey(k)
		if held[k] || held[perm] || (scope != "" && Allows(r.Context(), perm, []string{scope})) {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkIncomingPath checks a path into a request's JSON: keys and indexes
// separated by dots, optionally after $., such as data.code or items.0.id.
func checkIncomingPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("give a path into the request's JSON, such as data.code")
	}
	if len(path) > maxIncomingPath || textcheck.Problem(path, maxIncomingPath) != "" {
		return fmt.Errorf("a path is up to %d characters of text", maxIncomingPath)
	}
	for _, part := range incomingPathParts(path) {
		if part == "" {
			return fmt.Errorf("%q has an empty part", path)
		}
	}
	return nil
}

func incomingPathParts(path string) []string {
	path = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$"), ".")
	return strings.Split(path, ".")
}

// lookupPath finds what is at a path in a request's JSON.
func lookupPath(doc any, path string) (any, bool) {
	at := doc
	for _, part := range incomingPathParts(path) {
		switch v := at.(type) {
		case map[string]any:
			next, ok := v[part]
			if !ok {
				return nil, false
			}
			at = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			at = v[i]
		default:
			return nil, false
		}
	}
	return at, true
}

// scalarText is a JSON value as text: a string, a number or true or false.
func scalarText(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// incomingLimiter keeps each incoming hook to its requests a minute. It is
// kept by each server, so several servers each allow as much.
type incomingLimiter struct {
	mu     sync.Mutex
	recent map[string][]time.Time
	now    func() time.Time
}

func newIncomingLimiter() *incomingLimiter {
	return &incomingLimiter{recent: map[string][]time.Time{}, now: time.Now}
}

// allow records a request to a hook, or says how long to wait before the
// next.
func (l *incomingLimiter) allow(id string, perMinute int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	kept := l.recent[id][:0]
	for _, t := range l.recent[id] {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= perMinute {
		l.recent[id] = kept
		return time.Minute - now.Sub(kept[0])
	}
	l.recent[id] = append(kept, now)
	if len(l.recent) > 10000 {
		for k, ts := range l.recent {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= time.Minute {
				delete(l.recent, k)
			}
		}
	}
	return 0
}

// handleIncoming takes a request to an incoming hook and does what the hook
// does: POST /api/v1/incoming/{id}. It answers 202 with the IDs of the
// events recorded, or an error naming what failed, never the secret.
func (s *Server) handleIncoming(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.GetIncomingHook(r.PathValue("id"))
	if errors.Is(err, store.ErrIncomingNotFound) {
		writeJSONError(w, http.StatusNotFound, "No incoming hook has this address")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the incoming hook")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Request bodies are limited to %d bytes", tooBig.Limit))
			return
		}
		writeJSONError(w, http.StatusBadRequest, "Could not read the request body")
		return
	}
	delivery, status, msg := s.authenticateIncoming(h, r, body)
	if status != 0 {
		writeJSONError(w, status, msg)
		return
	}
	if !h.Enabled {
		writeJSONError(w, http.StatusForbidden, "This incoming hook is turned off")
		return
	}
	if wait := s.incoming.allow(h.ID, h.RateLimit); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeJSONError(w, http.StatusTooManyRequests, fmt.Sprintf("This incoming hook takes %d requests a minute; wait a little", h.RateLimit))
		return
	}
	if delivery != nil {
		if err := s.store.ClaimIncomingDelivery(h.ID, delivery.id, delivery.until); errors.Is(err, store.ErrDeliveryUsed) {
			writeJSONError(w, http.StatusConflict, "This delivery was received already: give each request its own BAM-Delivery")
			return
		} else if err != nil {
			writeServerError(w, err, "record the delivery")
			return
		}
	}
	if err := s.store.TouchIncomingHook(h); err != nil {
		writeServerError(w, err, "record the incoming hook's use")
		return
	}
	doc, err := incomingPayload(r, body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	perms, err := s.store.IncomingHookKeys(h.ID)
	if err != nil {
		writeServerError(w, err, "read the incoming hook's permissions")
		return
	}
	public, err := s.store.PublicPermissions()
	if err != nil {
		writeServerError(w, err, "read the public role")
		return
	}
	events := &store.EventLog{}
	ctx := context.WithValue(r.Context(), incomingKey{}, &incomingCaller{hook: h, log: events})
	ctx = WithPermissions(ctx, union(perms, public))
	r = r.WithContext(context.WithValue(ctx, authenticatedKey{}, true))

	assetID, status, msg := s.incomingAct(r, h, doc)
	if status != 0 {
		writeJSONError(w, status, msg)
		return
	}
	ids := events.IDs()
	var first any
	if len(ids) > 0 {
		first = ids[0]
	}
	out := map[string]any{"event_id": first, "event_ids": ids}
	if assetID != "" {
		out["asset_id"] = assetID
	}
	writeJSON(w, http.StatusAccepted, out)
}

// incomingDeliveryID is a signed request's delivery ID, kept until its
// timestamp is too old to be taken anyway.
type incomingDeliveryID struct {
	id    string
	until time.Time
}

// authenticateIncoming checks a request's secret, or its signature, which
// is BAM-Signature as webhooks send it (docs/notifications.md): v1= and the
// hex HMAC-SHA256 of BAM-Timestamp, a dot and the body. It answers a
// signed request's delivery ID, or the status and why it is refused.
func (s *Server) authenticateIncoming(h *models.IncomingHook, r *http.Request, body []byte) (*incomingDeliveryID, int, string) {
	cred, err := s.store.IncomingHookCredential(h.ID)
	if err != nil {
		return nil, http.StatusInternalServerError, "Failed to read the incoming hook"
	}
	if cred.Hash == "" && cred.Sealed == "" {
		return nil, http.StatusUnauthorized, "This incoming hook has no secret, as after a restore without secrets: give it a new one"
	}
	if h.Auth == models.IncomingBearer {
		secret, ok := bearerToken(r)
		if !ok || secret == "" {
			return nil, http.StatusUnauthorized, "Send the hook's secret as Authorization: Bearer <secret>"
		}
		if !cred.MatchesBearer(secret) {
			return nil, http.StatusUnauthorized, "The secret is not this hook's"
		}
		return nil, 0, ""
	}
	ts, delivery, got := r.Header.Get("BAM-Timestamp"), strings.TrimSpace(r.Header.Get("BAM-Delivery")), r.Header.Get("BAM-Signature")
	if ts == "" || delivery == "" || got == "" {
		return nil, http.StatusUnauthorized, "A signed request carries BAM-Timestamp, BAM-Delivery and BAM-Signature"
	}
	sent, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, http.StatusUnauthorized, "BAM-Timestamp is seconds since 1970"
	}
	at := time.Unix(sent, 0)
	if d := time.Since(at); d > incomingWindow || d < -incomingWindow {
		return nil, http.StatusUnauthorized, "BAM-Timestamp is more than five minutes from this server's time"
	}
	if len(delivery) > 255 || textcheck.Problem(delivery, 255) != "" {
		return nil, http.StatusBadRequest, "BAM-Delivery is up to 255 characters of text"
	}
	secret, err := s.sealer.Open(cred.Sealed, store.IncomingPurpose(h.ID))
	if errors.Is(err, auth.ErrNoSecretKey) || errors.Is(err, auth.ErrUnsealable) {
		return nil, http.StatusServiceUnavailable, "This hook's signing secret cannot be opened with this server's [auth] secret_key: give it a new one"
	}
	if err != nil {
		return nil, http.StatusInternalServerError, "Failed to open the signing secret"
	}
	if !hmac.Equal([]byte(notify.Signature(string(secret), ts, body)), []byte(got)) {
		return nil, http.StatusUnauthorized, "BAM-Signature does not match the body"
	}
	return &incomingDeliveryID{id: delivery, until: at.Add(incomingWindow + time.Second)}, 0, ""
}

// incomingPayload reads a request's body: a JSON object, form fields, or
// nothing, as an empty object.
func incomingPayload(r *http.Request, body []byte) (map[string]any, error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(body)) == 0 {
		return doc, nil
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "application/x-www-form-urlencoded" {
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, errors.New("the form in the body could not be read")
		}
		for k, vs := range form {
			if textcheck.Problem(k, len(k)) != "" || textcheck.Problem(vs[0], len(vs[0])) != "" {
				return nil, errors.New("the form holds a NUL character or bytes that are not UTF-8")
			}
			doc[k] = vs[0]
		}
		return doc, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("the body must be a JSON object, or form fields")
	}
	return doc, nil
}

// incomingAct finds the asset and does what the hook does, as the hook. It
// answers the asset acted on, or the status and why it failed.
func (s *Server) incomingAct(r *http.Request, h *models.IncomingHook, doc map[string]any) (string, int, string) {
	assetID, status, msg := s.incomingAsset(r, h, doc)
	if status != 0 {
		return "", status, msg
	}
	path := "/api/v1/assets/" + url.PathEscape(assetID)
	switch h.Action {
	case models.IncomingTriggerHook:
		status, msg = s.incomingCall(r, "Triggering the hook", http.MethodPost, path+"/hooks/"+url.PathEscape(h.Hook), nil)
	case models.IncomingQuantity:
		body := map[string]int{}
		for _, k := range []string{"adjust", "set"} {
			v, ok := doc[k]
			if !ok {
				continue
			}
			text, _ := scalarText(v)
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				return "", http.StatusBadRequest, fmt.Sprintf("%s is a whole number", k)
			}
			body[k] = n
		}
		status, msg = s.incomingCall(r, "Recording the use or restock", http.MethodPost, path+"/quantity", body)
	case models.IncomingFields:
		values := map[string]string{}
		if len(h.Fields) > 0 {
			for key, p := range h.Fields {
				v, found := lookupPath(doc, p)
				text, ok := scalarText(v)
				if !found || !ok {
					return "", http.StatusBadRequest, fmt.Sprintf("The request has no text, number or true or false at %s, for the field %s", p, key)
				}
				values[key] = text
			}
		} else {
			given, ok := doc["field_values"].(map[string]any)
			if !ok || len(given) == 0 {
				return "", http.StatusBadRequest, "Give field_values, an object of the fields to set by key"
			}
			for key, v := range given {
				text, ok := scalarText(v)
				if !ok {
					return "", http.StatusBadRequest, fmt.Sprintf("field_values.%s is not text, a number or true or false", key)
				}
				values[key] = text
			}
		}
		status, msg = s.incomingCall(r, "Setting the fields", http.MethodPut, path, map[string]any{"field_values": values})
	case models.IncomingFlow:
		return s.incomingFlow(r, h, doc, assetID)
	}
	return assetID, status, msg
}

// incomingAsset finds the asset a request acts on, among those the hook may
// read or act on; "" for a flow that finds nothing first.
func (s *Server) incomingAsset(r *http.Request, h *models.IncomingHook, doc map[string]any) (string, int, string) {
	a := h.Asset
	if a.By == "" {
		return "", 0, ""
	}
	if a.By == models.IncomingAssetFixed {
		return a.AssetID, 0, ""
	}
	v, found := lookupPath(doc, a.Path)
	value, ok := scalarText(v)
	value = strings.TrimSpace(value)
	if !found || !ok || value == "" {
		return "", http.StatusBadRequest, fmt.Sprintf("The request has no asset %s at %s", a.By, a.Path)
	}
	var ids []string
	var err error
	switch a.By {
	case models.IncomingAssetID:
		ids = []string{value}
	case models.IncomingAssetCode:
		ids, err = s.store.AssetsByCode(value)
	case models.IncomingAssetField:
		ids, err = s.store.AssetsByFieldValue(a.Field, value)
	}
	if err != nil {
		return "", http.StatusInternalServerError, "Failed to find the asset"
	}
	perms := []string{rbac.PermAssetsRead, incomingPerm(h.Action)}
	var reached []string
	for _, id := range ids {
		asset, err := s.store.GetAssetByID(id)
		if errors.Is(err, store.ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return "", http.StatusInternalServerError, "Failed to read the asset"
		}
		if allowsAny(r, asset.Scopes, perms) {
			reached = append(reached, id)
		}
	}
	what := map[string]string{models.IncomingAssetID: "that ID", models.IncomingAssetCode: "that code", models.IncomingAssetField: a.Field + " " + strconv.Quote(value)}[a.By]
	switch len(reached) {
	case 0:
		return "", http.StatusNotFound, "No asset this hook can reach has " + what
	case 1:
		return reached[0], 0, ""
	}
	return "", http.StatusConflict, fmt.Sprintf("%d assets this hook can reach have %s", len(reached), what)
}

// incomingCall asks the API, in process, as the hook, answering a refusal
// with its status and words, after what was being done.
func (s *Server) incomingCall(r *http.Request, doing, method, path string, in any) (int, string) {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return http.StatusInternalServerError, doing + " failed"
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), method, path, bytes.NewReader(body))
	if err != nil {
		return http.StatusInternalServerError, doing + " failed"
	}
	req.RequestURI = path
	req.RemoteAddr = r.RemoteAddr
	req.Host = r.Host
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}}
	s.httpServer.Handler.ServeHTTP(rec, req)
	if rec.status == 0 || (rec.status >= 200 && rec.status <= 299) {
		return 0, ""
	}
	var e ErrorResponse
	_ = json.Unmarshal(rec.body.Bytes(), &e)
	if e.Error == "" {
		e.Error = http.StatusText(rec.status)
	}
	return rec.status, doing + ": " + e.Error
}

// incomingFlow runs the hook's flow to its end, answering it from the
// request: category_id, answers (or else the request's own fields) and
// place_code, with the asset found answering a first Find.
func (s *Server) incomingFlow(r *http.Request, h *models.IncomingHook, doc map[string]any, assetID string) (string, int, string) {
	f, err := s.store.GetFlowByID(h.FlowID)
	if errors.Is(err, store.ErrFlowNotFound) {
		return "", http.StatusNotFound, "Running the flow: its flow is gone"
	}
	if err != nil {
		return "", http.StatusInternalServerError, "Failed to read the flow"
	}
	text := func(k string) string {
		t, _ := scalarText(doc[k])
		return strings.TrimSpace(t)
	}
	in := UnattendedAnswers{CategoryID: text("category_id"), PlaceCode: text("place_code"), AssetID: assetID, Answers: map[string]string{}}
	from := doc
	if given, ok := doc["answers"].(map[string]any); ok {
		from = given
	}
	for k, v := range from {
		if t, ok := scalarText(v); ok {
			in.Answers[k] = t
		}
	}
	runner := &flowRunner{s: s, r: r, flow: f}
	st, err := runner.runUnattended(in)
	if err != nil {
		var fe *flowError
		if errors.As(err, &fe) {
			return "", fe.Status, "Running the flow: " + fe.Message
		}
		log.Printf("failed to run the flow for an incoming hook: %v", err)
		return "", http.StatusInternalServerError, "Failed to run the flow"
	}
	return st.Values[flows.ValueAssetID], 0, ""
}
