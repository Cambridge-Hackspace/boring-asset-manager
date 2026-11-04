-- 056_user_favorites.sql: what each person stars to find again (#225,
-- docs/favorites.md). kind is asset, category, report or flow, and
-- object_id the ID of one; there is no foreign key to it, as the kinds live
-- in different tables. A favorite of something the person cannot see, or
-- that is gone, is left alone and not listed.

CREATE TABLE user_favorites (
    user_id VARCHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    object_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, kind, object_id),
    CONSTRAINT fk_user_favorites_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
