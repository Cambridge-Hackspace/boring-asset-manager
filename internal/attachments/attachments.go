// Package attachments prepares files attached to assets and categories
// (docs/data-model.md#attachments): it decides each upload's kind and type
// from its content, not from its name alone, and makes an image ready as
// package photos makes a photo.
package attachments

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/photos"
)

// ErrType refuses a file of a type that may not be attached.
var ErrType = errors.New("only JPEG and PNG images, PDFs, and plain text, Markdown and CSV files can be attached")

// ErrImageTooBig refuses an image larger than photos may be.
var ErrImageTooBig = fmt.Errorf("images are limited to %d MB", photos.MaxBytes>>20)

// Prepared is an upload ready to store: its kind and type, its bytes, and,
// for an image, its thumbnail and size.
type Prepared struct {
	Kind        string
	ContentType string
	Full, Thumb []byte
	Width       int
	Height      int
}

// documents are the documents' types by extension.
var documents = map[string]string{
	".pdf":      "application/pdf",
	".txt":      "text/plain; charset=utf-8",
	".text":     "text/plain; charset=utf-8",
	".log":      "text/plain; charset=utf-8",
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
}

// pdfMagic starts every PDF.
var pdfMagic = []byte("%PDF-")

// Prepare decides what an upload is, by its content and its name, and
// makes what is stored of it. An image (a JPEG or PNG) is decoded and
// encoded again, as a photo is. A document is a PDF, whose content must
// begin as a PDF does, or plain text, Markdown or CSV, whose content must
// be text. Anything else is other, and only of a type allowed lists: a
// media type its content shows, or an extension, for content that shows
// no type of its own.
func Prepare(data []byte, filename string, allowed []string) (*Prepared, error) {
	sniffed := baseType(http.DetectContentType(data))
	if sniffed == "image/jpeg" || sniffed == "image/png" {
		if len(data) > photos.MaxBytes {
			return nil, ErrImageTooBig
		}
		p, err := photos.Prepare(data)
		if err != nil {
			return nil, err
		}
		return &Prepared{Kind: models.AttachmentImage, ContentType: p.ContentType, Full: p.Full, Thumb: p.Thumb, Width: p.Width, Height: p.Height}, nil
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if bytes.HasPrefix(data, pdfMagic) {
		return &Prepared{Kind: models.AttachmentDocument, ContentType: "application/pdf", Full: data}, nil
	}
	if t, ok := documents[ext]; ok && ext != ".pdf" && IsText(data) {
		return &Prepared{Kind: models.AttachmentDocument, ContentType: t, Full: data}, nil
	}
	generic := sniffed == "application/octet-stream" || sniffed == "text/plain"
	for _, a := range allowed {
		if a == sniffed || generic && a == ext && ext != "" {
			return &Prepared{Kind: models.AttachmentOther, ContentType: sniffed, Full: data}, nil
		}
	}
	if _, ok := documents[ext]; ok {
		return nil, fmt.Errorf("%w: this is named %s, but its content is not one", ErrType, ext)
	}
	if len(allowed) > 0 {
		return nil, fmt.Errorf("%w, as can %s", ErrType, strings.Join(allowed, ", "))
	}
	return nil, ErrType
}

// baseType is a media type without its parameters.
func baseType(t string) string {
	if base, _, err := mime.ParseMediaType(t); err == nil {
		return base
	}
	return t
}

// IsText reports whether data is text: UTF-8, after any byte order mark,
// with no control characters but tabs, line and page breaks.
func IsText(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' {
			return false
		}
	}
	return true
}

// Served is how an attachment is served: its type and disposition. Images
// are shown, as photos always were. Documents are downloaded, as
// themselves; other files are downloaded as bytes, whatever their type, so
// a browser never runs one.
func Served(a models.Attachment) (contentType string, inline bool) {
	switch a.Kind {
	case models.AttachmentImage:
		return a.ContentType, true
	case models.AttachmentDocument:
		return a.ContentType, false
	}
	return "application/octet-stream", false
}
