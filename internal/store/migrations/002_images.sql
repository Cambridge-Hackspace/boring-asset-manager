-- 002_images.sql: uploaded images placed on labels by static_image elements.
-- The bytes live under storage.local_path/images/<id>; this table holds metadata.

CREATE TABLE IF NOT EXISTS images (
    id VARCHAR(36) PRIMARY KEY,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(64) NOT NULL,
    width_px INTEGER NOT NULL,
    height_px INTEGER NOT NULL,
    byte_size INTEGER NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL
);
