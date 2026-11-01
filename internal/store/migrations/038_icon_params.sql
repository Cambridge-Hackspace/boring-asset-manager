-- 038_icon_params.sql: an icon's params bound to asset fields
-- (docs/icons.md). An asset drawn with a category's icon is drawn with each bound
-- param set to its value of the field, a number, through a wrapper icon
-- each build generates. Deleting the icon or the field drops the binding.

CREATE TABLE IF NOT EXISTS icon_param_bindings (
    category_id VARCHAR(36) NOT NULL,
    param VARCHAR(64) NOT NULL,
    field_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (category_id, param),
    CONSTRAINT fk_icon_param_bindings_icon FOREIGN KEY (category_id) REFERENCES category_icons(category_id) ON DELETE CASCADE,
    CONSTRAINT fk_icon_param_bindings_field FOREIGN KEY (field_id) REFERENCES fields(id) ON DELETE CASCADE
);
