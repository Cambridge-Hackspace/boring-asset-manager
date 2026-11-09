package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Review states (docs/review.md): confirming, verifying and returning
// assets to draft. Where the instance lands changes through an assistant
// as drafts, as it does unless turned off, confirming and verifying are a
// person's to do, and these refuse.

type categoryReviewIn struct {
	CategoryID string  `json:"category_id"`
	Enabled    *bool   `json:"enabled,omitempty" jsonschema:"review states on or off for its assets and those below it; left out, its parent's"`
	Months     *int    `json:"months,omitempty" jsonschema:"ask for review that many months after an asset is confirmed or verified, 0 to 120, 0 for never; left out, its parent's"`
	OnEdit     *string `json:"on_edit,omitempty" jsonschema:"draft to return a confirmed or verified asset to draft when edited, keep to leave its state; left out, its parent's"`
	SelfVerify *bool   `json:"self_verify,omitempty" jsonschema:"let whoever confirmed an asset verify it too; left out, its parent's"`
}

type reviewIn struct {
	Asset string `json:"asset" jsonschema:"the asset, by ID or code"`
	Note  string `json:"note,omitempty" jsonschema:"what was checked, or for return_to_draft why it goes back, which it needs"`
}

func reviewTools() []tool {
	move := func(name, description, title, apiMove, done string, perm string) tool {
		return define(&mcp.Tool{Name: name, Description: description, Annotations: changes(title, true)},
			[]string{"POST /api/v1/assets/{id}/review"}, []string{perm}, func(ctx context.Context, e *env, in reviewIn) (reply[assetOut], error) {
				id, err := e.assetID(ctx, in.Asset)
				if err != nil {
					return reply[assetOut]{}, err
				}
				var res apiAsset
				if err := e.call(ctx, http.MethodPost, assetPath(id, "/review"), map[string]string{"move": apiMove, "note": in.Note}, &res); err != nil {
					return reply[assetOut]{}, err
				}
				out := e.summaryOf(ctx, &res.Asset)
				return say(out, "%s is %s.", describe(out.Asset), done)
			})
	}
	return []tool{
		simple(&mcp.Tool{Name: "set_category_review", Description: "A category's own review settings, the whole of them: each left out takes its parent's, " +
			"as subcategories take a category's fields. Every asset's next review date is worked out again.",
			Annotations: changes("Set review settings", true)}, "PUT /api/v1/categories/{id}/review", []string{rbac.PermCategoriesManage},
			func(in categoryReviewIn) (request, error) {
				body := map[string]any{"enabled": in.Enabled, "months": in.Months, "on_edit": in.OnEdit, "self_verify": in.SelfVerify}
				return request{path: pathOf("/api/v1/categories/{id}/review", in.CategoryID), body: body}, need(in.CategoryID, "category_id")
			},
			func(categoryReviewIn, map[string]any) string { return "Set." }),
		simple(&mcp.Tool{Name: "get_review_settings", Description: "Whether changes through assistants land as drafts for a person to accept or reject, " +
			"and how many days before a review date an asset's review shows as due and is announced.",
			Annotations: readOnly("Review settings")}, "GET /api/v1/settings/review", []string{rbac.PermSettingsManage},
			func(none) (request, error) { return request{path: "/api/v1/settings/review"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("Assistants' changes as drafts: %v; due %v days before.", out["assistant_drafts"], out["due_days"])
			}),
		simple(&mcp.Tool{Name: "review_summary", Description: "What waits for the person you act for to review: drafts and reviews due or overdue " +
			"they may confirm, confirmed assets they may verify, and assistants' changes they may accept or reject. search_assets with review finds them.",
			Annotations: readOnly("Review summary")}, "GET /api/v1/review", []string{rbac.PermAssetsRead},
			func(none) (request, error) { return request{path: "/api/v1/review"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%v drafts, %v due, %v overdue, %v to verify, %v assistants' changes waiting.",
					out["drafts"], out["due"], out["overdue"], out["to_verify"], out["proposals"])
			}),
		move("confirm_asset", "Confirm an asset in a category that uses review states: say its record is right, as someone responsible for it. "+
			"It starts the time until its next review again. Refused through an assistant where the instance lands assistants' changes as drafts.",
			"Confirm an asset", "confirm", "confirmed", rbac.PermAssetsConfirm),
		move("verify_asset", "Verify a confirmed asset: check it again, as someone other than whoever confirmed it, such as whoever keeps the inventory. "+
			"Refused through an assistant where the instance lands assistants' changes as drafts.",
			"Verify an asset", "verify", "verified", rbac.PermAssetsVerify),
		move("return_to_draft", "Return a confirmed or verified asset to draft, with a note saying why: what is wrong with its record.",
			"Return an asset to draft", "return", "back to draft", rbac.PermAssetsVerify),
	}
}
