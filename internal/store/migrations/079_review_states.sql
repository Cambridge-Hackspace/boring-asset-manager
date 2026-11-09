-- 079_review_states.sql: review states and dates to review by (#265,
-- docs/review.md). 076 is #300's, 077 #264's and 078 #262's.
--
-- An asset in a category that uses review states is draft, confirmed or
-- verified:
--
--   - review_state is draft, confirmed or verified; NULL until it is first
--     set, which reads as draft once one of its categories uses review;
--   - review_by and review_at are who set it, a user's or an API token's
--     ID, and when;
--   - confirmed_by and confirmed_at are who last confirmed it, and when, so
--     that someone cannot verify what they confirmed themselves;
--   - reviewed_at is when it was last confirmed or verified, and review_due
--     when it is next to be reviewed: reviewed_at and its categories' months,
--     NULL when they ask for no review or it is a draft;
--   - review_notified is the review_due that asset.review_due was recorded
--     for. Each is claimed by setting it, as running low is flagged, so
--     servers sharing the database record it once for each date.

ALTER TABLE assets ADD COLUMN review_state VARCHAR(16);
ALTER TABLE assets ADD COLUMN review_by VARCHAR(36);
ALTER TABLE assets ADD COLUMN review_at TIMESTAMP;
ALTER TABLE assets ADD COLUMN confirmed_by VARCHAR(36);
ALTER TABLE assets ADD COLUMN confirmed_at TIMESTAMP;
ALTER TABLE assets ADD COLUMN reviewed_at TIMESTAMP;
ALTER TABLE assets ADD COLUMN review_due TIMESTAMP;
ALTER TABLE assets ADD COLUMN review_notified TIMESTAMP;

CREATE INDEX idx_assets_review_due ON assets (review_due);

-- A category's own review settings, each NULL to take its parent's, as its
-- subcategories take its own:
--
--   - review_enabled turns review states on, or off below a category that
--     has them on;
--   - review_months asks for review that many months after an asset is
--     confirmed or verified, 0 for never;
--   - review_on_edit is draft, returning a confirmed or verified asset to
--     draft when it is edited, or keep;
--   - review_self_verify lets whoever confirmed an asset verify it too.

ALTER TABLE categories ADD COLUMN review_enabled BOOLEAN;
ALTER TABLE categories ADD COLUMN review_months INTEGER;
ALTER TABLE categories ADD COLUMN review_on_edit VARCHAR(16);
ALTER TABLE categories ADD COLUMN review_self_verify BOOLEAN;

-- asset_proposals are changes made through an assistant that landed as a
-- draft for someone to accept or reject (docs/review.md#assistants), one
-- for each asset with any waiting:
--
--   - prior_state is the review state the asset had before them;
--   - record is the asset as it was before them, as JSON, which rejecting
--     puts back; NULL when it could not be read, and then they can only be
--     accepted;
--   - is_new marks an asset the assistant made, which rejecting archives;
--   - proposed_by and via are who made them, a user's or an API token's
--     ID, and the assistant, as events name it.
--
-- They go with their asset.

CREATE TABLE asset_proposals (
    asset_id VARCHAR(36) PRIMARY KEY,
    prior_state VARCHAR(16),
    record TEXT,
    is_new BOOLEAN NOT NULL,
    proposed_by VARCHAR(36),
    via VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_asset_proposals_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

-- assets.confirm moves an asset from draft to confirmed, and assets.verify
-- from confirmed to verified, or back to draft; both are scopable. Every
-- role that edits assets is granted assets.confirm where it edits them, so
-- what it adds is taken as true as it is today, until a category asks for
-- more; assets.verify goes to the admin role, which is granted every
-- permission when roles are seeded. What a database seeded before migrating
-- already has is skipped (as in migration 070).

INSERT INTO permissions (id, permission_key, description)
SELECT '5d1e8b3a-7c4f-4e2a-9b6d-0f3a5c7e9b21', 'assets.confirm', 'Core permission: assets.confirm'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.confirm');

INSERT INTO permissions (id, permission_key, description)
SELECT '8a2f6c4e-1b3d-4f5a-8e7c-2d4b6f8a0c35', 'assets.verify', 'Core permission: assets.verify'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.verify');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'assets.confirm'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, np.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions np ON np.permission_key = 'assets.confirm'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id);
