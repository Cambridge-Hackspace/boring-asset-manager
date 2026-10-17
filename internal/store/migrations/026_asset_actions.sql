-- 026_asset_actions.sql: actions on assets (README §5.4).
--
-- An action is a button people press on an asset, such as Identify, which
-- records an asset.action event for devices and channels to act on: a key,
-- a label, and a question to confirm first, if any. A category offers
-- actions on its assets; with cascades set, on those of its subcategories
-- too.
--
-- assets.act presses them. It goes to every role that reads assets, in the
-- same scopes, except the public role: pressing a button is for members.

CREATE TABLE IF NOT EXISTS asset_actions (
    id VARCHAR(36) PRIMARY KEY,
    action_key VARCHAR(64) NOT NULL,
    label VARCHAR(255) NOT NULL,
    confirm_question VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_asset_actions_key UNIQUE (action_key)
);

CREATE TABLE IF NOT EXISTS category_actions (
    category_id VARCHAR(36) NOT NULL,
    action_id VARCHAR(36) NOT NULL,
    cascades BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (category_id, action_id),
    CONSTRAINT fk_category_actions_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_actions_action FOREIGN KEY (action_id) REFERENCES asset_actions(id) ON DELETE CASCADE
);

INSERT INTO permissions (id, permission_key, description)
SELECT 'b3d5f7a9-1c2e-4d6f-8a0b-c4e6a8b0d226', 'assets.act', 'Core permission: assets.act'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'assets.act');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'assets.act'
WHERE p.permission_key = 'assets.read'
AND rp.role_id <> '9a2c0e4b-6d8f-4b3a-9c7e-bf3a5b7d9e21'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);

INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
SELECT rps.role_id, np.id, rps.scope_id
FROM role_permission_scopes rps
JOIN permissions p ON p.id = rps.permission_id
JOIN permissions np ON np.permission_key = 'assets.act'
WHERE p.permission_key = 'assets.read'
AND rps.role_id <> '9a2c0e4b-6d8f-4b3a-9c7e-bf3a5b7d9e21'
AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id);
