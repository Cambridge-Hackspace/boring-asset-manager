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
	// EmailNotifications is whether email sent to a role they hold reaches
	// them (docs/notifications.md). It is on until they turn it off.
	EmailNotifications bool `json:"email_notifications"`
	// LoanReminders is whether they are emailed about their loans before
	// they are due and once overdue, when the instance sends reminders. It
	// is on until they turn it off.
	LoanReminders bool `json:"loan_reminders"`
	// MentionEmails is whether they are emailed when a comment mentions
	// them (docs/comments.md#mentions). It is on until they turn it off.
	MentionEmails bool `json:"mention_emails"`
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
	// RequiresTwoFactor makes whoever holds the role sign in with a second
	// factor (docs/access.md#requiring-two-factor-sign-in).
	RequiresTwoFactor bool `json:"requires_two_factor"`
	// Protected is set for the admin role, which cannot be changed.
	Protected bool `json:"protected"`
	// Public is set for the public role: everyone holds it, signed in or
	// not, and it may only read assets.
	Public bool `json:"public"`
}

// RoleAssignment is a role as a user or an API token holds it: in every
// scope its permissions name, or, when ScopeIDs names any, narrowed to those
// scopes (docs/access.md).
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
	// FactorSetup marks the session of someone required to have a second
	// factor who has none: it can do nothing but set one up.
	FactorSetup bool `json:"factor_setup"`
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
	// instead of counting them (docs/stock-and-lending.md).
	Unit string `json:"unit"`
	// IsLendable lets its assets, and those of its subcategories, be lent.
	IsLendable   bool `json:"is_lendable"`
	IsRunningLow bool `json:"is_running_low"`
	// LowStockThreshold flags a consumable category as running low when its
	// active count drops below it (docs/stock-and-lending.md); nil means no threshold.
	LowStockThreshold *int `json:"low_stock_threshold"`
	// ScanTemplateID names the scan template its assets, and those of its
	// subcategories that name none, show when scanned (docs/scanning.md).
	ScanTemplateID *string `json:"scan_template_id"`
	// LabelTemplateIDs names the label templates its assets, and those of
	// its subcategories that name none, join the spool with, in order of
	// preference: each label takes the first that suits its printer
	// (docs/printing.md). LabelTemplateID is the first, or nil for none.
	LabelTemplateIDs []string `json:"label_template_ids"`
	LabelTemplateID  *string  `json:"label_template_id"`
	// VendorIDs are the vendors it is bought from, in order of preference
	// (docs/vendors.md): set by the API, with those the requester may see,
	// and left empty by the store's reads (store.WithCategoryVendors).
	VendorIDs []string  `json:"vendor_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Event kinds (docs/stock-and-lending.md).
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
	// its date to collect by (docs/flows.md), with the Flow and the date as
	// DueAt; channels can subscribe to it.
	EventFlowOverdue = "asset.flow_overdue"
	// EventAssetLabelPrinted records labels of an asset sent to a printer or
	// downloaded from the spool, with its Label; it is for history.
	EventAssetLabelPrinted = "asset.label_printed"
	// EventAssetHook records a hook triggered on an asset, with its
	// Hook; channels and live connections can subscribe to it.
	EventAssetHook = "asset.hook"
	// EventCategoryIconChanged records a new published version of a
	// category's icon (docs/icons.md), so that what shows it can redraw; it
	// is for history.
	EventCategoryIconChanged = "category.icon_changed"
	// EventCategoryEdited records changes to a category listed in the
	// payload's Changes: for now, attachments added and removed
	// (docs/data-model.md#attachments). It is for history.
	EventCategoryEdited = "category.edited"
	// EventVendorCreated and EventVendorEdited record a vendor made, and
	// changes to one listed in the payload's Changes (docs/vendors.md).
	// They are for its history.
	EventVendorCreated = "vendor.created"
	EventVendorEdited  = "vendor.edited"
	// EventCommentAdded records a comment written on an asset, a category,
	// a loan or a flow's filing (docs/comments.md), with the Comment and
	// its text; channels can subscribe to it. EventCommentEdited and
	// EventCommentDeleted keep the text it had before, in Changes, for
	// history.
	EventCommentAdded   = "comment.added"
	EventCommentEdited  = "comment.edited"
	EventCommentDeleted = "comment.deleted"
	// EventAuthThrottled records sign-ins held back for too many failed
	// passwords (docs/access.md), with Throttle; it is about no asset or
	// category, so it reaches notification channels only.
	EventAuthThrottled = "auth.throttled"
	// EventNotifierDisabled records a notification channel turned off for
	// failing (docs/notifications.md), with the Channel and why as the
	// Note; other channels can subscribe to it.
	EventNotifierDisabled = "notifier.disabled"
	// EventAuthFactorsReset records that someone's second factors were
	// removed for them, by an admin or on the command line, with the
	// Account; channels can subscribe to it.
	EventAuthFactorsReset = "auth.factors_reset"
	// EventAuthRecoveryCodeUsed records someone signing in with a recovery
	// code, with the Account; channels can subscribe to it.
	EventAuthRecoveryCodeUsed = "auth.recovery_code_used"
	// EventAuthBreakGlass records someone signing in with a password while
	// passwords are off, at the break-glass page, with the Account;
	// channels can subscribe to it.
	EventAuthBreakGlass = "auth.break_glass"
	// EventAuthRoleKept records a role an identity provider no longer
	// gives, kept because it holds the last users.manage, with the Account
	// and the role in the Note; it is for history.
	EventAuthRoleKept = "auth.role_kept"
	// EventRoleTwoFactorOff records a role no longer requiring two-factor
	// sign-in, with the role's name as the Note; channels can subscribe to
	// it.
	EventRoleTwoFactorOff = "role.two_factor_off"
	// EventRelayOnline and EventRelayOffline record a relay coming online,
	// linked to the main node over any transport, and going offline, with
	// the Relay (docs/relays.md); channels can subscribe to them.
	EventRelayOnline  = "relay.online"
	EventRelayOffline = "relay.offline"
	// EventInstanceUpgraded records BAM! starting at a release other than
	// the one it last ran, an upgrade or a downgrade, with the Instance;
	// EventInstanceUpdateFailed records bam-update putting the previous
	// release back after a new one failed to come up; and
	// EventInstanceUpdateAvailable a newer release found by the update
	// check (docs/installing.md). They are about no asset or category, and
	// channels can subscribe to them.
	EventInstanceUpgraded        = "instance.upgraded"
	EventInstanceUpdateFailed    = "instance.update_failed"
	EventInstanceUpdateAvailable = "instance.update_available"
	// EventWarrantyExpiring records once that an asset's warranty ends
	// soon (docs/data-model.md#purchases), with the end as DueAt, and again
	// for a new end. Channels can subscribe to it.
	EventWarrantyExpiring = "warranty.expiring"
)

// HistoryOnly reports whether events of this kind are kept only for an
// asset's history, with no channel able to subscribe to them.
func HistoryOnly(kind string) bool {
	switch kind {
	case EventQuantityChanged, EventAssetCreated, EventAssetEdited, EventAssetArchived, EventAssetRestored,
		EventAssetCheckedOut, EventAssetCheckedIn, EventAssetLabelPrinted, EventCategoryIconChanged, EventCategoryEdited, EventAuthRoleKept,
		EventVendorCreated, EventVendorEdited, EventCommentEdited, EventCommentDeleted:
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
	// TriggerEvent marks a hook fired by another event (docs/hooks.md).
	TriggerEvent = "event"
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
	// Category, Asset or Vendor is what the event is about.
	Category *EventCategory `json:"category,omitempty"`
	Asset    *EventAsset    `json:"asset,omitempty"`
	Vendor   *EventVendor   `json:"vendor,omitempty"`
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
	// Hook is the hook a hook event triggered, and what a device
	// needs to find the thing.
	Hook *EventHook `json:"hook,omitempty"`
	// Throttle is what an auth.throttled event held back.
	Throttle *EventThrottle `json:"throttle,omitempty"`
	// Channel is the notification channel a notifier event is about.
	Channel *EventChannel `json:"channel,omitempty"`
	// Account is whose sign-in an auth event is about, such as whose
	// second factors were reset.
	Account *EventActor `json:"account,omitempty"`
	// Relay is the relay a relay event is about.
	Relay *EventRelay `json:"relay,omitempty"`
	// Instance is the change of release an instance event is about.
	Instance *EventInstance `json:"instance,omitempty"`
	// Comment is the comment a comment event is about.
	Comment *EventComment `json:"comment,omitempty"`
}

// EventInstance is a change of the release BAM! runs, as an instance event
// describes it. From is the release running before, or still running, and
// To the one it went to, was tried, or is available. The schema versions
// are the database's, before and after the upgrade's migrations.
type EventInstance struct {
	From       string `json:"from,omitempty"`
	To         string `json:"to"`
	SchemaFrom int    `json:"schema_from,omitempty"`
	SchemaTo   int    `json:"schema_to,omitempty"`
	// Downgrade marks an upgraded event going to an older release.
	Downgrade bool `json:"downgrade,omitempty"`
	// Reason is why a failed update failed, and Restored whether the
	// database was put back from the backup taken before it.
	Reason   string `json:"reason,omitempty"`
	Restored bool   `json:"restored,omitempty"`
	// NotesURL is the page of To's release notes, and UpdatingURL the docs
	// on updating, where known.
	NotesURL    string `json:"notes_url,omitempty"`
	UpdatingURL string `json:"updating_url,omitempty"`
}

// EventComment is a comment, as an event names it: what it is on, one of
// the CommentOn… kinds, and its text then. A loan's and a filing's
// comments are events about their asset, naming the loan or the flow here.
type EventComment struct {
	ID     string     `json:"id"`
	On     string     `json:"on"`
	LoanID string     `json:"loan_id,omitempty"`
	Flow   *EventFlow `json:"flow,omitempty"`
	Text   string     `json:"text,omitempty"`
	// Mentions name the people the text mentions, as <@id>, so that it
	// can be told with their names.
	Mentions []EventMention `json:"mentions,omitempty"`
}

// EventMention is a person a comment mentions, by ID and their name then.
type EventMention struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EventRelay is a relay, as an event names it.
type EventRelay struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Throttle kinds: what a sign-in throttle counts failures against.
const (
	ThrottleAccount = "account"
	ThrottleAddress = "address"
)

// EventThrottle is what an auth.throttled event held back: an account,
// named by its user, or a client address; and until when.
type EventThrottle struct {
	Kind    string      `json:"kind"`
	User    *EventActor `json:"user,omitempty"`
	Address string      `json:"address,omitempty"`
	Until   time.Time   `json:"until"`
}

// EventChannel is a notification channel, as an event names it.
type EventChannel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
}

// EventLabel describes an asset's labels printed (docs/printing.md): the
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

// EventVendor is the vendor an event is about, as it was then.
type EventVendor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	// Incoming is set when an incoming hook acted (docs/hooks.md); ID and
	// Name are then the hook's.
	Incoming bool `json:"incoming,omitempty"`
	// Via names the assistant the change was made through, by the name its
	// MCP client gives, or "MCP" when it gives none (docs/mcp.md).
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

// Incoming hooks' ways to authenticate (docs/hooks.md#incoming-hooks).
const (
	IncomingBearer = "bearer"
	IncomingHMAC   = "hmac"
)

// What incoming hooks do.
const (
	IncomingTriggerHook = "hook"
	IncomingQuantity    = "quantity"
	IncomingFields      = "fields"
	IncomingFlow        = "flow"
)

// How incoming hooks find the asset they act on.
const (
	IncomingAssetFixed = "fixed"
	IncomingAssetID    = "id"
	IncomingAssetCode  = "code"
	IncomingAssetField = "field"
)

// IncomingHook is a URL that lets something outside BAM! act on it
// (docs/hooks.md#incoming-hooks). Its secret is shown once, when it is
// made or replaced, and cannot be read back.
type IncomingHook struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Auth is IncomingBearer or IncomingHMAC.
	Auth string `json:"auth"`
	// HasSecret is false for a hook restored without its secret, which
	// refuses every request until given a new one.
	HasSecret bool `json:"has_secret"`
	// Action is what it does, one of the Incoming… actions.
	Action string `json:"action"`
	IncomingSettings
	// RateLimit is how many requests it takes a minute.
	RateLimit int `json:"rate_limit"`
	// Roles are what it acts as, each in every scope or narrowed to some.
	Roles      []RoleAssignment `json:"roles"`
	CreatedBy  string           `json:"created_by,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	LastUsedAt *time.Time       `json:"last_used_at"`
}

