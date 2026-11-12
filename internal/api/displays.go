package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/notify"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// Displays (docs/printing.md#displays) are printers whose driver drives
// e-paper displays. Printing to one renders the label and pushes it, or
// keeps it for the display to pull; each remembers the template and asset
// it shows, and as whom, so that hooks can refresh it when the asset
// changes. Refreshes go through the spool like any job.

// DisplaySettings are the instance's settings for displays: how long a
// refresh waits for the rest of a burst of changes, and how long a display
// that pulls waits before asking again when its own options do not say, in
// seconds; and paperlesspaper's cloud, for displays pushed through it.
type DisplaySettings struct {
	RefreshWindowSeconds int `json:"refresh_window_seconds"`
	PullRefreshSeconds   int `json:"pull_refresh_seconds"`
	// PaperlessPaperBaseURL is paperlesspaper's API, its own unless set.
	PaperlessPaperBaseURL string `json:"paperlesspaper_base_url"`
	// PaperlessPaperAPIKey sets its API key, or with "" clears it; it is
	// never answered, only whether one is set.
	PaperlessPaperAPIKey    *string `json:"paperlesspaper_api_key,omitempty"`
	PaperlessPaperAPIKeySet bool    `json:"paperlesspaper_api_key_set"`
}

// handleDisplaySettings reads them: GET /api/v1/settings/displays.
func (s *Server) handleDisplaySettings(w http.ResponseWriter, r *http.Request) {
	window, pull, err := s.store.DisplaySettings()
	if err != nil {
		writeServerError(w, err, "read the display settings")
		return
	}
	base, err := s.store.Setting(store.PaperlessPaperBaseSetting, printers.DefaultPaperlessPaperBase)
	if err != nil {
		writeServerError(w, err, "read the display settings")
		return
	}
	key, err := s.store.Setting(store.PaperlessPaperKeySetting, "")
	if err != nil {
		writeServerError(w, err, "read the display settings")
		return
	}
	writeJSON(w, http.StatusOK, DisplaySettings{RefreshWindowSeconds: window, PullRefreshSeconds: pull, PaperlessPaperBaseURL: base, PaperlessPaperAPIKeySet: key != ""})
}

// paperlessAccount is paperlesspaper's API and its key, opened.
func (s *Server) paperlessAccount() (printers.PaperlessPaperAccount, error) {
	base, err := s.store.Setting(store.PaperlessPaperBaseSetting, printers.DefaultPaperlessPaperBase)
	if err != nil {
		return printers.PaperlessPaperAccount{}, err
	}
	sealed, err := s.store.Setting(store.PaperlessPaperKeySetting, "")
	if err != nil || sealed == "" {
		return printers.PaperlessPaperAccount{Base: base}, err
	}
	key, err := s.sealer.Open(sealed, store.PaperlessPaperKeyPurpose)
	if err != nil {
		return printers.PaperlessPaperAccount{}, fmt.Errorf("opening paperlesspaper's API key: %v; set it again under Admin › Displays", err)
	}
	return printers.PaperlessPaperAccount{Base: base, APIKey: string(key)}, nil
}

// paperlessClient makes paperlesspaper's uploads.
var paperlessClient = &http.Client{Timeout: 60 * time.Second}

// handleSetDisplaySettings sets them: PUT /api/v1/settings/displays.
func (s *Server) handleSetDisplaySettings(w http.ResponseWriter, r *http.Request) {
	var req DisplaySettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshWindowSeconds < 0 || req.RefreshWindowSeconds > store.MaxDisplayRefreshWindow {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("refresh_window_seconds is from 0 to %d, an hour", store.MaxDisplayRefreshWindow))
		return
	}
	if req.PullRefreshSeconds < printers.MinDisplayRefresh || req.PullRefreshSeconds > printers.MaxDisplayRefresh {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("pull_refresh_seconds is from %d to %d, a day", printers.MinDisplayRefresh, printers.MaxDisplayRefresh))
		return
	}
	base := strings.TrimRight(strings.TrimSpace(req.PaperlessPaperBaseURL), "/")
	if base == "" {
		base = printers.DefaultPaperlessPaperBase
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		writeJSONError(w, http.StatusBadRequest, "paperlesspaper_base_url is an http(s) URL, such as "+printers.DefaultPaperlessPaperBase)
		return
	}
	sealed := ""
	if req.PaperlessPaperAPIKey != nil {
		if key := strings.TrimSpace(*req.PaperlessPaperAPIKey); key != "" {
			if !s.sealer.HasKey() {
				writeJSONError(w, http.StatusConflict, "Keeping paperlesspaper's API key needs an [auth] secret_key in the configuration, to seal it with")
				return
			}
			var err error
			if sealed, err = s.sealer.Seal([]byte(key), store.PaperlessPaperKeyPurpose); err != nil {
				writeServerError(w, err, "seal paperlesspaper's API key")
				return
			}
		}
	}
	for key, value := range map[string]string{store.DisplayRefreshWindowSetting: strconv.Itoa(req.RefreshWindowSeconds), store.DisplayPullRefreshSetting: strconv.Itoa(req.PullRefreshSeconds), store.PaperlessPaperBaseSetting: base} {
		if err := s.store.SetSetting(key, value); err != nil {
			writeServerError(w, err, "save the display settings")
			return
		}
	}
	if req.PaperlessPaperAPIKey != nil {
		if err := s.store.SetSetting(store.PaperlessPaperKeySetting, sealed); err != nil {
			writeServerError(w, err, "save the display settings")
			return
		}
	}
	s.handleDisplaySettings(w, r)
}

