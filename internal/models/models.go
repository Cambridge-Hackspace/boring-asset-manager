package models

import (
	"encoding/json"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// User represents a system user account. Name is for display only; when it
// is empty, the email is shown instead.
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	PasswordHash string     `json:"-"`
	TOTPSecret   *string    `json:"-"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	// Theme is how the web interface looks to them: "light" or "dark", as
	// they chose, or "system" to follow their system's setting.
	Theme string `json:"theme"`
}

// Role represents a defined RBAC role.
type Role struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RoleWithPermissions is a role and the permission keys it grants.
type RoleWithPermissions struct {
	Role
	Permissions []string `json:"permissions"`
	// UserCount is how many users who are not archived hold the role.
	UserCount int `json:"user_count"`
	// Protected is set for the admin role, which cannot be changed.
	Protected bool `json:"protected"`
	// Public is set for the public role: everyone holds it, signed in or
	// not, and it may only read assets.
	Public bool `json:"public"`
}

// RoleAssignment is a role as a user or an API token holds it: in every
// scope its permissions name, or, when ScopeIDs names any, narrowed to those
// scopes (README §3.3).
type RoleAssignment struct {
	RoleID   string   `json:"role_id"`
	ScopeIDs []string `json:"scope_ids"`
}

// SetRoles sets a token's roles, and RoleIDs from them.
func (t *APIToken) SetRoles(roles []RoleAssignment) {
	if roles == nil {
		roles = []RoleAssignment{}
	}
	t.Roles = roles
	t.RoleIDs = make([]string, 0, len(roles))
	for _, a := range roles {
		t.RoleIDs = append(t.RoleIDs, a.RoleID)
	}
}

// Unrestricted assigns each of these roles in every scope.
func Unrestricted(roleIDs []string) []RoleAssignment {
	out := make([]RoleAssignment, 0, len(roleIDs))
	for _, id := range roleIDs {
		out = append(out, RoleAssignment{RoleID: id, ScopeIDs: []string{}})
	}
	return out
}

// Permission represents a granular permission key.
type Permission struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Description string `json:"description"`
}

// Session represents an authenticated user session stored in the database.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Field represents a definition in the global field corpus.
type Field struct {
	ID             string          `json:"id"`
	Key            string          `json:"key"`
	Label          string          `json:"label"`
	ValidationType string          `json:"validation_type"` // text, number, date, url, select
	Options        json.RawMessage `json:"options,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// SelectOptions defines the structured JSON options schema for "select" fields.
type SelectOptions struct {
	Choices []SelectChoice `json:"choices"`
	Default *string        `json:"default,omitempty"`
}

// SelectChoice defines an individual dropdown item in SelectOptions.
type SelectChoice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Category represents a node in the single-parent category tree.
type Category struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	ParentID     *string `json:"parent_id,omitempty"`
	ColorHex     string  `json:"color_hex"`
	IsConsumable bool    `json:"is_consumable"`
	// Unit, on a consumable category, totals its assets' stock in that unit
	// instead of counting them (README §5.1).
	Unit string `json:"unit"`
	// IsLendable lets its assets, and those of its subcategories, be lent.
	IsLendable   bool `json:"is_lendable"`
	IsRunningLow bool `json:"is_running_low"`
	// LowStockThreshold flags a consumable category as running low when its
	// active count drops below it (README §5.1); nil means no threshold.
	LowStockThreshold *int `json:"low_stock_threshold"`
	// ScanTemplateID names the scan template its assets, and those of its
	// subcategories that name none, show when scanned (README §6.4).
	ScanTemplateID *string `json:"scan_template_id"`
	// LabelTemplateIDs names the label templates its assets, and those of
	// its subcategories that name none, join the spool with, in order of
	// preference: each label takes the first that suits its printer
	// (README §6.2). LabelTemplateID is the first, or nil for none.
	LabelTemplateIDs []string  `json:"label_template_ids"`
	LabelTemplateID  *string   `json:"label_template_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Event kinds (README §5.1).
const (
	EventRunningLow      = "category.running_low"
	EventRestocked       = "category.restocked"
	EventAssetRunningLow = "asset.running_low"
	EventAssetRestocked  = "asset.restocked"
	// EventQuantityChanged records a change of an asset's quantity, for its
	// history. Channels cannot subscribe to it.
	EventQuantityChanged = "asset.quantity_changed"
	// EventAssetCreated, EventAssetEdited, EventAssetArchived and
	// EventAssetRestored record changes to an asset, listed in the payload's
	// Changes, for its history. Channels cannot subscribe to them either.
	EventAssetCreated  = "asset.created"
	EventAssetEdited   = "asset.edited"
	EventAssetArchived = "asset.archived"
	EventAssetRestored = "asset.restored"
	// EventAssetConditionChanged records a change of an asset's condition, in
	// the payload's Changes, with its Note. Channels can subscribe to it.
	EventAssetConditionChanged = "asset.condition_changed"
	// EventAssetCheckedOut and EventAssetCheckedIn record a loan, with its
	// Borrower and, when checked out, when it is due; they are for history.
	// EventAssetOverdue records once that a loan is past due; channels can
	// subscribe to it.
	EventAssetCheckedOut = "asset.checked_out"
	EventAssetCheckedIn  = "asset.checked_in"
	EventAssetOverdue    = "asset.overdue"
	// EventFlowOverdue records once that something a flow filed is past
	// its date to collect by (README §6.7), with the Flow and the date as
	// DueAt; channels can subscribe to it.
	EventFlowOverdue = "asset.flow_overdue"
	// EventAssetLabelPrinted records labels of an asset sent to a printer or
	// downloaded from the spool, with its Label; it is for history.
	EventAssetLabelPrinted = "asset.label_printed"
	// EventAssetAction records an action pressed on an asset, with its
	// Action; channels and live connections can subscribe to it.
	EventAssetAction = "asset.action"
	// EventCategoryIconChanged records a new published version of a
	// category's icon (README §3.7), so that what shows it can redraw; it
	// is for history.
	EventCategoryIconChanged = "category.icon_changed"
)

// HistoryOnly reports whether events of this kind are kept only for an
// asset's history, with no channel able to subscribe to them.
func HistoryOnly(kind string) bool {
	switch kind {
	case EventQuantityChanged, EventAssetCreated, EventAssetEdited, EventAssetArchived, EventAssetRestored,
		EventAssetCheckedOut, EventAssetCheckedIn, EventAssetLabelPrinted, EventCategoryIconChanged:
		return true
	}
	return false
}

// EventFlow is a flow, as an event names it.
type EventFlow struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Event triggers: what caused an event.
const (
	TriggerManual    = "manual"
	TriggerThreshold = "threshold"
	// TriggerImport marks a change made by a CSV import.
	TriggerImport = "import"
	// TriggerSchedule marks an event BAM recorded by itself, such as a loan
	// becoming overdue.
	TriggerSchedule = "schedule"
)

// Event records something notifiers may announce. Its payload is a snapshot
// taken when it happened.
type Event struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	CategoryID  *string      `json:"category_id,omitempty"`
	AssetID     *string      `json:"asset_id,omitempty"`
	ActorUserID *string      `json:"actor_user_id,omitempty"`
	Payload     EventPayload `json:"payload"`
	CreatedAt   time.Time    `json:"created_at"`
}

// EventPayload describes an event.
type EventPayload struct {
	// Category or Asset is what the event is about.
	Category *EventCategory `json:"category,omitempty"`
	Asset    *EventAsset    `json:"asset,omitempty"`
	// Actor is the user whose action caused the event, if any.
	Actor *EventActor `json:"actor,omitempty"`
	// Trigger is TriggerManual or TriggerThreshold.
	Trigger string `json:"trigger"`
	// ActiveCount and Threshold are set for a category's threshold events.
	// An asset's threshold events set Quantity and Threshold instead.
	ActiveCount *int `json:"active_count,omitempty"`
	Threshold   *int `json:"threshold,omitempty"`
	// Quantity is an asset's quantity after the event, and Change how much a
	// quantity change moved it. Unit is the asset's unit, if it has one.
	Quantity *int   `json:"quantity,omitempty"`
	Change   *int   `json:"change,omitempty"`
	Unit     string `json:"unit,omitempty"`
	// Changes lists what an asset's created, edited or archived event changed.
	Changes []EventChange `json:"changes,omitempty"`
	// Note is what a person said about a change of condition.
	Note string `json:"note,omitempty"`
	// Borrower is who has, or had, the asset of a loan event, and DueAt when
	// the loan is due back, if ever.
	Borrower *EventActor `json:"borrower,omitempty"`
	DueAt    *time.Time  `json:"due_at,omitempty"`
	// Flow is the flow that filed the asset, for EventFlowOverdue.
	Flow *EventFlow `json:"flow,omitempty"`
	// Label is what a label printed event printed.
	Label *EventLabel `json:"label,omitempty"`
	// Action is the action an action event pressed, and what a device
	// needs to find the thing.
	Action *EventAction `json:"action,omitempty"`
}

// EventLabel describes an asset's labels printed (README §6.2): the
// template, as named then; the printer, as named then, or Download for a
// PDF or ZIP from the spool; and how many copies.
type EventLabel struct {
	Template string `json:"template"`
	Printer  string `json:"printer,omitempty"`
	Download bool   `json:"download,omitempty"`
	Copies   int    `json:"copies"`
}

// EventChange is one change to an asset, as it read when it was made. What
// says which part changed:
//
//   - "name": From and To are the old and new names.
//   - "category": the asset joined the category named To, or left the one
//     named From; Key is the category's ID.
//   - "field": Key is the field's key and Label its label then.
//   - "code": Key is the generator type and Label its name.
//   - "stock": To is "counted" when counting by quantity starts, and From
//     is "counted" when it stops. The quantity has its own events.
//   - "unit" and "threshold": the stock's unit and low-stock threshold.
//   - "icon": its own icon's name; an empty one is its categories'.
//
// An empty From or To means there was none.
type EventChange struct {
	What  string `json:"what"`
	Key   string `json:"key,omitempty"`
	Label string `json:"label,omitempty"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// EventAsset is the asset an event is about, as it was then.
type EventAsset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EventCategory is the category an event is about, as it was then.
type EventCategory struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ColorHex string `json:"color_hex"`
}

