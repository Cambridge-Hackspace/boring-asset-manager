-- 048_user_theme.sql: whether the web interface is light or dark for a
-- person (#168). theme is "light" or "dark", the one they chose, or NULL
-- to follow their system's setting, as everyone does until they choose.
-- It is shown to them as "system" then (PUT /api/v1/auth/theme).

ALTER TABLE users ADD COLUMN theme VARCHAR(16) NULL;