// IncomingSettings are what an incoming hook's action takes, kept as JSON.
type IncomingSettings struct {
	// Hook is the key of the hook to trigger, for IncomingTriggerHook.
	Hook string `json:"hook,omitempty"`
	// FlowID is the flow to run, for IncomingFlow.
	FlowID string `json:"flow_id,omitempty"`
	// Fields maps fields' keys to where in the request their values are,
	// for IncomingFields; empty, the request's field_values are set.
	Fields map[string]string `json:"fields,omitempty"`
	// Asset is how the asset is found.
	Asset IncomingAsset `json:"asset"`
}

// IncomingAsset is how an incoming hook finds the asset it acts on: fixed,
// or from the request, by an ID, a code or a field's value at a path in its
// JSON. By is empty only for a flow that finds nothing first.
type IncomingAsset struct {
	By      string `json:"by"`
	AssetID string `json:"asset_id,omitempty"`
	Path    string `json:"path,omitempty"`
	Field   string `json:"field,omitempty"`
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

// OAuthClient is an assistant registered to sign people in (docs/mcp.md).
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
	// Stock is the asset's stock when it is counted by quantity
	// (docs/stock-and-lending.md), and nil when it is not.
	Stock *AssetStock `json:"stock"`
	// Condition is ConditionOK, ConditionNeedsMaintenance or
	// ConditionOutOfService, with a note saying what is wrong.
	Condition     string `json:"condition"`
	ConditionNote string `json:"condition_note"`
	// IconName is the asset's own icon (docs/icons.md), drawn in place of its
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
	// Photos are its image attachments, in order: the first is the one
	// lists show.
	Photos []AssetPhoto `json:"photos"`
	// Scopes are the scopes it is in: those of its categories and their
	// ancestors, or the default scope (docs/access.md).
	Scopes []string `json:"scopes"`
	// ScanTemplate is the scan template it shows when scanned, and why.
	ScanTemplate TemplateChoice `json:"scan_template"`
	// LabelTemplate is the label template the spool queues it with, and why;
	// TemplateID is empty when no category names one.
	LabelTemplate TemplateChoice `json:"label_template"`
	// Hooks are those its categories offer on it (docs/hooks.md).
	Hooks []OfferedHook `json:"hooks"`
	// ResolvedIcon is the icon it is drawn with on screen, and why; nil
	// when neither it nor any of its categories has one.
	ResolvedIcon *ResolvedIcon `json:"resolved_icon"`
	// ResolvedIcons is the icon it is drawn with for each rendition it has
	// one for, ResolvedIcon among them.
	ResolvedIcons map[string]*ResolvedIcon `json:"resolved_icons"`
	// Purchase is where it came from: empty when nothing is recorded.
	Purchase Purchase `json:"purchase"`
}

