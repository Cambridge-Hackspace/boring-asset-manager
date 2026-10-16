package models

import (
	"encoding/json"
	"time"

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
	LowStockThreshold *int      `json:"low_stock_threshold"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
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
)

// HistoryOnly reports whether events of this kind are kept only for an
// asset's history, with no channel able to subscribe to them.
func HistoryOnly(kind string) bool {
	switch kind {
	case EventQuantityChanged, EventAssetCreated, EventAssetEdited, EventAssetArchived, EventAssetRestored,
		EventAssetCheckedOut, EventAssetCheckedIn:
		return true
	}
	return false
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
}

// APIToken is a token for scripts and dashboards. Its secret is shown once,
// when it is created, and never stored.
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	RoleIDs    []string   `json:"role_ids"`
	CreatedBy  string     `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
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
	// Photos are its photos, in order.
	Photos []AssetPhoto `json:"photos"`
	// Scopes are the scopes it is in: those of its categories and their
	// ancestors, or the default scope (README §3.3).
	Scopes []string `json:"scopes"`
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
	Media      string              `json:"media"`
	// Options are driver options as a JSON object; absent means defaults.
	Options   json.RawMessage `json:"options,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
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
	ID        string          `json:"id"`
	Name      string          `json:"name"`
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
	// roles holding a permission in it.
	CategoryCount int `json:"category_count"`
	RoleCount     int `json:"role_count"`
}
