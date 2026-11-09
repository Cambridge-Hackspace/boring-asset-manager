package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/cron"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Hooks on assets (docs/hooks.md): categories.manage defines them and
// chooses which categories offer them; assets.hook triggers them.

// HookRequest creates or replaces a hook. Triggers left out keep a hook's
// triggers when it is replaced, and make a new one a button.
type HookRequest struct {
	Key      string                `json:"key"`
	Label    string                `json:"label"`
	Confirm  string                `json:"confirm"`
	Triggers *[]models.HookTrigger `json:"triggers"`
}

// hookEventKinds are the events about an asset an event trigger can fire
// on: every kind but a hook's own, so that hooks never fire each other,
// and archiving, after which no hook fires on the asset.
var hookEventKinds = map[string]bool{
	models.EventAssetCreated: true, models.EventAssetEdited: true, models.EventAssetRestored: true,
	models.EventQuantityChanged: true, models.EventAssetRunningLow: true, models.EventAssetRestocked: true,
	models.EventAssetConditionChanged: true, models.EventAssetCheckedOut: true, models.EventAssetCheckedIn: true,
	models.EventAssetOverdue: true, models.EventFlowOverdue: true, models.EventAssetLabelPrinted: true,
	models.EventAssetReviewDue: true,
}

// maxHookInterval is the longest least interval between an event
// trigger's firings: a week.
const maxHookInterval = 7 * 24 * 60 * 60

// checkTriggers checks a hook's triggers (docs/hooks.md), saying what is
// wrong with the first that is.
func (s *Server) checkTriggers(triggers []models.HookTrigger) error {
	buttons := 0
	for i, t := range triggers {
		which := "trigger " + strconv.Itoa(i+1)
		switch t.Type {
		case models.HookTriggerButton:
			if buttons++; buttons > 1 {
				return errors.New(which + ": a hook has one button at most")
			}
			if len(t.Kinds) > 0 || t.Condition != nil || t.IntervalSeconds != 0 || t.Cron != "" || t.TimeZone != "" || len(t.Filter) > 0 {
				return errors.New(which + ": a button has no kinds, condition, interval or schedule")
			}
		case models.HookTriggerEvent:
			if len(t.Kinds) == 0 {
				return errors.New(which + ": choose the events that fire it")
			}
			for _, k := range t.Kinds {
				if !hookEventKinds[k] {
					return fmt.Errorf("%s: %q is not an event about an asset a hook can fire on", which, k)
				}
			}
			if t.IntervalSeconds < 0 || t.IntervalSeconds > maxHookInterval {
				return fmt.Errorf("%s: the interval is 0 to %d seconds", which, maxHookInterval)
			}
			if c := t.Condition; c != nil && c.Field != hookConditionField {
				if _, err := s.store.GetFieldByKey(c.Field); err != nil {
					return fmt.Errorf("%s: the condition's field %q is neither a field's key nor %q", which, c.Field, hookConditionField)
				}
			}
			if t.Cron != "" || t.TimeZone != "" || len(t.Filter) > 0 {
				return errors.New(which + ": an event trigger has no schedule or filter")
			}
		case models.HookTriggerSchedule:
			if _, err := cron.Parse(t.Cron); err != nil {
				return fmt.Errorf("%s: %v", which, err)
			}
			if _, err := time.LoadLocation(t.TimeZone); err != nil {
				return fmt.Errorf("%s: %q is not a time zone, such as Europe/London", which, t.TimeZone)
			}
			if _, err := hookFilter(t.Filter); err != nil {
				return fmt.Errorf("%s: the filter: %v", which, err)
			}
			if len(t.Kinds) > 0 || t.Condition != nil || t.IntervalSeconds != 0 {
				return errors.New(which + ": a schedule has no kinds, condition or interval")
			}
		default:
			return fmt.Errorf("%s: the type is button, event or schedule, not %q", which, t.Type)
		}
	}
	return nil
}

// hookConditionField is the condition an event trigger's condition can
// name besides a field's key: the asset's condition.
const hookConditionField = "condition"

// hookFilter reads a schedule's filter, a report's (docs/reports.md),
// refusing what a report's filter would not take.
func hookFilter(raw json.RawMessage) (reports.Filter, error) {
	var f reports.Filter
	if len(raw) == 0 || string(raw) == "null" {
		return f, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, err
	}
	if f.Archived {
		return f, errors.New("archived assets are never fired on")
	}
	switch f.Loan {
	case "", store.LoansOpen, store.LoansOverdue:
	default:
		return f, fmt.Errorf("loan is open or overdue, not %q", f.Loan)
	}
	return f, nil
}

// handleListHooks: GET /api/v1/hooks
func (s *Server) handleListHooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.store.ListHooks()
	if err != nil {
		writeServerError(w, err, "list hooks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hooks": hooks})
}

