-- 083_lasers.sql: one-shot printers, lasers that engrave a label onto a
-- workpiece and cut along its outline (#242, docs/printing.md#lasers).
-- 081 and 082 are other branches'.
--
-- laser_materials are a one-shot printer's materials as its admin has
-- changed them: a driver's preset tuned, under the preset's ID, or a
-- material of the printer's own. A preset without a row is as the driver
-- has it, and deleting a tuned preset's row puts it back.
--
--   - engrave_power and cut_power are percentages of the laser's power,
--     engrave_speed and cut_speed millimeters a second, and engrave_lpi
--     the lines an inch a label is engraved at;
--   - cut_power, cut_speed and cut_passes are NULL for a material the
--     printer does not cut;
--   - from_back mirrors its jobs, for a material engraved from the back.

CREATE TABLE laser_materials (
    printer_id VARCHAR(36) NOT NULL,
    material_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    engrave_power DOUBLE PRECISION NOT NULL,
    engrave_speed DOUBLE PRECISION NOT NULL,
    engrave_passes INTEGER NOT NULL,
    engrave_lpi INTEGER NOT NULL,
    cut_power DOUBLE PRECISION,
    cut_speed DOUBLE PRECISION,
    cut_passes INTEGER,
    from_back BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (printer_id, material_id),
    CONSTRAINT fk_laser_materials_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE CASCADE
);

-- laser_jobs are the history of a one-shot printer's jobs: who prepared and
-- who started each, what it engraved, the material and its settings as
-- they were, and how it ended. The names are as they were, so the history
-- outlives the printer, template and asset.
--
--   - material is the material as the job used it, as JSON, power and
--     speed among it; placement is where the workpiece lay on the bed and
--     where the label went on it, in millimeters, whether it was mirrored,
--     and how many outlines it cut, as JSON;
--   - state is prepared, ready (sent to a machine started with its own
--     button), running, paused, done, cancelled, failed, or discarded (a
--     prepared job never started, which another took the place of);
--   - prepared_by and started_by are a user's or an API token's ID, and
--     their _token columns say which.

CREATE TABLE laser_jobs (
    id VARCHAR(36) PRIMARY KEY,
    printer_id VARCHAR(36),
    printer_name VARCHAR(255) NOT NULL,
    template_id VARCHAR(36),
    template_name VARCHAR(255) NOT NULL,
    asset_id VARCHAR(36),
    asset_name VARCHAR(255) NOT NULL DEFAULT '',
    material TEXT NOT NULL,
    placement TEXT NOT NULL,
    state VARCHAR(16) NOT NULL,
    message TEXT,
    prepared_by VARCHAR(36),
    prepared_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    started_by VARCHAR(36),
    started_by_token BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    CONSTRAINT fk_laser_jobs_printer FOREIGN KEY (printer_id) REFERENCES printers(id) ON DELETE SET NULL,
    CONSTRAINT fk_laser_jobs_template FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL,
    CONSTRAINT fk_laser_jobs_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE SET NULL
);

CREATE INDEX idx_laser_jobs_printer ON laser_jobs (printer_id, created_at);
CREATE INDEX idx_laser_jobs_asset ON laser_jobs (asset_id);

-- lasers.run starts a laser's job: BAM!'s Start, for a machine without a
-- button of its own. It is not scopable. It goes to the admin role, which
-- is granted every permission when roles are seeded.

INSERT INTO permissions (id, permission_key, description)
SELECT '7b2d4f6a-8c1e-4a3b-9d5f-2e6c8a0b4d17', 'lasers.run', 'Core permission: lasers.run'
FROM (SELECT 1 AS one) AS seed
WHERE NOT EXISTS (SELECT 1 FROM permissions WHERE permission_key = 'lasers.run');
