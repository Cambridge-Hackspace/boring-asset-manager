package labels

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fieldkey"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
)

// RefKind identifies what a content binding placeholder refers to.
type RefKind int

const (
	RefAssetID         RefKind = iota // {{asset.id}}
	RefAssetName                      // {{asset.name}}
	RefAssetCode                      // {{asset.code.<generator_type>}}
	RefField                          // {{field.<key>}}
	RefBaseURL                        // {{base_url}}
	RefAssetCategories                // {{asset.categories}}
	RefAssetLocation                  // {{asset.location}}
	RefAssetCondition                 // {{asset.condition}}
	RefStockQuantity                  // {{stock.quantity}}
	RefStockUnit                      // {{stock.unit}}
	RefLoanBorrower                   // {{loan.borrower}}
	RefLoanDue                        // {{loan.due}}
	RefScanURL                        // {{scan_url}}
	RefAssetIcon                      // {{asset.icon}}
)

// simpleRefs are the placeholders that take no argument.
var simpleRefs = map[string]RefKind{
	"asset.id":         RefAssetID,
	"asset.name":       RefAssetName,
	"base_url":         RefBaseURL,
	"asset.categories": RefAssetCategories,
	"asset.location":   RefAssetLocation,
	"asset.condition":  RefAssetCondition,
	"stock.quantity":   RefStockQuantity,
	"stock.unit":       RefStockUnit,
	"loan.borrower":    RefLoanBorrower,
	"loan.due":         RefLoanDue,
	"scan_url":         RefScanURL,
	"asset.icon":       RefAssetIcon,
}

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
	if kind, ok := simpleRefs[name]; ok {
		return Ref{Kind: kind}, nil
	}
	switch {
	case strings.HasPrefix(name, "asset.code."):
		genType := strings.TrimPrefix(name, "asset.code.")
		if !generators.IsValidGeneratorType(genType) {
			return Ref{}, fmt.Errorf("%w: unknown generator type in {{%s}}", ErrInvalidBinding, name)
		}
		return Ref{Kind: RefAssetCode, Arg: genType}, nil
	case strings.HasPrefix(name, "field."):
		key := strings.TrimPrefix(name, "field.")
		if !fieldkey.Valid(key) {
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
//
// It is sent as JSON to the editor's live renderer, which draws a label in
// the browser with an asset's data.
type Data struct {
	AssetID   string `json:"asset_id"`
	AssetName string `json:"asset_name"`
	BaseURL   string `json:"base_url"`
	// Codes maps generator type to the asset's assigned code.
	Codes map[string]string `json:"codes"`
	// Fields maps field key to the asset's value.
	Fields map[string]string `json:"fields"`
	// Categories, Location, Condition, Quantity, Unit, Borrower and Due are
	// the asset's categories, where it is, its condition, its stock and who
	// has it until when, as text; empty when it has none.
	Categories string `json:"categories"`
	Location   string `json:"location"`
	Condition  string `json:"condition"`
	Quantity   string `json:"quantity"`
	Unit       string `json:"unit"`
	Borrower   string `json:"borrower"`
	Due        string `json:"due"`
	// Icon is the name of the icon the asset is drawn with (README §3.7),
	// its own or its categories', empty when it has none; IconColor is
	// what its category token paints in. BoundIcon is the icon drawn with
	// its params set to the asset's fields, when they are bound to some it
	// has. StateIcon is the variant the asset's state is drawn with, empty
	// when it is in none its icon has.
	Icon      string `json:"icon"`
	IconColor string `json:"icon_color"`
	BoundIcon string `json:"bound_icon,omitempty"`
	StateIcon string `json:"state_icon,omitempty"`
	// LabelIcon is the asset's label icon, as Icon and the rest are its
	// screen icon, when it is another; nil when its labels draw its screen
	// icon.
	LabelIcon *IconData `json:"label_icon,omitempty"`
	// DrawnIcons holds the icon drawn with the asset's own texts (its
	// name, a code), by the name it is drawn under, as isolex's IR has
	// it; an icon it lacks is drawn as the build draws it.
	DrawnIcons map[string]json.RawMessage `json:"drawn_icons,omitempty"`
}

// IconData is the icon an asset is drawn with, as Data has its screen
// icon.
type IconData struct {
	Icon      string `json:"icon"`
	IconColor string `json:"icon_color"`
	BoundIcon string `json:"bound_icon,omitempty"`
	StateIcon string `json:"state_icon,omitempty"`
}

// IconFor is the asset's icon for rendition, RenditionLabel when empty:
// its label icon, or its screen icon when it has none or rendition is
// RenditionScreen.
func (d Data) IconFor(rendition string) IconData {
	if rendition != RenditionScreen && d.LabelIcon != nil {
		return *d.LabelIcon
	}
	return IconData{Icon: d.Icon, IconColor: d.IconColor, BoundIcon: d.BoundIcon, StateIcon: d.StateIcon}
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
		case RefAssetCategories:
			sb.WriteString(d.Categories)
		case RefAssetLocation:
			sb.WriteString(d.Location)
		case RefAssetCondition:
			sb.WriteString(d.Condition)
		case RefStockQuantity:
			sb.WriteString(d.Quantity)
		case RefStockUnit:
			sb.WriteString(d.Unit)
		case RefLoanBorrower:
			sb.WriteString(d.Borrower)
		case RefLoanDue:
			sb.WriteString(d.Due)
		case RefAssetIcon:
			sb.WriteString(d.Icon)
		case RefScanURL:
			// The asset's scan page (README §6.4), by ID, which never changes.
			sb.WriteString(strings.TrimRight(d.BaseURL, "/") + "/scan/" + d.AssetID)
		}
	}
	return sb.String()
}