// EventActor is a user, as they were when the event happened.
type EventActor struct {
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	// Token is set when an API token acted; ID and Name are then the token's.
	Token bool `json:"token,omitempty"`
	// Via names the assistant the change was made through, by the name its
	// MCP client gives, or "MCP" when it gives none (README §6.6).
	Via string `json:"via,omitempty"`
}

// Signature is who acted, as messages name them: the name, and the
// assistant they acted through, if any ("Ada, via Claude Desktop").
func (a *EventActor) Signature() string {
	if a.Via == "" {
		return a.Display()
	}
	return a.Display() + ", via " + a.Via
}

// APIToken is a token for scripts and dashboards. Its secret is shown once,
// when it is created, and never stored.
type APIToken struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	RoleIDs []string `json:"role_ids"`
	// Roles are its roles with the scopes each is narrowed to.
	Roles []RoleAssignment `json:"roles"`
	// UserID is set on a personal token: it holds no roles and acts as this
	// user, named UserName.
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	// OAuthClient names the assistant a grant signed in through OAuth
	// went to; such a token renews until RefreshExpiresAt while used.
	OAuthClient      string     `json:"oauth_client,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        *time.Time `json:"expires_at"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
}

// OAuthClient is an assistant registered to sign people in (README §6.6).
type OAuthClient struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// Display is the actor's name, or their email when they have none.
func (a *EventActor) Display() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

