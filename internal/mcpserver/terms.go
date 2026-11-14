package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Related terms (docs/data-model.md#related-terms): the graph of what
// things are called that search expands through. BAM! runs no assistant
// of its own to fill it in; a person's own, through these tools, can, as
// that person, and each term and link it adds records it.

type expandIn struct {
	Search string `json:"search" jsonschema:"plain words, as search_assets takes them"`
}

type termIn struct {
	ID        string    `json:"id,omitempty" jsonschema:"the term to change; empty to add one"`
	Label     *string   `json:"label,omitempty" jsonschema:"its preferred label, the name people most often use; no two terms share one"`
	AltLabels *[]string `json:"alt_labels,omitempty" jsonschema:"the other names it goes by, such as abbreviations and synonyms; the whole list"`
	Note      *string   `json:"note,omitempty" jsonschema:"what it means, for people reading the terms"`
}

type termLinkIn struct {
	ID       string `json:"id" jsonschema:"the term"`
	Relation string `json:"relation" jsonschema:"broader: id is narrower than other, and a search for other finds it; narrower: the other way; related: suggested beside each other, not searched"`
	Other    string `json:"other" jsonschema:"the other term"`
}

type importTermIn struct {
	Label     string   `json:"label"`
	AltLabels []string `json:"alt_labels,omitempty"`
	Note      string   `json:"note,omitempty"`
	Broader   []string `json:"broader,omitempty" jsonschema:"labels of the terms broader than it; one no term has yet is added"`
	Related   []string `json:"related,omitempty" jsonschema:"labels of the terms related to it; one no term has yet is added"`
}

type importTermsIn struct {
	Name  string         `json:"name" jsonschema:"what the import is, such as Filament words, so that a person can review it and remove it as one"`
	Terms []importTermIn `json:"terms"`
}

func termTools() []tool {
	read := []string{rbac.PermAssetsRead}
	manage := []string{rbac.PermTermsManage}
	return []tool{
		simple(&mcp.Tool{Name: "list_terms", Description: "The related terms search expands through: each term's preferred label, other names and note, the links between terms (term_id is narrower than other_id for broader; related links are suggestions), and where each came from. " +
			"A search finds, beside what matches its words, what matches any other name of a term called that, and of every term narrower than it.",
			Annotations: readOnly("List related terms")}, "GET /api/v1/terms", read,
			func(struct{}) (request, error) { return request{path: "/api/v1/terms"}, nil },
			func(_ struct{}, out map[string]any) string {
				return fmt.Sprintf("%d terms, %d links.", count(out, "terms"), count(out, "links"))
			}),
		simple(&mcp.Tool{Name: "get_term", Description: "One related term: its preferred label, other names, note, and who added it.",
			Annotations: readOnly("Get a related term")}, "GET /api/v1/terms/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/terms/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", labelOf(out)) }),
		simple(&mcp.Tool{Name: "expand_search", Description: "What a search for some words also looks for through the related terms, each with why (such as TPU, narrower than flexible filament), and the related terms suggested in its place. Use it to check terms after changing them.",
			Annotations: readOnly("Expand a search")}, "GET /api/v1/terms/expand", read,
			func(in expandIn) (request, error) {
				return request{path: "/api/v1/terms/expand", query: url.Values{"q": {in.Search}}}, need(in.Search, "search")
			},
			func(in expandIn, out map[string]any) string {
				return fmt.Sprintf("%q also looks for %d other names, and suggests %d related terms.", in.Search, count(out, "expansion"), count(out, "related"))
			}),
		define(&mcp.Tool{Name: "save_term", Description: "Add a related term, or change one: only what is given changes. A term is a thing as people call it: its preferred label, and the other names it goes by. " +
			"Link it to others with link_terms. Each term records that it was added through you, for the person you act for.",
			Annotations: changes("Save a related term", true)}, []string{"POST /api/v1/terms", "PUT /api/v1/terms/{id}"}, manage,
			func(ctx context.Context, e *env, in termIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Label == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new term needs a label")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/terms", in.body(models.Term{}), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Added %q.", labelOf(out))
				}
				var got struct{ Term models.Term }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/terms/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/terms/{id}", in.ID), in.body(got.Term), &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %q.", labelOf(out))
			}),
		simple(&mcp.Tool{Name: "delete_term", Description: "Remove a related term, with its links.",
			Annotations: destroys("Remove a related term", true)}, "DELETE /api/v1/terms/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/terms/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Removed the term." }),
		simple(&mcp.Tool{Name: "link_terms", Description: "Link two related terms: one broader or narrower than the other, so that a search for the broader finds the narrower, or related, suggested beside each other. Linking them again changes nothing; nothing is made narrower than itself.",
			Annotations: changes("Link related terms", true)}, "PUT /api/v1/terms/{id}/links/{relation}/{other}", manage,
			func(in termLinkIn) (request, error) {
				if err := need(in.ID, "id"); err != nil {
					return request{}, err
				}
				return request{path: pathOf("/api/v1/terms/{id}/links/{relation}/{other}", in.ID, in.Relation, in.Other)}, need(in.Other, "other")
			},
			func(in termLinkIn, _ map[string]any) string { return "Linked the terms, " + in.Relation + "." }),
		simple(&mcp.Tool{Name: "unlink_terms", Description: "Remove a link between two related terms, named as link_terms names it.",
			Annotations: destroys("Unlink related terms", true)}, "DELETE /api/v1/terms/{id}/links/{relation}/{other}", manage,
			func(in termLinkIn) (request, error) {
				if err := need(in.ID, "id"); err != nil {
					return request{}, err
				}
				return request{path: pathOf("/api/v1/terms/{id}/links/{relation}/{other}", in.ID, in.Relation, in.Other)}, need(in.Other, "other")
			},
			func(termLinkIn, map[string]any) string { return "Unlinked the terms." }),
		simple(&mcp.Tool{Name: "import_terms", Description: "Add many related terms and their links at once, as one import that a person can review and remove as one: each term with its other names, and the terms broader than it and related to it by label. " +
			"A term whose label another has already is not added again; its links are added to that one. Prefer this to many save_term calls.",
			Annotations: changes("Import related terms", false)}, "POST /api/v1/terms/imports", manage,
			func(in importTermsIn) (request, error) {
				return request{path: "/api/v1/terms/imports", body: in}, need(in.Name, "name")
			},
			func(_ importTermsIn, out map[string]any) string {
				imp, _ := out["import"].(map[string]any)
				return fmt.Sprintf("Imported %v terms and %v links.", imp["terms"], imp["links"])
			}),
		simple(&mcp.Tool{Name: "delete_term_import", Description: "Remove an import of related terms (list_terms gives them to those who manage terms): the terms and links it added, and links to its terms.",
			Annotations: destroys("Remove an import of related terms", true)}, "DELETE /api/v1/terms/imports/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/terms/imports/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Removed the import." }),
	}
}

// body is the term as it is to be saved: what was given, over what it is.
func (in termIn) body(was models.Term) map[string]any {
	out := map[string]any{"label": was.Label, "alt_labels": was.AltLabels, "note": was.Note}
	if in.Label != nil {
		out["label"] = *in.Label
	}
	if in.AltLabels != nil {
		out["alt_labels"] = *in.AltLabels
	}
	if in.Note != nil {
		out["note"] = *in.Note
	}
	return out
}

// labelOf is the label of the term an answer holds.
func labelOf(out map[string]any) string {
	t, _ := out["term"].(map[string]any)
	l, _ := t["label"].(string)
	return l
}
