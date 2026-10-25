-- 036_asset_icon.sql: an asset's own icon (README §3.7), drawn in place of
-- the one its categories give it: the name of any icon the build draws.
-- NULL, as every asset starts, takes its categories'.

ALTER TABLE assets ADD COLUMN icon_name VARCHAR(64) NULL;
