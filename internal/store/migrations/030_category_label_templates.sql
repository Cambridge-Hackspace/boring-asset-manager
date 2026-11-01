-- 030_category_label_templates.sql: several label templates per category,
-- by preference (docs/printing.md).
--
-- A category names its label templates in order; its assets' labels take
-- the first that suits the printer they go to. The one template a category
-- named before becomes its first, and the column that held it goes.
--
-- A spool entry remembers whether its template came from its categories,
-- so that when a printer's roll changes, those labels can move to another
-- of their categories' templates; a template chosen by hand stays.

CREATE TABLE IF NOT EXISTS category_label_templates (
    category_id VARCHAR(36) NOT NULL,
    template_id VARCHAR(64) NOT NULL,
    sort_order INTEGER NOT NULL,
    PRIMARY KEY (category_id, template_id),
    CONSTRAINT fk_category_label_templates_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_label_templates_template FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE CASCADE
);

INSERT INTO category_label_templates (category_id, template_id, sort_order)
SELECT id, label_template_id, 0 FROM categories WHERE label_template_id IS NOT NULL AND label_template_id <> '';

ALTER TABLE categories DROP COLUMN label_template_id;

ALTER TABLE spool_entries ADD COLUMN template_from_category BOOLEAN NOT NULL DEFAULT FALSE;
