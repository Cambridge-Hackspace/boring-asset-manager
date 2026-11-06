-- 067_attachments.sql: files attached to assets and categories (#220,
-- docs/data-model.md#attachments), grown out of assets' photos.
--
-- Each owner has a table of its own, as its other rows do, so that each
-- goes with its owner's kind of information in backups. A vendor's or a
-- contract's will be another table, beside these.
--
-- kind is image, document or other. An image is a photo as before:
-- re-encoded, with a thumbnail, thumb_id, and its size in pixels. The
-- others have no thumbnail. Every file lives under
-- storage.local_path/photos/: the file as <id>, an image's thumbnail as
-- <thumb_id>. Position orders an owner's attachments, from 0; an asset's
-- first image is the one lists show. added_by is the user who added it,
-- if one did.
--
-- Assets' photos become their image attachments, in the same order, named
-- by their files' names, keeping their files where they are.

CREATE TABLE asset_attachments (
    id VARCHAR(36) PRIMARY KEY,
    asset_id VARCHAR(36) NOT NULL,
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
    CONSTRAINT fk_asset_attachments_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_attachments_asset ON asset_attachments (asset_id, position);

INSERT INTO asset_attachments (id, asset_id, kind, position, name, description, filename, content_type, byte_size, thumb_id, width_px, height_px, added_by, created_at)
    SELECT id, asset_id, 'image', position, filename, '', filename, content_type, byte_size, thumb_id, width_px, height_px, NULL, created_at
    FROM asset_photos;

DROP TABLE asset_photos;

CREATE TABLE category_attachments (
    id VARCHAR(36) PRIMARY KEY,
    category_id VARCHAR(36) NOT NULL,
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
    CONSTRAINT fk_category_attachments_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE INDEX idx_category_attachments_category ON category_attachments (category_id, position);
