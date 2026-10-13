package models

import (
	"encoding/json"
	"time"
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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ParentID     *string   `json:"parent_id,omitempty"`
	ColorHex     string    `json:"color_hex"`
	IsConsumable bool      `json:"is_consumable"`
	IsRunningLow bool      `json:"is_running_low"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

// Template represents a physical label layout specification.
type Template struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Units      string          `json:"units"` // "mm", "in"
	Width      float64         `json:"width"`
	Height     float64         `json:"height"`
	DPI        int             `json:"dpi"`
	LayoutJSON json.RawMessage `json:"layout_json"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}