// handleSaveHook: POST /api/v1/hooks and PUT /api/v1/hooks/{id}
func (s *Server) handleSaveHook(w http.ResponseWriter, r *http.Request) {
	var req HookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	in := store.HookInput{Key: req.Key, Label: req.Label, Confirm: req.Confirm}
	switch {
	case req.Triggers != nil:
		in.Triggers = *req.Triggers
		if len(in.Triggers) == 0 {
			writeJSONError(w, http.StatusBadRequest, "A hook needs a trigger: a button, events or a schedule")
			return
		}
		if err := s.checkTriggers(in.Triggers); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	case id != "":
		// Left out, a hook keeps its triggers.
		was, err := s.store.GetHook(id)
		if err != nil {
			writeHookError(w, err, "save the hook")
			return
		}
		in.Triggers = was.Triggers
	}
	a, err := s.store.SaveHook(id, in)
	if err != nil {
		writeHookError(w, err, "save the hook")
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"hook": a})
}

// handleDeleteHook: DELETE /api/v1/hooks/{id}
func (s *Server) handleDeleteHook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteHook(r.PathValue("id")); err != nil {
		writeHookError(w, err, "delete the hook")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Hook deleted"})
}

// CategoryHooksRequest replaces the hooks a category offers.
type CategoryHooksRequest struct {
	Hooks []models.CategoryHook `json:"hooks"`
}

// handleCategoryHooks: GET /api/v1/categories/{id}/hooks
func (s *Server) handleCategoryHooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.store.CategoryHooks(r.PathValue("id"))
	if err != nil {
		writeHookError(w, err, "read the category's hooks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hooks": hooks})
}

// handleSetCategoryHooks: PUT /api/v1/categories/{id}/hooks
func (s *Server) handleSetCategoryHooks(w http.ResponseWriter, r *http.Request) {
	var req CategoryHooksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	hooks, err := s.store.SetCategoryHooks(r.PathValue("id"), req.Hooks)
	if err != nil {
		writeHookError(w, err, "set the category's hooks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hooks": hooks})
}

// handleTriggerHook triggers a hook on an asset, recording an
// asset.hook event: POST /api/v1/assets/{id}/hooks/{key}
func (s *Server) handleTriggerHook(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.PathValue("key")
	a, ok := s.assetFor(w, r, id, rbac.PermAssetsHook)
	if !ok {
		return
	}
	// Only a press that can be taken counts against the limit.
	offered := false
	for _, o := range a.Hooks {
		offered = offered || o.Key == key
	}
	if !offered {
		writeHookError(w, store.ErrHookNotOffered, "trigger the hook")
		return
	}
	if wait := s.presses.allow(actorOf(r), id); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeJSONError(w, http.StatusTooManyRequests, "That was pressed a moment ago; wait a little before pressing it again")
		return
	}
	eventID, err := s.actingStore(r).RecordAssetHook(id, key)
	if err != nil {
		writeHookError(w, err, "trigger the hook")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event_id": eventID})
}

// writeHookError answers a failure to read, change or trigger hooks.
func writeHookError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrHookNotFound):
		writeJSONError(w, http.StatusNotFound, "Hook not found")
	case errors.Is(err, store.ErrHookNotOffered):
		writeJSONError(w, http.StatusNotFound, "That hook is not offered on this asset")
	case errors.Is(err, store.ErrHookKeyExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalidHook):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, "The asset is archived")
	default:
		writeServerError(w, err, action)
	}
}

// pressLimiter keeps one person from pressing hooks too often: on one
// asset at most once every pressGap, and at most pressBurst presses in any
// pressWindow. It is kept by each server, so several servers each allow
// as much.
type pressLimiter struct {
	mu     sync.Mutex
	last   map[string]time.Time   // person and asset to their last press
	recent map[string][]time.Time // person to their presses in the window
	now    func() time.Time
}

const (
	pressGap    = 2 * time.Second
	pressWindow = time.Minute
	pressBurst  = 30
)

func newPressLimiter() *pressLimiter {
	return &pressLimiter{last: map[string]time.Time{}, recent: map[string][]time.Time{}, now: time.Now}
}

// allow records a press by a person on an asset, or says how long to wait
// before the next one.
func (l *pressLimiter) allow(person, asset string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	key := person + "\x00" + asset
	if last, ok := l.last[key]; ok && now.Sub(last) < pressGap {
		return pressGap - now.Sub(last)
	}
	kept := l.recent[person][:0]
	for _, t := range l.recent[person] {
		if now.Sub(t) < pressWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= pressBurst {
		l.recent[person] = kept
		return pressWindow - now.Sub(kept[0])
	}
	l.recent[person] = append(kept, now)
	l.last[key] = now
	// Forget presses long past, so the maps do not grow without bound.
	if len(l.last) > 10000 {
		for k, t := range l.last {
			if now.Sub(t) >= pressGap {
				delete(l.last, k)
			}
		}
	}
	return 0
}
