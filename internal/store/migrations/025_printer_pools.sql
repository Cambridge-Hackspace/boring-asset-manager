-- 025_printer_pools.sql: printer pools (README §6.2).
--
-- A pool is a set of printers loaded alike that share a job's labels. Its
-- jobs are cut into batches of batch_size labels; each member suiting the
-- template takes the next one when it has sent its last, so a faster
-- printer prints more. Members behind a spooler (IPP, Windows) accept a
-- batch at once whatever their speed, so they get a fixed share of the
-- batches, by share, and so does every member when fixed_shares is set.
-- A printer may be in several pools, and still be used on its own.
--
-- Spool entries and jobs may name a pool instead of a printer. A pool
-- job's members are recorded when it is made: those that suit its
-- template, whether they take batches as they free up (pulls), and
-- whether they dropped out of it after failing.

CREATE TABLE IF NOT EXISTS printer_pools (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    batch_size INTEGER NOT NULL DEFAULT 10,
    fixed_shares BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_printer_pools_name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS printer_pool_members (
    pool_id VARCHAR(36) NOT NULL,
    printer_id VARCHAR(36) NOT NULL,
    share INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (pool_id, printer_id),
    CONSTRAINT fk_printer_pool_members_pool FOREIGN KEY (pool_id) REFERENCES printer_pools(id) ON DELETE CASCADE,
    CONSTRAINT fk_printer_pool_members_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE
);

ALTER TABLE spool_entries ADD COLUMN pool_id VARCHAR(36);
ALTER TABLE spool_jobs ADD COLUMN pool_id VARCHAR(36);

CREATE TABLE IF NOT EXISTS spool_job_members (
    job_id VARCHAR(36) NOT NULL,
    printer_id VARCHAR(36) NOT NULL,
    pulls BOOLEAN NOT NULL,
    dropped BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (job_id, printer_id),
    CONSTRAINT fk_spool_job_members_job FOREIGN KEY (job_id) REFERENCES spool_jobs(id) ON DELETE CASCADE,
    CONSTRAINT fk_spool_job_members_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE
);
