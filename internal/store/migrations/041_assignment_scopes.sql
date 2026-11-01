-- 041_assignment_scopes.sql: a role assigned in some scopes only
-- (docs/access.md). An assignment of a role to a user, or to an API token, may name
-- scopes: the role then grants its asset permissions in those scopes only,
-- and nothing else. An assignment with no rows here is unrestricted, as
-- every assignment was before.

CREATE TABLE IF NOT EXISTS user_role_scopes (
    user_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (user_id, role_id, scope_id),
    CONSTRAINT fk_user_role_scopes_assignment FOREIGN KEY (user_id, role_id) REFERENCES user_roles(user_id, role_id) ON DELETE CASCADE,
    CONSTRAINT fk_user_role_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS api_token_role_scopes (
    token_id VARCHAR(36) NOT NULL,
    role_id VARCHAR(36) NOT NULL,
    scope_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (token_id, role_id, scope_id),
    CONSTRAINT fk_api_token_role_scopes_assignment FOREIGN KEY (token_id, role_id) REFERENCES api_token_roles(token_id, role_id) ON DELETE CASCADE,
    CONSTRAINT fk_api_token_role_scopes_scope FOREIGN KEY (scope_id) REFERENCES scopes(id) ON DELETE CASCADE
);
