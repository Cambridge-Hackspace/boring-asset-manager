-- 057_identity_providers.sql: signing in through identity providers (#184,
-- docs/access.md#signing-in-with-an-identity-provider).
--
-- auth_providers are the providers set up under Admin, or in the
-- configuration (from_config, which Admin shows read-only and the server
-- keeps in step at start). settings holds the protocol's settings as JSON;
-- secrets its secrets as JSON, sealed with [auth] secret_key and bound to
-- the provider's ID. Those from the configuration keep theirs there.
-- default_role_ids, allowed_domains and allowed_groups are JSON lists.
--
-- auth_provider_rules give a role, optionally narrowed to scopes (a JSON
-- list), to whoever's claim matches, at every sign-in through the provider.
--
-- user_identities link a user to a provider's stable subject: OIDC's sub,
-- or SAML's persistent NameID. Signing in matches on these, never on email.
--
-- user_roles.granted_by is the provider whose rule gave the role, or NULL
-- for one given by hand; signing in through a provider adds and takes away
-- only the roles its rules gave.
--
-- provider_sign_ins are sign-ins sent to a provider and not back yet. id is
-- the SHA-256 of the token in the browser's cookie; state is what the
-- protocol keeps between the two legs, as JSON. They last ten minutes.

CREATE TABLE auth_providers (
    id VARCHAR(36) PRIMARY KEY,
    provider_key VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    protocol VARCHAR(32) NOT NULL,
    settings TEXT NOT NULL,
    secrets TEXT NOT NULL,
    button_label VARCHAR(255) NOT NULL,
    icon VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    position INTEGER NOT NULL DEFAULT 0,
    provision BOOLEAN NOT NULL DEFAULT FALSE,
    default_role_ids TEXT NOT NULL,
    allowed_domains TEXT NOT NULL,
    allowed_groups TEXT NOT NULL,
    link_by_email BOOLEAN NOT NULL DEFAULT FALSE,
    trust_email BOOLEAN NOT NULL DEFAULT FALSE,
    from_config BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_auth_providers_key UNIQUE (provider_key)
);

CREATE TABLE auth_provider_rules (
    id VARCHAR(36) PRIMARY KEY,
    provider_id VARCHAR(36) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    claim VARCHAR(255) NOT NULL,
    claim_value VARCHAR(255) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    scope_ids TEXT NOT NULL,
    CONSTRAINT fk_auth_provider_rules_provider FOREIGN KEY (provider_id) REFERENCES auth_providers(id) ON DELETE CASCADE,
    CONSTRAINT fk_auth_provider_rules_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
);

CREATE INDEX idx_auth_provider_rules_provider ON auth_provider_rules (provider_id);

CREATE TABLE user_identities (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    provider_id VARCHAR(36) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NULL,
    CONSTRAINT uq_user_identities_subject UNIQUE (provider_id, subject),
    CONSTRAINT fk_user_identities_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_identities_provider FOREIGN KEY (provider_id) REFERENCES auth_providers(id) ON DELETE CASCADE
);

CREATE INDEX idx_user_identities_user ON user_identities (user_id);

ALTER TABLE user_roles ADD COLUMN granted_by VARCHAR(36) NULL;

CREATE TABLE provider_sign_ins (
    id VARCHAR(64) PRIMARY KEY,
    provider_id VARCHAR(36) NOT NULL,
    state TEXT NOT NULL,
    next_path TEXT NOT NULL,
    link_user_id VARCHAR(36) NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_provider_sign_ins_provider FOREIGN KEY (provider_id) REFERENCES auth_providers(id) ON DELETE CASCADE
);

CREATE INDEX idx_provider_sign_ins_expires ON provider_sign_ins (expires_at);
