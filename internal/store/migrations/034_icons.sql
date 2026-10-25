-- 034_icons.sql: icons written in isolex (README §3.7).
--
-- A category may carry an isolex source that draws its icon, named by
-- icon_name. source is what is published: the latest version that
-- compiled clean with everything else, and so the one every build uses;
-- draft is a later edit that did not compile, kept with its errors until
-- it is fixed or discarded. Library files hold what icons share and no
-- category owns (bases, fragments, axes), published and drafted alike.
--
-- The palette is the lexicon's vocabulary: a build declares exactly its
-- tokens. icon_settings holds the rest of the lexicon block, and
-- icon_build the last build that compiled clean, which is what is drawn.
-- icon_revisions keeps every published version of every source.

CREATE TABLE IF NOT EXISTS category_icons (
    category_id VARCHAR(36) PRIMARY KEY,
    icon_name VARCHAR(64) NOT NULL,
    source TEXT NULL,
    draft TEXT NULL,
    draft_errors TEXT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_category_icons_name UNIQUE (icon_name),
    CONSTRAINT fk_category_icons_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS icon_library (
    id VARCHAR(36) PRIMARY KEY,
    path VARCHAR(255) NOT NULL,
    source TEXT NULL,
    draft TEXT NULL,
    draft_errors TEXT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_icon_library_path UNIQUE (path)
);

CREATE TABLE IF NOT EXISTS icon_revisions (
    id VARCHAR(36) PRIMARY KEY,
    category_id VARCHAR(36) NULL,
    library_id VARCHAR(36) NULL,
    source TEXT NOT NULL,
    actor_user_id VARCHAR(36) NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_icon_revisions_category FOREIGN KEY (category_id) REFERENCES category_icons(category_id) ON DELETE CASCADE,
    CONSTRAINT fk_icon_revisions_library FOREIGN KEY (library_id) REFERENCES icon_library(id) ON DELETE CASCADE
);

CREATE INDEX idx_icon_revisions_category ON icon_revisions (category_id, created_at);
CREATE INDEX idx_icon_revisions_library ON icon_revisions (library_id, created_at);

CREATE TABLE IF NOT EXISTS icon_palette (
    token VARCHAR(64) PRIMARY KEY,
    light VARCHAR(16) NOT NULL,
    dark VARCHAR(16) NOT NULL,
    sort_order INTEGER NOT NULL
);

-- The tokens isolex paints with by default and a few common ones, in
-- bam's zinc, with red for the accent isolex uses for danger.
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('body-top', '#f4f4f5', '#71717a', 0);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('body-left', '#d4d4d8', '#52525b', 1);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('body-right', '#a1a1aa', '#3f3f46', 2);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('outline', '#27272a', '#18181b', 3);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('ink', '#18181b', '#f4f4f5', 4);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('ink-muted', '#71717a', '#a1a1aa', 5);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('shadow', '#000000', '#000000', 6);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('virt', '#e4e4e7', '#3f3f46', 7);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('accent', '#dc2626', '#f87171', 8);
INSERT INTO icon_palette (token, light, dark, sort_order) VALUES ('accent-dim', '#fca5a5', '#7f1d1d', 9);

CREATE TABLE IF NOT EXISTS icon_settings (
    id VARCHAR(16) PRIMARY KEY,
    stroke_widths TEXT NOT NULL,
    reserved TEXT NOT NULL,
    limits TEXT NOT NULL
);

INSERT INTO icon_settings (id, stroke_widths, reserved, limits) VALUES ('icons', '[]', '[]', '{}');

CREATE TABLE IF NOT EXISTS icon_build (
    id VARCHAR(16) PRIMARY KEY,
    source_hash VARCHAR(64) NOT NULL,
    isolex_version VARCHAR(32) NOT NULL,
    ir TEXT NOT NULL,
    warnings TEXT NOT NULL,
    built_at TIMESTAMP NOT NULL
);

-- Builds and the changes that cause them run one at a time.
INSERT INTO app_locks (name, counter) VALUES ('icons', 0);
