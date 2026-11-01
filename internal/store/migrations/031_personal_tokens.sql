-- 031_personal_tokens.sql: personal API tokens (docs/access.md), which a user
-- makes for themselves, for an assistant (docs/mcp.md). A personal token holds no
-- roles: it acts as its user, with their permissions as they are at each
-- request, so user_id names whose it is. Other tokens leave it empty.

ALTER TABLE api_tokens ADD COLUMN user_id VARCHAR(36) NULL;

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);