// CategoryField defines field assignment and requirement constraint on a category.
type CategoryField struct {
	CategoryID string `json:"category_id"`
	FieldID    string `json:"field_id"`
	IsRequired bool   `json:"is_required"`
}

// EffectiveField represents a field resolved through category inheritance.
type EffectiveField struct {
	Field            Field  `json:"field"`
	IsRequired       bool   `json:"is_required"`
	SourceCategoryID string `json:"source_category_id,omitempty"`
}

// Asset represents a tracked physical item.
type Asset struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	// Stock is the asset's stock when it is counted by quantity (README
	// §5.1), and nil when it is not.
	Stock *AssetStock `json:"stock"`
	// Condition is ConditionOK, ConditionNeedsMaintenance or
	// ConditionOutOfService, with a note saying what is wrong.
	Condition     string `json:"condition"`
	ConditionNote string `json:"condition_note"`
	// IconName is the asset's own icon (README §3.7), drawn in place of its
	// categories'; nil when it has none. It is its screen icon
	// (RenditionScreen); LabelIconName is its own label icon.
	IconName      *string `json:"icon_name"`
	LabelIconName *string `json:"label_icon_name"`
}

// OwnIcon is the asset's own icon for rendition, nil when it has none.
func (a *Asset) OwnIcon(rendition string) *string {
	if rendition == RenditionLabel {
		return a.LabelIconName
	}
	return a.IconName
}

// AssetStock is an asset's stock, counted by quantity.
type AssetStock struct {
	Quantity int    `json:"quantity"`
	Unit     string `json:"unit"`
	// LowStockThreshold flags the asset as running low when its quantity
	// drops below it; nil means no threshold.
	LowStockThreshold *int `json:"low_stock_threshold"`
	IsRunningLow      bool `json:"is_running_low"`
}

