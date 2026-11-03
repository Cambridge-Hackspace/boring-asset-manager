-- 052_email_notifications.sql: whether email that reaches a person through
-- a role reaches them (#209, docs/notifications.md). Everyone starts with it
-- on; they turn it off under Your account (PUT /api/v1/auth/email).

ALTER TABLE users ADD COLUMN email_notifications BOOLEAN NOT NULL DEFAULT TRUE;
