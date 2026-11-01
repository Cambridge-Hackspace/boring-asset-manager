-- 009_asset_stock.sql: stock counted by quantity on an asset (docs/stock-and-lending.md).
-- stock_quantity is NULL for an asset whose stock is not counted that way;
-- the unit, threshold and running-low flag only mean something when it is
-- set. Events about an asset name it in asset_id, which, like category_id,
-- is deliberately not a foreign key.

ALTER TABLE assets ADD COLUMN stock_quantity INTEGER;
ALTER TABLE assets ADD COLUMN stock_unit VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN low_stock_threshold INTEGER;
ALTER TABLE assets ADD COLUMN is_running_low BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE events ADD COLUMN asset_id VARCHAR(36);

CREATE INDEX idx_events_asset ON events (asset_id, created_at);