// AssetFieldValue stores an asset's field value as text.
type AssetFieldValue struct {
	AssetID   string    `json:"asset_id"`
	FieldID   string    `json:"field_id"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AssetGeneratorKey stores compact/custom codes mapped to generator types.
type AssetGeneratorKey struct {
	AssetID       string `json:"asset_id"`
	GeneratorType string `json:"generator_type"` // "qr", "micro_qr", "code128"
	AssignedCode  string `json:"assigned_code"`
}

// AssetWithDetails represents an aggregate view of an asset with its relations.
type AssetWithDetails struct {
	Asset           Asset               `json:"asset"`
	Categories      []Category          `json:"categories"`
	GeneratorKeys   []AssetGeneratorKey `json:"generator_keys"`
	FieldValues     map[string]string   `json:"field_values"` // key -> value
	EffectiveFields []EffectiveField    `json:"effective_fields"`
	// Lendable is set when one of its categories, or their ancestors, is
	// lendable; Loan is its open loan, if it is lent out.
	// Weighed is set for an asset whose stock is its weight in the unit
	// one of its categories totals: a spool, not a pooled quantity.
	Weighed  bool  `json:"weighed"`
	Lendable bool  `json:"lendable"`
	Loan     *Loan `json:"loan"`
	// Links are the asset's links to others; Location is where it is,
	// outermost first, through location links; SuggestedRelationTypeIDs are
	// the types its categories, or their ancestors, suggest.
	Links                    []AssetLink   `json:"links"`
	Location                 []LinkedAsset `json:"location"`
	SuggestedRelationTypeIDs []string      `json:"suggested_relation_type_ids"`
	// Holds is how many active assets are in it, through location links,
	// directly: what its scene shows.
	Holds int `json:"holds"`
	// Photos are its photos, in order.
	Photos []AssetPhoto `json:"photos"`
	// Scopes are the scopes it is in: those of its categories and their
	// ancestors, or the default scope (README §3.3).
	Scopes []string `json:"scopes"`
	// ScanTemplate is the scan template it shows when scanned, and why.
	ScanTemplate TemplateChoice `json:"scan_template"`
	// LabelTemplate is the label template the spool queues it with, and why;
	// TemplateID is empty when no category names one.
	LabelTemplate TemplateChoice `json:"label_template"`
	// Actions are those its categories offer on it (README §5.4).
	Actions []OfferedAction `json:"actions"`
	// ResolvedIcon is the icon it is drawn with on screen, and why; nil
	// when neither it nor any of its categories has one.
	ResolvedIcon *ResolvedIcon `json:"resolved_icon"`
	// ResolvedIcons is the icon it is drawn with for each rendition it has
	// one for, ResolvedIcon among them.
	ResolvedIcons map[string]*ResolvedIcon `json:"resolved_icons"`
}

// Renditions are the forms an icon takes for where it is drawn (README
// §3.7): RenditionScreen, every icon on screen, and RenditionLabel, on
// labels. A category, and an asset, has an icon for each; one with no
// label icon is drawn on labels with its screen icon.
const (
	RenditionScreen = "screen"
	RenditionLabel  = "label"
)

// Renditions lists every rendition, the default first.
var Renditions = []string{RenditionScreen, RenditionLabel}

// ValidRendition reports whether r is a rendition.
func ValidRendition(r string) bool {
	for _, x := range Renditions {
		if r == x {
			return true
		}
	}
	return false
}

// Where an asset's icon comes from (ResolvedIcon.From).
const (
	IconFromAsset    = "asset"
	IconFromCategory = "category"
)

// ResolvedIcon is the icon an asset is drawn with (README §3.7): its own,
// From IconFromAsset, or else that of the nearest category up from its
// categories with one, From IconFromCategory, chosen as its templates are
// (TemplateChoice), named SourceCategoryName. Missing is set when the current build draws no icon
// by Name, so the unknown icon is drawn in its place. Color is what the
// "category" token paints in: the color of its nearest category with one,
// chosen the same way, or empty for the palette's own.
//
// When params of its icon are bound to fields it has numbers in (README
// §3.7), Bound is the wrapper drawing the icon with them, Params, and
// Name is that. When the asset is in a state (IconState*) whose variant
// the build draws for that, Name is the variant, <Bound or Base>-<Variant>,
// and State says which. Base is the icon as it is named, with neither,
// set whenever Name is not it.
type ResolvedIcon struct {
	Name string `json:"name"`
	From string `json:"from"`
	// Rendition is whose icon it is: the rendition asked for, or
	// RenditionScreen when there is no icon for that one.
	Rendition          string             `json:"rendition"`
	SourceCategoryID   string             `json:"source_category_id,omitempty"`
	SourceCategoryName string             `json:"source_category_name,omitempty"`
	Missing            bool               `json:"missing,omitempty"`
	Color              string             `json:"color,omitempty"`
	Base               string             `json:"base,omitempty"`
	Bound              string             `json:"bound,omitempty"`
	Params             map[string]float64 `json:"params,omitempty"`
	State              string             `json:"state,omitempty"`
	Variant            string             `json:"variant,omitempty"`
	// Texts are the asset's own texts its icon's text params are set to,
	// by param: its name, a code, a field's value (README §3.7).
	Texts map[string]string `json:"texts,omitempty"`
	// Drawn holds the icon drawn with Texts, by the name it is drawn
	// under: Name, and, when that shows a state, Shape too. A name it
	// lacks is drawn as the build draws it.
	Drawn map[string]json.RawMessage `json:"drawn,omitempty"`
}

// FirstCode is the code an asset is known by where one is shown: its QR
// code, else its Micro QR, else its Code 128, else whichever it has.
func (a *AssetWithDetails) FirstCode() string {
	for _, t := range []string{"qr", "micro_qr", "code128"} {
		for _, k := range a.GeneratorKeys {
			if k.GeneratorType == t {
				return k.AssignedCode
			}
		}
	}
	if len(a.GeneratorKeys) > 0 {
		return a.GeneratorKeys[0].AssignedCode
	}
	return ""
}

// Shape is what the asset is drawn as but for its state: Bound, or Base,
// or Name.
func (r *ResolvedIcon) Shape() string {
	if r.Bound != "" {
		return r.Bound
	}
	return r.Plain()
}

// IconParamBinding is a param of a category's icon bound to what an asset
// has (README §3.7): a field's value, Source "field", or for a text param,
// its name ("name") or first code ("code"), with no field.
type IconParamBinding struct {
	Param      string `json:"param"`
	Source     string `json:"source"`
	FieldID    string `json:"field_id"`
	FieldKey   string `json:"field_key"`
	FieldLabel string `json:"field_label"`
}

// Plain is the icon without its state: Base, or Name when no state is
// shown.
func (r *ResolvedIcon) Plain() string {
	if r.Base != "" {
		return r.Base
	}
	return r.Name
}

// The states an asset's icon can show (README §3.7), worst first: an
// asset in several shows the first whose variant its icon has.
const (
	IconStateOutOfService     = "out_of_service"
	IconStateNeedsMaintenance = "needs_maintenance"
	IconStateOverdue          = "overdue"
	IconStateOnLoan           = "on_loan"
	IconStateOutOfStock       = "out_of_stock"
	IconStateLowStock         = "low_stock"
)

// IconStates are the states, worst first.
var IconStates = []string{IconStateOutOfService, IconStateNeedsMaintenance, IconStateOverdue,
	IconStateOnLoan, IconStateOutOfStock, IconStateLowStock}

// DefaultIconVariants are the variants each state is drawn with until
// someone chooses others.
var DefaultIconVariants = map[string]string{
	IconStateOutOfService:     "down",
	IconStateNeedsMaintenance: "maintenance",
	IconStateOverdue:          "overdue",
	IconStateOnLoan:           "lent",
	IconStateOutOfStock:       "empty",
	IconStateLowStock:         "low",
}

// TemplateChoice is the scan template (README §6.4) or label template
// (§6.2) an asset uses: the one its nearest category naming one names,
// SourceCategoryID, or, for scan templates, the default when none does.
// Conflict is set when categories at the same distance name different
// templates and the tie was broken by how deep they are, how many
// subcategories they have or how old they are; Contenders are those
// categories, for someone to settle.
type TemplateChoice struct {
	TemplateID string `json:"template_id"`
	// TemplateIDs, for label templates, are all those the category names,
	// in order of preference; TemplateID is the first.
	TemplateIDs      []string `json:"template_ids,omitempty"`
	SourceCategoryID string   `json:"source_category_id,omitempty"`
	Conflict         bool     `json:"conflict,omitempty"`
	Contenders       []string `json:"contenders,omitempty"`
}

// Image is an uploaded PNG or JPEG placed on labels by static_image elements.
type Image struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	WidthPx     int       `json:"width_px"`
	HeightPx    int       `json:"height_px"`
	ByteSize    int64     `json:"byte_size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

// Printer is a label printer.
type Printer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Model  string `json:"model"`
	// Connection says how BAM reaches the printer.
	Connection printers.Connection `json:"connection"`
	// Media names the roll loaded: one the driver has built in, or custom
	// stock by its ID.
	Media string `json:"media"`
	// Stock is the custom stock Media names, if it names one.
	Stock *printers.Stock `json:"stock,omitempty"`
	// Loaded describes the roll Media names, built in or custom.
	Loaded *printers.MediaInfo `json:"loaded,omitempty"`
	// Options are driver options as a JSON object; absent means defaults.
	Options   json.RawMessage `json:"options,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// LabelStock is custom stock as stored: its size, and the printers that
// have it loaded.
type LabelStock struct {
	printers.Stock
	// Printers names the printers with it loaded.
	Printers  []string  `json:"printers"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NotificationChannel sends events to one destination through a notifier
// driver (README §5.2). Options hold the driver's options, secrets included;
// the API never returns secret values.
type NotificationChannel struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Driver    string            `json:"driver"`
	Options   map[string]string `json:"options"`
	Enabled   bool              `json:"enabled"`
	Events    []string          `json:"events"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Delivery states.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
)

// Delivery is one event sent, or to be sent, to one channel.
type Delivery struct {
	ID            string     `json:"id"`
	EventID       string     `json:"event_id"`
	ChannelID     string     `json:"channel_id"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LastError     string     `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// An asset's condition (README §3.2).
const (
	ConditionOK               = "ok"
	ConditionNeedsMaintenance = "needs_maintenance"
	ConditionOutOfService     = "out_of_service"
)

// ValidCondition reports whether c is one of the conditions above.
func ValidCondition(c string) bool {
	return c == ConditionOK || c == ConditionNeedsMaintenance || c == ConditionOutOfService
}

// Person is a user as others see them: in loans and pickers.
type Person struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	// Archived is set for a user who has left; their loans stay theirs.
	Archived bool `json:"archived,omitempty"`
}

// Loan is an asset lent to someone (README §5.3). It is open until
// CheckedInAt is set, and Overdue while open past DueAt.
type Loan struct {
	ID           string     `json:"id"`
	AssetID      string     `json:"asset_id"`
	AssetName    string     `json:"asset_name"`
	Borrower     Person     `json:"borrower"`
	CheckedOutAt time.Time  `json:"checked_out_at"`
	DueAt        *time.Time `json:"due_at"`
	CheckedInAt  *time.Time `json:"checked_in_at"`
	Overdue      bool       `json:"overdue"`
}

// RelationType is a kind of link between assets (README §3.6), named from
// each end: an asset "contains" another, which is "contained in" it.
type RelationType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	InverseName string `json:"inverse_name"`
	// IsSymmetric reads the same both ways, as "used with" does.
	IsSymmetric bool `json:"is_symmetric"`
	// IsSingle lets an asset be the far end of one link of the type only,
	// so linking it again moves it.
	IsSingle bool `json:"is_single"`
	// IsLocation makes its links an asset's location.
	IsLocation bool      `json:"is_location"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// LinkCount is how many links use it.
	LinkCount int `json:"link_count"`
}

