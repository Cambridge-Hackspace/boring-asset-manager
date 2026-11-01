-- 047_icon_renditions.sql: an icon for each rendition (docs/icons.md). A
-- rendition is the form an icon takes for where it is drawn: screen, what
-- every icon was before this, and label. A category has an icon for each,
-- its own source, name, history and param bindings; an asset has its own
-- icon for each. Every icon there is becomes the screen one.
--
-- The tables keyed by category are rebuilt keyed by category and
-- rendition: copied aside, dropped, made again and copied back, which
-- every database does the same way.

CREATE TABLE icon_renditions_icons (
    category_id VARCHAR(36) NOT NULL,
    icon_name VARCHAR(64) NOT NULL,
    source TEXT NULL,
    draft TEXT NULL,
    draft_errors TEXT NULL,
    updated_at TIMESTAMP NOT NULL
);
INSERT INTO icon_renditions_icons (category_id, icon_name, source, draft, draft_errors, updated_at)
    SELECT category_id, icon_name, source, draft, draft_errors, updated_at FROM category_icons;

CREATE TABLE icon_renditions_revisions (
    id VARCHAR(36) NOT NULL,
    category_id VARCHAR(36) NULL,
    library_id VARCHAR(36) NULL,
    source TEXT NOT NULL,
    actor_user_id VARCHAR(36) NULL,
    created_at TIMESTAMP NOT NULL
);
INSERT INTO icon_renditions_revisions (id, category_id, library_id, source, actor_user_id, created_at)
    SELECT id, category_id, library_id, source, actor_user_id, created_at FROM icon_revisions;

CREATE TABLE icon_renditions_params (
    category_id VARCHAR(36) NOT NULL,
    param VARCHAR(64) NOT NULL,
    field_id VARCHAR(36) NOT NULL
);
INSERT INTO icon_renditions_params (category_id, param, field_id)
    SELECT category_id, param, field_id FROM icon_param_bindings;

CREATE TABLE icon_renditions_texts (
    category_id VARCHAR(36) NOT NULL,
    param VARCHAR(64) NOT NULL,
    source VARCHAR(16) NOT NULL,
    field_id VARCHAR(36) NULL
);
INSERT INTO icon_renditions_texts (category_id, param, source, field_id)
    SELECT category_id, param, source, field_id FROM icon_text_bindings;

DROP TABLE icon_text_bindings;
DROP TABLE icon_param_bindings;
DROP TABLE icon_revisions;
DROP TABLE category_icons;

CREATE TABLE category_icons (
    category_id VARCHAR(36) NOT NULL,
    rendition VARCHAR(16) NOT NULL,
    icon_name VARCHAR(64) NOT NULL,
    source TEXT NULL,
    draft TEXT NULL,
    draft_errors TEXT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (category_id, rendition),
    CONSTRAINT uq_category_icons_name UNIQUE (icon_name),
    CONSTRAINT fk_category_icons_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE icon_revisions (
    id VARCHAR(36) PRIMARY KEY,
    category_id VARCHAR(36) NULL,
    rendition VARCHAR(16) NULL,
    library_id VARCHAR(36) NULL,
    source TEXT NOT NULL,
    actor_user_id VARCHAR(36) NULL,
    created_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_icon_revisions_category FOREIGN KEY (category_id, rendition) REFERENCES category_icons(category_id, rendition) ON DELETE CASCADE,
    CONSTRAINT fk_icon_revisions_library FOREIGN KEY (library_id) REFERENCES icon_library(id) ON DELETE CASCADE
);

CREATE INDEX idx_icon_revisions_category ON icon_revisions (category_id, rendition, created_at);
CREATE INDEX idx_icon_revisions_library ON icon_revisions (library_id, created_at);

CREATE TABLE icon_param_bindings (
    category_id VARCHAR(36) NOT NULL,
    rendition VARCHAR(16) NOT NULL,
    param VARCHAR(64) NOT NULL,
    field_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (category_id, rendition, param),
    CONSTRAINT fk_icon_param_bindings_icon FOREIGN KEY (category_id, rendition) REFERENCES category_icons(category_id, rendition) ON DELETE CASCADE,
    CONSTRAINT fk_icon_param_bindings_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);

CREATE TABLE icon_text_bindings (
    category_id VARCHAR(36) NOT NULL,
    rendition VARCHAR(16) NOT NULL,
    param VARCHAR(64) NOT NULL,
    source VARCHAR(16) NOT NULL,
    field_id VARCHAR(36) NULL,
    PRIMARY KEY (category_id, rendition, param),
    CONSTRAINT fk_icon_text_bindings_icon FOREIGN KEY (category_id, rendition) REFERENCES category_icons(category_id, rendition) ON DELETE CASCADE,
    CONSTRAINT fk_icon_text_bindings_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);

INSERT INTO category_icons (category_id, rendition, icon_name, source, draft, draft_errors, updated_at)
    SELECT category_id, 'screen', icon_name, source, draft, draft_errors, updated_at FROM icon_renditions_icons;
INSERT INTO icon_revisions (id, category_id, rendition, library_id, source, actor_user_id, created_at)
    SELECT id, category_id, CASE WHEN category_id IS NULL THEN NULL ELSE 'screen' END, library_id, source, actor_user_id, created_at FROM icon_renditions_revisions;
INSERT INTO icon_param_bindings (category_id, rendition, param, field_id)
    SELECT category_id, 'screen', param, field_id FROM icon_renditions_params;
INSERT INTO icon_text_bindings (category_id, rendition, param, source, field_id)
    SELECT category_id, 'screen', param, source, field_id FROM icon_renditions_texts;

DROP TABLE icon_renditions_texts;
DROP TABLE icon_renditions_params;
DROP TABLE icon_renditions_revisions;
DROP TABLE icon_renditions_icons;

-- an asset's own label icon, beside its own screen icon (icon_name)
ALTER TABLE assets ADD COLUMN label_icon_name VARCHAR(64) NULL;
