package config

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the complete application configuration.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Storage  StorageConfig  `toml:"storage"`
	Printers PrintersConfig `toml:"printers"`
	MCP      MCPConfig      `toml:"mcp"`
}

// ServerConfig holds HTTP server listening configuration.
type ServerConfig struct {
	Host    string `toml:"host"`
	Port    int    `toml:"port"`
	BaseURL string `toml:"base_url"`
}

// DatabaseConfig holds database connection configuration.
type DatabaseConfig struct {
	Type string `toml:"type"` // "sqlite", "postgres", "mariadb"
	DSN  string `toml:"dsn"`
}

// StorageConfig says where uploaded files are kept (README §2): in a
// directory, LocalPath, or with Backend "s3", in a bucket of an
// S3-compatible store.
type StorageConfig struct {
	// Backend is "local" (the default, also when empty) or "s3".
	Backend   string   `toml:"backend"`
	LocalPath string   `toml:"local_path"`
	S3        S3Config `toml:"s3"`
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
}

// MCPConfig turns the MCP endpoint, for assistants, on or off (README §6.6).
type MCPConfig struct {
	// Enabled serves /api/v1/mcp. It is on unless turned off, so a config
	// file written before it existed, which has no [mcp] table, keeps it on.
	Enabled bool `toml:"enabled"`
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
		MCP: MCPConfig{Enabled: true},
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

	// Refused rather than ignored when not understood: a mistyped "off"
	// must not leave the endpoint on.
	if v := os.Getenv("BAM_MCP_ENABLED"); v != "" {
		on, err := parseSwitch(v)
		if err != nil {
			return fmt.Errorf("BAM_MCP_ENABLED: %w", err)
		}
		cfg.MCP.Enabled = on
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
	for _, pattern := range c.Printers.Devices {
		if !strings.HasPrefix(pattern, "/") {
			return fmt.Errorf("printers.devices pattern %q must be an absolute path", pattern)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("printers.devices pattern %q: %v", pattern, err)
		}
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
