-- 044_flow_overdue.sql: a date to collect by (README §6.7). A flow may name
-- a date field as its "leave until"; what it filed is announced once when
-- that date has passed, as asset.flow_overdue, and overdue_notified_at
-- records that it was, until the date is moved on.

ALTER TABLE flow_filings ADD COLUMN overdue_notified_at TIMESTAMP NULL;
