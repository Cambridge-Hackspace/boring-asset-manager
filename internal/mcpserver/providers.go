package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Identity providers and people's identities at them (docs/access.md#signing-in-with-an-identity-provider).

type providerRuleIn struct {
	Claim    string   `json:"claim" jsonschema:"the claim or attribute, such as groups"`
	Value    string   `json:"value" jsonschema:"the value it must hold, such as makerspace-admins"`
	RoleID   string   `json:"role_id" jsonschema:"the role it gives"`
	ScopeIDs []string `json:"scope_ids,omitempty" jsonschema:"the scopes the role is narrowed to; none for everywhere"`
}

type providerIn struct {
	ID             string            `json:"id,omitempty" jsonschema:"the provider to change; leave out to make one"`
	Key            string            `json:"key" jsonschema:"a lowercase key, such as hackspace, used in its addresses"`
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol" jsonschema:"one of list_identity_providers' protocols"`
	Settings       map[string]string `json:"settings,omitempty" jsonschema:"the protocol's settings, by name"`
	Secrets        map[string]string `json:"secrets,omitempty" jsonschema:"the protocol's secrets, by name; those left out are kept"`
	ButtonLabel    string            `json:"button_label,omitempty"`
	Icon           string            `json:"icon,omitempty"`
	Enabled        bool              `json:"enabled"`
	Position       int               `json:"position,omitempty" jsonschema:"its place among the buttons, lowest first"`
	Provision      bool              `json:"provision,omitempty" jsonschema:"make an account for someone it vouches for and BAM! has never seen"`
	DefaultRoleIDs []string          `json:"default_role_ids,omitempty" jsonschema:"roles a new account starts with"`
	AllowedDomains []string          `json:"allowed_domains,omitempty" jsonschema:"make accounts only for emails at these domains"`
	AllowedGroups  []string          `json:"allowed_groups,omitempty" jsonschema:"make accounts only for members of these groups"`
	LinkByEmail    bool              `json:"link_by_email,omitempty" jsonschema:"link to an account with the same verified email"`
	TrustEmail     bool              `json:"trust_email,omitempty" jsonschema:"count its emails as verified though it does not say"`
	MFAAMR         []string          `json:"mfa_amr,omitempty" jsonschema:"amr values that make a sign-in count as two-factor; left out, mfa, otp, hwk, swk and fpt"`
	MFAACR         []string          `json:"mfa_acr,omitempty" jsonschema:"acr values, or SAML AuthnContextClassRef, that make a sign-in count as two-factor"`
	MFAFallback    bool              `json:"mfa_fallback,omitempty" jsonschema:"ask for BAM!'s own second factor when the provider's sign-in does not count, rather than refusing someone whose roles require one"`
	MFAAlwaysLocal bool              `json:"mfa_always_local,omitempty" jsonschema:"always ask for BAM!'s own second factor, whatever the provider says"`
	Rules          []providerRuleIn  `json:"rules,omitempty" jsonschema:"roles given from claims, re-applied at every sign-in; the whole list"`
}

