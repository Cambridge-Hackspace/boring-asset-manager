-- 020_assets_track.sql: assets.track (README §3.3), seeing the people
-- around assets: borrowers, who changed them, and the loans list. Reading
-- assets showed them until now, so every role that reads assets keeps
-- seeing them, in the same scopes.

INSERT INTO permissions (id, permission_key, description)
SELECT '7e0a8c2f-4b6d-4f1e-9a5c-9d1e3f5b7c20', 'assets.track', 'Core permission: assets.track'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.track');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'assets.track'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, np.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions np ON np.permission_key = 'assets.track'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id);
