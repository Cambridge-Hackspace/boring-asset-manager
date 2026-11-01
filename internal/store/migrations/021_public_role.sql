-- 021_public_role.sql: the public role and scope (docs/access.md).
--
-- Everyone holds the public role, signed in or not; it may only read. It
-- reads the assets in the public scope, which labels no categories until
-- someone puts some in it, so nothing is public until then.
--
-- A role or scope someone already named public is renamed first: it was
-- never meant for everyone, and must not become so.

UPDATE roles SET name = 'public (renamed)' WHERE name = 'public' AND id <> '9a2c0e4b-6d8f-4b3a-9c7e-bf3a5b7d9e21';
UPDATE scopes SET name = 'public-renamed' WHERE name = 'public' AND id <> '8f1b9d3a-5c7e-4a2f-8b6d-ae2f4a6c8d21';

INSERT INTO scopes (id, name, created_at)
SELECT '8f1b9d3a-5c7e-4a2f-8b6d-ae2f4a6c8d21', 'public', CURRENT_TIMESTAMP
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM scopes WHERE name = 'public');

INSERT INTO roles (id, name, description)
SELECT '9a2c0e4b-6d8f-4b3a-9c7e-bf3a5b7d9e21', 'public', 'Everyone, signed in or not'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'public');

-- On a new database, seeding adds the permissions after the migrations;
-- this one is needed now.
INSERT INTO permissions (id, permission_key, description)
SELECT 'ab3d1f5c-7e9a-4c4b-8d8f-c04b6c8e0f21', 'assets.read', 'Core permission: assets.read'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.read');

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT r.id, p.id, sc.id
FROM roles r
JOIN permissions p ON p.permission_key = 'assets.read'
JOIN scopes sc ON sc.name = 'public'
WHERE r.name = 'public'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = r.id AND held.permission_id = p.id AND held.scope_id = sc.id);
