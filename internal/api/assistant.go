package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// The assistant beside the page (docs/assistant.md): its settings, which an
// admin sets and the configuration may set in their place, its guides, and
// each person's conversations, which no one else reads, admins included.

// assistantSettings are the instance's settings for the assistant, with
// those the configuration sets in their place, and which those are.
func (s *Server) assistantSettings() (store.AssistantSettings, map[string]bool, error) {
	set, err := s.store.AssistantSettings()
	if err != nil {
		return set, nil, err
	}
	c := s.cfg.Assistant
	fixed := map[string]bool{}
	if c.Enabled != nil {
		set.Enabled, fixed["enabled"] = *c.Enabled, true
	}
	if c.Provider != "" && c.Provider != set.Provider {
		// A provider given here takes its own address and model, unless
		// those are given too.
		set.Provider, set.Endpoint, set.Model = c.Provider, store.DefaultAssistantEndpoint[c.Provider], store.DefaultAssistantModel[c.Provider]
	}
	fixed["provider"] = c.Provider != ""
	for _, f := range []struct {
		key  string
		from string
		to   *string
	}{{"endpoint", c.Endpoint, &set.Endpoint}, {"model", c.Model, &set.Model}, {"name", c.Name, &set.Name}} {
		if f.from != "" {
			*f.to, fixed[f.key] = f.from, true
		}
	}
	if c.APIKey != "" {
		set.APIKey, set.KeySet, fixed["api_key"] = c.APIKey, true, true
	}
	for _, f := range []struct {
		key  string
		from *int
		to   *int
	}{{"max_tokens", c.MaxTokens, &set.MaxTokens}, {"max_steps", c.MaxSteps, &set.MaxSteps}, {"max_tools", c.MaxTools, &set.MaxTools},
		{"person_messages", c.PersonMessages, &set.PersonMessages}, {"instance_messages", c.InstanceMessages, &set.InstanceMessages}, {"keep_days", c.KeepDays, &set.KeepDays}} {
		if f.from != nil {
			*f.to, fixed[f.key] = *f.from, true
		}
	}
	if c.PersonTokens != nil {
		set.PersonTokens, fixed["person_tokens"] = *c.PersonTokens, true
	}
	if c.InstanceTokens != nil {
		set.InstanceTokens, fixed["instance_tokens"] = *c.InstanceTokens, true
	}
	for k, v := range fixed {
		if !v {
			delete(fixed, k)
		}
	}
	return set, fixed, nil
}

// assistantDisclosure says plainly where what people type goes.
func assistantDisclosure(set store.AssistantSettings) string {
	return fmt.Sprintf("Turning the assistant on sends what people type to it, the page they are on, and what BAM!'s tools answer it, "+
		"which can be anything they may see, to the provider at %s, to be read by the model %s. "+
		"What that provider keeps, and for how long, is between you and it.", set.Endpoint, set.Model)
}

// AssistantSettingsView is the settings as Admin reads them: the key never.
type AssistantSettingsView struct {
	store.AssistantSettings
	// Fixed are the settings the configuration sets, which Admin cannot.
	Fixed map[string]bool `json:"fixed"`
	// Disclosure is what turning it on means, said plainly.
	Disclosure string `json:"disclosure"`
	// Defaults are each provider's endpoint and model.
	Endpoints map[string]string `json:"default_endpoints"`
	Models    map[string]string `json:"default_models"`
}

// handleAssistantSettings: GET /api/v1/settings/assistant (settings.manage).
func (s *Server) handleAssistantSettings(w http.ResponseWriter, r *http.Request) {
	set, fixed, err := s.assistantSettings()
	if err != nil {
		writeServerError(w, err, "read the assistant's settings")
		return
	}
	set.APIKey = ""
	writeJSON(w, http.StatusOK, AssistantSettingsView{AssistantSettings: set, Fixed: fixed, Disclosure: assistantDisclosure(set),
		Endpoints: store.DefaultAssistantEndpoint, Models: store.DefaultAssistantModel})
}

// AssistantSettingsRequest changes the settings. APIKey, when given, is the
// new key; ClearKey removes the one there is. Acknowledge says the admin
// read the disclosure, which turning it on takes.
type AssistantSettingsRequest struct {
	store.AssistantSettings
	APIKey      string `json:"api_key"`
	ClearKey    bool   `json:"clear_key"`
	Acknowledge bool   `json:"acknowledge"`
}

