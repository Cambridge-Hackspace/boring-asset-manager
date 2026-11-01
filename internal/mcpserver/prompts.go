package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Prompts are the tasks people most often bring to an assistant, as a
// first message it can start from (docs/mcp.md).

type prompt struct {
	def  *mcp.Prompt
	text func(args map[string]string) string
}

func prompts() []prompt {
	arg := func(name, description string, required bool) *mcp.PromptArgument {
		return &mcp.PromptArgument{Name: name, Description: description, Required: required}
	}
	return []prompt{
		{&mcp.Prompt{Name: "stocktake", Title: "Stocktake a place", Description: "Walk through what is in a shelf, bin or room, confirming, moving or reporting each thing.",
			Arguments: []*mcp.PromptArgument{arg("place", "the place, by name or the code on its label", true)}},
			func(a map[string]string) string {
				return fmt.Sprintf("Help me stocktake %s in BAM!. Find it (get_asset takes a code; search_assets a name), and list what BAM! says is in it, "+
					"including inside containers. Then go through them with me one at a time: for each, I say whether it is there; move anything I find "+
					"elsewhere with move_asset, record stock I count with record_stock, and report anything broken with set_condition. Finish with what "+
					"was missing and what turned up that BAM! did not know about.", a["place"])
			}},
		{&mcp.Prompt{Name: "add_things", Title: "Add things", Description: "Add assets from a list, or a photo of them, putting them in a category and a place, and offer their labels.",
			Arguments: []*mcp.PromptArgument{arg("things", "what to add: a list, or say a photo follows", true), arg("place", "where they are, if all in one place", false)}},
			func(a map[string]string) string {
				where := ""
				if a["place"] != "" {
					where = " They are in " + a["place"] + "."
				}
				return fmt.Sprintf("Add these to BAM!: %s.%s Check list_categories for where each belongs, and get_category or fields_for for the fields its "+
					"category records; ask me for anything required that I have not said. Give them the next free codes with assign_next_code. Show me "+
					"what you will add before create_asset, then offer to print their labels: label_options says which templates and printers suit them.",
					a["things"], where)
			}},
		{&mcp.Prompt{Name: "needs_attention", Title: "What needs attention", Description: "Overdue loans, things running low, and things reported broken or needing maintenance."},
			func(map[string]string) string {
				return "What needs attention in BAM!? Check overdue loans (list_loans, overdue), things running low (search_assets, running_low), and " +
					"things needing maintenance or out of service (search_assets, condition problem). Summarize each, most urgent first, naming things " +
					"and people by name, with the links to their pages."
			}},
		{&mcp.Prompt{Name: "find", Title: "Find something", Description: "Find where something is, or who has it.",
			Arguments: []*mcp.PromptArgument{arg("what", "what you are looking for", true)}},
			func(a map[string]string) string {
				return fmt.Sprintf("Where is %s? Search BAM! by name and code (search_assets), and tell me where each match is, whether it is lent out "+
					"and to whom, and its condition, with a link to its page. If nothing matches, try other words for it.", a["what"])
			}},
		{&mcp.Prompt{Name: "set_up", Title: "Set up BAM!", Description: "Walk through setting BAM! up: people and roles, categories and fields, places, labels and printers."},
			func(map[string]string) string {
				return "Help me set BAM! up, one step at a time, asking before each change. Read bam://guide and bam://help/start first. Then: who uses it " +
					"and what each may do (list_users, list_roles); the categories things fall into and the fields each records (list_categories, " +
					"list_fields); the places things are kept, as assets that contain others; the codes on labels (list_generators), a label template, " +
					"and the printers (list_printers). Say what is already done before suggesting more."
			}},
		{&mcp.Prompt{Name: "design_label", Title: "Design a label", Description: "Design a label template with you: what it shows, for which printer and roll, previewed as it prints.",
			Arguments: []*mcp.PromptArgument{arg("purpose", "what the labels are for, such as bins of screws or tools", true), arg("printer", "the printer, by name", false)}},
			func(a map[string]string) string {
				printer := "Ask me which printer it is for (list_printers)"
				if a["printer"] != "" {
					printer = "It is for the printer " + a["printer"] + " (list_printers)"
				}
				return fmt.Sprintf("Design a label template with me for %s. %s, and its roll. Ask what each label should show: a code to scan (a QR code "+
					"of {{scan_url}} opens the asset's page), its name, fields. Make it with save_template, then look at it with preview_template, for a "+
					"real asset and checked against the printer, and read what each element prints; adjust until it reads well and fits. Offer a "+
					"test_print when it looks right.", a["purpose"], printer)
			}},
	}
}

func addPrompts(s *mcp.Server) {
	for _, p := range prompts() {
		p := p
		s.AddPrompt(p.def, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			for _, a := range p.def.Arguments {
				if a.Required && strings.TrimSpace(req.Params.Arguments[a.Name]) == "" {
					return nil, fmt.Errorf("%s is required", a.Name)
				}
			}
			return &mcp.GetPromptResult{Description: p.def.Description, Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: p.text(req.Params.Arguments)}},
			}}, nil
		})
	}
}

// confirmation is what a destructive tool asks the person before acting,
// where the client can ask (MCP elicitation, on a request that takes input
// back: protocol 2026-07-28); elsewhere the client's own approval stands.
func confirmation(def *mcp.Tool, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if def.Annotations.DestructiveHint == nil || !*def.Annotations.DestructiveHint || !canAsk(req) {
		return nil, nil
	}
	digest := argumentsDigest(def.Name, req.Params.Arguments)
	if answer, ok := req.Params.InputResponses["confirm"].(*mcp.ElicitResult); ok && req.Params.RequestState == digest {
		if answer.Action == "accept" {
			return nil, nil
		}
		return nil, fmt.Errorf("the person did not confirm %s; nothing was done", strings.ToLower(def.Annotations.Title))
	}
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{"confirm": &mcp.ElicitParams{
			Mode:            "form",
			Message:         confirmMessage(def, req),
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}},
		RequestState: digest,
	}, nil
}

// canAsk reports whether the client takes questions back on a request.
func canAsk(req *mcp.CallToolRequest) bool {
	if req == nil || req.ProtocolVersion() < "2026-07-28" {
		return false
	}
	caps := req.ClientCapabilities()
	return caps != nil && caps.Elicitation != nil
}

// argumentsDigest ties a confirmation to exactly the call it was asked
// for: the tool and its arguments, however their keys are ordered.
func argumentsDigest(name string, args any) string {
	raw, _ := json.Marshal(args)
	var v any
	_ = json.Unmarshal(raw, &v)
	canonical, _ := json.Marshal(v)
	sum := sha256.Sum256(append([]byte(name+"\x00"), canonical...))
	return hex.EncodeToString(sum[:16])
}

func confirmMessage(def *mcp.Tool, req *mcp.CallToolRequest) string {
	raw, _ := json.Marshal(req.Params.Arguments)
	args := string(raw)
	if len(args) > 300 {
		args = args[:300] + "…"
	}
	return fmt.Sprintf("%s: %s\n\n%s\n\nGo ahead?", def.Annotations.Title, args, def.Description)
}
