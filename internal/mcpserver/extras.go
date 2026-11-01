package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type triggerIn struct {
	Asset string `json:"asset" jsonschema:"the asset, by ID or code"`
	Key   string `json:"key" jsonschema:"the hook's key, from get_asset's hooks"`
	Wait  *int   `json:"wait_seconds,omitempty" jsonschema:"how long to wait for devices to answer, 0 to 10 seconds (5 if not given)"`
}

type triggerOut struct {
	EventID string             `json:"event_id"`
	Replies []models.HookReply `json:"replies" jsonschema:"what the devices that acted on it answered"`
}

type repliesIn struct {
	Asset   string `json:"asset" jsonschema:"the asset, by ID or code"`
	EventID string `json:"event_id" jsonschema:"the trigger, as trigger_hook answered"`
}

type photosIn struct {
	Asset string `json:"asset" jsonschema:"the asset, by ID or code"`
	Full  bool   `json:"full,omitempty" jsonschema:"the photos at full size rather than as thumbnails"`
}

type photosOut struct {
	Photos []photoView `json:"photos"`
}

type addPhotoIn struct {
	Asset    string `json:"asset" jsonschema:"the asset, by ID or code"`
	Data     string `json:"data" jsonschema:"the photo, a JPEG or PNG, base64-encoded"`
	Filename string `json:"filename,omitempty"`
}

type reorderIn struct {
	Asset    string   `json:"asset" jsonschema:"the asset, by ID or code"`
	PhotoIDs []string `json:"photo_ids" jsonschema:"every photo's ID, in the order to show them; the first is shown on its page"`
}

type removePhotoIn struct {
	Asset   string `json:"asset" jsonschema:"the asset, by ID or code"`
	PhotoID string `json:"photo_id"`
}

type importIn struct {
	CSV         string   `json:"csv" jsonschema:"the file's text, with a header row, as export_assets gives it"`
	Mapping     []string `json:"mapping,omitempty" jsonschema:"where each column goes, one target per column, as the preview suggests; required to import"`
	RemoveLinks bool     `json:"remove_links,omitempty" jsonschema:"remove links no row states"`
	Confirm     bool     `json:"confirm,omitempty" jsonschema:"import, rather than preview; needs preview_id"`
	PreviewID   string   `json:"preview_id,omitempty" jsonschema:"the preview_id of a preview of this same file, mapping and remove_links"`
}

type importOut struct {
	Preview   map[string]any `json:"preview,omitempty" jsonschema:"what importing would do: the columns, rows created, updated and unchanged, links, and problems"`
	PreviewID string         `json:"preview_id,omitempty" jsonschema:"pass with confirm to import exactly this"`
	Result    map[string]any `json:"result,omitempty" jsonschema:"what the import did"`
}

// previewID names an import's file and choices, so an import goes ahead
// only as previewed.
func (in importIn) previewID() string {
	raw, _ := json.Marshal([]any{in.CSV, in.Mapping, in.RemoveLinks})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}

