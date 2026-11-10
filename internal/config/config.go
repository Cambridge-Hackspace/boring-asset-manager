package config

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/release"
	"github.com/pelletier/go-toml/v2"
)

// Config represents the complete application configuration.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Storage  StorageConfig  `toml:"storage"`
	Printers PrintersConfig `toml:"printers"`
	MCP      MCPConfig      `toml:"mcp"`
	// Notifications holds how deliveries are kept and when failing
	// channels are turned off (docs/notifications.md).
	Notifications NotificationsConfig `toml:"notifications"`
	Email         EmailConfig         `toml:"email"`
	Auth          AuthConfig          `toml:"auth"`
	// Relay turns the relay bus's transports on or off (docs/relays.md).
	Relay RelayConfig `toml:"relay"`
	// Attachments bounds the files attached to assets and categories
	// (docs/data-model.md#attachments).
	Attachments AttachmentsConfig `toml:"attachments"`
	// Updates turns the check for a newer release on (docs/installing.md).
	Updates UpdatesConfig `toml:"updates"`
	// Purchases says when warranties are announced
	// (docs/data-model.md#purchases).
	Purchases PurchasesConfig `toml:"purchases"`
	// Contracts says when contracts ending are announced
	// (docs/contracts.md).
	Contracts ContractsConfig `toml:"contracts"`
	// Assistant sets the assistant beside the page, in place of Admin
	// (docs/assistant.md).
	Assistant AssistantConfig `toml:"assistant"`
}

// UpdatesConfig is whether BAM! asks, once a day, whether a newer release
// is out, announcing one as instance.update_available, and where it asks.
// The check is off unless turned on, as it reaches out to the network.
type UpdatesConfig struct {
	Check bool `toml:"check"`
	// URL answers with the latest release, as GitHub's releases API does,
	// or with a list of releases; release.LatestURL unless given.
	URL string `toml:"url"`
}

// PurchasesConfig says how many days before an asset's warranty ends its
// warranty.expiring event is recorded: 0 on the day it ends.
type PurchasesConfig struct {
	WarrantyNoticeDays int `toml:"warranty_notice_days"`
}

// MaxWarrantyNoticeDays is the most purchases.warranty_notice_days may be.
const MaxWarrantyNoticeDays = 365

// ContractsConfig says how many days before a contract ends, or before the
// deadline for giving notice on one that renews automatically, its
// contract.expiring event is recorded: 0 on the day.
type ContractsConfig struct {
	ReminderDays int `toml:"reminder_days"`
}

// MaxContractReminderDays is the most contracts.reminder_days may be.
const MaxContractReminderDays = 365

// AttachmentsConfig bounds attachments, and says which types beyond images
// and documents may be attached.
type AttachmentsConfig struct {
	// MaxFileMB is the largest file, in megabytes (MiB). Images are held to
	// photos' own limit too, as they are decoded.
	MaxFileMB int `toml:"max_file_mb"`
	// MaxPerObject is how many attachments one asset or category may have.
	MaxPerObject int `toml:"max_per_object"`
	// AllowedTypes are the other types that may be attached: media types,
	// such as "application/zip", which a file's content must show it is,
	// or extensions, such as ".stl", for a file whose content shows no
	// type of its own. Empty, the default, allows none.
	AllowedTypes []string `toml:"allowed_types"`
}

// MaxAttachmentMB is the most attachments.max_file_mb may be.
const MaxAttachmentMB = 1024

// MaxFileBytes is the largest attachment, in bytes.
func (a AttachmentsConfig) MaxFileBytes() int64 { return int64(a.MaxFileMB) << 20 }

// ServerConfig holds HTTP server listening configuration.
type ServerConfig struct {
	Host    string `toml:"host"`
	Port    int    `toml:"port"`
	BaseURL string `toml:"base_url"`
	// TrustedProxies are the addresses and CIDR ranges of reverse proxies
	// whose X-Forwarded-For and Forwarded headers say who the client is
	// (#246). Empty, the default, trusts no header.
	TrustedProxies []string `toml:"trusted_proxies"`
}

