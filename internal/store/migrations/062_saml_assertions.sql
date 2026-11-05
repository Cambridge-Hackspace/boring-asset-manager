-- 062_saml_assertions.sql: SAML assertions used to sign in (#186,
-- docs/access.md#saml-20).
--
-- Each assertion an identity provider signs is taken once: its ID is kept
-- here, for its provider, until it expires, and a second sign-in with it
-- is refused, however many servers it is given to.

CREATE TABLE saml_assertions (
    provider_id VARCHAR(36) NOT NULL REFERENCES auth_providers(id) ON DELETE CASCADE,
    assertion_id VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    PRIMARY KEY (provider_id, assertion_id)
);

CREATE INDEX idx_saml_assertions_expires ON saml_assertions (expires_at);
