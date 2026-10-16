-- 013_asset_condition.sql: an asset's condition (README §3.2): ok, needs
-- maintenance or out of service, with a note. CONDITION is reserved on
-- MariaDB, hence the column's name.
--
-- The assets:report permission reports a problem without editing assets.
-- Reporting fits anyone who may edit assets, so every role with
-- assets:write is granted it, skipping what a database seeded before
-- migrating already has (as in migration 011).

ALTER TABLE assets ADD COLUMN asset_condition VARCHAR(32) NOT NULL DEFAULT 'ok';
ALTER TABLE assets ADD COLUMN condition_note VARCHAR(255) NOT NULL DEFAULT '';

INSERT INTO permissions (id, permission_key, description)
SELECT 'c4b1d7e2-3f5a-4e8b-9a6c-2d0f8e1b5c13', 'assets:report', 'Core permission: assets:report'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets:report');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, ar.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions ar ON ar.permission_key = 'assets:report'
WHERE p.permission_key = 'assets:write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = ar.id);