// handleSetAssistantSettings: PUT /api/v1/settings/assistant (settings.manage).
// What the configuration sets is kept as it is.
func (s *Server) handleSetAssistantSettings(w http.ResponseWriter, r *http.Request) {
	var req AssistantSettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	stored, err := s.store.AssistantSettings()
	if err != nil {
		writeServerError(w, err, "read the assistant's settings")
		return
	}
	_, fixed, err := s.assistantSettings()
	if err != nil {
		writeServerError(w, err, "read the assistant's settings")
		return
	}
	next := req.AssistantSettings
	next.APIKey = stored.APIKey
	if req.ClearKey {
		next.APIKey = ""
	}
	if req.APIKey != "" {
		next.APIKey = strings.TrimSpace(req.APIKey)
	}
	keep := map[string]func(){
		"enabled": func() { next.Enabled = stored.Enabled }, "provider": func() { next.Provider = stored.Provider },
		"endpoint": func() { next.Endpoint = stored.Endpoint }, "model": func() { next.Model = stored.Model },
		"name": func() { next.Name = stored.Name }, "api_key": func() { next.APIKey = stored.APIKey },
		"max_tokens": func() { next.MaxTokens = stored.MaxTokens }, "max_steps": func() { next.MaxSteps = stored.MaxSteps },
		"max_tools": func() { next.MaxTools = stored.MaxTools }, "person_messages": func() { next.PersonMessages = stored.PersonMessages },
		"person_tokens": func() { next.PersonTokens = stored.PersonTokens }, "instance_messages": func() { next.InstanceMessages = stored.InstanceMessages },
		"instance_tokens": func() { next.InstanceTokens = stored.InstanceTokens }, "keep_days": func() { next.KeepDays = stored.KeepDays },
	}
	for k := range fixed {
		keep[k]()
	}
	next.AcknowledgedBy, next.AcknowledgedAt = stored.AcknowledgedBy, stored.AcknowledgedAt
	if err := next.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if next.Enabled && !stored.Enabled {
		// What it sends, and where, is said before it is turned on.
		effective := next
		if fixed["endpoint"] || fixed["model"] {
			effective, _, _ = s.assistantSettings()
			effective.Enabled = true
		}
		if !req.Acknowledge {
			writeJSONError(w, http.StatusBadRequest, assistantDisclosure(effective)+" Confirm that you have read this to turn it on.")
			return
		}
		u := CurrentUser(r.Context())
		now := time.Now().UTC()
		next.AcknowledgedAt = &now
		if u != nil {
			next.AcknowledgedBy = &u.ID
		}
	}
	if next.Enabled && next.Provider == store.AssistantAnthropic && next.APIKey == "" && !fixed["api_key"] {
		writeJSONError(w, http.StatusBadRequest, "Anthropic's API needs a key: give one to turn the assistant on")
		return
	}
	by := ""
	if u := CurrentUser(r.Context()); u != nil {
		by = u.ID
	}
	if err := s.store.SaveAssistantSettings(next, by); errors.Is(err, store.ErrInvalidSetting) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeServerError(w, err, "save the assistant's settings")
		return
	}
	s.handleAssistantSettings(w, r)
}

// AssistantStatus is what the panel needs to know: whether the requester
// may use the assistant, what it is called, and how much of the day's
// limits is left.
type AssistantStatus struct {
	// Available is the assistant turned on and the requester holding
	// assistant.use.
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	// Limits, 0 for none, and what is used of them today.
	Limits   assistantLimits    `json:"limits"`
	Used     store.AssistantUse `json:"used"`
	Instance store.AssistantUse `json:"instance_used"`
	// ResetsAt is when the day's limits start again.
	ResetsAt time.Time `json:"resets_at"`
}

type assistantLimits struct {
	PersonMessages   int   `json:"person_messages"`
	PersonTokens     int64 `json:"person_tokens"`
	InstanceMessages int   `json:"instance_messages"`
	InstanceTokens   int64 `json:"instance_tokens"`
}

// nextDay is when the day t is in ends, in UTC, which limits count in.
func nextDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

// handleAssistantStatus: GET /api/v1/assistant (a person).
func (s *Server) handleAssistantStatus(w http.ResponseWriter, r *http.Request) {
	set, _, err := s.assistantSettings()
	if err != nil {
		writeServerError(w, err, "read the assistant's settings")
		return
	}
	u := CurrentUser(r.Context())
	now := time.Now()
	person, instance, err := s.store.AssistantUsage(u.ID, store.AssistantDay(now))
	if err != nil {
		writeServerError(w, err, "read the assistant's use")
		return
	}
	writeJSON(w, http.StatusOK, AssistantStatus{
		Available: set.Enabled && HasPermission(r.Context(), rbac.PermAssistantUse) && CurrentToken(r.Context()) == nil,
		Enabled:   set.Enabled, Name: set.Name, Provider: set.Provider, Model: set.Model,
		Limits: assistantLimits{set.PersonMessages, set.PersonTokens, set.InstanceMessages, set.InstanceTokens},
		Used:   person, Instance: instance, ResetsAt: nextDay(now),
	})
}

