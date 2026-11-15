-- 087_saved_searches.sql: searches kept under a name to run again (#390,
-- docs/data-model.md#saved-searches). 086 is another branch's.
--
-- search_text is what was typed, plain words or a query, as the asset list
-- reads it; it is kept as typed, and checked again each time it is run, so
-- one whose query no longer runs (a field it names was deleted) says so
-- rather than vanishing. A saved search is its person's own; shared ones
-- are listed for everyone who can read assets, and only searches.share
-- shares one. Names are unique for each person, ignoring case, which the
-- store checks.

CREATE TABLE saved_searches (
    id VARCHAR(36) NOT NULL PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    search_text TEXT NOT NULL,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_saved_searches_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_saved_searches_user ON saved_searches (user_id);

-- searches.share shares a saved search with everyone who can read assets.
-- It is not scopable. It goes to the admin role, which is granted every
-- permission when roles are seeded.

INSERT INTO permissions (id, permission_key, description)
SELECT '5e8a2c4d-7f1b-4c9e-b3a6-1d4f7e9c2b58', 'searches.share', 'Core permission: searches.share'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'searches.share');
