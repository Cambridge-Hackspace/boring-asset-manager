package labels

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
)

// RefKind identifies what a content binding placeholder refers to.
type RefKind int

const (
	RefAssetID   RefKind = iota // {{asset.id}}
	RefAssetName                // {{asset.name}}
	RefAssetCode                // {{asset.code.<generator_type>}}
	RefField                    // {{field.<key>}}
	RefBaseURL                  // {{base_url}}
)

// Ref is one placeholder. Arg holds the generator type or field key.
type Ref struct {
	Kind RefKind
	Arg  string
}

// Binding is a parsed content binding: literal text interleaved with
// placeholders.
type Binding struct {
	parts []bindingPart
}

type bindingPart struct {
	literal string
	ref     *Ref
}

// ErrInvalidBinding is wrapped by every binding parse error.
var ErrInvalidBinding = errors.New("invalid content binding")

// ParseBinding parses literal text containing {{...}} placeholders. Whitespace
// inside the braces is ignored.
func ParseBinding(s string) (Binding, error) {
	var b Binding
	rest := s
	for rest != "" {
		start := strings.Index(rest, "{{")
		if start < 0 {
			b.parts = append(b.parts, bindingPart{literal: rest})
			break
		}
		if start > 0 {
			b.parts = append(b.parts, bindingPart{literal: rest[:start]})
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			return Binding{}, fmt.Errorf("%w: unclosed %q", ErrInvalidBinding, "{{")
		}
		ref, err := parseRef(strings.TrimSpace(rest[start+2 : start+2+end]))
		if err != nil {
			return Binding{}, err
		}
		b.parts = append(b.parts, bindingPart{ref: &ref})
		rest = rest[start+2+end+2:]
	}
	return b, nil
}

func parseRef(name string) (Ref, error) {
	switch {
	case name == "asset.id":
		return Ref{Kind: RefAssetID}, nil
	case name == "asset.name":
		return Ref{Kind: RefAssetName}, nil
	case name == "base_url":
		return Ref{Kind: RefBaseURL}, nil
	case strings.HasPrefix(name, "asset.code."):
		genType := strings.TrimPrefix(name, "asset.code.")
		if !generators.IsValidGeneratorType(genType) {
			return Ref{}, fmt.Errorf("%w: unknown generator type in {{%s}}", ErrInvalidBinding, name)
		}
		return Ref{Kind: RefAssetCode, Arg: genType}, nil
	case strings.HasPrefix(name, "field."):
		key := strings.TrimPrefix(name, "field.")
		if fields.ValidateKey(key) != nil {
			return Ref{}, fmt.Errorf("%w: malformed field key in {{%s}}", ErrInvalidBinding, name)
		}
		return Ref{Kind: RefField, Arg: key}, nil
	default:
		return Ref{}, fmt.Errorf("%w: unknown placeholder {{%s}}", ErrInvalidBinding, name)
	}
}

// Refs returns the binding's placeholders in order.
func (b Binding) Refs() []Ref {
	var refs []Ref
	for _, p := range b.parts {
		if p.ref != nil {
			refs = append(refs, *p.ref)
		}
	}
	return refs
}

// IsEmpty reports whether the binding has neither text nor placeholders.
func (b Binding) IsEmpty() bool {
	return len(b.parts) == 0
}

// Data is what a binding resolves against.
type Data struct {
	AssetID   string
	AssetName string
	BaseURL   string
	// Codes maps generator type to the asset's assigned code.
	Codes map[string]string
	// Fields maps field key to the asset's value.
	Fields map[string]string
}

// Resolve substitutes every placeholder. Missing codes and field values
// resolve to the empty string; callers that need a value, such as generator
// elements, check the result.
func (b Binding) Resolve(d Data) string {
	var sb strings.Builder
	for _, p := range b.parts {
		if p.ref == nil {
			sb.WriteString(p.literal)
			continue
		}
		switch p.ref.Kind {
		case RefAssetID:
			sb.WriteString(d.AssetID)
		case RefAssetName:
			sb.WriteString(d.AssetName)
		case RefBaseURL:
			sb.WriteString(strings.TrimRight(d.BaseURL, "/"))
		case RefAssetCode:
			sb.WriteString(d.Codes[p.ref.Arg])
		case RefField:
			sb.WriteString(d.Fields[p.ref.Arg])
		}
	}
	return sb.String()
}
