-- 051_sign_in_throttles.sql: failed sign-ins, counted to throttle password
-- guessing (#246, docs/access.md). throttle_key is "account:<email>" or
-- "address:<client address>". A row lapses a window after its last failure
-- or the end of its wait, whichever is later; failures in it are counted
-- under the sign_in lock, so servers sharing the database count together.

CREATE TABLE sign_in_throttles (
    throttle_key VARCHAR(300) PRIMARY KEY,
    failures INTEGER NOT NULL,
    last_failure_at TIMESTAMP NOT NULL,
    blocked_until TIMESTAMP NULL
);

CREATE INDEX idx_sign_in_throttles_last_failure ON sign_in_throttles (last_failure_at);

INSERT INTO app_locks (name, counter) VALUES ('sign_in', 0);
