-- 024_spool_jobs.sql: printing from the spool (README §6.2).
--
-- Printing the spool makes a job for each printer and template: its
-- labels, as the template and printer were named then, its state and who
-- asked. A job's labels go out in batches, which printers take one at a
-- time; a job on one printer is one batch. A batch being sent beats
-- heartbeat_at, so one left sending by a server that stopped is found and
-- its labels put back. Downloads are recorded as jobs too, to send again.
-- Entries name the batch they were last in.

CREATE TABLE IF NOT EXISTS spool_jobs (
    id VARCHAR(36) PRIMARY KEY,
    template_id VARCHAR(36),
    template_name VARCHAR(255) NOT NULL,
    printer_id VARCHAR(36),
    target_name VARCHAR(255) NOT NULL,
    to_download BOOLEAN NOT NULL DEFAULT FALSE,
    state VARCHAR(16) NOT NULL,
    error VARCHAR(500) NOT NULL DEFAULT '',
    labels INTEGER NOT NULL,
    created_by VARCHAR(36),
    created_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    CONSTRAINT fk_spool_jobs_template FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL,
    CONSTRAINT fk_spool_jobs_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE SET NULL
);

CREATE INDEX idx_spool_jobs_created ON spool_jobs (created_at);

CREATE TABLE IF NOT EXISTS spool_batches (
    id VARCHAR(36) PRIMARY KEY,
    job_id VARCHAR(36) NOT NULL,
    seq INTEGER NOT NULL,
    printer_id VARCHAR(36),
    printer_name VARCHAR(255) NOT NULL DEFAULT '',
    state VARCHAR(16) NOT NULL,
    error VARCHAR(500) NOT NULL DEFAULT '',
    labels INTEGER NOT NULL,
    heartbeat_at TIMESTAMP NULL,
    started_at TIMESTAMP NULL,
    finished_at TIMESTAMP NULL,
    CONSTRAINT fk_spool_batches_job FOREIGN KEY (job_id) REFERENCES spool_jobs(id) ON DELETE CASCADE,
    CONSTRAINT fk_spool_batches_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE SET NULL
);

CREATE INDEX idx_spool_batches_state ON spool_batches (state, printer_id);
CREATE INDEX idx_spool_batches_job ON spool_batches (job_id, seq);

ALTER TABLE spool_entries ADD COLUMN batch_id VARCHAR(36);
CREATE INDEX idx_spool_entries_batch ON spool_entries (batch_id);
