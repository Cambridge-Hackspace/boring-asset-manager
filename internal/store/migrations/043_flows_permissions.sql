-- 043_flows_permissions.sql: the permissions of flows (README §3.3, §6.7).
--
-- flows.manage makes and changes flows: every role that manages
-- categories is granted it. flows.run runs them, and is scopable: every
-- role that writes assets is granted it, in the same scopes. Both skip what
-- a database seeded before migrating already has (as in migration 013).

INSERT INTO permissions (id, permission_key, description)
SELECT '5f0c9e2a-7b41-4d8e-a6c3-2e9b1d7f4a42', 'flows.manage', 'Core permission: flows.manage'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'flows.manage');

INSERT INTO permissions (id, permission_key, description)
SELECT '8a3d6b1f-2c95-4e70-b8d4-6f1a3c9e5b42', 'flows.run', 'Core permission: flows.run'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'flows.run');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, fm.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions fm ON fm.permission_key = 'flows.manage'
WHERE p.permission_key = 'categories.manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = fm.id);

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, fr.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions fr ON fr.permission_key = 'flows.run'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = fr.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, fr.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions fr ON fr.permission_key = 'flows.run'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = fr.id AND held.scope_id = rps.scope_id);
