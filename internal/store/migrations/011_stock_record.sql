-- 011_stock_record.sql: the stock:record permission (docs/access.md), which
-- uses, restocks and flags stock without being able to edit assets. Those
-- actions needed assets:write before, so every role that has it is granted
-- stock:record, and nobody loses what they could do. Both steps skip what is
-- already there: a database seeded by this release before migrating has the
-- permission, and the admin role may hold it.

INSERT INTO permissions (id, permission_key, description)
SELECT '6a0e2f4c-8d1b-4f3a-9c5e-7b2d1e0f3a11', 'stock:record', 'Core permission: stock:record'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'stock:record');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, sr.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions sr ON sr.permission_key = 'stock:record'
WHERE p.permission_key = 'assets:write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = sr.id);
