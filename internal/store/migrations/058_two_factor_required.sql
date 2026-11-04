-- 058_two_factor_required.sql: requiring two-factor sign-in by role, and
-- from identity providers (#192, docs/access.md#requiring-two-factor-sign-in).
--
-- roles.requires_two_factor: whoever holds the role must sign in with a
-- second factor. The admin role has it on.
--
-- sessions.factor_setup marks a session of someone required to have a
-- second factor who has none: it can do nothing but set one up.
--
-- On auth_providers, mfa_amr and mfa_acr are the JSON lists of OIDC amr,
-- and acr or SAML AuthnContextClassRef, values that count as two-factor;
-- mfa_fallback asks for BAM!'s own second factor when the provider's sign-in
-- does not count; mfa_always_local asks for it whatever the provider says.

ALTER TABLE roles ADD COLUMN requires_two_factor BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE roles SET requires_two_factor = TRUE WHERE name = 'admin';

ALTER TABLE sessions ADD COLUMN factor_setup BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE auth_providers ADD COLUMN mfa_amr TEXT NULL;
ALTER TABLE auth_providers ADD COLUMN mfa_acr TEXT NULL;
ALTER TABLE auth_providers ADD COLUMN mfa_fallback BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE auth_providers ADD COLUMN mfa_always_local BOOLEAN NOT NULL DEFAULT FALSE;