// LinkedAsset is the asset at the other end of a link, or in a location.
type LinkedAsset struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived,omitempty"`
}

// AssetLink is a link as one of its assets sees it: Label is the type's
// name read from that end ("contains" or "contained in").
type AssetLink struct {
	ID     string      `json:"id"`
	TypeID string      `json:"type_id"`
	Label  string      `json:"label"`
	Asset  LinkedAsset `json:"asset"`
	// Outgoing is set when this asset is the link's from end.
	Outgoing bool `json:"outgoing"`
}

// Report is a saved report (README §6.3): a name and its spec, JSON that
// the reports package reads.
type Report struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is report or scan (README §6.4); Default marks the scan template
	// assets use when no category names one.
	Kind      string          `json:"kind"`
	Default   bool            `json:"default"`
	Spec      json.RawMessage `json:"spec"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// AssetPhoto is a photo of an asset; its bytes, and its thumbnail's, are
// stored as files.
type AssetPhoto struct {
	ID          string    `json:"id"`
	ThumbID     string    `json:"-"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	WidthPx     int       `json:"width_px"`
	HeightPx    int       `json:"height_px"`
	ByteSize    int64     `json:"byte_size"`
	CreatedAt   time.Time `json:"created_at"`
}

// Scope is a label on categories that roles can hold asset permissions in
// (README §3.3). Default is the reserved scope of assets in no other.
type Scope struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Reserved  bool      `json:"reserved"`
	// CategoryCount and RoleCount count the categories it labels and the
	// roles holding a permission in it; AssignmentCount, the roles of users
	// and API tokens narrowed to it.
	CategoryCount   int `json:"category_count"`
	RoleCount       int `json:"role_count"`
	AssignmentCount int `json:"assignment_count"`
}

