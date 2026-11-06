-- 069_comments.sql: comments in Markdown on assets, categories, loans and
-- flows' filings (#223, docs/comments.md).
--
-- Each kind of thing has a table of its own, as attachments do (067), so
-- that each goes with its owner's kind of information in backups: an
-- asset's and a loan's with the assets, a category's with the catalog, and
-- a filing's with the flows. Each is deleted with what it is on.
--
-- body is Markdown, rendered to HTML when read. author_id is the user or
-- API token who wrote it, and deleted_by who deleted it; like added_by,
-- neither is a foreign key, so a comment outlives its author's account. An
-- edited comment has edited_at. A deleted one keeps its row, with an empty
-- body, deleted_at and deleted_by, so that "deleted by" shows in its place.
-- Every version's text is kept in the events of what it is on.

CREATE TABLE asset_comments (
    id VARCHAR(36) PRIMARY KEY,
    asset_id VARCHAR(36) NOT NULL,
    body TEXT NOT NULL,
    author_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    edited_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    deleted_by VARCHAR(36),
    CONSTRAINT fk_asset_comments_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_comments_asset ON asset_comments (asset_id, created_at);

CREATE TABLE category_comments (
    id VARCHAR(36) PRIMARY KEY,
    category_id VARCHAR(36) NOT NULL,
    body TEXT NOT NULL,
    author_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    edited_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    deleted_by VARCHAR(36),
    CONSTRAINT fk_category_comments_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE INDEX idx_category_comments_category ON category_comments (category_id, created_at);

CREATE TABLE loan_comments (
    id VARCHAR(36) PRIMARY KEY,
    loan_id VARCHAR(36) NOT NULL,
    body TEXT NOT NULL,
    author_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    edited_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    deleted_by VARCHAR(36),
    CONSTRAINT fk_loan_comments_loan FOREIGN KEY (loan_id) REFERENCES asset_loans(id) ON DELETE CASCADE
);

CREATE INDEX idx_loan_comments_loan ON loan_comments (loan_id, created_at);

CREATE TABLE flow_filing_comments (
    id VARCHAR(36) PRIMARY KEY,
    flow_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    body TEXT NOT NULL,
    author_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    edited_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    deleted_by VARCHAR(36),
    CONSTRAINT fk_flow_filing_comments_filing FOREIGN KEY (flow_id, asset_id) REFERENCES flow_filings(flow_id, asset_id) ON DELETE CASCADE
);

CREATE INDEX idx_flow_filing_comments_filing ON flow_filing_comments (flow_id, asset_id, created_at);

-- comments.write writes comments, and comments.moderate deletes anyone's;
-- both are scopable. Every role that writes assets is granted
-- comments.write, and every role that deletes them comments.moderate, in
-- the same scopes, skipping what a database seeded before migrating
-- already has (as in migration 043).

INSERT INTO permissions (id, permission_key, description)
SELECT 'f6493cbb-92bc-4199-a848-2896ff633c67', 'comments.write', 'Core permission: comments.write'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'comments.write');

INSERT INTO permissions (id, permission_key, description)
SELECT '26b58412-936a-4556-b700-05a8fc749e65', 'comments.moderate', 'Core permission: comments.moderate'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'comments.moderate');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, cw.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions cw ON cw.permission_key = 'comments.write'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = cw.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, cw.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions cw ON cw.permission_key = 'comments.write'
WHERE p.permission_key = 'assets.write'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = cw.id AND held.scope_id = rps.scope_id);

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, cm.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions cm ON cm.permission_key = 'comments.moderate'
WHERE p.permission_key = 'assets.delete'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = cm.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, cm.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions cm ON cm.permission_key = 'comments.moderate'
WHERE p.permission_key = 'assets.delete'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = cm.id AND held.scope_id = rps.scope_id);
