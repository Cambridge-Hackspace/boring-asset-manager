-- 003_printers.sql: label printers (docs/printing.md). connection_type and address
-- say how BAM reaches the printer; options holds the driver's options as a
-- JSON object, or NULL for its defaults.

CREATE TABLE IF NOT EXISTS printers (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    driver VARCHAR(32) NOT NULL,
    model VARCHAR(32) NOT NULL,
    connection_type VARCHAR(16) NOT NULL,
    address VARCHAR(1024) NOT NULL,
    media VARCHAR(32) NOT NULL,
    options TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_printers_name UNIQUE (name)
);
