-- 012_api_tokens.sql: API tokens for scripts and dashboards (README §3.4).
-- A token is shown once; only the SHA-256 hash of its secret is stored, as
-- for sessions. Its permissions are those of its roles. Revoked tokens are
-- kept, so the history can still name them.

CREATE TABLE IF NOT EXISTS api_tokens (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    created_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NULL,
    last_used_at TIMESTAMP NULL,
    revoked_at TIMESTAMP NULL,
    CONSTRAINT uq_api_tokens_hash UNIQUE (token_hash)
);

CREATE TABLE IF NOT EXISTS api_token_roles (
    token_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (token_id, role_id),
    CONSTRAINT fk_api_token_roles_token FOREIGN KEY (token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
    CONSTRAINT fk_api_token_roles_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);