// Purchase is where an asset came from (docs/data-model.md#purchases). Every
// detail is optional. Dates are YYYY-MM-DD, empty when not known.
type Purchase struct {
	// VendorID is the vendor it was bought from, and VendorName its name;
	// both empty when none is recorded, or the requester may not see it.
	VendorID     string `json:"vendor_id"`
	VendorName   string `json:"vendor_name"`
	OrderDate    string `json:"order_date"`
	ReceivedDate string `json:"received_date"`
	WarrantyEnd  string `json:"warranty_end"`
	// Price is a decimal with as many places as its Currency has, such as
	// 12.50, kept exactly as minor units (internal/money); Currency is its
	// ISO 4217 code. OrderNumber is the order or invoice number, and
	// InvoiceID the invoice: one of the asset's attachments, whose name is
	// InvoiceName. They are left out for a requester without purchases.read
	// for the asset, and PricesHidden says so.
	Price        string `json:"price,omitempty"`
	Currency     string `json:"currency,omitempty"`
	OrderNumber  string `json:"order_number,omitempty"`
	InvoiceID    string `json:"invoice_attachment_id,omitempty"`
	InvoiceName  string `json:"invoice_name,omitempty"`
	PricesHidden bool   `json:"prices_hidden,omitempty"`
}

// IsZero reports whether nothing is recorded.
func (p Purchase) IsZero() bool {
	return p.VendorID == "" && p.OrderDate == "" && p.ReceivedDate == "" && p.WarrantyEnd == "" &&
		p.Price == "" && p.Currency == "" && p.OrderNumber == "" && p.InvoiceID == ""
}

