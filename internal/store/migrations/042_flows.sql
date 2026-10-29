-- 042_flows.sql: flows (README §6.7, #121), guided pages for doing one
-- everyday thing with assets, such as filing a new one or finishing with
-- one, built from configurable steps.
--
-- A flow's steps are kept as versioned JSON in definition. A run is a
-- person working through a flow, held here between steps so that none can
-- be skipped or replayed. flow_filings records which assets each flow
-- filed, and who filed them, for a Find step to look among. Their
-- permissions are migration 043's.

CREATE TABLE IF NOT EXISTS flows (
    id VARCHAR(36) PRIMARY KEY,
    slug VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    definition TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_by VARCHAR(36) NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT uq_flows_slug UNIQUE (slug)
);

CREATE TABLE IF NOT EXISTS flow_runs (
    id VARCHAR(36) PRIMARY KEY,
    flow_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    state TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    CONSTRAINT fk_flow_runs_flow FOREIGN KEY (flow_id) REFERENCES flows(id) ON DELETE CASCADE,
    CONSTRAINT fk_flow_runs_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS flow_filings (
    flow_id VARCHAR(36) NOT NULL,
    asset_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NULL,
    filed_at TIMESTAMP NOT NULL,
    PRIMARY KEY (flow_id, asset_id),
    CONSTRAINT fk_flow_filings_flow FOREIGN KEY (flow_id) REFERENCES flows(id) ON DELETE CASCADE,
    CONSTRAINT fk_flow_filings_asset FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
);

CREATE INDEX idx_flow_filings_asset ON flow_filings (asset_id);