// Spool destinations (README §6.2): DestinationDownload, or
// DestinationPrinterPrefix or DestinationPoolPrefix followed by a printer's
// or a pool's ID. An entry with none waits for one to be chosen.
const (
	DestinationDownload      = "download"
	DestinationPrinterPrefix = "printer:"
	DestinationPoolPrefix    = "pool:"
)

// SpoolEntry is one asset's label in the spool (README §6.2): waiting to be
// printed, or printed once PrintedAt is set.
type SpoolEntry struct {
	ID            string `json:"id"`
	AssetID       string `json:"asset_id"`
	AssetName     string `json:"asset_name"`
	AssetArchived bool   `json:"asset_archived"`
	// AssetScopes are the asset's scopes, for checking who may see it.
	AssetScopes []string `json:"-"`
	// TemplateFromCategory is set when the template is the asset's
	// categories' choice, so it may move to another of theirs when its
	// printer's roll changes; one chosen by hand stays.
	TemplateFromCategory bool `json:"template_from_category"`
	// TemplateID is the label template, if one is chosen; Destination is
	// where it prints, empty until chosen.
	TemplateID  *string `json:"template_id"`
	Destination string  `json:"destination"`
	Copies      int     `json:"copies"`
	// Problem says why its label could not be produced last time.
	Problem string `json:"problem,omitempty"`
	// Job is the job it is being printed in, while that is waiting or
	// sending; it cannot be changed meanwhile.
	Job     *SpoolJobRef `json:"job,omitempty"`
	AddedBy EventActor   `json:"added_by"`
	AddedAt time.Time    `json:"added_at"`
	// PrintedAt, PrintedBy, PrintedTemplate and PrintedOn say when, by
	// whom, with which template and on which printer (or "Download") it
	// was printed, as they were named then.
	PrintedAt       *time.Time  `json:"printed_at,omitempty"`
	PrintedBy       *EventActor `json:"printed_by,omitempty"`
	PrintedTemplate string      `json:"printed_template,omitempty"`
	PrintedOn       string      `json:"printed_on,omitempty"`
}