// HidePrices leaves out what only purchases.read sees: the price, currency,
// order number and invoice.
func (p *Purchase) HidePrices() {
	p.Price, p.Currency, p.OrderNumber, p.InvoiceID, p.InvoiceName = "", "", "", "", ""
	p.PricesHidden = true
}

// HideVendor leaves out the vendor, for a requester who may not see it.
func (p *Purchase) HideVendor() {
	p.VendorID, p.VendorName = "", ""
}

// Renditions are the forms an icon takes for where it is drawn
// (docs/icons.md): RenditionScreen, every icon on screen, and RenditionLabel, on
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

// ResolvedIcon is the icon an asset is drawn with (docs/icons.md): its own,
// From IconFromAsset, or else that of the nearest category up from its
// categories with one, From IconFromCategory, chosen as its templates are
// (TemplateChoice), named SourceCategoryName. Missing is set when the current build draws no icon
// by Name, so the unknown icon is drawn in its place. Color is what the
// "category" token paints in: the color of its nearest category with one,
// chosen the same way, or empty for the palette's own.
//
// When params of its icon are bound to fields it has numbers in
// (docs/icons.md), Bound is the wrapper drawing the icon with them, Params, and
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
	// by param: its name, a code, a field's value (docs/icons.md).
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
// has (docs/icons.md): a field's value, Source "field", or for a text param,
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

