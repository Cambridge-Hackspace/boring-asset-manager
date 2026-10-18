package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// How tools show assets: by name and code, where they are and what state
// they are in, as a person would describe them, and with their page.

type assetSummary struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	URL           string            `json:"url,omitempty" jsonschema:"its page in the web app, to give the person"`
	Codes         map[string]string `json:"codes,omitempty" jsonschema:"its codes, by code type (qr, micro_qr, code128)"`
	Categories    []string          `json:"categories,omitempty" jsonschema:"its categories, by name"`
	Where         string            `json:"where,omitempty" jsonschema:"where it is, outermost first, such as Shelf A › Bin 3"`
	Condition     string            `json:"condition" jsonschema:"ok, needs_maintenance or out_of_service"`
	ConditionNote string            `json:"condition_note,omitempty"`
	Stock         *stockView        `json:"stock,omitempty" jsonschema:"set when it is counted by quantity"`
	Loan          *loanView         `json:"loan,omitempty" jsonschema:"set while it is lent out"`
	Archived      bool              `json:"archived,omitempty"`
}

type stockView struct {
	Quantity          int    `json:"quantity"`
	Unit              string `json:"unit,omitempty"`
	LowStockThreshold *int   `json:"low_stock_threshold,omitempty"`
	RunningLow        bool   `json:"running_low"`
}

type loanView struct {
	Borrower string     `json:"borrower,omitempty" jsonschema:"who has it, by name; empty when you may not see who (assets.track)"`
	Since    time.Time  `json:"since"`
	Due      *time.Time `json:"due,omitempty"`
	Overdue  bool       `json:"overdue"`
}

type ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fieldView struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type" jsonschema:"text, number, date, url or select"`
	Value    string `json:"value"`
	Required bool   `json:"required,omitempty"`
}

type linkView struct {
	ID    string `json:"id" jsonschema:"the link's ID, to unlink it"`
	Label string `json:"label" jsonschema:"the link read from this asset, such as contains or contained in"`
	Asset ref    `json:"asset"`
	Type  string `json:"type_id"`
}

type assetDetail struct {
	assetSummary
	CategoryRefs []ref        `json:"category_refs,omitempty"`
	Fields       []fieldView  `json:"fields,omitempty" jsonschema:"its fields, those its categories give it, with their values"`
	Location     []ref        `json:"location,omitempty" jsonschema:"the places it is in, outermost first"`
	Contents     []ref        `json:"contents,omitempty" jsonschema:"what is in it, directly"`
	Links        []linkView   `json:"links,omitempty"`
	Photos       []photoView  `json:"photos,omitempty"`
	Actions      []actionView `json:"actions,omitempty" jsonschema:"the actions it offers, to press_action"`
	Lendable     bool         `json:"lendable"`
	Scopes       []string     `json:"scopes,omitempty"`
	ScanURL      string       `json:"scan_url,omitempty" jsonschema:"what its QR labels open"`
}

type photoView struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
}

type actionView struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty" jsonschema:"a question to ask the person before pressing it"`
}

func summarize(a *models.AssetWithDetails, base string) assetSummary {
	s := assetSummary{ID: a.Asset.ID, Name: a.Asset.Name, Condition: a.Asset.Condition, ConditionNote: a.Asset.ConditionNote,
		Archived: a.Asset.ArchivedAt != nil}
	if base != "" {
		s.URL = base + "/assets/" + url.PathEscape(a.Asset.ID)
	}
	for _, k := range a.GeneratorKeys {
		if s.Codes == nil {
			s.Codes = map[string]string{}
		}
		s.Codes[k.GeneratorType] = k.AssignedCode
	}
	for _, c := range a.Categories {
		s.Categories = append(s.Categories, c.Name)
	}
	var where []string
	for _, l := range a.Location {
		where = append(where, l.Name)
	}
	s.Where = strings.Join(where, " › ")
	if st := a.Asset.Stock; st != nil {
		s.Stock = &stockView{Quantity: st.Quantity, Unit: st.Unit, LowStockThreshold: st.LowStockThreshold, RunningLow: st.IsRunningLow}
	}
	if l := a.Loan; l != nil {
		s.Loan = &loanView{Borrower: l.Borrower.Name, Since: l.CheckedOutAt, Due: l.DueAt, Overdue: l.Overdue}
	}
	return s
}

// runningLow reports whether an asset is flagged running low, itself or by
// one of its categories.
func runningLow(a *models.AssetWithDetails) bool {
	if a.Asset.Stock != nil && a.Asset.Stock.IsRunningLow {
		return true
	}
	for _, c := range a.Categories {
		if c.IsRunningLow {
			return true
		}
	}
	return false
}

