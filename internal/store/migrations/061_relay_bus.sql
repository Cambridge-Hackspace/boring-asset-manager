-- 061_relay_bus.sql: the relay bus, when several servers share a database
-- (#228, docs/relays.md#several-servers).
--
-- One server holds each relay at a time over each transport, and claims it
-- in relay_holds, refreshing seen_at while it does; a claim not refreshed
-- lately was left by a stopped server. A server sending to a relay another
-- holds leaves the message in relay_mail for that server, which takes it
-- and sends it on; the reply comes back the same way. Mail no server takes
-- is dropped after a few minutes: the sender has given up on it by then.

CREATE TABLE relay_holds (
    node VARCHAR(64) NOT NULL,
    transport VARCHAR(16) NOT NULL,
    server VARCHAR(64) NOT NULL,
    seen_at TIMESTAMP NOT NULL,
    PRIMARY KEY (node, transport)
);

CREATE TABLE relay_mail (
    id VARCHAR(36) PRIMARY KEY,
    server VARCHAR(64) NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX relay_mail_server ON relay_mail (server, created_at);
