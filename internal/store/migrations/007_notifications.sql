-- 007_notifications.sql: notification channels and their deliveries
-- (docs/notifications.md). options is the driver's options as a JSON object of strings,
-- secrets included; events is a JSON array of the event kinds the channel
-- subscribes to. A delivery is one event for one channel: pending until it is
-- sent or fails for good. A worker claims it by setting lease_token and
-- lease_until, and only the holder of an unexpired lease may finish it.

CREATE TABLE IF NOT EXISTS notification_channels (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    driver VARCHAR(32) NOT NULL,
    options TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    events TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_notification_channels_name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id VARCHAR(36) PRIMARY KEY,
    event_id VARCHAR(36) NOT NULL,
    channel_id VARCHAR(36) NOT NULL,
    status VARCHAR(16) NOT NULL,
    attempts INTEGER NOT NULL,
    next_attempt_at TIMESTAMP NOT NULL,
    lease_token VARCHAR(36),
    lease_until TIMESTAMP NULL,
    last_error TEXT,
    created_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP NULL,
    CONSTRAINT fk_deliveries_event FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE,
    CONSTRAINT fk_deliveries_channel FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE
);

CREATE INDEX idx_deliveries_due ON notification_deliveries (status, next_attempt_at);
CREATE INDEX idx_deliveries_created_at ON notification_deliveries (created_at);