// limitReached says which limit a day's use has reached, or "".
func limitReached(set store.AssistantSettings, person, instance store.AssistantUse, message bool) string {
	switch {
	case message && set.PersonMessages > 0 && person.Messages >= set.PersonMessages:
		return fmt.Sprintf("You have sent the %d messages a day each person may send to the assistant", set.PersonMessages)
	case message && set.InstanceMessages > 0 && instance.Messages >= set.InstanceMessages:
		return fmt.Sprintf("Everyone together has sent the %d messages a day this instance allows the assistant", set.InstanceMessages)
	case set.PersonTokens > 0 && person.Tokens >= set.PersonTokens:
		return fmt.Sprintf("You have used the %d tokens a day each person may use with the assistant", set.PersonTokens)
	case set.InstanceTokens > 0 && instance.Tokens >= set.InstanceTokens:
		return fmt.Sprintf("Everyone together has used the %d tokens a day this instance allows the assistant", set.InstanceTokens)
	}
	return ""
}

// limitMessage says a limit was reached and when it resets.
func limitMessage(reached string, at time.Time) string {
	return reached + ". It resets at " + at.Format("15:04 MST on 2 January") + "."
}

// GuideView is a guide as the API shows it, with its address.
type GuideView struct {
	store.Guide
	URL string `json:"url"`
	// Categories and LinkTypes are those it names that are still there,
	// by name.
	Categories []namedRef `json:"categories"`
	LinkTypes  []namedRef `json:"link_types"`
}

type namedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// guideURL is the address of a guide, /g/<slug>, on this server.
func (s *Server) guideURL(r *http.Request, slug string) string {
	return strings.TrimSuffix(s.flowURL(r, slug), "/f/"+url.PathEscape(slug)) + "/g/" + url.PathEscape(slug)
}

// guideView names what a guide works with.
func (s *Server) guideView(r *http.Request, g store.Guide) (GuideView, error) {
	v := GuideView{Guide: g, URL: s.guideURL(r, g.Slug), Categories: []namedRef{}, LinkTypes: []namedRef{}}
	cats, err := s.store.ListCategories()
	if err != nil {
		return v, err
	}
	names := map[string]string{}
	for _, c := range cats {
		names[c.ID] = c.Name
	}
	for _, id := range g.Category {
		if n, ok := names[id]; ok {
			v.Categories = append(v.Categories, namedRef{id, n})
		}
	}
	types, err := s.store.ListRelationTypes()
	if err != nil {
		return v, err
	}
	names = map[string]string{}
	for _, t := range types {
		names[t.ID] = t.Name
	}
	for _, id := range g.LinkType {
		if n, ok := names[id]; ok {
			v.LinkTypes = append(v.LinkTypes, namedRef{id, n})
		}
	}
	return v, nil
}

// handleListGuides: GET /api/v1/assistant/guides (assistant.use or
// settings.manage). Those using the assistant see only those turned on.
func (s *Server) handleListGuides(w http.ResponseWriter, r *http.Request) {
	guides, err := s.store.ListGuides()
	if err != nil {
		writeServerError(w, err, "list the guides")
		return
	}
	manage := HasPermission(r.Context(), rbac.PermSettingsManage)
	out := []GuideView{}
	for _, g := range guides {
		if !g.Enabled && !manage {
			continue
		}
		v, err := s.guideView(r, g)
		if err != nil {
			writeServerError(w, err, "list the guides")
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"guides": out})
}

// guideFor reads the guide at the request's address; one turned off only
// for those who manage guides.
func (s *Server) guideFor(w http.ResponseWriter, r *http.Request) (*store.Guide, bool) {
	g, err := s.store.GetGuide(r.PathValue("slug"))
	if errors.Is(err, store.ErrGuideNotFound) || err == nil && !g.Enabled && !HasPermission(r.Context(), rbac.PermSettingsManage) {
		writeJSONError(w, http.StatusNotFound, "Guide not found")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the guide")
		return nil, false
	}
	return g, true
}

// handleGetGuide: GET /api/v1/assistant/guides/{slug}.
func (s *Server) handleGetGuide(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guideFor(w, r)
	if !ok {
		return
	}
	v, err := s.guideView(r, *g)
	if err != nil {
		writeServerError(w, err, "read the guide")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"guide": v})
}

