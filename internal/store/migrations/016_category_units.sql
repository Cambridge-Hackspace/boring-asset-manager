-- 016_category_units.sql: weights for things counted by items (README §5.1).
-- A consumable category with a unit totals the stock of its active assets
-- in that unit, rather than counting them, and its threshold is in that
-- unit: two spools of 1000 g and 2000 g make 3000 g. Empty keeps counting
-- items.

ALTER TABLE categories ADD COLUMN stock_unit VARCHAR(32) NOT NULL DEFAULT '';