// DatabaseConfig holds database connection configuration.
type DatabaseConfig struct {
	Type string `toml:"type"` // "sqlite", "postgres", "mariadb"
	DSN  string `toml:"dsn"`
}

// StorageConfig says where uploaded files are kept (docs/configuration.md): in a
// directory, LocalPath, or with Backend "s3", in a bucket of an
// S3-compatible store.
type StorageConfig struct {
	// Backend is "local" (the default, also when empty) or "s3".
	Backend   string `toml:"backend"`
	LocalPath string `toml:"local_path"`
	// StagingPath is where archives uploaded through the web are kept while
	// they arrive and until they are used, with the local backend (#141);
	// empty is a directory named staging beside LocalPath. With s3 they are
	// staged in the bucket.
	StagingPath string   `toml:"staging_path"`
	S3          S3Config `toml:"s3"`
}

// StagingDir is where uploads are staged with the local backend.
func (s StorageConfig) StagingDir() string {
	if s.StagingPath != "" {
		return s.StagingPath
	}
	return filepath.Join(filepath.Dir(filepath.Clean(s.LocalPath)), "staging")
}

// S3Config reaches a bucket of an S3-compatible store: AWS, MinIO, Garage,
// Ceph, Backblaze B2, Cloudflare R2. Its keys are never logged.
type S3Config struct {
	// Endpoint is the store's address, such as
	// https://s3.eu-west-2.amazonaws.com or http://minio:9000.
	Endpoint string `toml:"endpoint"`
	// Region signs requests; empty is us-east-1, as most stores but AWS
	// take.
	Region string `toml:"region"`
	Bucket string `toml:"bucket"`
	// Prefix starts the name of every object bam keeps, so one bucket can
	// hold more than bam's; a / is added if it has none.
	Prefix string `toml:"prefix"`
	// PathStyle addresses the bucket in the path, as MinIO, Garage and
	// most stores but AWS expect.
	PathStyle       bool   `toml:"path_style"`
	AccessKeyID     string `toml:"access_key_id"`
	SecretAccessKey string `toml:"secret_access_key"`
}

// The storage backends.
const (
	StorageLocal = "local"
	StorageS3    = "s3"
)

// PrintersConfig holds what printer connections may reach on this server.
type PrintersConfig struct {
	// Devices lists the device path patterns (such as "/dev/usb/lp*") that
	// printers may be connected to directly. Empty means none: anyone who can
	// manage printers could otherwise make BAM write to any file it can open.
	Devices []string `toml:"devices"`
	// Bluetooth lists the Bluetooth addresses, or patterns of them such as
	// "7C:2F:80:*", of the printers ble connections may reach. Empty means
	// none: Bluetooth LE printers are reached only where they are allowed.
	Bluetooth []string `toml:"bluetooth"`
}

// RelayConfig turns the relay bus's transports on or off
// (docs/relays.md): either, both, or neither, which turns relays off on
// this node.
type RelayConfig struct {
	// BoneMesh links relays over BoneMesh, on a port of its own,
	// BoneMeshPort.
	BoneMesh     bool `toml:"bonemesh"`
	BoneMeshPort int  `toml:"bonemesh_port"`
	// WebSocket takes relays' connections at /api/v1/relay/live, on the
	// server's own port.
	WebSocket bool `toml:"websocket"`
	// Allow, on a relay, is what it may reach locally (docs/relays.md):
	// TCP printers as host:port, ranges or name patterns, IPP queues' URLs,
	// and windows: printers' names. Device paths are [printers] devices,
	// and Bluetooth printers [printers] bluetooth. Empty reaches nothing.
	Allow []string `toml:"allow"`
}

// DefaultBoneMeshPort is the port the main node takes BoneMesh links on,
// unless set.
const DefaultBoneMeshPort = 7443

