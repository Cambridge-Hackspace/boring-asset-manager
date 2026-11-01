-- 019_scopes.sql: scoped permissions (docs/access.md).
--
-- Permission keys become resource.action (assets.read), so a role can hold
-- one in a scope as scope:resource.action (tools:assets.read). A scope is a
-- label on categories, passed down to their subcategories; an asset is in
-- the scopes of its categories and their ancestors, or, when they have
-- none, in the reserved default scope. A role's permissions in every scope
-- stay in role_permissions; those it holds in some scopes only are in
-- role_permission_scopes.

-- The description names the key, so it is renamed first: MariaDB applies
-- assignments in order, and the key must still be the old one.
UPDATE permissions
SET description = REPLACE(description, permission_key, REPLACE(permission_key, ':', '.')),
    permission_key = REPLACE(permission_key, ':', '.')
WHERE permission_key LIKE '%:%';

CREATE TABLE IF NOT EXISTS scopes (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_scopes_name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS category_scopes (
    category_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (category_id, scope_id),
    CONSTRAINT fk_category_scopes_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS role_permission_scopes (
    role_id VARCHAR(36) NOT NULL,
    permission_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (role_id, permission_id, scope_id),
    CONSTRAINT fk_role_permission_scopes_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    CONSTRAINT fk_role_permission_scopes_permission FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE,
    CONSTRAINT fk_role_permission_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);

INSERT INTO scopes (id, name, created_at)
SELECT '6d9f7b1e-3a5c-4e0d-8f4b-8c0d2e4a6b19', 'default', CURRENT_TIMESTAMP
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM scopes WHERE name = 'default');
