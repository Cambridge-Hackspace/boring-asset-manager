-- 046_uploads.sql: archives uploaded through the web in chunks, resumably
-- (docs/backups.md, #141). An upload is staged apart from the files (in a
-- staging directory, or as an S3 multipart upload), and only the person who
-- began it may add to it or use it. received is how many bytes have
-- arrived, in order; hash_state is the SHA-256 of those so far, as Go's
-- hash state, so the whole file is checked without reading it again.
-- upload_parts holds each part's tag, which S3 needs to join them. Uploads
-- left unused are removed after a day.

CREATE TABLE IF NOT EXISTS uploads (
    id VARCHAR(36) PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    filename VARCHAR(255) NOT NULL,
    size BIGINT NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    part_size BIGINT NOT NULL,
    received BIGINT NOT NULL,
    hash_state TEXT NOT NULL,
    staging_token VARCHAR(1024) NOT NULL,
    complete BOOLEAN NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_uploads_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_uploads_updated ON uploads (updated_at);

CREATE TABLE IF NOT EXISTS upload_parts (
    upload_id VARCHAR(36) NOT NULL,
    part INTEGER NOT NULL,
    tag VARCHAR(1024) NOT NULL,
    PRIMARY KEY (upload_id, part),
    CONSTRAINT fk_upload_parts_upload FOREIGN KEY (upload_id) REFERENCES uploads(id) ON DELETE CASCADE
);
