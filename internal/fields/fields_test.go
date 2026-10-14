package fields

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestValidateKey(t *testing.T) {
	valid := []string{"asset_tag", "status", "mac_address", "ip_v4", "x"}
	for _, k := range valid {
		if err := ValidateKey(k); err != nil {
			t.Errorf("expected key %q to be valid, got %v", k, err)
		}
	}

	invalid := []string{"", "AssetTag", "asset-tag", "tag with spaces", "tag!", stringsRepeat("a", 65)}
	for _, k := range invalid {
		if err := ValidateKey(k); err == nil {
			t.Errorf("expected key %q to be invalid, got nil", k)
		}
	}
}

func stringsRepeat(s string, count int) string {
	res := ""
	for i := 0; i < count; i++ {
		res += s
	}
	return res
}

func TestValidateSelectOptions(t *testing.T) {
	// 1. Valid options with default
	validJSON := json.RawMessage(`{
		"choices": [
			{"label": "Operational", "value": "operational"},
			{"label": "Needs Repair", "value": "needs_repair"}
		],
		"default": "operational"
	}`)
	opts, err := ValidateSelectOptions(validJSON)
	if err != nil {
		t.Fatalf("expected valid select options, got: %v", err)
	}
	if len(opts.Choices) != 2 || *opts.Default != "operational" {
		t.Errorf("parsed options mismatch")
	}

	// 2. Duplicate choice value
	dupJSON := json.RawMessage(`{
		"choices": [
			{"label": "A", "value": "same"},
			{"label": "B", "value": "same"}
		]
	}`)
	if _, err := ValidateSelectOptions(dupJSON); err == nil {
		t.Errorf("expected error for duplicate choice value, got nil")
	}

	// 3. Default not in choices
	invalidDefault := json.RawMessage(`{
		"choices": [
			{"label": "A", "value": "opt_a"}
		],
		"default": "non_existent"
	}`)
	if _, err := ValidateSelectOptions(invalidDefault); err == nil {
		t.Errorf("expected error for non-existent default choice, got nil")
	}

	// 4. Empty choices
	emptyJSON := json.RawMessage(`{"choices": []}`)
	if _, err := ValidateSelectOptions(emptyJSON); err == nil {
		t.Errorf("expected error for empty choices, got nil")
	}
}

func TestValidateFieldValue(t *testing.T) {
	// Number
	numField := &models.Field{Key: "weight", ValidationType: TypeNumber}
	if err := ValidateFieldValue(numField, "123.45"); err != nil {
		t.Errorf("valid number rejected: %v", err)
	}
	if err := ValidateFieldValue(numField, "abc"); err == nil {
		t.Errorf("invalid number accepted")
	}

	// Date
	dateField := &models.Field{Key: "purchased_at", ValidationType: TypeDate}
	if err := ValidateFieldValue(dateField, "2024-05-15"); err != nil {
		t.Errorf("valid date rejected: %v", err)
	}
	if err := ValidateFieldValue(dateField, "2024-05-15T14:30:00Z"); err != nil {
		t.Errorf("valid RFC3339 date rejected: %v", err)
	}
	if err := ValidateFieldValue(dateField, "not-a-date"); err == nil {
		t.Errorf("invalid date accepted")
	}

	// URL
	urlField := &models.Field{Key: "manual_url", ValidationType: TypeURL}
	if err := ValidateFieldValue(urlField, "https://example.com/manual.pdf"); err != nil {
		t.Errorf("valid url rejected: %v", err)
	}
	if err := ValidateFieldValue(urlField, "not-a-url"); err == nil {
		t.Errorf("invalid url accepted")
	}

	// Select
	selectJSON := json.RawMessage(`{
		"choices": [
			{"label": "Small", "value": "s"},
			{"label": "Large", "value": "l"}
		]
	}`)
	selectField := &models.Field{Key: "size", ValidationType: TypeSelect, Options: selectJSON}
	if err := ValidateFieldValue(selectField, "s"); err != nil {
		t.Errorf("valid select choice rejected: %v", err)
	}
	if err := ValidateFieldValue(selectField, "xl"); err == nil {
		t.Errorf("invalid select choice accepted")
	}
}

// TestStoredTextRules checks the labels, choices and values a field keeps
// against the rules every database can hold: lengths in characters, no NUL.
func TestStoredTextRules(t *testing.T) {
	wrench := strings.Repeat("🔧", 255)
	if _, err := ValidateFieldDefinition("k", wrench, TypeText, nil); err != nil {
		t.Errorf("255-character label rejected: %v", err)
	}
	if _, err := ValidateFieldDefinition("k", wrench+"🔧", TypeText, nil); err == nil {
		t.Error("256-character label accepted")
	}
	nul := "a" + string(rune(0)) + "b"
	for name, raw := range map[string]string{
		"overlong label": `{"choices":[{"label":"` + wrench + `🔧","value":"v"}]}`,
		"overlong value": `{"choices":[{"label":"l","value":"` + wrench + `🔧"}]}`,
		"NUL label":      `{"choices":[{"label":"a\u0000b","value":"v"}]}`,
		"NUL value":      `{"choices":[{"label":"l","value":"a\u0000b"}]}`,
	} {
		if _, err := ValidateSelectOptions([]byte(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := ValidateSelectOptions([]byte(`{"choices":[{"label":"` + wrench + `","value":"` + wrench + `"}]}`)); err != nil {
		t.Errorf("255-character choice rejected: %v", err)
	}

	text := &models.Field{ValidationType: TypeText}
	if err := ValidateFieldValue(text, strings.Repeat("🔧", 10000)); err != nil {
		t.Errorf("10,000-character value rejected: %v", err)
	}
	for name, v := range map[string]string{"overlong": strings.Repeat("x", 10001), "NUL": nul} {
		if err := ValidateFieldValue(text, v); !errors.Is(err, ErrInvalidFieldValue) {
			t.Errorf("%s value: %v", name, err)
		}
	}
}