// DisplayView is a display as the API shows it: the printer, what it
// shows, and for one that pulls, where it fetches its image and how often.
type DisplayView struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Driver     string              `json:"driver"`
	Model      string              `json:"model"`
	Connection printers.Connection `json:"connection"`
	// Shown is what it shows; nil when it has been shown nothing.
	Shown *models.Display `json:"shown"`
	// PullURL is where a display that pulls fetches its image, with one
	// of its tokens; RefreshSeconds is how long it is told to wait
	// before asking again.
	PullURL        string `json:"pull_url,omitempty"`
	RefreshSeconds int    `json:"refresh_seconds,omitempty"`
}

// displayView is a display printer as the API shows it.
func (s *Server) displayView(r *http.Request, p *models.Printer, shown map[string]*models.Display) (DisplayView, error) {
	v := DisplayView{ID: p.ID, Name: p.Name, Driver: p.Driver, Model: p.Model, Connection: p.Connection, Shown: shown[p.ID]}
	if p.Connection.Type == printers.ConnPull {
		v.PullURL = s.baseURL(r) + "/api/v1/displays/" + p.ID + "/image"
		n, err := s.displayRefresh(p)
		if err != nil {
			return v, err
		}
		v.RefreshSeconds = n
	}
	return v, nil
}

// displayRefresh is how long a display that pulls is told to wait before
// asking again: its own option, or the instance's.
func (s *Server) displayRefresh(p *models.Printer) (int, error) {
	if n, ok := printers.DisplayRefresh(store.PrinterSettings(p)); ok {
		return n, nil
	}
	_, pull, err := s.store.DisplaySettings()
	return pull, err
}

// shownByPrinter reads what every display shows, by printer.
func (s *Server) shownByPrinter() (map[string]*models.Display, error) {
	list, err := s.store.ListDisplays()
	if err != nil {
		return nil, err
	}
	out := make(map[string]*models.Display, len(list))
	for i := range list {
		out[list[i].PrinterID] = &list[i]
	}
	return out, nil
}

// handleListDisplays lists the displays and what each shows:
// GET /api/v1/displays
func (s *Server) handleListDisplays(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPrinters()
	if err != nil {
		writeServerError(w, err, "list displays")
		return
	}
	shown, err := s.shownByPrinter()
	if err != nil {
		writeServerError(w, err, "list displays")
		return
	}
	out := []DisplayView{}
	for i := range list {
		if !printers.IsDisplay(list[i].Driver) {
			continue
		}
		v, err := s.displayView(r, &list[i], shown)
		if err != nil {
			writeServerError(w, err, "list displays")
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"displays": out})
}

// loadDisplay reads a printer that is a display, writing the error when
// it cannot.
func (s *Server) loadDisplay(w http.ResponseWriter, id string) (*models.Printer, bool) {
	p, ok := s.loadPrinter(w, id)
	if !ok {
		return nil, false
	}
	if !printers.IsDisplay(p.Driver) {
		writeJSONError(w, http.StatusNotFound, "That printer is not a display")
		return nil, false
	}
	return p, true
}

