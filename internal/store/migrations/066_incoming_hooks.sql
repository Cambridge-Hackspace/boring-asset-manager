-- 066_incoming_hooks.sql: incoming hooks, URLs that let something outside
-- BAM! act on it (#197, docs/hooks.md#incoming-hooks).
--
-- Each has a long random ID, its address; a secret, kept so that it cannot
-- be read back: a bearer secret as its SHA-256 hash, and a signing secret,
-- which checking a signature needs, sealed with [auth] secret_key; what it
-- does, and how it finds the asset, as JSON; and how many requests a
-- minute it takes. It acts as the roles it is given, each in every scope or
-- narrowed to some, as an API token does.
--
-- incoming_deliveries keeps the delivery IDs of signed requests until they
-- are too old to be taken anyway, so each is taken once, however many
-- servers it is sent to.

CREATE TABLE incoming_hooks (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL,
    auth VARCHAR(16) NOT NULL,
    secret_hash VARCHAR(64) NULL,
    sealed_secret TEXT NULL,
    action VARCHAR(16) NOT NULL,
    settings TEXT NOT NULL,
    rate_limit INTEGER NOT NULL,
    created_by VARCHAR(36) NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NULL
);

CREATE TABLE incoming_hook_roles (
    hook_id VARCHAR(64) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (hook_id, role_id),
    CONSTRAINT fk_incoming_hook_roles_hook FOREIGN KEY (hook_id) REFERENCES incoming_hooks(id) ON DELETE CASCADE,
    CONSTRAINT fk_incoming_hook_roles_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE TABLE incoming_hook_role_scopes (
    hook_id VARCHAR(64) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (hook_id, role_id, scope_id),
    CONSTRAINT fk_incoming_hook_role_scopes_assignment FOREIGN KEY (hook_id, role_id) REFERENCES incoming_hook_roles(hook_id, role_id) ON DELETE CASCADE,
    CONSTRAINT fk_incoming_hook_role_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);

CREATE TABLE incoming_deliveries (
    hook_id VARCHAR(64) NOT NULL,
    delivery_id VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    PRIMARY KEY (hook_id, delivery_id),
    CONSTRAINT fk_incoming_deliveries_hook FOREIGN KEY (hook_id) REFERENCES incoming_hooks(id) ON DELETE CASCADE
);

CREATE INDEX idx_incoming_deliveries_expires ON incoming_deliveries (expires_at);
