-- 086_terms.sql: related terms, which search expands through (#386,
-- docs/data-model.md#related-terms), shaped like W3C SKOS.
--
-- term_imports are batches of terms and links added together, from a file
-- or a thesaurus, so that they can be reviewed and removed as one: name
-- says what was imported.
--
-- terms are things as people call them: label is the preferred label, no
-- two terms' alike once folded (the store checks, as collations differ);
-- alt_labels is a JSON array of the other names it goes by; note says what
-- it means.
--
-- term_links join two terms: broader from the narrower term (term_id) to
-- the broader one (other_id); related once, from the term whose ID sorts
-- first. Search expands a term to everything narrower than it, and
-- suggests related ones.
--
-- Each row records who added it: added_by is a user's or an API token's
-- ID, and added_by_token says which; via names the assistant it was added
-- through over MCP, or is empty; import_id is the import it came in, and
-- removing the import removes it.

CREATE TABLE term_imports (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    added_by VARCHAR(36),
    added_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    via VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE terms (
    id VARCHAR(36) PRIMARY KEY,
    label VARCHAR(255) NOT NULL,
    alt_labels TEXT NOT NULL,
    note TEXT NOT NULL,
    added_by VARCHAR(36),
    added_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    via VARCHAR(255) NOT NULL DEFAULT '',
    import_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_terms_import FOREIGN KEY (import_id) REFERENCES term_imports(id) ON DELETE CASCADE
);

CREATE TABLE term_links (
    term_id VARCHAR(36) NOT NULL,
    other_id VARCHAR(36) NOT NULL,
    relation VARCHAR(16) NOT NULL,
    added_by VARCHAR(36),
    added_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    via VARCHAR(255) NOT NULL DEFAULT '',
    import_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (term_id, other_id, relation),
    CONSTRAINT fk_term_links_term FOREIGN KEY (term_id) REFERENCES terms(id) ON DELETE CASCADE,
    CONSTRAINT fk_term_links_other FOREIGN KEY (other_id) REFERENCES terms(id) ON DELETE CASCADE,
    CONSTRAINT fk_term_links_import FOREIGN KEY (import_id) REFERENCES term_imports(id) ON DELETE CASCADE
);

CREATE INDEX idx_term_links_other ON term_links (other_id);

-- terms.manage adds, changes and removes terms, links and imports. It is
-- not scopable. It goes to every role that manages categories, whose
-- names the terms are about.

INSERT INTO permissions (id, permission_key, description)
SELECT '7b3e9f2a-4c1d-4e8b-9a6f-2d5c8e1b7a94', 'terms.manage', 'Core permission: terms.manage'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'terms.manage');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, tm.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions tm ON tm.permission_key = 'terms.manage'
WHERE p.permission_key = 'categories.manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = tm.id);
