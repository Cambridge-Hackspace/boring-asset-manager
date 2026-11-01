-- 045_scan_flows.sql: flows offered on scan pages (docs/scanning.md, docs/flows.md). The
-- built-in Scanned template, if its quick actions are still as it was
-- seeded with, also offers the flows that start from what was scanned, such
-- as Collect it; the quick action shows only when there is one. Templates
-- made or changed here are left as they are.

UPDATE reports
SET spec = REPLACE(spec, '"actions":["open","use","borrow","return","report"]', '"actions":["open","use","borrow","return","report","flows"]')
WHERE id = 'bc4e2a6d-8f0b-4d5c-9e9a-d15c7d9f1a22' AND kind = 'scan';
