-- 070_purchases.sql: where an asset came from (#218,
-- docs/data-model.md#purchases).
--
-- asset_purchases holds an asset's purchase details, one row per asset that
-- has any, deleted with it. Each detail is optional:
--
--   - vendor_id is the vendor it was bought from (068), forgotten when the
--     vendor is deleted;
--   - order_date, received_date and warranty_end are dates, written
--     YYYY-MM-DD so they compare and sort as text on every database;
--   - price_minor is the price in the currency's minor units (pence, cents,
--     or yen, which have none), exactly, never a float; currency is its ISO
--     4217 code;
--   - order_number is the order or invoice number;
--   - invoice_attachment_id is the invoice, one of the asset's own
--     attachments (067), forgotten when that attachment is removed.
--
-- They are a table of their own, rather than columns on assets, so that the
-- invoice can name an attachment, which names its asset, without a cycle of
-- foreign keys between assets and asset_attachments.

CREATE TABLE asset_purchases (
    asset_id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36),
    order_date VARCHAR(10),
    received_date VARCHAR(10),
    price_minor BIGINT,
    currency VARCHAR(3) NOT NULL,
    order_number VARCHAR(255) NOT NULL,
    warranty_end VARCHAR(10),
    invoice_attachment_id VARCHAR(36),
    CONSTRAINT fk_asset_purchases_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_purchases_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE SET NULL,
    CONSTRAINT fk_asset_purchases_invoice FOREIGN KEY (invoice_attachment_id) REFERENCES asset_attachments(id) ON DELETE SET NULL
);

CREATE INDEX idx_asset_purchases_vendor ON asset_purchases (vendor_id);
CREATE INDEX idx_asset_purchases_warranty ON asset_purchases (warranty_end);

-- warranty_notices are the warranty.expiring events recorded, one for each
-- asset and warranty end. Each is claimed by inserting it, so servers
-- sharing the database record it once; a new end date is a new notice.
-- They go with their asset.

CREATE TABLE warranty_notices (
    asset_id VARCHAR(36) NOT NULL,
    warranty_end VARCHAR(10) NOT NULL,
    sent_at TIMESTAMP NOT NULL,
    PRIMARY KEY (asset_id, warranty_end),
    CONSTRAINT fk_warranty_notices_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

-- purchases.read sees an asset's price, currency, order number and invoice,
-- and is scopable. Every role that reads assets is granted it where it
-- reads them: in every scope when it reads them in every scope, and in each
-- scope it reads them in otherwise, the public role's among them, so no one
-- sees less than before. What a database seeded before migrating already
-- has is skipped (as in migration 022).

INSERT INTO permissions (id, permission_key, description)
SELECT 'e7a3c1f5-2b9d-4c6e-8a0f-3d5b7e9c1a24', 'purchases.read', 'Core permission: purchases.read'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'purchases.read');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'purchases.read'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, np.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions np ON np.permission_key = 'purchases.read'
WHERE p.permission_key = 'assets.read'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id);
