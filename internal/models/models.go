package models

import (
	"encoding/json"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// User represents a system user account.
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
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
	IsRunningLow bool    `json:"is_running_low"`
	// LowStockThreshold flags a consumable category as running low when its
	// active count drops below it (README §5.1); nil means no threshold.
	LowStockThreshold *int      `json:"low_stock_threshold"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Event kinds (README §5.1).
const (
	EventRunningLow = "category.running_low"
	EventRestocked  = "category.restocked"
)

// Event triggers: what caused an event.
const (
	TriggerManual    = "manual"
	TriggerThreshold = "threshold"
)

// Event records something notifiers may announce. Its payload is a snapshot
// taken when it happened.
type Event struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	CategoryID  *string      `json:"category_id,omitempty"`
	ActorUserID *string      `json:"actor_user_id,omitempty"`
	Payload     EventPayload `json:"payload"`
	CreatedAt   time.Time    `json:"created_at"`
}

// EventPayload describes an event.
type EventPayload struct {
	Category *EventCategory `json:"category,omitempty"`
	// Actor is the user whose action caused the event, if any.
	Actor *EventActor `json:"actor,omitempty"`
	// Trigger is TriggerManual or TriggerThreshold.
	Trigger string `json:"trigger"`
	// ActiveCount and Threshold are set for threshold events.
	ActiveCount *int `json:"active_count,omitempty"`
	Threshold   *int `json:"threshold,omitempty"`
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
	Email string `json:"email"`
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
