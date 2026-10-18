package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tokens are listed and revoked over MCP, never made, and no one's own
// password is changed through it (#43): a misled assistant must not be able
// to lock its person out, or leave itself a way back in.

// tokensFor is where the caller reads and revokes tokens: every token, with
// users.manage; a person's own personal tokens otherwise.
func (e *env) tokensFor() string {
	if e.caller.may([]string{rbac.PermUsersManage}) {
		return "/api/v1/tokens"
	}
	return "/api/v1/auth/tokens"
}

func credentialTools() []tool {
	list := define(&mcp.Tool{Name: "list_tokens", Description: "API tokens: with users.manage every one, personal tokens marked with whose they are; otherwise the personal tokens of the person you act for, " +
		"this one among them. Secrets are never shown; they are not kept.", Annotations: readOnly("List tokens")},
		[]string{"GET /api/v1/tokens", "GET /api/v1/auth/tokens"}, []string{rbac.PermUsersManage},
		func(ctx context.Context, e *env, _ none) (reply[map[string]any], error) {
			out := map[string]any{}
			if err := e.call(ctx, http.MethodGet, e.tokensFor(), nil, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			return say(out, "%d tokens.", count(out, "tokens"))
		})
	list.personToo = true
	revoke := define(&mcp.Tool{Name: "revoke_token", Description: "Revoke an API token for good: with users.manage any, otherwise one of your person's own personal tokens. " +
		"Revoking the one you act through ends your access at once.", Annotations: destroys("Revoke a token", true)},
		[]string{"DELETE /api/v1/tokens/{id}", "DELETE /api/v1/auth/tokens/{id}"}, []string{rbac.PermUsersManage},
		func(ctx context.Context, e *env, in idIn) (reply[map[string]any], error) {
			if err := need(in.ID, "id"); err != nil {
				return reply[map[string]any]{}, err
			}
			out := map[string]any{}
			if err := e.call(ctx, http.MethodDelete, e.tokensFor()+"/"+pathOf("{id}", in.ID), nil, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			return say(out, "Revoked %s.", fmt.Sprintf("%q", nameOf(out, "token")))
		})
	revoke.personToo = true
	return []tool{list, revoke}
}
