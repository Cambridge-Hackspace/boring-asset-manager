package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rawIn struct {
	Spec json.RawMessage `json:"spec"`
}

// A json.RawMessage holds any JSON, and a tool's schemas must say so: as a
// []byte it would be inferred an array of integers, and get_category failed
// its own output check on every category with a select field, whose options
// are an object.
func TestRawJSONIsAnyValueInToolSchemas(t *testing.T) {
	field := models.Field{Key: "package", Label: "Package", ValidationType: "select",
		Options: json.RawMessage(`{"choices":[{"label":"TO-92","value":"TO-92"}]}`)}
	tools := []tool{
		define(&mcp.Tool{Name: "field", Annotations: readOnly("Field")}, nil, nil,
			func(context.Context, *env, none) (reply[models.EffectiveField], error) {
				return say(models.EffectiveField{Field: field}, "a field")
			}),
		define(&mcp.Tool{Name: "echo", Annotations: readOnly("Echo")}, nil, nil,
			func(_ context.Context, _ *env, in rawIn) (reply[rawIn], error) { return say(in, "echoed") }),
	}
	cs, _ := connect(t, Caller{Kind: "token", ID: "t", Name: "T"}, tools, 200, "{}")

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "field"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("field: %s", text(res))
	}
	got, _ := json.Marshal(res.StructuredContent)
	var out models.EffectiveField
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if string(out.Field.Options) != string(field.Options) {
		t.Errorf("options came back as %s, want %s", out.Field.Options, field.Options)
	}

	// And as an argument: any JSON value is taken.
	for _, spec := range []any{map[string]any{"rows": 3}, []any{1, "two"}, "text", 4} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"spec": spec}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("echo %v: %s", spec, text(res))
		}
	}
}
