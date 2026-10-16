-- 022_scan_templates.sql: what scanning an asset's code shows (README §6.4).
--
-- A scan template is a report of kind scan: details of the asset scanned,
-- tables of what it holds, and quick actions. A category names the one its
-- assets use, and its subcategories inherit it unless they name their own;
-- an asset in no such category uses the default template. assets.scan looks
-- an asset up by its code; it goes to every role that reads assets, in the
-- same scopes, the public role among them.

ALTER TABLE reports ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'report';
ALTER TABLE reports ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE categories ADD COLUMN scan_template_id VARCHAR(36);

INSERT INTO reports (id, name, spec, kind, is_default, created_at, updated_at)
SELECT 'bc4e2a6d-8f0b-4d5c-9e9a-d15c7d9f1a22', 'Scanned',
     '{"filter":{},"sections":[{"type":"details","columns":[{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Where","binding":"{{asset.location}}"},{"heading":"Condition","binding":"{{asset.condition}}"},{"heading":"In stock","binding":"{{stock.quantity}} {{stock.unit}}"}]},{"type":"table","title":"In it","source":"contents","columns":[{"heading":"What","binding":"{{asset.name}}"},{"heading":"In stock","binding":"{{stock.quantity}} {{stock.unit}}"}]},{"type":"actions","actions":["open","use","borrow","return","report"]}]}',
     'scan', TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM reports WHERE id = 'bc4e2a6d-8f0b-4d5c-9e9a-d15c7d9f1a22');

INSERT INTO permissions (id, permission_key, description)
SELECT 'cd5f3b7e-9a1c-4e6d-8f0b-e26d8e0a2b22', 'assets.scan', 'Core permission: assets.scan'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.scan');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'assets.scan'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, np.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions np ON np.permission_key = 'assets.scan'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id);
