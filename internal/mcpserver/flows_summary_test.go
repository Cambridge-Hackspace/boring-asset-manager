package mcpserver

import "testing"

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
