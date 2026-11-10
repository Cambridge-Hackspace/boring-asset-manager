package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/appicon"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Installing BAM! on a phone from the instance itself (#213,
// docs/installing.md#on-a-phone): the web app manifest, the icons it lists,
// and the settings an admin gives them. The service worker that keeps the
// app's shell is the web interface's own (internal/webui).

// The installed app's settings. An empty name follows the instance's name
// (InstanceNameSetting), and an empty short name the name; an empty icon
// is BAM!'s own.
const (
	AppNameSetting       = "app.name"
	AppShortNameSetting  = "app.short_name"
	AppThemeSetting      = "app.theme_color"
	AppThemeDarkSetting  = "app.theme_color_dark"
	AppBackgroundSetting = "app.background_color"
	AppIconSetting       = "app.icon_image_id"
)

// BAM!'s colors (web/src/app.css): the paper its header is drawn on, light
// and dark, which phones color their status bar to match.
const (
	defaultAppTheme      = "#faf7f0"
	defaultAppThemeDark  = "#0b0b0a"
	defaultAppBackground = "#faf7f0"
)

// Limits on the names: launchers show about twelve characters under an
// icon, so a longer short name is cut.
const (
	maxAppName      = 100
	maxAppShortName = 30
)

// AppSettings are the installed app's settings, as stored: Name and
// ShortName may be empty, to follow the instance's name.
type AppSettings struct {
	Name            string `json:"name"`
	ShortName       string `json:"short_name"`
	ThemeColor      string `json:"theme_color"`
	ThemeColorDark  string `json:"theme_color_dark"`
	BackgroundColor string `json:"background_color"`
	IconImageID     string `json:"icon_image_id"`
}

// appSettingsReply adds what the empty settings follow, for a form to show.
type appSettingsReply struct {
	AppSettings
	InstanceName string `json:"instance_name"`
}

// appSettings reads the settings, with BAM!'s defaults for those unset.
func (s *Server) appSettings() (AppSettings, string, error) {
	var a AppSettings
	instance, err := s.store.Setting(InstanceNameSetting, defaultInstanceName)
	for _, f := range []struct {
		key, def string
		to       *string
	}{
		{AppNameSetting, "", &a.Name},
		{AppShortNameSetting, "", &a.ShortName},
		{AppThemeSetting, defaultAppTheme, &a.ThemeColor},
		{AppThemeDarkSetting, defaultAppThemeDark, &a.ThemeColorDark},
		{AppBackgroundSetting, defaultAppBackground, &a.BackgroundColor},
		{AppIconSetting, "", &a.IconImageID},
	} {
		if err != nil {
			break
		}
		*f.to, err = s.store.Setting(f.key, f.def)
	}
	return a, instance, err
}

// names are the app's name and short name, following the instance's.
func (a AppSettings) names(instance string) (string, string) {
	name := a.Name
	if name == "" {
		name = instance
	}
	short := a.ShortName
	if short == "" {
		short = name
	}
	return name, short
}

// handleAppSettings reads them: GET /api/v1/settings/app. Anyone may, signed
// in or not: the manifest says as much to every visitor, and the app colors
// the phone's status bar from them before anyone signs in.
func (s *Server) handleAppSettings(w http.ResponseWriter, r *http.Request) {
	a, instance, err := s.appSettings()
	if err != nil {
		writeServerError(w, err, "read the app's settings")
		return
	}
	writeJSON(w, http.StatusOK, appSettingsReply{AppSettings: a, InstanceName: instance})
}

// handleSetAppSettings sets them: PUT /api/v1/settings/app.
func (s *Server) handleSetAppSettings(w http.ResponseWriter, r *http.Request) {
	var req AppSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name, req.ShortName = strings.TrimSpace(req.Name), strings.TrimSpace(req.ShortName)
	req.IconImageID = strings.TrimSpace(req.IconImageID)
	switch {
	case len([]rune(req.Name)) > maxAppName:
		writeJSONError(w, http.StatusBadRequest, "The app's name is at most 100 characters")
		return
	case len([]rune(req.ShortName)) > maxAppShortName:
		writeJSONError(w, http.StatusBadRequest, "The app's short name is at most 30 characters")
		return
	}
	for _, c := range []struct{ value, field string }{
		{req.ThemeColor, "theme_color"}, {req.ThemeColorDark, "theme_color_dark"}, {req.BackgroundColor, "background_color"},
	} {
		if _, ok := appicon.ParseColor(c.value); !ok {
			writeJSONError(w, http.StatusBadRequest, c.field+" must be a color as #rrggbb")
			return
		}
	}
	req.ThemeColor, req.ThemeColorDark, req.BackgroundColor = strings.ToLower(req.ThemeColor), strings.ToLower(req.ThemeColorDark), strings.ToLower(req.BackgroundColor)
	if req.IconImageID != "" {
		if _, err := s.store.GetImage(req.IconImageID); errors.Is(err, store.ErrImageNotFound) {
			writeJSONError(w, http.StatusBadRequest, "No image in the library has that icon_image_id")
			return
		} else if err != nil {
			writeServerError(w, err, "read the icon's image")
			return
		}
	}
	if !s.saveAppSettings(w, req) {
		return
	}
	s.handleAppSettings(w, r)
}

