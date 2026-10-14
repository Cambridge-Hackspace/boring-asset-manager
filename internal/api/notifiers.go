package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// ChannelRequest creates or replaces a notification channel. On update, a
// secret option left blank keeps its stored value.
type ChannelRequest struct {
	Name    string            `json:"name"`
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options"`
	// Enabled defaults to true.
	Enabled *bool    `json:"enabled"`
	Events  []string `json:"events"`
}

// channelView is a channel as the API shows it: secret values are replaced
// by whether they are set.
type channelView struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Options    map[string]string `json:"options"`
	SecretsSet map[string]bool   `json:"secrets_set"`
	Enabled    bool              `json:"enabled"`
	Events     []string          `json:"events"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

func viewChannel(c models.NotificationChannel) channelView {
	options, secrets := notify.Redact(c)
	return channelView{
		ID: c.ID, Name: c.Name, Driver: c.Driver, Options: options, SecretsSet: secrets,
		Enabled: c.Enabled, Events: c.Events, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// channel turns a request into a channel, trimming values and dropping
// blank options.
func (req ChannelRequest) channel() models.NotificationChannel {
	c := models.NotificationChannel{
		Name:    strings.TrimSpace(req.Name),
		Driver:  req.Driver,
		Options: map[string]string{},
		Enabled: req.Enabled == nil || *req.Enabled,
		Events:  req.Events,
	}
	for k, v := range req.Options {
		if v = strings.TrimSpace(v); v != "" {
			c.Options[k] = v
		}
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	return c
}

func (s *Server) handleNotifierDrivers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"drivers": notify.Drivers(), "events": notify.EventKinds})
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListChannels()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list channels: %v", err))
		return
	}
	views := make([]channelView, 0, len(list))
	for _, c := range list {
		views = append(views, viewChannel(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": views})
}

func (s *Server) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetChannel(r.PathValue("id"))
	if s.channelError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": viewChannel(*c)})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req ChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	c := req.channel()
	if err := notify.Validate(c); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateChannel(c)
	if s.channelError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": viewChannel(*created)})
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	stored, err := s.store.GetChannel(r.PathValue("id"))
	if s.channelError(w, err) {
		return
	}
	var req ChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	c := req.channel()
	c.ID = stored.ID
	c.Options = notify.MergeSecrets(c, *stored)
	if err := notify.Validate(c); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.store.UpdateChannel(c)
	if s.channelError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": viewChannel(*updated)})
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	if s.channelError(w, s.store.DeleteChannel(r.PathValue("id"))) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Channel deleted"})
}

// handleTestChannel sends a channel's test message straight away:
// POST /api/v1/notifiers/{id}/test
func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetChannel(r.PathValue("id"))
	if s.channelError(w, err) {
		return
	}
	if err := notify.SendTest(r.Context(), *c, s.cfg.Server.BaseURL); err != nil {
		writeJSONError(w, http.StatusBadGateway, "The test message was not delivered: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Test message sent"})
}

// handleListDeliveries shows the delivery log, newest first:
// GET /api/v1/notifiers/deliveries?limit=n (default 50, at most 200)
func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeJSONError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = n
	}
	list, err := s.store.ListDeliveries(limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list deliveries: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": list})
}

// channelError writes the response for a channel store error, reporting
// whether there was one.
func (s *Server) channelError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrChannelNotFound):
		writeJSONError(w, http.StatusNotFound, "Channel not found")
	case errors.Is(err, store.ErrChannelNameExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
	return true
}
