-- 001_initial_schema.sql: Core schema migration for BAM (SQLite, PostgreSQL, MariaDB compatible)

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS fields (
    id VARCHAR(36) PRIMARY KEY,
    key VARCHAR(64) UNIQUE NOT NULL,
    label VARCHAR(255) NOT NULL,
    validation_type VARCHAR(32) NOT NULL,
    options TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS categories (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id VARCHAR(36) REFERENCES categories(id) ON DELETE SET NULL,
    color_hex VARCHAR(16) NOT NULL DEFAULT '#3b82f6',
    is_consumable BOOLEAN NOT NULL DEFAULT 0,
    is_running_low BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS category_fields (
    category_id VARCHAR(36) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    field_id VARCHAR(36) NOT NULL REFERENCES fields(id) ON DELETE CASCADE,
    is_required BOOLEAN NOT NULL DEFAULT 0,
    PRIMARY KEY (category_id, field_id)
);

CREATE TABLE IF NOT EXISTS assets (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS asset_categories (
    asset_id VARCHAR(36) NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    category_id VARCHAR(36) NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (asset_id, category_id)
);

CREATE TABLE IF NOT EXISTS asset_field_values (
    asset_id VARCHAR(36) NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    field_id VARCHAR(36) NOT NULL REFERENCES fields(id) ON DELETE CASCADE,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (asset_id, field_id)
);

CREATE TABLE IF NOT EXISTS asset_generator_keys (
    asset_id VARCHAR(36) NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    generator_type VARCHAR(32) NOT NULL,
    assigned_code VARCHAR(255) NOT NULL,
    PRIMARY KEY (asset_id, generator_type),
    CONSTRAINT uq_generator_code UNIQUE (generator_type, assigned_code)
);

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    totp_secret VARCHAR(128),
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) UNIQUE NOT NULL,
    description VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS permissions (
    id VARCHAR(36) PRIMARY KEY,
    key VARCHAR(64) UNIQUE NOT NULL,
    description VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id VARCHAR(36) NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id VARCHAR(36) NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id VARCHAR(36) NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key VARCHAR(128) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS templates (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    units VARCHAR(16) NOT NULL DEFAULT 'mm',
    width REAL NOT NULL,
    height REAL NOT NULL,
    dpi INTEGER NOT NULL DEFAULT 300,
    layout_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
