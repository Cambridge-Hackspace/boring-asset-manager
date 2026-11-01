-- 018_asset_photos.sql: photos of assets (docs/data-model.md). The files live under
-- storage.local_path/photos/: the photo as <id>, its thumbnail as
-- <thumb_id>. Position orders an asset's photos, from 0; the first is the
-- one lists show.

CREATE TABLE IF NOT EXISTS asset_photos (
    id VARCHAR(36) PRIMARY KEY,
    asset_id VARCHAR(36) NOT NULL,
    thumb_id VARCHAR(36) NOT NULL,
    position INTEGER NOT NULL,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(64) NOT NULL,
    width_px INTEGER NOT NULL,
    height_px INTEGER NOT NULL,
    byte_size INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_asset_photos_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_photos_asset ON asset_photos (asset_id, position);
