-- 085_search_direct.sql: whether a search that finds exactly one asset
-- opens it straight away, on its scan page where a scan template applies
-- (#389, docs/data-model.md#search). users.search_direct is the person's
-- own choice, or NULL to follow the instance's setting (search.direct in
-- settings), as everyone does until they choose.

ALTER TABLE users ADD COLUMN search_direct BOOLEAN NULL;
