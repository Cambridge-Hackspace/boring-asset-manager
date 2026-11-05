-- 059_account_links.sql: links emailed to people about their own accounts
-- (#211, docs/access.md#account-emails): an invitation to set a password
-- or sign in through a provider, a password reset, and the removal of a
-- lost second factor.
--
-- id is the SHA-256 of the token in the link, as sessions' are. A link
-- works once, until expires_at: used_at records that it was used. A new
-- link of one purpose replaces those of the same purpose not yet used.

CREATE TABLE account_links (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose VARCHAR(32) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP NULL
);

CREATE INDEX account_links_user ON account_links (user_id, purpose);
