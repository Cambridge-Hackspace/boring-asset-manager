-- 035_icons_manage.sql: the icons.manage permission (README §3.3, §3.7).
--
-- It changes what every icon is drawn with: the icon library, the palette
-- and the lexicon settings. Every role that manages categories is granted
-- it, as icons are part of what categories look like, skipping what a
-- database seeded before migrating already has (as in migration 013).

INSERT INTO permissions (id, permission_key, description)
SELECT 'ce45ebde-53a4-49bc-9498-4cd14c651f5e', 'icons.manage', 'Core permission: icons.manage'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'icons.manage');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, im.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions im ON im.permission_key = 'icons.manage'
WHERE p.permission_key = 'categories.manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = im.id);