// multipartBody is a form holding a file, and other fields.
func multipartBody(field, filename string, data []byte, values map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}
	for k, v := range values {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func extraTools() []tool {
	return []tool{
		define(&mcp.Tool{
			Name: "trigger_hook",
			Description: "Trigger one of an asset's hooks, such as Identify, which devices act on (a cabinet lights the drawer it is in), and wait a moment " +
				"for their answers. Ask the person first when the hook has a confirm question.",
			Annotations: changes("Trigger a hook", false),
		}, []string{"POST /api/v1/assets/{id}/hooks/{key}", "GET /api/v1/assets/{id}/hooks/replies"}, []string{rbac.PermAssetsHook}, func(ctx context.Context, e *env, in triggerIn) (reply[triggerOut], error) {
			wait := 5
			if in.Wait != nil {
				wait = *in.Wait
			}
			if wait < 0 || wait > 10 {
				return reply[triggerOut]{}, fmt.Errorf("wait_seconds must be from 0 to 10")
			}
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[triggerOut]{}, err
			}
			var triggered struct {
				EventID string `json:"event_id"`
			}
			if err := e.call(ctx, http.MethodPost, assetPath(id, "/hooks/", url.PathEscape(in.Key)), nil, &triggered); err != nil {
				return reply[triggerOut]{}, err
			}
			out := triggerOut{EventID: triggered.EventID, Replies: []models.HookReply{}}
			deadline := time.Now().Add(time.Duration(wait) * time.Second)
			for {
				var res struct {
					Replies []models.HookReply `json:"replies"`
				}
				if err := e.call(ctx, http.MethodGet, assetPath(id, "/hooks/replies?event_id=", url.QueryEscape(triggered.EventID)), nil, &res); err != nil {
					return reply[triggerOut]{}, err
				}
				if res.Replies != nil {
					out.Replies = res.Replies
				}
				if len(out.Replies) > 0 || !time.Now().Before(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return reply[triggerOut]{}, ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
			if len(out.Replies) == 0 {
				return say(out, "Triggered %s; nothing answered in %d seconds (hook_replies looks again).", in.Key, wait)
			}
			var said []string
			for _, r := range out.Replies {
				said = append(said, fmt.Sprintf("%s: %s", r.Name, r.Message))
			}
			return say(out, "Triggered %s. %s", in.Key, strings.Join(said, "; "))
		}),
		define(&mcp.Tool{
			Name:        "hook_replies",
			Description: "What devices answered to a hook triggered on an asset.",
			Annotations: readOnly("Hook replies"),
		}, []string{"GET /api/v1/assets/{id}/hooks/replies"}, []string{rbac.PermAssetsHook}, func(ctx context.Context, e *env, in repliesIn) (reply[triggerOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[triggerOut]{}, err
			}
			out := triggerOut{EventID: in.EventID, Replies: []models.HookReply{}}
			var res struct {
				Replies []models.HookReply `json:"replies"`
			}
			if err := e.call(ctx, http.MethodGet, assetPath(id, "/hooks/replies?event_id=", url.QueryEscape(in.EventID)), nil, &res); err != nil {
				return reply[triggerOut]{}, err
			}
			if res.Replies != nil {
				out.Replies = res.Replies
			}
			return say(out, "%d answers.", len(out.Replies))
		}),
		define(&mcp.Tool{
			Name:        "list_photos",
			Description: "An asset's photos, as images to look at: thumbnails, or full size. Up to 6 are shown.",
			Annotations: readOnly("Photos"),
		}, []string{"GET /api/v1/assets/{id}/photos/{photo_id}"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in photosIn) (reply[photosOut], error) {
			a, err := e.asset(ctx, in.Asset)
			if err != nil {
				return reply[photosOut]{}, err
			}
			out := photosOut{Photos: []photoView{}}
			var images []mcp.Content
			for i, p := range a.Photos {
				out.Photos = append(out.Photos, photoView{ID: p.ID, Filename: p.Filename})
				if i >= 6 {
					continue
				}
				path := assetPath(a.Asset.ID, "/photos/", url.PathEscape(p.ID))
				if !in.Full {
					path += "?size=thumb"
				}
				res := e.api(ctx, http.MethodGet, path, nil, "")
				if res.Status != http.StatusOK {
					return reply[photosOut]{}, apiError(res)
				}
				images = append(images, &mcp.ImageContent{Data: res.Body, MIMEType: res.Header.Get("Content-Type")})
			}
			r, _ := say(out, "%q has %d photos.", a.Asset.Name, len(a.Photos))
			r.extra = images
			return r, nil
		}),
		define(&mcp.Tool{
			Name:        "add_photo",
			Description: "Add a photo to an asset: a JPEG or PNG, base64-encoded, up to the size people may upload.",
			Annotations: changes("Add a photo", false),
		}, []string{"POST /api/v1/assets/{id}/photos"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in addPhotoIn) (reply[assetOut], error) {
			data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Data))
			if err != nil {
				return reply[assetOut]{}, fmt.Errorf("data must be base64: %v", err)
			}
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			name := in.Filename
			if name == "" {
				name = "photo"
			}
			body, contentType, err := multipartBody("file", name, data, nil)
			if err != nil {
				return reply[assetOut]{}, err
			}
			res := e.api(ctx, http.MethodPost, assetPath(id, "/photos"), body, contentType)
			if res.Status < 200 || res.Status > 299 {
				return reply[assetOut]{}, apiError(res)
			}
			var added apiAsset
			if err := json.Unmarshal(res.Body, &added); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &added.Asset)
			return say(out, "Added a photo to %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "reorder_photos",
			Description: "Put an asset's photos in order; the first is the one its page shows.",
			Annotations: changes("Reorder photos", true),
		}, []string{"PUT /api/v1/assets/{id}/photos"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in reorderIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			var res apiAsset
			if err := e.call(ctx, http.MethodPut, assetPath(id, "/photos"), map[string]any{"photo_ids": in.PhotoIDs}, &res); err != nil {
				return reply[assetOut]{}, err
			}
			out := e.summaryOf(ctx, &res.Asset)
			return say(out, "Reordered the photos of %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name:        "remove_photo",
			Description: "Remove a photo from an asset, for good.",
			Annotations: destroys("Remove a photo", true),
		}, []string{"DELETE /api/v1/assets/{id}/photos/{photo_id}"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in removePhotoIn) (reply[assetOut], error) {
			id, err := e.assetID(ctx, in.Asset)
			if err != nil {
				return reply[assetOut]{}, err
			}
			if err := e.call(ctx, http.MethodDelete, assetPath(id, "/photos/", url.PathEscape(in.PhotoID)), nil, nil); err != nil {
				return reply[assetOut]{}, err
			}
			out, err := e.reread(ctx, id)
			if err != nil {
				return reply[assetOut]{}, err
			}
			return say(out, "Removed the photo from %s.", describe(out.Asset))
		}),
		define(&mcp.Tool{
			Name: "import_assets",
			Description: "Import assets from a CSV file, as people do: first without confirm, to preview what it would create, update and link, " +
				"and its problems; then again with the same file, mapping and remove_links, confirm and the preview's preview_id, to import it. " +
				"Up to 50 assets created or changed at a time: split a larger file.",
			Annotations: changes("Import assets", false),
		}, []string{"POST /api/v1/assets/import/preview", "POST /api/v1/assets/import"}, []string{rbac.PermAssetsWrite}, func(ctx context.Context, e *env, in importIn) (reply[importOut], error) {
			values := map[string]string{"remove_links": fmt.Sprint(in.RemoveLinks)}
			if in.Mapping != nil {
				raw, _ := json.Marshal(in.Mapping)
				values["mapping"] = string(raw)
			}
			body, contentType, err := multipartBody("file", "import.csv", []byte(in.CSV), values)
			if err != nil {
				return reply[importOut]{}, err
			}
			res := e.api(ctx, http.MethodPost, "/api/v1/assets/import/preview", body, contentType)
			if res.Status != http.StatusOK {
				return reply[importOut]{}, apiError(res)
			}
			var counts struct {
				Create       int `json:"create"`
				Update       int `json:"update"`
				ProblemCount int `json:"problem_count"`
				LinksAdded   int `json:"links_added"`
				LinksMoved   int `json:"links_moved"`
				LinksRemoved int `json:"links_removed"`
			}
			_ = json.Unmarshal(res.Body, &counts)
			if !in.Confirm {
				out := importOut{PreviewID: in.previewID()}
				_ = json.Unmarshal(res.Body, &out.Preview)
				return say(out, "Importing would create %d assets and update %d, add %d links, move %d and remove %d, with %d problems. "+
					"To import, call again with confirm and preview_id %s.", counts.Create, counts.Update, counts.LinksAdded, counts.LinksMoved,
					counts.LinksRemoved, counts.ProblemCount, out.PreviewID)
			}
			if in.PreviewID != in.previewID() {
				return reply[importOut]{}, fmt.Errorf("preview_id does not match this file, mapping and remove_links: preview it first, and import what was previewed")
			}
			if err := checkItems(counts.Create + counts.Update); err != nil {
				return reply[importOut]{}, err
			}
			res = e.api(ctx, http.MethodPost, "/api/v1/assets/import", body, contentType)
			if res.Status != http.StatusOK {
				return reply[importOut]{}, apiError(res)
			}
			var out importOut
			_ = json.Unmarshal(res.Body, &out.Result)
			return say(out, "Imported: %d created, %d updated.", counts.Create, counts.Update)
		}),
	}
}