// handleGetDisplay reads a display and what it shows:
// GET /api/v1/displays/{id}
func (s *Server) handleGetDisplay(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadDisplay(w, r.PathValue("id"))
	if !ok {
		return
	}
	shown := map[string]*models.Display{}
	switch d, err := s.store.GetDisplay(p.ID); {
	case err == nil:
		shown[p.ID] = d
	case !errors.Is(err, store.ErrDisplayNotFound):
		writeServerError(w, err, "read the display")
		return
	}
	v, err := s.displayView(r, p, shown)
	if err != nil {
		writeServerError(w, err, "read the display")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"display": v})
}

// handleRefreshDisplay asks for a display to be shown its label again
// now, laid out afresh: POST /api/v1/displays/{id}/refresh
func (s *Server) handleRefreshDisplay(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadDisplay(w, r.PathValue("id"))
	if !ok {
		return
	}
	err := s.store.RefreshDisplay(p.ID, time.Now())
	if errors.Is(err, store.ErrDisplayNotFound) {
		writeJSONError(w, http.StatusConflict, p.Name+" has not been shown an asset's label to refresh; print one to it first")
		return
	}
	if err != nil {
		writeServerError(w, err, "refresh the display")
		return
	}
	s.wakeSpool()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "The display will be refreshed through the print queue"})
}

// refreshDueDisplays queues a job for every display whose refresh is due
// at now, each claimed first so that one server makes it, and returns how
// many it queued. The job acts as whoever last showed the display
// something, as the spool does for whoever printed.
func (s *Server) refreshDueDisplays(now time.Time) int {
	due, err := s.store.DueDisplayRefreshes(now)
	if err != nil {
		log.Printf("refreshing displays: %v", err)
		return 0
	}
	queued := 0
	for _, d := range due {
		claimed, err := s.store.ClaimDisplayRefresh(d.PrinterID, *d.RefreshAt)
		if err != nil {
			log.Printf("refreshing displays: %v", err)
			continue
		}
		if !claimed || d.AssetID == nil || d.TemplateID == nil || d.ShownBy == "" {
			continue
		}
		if err := s.queueRefresh(d); err != nil {
			log.Printf("refreshing display %s: %v", d.PrinterID, err)
			continue
		}
		queued++
	}
	return queued
}

// queueRefresh makes the job that shows a display its asset's label again.
func (s *Server) queueRefresh(d models.Display) error {
	p, err := s.store.GetPrinter(d.PrinterID)
	if err != nil {
		return err
	}
	t, err := s.store.GetTemplate(*d.TemplateID)
	if err != nil {
		return err
	}
	acting := s.store.As(d.ShownBy)
	if d.ShownByToken {
		acting = s.store.AsToken(d.ShownBy)
	}
	destination := models.DestinationPrinterPrefix + p.ID
	added, already, err := acting.AddToSpool([]store.SpoolAdd{{AssetID: *d.AssetID, TemplateID: d.TemplateID, Destination: destination, Copies: 1}})
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range added {
		ids = append(ids, e.ID)
	}
	if len(already) > 0 {
		// The label waits in the queue already: print that one, if it is
		// for this display and in no job yet.
		waiting, err := s.store.ListSpool(store.SpoolFilter{})
		if err != nil {
			return err
		}
		for _, e := range waiting {
			if e.AssetID == *d.AssetID && e.TemplateID != nil && *e.TemplateID == *d.TemplateID && e.Destination == destination && e.Job == nil {
				ids = append(ids, e.ID)
			}
		}
	}
	if len(ids) == 0 {
		// It is in a job already, which shows it.
		return nil
	}
	if _, err := acting.CreateSpoolJobs([]store.SpoolJobNew{{TemplateID: t.ID, TemplateName: t.Name, PrinterID: p.ID, PrinterName: p.Name, EntryIDs: ids}}); err != nil {
		return err
	}
	s.wakeSpool()
	return nil
}