// Spool job and batch states (README §6.2).
const (
	SpoolWaiting  = "waiting"
	SpoolSending  = "sending"
	SpoolDone     = "done"
	SpoolFailed   = "failed"
	SpoolCanceled = "canceled"
)

// SpoolJobRef names the job a waiting entry is in, while it is being
// printed: its state, and the printer it goes to.
type SpoolJobRef struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Target string `json:"target"`
}

// SpoolJob is a printing of spool entries on one printer with one
// template, or a download of them (README §6.2). Its template and target
// are named as they were then. Labels counts every copy; Printed those
// printed so far.
type SpoolJob struct {
	ID           string  `json:"id"`
	TemplateID   *string `json:"template_id"`
	TemplateName string  `json:"template_name"`
	PrinterID    *string `json:"printer_id"`
	Target       string  `json:"target"`
	// PoolID is the pool it went to, if any; Members say how each of its
	// printers is getting on with it.
	PoolID     *string          `json:"pool_id,omitempty"`
	Members    []SpoolJobMember `json:"members,omitempty"`
	Download   bool             `json:"download"`
	State      string           `json:"state"`
	Error      string           `json:"error,omitempty"`
	Labels     int              `json:"labels"`
	Printed    int              `json:"printed"`
	Entries    int              `json:"entries"`
	CreatedBy  EventActor       `json:"created_by"`
	CreatedAt  time.Time        `json:"created_at"`
	StartedAt  *time.Time       `json:"started_at,omitempty"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
}

// SpoolJobMember is a pool's printer in one of its jobs: the labels it has
// printed, whether it is sending now, and whether it dropped out after
// failing.
type SpoolJobMember struct {
	PrinterID string `json:"printer_id"`
	Printer   string `json:"printer"`
	Printed   int    `json:"printed"`
	Sending   bool   `json:"sending"`
	Dropped   bool   `json:"dropped"`
}

// PrinterPool is a set of printers loaded alike that share print jobs
// (README §6.2). Each job is cut into batches of BatchSize labels. Members
// take the next batch when they have sent their last, except those behind
// a spooler, and every member when FixedShares is set, which take a fixed
// share of the batches by their Share.
type PrinterPool struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	BatchSize   int          `json:"batch_size"`
	FixedShares bool         `json:"fixed_shares"`
	Members     []PoolMember `json:"members"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// PoolMember is a printer in a pool, and its share of fixed-share batches.
type PoolMember struct {
	PrinterID string `json:"printer_id"`
	Share     int    `json:"share"`
}

// AssetAction is an action people press on assets, such as Identify
// (README §5.4). Pressing one records an asset.action event, which devices
// and notification channels act on. Confirm, when set, is asked first.
type AssetAction struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	Confirm   string    `json:"confirm"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CategoryAction is an action a category offers on its assets, and with
// Cascades on those of its subcategories.
type CategoryAction struct {
	ActionID string `json:"action_id"`
	Cascades bool   `json:"cascades"`
}

// OfferedAction is an action an asset's categories offer on it.
type OfferedAction struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
}

// EventAction is the action an asset.action event pressed: its key and
// label, and the asset and the containers it is in, outermost first, each
// with its codes and field values, for a device to find it by.
type EventAction struct {
	Key      string       `json:"key"`
	Label    string       `json:"label"`
	Asset    EventThing   `json:"asset"`
	Location []EventThing `json:"location"`
}

// EventThing is an asset as an action event carries it: its codes, by
// generator type, and its field values, by field key.
type EventThing struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Codes  map[string]string `json:"codes"`
	Fields map[string]string `json:"fields"`
}

// LiveConnection is a live event connection a server holds open (README
// §5.4): for an API token, or for a signed-in user.
type LiveConnection struct {
	ID       string    `json:"id"`
	TokenID  *string   `json:"token_id,omitempty"`
	UserID   *string   `json:"user_id,omitempty"`
	Remote   string    `json:"remote"`
	OpenedAt time.Time `json:"opened_at"`
	SeenAt   time.Time `json:"seen_at"`
}

// ActionReply is a device's answer to an action pressed: done or failed,
// with a message, and who answered as they were named then.
type ActionReply struct {
	ID      string `json:"id"`
	EventID string `json:"event_id"`
	Name    string `json:"name"`
	Token   bool   `json:"token"`
	// ReplierID is the token or user that answered, so each answers once.
	ReplierID string    `json:"-"`
	OK        bool      `json:"ok"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// IconDiag is an error or warning the icon compiler reported, at a line
// and column of a file of the build (README §3.7).
type IconDiag struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// CategoryIcon is a category's isolex source for a rendition (README §3.7). Source is
// the published version, nil until one has compiled; Draft is a later
// edit that has not, with the errors that stopped it.
type CategoryIcon struct {
	CategoryID  string     `json:"category_id"`
	Rendition   string     `json:"rendition"`
	IconName    string     `json:"icon_name"`
	Source      *string    `json:"source"`
	Draft       *string    `json:"draft"`
	DraftErrors []IconDiag `json:"draft_errors"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IconLibraryFile is isolex source that no one category owns: bases,
// fragments and axes icons share. Source and Draft as for CategoryIcon.
type IconLibraryFile struct {
	ID          string     `json:"id"`
	Path        string     `json:"path"`
	Source      *string    `json:"source"`
	Draft       *string    `json:"draft"`
	DraftErrors []IconDiag `json:"draft_errors"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IconColor is a palette entry: a theme token and what it is painted
// with in light and dark mode.
type IconColor struct {
	Token string `json:"token"`
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// IconSettings is the rest of the lexicon block bam writes for every
// build: the stroke widths allowed (none means any), the tokens only a
// grant may paint with, and the compiler's limits by name (0 for none).
type IconSettings struct {
	StrokeWidths []float64        `json:"stroke_widths"`
	Reserved     []string         `json:"reserved"`
	Limits       map[string]int64 `json:"limits"`
}

// IconBuild is the last build that compiled clean: what icons are drawn
// from. Hash identifies its inputs, the isolex version among them.
type IconBuild struct {
	Hash          string          `json:"hash"`
	IsolexVersion string          `json:"isolex_version"`
	IR            json.RawMessage `json:"ir"`
	Warnings      []IconDiag      `json:"warnings"`
	BuiltAt       time.Time       `json:"built_at"`
}

// IconRevision is one published version of a source.
type IconRevision struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	ActorID   *string   `json:"actor_user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// IconSave is the outcome of saving a source: published, with the build
// it made, or kept as a draft with the errors that stopped it, wherever
// in the lexicon they are. Warnings never stop a save.
type IconSave struct {
	Published bool       `json:"published"`
	Errors    []IconDiag `json:"errors"`
	Warnings  []IconDiag `json:"warnings"`
	Build     *IconBuild `json:"build,omitempty"`
}

// Flow is a guided page for doing one everyday thing with assets (README
// §6.7): its address, /f/<slug>, its name, and what it does.
type Flow struct {
	ID         string           `json:"id"`
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Definition flows.Definition `json:"definition"`
	CreatedBy  string           `json:"created_by,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// FlowRun is someone working through a flow: the step they are on, the
// run's values, and what the steps done so far did.
type FlowRun struct {
	ID        string       `json:"id"`
	FlowID    string       `json:"flow_id"`
	UserID    string       `json:"user_id"`
	Step      int          `json:"step"`
	Values    flows.Values `json:"values"`
	Done      []FlowDone   `json:"done"`
	Finished  bool         `json:"finished"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	// Fields and Progress are kept for the run's own use (store.FlowRunState).
	Fields   map[string]string `json:"-"`
	Progress map[string]int    `json:"-"`
	// Answered is kept for the run's own use too: how many steps that ask
	// something the person has answered (#166).
	Answered int `json:"-"`
}

// FlowDone is what one step of a run did, in words, for Done to show.
type FlowDone struct {
	Step string `json:"step"`
	Kind string `json:"kind"`
	Said string `json:"said"`
}

// Upload is an archive being uploaded through the web in parts, or one
// uploaded, staged until it is used (README §2.3, #141).
type Upload struct {
	ID       string `json:"id"`
	UserID   string `json:"-"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	PartSize int64  `json:"part_size"`
	Received int64  `json:"received"`
	// HashState is the SHA-256 of what has arrived, as Go's hash state.
	HashState []byte `json:"-"`
	// StagingToken is what the staging needs to add to it: S3's upload ID.
	StagingToken string    `json:"-"`
	Complete     bool      `json:"complete"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
