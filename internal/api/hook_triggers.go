package api

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/cron"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Hooks fired by events and schedules (#194, docs/hooks.md). Every server
// runs a round every hookTriggerPoll; what a round acts on is claimed in
// the database first, so of several servers sharing it one acts on each
// event and each run of a schedule.
const (
	hookTriggerPoll = 10 * time.Second
	// hookEventsLookBack is how far back a round reads events: those
	// recorded longer ago, as while no server ran, are not acted on.
	hookEventsLookBack = 10 * time.Minute
	// maxScheduledAssets caps the assets one run of a schedule fires on.
	maxScheduledAssets = 500
)

// RunHookTriggers fires hooks on events and schedules until ctx ends.
func (s *Server) RunHookTriggers(ctx context.Context) {
	ticker := time.NewTicker(hookTriggerPoll)
	defer ticker.Stop()
	for {
		s.hookTriggerRound(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// hookTriggerRound fires what is due at now, and returns how many hooks it
// fired.
func (s *Server) hookTriggerRound(now time.Time) int {
	hooks, err := s.store.ListHooks()
	if err != nil {
		log.Printf("hook triggers: %v", err)
		return 0
	}
	kinds := map[string]bool{}
	fired := 0
	for _, h := range hooks {
		for i, t := range h.Triggers {
			switch t.Type {
			case models.HookTriggerEvent:
				for _, k := range t.Kinds {
					kinds[k] = true
				}
			case models.HookTriggerSchedule:
				fired += s.runHookSchedule(h, i, t, now)
			}
		}
	}
	if len(kinds) > 0 {
		fired += s.fireOnEvents(kinds, now)
	}
	if err := s.store.PruneHookEvents(now.Add(-2 * hookEventsLookBack)); err != nil {
		log.Printf("hook triggers: %v", err)
	}
	return fired
}

// fireOnEvents fires the hooks whose event triggers match the events of
// kinds recorded lately, each event claimed first.
func (s *Server) fireOnEvents(kinds map[string]bool, now time.Time) int {
	events, err := s.store.EventsSince(now.Add(-hookEventsLookBack), 1000)
	if err != nil {
		log.Printf("hook triggers: %v", err)
		return 0
	}
	var offered func([]string) []models.AssetHook
	fired := 0
	for _, e := range events {
		if e.AssetID == nil || !kinds[e.Kind] {
			continue
		}
		claimed, err := s.store.ClaimHookEvent(e.ID, now)
		if err != nil {
			log.Printf("hook triggers: %v", err)
			continue
		}
		if !claimed {
			continue
		}
		a, err := s.store.GetAssetByID(*e.AssetID)
		if err != nil || a.Asset.ArchivedAt != nil {
			continue
		}
		if offered == nil {
			if offered, err = s.store.HooksOffered(); err != nil {
				log.Printf("hook triggers: %v", err)
				return fired
			}
		}
		for _, h := range offered(categoryIDsOf(a)) {
			// A hook saved after the event does not act on it.
			if e.CreatedAt.Before(h.UpdatedAt) {
				continue
			}
			for _, t := range h.Triggers {
				if t.Type != models.HookTriggerEvent || !contains(t.Kinds, e.Kind) || !conditionHolds(t.Condition, a) {
					continue
				}
				if t.IntervalSeconds > 0 {
					ok, err := s.store.ClaimHookFiring(h.ID, a.Asset.ID, time.Duration(t.IntervalSeconds)*time.Second, now)
					if err != nil {
						log.Printf("hook triggers: %v", err)
						continue
					}
					if !ok {
						continue
					}
				}
				if _, err := s.store.RecordTriggeredHook(a.Asset.ID, h, models.HookTriggerEvent, &models.EventCause{ID: e.ID, Kind: e.Kind}); err != nil {
					log.Printf("hook triggers: firing %s on %s: %v", h.Key, a.Asset.ID, err)
					continue
				}
				fired++
				// One firing per hook for an event, whichever trigger matched.
				break
			}
		}
	}
	return fired
}

// runHookSchedule runs a hook's index-th trigger, a schedule, if it fell
// due since it last ran, firing the hook on every asset its filter finds
// that the hook is offered on. A schedule that never ran starts now,
// without catching up.
func (s *Server) runHookSchedule(h models.AssetHook, index int, t models.HookTrigger, now time.Time) int {
	sched, err := cron.Parse(t.Cron)
	if err != nil {
		return 0
	}
	loc, err := time.LoadLocation(t.TimeZone)
	if err != nil {
		return 0
	}
	last, ran, err := s.store.HookScheduleRan(h.ID, index)
	if err != nil {
		log.Printf("hook triggers: %v", err)
		return 0
	}
	if !ran {
		if _, err := s.store.ClaimHookSchedule(h.ID, index, time.Time{}, now); err != nil {
			log.Printf("hook triggers: %v", err)
		}
		return 0
	}
	due, ok := sched.Latest(last, now, loc)
	if !ok {
		return 0
	}
	claimed, err := s.store.ClaimHookSchedule(h.ID, index, last, due)
	if err != nil || !claimed {
		if err != nil {
			log.Printf("hook triggers: %v", err)
		}
		return 0
	}
	assets, err := s.scheduledAssets(h, t)
	if err != nil {
		log.Printf("hook triggers: running %s's schedule: %v", h.Key, err)
		return 0
	}
	fired := 0
	for _, a := range assets {
		_, err := s.store.RecordTriggeredHook(a.Asset.ID, h, models.HookTriggerSchedule, nil)
		if errors.Is(err, store.ErrAssetArchived) {
			continue
		}
		if err != nil {
			log.Printf("hook triggers: firing %s on %s: %v", h.Key, a.Asset.ID, err)
			continue
		}
		fired++
	}
	return fired
}

// scheduledAssets are the active assets a schedule's filter finds that the
// hook is offered on, at most maxScheduledAssets.
func (s *Server) scheduledAssets(h models.AssetHook, t models.HookTrigger) ([]models.AssetWithDetails, error) {
	f, err := hookFilter(t.Filter)
	if err != nil {
		return nil, err
	}
	assets, err := s.store.FilterAssets(store.AssetFilter{Condition: f.Condition, Loan: f.Loan})
	if err != nil {
		return nil, err
	}
	categories, err := s.store.ListCategories()
	if err != nil {
		return nil, err
	}
	offered, err := s.store.HooksOffered()
	if err != nil {
		return nil, err
	}
	var out []models.AssetWithDetails
	for i := range assets {
		a := &assets[i]
		if !f.Matches(a, categories) || (f.Search != "" && !store.MatchesSearch(a, f.Search)) || !offers(offered(categoryIDsOf(a)), h.ID) {
			continue
		}
		if len(out) == maxScheduledAssets {
			log.Printf("hook triggers: %s's schedule finds more than %d assets; firing on the first %d", h.Key, maxScheduledAssets, maxScheduledAssets)
			break
		}
		out = append(out, *a)
	}
	return out, nil
}

// conditionHolds reports whether an event trigger's condition, if any,
// holds of a.
func conditionHolds(c *models.HookCondition, a *models.AssetWithDetails) bool {
	if c == nil {
		return true
	}
	if c.Field == hookConditionField {
		return a.Asset.Condition == c.Equals
	}
	return a.FieldValues[c.Field] == c.Equals
}

func categoryIDsOf(a *models.AssetWithDetails) []string {
	ids := make([]string, len(a.Categories))
	for i, c := range a.Categories {
		ids[i] = c.ID
	}
	return ids
}

func offers(hooks []models.AssetHook, id string) bool {
	for _, h := range hooks {
		if h.ID == id {
			return true
		}
	}
	return false
}
