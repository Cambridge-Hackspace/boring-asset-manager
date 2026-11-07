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

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Comments (docs/comments.md): notes in Markdown on assets, categories,
// loans and flows' filings. An assistant reads them, adds them, and edits
// and deletes its own, as a person does.

// commentOnIn names what comments are on: an asset, a category, a loan, or
// an asset with the flow that filed it.
type commentOnIn struct {
	Asset      string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; with flow_id, its filing by that flow"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"the category, by ID"`
	LoanID     string `json:"loan_id,omitempty" jsonschema:"the loan, by ID, as list_loans and an asset's loan give it"`
	FlowID     string `json:"flow_id,omitempty" jsonschema:"with asset: the flow whose filing of it the comments are on, as list_filings gives it"`
}

// commentsPath is the route of the comments on what in names, with rest
// after it.
func commentsPath(ctx context.Context, e *env, in commentOnIn, rest string) (string, error) {
	given := 0
	for _, o := range []string{in.Asset, in.CategoryID, in.LoanID} {
		if o != "" {
			given++
		}
	}
	switch {
	case given > 1:
		return "", errors.New("give one of asset, category_id and loan_id")
	case in.FlowID != "" && in.Asset == "":
		return "", errors.New("flow_id names a filing of an asset: give the asset too")
	case in.CategoryID != "":
		return pathOf("/api/v1/categories/{id}/comments", in.CategoryID) + rest, nil
	case in.LoanID != "":
		return pathOf("/api/v1/loans/{id}/comments", in.LoanID) + rest, nil
	case in.Asset != "":
		id, err := e.assetID(ctx, in.Asset)
		if err != nil {
			return "", err
		}
		if in.FlowID != "" {
			return pathOf("/api/v1/assets/{id}/filings/{flow_id}/comments", id, in.FlowID) + rest, nil
		}
		return assetPath(id, "/comments") + rest, nil
	}
	return "", errors.New("give the asset, the category_id or the loan_id the comments are on")
}

type listCommentsIn struct {
	commentOnIn
	Own bool `json:"own,omitempty" jsonschema:"only its own comments, without its categories' and their ancestors'"`
}

type addCommentIn struct {
	commentOnIn
	Body string `json:"body" jsonschema:"the comment, in Markdown: headings, lists, tables, code, links, images within BAM! and task lists; <@id> mentions a person by their user ID, as list_comment_people gives it"`
}

type editCommentIn struct {
	ID   string `json:"id"`
	Body string `json:"body" jsonschema:"the comment's new text, in Markdown"`
}

type searchCommentsIn struct {
	Query string `json:"query" jsonschema:"words the comments hold"`
}

type commentAttachmentIn struct {
	Asset      string `json:"asset,omitempty" jsonschema:"the asset, by ID or code; or give category_id"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"the category, by ID; or give asset"`
	Data       string `json:"data" jsonschema:"the file, base64-encoded: a JPEG or PNG image, a PDF, plain text, Markdown or CSV, or a type the instance allows"`
	Filename   string `json:"filename" jsonschema:"its file's name, with its extension, such as belt.jpg"`
	Name       string `json:"name,omitempty" jsonschema:"what to call it; its file's name if not given"`
}

// commentsSaid sums up a list of comments.
func commentsSaid(out map[string]any) string {
	list, _ := out["comments"].([]any)
	own, inherited, deleted := 0, 0, 0
	for _, c := range list {
		m, _ := c.(map[string]any)
		switch {
		case m["deleted_at"] != nil:
			deleted++
		case m["category"] != nil:
			inherited++
		default:
			own++
		}
	}
	return fmt.Sprintf("%d comments of its own, %d from its categories, and %d deleted.", own, inherited, deleted)
}

