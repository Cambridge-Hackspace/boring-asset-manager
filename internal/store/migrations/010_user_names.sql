-- 010_user_names.sql: a display name for each user (docs/access.md). It is for
-- showing people, not for signing in; empty means the email is shown.

ALTER TABLE users ADD COLUMN name VARCHAR(255) NOT NULL DEFAULT '';
