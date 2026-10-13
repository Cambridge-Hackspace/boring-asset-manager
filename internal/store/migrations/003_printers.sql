-- 003_printers.sql: network label printers (README §4.4).

CREATE TABLE IF NOT EXISTS printers (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    driver VARCHAR(32) NOT NULL,
    model VARCHAR(32) NOT NULL,
    host VARCHAR(253) NOT NULL,
    port INTEGER NOT NULL,
    media VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_printers_name UNIQUE (name)
);