// The states an asset's icon can show (docs/icons.md), worst first: an
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

// TemplateChoice is the scan template (docs/scanning.md) or label template
// (docs/printing.md) an asset uses: the one its nearest category naming one names,
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
// driver (docs/notifications.md). Options hold the driver's options, secrets included;
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
	// Failures counts failed attempts since the channel last delivered,
	// the first at FailingSince. A channel failing long enough is turned
	// off, saying why in DisabledReason (#196, docs/notifications.md).
	Failures       int        `json:"failures"`
	FailingSince   *time.Time `json:"failing_since,omitempty"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
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
	// ResponseStatus and ResponseBody are what the receiver last answered,
	// when its driver says: a webhook's HTTP status and the first kilobyte
	// of its body, with the channel's secrets taken out.
	ResponseStatus *int   `json:"response_status,omitempty"`
	ResponseBody   string `json:"response_body,omitempty"`
}

// An asset's condition (docs/data-model.md).
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

// Loan is an asset lent to someone (docs/stock-and-lending.md). It is open until
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

// RelationType is a kind of link between assets (docs/data-model.md), named from
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

// Report is a saved report (docs/reports.md): a name and its spec, JSON that
// the reports package reads.
type Report struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is report or scan (docs/scanning.md); Default marks the scan template
	// assets use when no category names one.
	Kind      string          `json:"kind"`
	Default   bool            `json:"default"`
	Spec      json.RawMessage `json:"spec"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// The owners attachments belong to (docs/data-model.md#attachments).
const (
	OwnerAsset    = "asset"
	OwnerCategory = "category"
	OwnerVendor   = "vendor"
)

// What comments are on (docs/comments.md). A filing is an asset a flow
// filed, named by the flow and the asset.
const (
	CommentOnAsset    = "asset"
	CommentOnCategory = "category"
	CommentOnLoan     = "loan"
	CommentOnFiling   = "filing"
)

