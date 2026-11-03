-- 055_delivery_log.sql: what each delivery was answered, for the delivery
-- log, and channels that keep failing turned off (#196,
-- docs/notifications.md).
--
-- response_status and response_body are a webhook's answer: its HTTP
-- status, and the first kilobyte of its body, with the channel's secrets
-- taken out. failures counts a channel's failed attempts since its last
-- success, and failing_since is when the first of them was; a channel
-- failing for long enough is turned off, saying why in disabled_reason.

ALTER TABLE notification_deliveries ADD COLUMN response_status INTEGER NULL;
ALTER TABLE notification_deliveries ADD COLUMN response_body TEXT NULL;
ALTER TABLE notification_channels ADD COLUMN failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_channels ADD COLUMN failing_since TIMESTAMP NULL;
ALTER TABLE notification_channels ADD COLUMN disabled_reason TEXT NULL;

CREATE INDEX idx_deliveries_channel ON notification_deliveries (channel_id, created_at);