// deliverToDisplay delivers an image to a display whose connection the
// server itself delivers to: kept for one that pulls, posted through its
// channel for a webhook's. It reports whether it was one.
func (s *Server) deliverToDisplay(ctx context.Context, p *models.Printer, img []byte) (bool, error) {
	switch p.Connection.Type {
	case printers.ConnPull:
		if _, err := s.store.SetDisplayImage(p.ID, img, printers.DisplayContentType(img)); err != nil {
			return true, fmt.Errorf("%w: keeping the image: %v", printers.ErrUnreachable, err)
		}
		return true, nil
	case printers.ConnWebhook:
		ch, err := s.webhookChannel(p.Connection.Address)
		if err != nil {
			return true, fmt.Errorf("%w: %v", printers.ErrUnreachable, err)
		}
		d, _ := notify.Lookup("webhook")
		hook, ok := d.(interface {
			SendImage(ctx context.Context, options map[string]string, img []byte, contentType, displayID, deliveryID string) error
		})
		if !ok {
			return true, fmt.Errorf("%w: webhooks cannot post images", printers.ErrUnreachable)
		}
		if err := hook.SendImage(ctx, ch.Options, img, printers.DisplayContentType(img), p.ID, uuid.NewString()); err != nil {
			return true, fmt.Errorf("%w: posting to %s: %v", printers.ErrUnreachable, ch.Name, err)
		}
		return true, nil
	case printers.ConnPaperlessPaper:
		a, err := s.paperlessAccount()
		if err != nil {
			return true, fmt.Errorf("%w: %v", printers.ErrUnreachable, err)
		}
		if err := printers.PushPaperlessPaper(ctx, paperlessClient, a, p.Connection.Address, img); err != nil {
			return true, fmt.Errorf("%w: %v", printers.ErrUnreachable, err)
		}
		return true, nil
	}
	return false, nil
}

// webhookChannel is the webhook channel a display's connection names,
// turned on.
func (s *Server) webhookChannel(id string) (*models.NotificationChannel, error) {
	ch, err := s.store.GetChannel(id)
	if err != nil {
		return nil, errors.New("its webhook channel was not found; choose one under Admin › Notifications")
	}
	if ch.Driver != "webhook" {
		return nil, fmt.Errorf("its channel, %s, is not a webhook", ch.Name)
	}
	if !ch.Enabled {
		return nil, fmt.Errorf("its webhook channel, %s, is turned off", ch.Name)
	}
	return ch, nil
}

// rememberShown records what a display was shown, as the acting store's
// user or token, whom its refreshes act as. It does nothing for a printer.
func (s *Server) rememberShown(acting *store.Store, p *models.Printer, templateID, assetID string) {
	if !printers.IsDisplay(p.Driver) {
		return
	}
	var template, asset *string
	if templateID != "" {
		template = &templateID
	}
	if assetID != "" {
		asset = &assetID
	}
	if err := acting.ShowOnDisplay(p.ID, template, asset); err != nil {
		log.Printf("recording what %s shows: %v", p.Name, err)
	}
}

// Display tokens: each reaches its own display's image, and nothing else.

// displaySecret is the display token a request presents: as ?token=, in
// an Access-Token header (TRMNL's, which some of its servers spell
// ACCESS_TOKEN), as a bearer token that is a display's, or as the password
// of HTTP Basic, which is all an OpenPaper frame can send.
func displaySecret(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	for _, h := range []string{"Access-Token", "Access_Token"} {
		if t := strings.TrimSpace(r.Header.Get(h)); t != "" {
			return t
		}
	}
	if t, ok := bearerToken(r); ok && strings.HasPrefix(t, store.DisplayTokenPrefix) {
		return t
	}
	if _, t, ok := r.BasicAuth(); ok && strings.HasPrefix(t, store.DisplayTokenPrefix) {
		return t
	}
	return ""
}

