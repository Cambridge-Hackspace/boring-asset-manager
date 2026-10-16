-- 014_loans.sql: lending assets to people (README §5.3).
--
-- A category may be lendable, and so are its subcategories. A loan names the
-- asset, the user who has it and who lent it; it is open until checked in.
-- overdue_notified_at marks a loan whose overdue notice was recorded, so a
-- sweep run by several servers records it once. Users are archived rather
-- than deleted, so a loan keeps naming its borrower. lent_by and
-- returned_by name the user who acted, or the API token.
--
-- assets:borrow checks an asset out to oneself and back in; assets:lend
-- does both for anyone. Both fit anyone who may edit assets, so roles with
-- assets:write are granted them, as in migrations 011 and 013.

ALTER TABLE categories ADD COLUMN is_lendable BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS asset_loans (
    id VARCHAR(36) PRIMARY KEY,
    asset_id VARCHAR(36) NOT NULL,
    borrower_id VARCHAR(36) NOT NULL,
    lent_by VARCHAR(36),
    checked_out_at TIMESTAMP NOT NULL,
    due_at TIMESTAMP NULL,
    checked_in_at TIMESTAMP NULL,
    returned_by VARCHAR(36),
    overdue_notified_at TIMESTAMP NULL,
    CONSTRAINT fk_asset_loans_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_loans_borrower FOREIGN KEY (borrower_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_loans_asset ON asset_loans (asset_id, checked_in_at);
CREATE INDEX idx_asset_loans_borrower ON asset_loans (borrower_id, checked_in_at);

INSERT INTO permissions (id, permission_key, description)
SELECT 'e2d9a4b6-1c7f-4a35-8b0e-5f3c6d2a9e14', 'assets:borrow', 'Core permission: assets:borrow'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets:borrow');

INSERT INTO permissions (id, permission_key, description)
SELECT 'f7a3c5e1-9b2d-4c68-a1f4-3e8b0d6c2f15', 'assets:lend', 'Core permission: assets:lend'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets:lend');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key IN ('assets:borrow', 'assets:lend')
WHERE p.permission_key = 'assets:write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);