// handleSaveGuide makes a guide, or changes the one at the address:
// POST /api/v1/assistant/guides, PUT /api/v1/assistant/guides/{slug}
// (settings.manage).
func (s *Server) handleSaveGuide(w http.ResponseWriter, r *http.Request) {
	var req store.GuideInput
	if !decodeJSON(w, r, &req) {
		return
	}
	id := ""
	status := http.StatusCreated
	if slug := r.PathValue("slug"); slug != "" {
		g, ok := s.guideFor(w, r)
		if !ok {
			return
		}
		id, status = g.ID, http.StatusOK
	}
	by := ""
	if u := CurrentUser(r.Context()); u != nil {
		by = u.ID
	}
	g, err := s.store.SaveGuide(id, req, by)
	switch {
	case errors.Is(err, store.ErrInvalidGuide):
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Another guide already has this address")
		return
	case err != nil:
		writeStoreError(w, err, "save the guide")
		return
	}
	v, err := s.guideView(r, *g)
	if err != nil {
		writeServerError(w, err, "read the guide")
		return
	}
	writeJSON(w, status, map[string]any{"guide": v})
}

// handleDeleteGuide: DELETE /api/v1/assistant/guides/{slug} (settings.manage).
func (s *Server) handleDeleteGuide(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guideFor(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteGuide(g.ID); err != nil {
		writeServerError(w, err, "delete the guide")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Guide deleted"})
}

// handleGuideQR draws the QR code of a guide's address, for its poster:
// GET /api/v1/assistant/guides/{slug}/qr.svg
func (s *Server) handleGuideQR(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guideFor(w, r)
	if !ok {
		return
	}
	gen, err := generators.Lookup(generators.TypeQR)
	if err == nil {
		var sym generators.Symbol
		if sym, err = gen.Encode(s.guideURL(r, g.Slug), ""); err == nil {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(symbolSVG(sym))
			return
		}
	}
	writeServerError(w, err, "draw the guide's code")
}

// handleListConversations: GET /api/v1/assistant/conversations, the
// requester's own, most recent first.
func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListConversations(CurrentUser(r.Context()).ID)
	if err != nil {
		writeServerError(w, err, "list your conversations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

// conversationFor reads the requester's conversation at the request's
// address. Anyone else's is not there, to anyone, admins included.
func (s *Server) conversationFor(w http.ResponseWriter, r *http.Request) (*store.Conversation, bool) {
	c, err := s.store.GetConversation(CurrentUser(r.Context()).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrConversationNotFound) {
		writeJSONError(w, http.StatusNotFound, "Conversation not found")
		return nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the conversation")
		return nil, false
	}
	return c, true
}

// handleGetConversation: GET /api/v1/assistant/conversations/{id}, with its
// messages and any call waiting to be confirmed.
func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	c, ok := s.conversationFor(w, r)
	if !ok {
		return
	}
	msgs, err := s.store.ConversationMessages(c.ID)
	if err != nil {
		writeServerError(w, err, "read the conversation")
		return
	}
	out := map[string]any{"conversation": c, "messages": viewMessages(msgs), "pending": pendingCall(msgs, c.PendingCall)}
	if c.GuideID != "" {
		if g, err := s.store.GetGuideByID(c.GuideID); err == nil {
			if v, err := s.guideView(r, *g); err == nil {
				out["guide"] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteConversation: DELETE /api/v1/assistant/conversations/{id}.
func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	c, ok := s.conversationFor(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteConversation(c.UserID, c.ID); err != nil {
		writeServerError(w, err, "delete the conversation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Conversation deleted"})
}

// RunAssistant deletes, every hour, conversations older than the instance
// keeps them, and use counted more than a week ago.
func (s *Server) RunAssistant(ctx context.Context) {
	for {
		if n, err := s.SweepConversations(time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("deleting old conversations: %v", err)
		} else if n > 0 {
			log.Printf("deleted %d conversations with the assistant past their time", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

// SweepConversations deletes the conversations whose last message is
// older than the instance keeps them, as of now.
func (s *Server) SweepConversations(now time.Time) (int, error) {
	set, _, err := s.assistantSettings()
	if err != nil {
		return 0, err
	}
	if err := s.store.DeleteAssistantUsageBefore(store.AssistantDay(now.AddDate(0, 0, -7))); err != nil {
		return 0, err
	}
	return s.store.DeleteConversationsBefore(now.AddDate(0, 0, -set.KeepDays))
}
