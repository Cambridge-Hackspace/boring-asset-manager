-- 068_vendors.sql: vendors, the suppliers a space buys from, and how to
-- reach them (#217, docs/vendors.md).
--
-- A vendor's website, account number, address and notes are empty when not
-- given. notes are Markdown. Its contacts are in order, by position from 0.
--
-- A vendor's scopes decide who sees it, as a category's decide who sees its
-- assets: someone sees it who may read assets in one of them, and everyone
-- who reads assets sees one with none.
--
-- A vendor's files are kept as assets' and categories' are (067), in a
-- table of their own, under storage.local_path/photos/.
--
-- A category names the vendors it is bought from, in order of preference.
--
-- Events about a vendor name it in vendor_id, which, like category_id and
-- asset_id, is deliberately not a foreign key.

CREATE TABLE vendors (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    website VARCHAR(2048) NOT NULL,
    account_number VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    notes TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_vendors_name ON vendors (name);

CREATE TABLE vendor_contacts (
    id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36) NOT NULL,
    position INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(255) NOT NULL,
    email VARCHAR(320) NOT NULL,
    phone VARCHAR(64) NOT NULL,
    CONSTRAINT fk_vendor_contacts_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE
);

CREATE INDEX idx_vendor_contacts_vendor ON vendor_contacts (vendor_id, position);

CREATE TABLE vendor_scopes (
    vendor_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (vendor_id, scope_id),
    CONSTRAINT fk_vendor_scopes_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE,
    CONSTRAINT fk_vendor_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);

CREATE TABLE vendor_attachments (
    id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    position INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(127) NOT NULL,
    byte_size BIGINT NOT NULL,
    thumb_id VARCHAR(36),
    width_px INTEGER,
    height_px INTEGER,
    added_by VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_vendor_attachments_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE
);

CREATE INDEX idx_vendor_attachments_vendor ON vendor_attachments (vendor_id, position);

CREATE TABLE category_vendors (
    category_id VARCHAR(36) NOT NULL,
    vendor_id VARCHAR(36) NOT NULL,
    sort_order INTEGER NOT NULL,
    PRIMARY KEY (category_id, vendor_id),
    CONSTRAINT fk_category_vendors_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_vendors_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE
);

ALTER TABLE events ADD COLUMN vendor_id VARCHAR(36);

CREATE INDEX idx_events_vendor ON events (vendor_id, created_at);

-- vendors.manage creates and edits vendors, and is scopable. Every role
-- that manages categories is granted it, in every scope, skipping what a
-- database seeded before migrating already has (as in migration 043).

INSERT INTO permissions (id, permission_key, description)
SELECT 'c4e2a7d9-3b8f-4a61-9e5d-7f1b2c8a6d43', 'vendors.manage', 'Core permission: vendors.manage'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'vendors.manage');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, vm.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions vm ON vm.permission_key = 'vendors.manage'
WHERE p.permission_key = 'categories.manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = vm.id);
