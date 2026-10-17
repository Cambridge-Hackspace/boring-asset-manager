-- 023_spool.sql: the spool, a print queue for labels (README §6.2).
--
-- A category may name the label template its assets, and those of its
-- subcategories that name none, are queued with. A spool entry is one
-- asset's label waiting to print: with a template, a destination (a printer,
-- or to_download for a PDF or ZIP) and how many copies. Printed entries stay,
-- with printed_at set and the names of the template and printer used as they
-- were, for the Printed list. added_by and printed_by name the user or API
-- token who acted. problem holds why its label could not be produced last
-- time, until the entry changes. The spool lock keeps its bound on waiting
-- entries when several people add at once.

ALTER TABLE categories ADD COLUMN label_template_id VARCHAR(36);

CREATE TABLE IF NOT EXISTS spool_entries (
    id VARCHAR(36) PRIMARY KEY,
    asset_id VARCHAR(36) NOT NULL,
    template_id VARCHAR(36),
    printer_id VARCHAR(36),
    to_download BOOLEAN NOT NULL DEFAULT FALSE,
    copies INTEGER NOT NULL DEFAULT 1,
    problem VARCHAR(500) NOT NULL DEFAULT '',
    added_by VARCHAR(36),
    added_at TIMESTAMP NOT NULL,
    printed_at TIMESTAMP NULL,
    printed_by VARCHAR(36),
    printed_template VARCHAR(255) NOT NULL DEFAULT '',
    printed_on VARCHAR(255) NOT NULL DEFAULT '',
    CONSTRAINT fk_spool_entries_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    CONSTRAINT fk_spool_entries_template FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL,
    CONSTRAINT fk_spool_entries_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE SET NULL
);

CREATE INDEX idx_spool_entries_printed ON spool_entries (printed_at, added_at);
CREATE INDEX idx_spool_entries_asset ON spool_entries (asset_id);

INSERT INTO app_locks (name, counter)
SELECT 'spool', 0
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM app_locks WHERE name = 'spool');
