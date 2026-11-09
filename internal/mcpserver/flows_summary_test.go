package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

// A run's summary says how far along it is, where the API says (#166).
func TestPromptSummarySaysHowFarAlong(t *testing.T) {
	on := map[string]any{"prompt": map[string]any{"step": "about", "kind": "form"}, "progress": map[string]any{"step": 2.0, "of": 3.0}}
	if got, want := promptSummary(on), `On step "about" (form, 2 of 3); answer it with {"step": "about", ...}.`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	delete(on, "progress")
	if got, want := promptSummary(on), `On step "about" (form); answer it with {"step": "about", ...}.`; got != want {
		t.Errorf("without progress: got %s\nwant %s", got, want)
	}
}

// A form's summary names the answers it takes, by its questions' keys
// (#174).
func TestPromptSummaryNamesAFormsAnswers(t *testing.T) {
	on := map[string]any{"prompt": map[string]any{"step": "about", "kind": "form", "questions": []any{
		map[string]any{"key": "name", "label": "What is it?"}, map[string]any{"key": "owner", "label": "Whose?"},
	}}}
	if got, want := promptSummary(on), `On step "about" (form); answer it with {"step": "about", "answers": {"name": ..., "owner": ...}}.`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

// The flow tools describe version 2: Set fields, and answers by key.
func TestFlowToolsDescribeVersion2(t *testing.T) {
	described := map[string]string{}
	for _, tl := range flowTools() {
		b, _ := json.Marshal(tl.def)
		described[tl.def.Name] = string(b)
	}
	for name, says := range map[string][]string{
		"save_flow":     {"version: 2", "set_fields", "kind: link"},
		"run_flow":      {"answers (by question key)", "asset_ids | codes | create: {name}, done"},
		"flow_starters": {"set fields", "Add to a kit"},
	} {
		for _, s := range says {
			if !strings.Contains(described[name], s) {
				t.Errorf("%s does not say %q: %s", name, s, described[name])
			}
		}
	}
	if strings.Contains(described["run_flow"], "fields,") {
		t.Errorf("run_flow still takes fields: %s", described["run_flow"])
	}
}

// A Link step's summary names the answers it takes, what it has linked so
// far, and, when it may, filing something new (#264).
func TestPromptSummaryNamesALinksAnswers(t *testing.T) {
	on := map[string]any{"prompt": map[string]any{"step": "link", "kind": "link", "find_by": []any{"list"},
		"linked": []any{map[string]any{"id": "k1", "name": "Kit 1"}}, "create_in": "Kits"}}
	want := `On step "link" (link); answer it with {"step": "link", "asset_ids": [...]} or "codes", {"create": {"name": ...}} to file something new in Kits, and "done": true to move on. Linked so far: Kit 1.`
	if got := promptSummary(on); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// One linking what the flow names, left by a failure, is tried again.
	if got := promptSummary(map[string]any{"prompt": map[string]any{"step": "link", "kind": "link"}}); !strings.Contains(got, `{"step": "link", ...}`) {
		t.Errorf("a named link: %s", got)
	}
}
