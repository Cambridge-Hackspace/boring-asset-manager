-- 027_live_events.sql: live connections for devices (docs/hooks.md).
--
-- A device opens a WebSocket with an API token holding events.read, and is
-- sent the events it may see as they are recorded. Each open connection is
-- listed in live_connections by the server holding it, which refreshes
-- seen_at while it lasts, so every server can count a token's connections
-- and the tokens page can list them; rows not refreshed lately belong to
-- connections a stopped server held.
--
-- A device may answer an action event: done or failed, with a message.
-- action_replies keeps the answers, with who answered as they were named,
-- for the person who pressed the button to see.
--
-- events.read is given to no role but admin, which seeding gives every
-- permission: devices get roles of their own.

INSERT INTO permissions (id, permission_key, description)
SELECT 'c4e6a8b0-2d3f-4e7a-9b1c-d5f7b9c1e327', 'events.read', 'Core permission: events.read'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'events.read');

CREATE TABLE IF NOT EXISTS live_connections (
    id VARCHAR(36) PRIMARY KEY,
    token_id VARCHAR(36),
    user_id VARCHAR(36),
    remote VARCHAR(64) NOT NULL DEFAULT '',
    opened_at TIMESTAMP NOT NULL,
    seen_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_live_connections_token ON live_connections (token_id, seen_at);

CREATE TABLE IF NOT EXISTS action_replies (
    id VARCHAR(36) PRIMARY KEY,
    event_id VARCHAR(36) NOT NULL,
    token_id VARCHAR(36),
    user_id VARCHAR(36),
    name VARCHAR(255) NOT NULL DEFAULT '',
    ok BOOLEAN NOT NULL,
    message VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_action_replies_event FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE
);

CREATE INDEX idx_action_replies_event ON action_replies (event_id, created_at);
