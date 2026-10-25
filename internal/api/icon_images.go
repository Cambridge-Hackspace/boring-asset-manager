package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/raster"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// defaultIconHeight is how high an icon image is drawn unless asked, and
// maxIconHeight the most it may be asked to be.
const (
	defaultIconHeight = 64
	maxIconHeight     = 1024
)

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// handleIconImage draws an icon as an image (README §3.7), for where a
// page's drawing will not do: an export, a notification, a chat.
// GET /api/v1/icons/image with one of name, category (the icon it is drawn
// with, its own or inherited) or asset (its own, or its categories');
// format svg (the default) or png; theme light (the default) or dark;
// height in pixels, 1 to 1024, 64 unless given; and color, what the
// category token paints in, the category's own unless given. A name the
// build does not draw is drawn as the unknown icon, and X-Icon-Missing
// names it.
func (s *Server) handleIconImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	given := 0
	for _, k := range []string{"name", "category", "asset"} {
		if q.Get(k) != "" {
			given++
		}
	}
	if given != 1 {
		writeJSONError(w, http.StatusBadRequest, "give one of name, category or asset")
		return
	}
	format := q.Get("format")
	if format == "" {
		format = "svg"
	}
	if format != "svg" && format != "png" {
		writeJSONError(w, http.StatusBadRequest, "format is svg or png")
		return
	}
	theme := q.Get("theme")
	if theme == "" {
		theme = "light"
	}
	if theme != "light" && theme != "dark" {
		writeJSONError(w, http.StatusBadRequest, "theme is light or dark")
		return
	}
	height := defaultIconHeight
	if v := q.Get("height"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxIconHeight {
			writeJSONError(w, http.StatusBadRequest, "height is a whole number of pixels, 1 to "+strconv.Itoa(maxIconHeight))
			return
		}
		height = n
	}
	color := q.Get("color")
	if color != "" && !hexColor.MatchString(color) {
		writeJSONError(w, http.StatusBadRequest, "color is #rgb, #rrggbb or #rrggbbaa")
		return
	}

	// which icon, and its category's color
	var name, own string
	switch {
	case q.Get("asset") != "":
		a, ok := s.assetFor(w, r, q.Get("asset"), rbac.PermAssetsRead)
		if !ok {
			return
		}
		if a.ResolvedIcon == nil {
			writeJSONError(w, http.StatusNotFound, "this asset has no icon")
			return
		}
		name, own = a.ResolvedIcon.Name, a.ResolvedIcon.Color
	case q.Get("category") != "":
		cat, err := s.store.GetCategoryByID(q.Get("category"))
		if errors.Is(err, store.ErrCategoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "Category not found")
			return
		}
		if err != nil {
			writeServerError(w, err, "query category")
			return
		}
		shown, err := s.store.ShownCategoryIcons()
		if err != nil {
			writeServerError(w, err, "read the category icons")
			return
		}
		if name = shown[cat.ID]; name == "" {
			writeJSONError(w, http.StatusNotFound, "this category has no icon")
			return
		}
		own = cat.ColorHex
	default:
		name = q.Get("name")
		icons, err := s.store.ListCategoryIcons()
		if err != nil {
			writeServerError(w, err, "read the category icons")
			return
		}
		for _, ic := range icons {
			if ic.IconName == name {
				if cat, err := s.store.GetCategoryByID(ic.CategoryID); err == nil {
					own = cat.ColorHex
				}
			}
		}
	}
	if color == "" {
		color = own
	}

	build, err := s.store.CurrentIconBuild()
	if err != nil {
		writeServerError(w, err, "read the icon build")
		return
	}
	var icon *ir.Icon
	hash := "none"
	if build != nil {
		hash = build.Hash
		doc, err := ir.Parse(build.IR)
		if err != nil {
			writeServerError(w, err, "read the icon build")
			return
		}
		icon = doc.Icon(name)
	}
	if icon == nil {
		unknown := isolex.Unknown()
		icon = &unknown
		w.Header().Set("X-Icon-Missing", name)
	}

	palette, err := s.store.GetIconPalette()
	if err != nil {
		writeServerError(w, err, "read the icon palette")
		return
	}
	colors := map[string]string{}
	for _, c := range palette {
		colors[c.Token] = c.Light
		if theme == "dark" {
			colors[c.Token] = c.Dark
		}
	}
	// a category with no color paints as muted ink
	if color == "" {
		color = colors["ink-muted"]
	}
	colors[store.CategoryToken] = color

	var body bytes.Buffer
	contentType := "image/svg+xml"
	if format == "png" {
		contentType = "image/png"
		if err := raster.Encode(&body, icon, svg.Palette(colors), height); err != nil {
			writeServerError(w, err, "draw the icon")
			return
		}
	} else {
		body.WriteString(svg.Sized(icon, svg.Palette(colors), float64(height)))
	}
	sum := sha256.Sum256(append([]byte(hash+"\x00"), body.Bytes()...))
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body.Bytes())
}
