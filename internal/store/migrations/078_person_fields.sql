-- 078_person_fields.sql: person fields, which name people, and people
-- without accounts (#262, docs/data-model.md#person-fields).
--
-- A field of validation type person names one person, or several, by
-- user ID: asset_field_values.value holds their IDs, separated by commas,
-- so a renamed person's fields follow them. The type needs no column of
-- its own; fields.validation_type holds it.
--
-- asset_field_people indexes those values, one row for each person a
-- field of an asset names, so assets can be found by who is in a field.
-- It is kept with asset_field_values, and goes with its asset, its field
-- and its person.
--
-- users.can_sign_in is false for a person without an account: a name to
-- put in fields, with an email or none, no password, no identity and no
-- roles. Signing in through an identity provider that links by verified
-- email joins them to that sign-in, and sets it.
--
-- people.add adds people without accounts, and is not scopable. Every
-- role holding users.manage is granted it, as users.manage includes it.
-- What a database seeded before migrating already has is skipped (as in
-- migration 070).

ALTER TABLE users ADD COLUMN can_sign_in BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE asset_field_people (
    asset_id VARCHAR(36) NOT NULL,
    field_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (asset_id, field_id, user_id),
    CONSTRAINT fk_asset_field_people_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_field_people_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE,
    CONSTRAINT fk_asset_field_people_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_asset_field_people_user ON asset_field_people (user_id, field_id);

INSERT INTO permissions (id, permission_key, description)
SELECT 'b4d2e8a1-7c3f-4e5a-9b6d-1f0e2c4a8d37', 'people.add', 'Core permission: people.add'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'people.add');

INSERT INTO role_permissions (role_id, permission_id)
SELECT rp.role_id, np.id
FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
JOIN permissions np ON np.permission_key = 'people.add'
WHERE p.permission_key = 'users.manage'
AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id);
