-- 054_user_factors.sql: second factors for signing in (docs/access.md),
-- and the step between a password and a session.
--
-- user_factors holds each person's second factors: kind "totp" for an
-- authenticator app now, recovery codes and passkeys later. secret is
-- sealed with the configuration's [auth] secret_key, bound to the factor's
-- ID. last_step is the last TOTP step accepted, so that a code is used
-- once. A factor being set up has no confirmed_at, and is not offered at
-- sign-in.
--
-- sign_in_challenges are the second step of a sign-in: a password accepted,
-- a factor not yet. id is the SHA-256 of the token in the browser's cookie.
-- They last five minutes and grant nothing else.

CREATE TABLE user_factors (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    secret TEXT NOT NULL,
    last_step BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    confirmed_at TIMESTAMP NULL,
    last_used_at TIMESTAMP NULL
);

CREATE INDEX idx_user_factors_user ON user_factors (user_id);

CREATE TABLE sign_in_challenges (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_sign_in_challenges_expires ON sign_in_challenges (expires_at);
