-- 074_followed_links.sql: links followed through several hops (#263,
-- docs/data-model.md#following-links).
--
-- relation_types.is_followed marks a type whose links make sense as a
-- chain: the laser cutter depends on the extractor, which depends on the
-- three-phase supply. The graph of an asset follows them, and reports count
-- them. Following reads asset_links by type and either end, which its
-- unique key (type, from, to) and idx_asset_links_to (to, type) already
-- index, so no index is added.
--
-- A "depends on" / "needed by" type is seeded, followed, unless a type
-- already has one of its names, which would be written as the same verb in
-- CSV files; a type of the instance's own named "depends on" is followed
-- instead.

ALTER TABLE relation_types ADD COLUMN is_followed BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE relation_types SET is_followed = TRUE WHERE LOWER(name) = 'depends on';

INSERT INTO relation_types (id, name, inverse_name, is_symmetric, is_single, is_location, is_followed, created_at, updated_at)
SELECT '3a6c4f9d-8e5b-4cad-af10-4b5c6d7e8f74', 'depends on', 'needed by', FALSE, FALSE, FALSE, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM relation_types
    WHERE LOWER(name) IN ('depends on', 'needed by', 'depends_on', 'needed_by')
    OR LOWER(inverse_name) IN ('depends on', 'needed by', 'depends_on', 'needed_by'));
