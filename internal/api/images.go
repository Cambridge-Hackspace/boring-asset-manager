package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register decoders for image.Decode
	_ "image/png"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

const (
	// MaxImageBytes caps an uploaded image file.
	MaxImageBytes = 2 << 20
	// MaxImagePixels caps each side of an uploaded image, bounding the memory
	// a decode can take.
	MaxImagePixels = 4096
)

// imageUploadLimit leaves headroom over MaxImageBytes for multipart framing.
const imageUploadLimit = MaxImageBytes + 64<<10

var imageContentTypes = map[string]string{"png": "image/png", "jpeg": "image/jpeg"}

func (s *Server) handleListImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.store.ListImages()
	if err != nil {
		writeServerError(w, err, "list images")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

func (s *Server) handleGetImage(w http.ResponseWriter, r *http.Request) {
	img, err := s.store.GetImage(r.PathValue("id"))
	if errors.Is(err, store.ErrImageNotFound) {
		writeJSONError(w, http.StatusNotFound, "Image not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "query image")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"image": img})
}

func (s *Server) handleGetImageContent(w http.ResponseWriter, r *http.Request) {
	img, err := s.store.GetImage(r.PathValue("id"))
	if errors.Is(err, store.ErrImageNotFound) {
		writeJSONError(w, http.StatusNotFound, "Image not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "query image")
		return
	}
	data, err := s.images.Read(img.ID)
	if err != nil {
		writeServerError(w, err, "read the image file")
		return
	}
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleUploadImage accepts a multipart form with the image in the "file" part.
func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Images are limited to %d bytes", MaxImageBytes))
			return
		}
		writeJSONError(w, http.StatusBadRequest, `Expected a multipart form with the image in a "file" field`)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to read the uploaded file")
		return
	}
	if len(data) > MaxImageBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Images are limited to %d bytes", MaxImageBytes))
		return
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	contentType, supported := imageContentTypes[format]
	if err != nil || !supported {
		writeJSONError(w, http.StatusBadRequest, "Only PNG and JPEG images are supported")
		return
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImagePixels || cfg.Height > MaxImagePixels {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Images must be at most %d×%d pixels", MaxImagePixels, MaxImagePixels))
		return
	}
	// A full decode catches truncated or corrupt files now rather than at
	// render time.
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		writeJSONError(w, http.StatusBadRequest, "The image could not be decoded")
		return
	}

	sum := sha256.Sum256(data)
	img := &models.Image{
		ID:          uuid.NewString(),
		Filename:    cleanFilename(header.Filename),
		ContentType: contentType,
		WidthPx:     cfg.Width,
		HeightPx:    cfg.Height,
		ByteSize:    int64(len(data)),
		SHA256:      hex.EncodeToString(sum[:]),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.images.Save(img.ID, data); err != nil {
		writeServerError(w, err, "store the image")
		return
	}
	if err := s.store.CreateImage(img); err != nil {
		_ = s.images.Remove(img.ID)
		writeServerError(w, err, "record the image")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"image": img})
}

func (s *Server) handleDeleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.DeleteImage(id)
	var inUse *store.ImageInUseError
	switch {
	case errors.Is(err, store.ErrImageNotFound):
		writeJSONError(w, http.StatusNotFound, "Image not found")
		return
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, ErrorResponse{
			Error:   "Image is used by templates; remove it from them first",
			Details: map[string]any{"template_ids": inUse.TemplateIDs},
		})
		return
	case err != nil:
		writeServerError(w, err, "delete image")
		return
	}
	// The record is gone, so a leftover file is only wasted space.
	if err := s.images.Remove(id); err != nil {
		log.Printf("removing image file %s: %v", id, err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Image deleted successfully"})
}

// cleanFilename keeps a display name for an upload, dropping any directory
// part a client sent, control characters and bytes that are not UTF-8, and
// anything past 255 characters.
func cleanFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(name, ""))
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	if name == "" || name == "." || name == "/" {
		return "image"
	}
	if r := []rune(name); len(r) > textcheck.MaxName {
		name = string(r[:textcheck.MaxName])
	}
	return name
}
