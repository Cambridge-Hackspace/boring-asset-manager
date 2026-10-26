-- 037_icon_states.sql: the variant each asset state is drawn with (README
-- §3.7), as a JSON object from state to variant name; "" draws none.
-- NULL, as it starts, is bam's defaults.

ALTER TABLE icon_settings ADD COLUMN states TEXT NULL;
