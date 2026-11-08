-- 076_purchase_orders.sql: purchase orders to vendors (#222,
-- docs/purchase-orders.md). 075 is #280's.
--
-- A purchase order is to a vendor (068), whose scopes decide who sees it.
-- Deleting a vendor that has orders is refused; the cascade is for what
-- replaces vendors wholesale, such as restoring a backup.
--
--   - number is the order's own, allocated as one more than the highest
--     under the purchase_orders lock, and shown as PO-0012;
--   - state is draft, ordered, partly_received, received or canceled;
--   - currency is the ISO 4217 code its prices are in, one for the whole
--     order, empty until a line has a price;
--   - vendor_reference is the vendor's order or confirmation number;
--   - ordered_on, expected_on and received_on are dates, written
--     YYYY-MM-DD so they compare and sort as text on every database;
--     received_on is when the last of it came in;
--   - deliver_to is where it is to be delivered, and notes are Markdown.

CREATE TABLE purchase_orders (
    id VARCHAR(36) PRIMARY KEY,
    number INTEGER NOT NULL,
    vendor_id VARCHAR(36) NOT NULL,
    state VARCHAR(16) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    vendor_reference VARCHAR(255) NOT NULL,
    ordered_on VARCHAR(10),
    expected_on VARCHAR(10),
    received_on VARCHAR(10),
    deliver_to TEXT NOT NULL,
    notes TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_purchase_orders_number UNIQUE (number),
    CONSTRAINT fk_purchase_orders_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE
);

CREATE INDEX idx_purchase_orders_vendor ON purchase_orders (vendor_id);
CREATE INDEX idx_purchase_orders_state ON purchase_orders (state);

-- An order's lines, in order by position. A line drawn from a catalog
-- entry (073) names it in category_entry_id or asset_entry_id, one of them,
-- since entries are kept in two tables, and what receiving acts on, the
-- entry's category or asset, in category_id or asset_id. A free line may
-- name a category or an asset, or neither. Each is forgotten when what it
-- names is deleted, leaving the line's own text.
--
--   - part_number, description, pack_size and pack_unit are copied from the
--     entry when the line is added, and the line's own after that;
--   - quantity is how many packs are ordered, and quantity_received how
--     many have come;
--   - unit_price_minor is the price of a pack in the order's currency's
--     minor units, exactly, as asset_purchases' price_minor is (070).

CREATE TABLE purchase_order_lines (
    id VARCHAR(36) PRIMARY KEY,
    purchase_order_id VARCHAR(36) NOT NULL,
    position INTEGER NOT NULL,
    category_entry_id VARCHAR(36),
    asset_entry_id VARCHAR(36),
    category_id VARCHAR(36),
    asset_id VARCHAR(36),
    part_number VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    pack_size INTEGER,
    pack_unit VARCHAR(64) NOT NULL,
    quantity INTEGER NOT NULL,
    quantity_received INTEGER NOT NULL,
    unit_price_minor BIGINT,
    CONSTRAINT fk_purchase_order_lines_order FOREIGN KEY (purchase_order_id) REFERENCES purchase_orders(id) ON DELETE CASCADE,
    CONSTRAINT fk_purchase_order_lines_category_entry FOREIGN KEY (category_entry_id) REFERENCES category_catalog_entries(id) ON DELETE SET NULL,
    CONSTRAINT fk_purchase_order_lines_asset_entry FOREIGN KEY (asset_entry_id) REFERENCES asset_catalog_entries(id) ON DELETE SET NULL,
    CONSTRAINT fk_purchase_order_lines_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE SET NULL,
    CONSTRAINT fk_purchase_order_lines_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE SET NULL
);

CREATE INDEX idx_purchase_order_lines_order ON purchase_order_lines (purchase_order_id, position);
CREATE INDEX idx_purchase_order_lines_category ON purchase_order_lines (category_id);
CREATE INDEX idx_purchase_order_lines_asset ON purchase_order_lines (asset_id);
CREATE INDEX idx_purchase_order_lines_category_entry ON purchase_order_lines (category_entry_id);
CREATE INDEX idx_purchase_order_lines_asset_entry ON purchase_order_lines (asset_entry_id);

-- An order's files, such as the vendor's quote, confirmation and invoice,
-- are kept as contracts' are (072), in a table of their own, under
-- storage.local_path/photos/.

CREATE TABLE purchase_order_attachments (
    id VARCHAR(36) PRIMARY KEY,
    purchase_order_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    position INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(127) NOT NULL,
    byte_size BIGINT NOT NULL,
    thumb_id VARCHAR(36),
    width_px INTEGER,
    height_px INTEGER,
    added_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_purchase_order_attachments_order FOREIGN KEY (purchase_order_id) REFERENCES purchase_orders(id) ON DELETE CASCADE
);

CREATE INDEX idx_purchase_order_attachments_order ON purchase_order_attachments (purchase_order_id, position);

-- Events about an order, and those a receipt records on assets and
-- categories, name it in purchase_order_id, which, like contract_id, is
-- deliberately not a foreign key, so its history reads as it was after it
-- is deleted.

ALTER TABLE events ADD COLUMN purchase_order_id VARCHAR(36);

CREATE INDEX idx_events_purchase_order ON events (purchase_order_id, created_at);

-- New orders' numbers are allocated under this lock.

INSERT INTO app_locks (name, counter) VALUES ('purchase_orders', 0);
