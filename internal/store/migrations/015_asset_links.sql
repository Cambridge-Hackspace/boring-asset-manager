-- 015_asset_links.sql: associations between assets (README §3.6).
--
-- A relation type names a link from each end: from_asset "contains" to_asset,
-- and to_asset is "contained in" from_asset. A symmetric type reads the
-- same both ways ("used with"). A single type lets an asset be the far end
-- of one link of it only: a thing is in one place, so linking it to a new
-- bin moves it. A location type's links make an asset's location, such as
-- Shelf 2 > Bin A3. Each link is stored once. Categories may suggest the
-- types that suit their assets.

CREATE TABLE IF NOT EXISTS relation_types (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    inverse_name VARCHAR(64) NOT NULL,
    is_symmetric BOOLEAN NOT NULL DEFAULT FALSE,
    is_single BOOLEAN NOT NULL DEFAULT FALSE,
    is_location BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_relation_types_name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS asset_links (
    id VARCHAR(36) PRIMARY KEY,
    relation_type_id VARCHAR(36) NOT NULL,
    from_asset_id VARCHAR(36) NOT NULL,
    to_asset_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_asset_links UNIQUE (relation_type_id, from_asset_id, to_asset_id),
    CONSTRAINT fk_asset_links_type FOREIGN KEY (relation_type_id) REFERENCES relation_types(id),
    CONSTRAINT fk_asset_links_from FOREIGN KEY (from_asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_links_to FOREIGN KEY (to_asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_links_to ON asset_links (to_asset_id, relation_type_id);

CREATE TABLE IF NOT EXISTS category_relation_types (
    category_id VARCHAR(36) NOT NULL,
    relation_type_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (category_id, relation_type_id),
    CONSTRAINT fk_category_relation_types_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_relation_types_type FOREIGN KEY (relation_type_id) REFERENCES relation_types(id) ON DELETE CASCADE
);

INSERT INTO relation_types (id, name, inverse_name, is_symmetric, is_single, is_location, created_at, updated_at) VALUES
    ('0d3f1c6a-5b2e-4f7a-9c8d-1e2f3a4b5c15', 'contains', 'contained in', FALSE, TRUE, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('1e4a2d7b-6c3f-4a8b-8d9e-2f3a4b5c6d15', 'has part', 'part of', FALSE, TRUE, FALSE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('2f5b3e8c-7d4a-4b9c-9eaf-3a4b5c6d7e15', 'used with', 'used with', TRUE, FALSE, FALSE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- Changes to links serialize on this lock, so two at once cannot make a
-- cycle or put a thing in two places.
INSERT INTO app_locks (name, counter) VALUES ('asset_links', 0);
