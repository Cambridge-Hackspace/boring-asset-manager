-- 017_reports.sql: saved reports (docs/reports.md). A report's spec is JSON, as
-- a label template's is: a filter over assets and sections showing them.
--
-- reports:manage creates and edits reports; reading them needs only
-- assets:read. Reports are built from template bindings, so roles that
-- manage templates are granted it, as in migration 011. Two reports to
-- start from are seeded: a shopping list of what is running low, and what
-- is out on loan.

CREATE TABLE IF NOT EXISTS reports (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    spec TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

INSERT INTO reports (id, name, spec, created_at, updated_at) VALUES
    ('3a6c4e8b-0d2f-4b7a-9c1e-5f7a9b1d3e17', 'Running low',
     '{"filter":{"running_low":true},"sections":[{"type":"table","title":"Shopping list","columns":[{"heading":"Asset","binding":"{{asset.name}}"},{"heading":"In stock","binding":"{{stock.quantity}} {{stock.unit}}"},{"heading":"Where","binding":"{{asset.location}}"}],"group_by":"category"}]}',
     CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('4b7d5f9c-1e3a-4c8b-8d2f-6a8b0c2e4f17', 'On loan',
     '{"filter":{"loan":"open"},"sections":[{"type":"table","title":"Lent out","columns":[{"heading":"Asset","binding":"{{asset.name}}"},{"heading":"Borrower","binding":"{{loan.borrower}}"},{"heading":"Due back","binding":"{{loan.due}}"}],"sort_by":2}]}',
     CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT INTO permissions (id, permission_key, description)
SELECT '5c8e6a0d-2f4b-4d9c-9e3a-7b9c1d3f5a17', 'reports:manage', 'Core permission: reports:manage'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'reports:manage');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'reports:manage'
WHERE p.permission_key = 'templates:manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);