// displayRequest reports whether a request authenticates as a display:
// TRMNL's API, and a display's image fetched with a display token. Neither
// is an API token, and a browser's session counts for nothing.
func displayRequest(r *http.Request) bool {
	switch r.URL.Path {
	case trmnlSetupPath, trmnlDisplayPath, trmnlLogPath:
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/displays/")
	id, tail, _ := strings.Cut(rest, "/")
	return ok && id != "" && tail == "image" && displaySecret(r) != ""
}

// handleListDisplayTokens: GET /api/v1/displays/{id}/tokens
func (s *Server) handleListDisplayTokens(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadDisplay(w, r.PathValue("id"))
	if !ok {
		return
	}
	tokens, err := s.store.ListDisplayTokens(p.ID)
	if err != nil {
		writeServerError(w, err, "list the display's tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// DisplayTokenRequest makes a display token.
type DisplayTokenRequest struct {
	Name string `json:"name"`
}

// handleCreateDisplayToken makes a token for a display to fetch its image
// with, its secret shown this once: POST /api/v1/displays/{id}/tokens
func (s *Server) handleCreateDisplayToken(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadDisplay(w, r.PathValue("id"))
	if !ok {
		return
	}
	if p.Connection.Type != printers.ConnPull {
		writeJSONError(w, http.StatusConflict, p.Name+" does not pull its image, so it has no use for a token")
		return
	}
	var req DisplayTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, secret, err := s.actingStore(r).CreateDisplayToken(p.ID, req.Name)
	if errors.Is(err, store.ErrInvalidDisplayToken) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "make the display token")
		return
	}
	image := s.baseURL(r) + "/api/v1/displays/" + p.ID + "/image"
	if p.Driver == printers.DriverPaperlessPaper {
		// An OpenPaper frame takes an address of at most 128 characters,
		// and sends the token as Basic's password.
		writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret, "url": image, "username": "bam"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret, "url": image + "?token=" + secret})
}

// handleRevokeDisplayToken: DELETE /api/v1/displays/{id}/tokens/{token}
func (s *Server) handleRevokeDisplayToken(w http.ResponseWriter, r *http.Request) {
	err := s.store.RevokeDisplayToken(r.PathValue("id"), r.PathValue("token"))
	if errors.Is(err, store.ErrDisplayTokenNotFound) {
		writeJSONError(w, http.StatusNotFound, "Display token not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "revoke the display token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Display token revoked"})
}

// displayImage is the image a display that pulls shows, its media type
// and tag: what it was last shown, or a blank screen until then.
func (s *Server) displayImage(p *models.Printer) ([]byte, string, string, error) {
	img, contentType, etag, err := s.store.DisplayImage(p.ID)
	if !errors.Is(err, store.ErrDisplayNotFound) {
		return img, contentType, etag, err
	}
	img, err = printers.BlankDisplay(p.Driver, store.PrinterSettings(p))
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(img)
	return img, printers.DisplayContentType(img), "blank-" + hex.EncodeToString(sum[:8]), nil
}

// handleDisplayImage serves a display's image: GET
// /api/v1/displays/{id}/image, to the display with one of its own tokens,
// and to those who may see printers. It carries an ETag, so an image that
// has not changed is not sent again, and Refresh, the seconds to wait
// before asking again.
func (s *Server) handleDisplayImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	secret := displaySecret(r)
	switch {
	case secret != "":
		t, err := s.store.AuthenticateDisplayToken(secret)
		if errors.Is(err, store.ErrDisplayTokenNotFound) {
			writeJSONError(w, http.StatusUnauthorized, "Invalid or revoked display token")
			return
		}
		if err != nil {
			writeServerError(w, err, "read the display token")
			return
		}
		if t.PrinterID != id {
			writeJSONError(w, http.StatusForbidden, "A display's token fetches only its own display's image")
			return
		}
	case !HasPermission(r.Context(), rbac.PermLabelsPrint) && !HasPermission(r.Context(), rbac.PermSettingsManage) && !HasPermission(r.Context(), rbac.PermTemplatesManage):
		refuse(w, r)
		return
	}
	p, ok := s.loadDisplay(w, id)
	if !ok {
		return
	}
	if p.Connection.Type != printers.ConnPull {
		writeJSONError(w, http.StatusNotFound, p.Name+" does not pull its image: BAM! sends it")
		return
	}
	img, contentType, etag, err := s.displayImage(p)
	if err != nil {
		writeServerError(w, err, "read the display's image")
		return
	}
	refresh, err := s.displayRefresh(p)
	if err != nil {
		writeServerError(w, err, "read the display's refresh")
		return
	}
	if secret != "" {
		if err := s.store.RecordDisplayFetch(p.ID, time.Now()); err != nil {
			log.Printf("recording %s's fetch: %v", p.Name, err)
		}
	}
	tag := `"` + etag + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Refresh", strconv.Itoa(refresh))
	if p.Driver == printers.DriverPaperlessPaper {
		// An OpenPaper frame sleeps for what this says, and reads no Refresh.
		w.Header().Set("X-OpenPaper-Sleep-Seconds", strconv.Itoa(refresh))
	}
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(img)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(img)
	}
}

// etagMatches reports whether an If-None-Match header names tag, or is *.
func etagMatches(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == tag || t == "*" {
			return true
		}
	}
	return false
}

// TRMNL's "bring your own server" API (docs/printing.md#trmnl), as TRMNL
// documents it at docs.trmnl.com/go/diy/byos: a device asks /api/setup for
// a key, by its MAC address, then /api/display for the URL of the image to
// show and how long to sleep, and posts its logs to /api/log.
const (
	trmnlSetupPath   = "/api/setup"
	trmnlDisplayPath = "/api/display"
	trmnlLogPath     = "/api/log"
)

// trmnlStatus answers a TRMNL device, whose firmware reads the status in
// the body.
func trmnlStatus(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"status": code, "message": message})
}