// Comment is a note in Markdown on an asset, a category, a loan or a
// flow's filing. Body is its Markdown and HTML that, rendered and
// sanitized on the server; both are empty once it is deleted, as DeletedAt
// says, and its text stays in history.
type Comment struct {
	ID string `json:"id"`
	// On is one of the CommentOn… kinds, and OnID the ID of what it is
	// on: the asset, category or loan, or for a filing the asset, with
	// the flow as FlowID.
	On     string `json:"on"`
	OnID   string `json:"on_id"`
	FlowID string `json:"flow_id,omitempty"`
	// AssetID is the asset it is about: the asset itself, or the loan's
	// or the filing's; none for a category's.
	AssetID string `json:"asset_id,omitempty"`
	Body    string `json:"body"`
	HTML    string `json:"html"`
	// AuthorID is who wrote it, a user or an API token, and AuthorName
	// their name as it is now. The API shows them only to those who may
	// see who, as history does; Mine tells the author it is theirs.
	AuthorID   string     `json:"author_id,omitempty"`
	AuthorName string     `json:"author_name,omitempty"`
	Mine       bool       `json:"mine"`
	CreatedAt  time.Time  `json:"created_at"`
	EditedAt   *time.Time `json:"edited_at"`
	DeletedAt  *time.Time `json:"deleted_at"`
	// DeletedBy is who deleted it, and DeletedByName their name, shown as
	// the author is.
	DeletedBy     string `json:"deleted_by,omitempty"`
	DeletedByName string `json:"deleted_by_name,omitempty"`
	// Category is the category it is on, when it is listed with another
	// thing's comments: an asset's or a category's, from its categories
	// and their ancestors. Place names what it is on in search results.
	Category *CommentPlace `json:"category,omitempty"`
	Place    *CommentPlace `json:"place,omitempty"`
	// CanEdit and CanDelete say what the requester may do with it.
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	// Mentions are the people its text mentions, as <@id>, by their names
	// now, to those who may see who, so that an editor can show them.
	Mentions []CommentMention `json:"mentions,omitempty"`
}

// CommentMention is a person a comment mentions, or may mention.
type CommentMention struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CommentPlace names what a comment is on, as lists and search show it.
type CommentPlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Filing is an asset a flow filed (docs/flows.md), which comments can be
// on.
type Filing struct {
	FlowID   string    `json:"flow_id"`
	FlowSlug string    `json:"flow_slug"`
	FlowName string    `json:"flow_name"`
	AssetID  string    `json:"asset_id"`
	FiledAt  time.Time `json:"filed_at"`
	// Comments counts its comments that are not deleted.
	Comments int `json:"comments"`
}

// The kinds of attachment: an image is a photo, re-encoded and given a
// thumbnail; a document is a PDF, plain text, Markdown or CSV; other is a
// type the configuration allows.
const (
	AttachmentImage    = "image"
	AttachmentDocument = "document"
	AttachmentOther    = "other"
)

// Attachment is a file attached to an asset or a category; its bytes, and
// an image's thumbnail's, are stored as files.
type Attachment struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	// ThumbID is an image's thumbnail's file; WidthPx and HeightPx its
	// size. The others have none.
	ThumbID  string `json:"-"`
	WidthPx  int    `json:"width_px,omitempty"`
	HeightPx int    `json:"height_px,omitempty"`
	// AddedBy is the user who added it, if one did; AddedByName their
	// name, as it is now, for those who may see it.
	AddedBy     string    `json:"added_by,omitempty"`
	AddedByName string    `json:"added_by_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Photo is an image attachment as an asset's photos list it.
func (a Attachment) Photo() AssetPhoto {
	return AssetPhoto{ID: a.ID, ThumbID: a.ThumbID, Filename: a.Filename, ContentType: a.ContentType,
		WidthPx: a.WidthPx, HeightPx: a.HeightPx, ByteSize: a.ByteSize, CreatedAt: a.CreatedAt}
}

// AssetPhoto is an asset's image attachment, as its photos were listed
// before attachments (docs/data-model.md#attachments); its bytes, and its
// thumbnail's, are stored as files.
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
// (docs/access.md). Default is the reserved scope of assets in no other.
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
	// VendorCount counts the vendors it is on (docs/vendors.md).
	VendorCount int `json:"vendor_count"`
}

// Vendor is a supplier a space buys from, and how to reach it
// (docs/vendors.md). Its notes are Markdown.
type Vendor struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Website       string          `json:"website"`
	AccountNumber string          `json:"account_number"`
	Address       string          `json:"address"`
	Notes         string          `json:"notes"`
	Contacts      []VendorContact `json:"contacts"`
	// ScopeIDs are the scopes it is in, which decide who sees it; none, and
	// everyone who reads assets does.
	ScopeIDs  []string  `json:"scope_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VendorContact is someone at a vendor: their role is what they are for,
