-- 004_locks.sql: named lock rows. A transaction that updates one holds it
-- until the transaction ends, so work that must not interleave (such as the
-- anti-lockout check and the change it guards) runs one transaction at a time.

CREATE TABLE IF NOT EXISTS app_locks (
    name VARCHAR(64) PRIMARY KEY,
    counter INTEGER NOT NULL
);

INSERT INTO app_locks (name, counter) VALUES ('users_manage', 0);
