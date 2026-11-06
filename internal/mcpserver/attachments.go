package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Attachments (docs/data-model.md#attachments): files on assets and
// categories. They are added over MCP as photos are, base64-encoded, up to
// the size people may upload.

type ownerIn struct {
	Asset      string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
}

// attachmentsPath is the route of an asset's or a category's attachments,
// with rest after it.
func attachmentsPath(ctx context.Context, e *env, asset, categoryID string, rest ...string) (string, error) {
	switch {
	case asset != "" && categoryID != "":
		return "", errors.New("give asset or category_id, not both")
	case asset != "":
		id, err := e.assetID(ctx, asset)
		if err != nil {
			return "", err
		}
		return assetPath(id, append([]string{"/attachments"}, rest...)...), nil
	case categoryID != "":
		return "/api/v1/categories/" + url.PathEscape(categoryID) + "/attachments" + strings.Join(rest, ""), nil
	}
	return "", errors.New("give the asset, or the category_id, the attachments are on")
}

type attachmentView struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind" jsonschema:"image, document or other"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	ByteSize    int64     `json:"byte_size"`
	AddedBy     string    `json:"added_by_name,omitempty" jsonschema:"who added it, to those who may see it"`
	CreatedAt   time.Time `json:"created_at"`
}

type attachmentsOut struct {
	Attachments []attachmentView `json:"attachments"`
}

type attachmentsBody struct {
	Attachments []attachmentView `json:"attachments"`
	Attachment  *attachmentView  `json:"attachment"`
}

type readAttachmentIn struct {
	Asset        string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID   string `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	AttachmentID string `json:"attachment_id"`
	Thumbnail    bool   `json:"thumbnail,omitempty" jsonschema:"an image's thumbnail rather than the image"`
}

type addAttachmentIn struct {
	Asset       string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID  string `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	Data        string `json:"data" jsonschema:"the file, base64-encoded: a JPEG or PNG image, a PDF, plain text, Markdown or CSV, or a type the instance allows"`
	Filename    string `json:"filename" jsonschema:"its file's name, with its extension, such as manual.pdf"`
	Name        string `json:"name,omitempty" jsonschema:"what to call it; its file's name if not given"`
	Description string `json:"description,omitempty"`
}

type updateAttachmentIn struct {
	Asset        string  `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID   string  `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	AttachmentID string  `json:"attachment_id"`
	Name         *string `json:"name,omitempty"`
	Description  *string `json:"description,omitempty"`
}

type reorderAttachmentsIn struct {
	Asset         string   `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID    string   `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	AttachmentIDs []string `json:"attachment_ids" jsonschema:"every attachment's ID, in order; an asset's first image is the one its page and lists show"`
}