func (s *Server) saveAppSettings(w http.ResponseWriter, a AppSettings) bool {
	for key, value := range map[string]string{
		AppNameSetting: a.Name, AppShortNameSetting: a.ShortName, AppThemeSetting: a.ThemeColor,
		AppThemeDarkSetting: a.ThemeColorDark, AppBackgroundSetting: a.BackgroundColor, AppIconSetting: a.IconImageID,
	} {
		if err := s.store.SetSetting(key, value); err != nil {
			writeServerError(w, err, "save the app's settings")
			return false
		}
	}
	return true
}

// handleUploadAppIcon adds a logo to the image library and makes it the
// app's icon: POST /api/v1/settings/app/icon, a multipart form with the
// image in "file", as the library takes them.
func (s *Server) handleUploadAppIcon(w http.ResponseWriter, r *http.Request) {
	img, ok := s.saveUploadedImage(w, r)
	if !ok {
		return
	}
	if err := s.store.SetSetting(AppIconSetting, img.ID); err != nil {
		writeServerError(w, err, "save the app's icon")
		return
	}
	s.handleAppSettings(w, r)
}

// Shortcuts are what a long press on the installed app's icon offers. The
// shopping list's address is the app's own; it opens the Running low report
// until the shopping list (#214) has a page of its own there.
var appShortcuts = []struct{ Name, URL string }{
	{"Scan", "/scan"},
	{"Shopping list", "/shopping"},
	{"Loans", "/loans"},
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

// handleManifest serves the web app manifest: GET /manifest.webmanifest.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	a, instance, err := s.appSettings()
	if err != nil {
		writeServerError(w, err, "read the app's settings")
		return
	}
	name, short := a.names(instance)
	version := iconVersion(a)
	var icons []manifestIcon
	for _, ic := range appicon.Icons {
		if ic.File == "apple-touch-icon.png" {
			continue
		}
		purpose := "any"
		if ic.Maskable {
			purpose = "maskable"
		}
		size := strconv.Itoa(ic.Size) + "x" + strconv.Itoa(ic.Size)
		icons = append(icons, manifestIcon{Src: "/app-icons/" + ic.File + "?v=" + version, Sizes: size, Type: "image/png", Purpose: purpose})
	}
	type shortcut struct {
		Name  string         `json:"name"`
		URL   string         `json:"url"`
		Icons []manifestIcon `json:"icons"`
	}
	shortcuts := make([]shortcut, len(appShortcuts))
	for i, sc := range appShortcuts {
		shortcuts[i] = shortcut{Name: sc.Name, URL: sc.URL, Icons: icons[:1]}
	}
	body, err := json.MarshalIndent(map[string]any{
		"id":               "/",
		"name":             name,
		"short_name":       short,
		"description":      name + ": assets, stock and loans.",
		"lang":             "en-US",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"theme_color":      a.ThemeColor,
		"background_color": a.BackgroundColor,
		"icons":            icons,
		"shortcuts":        shortcuts,
	}, "", "  ")
	if err != nil {
		writeServerError(w, err, "write the manifest")
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

// iconVersion names what the icons are drawn from, so that the manifest's
// addresses for them change when it does and a phone fetches them again.
func iconVersion(a AppSettings) string {
	sum := sha256.Sum256([]byte(a.IconImageID + "\x00" + a.BackgroundColor))
	return hex.EncodeToString(sum[:6])
}

// appIconCache keeps the icons drawn, by what they were drawn from: they
// change only when the settings do, and are fetched by every phone.
type appIconCache struct {
	mu    sync.Mutex
	drawn map[string][]byte
}

func (c *appIconCache) get(key string, draw func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if data, ok := c.drawn[key]; ok {
		return data, nil
	}
	data, err := draw()
	if err != nil {
		return nil, err
	}
	// A handful of icons per setting: start again rather than grow.
	if c.drawn == nil || len(c.drawn) >= 4*len(appicon.Icons) {
		c.drawn = map[string][]byte{}
	}
	c.drawn[key] = data
	return data, nil
}

// handleAppIcon serves one of the icons: GET /app-icons/{file}. Anyone may
// fetch them; a phone does before anyone signs in.
func (s *Server) handleAppIcon(w http.ResponseWriter, r *http.Request) {
	icon, ok := appicon.Named(r.PathValue("file"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	a, _, err := s.appSettings()
	if err != nil {
		writeServerError(w, err, "read the app's settings")
		return
	}
	version := iconVersion(a)
	data, err := s.appIcons.get(version+"/"+icon.File, func() ([]byte, error) {
		background, _ := appicon.ParseColor(a.BackgroundColor)
		return appicon.Draw(s.appLogo(r, a.IconImageID), icon, background)
	})
	if err != nil {
		writeServerError(w, err, "draw the app's icon")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", `"`+version+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// appLogo is the image the icons are drawn from: the one chosen, or BAM!'s
// when none is, or it cannot be read.
func (s *Server) appLogo(r *http.Request, id string) image.Image {
	if id == "" {
		return appicon.BAM()
	}
	data, err := s.images.Read(r.Context(), id)
	if err == nil {
		var img image.Image
		if img, _, err = image.Decode(bytes.NewReader(data)); err == nil {
			return img
		}
	}
	log.Printf("drawing the app's icon from image %s, BAM!'s instead: %v", id, err)
	return appicon.BAM()
}
