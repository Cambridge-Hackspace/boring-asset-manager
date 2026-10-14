-- 006_inventory.sql: inventory thresholds and running-low events (README §5.1).
-- low_stock_threshold is NULL when a category has none. Events keep a JSON
-- snapshot of what they describe, so they still read correctly after the
-- category or user is renamed or deleted; category_id is deliberately not a
-- foreign key for the same reason.

ALTER TABLE categories ADD COLUMN low_stock_threshold INTEGER;

CREATE TABLE IF NOT EXISTS events (
    id VARCHAR(36) PRIMARY KEY,
    kind VARCHAR(64) NOT NULL,
    category_id VARCHAR(36),
    actor_user_id VARCHAR(36),
    payload TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_events_created_at ON events (created_at);

INSERT INTO app_locks (name, counter) VALUES ('inventory', 0);