// such as sales or support.
type VendorContact struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

// Spool destinations (docs/printing.md): DestinationDownload, or
// DestinationPrinterPrefix or DestinationPoolPrefix followed by a printer's
// or a pool's ID. An entry with none waits for one to be chosen.
const (
	DestinationDownload      = "download"
	DestinationPrinterPrefix = "printer:"
	DestinationPoolPrefix    = "pool:"
)

// SpoolEntry is one asset's label in the spool (docs/printing.md): waiting to be
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

// Spool job and batch states (docs/printing.md).
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
// template, or a download of them (docs/printing.md). Its template and target
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
// (docs/printing.md). Each job is cut into batches of BatchSize labels. Members
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

// AssetHook is a hook people trigger on assets, such as Identify
// (docs/hooks.md). Triggering one records an asset.hook event, which devices
// and notification channels act on. Confirm, when set, is asked first.
type AssetHook struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Confirm string `json:"confirm"`
	// Triggers are what fire it: a button people press, events, or a
	// schedule. A hook saved without any is a button.
	Triggers  []HookTrigger `json:"triggers"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Hook triggers (docs/hooks.md).
const (
	HookTriggerButton   = "button"
	HookTriggerEvent    = "event"
	HookTriggerSchedule = "schedule"
)

// HookTrigger is one way a hook fires. Kinds, Condition and
// IntervalSeconds are an event trigger's; Cron, TimeZone and Filter a
// schedule's.
type HookTrigger struct {
	Type string `json:"type"`
	// Kinds are the kinds of event about an asset that fire it.
	Kinds []string `json:"kinds,omitempty"`
	// Condition, when set, must hold of the asset.
	Condition *HookCondition `json:"condition,omitempty"`
	// IntervalSeconds is the least time between two firings for one
	// asset, so a burst of events fires it once.
	IntervalSeconds int `json:"interval_seconds,omitempty"`
	// Cron is five fields: minute, hour, day of the month, month, day of
	// the week; TimeZone the IANA zone it is read in, UTC when empty.
	Cron     string `json:"cron,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
	// Filter chooses the assets a schedule fires for, as a report's filter
	// does (docs/reports.md).
	Filter json.RawMessage `json:"filter,omitempty"`
}

// HookCondition holds when the asset's Field (a field's key, or
// "condition") is Equals.
type HookCondition struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
}

// Button reports whether people can press the hook.
func (h AssetHook) Button() bool {
	for _, t := range h.Triggers {
		if t.Type == HookTriggerButton {
			return true
		}
	}
	return false
}

// CategoryHook is a hook a category offers on its assets, and with
// Cascades on those of its subcategories.
type CategoryHook struct {
	HookID   string `json:"hook_id"`
	Cascades bool   `json:"cascades"`
}

// OfferedHook is a hook an asset's categories offer on it.
type OfferedHook struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
}

// EventHook is the hook an asset.hook event triggered: its key and
// label, and the asset and the containers it is in, outermost first, each
// with its codes and field values, for a device to find it by.
type EventHook struct {
	Key      string       `json:"key"`
	Label    string       `json:"label"`
	Asset    EventThing   `json:"asset"`
	Location []EventThing `json:"location"`
	// Trigger is what fired it: HookTriggerButton, HookTriggerEvent or
	// HookTriggerSchedule. Cause is the event that fired an event trigger.
	Trigger string      `json:"trigger,omitempty"`
	Cause   *EventCause `json:"cause,omitempty"`
}

