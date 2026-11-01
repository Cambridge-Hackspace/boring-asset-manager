-- 039_asset_placements.sql: where a thing stands in its location's scene
-- (docs/icons.md), in tiles from the floor's corner, when someone has placed
-- it; things not placed are packed. A placement belongs to the location
-- link, so moving the thing elsewhere drops it.

CREATE TABLE IF NOT EXISTS asset_placements (
    link_id VARCHAR(36) PRIMARY KEY,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    z DOUBLE PRECISION NOT NULL,
    CONSTRAINT fk_asset_placements_link FOREIGN KEY (link_id) REFERENCES asset_links(id) ON DELETE CASCADE
);