func (e *env) detail(ctx context.Context, a *models.AssetWithDetails) (assetDetail, error) {
	base := e.baseURL(ctx)
	d := assetDetail{assetSummary: summarize(a, base), Lendable: a.Lendable, Scopes: a.Scopes}
	if base != "" {
		d.ScanURL = base + "/scan/" + url.PathEscape(a.Asset.ID)
	}
	for _, c := range a.Categories {
		d.CategoryRefs = append(d.CategoryRefs, ref{ID: c.ID, Name: c.Name})
	}
	for _, f := range a.EffectiveFields {
		d.Fields = append(d.Fields, fieldView{Key: f.Field.Key, Label: f.Field.Label, Type: f.Field.ValidationType,
			Value: a.FieldValues[f.Field.Key], Required: f.IsRequired})
	}
	for _, l := range a.Location {
		d.Location = append(d.Location, ref{ID: l.ID, Name: l.Name})
	}
	types, err := e.relationTypes(ctx)
	if err != nil {
		return d, err
	}
	for _, l := range a.Links {
		d.Links = append(d.Links, linkView{ID: l.ID, Label: l.Label, Asset: ref{ID: l.Asset.ID, Name: l.Asset.Name}, Type: l.TypeID})
		if l.Outgoing && types[l.TypeID].IsLocation && !l.Asset.Archived {
			d.Contents = append(d.Contents, ref{ID: l.Asset.ID, Name: l.Asset.Name})
		}
	}
	for _, p := range a.Photos {
		d.Photos = append(d.Photos, photoView{ID: p.ID, Filename: p.Filename})
	}
	for _, o := range a.Actions {
		d.Actions = append(d.Actions, actionView{Key: o.Key, Label: o.Label, Confirm: o.Confirm})
	}
	return d, nil
}

// describe names an asset for a summary: its name, and a code if it has one.
func describe(s assetSummary) string {
	for _, t := range []string{"qr", "micro_qr", "code128"} {
		if c := s.Codes[t]; c != "" {
			return fmt.Sprintf("%q (%s)", s.Name, c)
		}
	}
	return fmt.Sprintf("%q", s.Name)
}

// cache holds what tools read more than once in a request.
type cache struct {
	once  sync.Once
	base  string
	types map[string]models.RelationType
}

// baseURL is the address people reach the server at, or "" if unknown.
func (e *env) baseURL(ctx context.Context) string {
	e.cache.once.Do(func() {
		var info struct {
			BaseURL string `json:"base_url"`
		}
		if e.call(ctx, http.MethodGet, "/api/v1/info", nil, &info) == nil {
			e.cache.base = strings.TrimRight(info.BaseURL, "/")
		}
	})
	return e.cache.base
}

// relationTypes are the link types by ID.
func (e *env) relationTypes(ctx context.Context) (map[string]models.RelationType, error) {
	if e.cache.types != nil {
		return e.cache.types, nil
	}
	var res struct {
		RelationTypes []models.RelationType `json:"relation_types"`
	}
	if err := e.call(ctx, http.MethodGet, "/api/v1/relation-types", nil, &res); err != nil {
		return nil, err
	}
	e.cache.types = map[string]models.RelationType{}
	for _, t := range res.RelationTypes {
		e.cache.types[t.ID] = t
	}
	return e.cache.types, nil
}

// locationType is the link type that says where things are.
func (e *env) locationType(ctx context.Context) (models.RelationType, error) {
	types, err := e.relationTypes(ctx)
	if err != nil {
		return models.RelationType{}, err
	}
	for _, t := range types {
		if t.IsLocation {
			return t, nil
		}
	}
	return models.RelationType{}, fmt.Errorf("no link type says where things are; make one, such as contains, with is_location set")
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// assetID finds the asset a reference names: its ID, or any of its codes,
// looked up as scanning does (assets.scan).
func (e *env) assetID(ctx context.Context, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", fmt.Errorf("name an asset, by its ID or one of its codes")
	}
	if uuidPattern.MatchString(reference) {
		return reference, nil
	}
	var found struct {
		Asset struct {
			ID string `json:"id"`
		} `json:"asset"`
	}
	if err := e.call(ctx, http.MethodGet, "/api/v1/scan/"+url.PathEscape(reference), nil, &found); err != nil {
		return "", fmt.Errorf("finding the asset with code %q: %w", reference, err)
	}
	return found.Asset.ID, nil
}

// asset reads an asset by ID or code.
func (e *env) asset(ctx context.Context, reference string) (*models.AssetWithDetails, error) {
	id, err := e.assetID(ctx, reference)
	if err != nil {
		return nil, err
	}
	var res struct {
		Asset models.AssetWithDetails `json:"asset"`
	}
	if err := e.call(ctx, http.MethodGet, "/api/v1/assets/"+url.PathEscape(id), nil, &res); err != nil {
		return nil, err
	}
	return &res.Asset, nil
}

// page is the part of a list a call shows: limit items (default 25, at
// most 200) from offset.
func page[T any](items []T, offset, limit int) ([]T, int, error) {
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 200 {
		return nil, 0, fmt.Errorf("limit must be from 1 to 200")
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("offset cannot be negative")
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := min(offset+limit, len(items))
	return items[offset:end], len(items) - end, nil
}