// EventCause is the event that fired a hook.
type EventCause struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// EventThing is an asset as a hook event carries it: its codes, by
// generator type, and its field values, by field key.
type EventThing struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Codes  map[string]string `json:"codes"`
	Fields map[string]string `json:"fields"`
}

// Relay is a relay an admin added (docs/relays.md): waiting to pair with
// its code, paired, or revoked, with its health.
type Relay struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Paired says it holds a certificate and credential; CodeExpiresAt,
	// while it has a code to pair with, when that lapses.
	Paired        bool       `json:"paired"`
	CodeExpiresAt *time.Time `json:"code_expires_at,omitempty"`
	PairedAt      *time.Time `json:"paired_at,omitempty"`
	// CertExpiresAt is when its BoneMesh certificate expires, unless it
	// renews it first.
	CertExpiresAt *time.Time `json:"cert_expires_at,omitempty"`
	// RevokedAt is when it was revoked; its name stays taken until its
	// certificate expires.
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	// Online says whether it is linked to the main node now, and StatusAt
	// since when it is, or is not.
	Online   bool       `json:"online"`
	StatusAt *time.Time `json:"status_at,omitempty"`
	// Version is its BAM! version, and Address where it last paired or
	// renewed from.
	Version string `json:"version"`
	Address string `json:"address"`
	// Allowlist is what it says it may reach locally.
	Allowlist []string  `json:"allowlist"`
	CreatedAt time.Time `json:"created_at"`
}

// RelayPrinter is a printer reached through a relay, with the last print
// job sent to it, if any.
type RelayPrinter struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	LastJob *RelayPrinterJob `json:"last_job,omitempty"`
}

// RelayPrinterJob is how a printer's last print job went: its state, why
// it failed if it did, and when it finished, or was made if it has not.
type RelayPrinterJob struct {
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

// LiveConnection is a live event connection a server holds open
// (docs/hooks.md): for an API token, or for a signed-in user.
type LiveConnection struct {
	ID       string    `json:"id"`
	TokenID  *string   `json:"token_id,omitempty"`
	UserID   *string   `json:"user_id,omitempty"`
	Remote   string    `json:"remote"`
	OpenedAt time.Time `json:"opened_at"`
	SeenAt   time.Time `json:"seen_at"`
}

// HookReply is a device's answer to a hook triggered: done or failed,
// with a message, and who answered as they were named then.
type HookReply struct {
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
// and column of a file of the build (docs/icons.md).
type IconDiag struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// CategoryIcon is a category's isolex source for a rendition (docs/icons.md). Source is
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

// Flow is a guided page for doing one everyday thing with assets
// (docs/flows.md): its address, /f/<slug>, its name, and what it does.
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
	// Photos are the photos the run's Set fields steps put on assets, for
	// the page that took each to add it (docs/flows.md).
	Photos []FlowPhoto `json:"photos,omitempty"`
	// Fields and Progress are kept for the run's own use (store.FlowRunState).
	Fields   map[string]string `json:"-"`
	Progress map[string]int    `json:"-"`
	// Answered is kept for the run's own use too: how many steps that ask
	// something the person has answered (#166).
	Answered int `json:"-"`
}

// FlowPhoto is a photo question's answer that a Set fields step put on an
// asset: the page holding the photo adds it to AssetID, which is empty
// until the Create it was set before has filed the asset.
type FlowPhoto struct {
	Question string `json:"question"`
	AssetID  string `json:"asset_id,omitempty"`
}

// FlowDone is what one step of a run did, in words, for Done to show.
type FlowDone struct {
	Step string `json:"step"`
	Kind string `json:"kind"`
	Said string `json:"said"`
}

// Upload is an archive being uploaded through the web in parts, or one
// uploaded, staged until it is used (docs/backups.md, #141).
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
