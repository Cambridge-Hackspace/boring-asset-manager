-- 031_personal_tokens.sql: personal API tokens (README §3.4), which a user
-- makes for themselves, for an assistant (§6.6). A personal token holds no
-- roles: it acts as its user, with their permissions as they are at each
-- request, so user_id names whose it is. Other tokens leave it empty.

ALTER TABLE api_tokens ADD COLUMN user_id VARCHAR(36) NULL;

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);
