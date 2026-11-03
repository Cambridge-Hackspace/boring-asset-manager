-- 053_hook_triggers.sql: hooks fired by events and schedules as well as
-- buttons (#194, docs/hooks.md).
--
-- asset_actions.triggers is a JSON list of triggers; NULL is a button
-- alone, as every hook was before. The rest is the state that lets several
-- servers sharing the database fire each trigger once: the events already
-- looked at (each claimed by inserting it), when each hook last fired for
-- each asset (for an event trigger's least interval), and when each
-- schedule last ran (claimed by changing it from what it was).

ALTER TABLE asset_actions ADD COLUMN triggers TEXT NULL;

CREATE TABLE hook_trigger_seen (
    event_id VARCHAR(36) PRIMARY KEY,
    seen_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_hook_trigger_seen_at ON hook_trigger_seen (seen_at);

CREATE TABLE hook_trigger_fired (
    hook_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    PRIMARY KEY (hook_id, asset_id),
    CONSTRAINT fk_hook_trigger_fired_hook FOREIGN KEY (hook_id) REFERENCES asset_actions(id) ON DELETE CASCADE
);

CREATE TABLE hook_schedule_runs (
    hook_id VARCHAR(36) NOT NULL,
    trigger_index INTEGER NOT NULL,
    ran_at TIMESTAMP NOT NULL,
    PRIMARY KEY (hook_id, trigger_index),
    CONSTRAINT fk_hook_schedule_runs_hook FOREIGN KEY (hook_id) REFERENCES asset_actions(id) ON DELETE CASCADE
);
