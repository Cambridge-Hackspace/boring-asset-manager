package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// eventsWindow is how far back GET /api/v1/events looks without ?after=.
const eventsWindow = time.Hour

// handleListEvents lists the events the requester would be sent live (README
// §5.4), oldest first, for those who poll rather than hold a WebSocket open,
// such as an assistant's subscription: GET /api/v1/events, with ?after= an
// event's ID (those recorded since it; the last hour's otherwise), ?kinds=,
// ?actions= and ?categories= as a live connection takes them, and ?limit=
// (100 unless given, at most 500).
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeJSONError(w, http.StatusBadRequest, "limit must be from 1 to 500")
			return
		}
		limit = n
	}
	since := time.Now().Add(-eventsWindow)
	after := q.Get("after")
	if after != "" {
		at, err := s.store.EventTime(after)
		if errors.Is(err, store.ErrEventNotFound) {
			writeJSONError(w, http.StatusBadRequest, "after must name an event")
			return
		}
		if err != nil {
			writeServerError(w, err, "read the event")
			return
		}
		since = at
	}
	filter := liveFilterOf(q)
	// Read more than asked, since some are not the requester's to see.
	events, err := s.store.EventsSince(since, 1000)
	if err != nil {
		writeServerError(w, err, "list events")
		return
	}
	// Those recorded at the same moment as after, and after itself, come
	// before it in the order events are read; skip up to it.
	if after != "" {
		for i, e := range events {
			if e.ID == after {
				events = events[i+1:]
				break
			}
		}
	}
	categories, scopes, err := s.store.EventPlaces(events)
	if err != nil {
		writeServerError(w, err, "list events")
		return
	}
	out := []models.Event{}
	more := false
	for _, e := range events {
		place := ""
		switch {
		case e.AssetID != nil:
			place = *e.AssetID
		case e.CategoryID != nil:
			place = *e.CategoryID
		default:
			continue
		}
		if !filter.matches(e, categories[place]) || !Allows(r.Context(), rbac.PermEventsRead, scopes[place]) {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		out = append(out, redactEvent(e, Allows(r.Context(), rbac.PermAssetsTrack, scopes[place])))
	}
	// A full read may have stopped short of the newest.
	if len(events) == 1000 {
		more = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "more": more})
}

// EventReplyRequest answers an action event, as a device does.
type EventReplyRequest struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// handleReplyToEvent answers an action pressed on an asset the requester
// receives events about, once: POST /api/v1/events/{id}/reply. It is how a
// device that polls, rather than holding a live connection, answers.
func (s *Server) handleReplyToEvent(w http.ResponseWriter, r *http.Request) {
	var req EventReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id := r.PathValue("id")
	assetID, err := s.store.EventAsset(id)
	if err == nil {
		var a *models.AssetWithDetails
		a, err = s.store.GetAssetByID(assetID)
		if err == nil && !Allows(r.Context(), rbac.PermEventsRead, a.Scopes) {
			err = store.ErrEventNotFound
		}
	}
	if errors.Is(err, store.ErrEventNotFound) || errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Event not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the event")
		return
	}
	replies, err := s.store.ActionReplies(id)
	if err != nil {
		writeServerError(w, err, "read the answers")
		return
	}
	who := actorOf(r)
	for _, prior := range replies {
		if prior.ReplierID == who {
			writeJSONError(w, http.StatusConflict, "You have answered this action already")
			return
		}
	}
	reply, err := s.actingStore(r).RecordActionReply(id, req.OK, req.Message)
	switch {
	case errors.Is(err, store.ErrNotAnAction):
		writeJSONError(w, http.StatusConflict, "Only an action can be answered")
	case err != nil:
		writeServerError(w, err, "keep the answer")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"reply": reply})
	}
}
