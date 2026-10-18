-- 028_label_stock.sql: custom label stock (README §4.4).
--
-- A stock is a roll known by its size alone: the label width, the length
-- and gap of die-cut labels (none for a continuous roll). Printers whose
-- drivers take any size may name one as their media, in place of one of
-- the rolls the driver has built in; printers.media then holds its ID.

CREATE TABLE IF NOT EXISTS label_stock (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    width_mm DOUBLE PRECISION NOT NULL,
    length_mm DOUBLE PRECISION NOT NULL DEFAULT 0,
    gap_mm DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_label_stock_name UNIQUE (name)
);
