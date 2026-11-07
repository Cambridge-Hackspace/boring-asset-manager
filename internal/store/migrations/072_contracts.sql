-- 072_contracts.sql: contracts with vendors, such as warranties, service and
-- maintenance agreements, leases and licenses (#221, docs/contracts.md).
-- 071 is #224's.
--
-- A contract is with a vendor (068), whose scopes decide who sees it.
-- Deleting a vendor that has contracts is refused; the cascade is for
-- what replaces vendors wholesale, such as restoring a backup.
--
--   - kind is warranty, service, maintenance, lease, license or other;
--   - reference is the vendor's reference for it, such as an agreement
--     number;
--   - start_date and end_date are dates, written YYYY-MM-DD so they compare
--     and sort as text on every database; one with no end runs until it is
--     ended;
--   - auto_renews says whether it renews by itself unless notice is given,
--     notice_days days before it ends (0 for none);
--   - cost_minor is its cost in the currency's minor units, exactly, as
--     asset_purchases' price_minor is (070), and currency its ISO 4217
--     code; billing says how often it is paid: once, monthly, quarterly,
--     yearly, or empty when not said;
--   - notes are Markdown.

CREATE TABLE contracts (
    id VARCHAR(36) PRIMARY KEY,
    vendor_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    name VARCHAR(255) NOT NULL,
    reference VARCHAR(255) NOT NULL,
    start_date VARCHAR(10),
    end_date VARCHAR(10),
    auto_renews BOOLEAN NOT NULL,
    notice_days INTEGER NOT NULL,
    cost_minor BIGINT,
    currency VARCHAR(3) NOT NULL,
    billing VARCHAR(16) NOT NULL,
    notes TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_contracts_vendor FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE CASCADE
);

CREATE INDEX idx_contracts_vendor ON contracts (vendor_id);
CREATE INDEX idx_contracts_end ON contracts (end_date);

-- What a contract covers: assets it names, and every asset in the
-- categories it names or below them. Each goes with what it names.

CREATE TABLE contract_categories (
    contract_id VARCHAR(36) NOT NULL,
    category_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (contract_id, category_id),
    CONSTRAINT fk_contract_categories_contract FOREIGN KEY (contract_id) REFERENCES contracts(id) ON DELETE CASCADE,
    CONSTRAINT fk_contract_categories_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE INDEX idx_contract_categories_category ON contract_categories (category_id);

CREATE TABLE contract_assets (
    contract_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (contract_id, asset_id),
    CONSTRAINT fk_contract_assets_contract FOREIGN KEY (contract_id) REFERENCES contracts(id) ON DELETE CASCADE,
    CONSTRAINT fk_contract_assets_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_contract_assets_asset ON contract_assets (asset_id);

-- A contract's files, such as the contract itself, are kept as vendors'
-- are (068), in a table of their own, under storage.local_path/photos/.

CREATE TABLE contract_attachments (
    id VARCHAR(36) PRIMARY KEY,
    contract_id VARCHAR(36) NOT NULL,
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
    CONSTRAINT fk_contract_attachments_contract FOREIGN KEY (contract_id) REFERENCES contracts(id) ON DELETE CASCADE
);

CREATE INDEX idx_contract_attachments_contract ON contract_attachments (contract_id, position);

-- contract_notices are the contract.expiring and contract.expired events
-- recorded, one for each contract, kind (expiring or expired) and the date
-- it is about: the end, or for one that renews automatically, the deadline
-- for giving notice. Each is claimed by inserting it, so servers sharing
-- the database record it once, as warranty_notices are (070); new dates
-- are new notices. They go with their contract.

CREATE TABLE contract_notices (
    contract_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    due_date VARCHAR(10) NOT NULL,
    sent_at TIMESTAMP NOT NULL,
    PRIMARY KEY (contract_id, kind, due_date),
    CONSTRAINT fk_contract_notices_contract FOREIGN KEY (contract_id) REFERENCES contracts(id) ON DELETE CASCADE
);

-- Events about a contract name it in contract_id, which, like vendor_id,
-- is deliberately not a foreign key, so its history reads as it was after
-- it is deleted.

ALTER TABLE events ADD COLUMN contract_id VARCHAR(36);

CREATE INDEX idx_events_contract ON events (contract_id, created_at);