// friendlyID is the short ID TRMNL shows a device by: the display's own,
// shortened.
func friendlyID(id string) string {
	return strings.ToUpper(strings.ReplaceAll(id, "-", ""))[:6]
}

// handleTRMNLSetup gives a TRMNL its key: GET /api/setup, with its MAC
// address as ID. Only a TRMNL set up in BAM! as a display with that
// address, and with no token yet, is given one: revoking its tokens lets
// it set up again.
func (s *Server) handleTRMNLSetup(w http.ResponseWriter, r *http.Request) {
	mac := r.Header.Get("ID")
	p, err := s.store.DisplayByMAC(printers.DriverTRMNL, mac)
	if errors.Is(err, store.ErrPrinterNotFound) {
		trmnlStatus(w, http.StatusNotFound, fmt.Sprintf("No TRMNL with MAC address %q is set up in BAM!: add it under Admin › Printers with the trmnl driver", mac))
		return
	}
	if err != nil {
		writeServerError(w, err, "find the TRMNL")
		return
	}
	tokens, err := s.store.ListDisplayTokens(p.ID)
	if err != nil {
		writeServerError(w, err, "read the TRMNL's tokens")
		return
	}
	if len(tokens) > 0 {
		trmnlStatus(w, http.StatusConflict, p.Name+" is set up already; revoke its tokens in BAM! to set it up again")
		return
	}
	_, secret, err := s.store.CreateDisplayToken(p.ID, "TRMNL setup")
	if err != nil {
		writeServerError(w, err, "make the TRMNL's token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": http.StatusOK, "api_key": secret, "friendly_id": friendlyID(p.ID),
		"image_url": s.baseURL(r) + "/api/v1/displays/" + p.ID + "/image?token=" + secret,
		"message":   "Set up in BAM! as " + p.Name,
	})
}

// trmnlDevice is the TRMNL a request comes from, by its Access-Token,
// answering for itself when it cannot be told.
func (s *Server) trmnlDevice(w http.ResponseWriter, r *http.Request) (*models.Printer, string, bool) {
	secret := displaySecret(r)
	t, err := s.store.AuthenticateDisplayToken(secret)
	if errors.Is(err, store.ErrDisplayTokenNotFound) {
		trmnlStatus(w, http.StatusUnauthorized, "Unknown or revoked Access-Token: set the device up again")
		return nil, "", false
	}
	if err != nil {
		writeServerError(w, err, "read the TRMNL's token")
		return nil, "", false
	}
	p, err := s.store.GetPrinter(t.PrinterID)
	if err != nil {
		writeServerError(w, err, "read the TRMNL")
		return nil, "", false
	}
	if p.Driver != printers.DriverTRMNL {
		trmnlStatus(w, http.StatusForbidden, p.Name+" is not a TRMNL")
		return nil, "", false
	}
	// A device that says who it is must be the one the token is for.
	if id := r.Header.Get("ID"); id != "" {
		if c, err := printers.NormalizeConnection(printers.Connection{Type: printers.ConnPull, Address: id}); err != nil || c.Address != p.Connection.Address {
			trmnlStatus(w, http.StatusForbidden, "That Access-Token is another device's")
			return nil, "", false
		}
	}
	return p, secret, true
}

// handleTRMNLDisplay tells a TRMNL what to show: GET /api/display. The
// image's file name changes only when the image does, as TRMNL asks.
func (s *Server) handleTRMNLDisplay(w http.ResponseWriter, r *http.Request) {
	p, secret, ok := s.trmnlDevice(w, r)
	if !ok {
		return
	}
	_, _, etag, err := s.displayImage(p)
	if err != nil {
		writeServerError(w, err, "read the TRMNL's image")
		return
	}
	refresh, err := s.displayRefresh(p)
	if err != nil {
		writeServerError(w, err, "read the TRMNL's refresh")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           0,
		"image_url":        s.baseURL(r) + "/api/v1/displays/" + p.ID + "/image?token=" + secret,
		"filename":         "bam-" + strings.TrimPrefix(etag, "blank-")[:12],
		"refresh_rate":     refresh,
		"reset_firmware":   false,
		"update_firmware":  false,
		"firmware_url":     nil,
		"special_function": "none",
	})
}

// handleTRMNLLog takes a TRMNL's logs, which BAM! does not keep: POST
// /api/log.
func (s *Server) handleTRMNLLog(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.trmnlDevice(w, r); !ok {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}