// NotificationsConfig is how long the delivery log is kept, and when a
// channel that keeps failing is turned off (#196). Zero turns each off.
type NotificationsConfig struct {
	// KeepDays is how many days finished deliveries stay in the log.
	KeepDays int `toml:"keep_days"`
	// DisableAfterHours turns a channel off once every attempt has failed
	// for this many hours.
	DisableAfterHours int `toml:"disable_after_hours"`
	// DisableAfterFailures, when set, turns a channel off after this many
	// failed attempts in a row, however soon.
	DisableAfterFailures int `toml:"disable_after_failures"`
}

// MCPConfig turns the MCP endpoint, for assistants, on or off (docs/mcp.md).
type MCPConfig struct {
	// Enabled serves /api/v1/mcp. It is on unless turned off, so a config
	// file written before it existed, which has no [mcp] table, keeps it on.
	Enabled bool `toml:"enabled"`
}

// AuthConfig holds what signing in needs beyond the database
// (docs/access.md).
type AuthConfig struct {
	// SecretKey encrypts second factors' secrets in the database, so that a
	// copy of the database alone cannot make their codes: 32 random bytes,
	// as base64, which bam init writes. Without it, two-factor sign-in
	// cannot be set up.
	SecretKey string `toml:"secret_key"`
	// Providers are identity providers set here rather than under Admin,
	// for instances provisioned by a script (docs/access.md): shown in
	// Admin, read-only. BAM_AUTH_PROVIDERS gives them as a JSON list.
	Providers []ProviderConfig `toml:"providers,omitempty"`
	// TestProtocol offers the "test" sign-in protocol, which signs in as
	// whoever its provider's settings name. It is for tests alone: never
	// turn it on for an instance people use.
	TestProtocol bool `toml:"test_protocol,omitempty"`
}

// ProviderConfig is an identity provider set in the configuration. Its
// settings and secrets are the protocol's; its rules give roles by name.
type ProviderConfig struct {
	Key            string            `toml:"key" json:"key"`
	Name           string            `toml:"name" json:"name"`
	Protocol       string            `toml:"protocol" json:"protocol"`
	Settings       map[string]string `toml:"settings" json:"settings"`
	Secrets        map[string]string `toml:"secrets" json:"secrets"`
	ButtonLabel    string            `toml:"button_label" json:"button_label"`
	Icon           string            `toml:"icon" json:"icon"`
	Disabled       bool              `toml:"disabled" json:"disabled"`
	Provision      bool              `toml:"provision" json:"provision"`
	DefaultRoles   []string          `toml:"default_roles" json:"default_roles"`
	AllowedDomains []string          `toml:"allowed_domains" json:"allowed_domains"`
	AllowedGroups  []string          `toml:"allowed_groups" json:"allowed_groups"`
	LinkByEmail    bool              `toml:"link_by_email" json:"link_by_email"`
	TrustEmail     bool              `toml:"trust_email" json:"trust_email"`
	// MFAAMR, left out, is the default amr values that count as two-factor
	// (docs/access.md#requiring-two-factor-sign-in).
	MFAAMR         []string       `toml:"mfa_amr,omitempty" json:"mfa_amr,omitempty"`
	MFAACR         []string       `toml:"mfa_acr,omitempty" json:"mfa_acr,omitempty"`
	MFAFallback    bool           `toml:"mfa_fallback,omitempty" json:"mfa_fallback,omitempty"`
	MFAAlwaysLocal bool           `toml:"mfa_always_local,omitempty" json:"mfa_always_local,omitempty"`
	Rules          []ProviderRule `toml:"rules" json:"rules"`
}

// ProviderRule gives a role, by name, optionally narrowed to scopes, by
// name, to whoever's claim holds the value.
type ProviderRule struct {
	Claim  string   `toml:"claim" json:"claim"`
	Value  string   `toml:"value" json:"value"`
	Role   string   `toml:"role" json:"role"`
	Scopes []string `toml:"scopes" json:"scopes"`
}