func providerTools() []tool {
	settings := []string{rbac.PermSettingsManage}
	users := []string{rbac.PermUsersManage}
	return []tool{
		simple(&mcp.Tool{Name: "list_identity_providers", Description: "The identity providers people sign in through, with their rules, the protocols they can use, " +
			"and whether signing in with a password is on. Secrets are never shown, only whether each is set.",
			Annotations: readOnly("List identity providers")}, "GET /api/v1/auth/providers", settings,
			func(none) (request, error) { return request{path: "/api/v1/auth/providers"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%d identity providers.", count(out, "providers"))
			}),
		define(&mcp.Tool{Name: "save_identity_provider", Description: "Make an identity provider, or change one, given whole. Secrets left out are kept. " +
			"One set in the configuration file cannot be changed here.",
			Annotations: changes("Save an identity provider", true)}, []string{"POST /api/v1/auth/providers", "PUT /api/v1/auth/providers/{id}"}, settings,
			func(ctx context.Context, e *env, in providerIn) (reply[map[string]any], error) {
				rules := []map[string]any{}
				for _, r := range in.Rules {
					rules = append(rules, map[string]any{"claim": r.Claim, "value": r.Value, "role_id": r.RoleID, "scope_ids": r.ScopeIDs})
				}
				body := map[string]any{"key": in.Key, "name": in.Name, "protocol": in.Protocol, "settings": in.Settings, "secrets": in.Secrets,
					"button_label": in.ButtonLabel, "icon": in.Icon, "enabled": in.Enabled, "position": in.Position, "provision": in.Provision,
					"default_role_ids": in.DefaultRoleIDs, "allowed_domains": in.AllowedDomains, "allowed_groups": in.AllowedGroups,
					"link_by_email": in.LinkByEmail, "trust_email": in.TrustEmail, "rules": rules,
					"mfa_amr": in.MFAAMR, "mfa_acr": in.MFAACR, "mfa_fallback": in.MFAFallback, "mfa_always_local": in.MFAAlwaysLocal}
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/auth/providers", body, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the identity provider %q.", nameOf(out, "provider"))
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/auth/providers/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the identity provider %q.", nameOf(out, "provider"))
			}),
		simple(&mcp.Tool{Name: "delete_identity_provider", Description: "Remove an identity provider, and every identity at it: someone with no password and no other identity " +
			"can then no longer sign in. The roles its rules gave stay, as given by hand.",
			Annotations: destroys("Remove an identity provider", true)}, "DELETE /api/v1/auth/providers/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/auth/providers/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Removed the identity provider." }),
		simple(&mcp.Tool{Name: "set_password_sign_in", Description: "Turn signing in with a password on or off for everyone. Off, people sign in only through the identity providers, " +
			"and those who manage users with a password at /signin/local. Refused while no provider is enabled.",
			Annotations: destroys("Turn password sign-in on or off", true)}, "PUT /api/v1/auth/settings", settings,
			func(in struct {
				On bool `json:"on"`
			}) (request, error) {
				return request{path: "/api/v1/auth/settings", body: map[string]bool{"local_passwords": in.On}}, nil
			},
			func(in struct {
				On bool `json:"on"`
			}, _ map[string]any) string {
				if in.On {
					return "Signing in with a password is on."
				}
				return "Signing in with a password is off."
			}),
		simple(&mcp.Tool{Name: "list_user_identities", Description: "Someone's identities at identity providers, and which of their roles a provider's rule gave.",
			Annotations: readOnly("List someone's identities")}, "GET /api/v1/users/{id}/identities", users,
			func(in userIn) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/identities", in.ID)}, need(in.ID, "id")
			},
			func(_ userIn, out map[string]any) string {
				return fmt.Sprintf("%d identities.", count(out, "identities"))
			}),
		simple(&mcp.Tool{Name: "link_user_identity", Description: "Link someone's account to their identity at a provider, by the subject the provider names them by. " +
			"Only for someone who asked, and is who they say: whoever signs in as that subject signs in as them.",
			Annotations: changes("Link an identity", true)}, "POST /api/v1/users/{id}/identities", users,
			func(in struct {
				ID         string `json:"id" jsonschema:"the user"`
				ProviderID string `json:"provider_id"`
				Subject    string `json:"subject"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/identities", in.ID), body: map[string]string{"provider_id": in.ProviderID, "subject": in.Subject}}, need(in.ID, "id")
			},
			func(struct {
				ID         string `json:"id" jsonschema:"the user"`
				ProviderID string `json:"provider_id"`
				Subject    string `json:"subject"`
			}, map[string]any) string {
				return "Linked the identity."
			}),
		simple(&mcp.Tool{Name: "unlink_user_identity", Description: "Unlink one of someone's identities. Refused when it would leave them no way to sign in.",
			Annotations: destroys("Unlink an identity", true)}, "DELETE /api/v1/users/{id}/identities/{identity}", users,
			func(in struct {
				ID       string `json:"id" jsonschema:"the user"`
				Identity string `json:"identity"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/identities/{identity}", in.ID, in.Identity)}, need(in.ID, "id")
			},
			func(struct {
				ID       string `json:"id" jsonschema:"the user"`
				Identity string `json:"identity"`
			}, map[string]any) string {
				return "Unlinked the identity."
			}),
	}
}