type removeAttachmentIn struct {
	Asset        string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID   string `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	AttachmentID string `json:"attachment_id"`
}

type attachmentOut struct {
	Attachment attachmentView `json:"attachment"`
}

func attachmentTools() []tool {
	anyChange := []string{rbac.PermAssetsWrite, rbac.PermCategoriesManage}
	return []tool{
		define(&mcp.Tool{
			Name:        "list_attachments",
			Description: "The files attached to an asset or a category, in order: images (an asset's photos), documents and other files.",
			Annotations: readOnly("Attachments"),
		}, []string{"GET /api/v1/assets/{id}/attachments", "GET /api/v1/categories/{id}/attachments"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in ownerIn) (reply[attachmentsOut], error) {
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID)
			if err != nil {
				return reply[attachmentsOut]{}, err
			}
			var body attachmentsBody
			if err := e.call(ctx, http.MethodGet, path, nil, &body); err != nil {
				return reply[attachmentsOut]{}, err
			}
			out := attachmentsOut{Attachments: body.Attachments}
			if out.Attachments == nil {
				out.Attachments = []attachmentView{}
			}
			return say(out, "%d attachments.", len(out.Attachments))
		}),
		define(&mcp.Tool{
			Name:        "read_attachment",
			Description: "One attachment's file: an image to look at, text as text, and anything else as a file to keep.",
			Annotations: readOnly("Read an attachment"),
		}, []string{"GET /api/v1/assets/{id}/attachments/{attachment_id}", "GET /api/v1/categories/{id}/attachments/{attachment_id}"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in readAttachmentIn) (reply[attachmentsOut], error) {
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID, "/", url.PathEscape(in.AttachmentID))
			if err != nil {
				return reply[attachmentsOut]{}, err
			}
			if in.Thumbnail {
				path += "?size=thumb"
			}
			res := e.api(ctx, http.MethodGet, path, nil, "")
			if res.Status != http.StatusOK {
				return reply[attachmentsOut]{}, apiError(res)
			}
			r, _ := say(attachmentsOut{Attachments: []attachmentView{}}, "%d bytes.", len(res.Body))
			if mime := res.Header.Get("Content-Type"); strings.HasPrefix(mime, "image/") {
				r.extra = []mcp.Content{&mcp.ImageContent{Data: res.Body, MIMEType: mime}}
			} else {
				r.extra = []mcp.Content{fileContent("bam://attachments/"+url.PathEscape(in.AttachmentID), res)}
			}
			return r, nil
		}),
		define(&mcp.Tool{
			Name:        "add_attachment",
			Description: "Attach a file to an asset or a category, base64-encoded, up to the size people may upload. Its type is checked by its content.",
			Annotations: changes("Add an attachment", false),
		}, []string{"POST /api/v1/assets/{id}/attachments", "POST /api/v1/categories/{id}/attachments"}, anyChange, func(ctx context.Context, e *env, in addAttachmentIn) (reply[attachmentOut], error) {
			data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Data))
			if err != nil {
				return reply[attachmentOut]{}, fmt.Errorf("data must be base64: %v", err)
			}
			if strings.TrimSpace(in.Filename) == "" {
				return reply[attachmentOut]{}, errors.New("filename is required, with its extension")
			}
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID)
			if err != nil {
				return reply[attachmentOut]{}, err
			}
			body, contentType, err := multipartBody("file", in.Filename, data, map[string]string{"name": in.Name, "description": in.Description})
			if err != nil {
				return reply[attachmentOut]{}, err
			}
			res := e.api(ctx, http.MethodPost, path, body, contentType)
			if res.Status < 200 || res.Status > 299 {
				return reply[attachmentOut]{}, apiError(res)
			}
			var added attachmentsBody
			if err := json.Unmarshal(res.Body, &added); err != nil || added.Attachment == nil {
				return reply[attachmentOut]{}, fmt.Errorf("reading the answer: %v", err)
			}
			return say(attachmentOut{Attachment: *added.Attachment}, "Attached %s, as %s.", added.Attachment.Name, added.Attachment.Kind)
		}),
		define(&mcp.Tool{
			Name:        "update_attachment",
			Description: "Rename an attachment, or describe it.",
			Annotations: changes("Rename or describe an attachment", true),
		}, []string{"PUT /api/v1/assets/{id}/attachments/{attachment_id}", "PUT /api/v1/categories/{id}/attachments/{attachment_id}"}, anyChange, func(ctx context.Context, e *env, in updateAttachmentIn) (reply[attachmentOut], error) {
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID, "/", url.PathEscape(in.AttachmentID))
			if err != nil {
				return reply[attachmentOut]{}, err
			}
			var res struct {
				Attachment attachmentView `json:"attachment"`
			}
			if err := e.call(ctx, http.MethodPut, path, map[string]any{"name": in.Name, "description": in.Description}, &res); err != nil {
				return reply[attachmentOut]{}, err
			}
			return say(attachmentOut{Attachment: res.Attachment}, "Saved %s.", res.Attachment.Name)
		}),
		define(&mcp.Tool{
			Name:        "reorder_attachments",
			Description: "Put the attachments of an asset or a category in order.",
			Annotations: changes("Reorder attachments", true),
		}, []string{"PUT /api/v1/assets/{id}/attachments", "PUT /api/v1/categories/{id}/attachments"}, anyChange, func(ctx context.Context, e *env, in reorderAttachmentsIn) (reply[attachmentsOut], error) {
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID)
			if err != nil {
				return reply[attachmentsOut]{}, err
			}
			var body attachmentsBody
			if err := e.call(ctx, http.MethodPut, path, map[string]any{"attachment_ids": in.AttachmentIDs}, &body); err != nil {
				return reply[attachmentsOut]{}, err
			}
			return say(attachmentsOut{Attachments: body.Attachments}, "Reordered %d attachments.", len(body.Attachments))
		}),
		define(&mcp.Tool{
			Name:        "remove_attachment",
			Description: "Remove an attachment from an asset or a category, with its file, for good.",
			Annotations: destroys("Remove an attachment", true),
		}, []string{"DELETE /api/v1/assets/{id}/attachments/{attachment_id}", "DELETE /api/v1/categories/{id}/attachments/{attachment_id}"}, anyChange, func(ctx context.Context, e *env, in removeAttachmentIn) (reply[attachmentsOut], error) {
			path, err := attachmentsPath(ctx, e, in.Asset, in.CategoryID, "/", url.PathEscape(in.AttachmentID))
			if err != nil {
				return reply[attachmentsOut]{}, err
			}
			var body attachmentsBody
			if err := e.call(ctx, http.MethodDelete, path, nil, &body); err != nil {
				return reply[attachmentsOut]{}, err
			}
			return say(attachmentsOut{Attachments: body.Attachments}, "Removed it; %d attachments are left.", len(body.Attachments))
		}),
	}
}
