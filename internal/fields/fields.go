package fields

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

var (
	keyRegex = regexp.MustCompile(`^[a-z0-9_]+$`)

	ErrInvalidKey            = errors.New("field key must be non-empty snake_case (lowercase letters, numbers, underscores)")
	ErrInvalidLabel          = errors.New("field label cannot be empty")
	ErrInvalidValidationType = errors.New("validation_type must be one of: text, number, date, url, select")
	ErrSelectOptionsRequired = errors.New("options schema is required for validation_type 'select'")
	ErrEmptyChoices          = errors.New("select options must contain at least one choice")
	ErrDuplicateChoiceValue  = errors.New("select choices must have unique values")
	ErrInvalidDefaultChoice  = errors.New("select default value must match one of the available choices")
	ErrInvalidFieldValue     = errors.New("invalid field value")
)

const (
	TypeText   = "text"
	TypeNumber = "number"
	TypeDate   = "date"
	TypeURL    = "url"
	TypeSelect = "select"
)

// ValidValidationTypes returns the allowed validation type strings.
func ValidValidationTypes() []string {
	return []string{TypeText, TypeNumber, TypeDate, TypeURL, TypeSelect}
}

// IsValidValidationType checks whether a validation type is recognized.
func IsValidValidationType(t string) bool {
	switch t {
	case TypeText, TypeNumber, TypeDate, TypeURL, TypeSelect:
		return true
	default:
		return false
	}
}

// ValidateKey validates that a field key follows snake_case conventions.
func ValidateKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 || !keyRegex.MatchString(key) {
		return ErrInvalidKey
	}
	return nil
}

// ValidateSelectOptions parses and enforces uniqueness & default integrity on select options.
func ValidateSelectOptions(raw json.RawMessage) (*models.SelectOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ErrSelectOptionsRequired
	}

	var opts models.SelectOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil, fmt.Errorf("parsing select options JSON: %w", err)
	}

	if len(opts.Choices) == 0 {
		return nil, ErrEmptyChoices
	}

	seen := make(map[string]bool)
	for _, choice := range opts.Choices {
		if strings.TrimSpace(choice.Label) == "" {
			return nil, errors.New("choice label cannot be empty")
		}
		if err := textcheck.Check("choice label", choice.Label, textcheck.MaxName); err != nil {
			return nil, err
		}
		val := strings.TrimSpace(choice.Value)
		if val == "" {
			return nil, errors.New("choice value cannot be empty")
		}
		if err := textcheck.Check("choice value", val, textcheck.MaxName); err != nil {
			return nil, err
		}
		if seen[val] {
			return nil, fmt.Errorf("%w: duplicate value %q", ErrDuplicateChoiceValue, val)
		}
		seen[val] = true
	}

	if opts.Default != nil && strings.TrimSpace(*opts.Default) != "" {
		if !seen[*opts.Default] {
			return nil, fmt.Errorf("%w: default %q not found in choices", ErrInvalidDefaultChoice, *opts.Default)
		}
	}

	return &opts, nil
}

// ValidateFieldDefinition verifies all attributes of a field definition.
func ValidateFieldDefinition(key, label, validationType string, rawOptions json.RawMessage) (*models.SelectOptions, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if strings.TrimSpace(label) == "" {
		return nil, ErrInvalidLabel
	}
	if err := textcheck.Check("field label", label, textcheck.MaxName); err != nil {
		return nil, err
	}
	if !IsValidValidationType(validationType) {
		return nil, ErrInvalidValidationType
	}

	if validationType == TypeSelect {
		return ValidateSelectOptions(rawOptions)
	}

	return nil, nil
}

// ValidateFieldValue validates that a string value adheres to the field's validation type and options.
func ValidateFieldValue(f *models.Field, val string) error {
	// If the value is empty, validation depends on whether the field is required (checked separately in asset logic).
	if val == "" {
		return nil
	}
	if p := textcheck.Problem(val, textcheck.MaxValue); p != "" {
		return fmt.Errorf("%w: the value %s", ErrInvalidFieldValue, p)
	}

	switch f.ValidationType {
	case TypeText:
		return nil

	case TypeNumber:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return fmt.Errorf("%w: %q is not a valid number", ErrInvalidFieldValue, val)
		}
		return nil

	case TypeDate:
		formats := []string{
			"2006-01-02",
			time.RFC3339,
			"2006-01-02 15:04:05",
			time.RFC3339Nano,
		}
		parsed := false
		for _, layout := range formats {
			if _, err := time.Parse(layout, val); err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			return fmt.Errorf("%w: %q is not a valid date format (expected YYYY-MM-DD or RFC3339)", ErrInvalidFieldValue, val)
		}
		return nil

	case TypeURL:
		u, err := url.ParseRequestURI(val)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%w: %q is not a valid absolute URL", ErrInvalidFieldValue, val)
		}
		return nil

	case TypeSelect:
		opts, err := ValidateSelectOptions(f.Options)
		if err != nil {
			return fmt.Errorf("invalid select field configuration: %w", err)
		}
		for _, choice := range opts.Choices {
			if choice.Value == val {
				return nil
			}
		}
		return fmt.Errorf("%w: %q is not an allowed choice for field %q", ErrInvalidFieldValue, val, f.Key)

	default:
		return fmt.Errorf("unsupported validation type: %s", f.ValidationType)
	}
}