// Key is the secret key's bytes, or nil when there is none.
func (a AuthConfig) Key() ([]byte, error) {
	if a.SecretKey == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(a.SecretKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("auth.secret_key must be 32 bytes as base64, such as bam init writes")
	}
	return key, nil
}

// EmailConfig is the SMTP server BAM! sends email through
// (docs/notifications.md). Email is off while Host is empty.
type EmailConfig struct {
	Host string `toml:"host"`
	// Port is 587 for STARTTLS, 465 for implicit TLS, or 25; when 0, the
	// usual port for TLS.
	Port int `toml:"port"`
	// TLS is EmailSTARTTLS (the default, also when empty), EmailImplicitTLS
	// or EmailNoTLS, which sends in the clear and is chosen explicitly.
	TLS      string `toml:"tls"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	// From is the sender, as "BAM! <bam@example.org>" or an address alone.
	From    string `toml:"from"`
	ReplyTo string `toml:"reply_to"`
	// CA is a PEM certificate to trust for a server whose certificate no
	// public authority signed; empty uses the system's authorities.
	CA string `toml:"ca"`
}

// Email TLS modes.
const (
	EmailSTARTTLS    = "starttls"
	EmailImplicitTLS = "implicit"
	EmailNoTLS       = "none"
)

// Enabled reports whether email is set up.
func (e EmailConfig) Enabled() bool { return e.Host != "" }

// Address is the server's host and port, the port defaulting by TLS mode.
func (e EmailConfig) Address() string {
	port := e.Port
	if port == 0 {
		port = 587
		if e.TLS == EmailImplicitTLS {
			port = 465
		}
	}
	return net.JoinHostPort(e.Host, strconv.Itoa(port))
}

func (e *EmailConfig) validate() error {
	if !e.Enabled() {
		return nil
	}
	switch e.TLS {
	case "":
		e.TLS = EmailSTARTTLS
	case EmailSTARTTLS, EmailImplicitTLS, EmailNoTLS:
	default:
		return fmt.Errorf("email.tls must be %q, %q or %q, not %q", EmailSTARTTLS, EmailImplicitTLS, EmailNoTLS, e.TLS)
	}
	if e.Port < 0 || e.Port > 65535 {
		return fmt.Errorf("invalid email port: %d", e.Port)
	}
	if _, err := mail.ParseAddress(e.From); err != nil {
		return fmt.Errorf("email.from %q is not an email address: %v", e.From, err)
	}
	if e.ReplyTo != "" {
		if _, err := mail.ParseAddress(e.ReplyTo); err != nil {
			return fmt.Errorf("email.reply_to %q is not an email address: %v", e.ReplyTo, err)
		}
	}
	if e.CA != "" {
		if block, _ := pem.Decode([]byte(e.CA)); block == nil {
			return errors.New("email.ca is not a PEM certificate")
		}
	}
	return nil
}

// DefaultConfig returns safe default settings.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:    "0.0.0.0",
			Port:    8080,
			BaseURL: "http://localhost:8080",
		},
		Database: DatabaseConfig{
			Type: "sqlite",
			DSN:  "data/bam.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		},
		Storage: StorageConfig{
			LocalPath: "./data/uploads",
		},
		MCP:           MCPConfig{Enabled: true},
		Notifications: NotificationsConfig{KeepDays: 30, DisableAfterHours: 24},
		Relay:         RelayConfig{BoneMeshPort: DefaultBoneMeshPort},
		Attachments:   AttachmentsConfig{MaxFileMB: 25, MaxPerObject: 50},
		Updates:       UpdatesConfig{URL: release.LatestURL},
		Purchases:     PurchasesConfig{WarrantyNoticeDays: 30},
		Contracts:     ContractsConfig{ReminderDays: 30},
	}
}

// Load loads configuration from a TOML file path (if provided) and applies
// environment variable overrides.
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
			}
		} else {
			if err := toml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing TOML config file %s: %w", configPath, err)
			}
		}
	}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) error {
	if host := os.Getenv("BAM_SERVER_HOST"); host != "" {
		cfg.Server.Host = host
	}
	if portStr := os.Getenv("BAM_SERVER_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.Server.Port = p
		}
	}
	if baseURL := os.Getenv("BAM_SERVER_BASE_URL"); baseURL != "" {
		cfg.Server.BaseURL = baseURL
	}
	if proxies := os.Getenv("BAM_SERVER_TRUSTED_PROXIES"); proxies != "" {
		cfg.Server.TrustedProxies = strings.Split(proxies, ",")
	}

	if dbType := os.Getenv("BAM_DATABASE_TYPE"); dbType != "" {
		cfg.Database.Type = dbType
	}
	if dsn := os.Getenv("BAM_DATABASE_DSN"); dsn != "" {
		cfg.Database.DSN = dsn
	}
	// Convenience env for test runners such as reaper ($REAPER_STATE)
	if dbPath := os.Getenv("BAM_DB_PATH"); dbPath != "" {
		cfg.Database.Type = "sqlite"
		cfg.Database.DSN = dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	if storagePath := os.Getenv("BAM_STORAGE_LOCAL_PATH"); storagePath != "" {
		cfg.Storage.LocalPath = storagePath
	}
	if storagePath := os.Getenv("BAM_STORAGE_PATH"); storagePath != "" {
		cfg.Storage.LocalPath = storagePath
	}
	if staging := os.Getenv("BAM_STORAGE_STAGING_PATH"); staging != "" {
		cfg.Storage.StagingPath = staging
	}
	if backend := os.Getenv("BAM_STORAGE_BACKEND"); backend != "" {
		cfg.Storage.Backend = backend
	}
	for env, field := range map[string]*string{
		"BAM_STORAGE_S3_ENDPOINT":          &cfg.Storage.S3.Endpoint,
		"BAM_STORAGE_S3_REGION":            &cfg.Storage.S3.Region,
		"BAM_STORAGE_S3_BUCKET":            &cfg.Storage.S3.Bucket,
		"BAM_STORAGE_S3_PREFIX":            &cfg.Storage.S3.Prefix,
		"BAM_STORAGE_S3_ACCESS_KEY_ID":     &cfg.Storage.S3.AccessKeyID,
		"BAM_STORAGE_S3_SECRET_ACCESS_KEY": &cfg.Storage.S3.SecretAccessKey,
	} {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
	if v := os.Getenv("BAM_STORAGE_S3_PATH_STYLE"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_STORAGE_S3_PATH_STYLE: %w", err)
		}
		cfg.Storage.S3.PathStyle = on
	}

	if devices := os.Getenv("BAM_PRINTERS_DEVICES"); devices != "" {
		cfg.Printers.Devices = strings.Split(devices, ",")
	}
	if bluetooth := os.Getenv("BAM_PRINTERS_BLUETOOTH"); bluetooth != "" {
		cfg.Printers.Bluetooth = strings.Split(bluetooth, ",")
	}

	for env, field := range map[string]*string{
		"BAM_EMAIL_HOST":      &cfg.Email.Host,
		"BAM_EMAIL_TLS":       &cfg.Email.TLS,
		"BAM_EMAIL_USERNAME":  &cfg.Email.Username,
		"BAM_EMAIL_PASSWORD":  &cfg.Email.Password,
		"BAM_EMAIL_FROM":      &cfg.Email.From,
		"BAM_EMAIL_REPLY_TO":  &cfg.Email.ReplyTo,
		"BAM_EMAIL_CA":        &cfg.Email.CA,
		"BAM_AUTH_SECRET_KEY": &cfg.Auth.SecretKey,
	} {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
	if v := os.Getenv("BAM_EMAIL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("BAM_EMAIL_PORT: %q is not a port", v)
		}
		cfg.Email.Port = p
	}

	// Refused rather than ignored when not understood: a mistyped "off"
	// must not leave the endpoint on.
	if v := os.Getenv("BAM_MCP_ENABLED"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_MCP_ENABLED: %w", err)
		}
		cfg.MCP.Enabled = on
	}
	for env, field := range map[string]*bool{
		"BAM_RELAY_BONEMESH":  &cfg.Relay.BoneMesh,
		"BAM_RELAY_WEBSOCKET": &cfg.Relay.WebSocket,
	} {
		if v := os.Getenv(env); v != "" {
			on, err := parseSwitch(v)
			if err != nil {
				return fmt.Errorf("%s: %w", env, err)
			}
			*field = on
		}
	}
	if v := os.Getenv("BAM_RELAY_BONEMESH_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("BAM_RELAY_BONEMESH_PORT: %q is not a port", v)
		}
		cfg.Relay.BoneMeshPort = p
	}
	if v := os.Getenv("BAM_RELAY_ALLOW"); v != "" {
		cfg.Relay.Allow = strings.Split(v, ",")
	}
	for env, field := range map[string]*int{
		"BAM_ATTACHMENTS_MAX_FILE_MB":        &cfg.Attachments.MaxFileMB,
		"BAM_ATTACHMENTS_MAX_PER_OBJECT":     &cfg.Attachments.MaxPerObject,
		"BAM_PURCHASES_WARRANTY_NOTICE_DAYS": &cfg.Purchases.WarrantyNoticeDays,
		"BAM_CONTRACTS_REMINDER_DAYS":        &cfg.Contracts.ReminderDays,
	} {
		if v := os.Getenv(env); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", env, v)
			}
			*field = n
		}
	}
	if v := os.Getenv("BAM_ATTACHMENTS_ALLOWED_TYPES"); v != "" {
		cfg.Attachments.AllowedTypes = strings.Split(v, ",")
	}
	if v := os.Getenv("BAM_UPDATES_CHECK"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_UPDATES_CHECK: %w", err)
		}
		cfg.Updates.Check = on
	}
	if v := os.Getenv("BAM_UPDATES_URL"); v != "" {
		cfg.Updates.URL = v
	}
	if v := os.Getenv("BAM_AUTH_TEST_PROTOCOL"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_AUTH_TEST_PROTOCOL: %w", err)
		}
		cfg.Auth.TestProtocol = on
	}
	if err := assistantEnv(&cfg.Assistant); err != nil {
		return err
	}
	// The providers replace those in the file, whole.
	if v := os.Getenv("BAM_AUTH_PROVIDERS"); v != "" {
		var providers []ProviderConfig
		if err := json.Unmarshal([]byte(v), &providers); err != nil {
			return fmt.Errorf("BAM_AUTH_PROVIDERS: not a JSON list of providers: %w", err)
		}
		cfg.Auth.Providers = providers
	}
	return nil
}

// parseSwitch reads an on or off setting from the environment.
func parseSwitch(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%q is neither true nor false", v)
}

// Validate checks that required fields are present and valid.
func (c *Config) Validate() error {
	if n := c.Notifications; n.KeepDays < 0 || n.DisableAfterHours < 0 || n.DisableAfterFailures < 0 {
		return errors.New("notifications.keep_days, disable_after_hours and disable_after_failures cannot be negative")
	}
	if c.Updates.URL == "" {
		c.Updates.URL = release.LatestURL
	}
	if u, err := url.Parse(c.Updates.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("updates.url %q is not an http:// or https:// address", c.Updates.URL)
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.Database.Type != "sqlite" && c.Database.Type != "postgres" && c.Database.Type != "mariadb" {
		return fmt.Errorf("unsupported database type %q (must be sqlite, postgres, or mariadb)", c.Database.Type)
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database dsn cannot be empty")
	}
	if err := c.Storage.validate(); err != nil {
		return err
	}
	if _, err := ParseProxies(c.Server.TrustedProxies); err != nil {
		return err
	}
	if err := c.Email.validate(); err != nil {
		return err
	}
	if p := c.Assistant.Provider; p != "" && p != "anthropic" && p != "openai" {
		return fmt.Errorf("assistant.provider must be \"anthropic\" or \"openai\", not %q", p)
	}
	if _, err := c.Auth.Key(); err != nil {
		return err
	}
	if err := c.Attachments.validate(); err != nil {
		return err
	}
	if d := c.Purchases.WarrantyNoticeDays; d < 0 || d > MaxWarrantyNoticeDays {
		return fmt.Errorf("purchases.warranty_notice_days must be from 0 to %d", MaxWarrantyNoticeDays)
	}
	if d := c.Contracts.ReminderDays; d < 0 || d > MaxContractReminderDays {
		return fmt.Errorf("contracts.reminder_days must be from 0 to %d", MaxContractReminderDays)
	}
	if p := c.Relay.BoneMeshPort; p <= 0 || p > 65535 {
		return fmt.Errorf("invalid relay.bonemesh_port: %d", p)
	}
	for _, pattern := range c.Printers.Devices {
		if !strings.HasPrefix(pattern, "/") {
			return fmt.Errorf("printers.devices pattern %q must be an absolute path", pattern)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("printers.devices pattern %q: %v", pattern, err)
		}
	}
	for _, pattern := range c.Printers.Bluetooth {
		if !bluetoothPattern.MatchString(pattern) {
			return fmt.Errorf("printers.bluetooth pattern %q must be a Bluetooth address such as 7C:2F:80:A1:B2:C3, with * or ? for any part", pattern)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("printers.bluetooth pattern %q: %v", pattern, err)
		}
	}
	return nil
}

// bluetoothPattern is a pattern of Bluetooth addresses.
var bluetoothPattern = regexp.MustCompile(`^[0-9A-Fa-f:*?]+$`)

// mediaType is a media type as attachments.allowed_types names one, and
// extension an extension.
var (
	mediaType = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
	extension = regexp.MustCompile(`^\.[a-z0-9][a-z0-9_+-]*$`)
)

// validate checks the attachments' limits and types, writing each type in
// lower case.
func (a *AttachmentsConfig) validate() error {
	if a.MaxFileMB < 1 || a.MaxFileMB > MaxAttachmentMB {
		return fmt.Errorf("attachments.max_file_mb must be from 1 to %d", MaxAttachmentMB)
	}
	if a.MaxPerObject < 1 {
		return errors.New("attachments.max_per_object must be at least 1")
	}
	for i, t := range a.AllowedTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if !mediaType.MatchString(t) && !extension.MatchString(t) {
			return fmt.Errorf("attachments.allowed_types: %q is neither a media type, such as application/zip, nor an extension, such as .stl", a.AllowedTypes[i])
		}
		a.AllowedTypes[i] = t
	}
	return nil
}

// prefixSegment is one part of an S3 prefix, as of a file's key.
var prefixSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)

// validate checks the storage settings, and gives a prefix its /.
func (s *StorageConfig) validate() error {
	switch s.Backend {
	case "", StorageLocal:
		s.Backend = StorageLocal
		if s.LocalPath == "" {
			return fmt.Errorf("storage.local_path cannot be empty")
		}
	case StorageS3:
		var missing []string
		for name, v := range map[string]string{"endpoint": s.S3.Endpoint, "bucket": s.S3.Bucket,
			"access_key_id": s.S3.AccessKeyID, "secret_access_key": s.S3.SecretAccessKey} {
			if strings.TrimSpace(v) == "" {
				missing = append(missing, "storage.s3."+name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("storage backend s3 needs %s", strings.Join(missing, ", "))
		}
		if u, err := url.Parse(s.S3.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("storage.s3.endpoint %q must be an http:// or https:// address", s.S3.Endpoint)
		}
		if s.S3.Prefix != "" && !strings.HasSuffix(s.S3.Prefix, "/") {
			s.S3.Prefix += "/"
		}
		for _, seg := range strings.Split(strings.TrimSuffix(s.S3.Prefix, "/"), "/") {
			if s.S3.Prefix != "" && !prefixSegment.MatchString(seg) {
				return fmt.Errorf("storage.s3.prefix %q: each part is letters, digits, ., - and _, separated by /", s.S3.Prefix)
			}
		}
	default:
		return fmt.Errorf("unsupported storage backend %q (must be local or s3)", s.Backend)
	}
	return nil
}

// SaveTOML marshals and writes the configuration to the target path.
func (c *Config) SaveTOML(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling TOML config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config file %s: %w", path, err)
	}
	return nil
}

// ParseProxies reads trusted proxies as networks: each an address, which is
// a network of one, or a CIDR range.
func ParseProxies(proxies []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(proxies))
	for _, p := range proxies {
		p = strings.TrimSpace(p)
		if prefix, err := netip.ParsePrefix(p); err == nil {
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(p)
		if err != nil {
			return nil, fmt.Errorf("server.trusted_proxies: %q is neither an address nor a CIDR range", p)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// AssistantConfig sets the assistant beside the page (docs/assistant.md)
// here rather than under Admin, for instances provisioned by a script: each
// setting given takes the place of Admin's, which shows it as set here.
// Left out, each is Admin's to set.
type AssistantConfig struct {
	Enabled  *bool  `toml:"enabled,omitempty"`
	Provider string `toml:"provider,omitempty"`
	Endpoint string `toml:"endpoint,omitempty"`
	Model    string `toml:"model,omitempty"`
	// APIKey is the provider's key; it is never shown or returned.
	APIKey           string `toml:"api_key,omitempty"`
	Name             string `toml:"name,omitempty"`
	MaxTokens        *int   `toml:"max_tokens,omitempty"`
	MaxSteps         *int   `toml:"max_steps,omitempty"`
	MaxTools         *int   `toml:"max_tools,omitempty"`
	PersonMessages   *int   `toml:"person_messages,omitempty"`
	PersonTokens     *int64 `toml:"person_tokens,omitempty"`
	InstanceMessages *int   `toml:"instance_messages,omitempty"`
	InstanceTokens   *int64 `toml:"instance_tokens,omitempty"`
	KeepDays         *int   `toml:"keep_days,omitempty"`
}

// assistantEnv reads BAM_ASSISTANT_* into the assistant's settings.
func assistantEnv(a *AssistantConfig) error {
	if v := os.Getenv("BAM_ASSISTANT_ENABLED"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_ASSISTANT_ENABLED: %w", err)
		}
		a.Enabled = &on
	}
	for env, field := range map[string]*string{
		"BAM_ASSISTANT_PROVIDER": &a.Provider, "BAM_ASSISTANT_ENDPOINT": &a.Endpoint, "BAM_ASSISTANT_MODEL": &a.Model,
		"BAM_ASSISTANT_API_KEY": &a.APIKey, "BAM_ASSISTANT_NAME": &a.Name,
	} {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
	for env, field := range map[string]**int{
		"BAM_ASSISTANT_MAX_TOKENS": &a.MaxTokens, "BAM_ASSISTANT_MAX_STEPS": &a.MaxSteps, "BAM_ASSISTANT_MAX_TOOLS": &a.MaxTools,
		"BAM_ASSISTANT_PERSON_MESSAGES": &a.PersonMessages, "BAM_ASSISTANT_INSTANCE_MESSAGES": &a.InstanceMessages, "BAM_ASSISTANT_KEEP_DAYS": &a.KeepDays,
	} {
		if v := os.Getenv(env); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", env, v)
			}
			*field = &n
		}
	}
	for env, field := range map[string]**int64{
		"BAM_ASSISTANT_PERSON_TOKENS": &a.PersonTokens, "BAM_ASSISTANT_INSTANCE_TOKENS": &a.InstanceTokens,
	} {
		if v := os.Getenv(env); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", env, v)
			}
			*field = &n
		}
	}
	return nil
}
