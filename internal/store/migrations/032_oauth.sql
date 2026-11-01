-- 032_oauth.sql: signing assistants in by OAuth 2.1 (docs/mcp.md). A client
-- registers itself; a person approves it, and it trades the code for a
-- personal token that acts as them, renewed while used.
--
-- A grant is a personal token (migration 031): its access secret lasts an
-- hour, and a refresh secret, kept only as its hash like every secret,
-- renews both. Codes are single-use, short-lived, and kept hashed too.

CREATE TABLE IF NOT EXISTS oauth_clients (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    redirect_uris TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_codes (
    code_hash VARCHAR(64) PRIMARY KEY,
    client_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    redirect_uri TEXT NOT NULL,
    challenge VARCHAR(128) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_oauth_codes_client FOREIGN KEY (client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE,
    CONSTRAINT fk_oauth_codes_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

ALTER TABLE api_tokens ADD COLUMN oauth_client_id VARCHAR(36) NULL;
ALTER TABLE api_tokens ADD COLUMN refresh_hash VARCHAR(64) NULL;
ALTER TABLE api_tokens ADD COLUMN refresh_expires_at TIMESTAMP NULL;

CREATE INDEX idx_api_tokens_refresh ON api_tokens (refresh_hash);
