package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the complete application configuration.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Storage  StorageConfig  `toml:"storage"`
}

// ServerConfig holds HTTP server listening and security configuration.
type ServerConfig struct {
	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	BaseURL       string `toml:"base_url"`
	SessionSecret string `toml:"session_secret"`
}

// DatabaseConfig holds database connection configuration.
type DatabaseConfig struct {
	Type string `toml:"type"` // "sqlite", "postgres", "mariadb"
	DSN  string `toml:"dsn"`
}

// StorageConfig holds filesystem storage configuration.
type StorageConfig struct {
	LocalPath string `toml:"local_path"`
}

// DefaultConfig returns safe default settings.
func DefaultConfig() *Config {
	randomSecret := make([]byte, 32)
	_, _ = rand.Read(randomSecret)

	return &Config{
		Server: ServerConfig{
			Host:          "0.0.0.0",
			Port:          8080,
			BaseURL:       "http://localhost:8080",
			SessionSecret: hex.EncodeToString(randomSecret),
		},
		Database: DatabaseConfig{
			Type: "sqlite",
			DSN:  "data/bam.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		},
		Storage: StorageConfig{
			LocalPath: "./data/uploads",
		},
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

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
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
	if secret := os.Getenv("BAM_SERVER_SESSION_SECRET"); secret != "" {
		cfg.Server.SessionSecret = secret
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
	if c.Server.SessionSecret == "" {
		return fmt.Errorf("session_secret cannot be empty")
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
