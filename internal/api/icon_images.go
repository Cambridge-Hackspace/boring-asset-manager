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
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
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

// handleIconImage draws an icon as an image (docs/icons.md), for where a
// page's drawing will not do: an export, a notification, a chat.
// GET /api/v1/icons/image with one of name, category (the icon it is drawn
// with, its own or inherited) or asset (its own, or its categories'), each
// for rendition, the screen's unless given;
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
	opts, ok := imageOptionsOf(w, r, defaultIconHeight, maxIconHeight)
	if !ok {
		return
	}
	color := q.Get("color")
	if rn := rendition(r); rn != "" && !models.ValidRendition(rn) {
		writeJSONError(w, http.StatusBadRequest, "rendition is screen or label")
		return
	}
	if color != "" && !hexColor.MatchString(color) {
		writeJSONError(w, http.StatusBadRequest, "color is #rgb, #rrggbb or #rrggbbaa")
		return
	}

	// which icon, and its category's color; an asset's drawn with its texts
	var name, own string
	var drawn []byte
	switch {
	case q.Get("asset") != "":
		a, ok := s.assetFor(w, r, q.Get("asset"), rbac.PermAssetsRead)
		if !ok {
			return
		}
		icon := a.ResolvedIcons[rendition(r)]
		if rendition(r) == "" {
			icon = a.ResolvedIcon
		}
		if icon == nil {
			writeJSONError(w, http.StatusNotFound, "this asset has no icon")
			return
		}
		name, own = icon.Name, icon.Color
		drawn = icon.Drawn[name]
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
		shown, err := s.store.ShownCategoryIcons(rendition(r))
		if errors.Is(err, store.ErrUnknownRendition) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if drawn != nil {
		if d, err := ir.ParseIcon(drawn); err == nil {
			icon = d
		}
	}
	if icon == nil {
		unknown := isolex.Unknown()
		icon = &unknown
		w.Header().Set("X-Icon-Missing", name)
	}

	colors, err := s.themeColors(opts.theme)
	if err != nil {
		writeServerError(w, err, "read the icon palette")
		return
	}
	// a category with no color paints as muted ink
	if color == "" {
		color = colors["ink-muted"]
	}
	colors[store.CategoryToken] = color
	writeIconImage(w, r, hash, icon, colors, opts)
}

// imageOptions is how an image is asked to be drawn: format svg or png,
// theme light or dark, and height in pixels.
type imageOptions struct {
	format, theme string
	height        int
}

// imageOptionsOf reads an image's format, theme and height from the
// query, answering a bad one itself.
func imageOptionsOf(w http.ResponseWriter, r *http.Request, defaultHeight, maxHeight int) (imageOptions, bool) {
	q := r.URL.Query()
	opts := imageOptions{format: q.Get("format"), theme: q.Get("theme"), height: defaultHeight}
	if opts.format == "" {
		opts.format = "svg"
	}
	if opts.format != "svg" && opts.format != "png" {
		writeJSONError(w, http.StatusBadRequest, "format is svg or png")
		return opts, false
	}
	if opts.theme == "" {
		opts.theme = "light"
	}
	if opts.theme != "light" && opts.theme != "dark" {
		writeJSONError(w, http.StatusBadRequest, "theme is light or dark")
		return opts, false
	}
	if v := q.Get("height"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxHeight {
			writeJSONError(w, http.StatusBadRequest, "height is a whole number of pixels, 1 to "+strconv.Itoa(maxHeight))
			return opts, false
		}
		opts.height = n
	}
	return opts, true
}

// themeColors is the palette's color for each token in a theme.
func (s *Server) themeColors(theme string) (map[string]string, error) {
	palette, err := s.store.GetIconPalette()
	if err != nil {
		return nil, err
	}
	colors := map[string]string{}
	for _, c := range palette {
		colors[c.Token] = c.Light
		if theme == "dark" {
			colors[c.Token] = c.Dark
		}
	}
	return colors, nil
}

// writeIconImage draws an icon in colors as opts ask, tagged by the build
// it is drawn from and what is drawn.
func writeIconImage(w http.ResponseWriter, r *http.Request, hash string, icon *ir.Icon, colors map[string]string, opts imageOptions) {
	var body bytes.Buffer
	contentType := "image/svg+xml"
	if opts.format == "png" {
		contentType = "image/png"
		if err := raster.Encode(&body, icon, svg.Palette(colors), opts.height); err != nil {
			writeServerError(w, err, "draw the image")
			return
		}
	} else {
		body.WriteString(svg.Sized(icon, svg.Palette(colors), float64(opts.height)))
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