func commentTools() []tool {
	read := []string{rbac.PermAssetsRead}
	write := []string{rbac.PermCommentsWrite}
	return []tool{
		define(&mcp.Tool{Name: "list_comments", Description: "The comments on an asset, a category, a loan or a flow's filing of an asset, oldest first, each with its Markdown and its HTML. " +
			"An asset's and a category's are followed by those on its categories and their ancestors, nearest first, each naming its category, unless own is set. " +
			"A deleted comment shows who deleted it, and its text is kept in comment_history.",
			Annotations: readOnly("List comments")},
			[]string{"GET /api/v1/assets/{id}/comments", "GET /api/v1/categories/{id}/comments", "GET /api/v1/loans/{id}/comments", "GET /api/v1/assets/{id}/filings/{flow_id}/comments"}, read,
			func(ctx context.Context, e *env, in listCommentsIn) (reply[map[string]any], error) {
				rest := ""
				if in.Own {
					rest = "?own=1"
				}
				path, err := commentsPath(ctx, e, in.commentOnIn, rest)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%s", commentsSaid(out))
			}),
		define(&mcp.Tool{Name: "add_comment", Description: "Write a comment, in Markdown, on an asset, a category, a loan or a flow's filing of an asset. " +
			"Channels subscribed to comment.added are told. To attach a file, use attach_to_comment first and put the Markdown it gives in the comment. " +
			"To mention someone, put <@id> with their user ID from list_comment_people: they are emailed once, if they may read it and have not turned mentions off.",
			Annotations: changes("Add a comment", false)},
			[]string{"POST /api/v1/assets/{id}/comments", "POST /api/v1/categories/{id}/comments", "POST /api/v1/loans/{id}/comments", "POST /api/v1/assets/{id}/filings/{flow_id}/comments"}, write,
			func(ctx context.Context, e *env, in addCommentIn) (reply[map[string]any], error) {
				if strings.TrimSpace(in.Body) == "" {
					return reply[map[string]any]{}, errors.New("body is required: the comment's text")
				}
				path, err := commentsPath(ctx, e, in.commentOnIn, "")
				if err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPost, path, map[string]any{"body": in.Body}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Commented.")
			}),
		simple(&mcp.Tool{Name: "get_comment", Description: "One comment, with its Markdown, its HTML, what it is on, and whether the caller may edit or delete it.",
			Annotations: readOnly("Get a comment")}, "GET /api/v1/comments/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/comments/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "The comment." }),
		simple(&mcp.Tool{Name: "edit_comment", Description: "Replace the text of a comment the caller wrote. It shows as edited, and what it said before is kept in comment_history. " +
			"Only the people the edit mentions for the first time, with <@id>, are emailed.",
			Annotations: changes("Edit a comment", true)}, "PUT /api/v1/comments/{id}", write,
			func(in editCommentIn) (request, error) {
				return request{path: pathOf("/api/v1/comments/{id}", in.ID), body: map[string]any{"body": in.Body}}, need(in.ID, "id")
			},
			func(editCommentIn, map[string]any) string { return "Edited the comment." }),
		simple(&mcp.Tool{Name: "delete_comment", Description: "Delete a comment the caller wrote, or with comments.moderate anyone's. \"Deleted by\" the caller shows in its place, and its text is kept in comment_history.",
			Annotations: destroys("Delete a comment", false)}, "DELETE /api/v1/comments/{id}", []string{rbac.PermCommentsWrite, rbac.PermCommentsModerate},
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/comments/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the comment." }),
		simple(&mcp.Tool{Name: "comment_history", Description: "A comment's history, oldest first: when it was written, each edit with what it said before, and its deletion with what it said.",
			Annotations: readOnly("Comment history")}, "GET /api/v1/comments/{id}/history", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/comments/{id}/history", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%d events.", count(out, "events")) }),
		simple(&mcp.Tool{Name: "search_comments", Description: "Find the comments, not deleted, whose text holds some words, newest first, among those the caller may read, each naming what it is on.",
			Annotations: readOnly("Search comments")}, "GET /api/v1/comments", read,
			func(in searchCommentsIn) (request, error) {
				if strings.TrimSpace(in.Query) == "" {
					return request{}, errors.New("query is required: the words to find")
				}
				return request{path: "/api/v1/comments", query: url.Values{"q": {in.Query}}}, nil
			},
			func(_ searchCommentsIn, out map[string]any) string {
				return fmt.Sprintf("%d comments.", count(out, "comments"))
			}),
		func() tool {
			t := define(&mcp.Tool{Name: "list_comment_people", Description: "The people a comment on an asset, a category, a loan or a flow's filing of an asset may mention: those who may read it, by name, with the user ID to mention each by, as <@id>. " +
				"It takes comments.write and assets.track where it is, and leaves out the caller.",
				Annotations: readOnly("List people to mention")},
				[]string{"GET /api/v1/assets/{id}/comments/people", "GET /api/v1/categories/{id}/comments/people", "GET /api/v1/loans/{id}/comments/people", "GET /api/v1/assets/{id}/filings/{flow_id}/comments/people"},
				write,
				func(ctx context.Context, e *env, in commentOnIn) (reply[map[string]any], error) {
					path, err := commentsPath(ctx, e, in, "/people")
					if err != nil {
						return reply[map[string]any]{}, err
					}
					out := map[string]any{}
					if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "%d people.", count(out, "people"))
				})
			t.needsAll = []string{rbac.PermCommentsWrite, rbac.PermAssetsTrack}
			return t
		}(),
		simple(&mcp.Tool{Name: "list_filings", Description: "The flows that filed an asset, most recently first, with how many comments each filing has; comments can be on each.",
			Annotations: readOnly("List an asset's filings")}, "GET /api/v1/assets/{id}/filings", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/assets/{id}/filings", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%d filings.", count(out, "filings")) }),
		define(&mcp.Tool{Name: "attach_to_comment", Description: "Attach a file, base64-encoded, to the asset or category a comment is being written on, as the comment editor does, " +
			"with comments.write. It answers the Markdown that links to it, an image shown, to put in the comment. A loan's or a filing's comment attaches to its asset.",
			Annotations: changes("Attach a file to a comment", false)},
			[]string{"POST /api/v1/assets/{id}/comments/attachments", "POST /api/v1/categories/{id}/comments/attachments"}, write,
			func(ctx context.Context, e *env, in commentAttachmentIn) (reply[map[string]any], error) {
				data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Data))
				if err != nil {
					return reply[map[string]any]{}, fmt.Errorf("data must be base64: %v", err)
				}
				if strings.TrimSpace(in.Filename) == "" {
					return reply[map[string]any]{}, errors.New("filename is required, with its extension")
				}
				if (in.Asset == "") == (in.CategoryID == "") {
					return reply[map[string]any]{}, errors.New("give one of asset and category_id")
				}
				path, err := commentsPath(ctx, e, commentOnIn{Asset: in.Asset, CategoryID: in.CategoryID}, "/attachments")
				if err != nil {
					return reply[map[string]any]{}, err
				}
				body, contentType, err := multipartBody("file", in.Filename, data, map[string]string{"name": in.Name})
				if err != nil {
					return reply[map[string]any]{}, err
				}
				res := e.api(ctx, http.MethodPost, path, body, contentType)
				if res.Status < 200 || res.Status > 299 {
					return reply[map[string]any]{}, apiError(res)
				}
				out := map[string]any{}
				if err := json.Unmarshal(res.Body, &out); err != nil {
					return reply[map[string]any]{}, fmt.Errorf("reading the answer: %v", err)
				}
				return say(out, "Attached it; put this in the comment: %v", out["markdown"])
			}),
	}
}
