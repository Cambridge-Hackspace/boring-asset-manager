-- 060_passkeys.sql: passkeys and security keys as a second factor (#191,
-- docs/access.md#passkeys).
--
-- A passkey is a user_factors row of kind "passkey", with no secret: name
-- is what its owner called it, credential_id its ID and public_key its COSE
-- key, both base64url, and rp_id the host of base_url it was registered
-- for, which it can be used at alone. last_step holds its signature
-- counter.
--
-- passkey_challenges are challenges sent to a browser and not yet
-- answered, each for one user and one purpose: registering a passkey,
-- signing in with one, or confirming it is them. An answer takes its
-- challenge, so it cannot be used twice. They last five minutes.

ALTER TABLE user_factors ADD COLUMN name VARCHAR(100) NULL;
ALTER TABLE user_factors ADD COLUMN credential_id TEXT NULL;
ALTER TABLE user_factors ADD COLUMN public_key TEXT NULL;
ALTER TABLE user_factors ADD COLUMN rp_id VARCHAR(255) NULL;

CREATE TABLE passkey_challenges (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose VARCHAR(16) NOT NULL,
    challenge VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_passkey_challenges_user ON passkey_challenges (user_id);
CREATE INDEX idx_passkey_challenges_expires ON passkey_challenges (expires_at);
