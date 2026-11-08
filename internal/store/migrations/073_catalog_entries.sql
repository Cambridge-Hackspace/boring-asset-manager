-- 073_catalog_entries.sql: vendors' catalogs, what each vendor sells, with
-- part numbers and prices (#219, docs/vendors.md#catalogs).
--
-- A catalog entry is one thing a vendor sells: its part number, a
-- description, a link to its product page, its pack (pack_size of
-- pack_unit, such as 100 screws), and its price as last checked. Each is
-- linked to what is kept: a category, for things counted by items, or an
-- asset, for things counted by quantity. They are two tables, the same but
-- for that link, so that each goes with its kind of information in a
-- backup: category_catalog_entries with the catalog, and
-- asset_catalog_entries with the assets.
--
--   - pack_size is a whole number of at least 1, or NULL when not known;
--   - price_minor is the price in the currency's minor units, exactly, as
--     asset_purchases' is (070); currency is its ISO 4217 code;
--   - price_checked is when the price was last checked, written YYYY-MM-DD.
--     Only the last checked price is kept;
--   - preferred marks the entry to reorder from: at most one of a
--     category's, or an asset's, entries, which the store keeps so.
--
-- Entries go with their vendor, and with what they are linked to. Their
-- IDs are unique across both tables.

CREATE TABLE category_catalog_entries (
    id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36) NOT NULL,
    category_id VARCHAR(36) NOT NULL,
    part_number VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    url VARCHAR(2048) NOT NULL,
    pack_size INTEGER,
    pack_unit VARCHAR(64) NOT NULL,
    price_minor BIGINT,
    currency VARCHAR(3) NOT NULL,
    price_checked VARCHAR(10),
    preferred BOOLEAN NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_category_catalog_entries_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_catalog_entries_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE INDEX idx_category_catalog_entries_vendor ON category_catalog_entries (vendor_id);
CREATE INDEX idx_category_catalog_entries_category ON category_catalog_entries (category_id);

CREATE TABLE asset_catalog_entries (
    id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    part_number VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    url VARCHAR(2048) NOT NULL,
    pack_size INTEGER,
    pack_unit VARCHAR(64) NOT NULL,
    price_minor BIGINT,
    currency VARCHAR(3) NOT NULL,
    price_checked VARCHAR(10),
    preferred BOOLEAN NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_asset_catalog_entries_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_catalog_entries_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_catalog_entries_vendor ON asset_catalog_entries (vendor_id);
CREATE INDEX idx_asset_catalog_entries_asset ON asset_catalog_entries (asset_id);
