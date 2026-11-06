package attachments

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	img.Set(1, 1, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestKindsAreDecidedByContent: what a file is comes from its content; its
// name only says which text it is, or an extension allowed for content
// that shows no type of its own.
func TestKindsAreDecidedByContent(t *testing.T) {
	pdf := []byte("%PDF-1.7\n%%EOF\n")
	zip := []byte("PK\x03\x04\x14\x00\x00\x00rest")
	html := []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	for _, c := range []struct {
		name     string
		data     []byte
		allowed  []string
		kind     string
		typ      string
		refusing bool
	}{
		{name: "photo.png", data: testPNG(t), kind: models.AttachmentImage, typ: "image/png"},
		{name: "photo.txt", data: testPNG(t), kind: models.AttachmentImage, typ: "image/png"},
		{name: "manual.pdf", data: pdf, kind: models.AttachmentDocument, typ: "application/pdf"},
		{name: "manual", data: pdf, kind: models.AttachmentDocument, typ: "application/pdf"},
		{name: "notes.txt", data: []byte("Oil it.\r\n\tWeekly.\f"), kind: models.AttachmentDocument, typ: "text/plain; charset=utf-8"},
		{name: "README.MD", data: []byte("\xef\xbb\xbf# Read me"), kind: models.AttachmentDocument, typ: "text/markdown; charset=utf-8"},
		{name: "parts.csv", data: []byte("part,count\nM3,40\n"), kind: models.AttachmentDocument, typ: "text/csv; charset=utf-8"},
		{name: "page.txt", data: html, kind: models.AttachmentDocument, typ: "text/plain; charset=utf-8"},
		{name: "fake.pdf", data: []byte("not one"), refusing: true},
		{name: "fake.png", data: []byte("not one"), refusing: true},
		{name: "binary.txt", data: []byte{'a', 0, 'b'}, refusing: true},
		{name: "latin1.csv", data: []byte("caf\xe9"), refusing: true},
		{name: "page.html", data: html, refusing: true},
		{name: "parts.zip", data: zip, refusing: true},
		{name: "parts.zip", data: zip, allowed: []string{"application/zip"}, kind: models.AttachmentOther, typ: "application/zip"},
		{name: "page.html", data: html, allowed: []string{"text/html"}, kind: models.AttachmentOther, typ: "text/html"},
		{name: "model.stl", data: []byte{0, 1, 2, 3}, allowed: []string{".stl"}, kind: models.AttachmentOther, typ: "application/octet-stream"},
		{name: "model.stl", data: zip, allowed: []string{".stl"}, refusing: true},
		{name: "model", data: []byte{0, 1, 2, 3}, allowed: []string{""}, refusing: true},
	} {
		p, err := Prepare(c.data, c.name, c.allowed)
		if c.refusing {
			if err == nil {
				t.Errorf("%s (%v): kept as %+v", c.name, c.allowed, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if p.Kind != c.kind || p.ContentType != c.typ {
			t.Errorf("%s: %s %s, want %s %s", c.name, p.Kind, p.ContentType, c.kind, c.typ)
		}
		if (p.Kind == models.AttachmentImage) != (p.Thumb != nil) {
			t.Errorf("%s: thumbnail %d bytes", c.name, len(p.Thumb))
		}
	}
}

// TestOnlyImagesAreShown: images show, as photos do; everything else is a
// download, and other files are downloaded as bytes, whatever they are.
func TestOnlyImagesAreShown(t *testing.T) {
	for _, c := range []struct {
		a      models.Attachment
		typ    string
		inline bool
	}{
		{models.Attachment{Kind: models.AttachmentImage, ContentType: "image/png"}, "image/png", true},
		{models.Attachment{Kind: models.AttachmentDocument, ContentType: "application/pdf"}, "application/pdf", false},
		{models.Attachment{Kind: models.AttachmentOther, ContentType: "text/html"}, "application/octet-stream", false},
	} {
		if typ, inline := Served(c.a); typ != c.typ || inline != c.inline {
			t.Errorf("%+v: %s %v", c.a, typ, inline)
		}
	}
}
