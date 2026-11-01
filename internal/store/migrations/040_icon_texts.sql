-- 040_icon_texts.sql: an asset's own text on its icon (docs/icons.md). A
-- category's icon may bind a text param to the asset's name, its first
-- code, or a field; an asset drawn with it is compiled with that text,
-- one icon at a time, from the files the current build was made from,
-- which the build now keeps. Deleting the icon or the field drops the
-- binding.

CREATE TABLE IF NOT EXISTS icon_text_bindings (
    category_id VARCHAR(36) NOT NULL,
    param VARCHAR(64) NOT NULL,
    source VARCHAR(16) NOT NULL,
    field_id VARCHAR(36) NULL,
    PRIMARY KEY (category_id, param),
    CONSTRAINT fk_icon_text_bindings_icon FOREIGN KEY (category_id) REFERENCES category_icons(category_id) ON DELETE CASCADE,
    CONSTRAINT fk_icon_text_bindings_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);

ALTER TABLE icon_build ADD COLUMN inputs TEXT NULL;
