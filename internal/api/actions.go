package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Actions on assets (README §5.4): categories.manage defines them and
// chooses which categories offer them; assets.act presses them.

// ActionRequest creates or replaces an action.
type ActionRequest struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Confirm string `json:"confirm"`
}

// handleListActions: GET /api/v1/actions
func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.store.ListActions()
	if err != nil {
		writeServerError(w, err, "list actions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

// handleSaveAction: POST /api/v1/actions and PUT /api/v1/actions/{id}
func (s *Server) handleSaveAction(w http.ResponseWriter, r *http.Request) {
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	a, err := s.store.SaveAction(id, store.ActionInput{Key: req.Key, Label: req.Label, Confirm: req.Confirm})
	if err != nil {
		writeActionError(w, err, "save the action")
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"action": a})
}

// handleDeleteAction: DELETE /api/v1/actions/{id}
func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAction(r.PathValue("id")); err != nil {
		writeActionError(w, err, "delete the action")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Action deleted"})
}

// CategoryActionsRequest replaces the actions a category offers.
type CategoryActionsRequest struct {
	Actions []models.CategoryAction `json:"actions"`
}

// handleCategoryActions: GET /api/v1/categories/{id}/actions
func (s *Server) handleCategoryActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.store.CategoryActions(r.PathValue("id"))
	if err != nil {
		writeActionError(w, err, "read the category's actions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

// handleSetCategoryActions: PUT /api/v1/categories/{id}/actions
func (s *Server) handleSetCategoryActions(w http.ResponseWriter, r *http.Request) {
	var req CategoryActionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	actions, err := s.store.SetCategoryActions(r.PathValue("id"), req.Actions)
	if err != nil {
		writeActionError(w, err, "set the category's actions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

// handlePressAction presses an action on an asset, recording an
// asset.action event: POST /api/v1/assets/{id}/actions/{key}
func (s *Server) handlePressAction(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.PathValue("key")
	a, ok := s.assetFor(w, r, id, rbac.PermAssetsAct)
	if !ok {
		return
	}
	// Only a press that can be taken counts against the limit.
	offered := false
	for _, o := range a.Actions {
		offered = offered || o.Key == key
	}
	if !offered {
		writeActionError(w, store.ErrActionNotOffered, "press the action")
		return
	}
	if wait := s.presses.allow(actorOf(r), id); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
		writeJSONError(w, http.StatusTooManyRequests, "That was pressed a moment ago; wait a little before pressing it again")
		return
	}
	eventID, err := s.actingStore(r).RecordAssetAction(id, key)
	if err != nil {
		writeActionError(w, err, "press the action")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event_id": eventID})
}

// writeActionError answers a failure to read, change or press actions.
func writeActionError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrActionNotFound):
		writeJSONError(w, http.StatusNotFound, "Action not found")
	case errors.Is(err, store.ErrActionNotOffered):
		writeJSONError(w, http.StatusNotFound, "That action is not offered on this asset")
	case errors.Is(err, store.ErrActionKeyExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalidAction):
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

// pressLimiter keeps one person from pressing actions too often: on one
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
