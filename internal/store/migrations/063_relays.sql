-- 063_relays.sql: pairing relays (#229, docs/relays.md#pairing).
--
-- relays lists the relays an admin added. Until one pairs it has a
-- one-time code, kept as its SHA-256 (code_hash) until code_expires_at;
-- pairing clears the code and keeps the relay's public identity key, the
-- SHA-256 of its WebSocket credential, and when its BoneMesh certificate
-- expires. Revoking a paired relay sets revoked_at and drops its
-- credential, and the row stays, refusing the relay's key and keeping its
-- name, until its last certificate has expired: that is the denylist.
-- online, status_at, version, address and allowlist are its health.
--
-- relay_mesh holds the mesh root and the main node's own BoneMesh
-- identity, one row. Both private keys are sealed with [auth] secret_key.
-- mesh names the mesh, and changes when the root is rotated.

CREATE TABLE relays (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(63) NOT NULL UNIQUE,
    code_hash VARCHAR(64),
    code_expires_at TIMESTAMP NULL,
    identity_key TEXT,
    credential_hash VARCHAR(64),
    cert_expires_at TIMESTAMP NULL,
    paired_at TIMESTAMP NULL,
    revoked_at TIMESTAMP NULL,
    online BOOLEAN NOT NULL DEFAULT FALSE,
    status_at TIMESTAMP NULL,
    version VARCHAR(64) NOT NULL DEFAULT '',
    address VARCHAR(64) NOT NULL DEFAULT '',
    allowlist TEXT,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX relays_code ON relays (code_hash);
CREATE INDEX relays_credential ON relays (credential_hash);

CREATE TABLE relay_mesh (
    id VARCHAR(16) PRIMARY KEY,
    mesh VARCHAR(64) NOT NULL,
    root_public TEXT NOT NULL,
    root_private TEXT NOT NULL,
    main_public TEXT NOT NULL,
    main_private TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
