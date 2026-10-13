-- 001_initial_schema.sql: Core schema migration for BAM (SQLite, PostgreSQL, MariaDB compatible)
--
-- Portability notes:
--   * Avoid reserved words as identifiers (KEY is reserved in MariaDB/MySQL).
--   * Foreign keys are declared at table level; column-level REFERENCES clauses
--     are not enforced by every dialect.
--   * TIMESTAMP is rewritten to DATETIME(6) by the migration runner on MariaDB,
--     where TIMESTAMP columns gain implicit ON UPDATE CURRENT_TIMESTAMP defaults.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS fields (
    id VARCHAR(36) PRIMARY KEY,
    field_key VARCHAR(64) NOT NULL,
    label VARCHAR(255) NOT NULL,
    validation_type VARCHAR(32) NOT NULL,
    options TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_fields_key UNIQUE (field_key)
);

CREATE TABLE IF NOT EXISTS categories (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id VARCHAR(36),
    color_hex VARCHAR(16) NOT NULL DEFAULT '#3b82f6',
    is_consumable BOOLEAN NOT NULL DEFAULT FALSE,
    is_running_low BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_categories_parent FOREIGN KEY (parent_id) REFERENCES categories(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS category_fields (
    category_id VARCHAR(36) NOT NULL,
    field_id VARCHAR(36) NOT NULL,
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (category_id, field_id),
    CONSTRAINT fk_category_fields_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_fields_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS assets (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS asset_categories (
    asset_id VARCHAR(36) NOT NULL,
    category_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (asset_id, category_id),
    CONSTRAINT fk_asset_categories_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_categories_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS asset_field_values (
    asset_id VARCHAR(36) NOT NULL,
    field_id VARCHAR(36) NOT NULL,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (asset_id, field_id),
    CONSTRAINT fk_asset_field_values_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_field_values_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS asset_generator_keys (
    asset_id VARCHAR(36) NOT NULL,
    generator_type VARCHAR(32) NOT NULL,
    assigned_code VARCHAR(255) NOT NULL,
    PRIMARY KEY (asset_id, generator_type),
    CONSTRAINT uq_generator_code UNIQUE (generator_type, assigned_code),
    CONSTRAINT fk_asset_generator_keys_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    totp_secret VARCHAR(128),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP NULL,
    CONSTRAINT uq_users_email UNIQUE (email)
);

CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(255) NOT NULL,
    CONSTRAINT uq_roles_name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS permissions (
    id VARCHAR(36) PRIMARY KEY,
    permission_key VARCHAR(64) NOT NULL,
    description VARCHAR(255) NOT NULL,
    CONSTRAINT uq_permissions_key UNIQUE (permission_key)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id VARCHAR(36) NOT NULL,
    permission_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    CONSTRAINT fk_role_permissions_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    CONSTRAINT fk_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS settings (
    setting_key VARCHAR(128) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS templates (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    units VARCHAR(16) NOT NULL DEFAULT 'mm',
    width DOUBLE PRECISION NOT NULL,
    height DOUBLE PRECISION NOT NULL,
    dpi INTEGER NOT NULL DEFAULT 300,
    layout_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
