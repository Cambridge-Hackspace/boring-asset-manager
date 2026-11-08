-- 075_category_implies.sql: categories that imply other categories, so
-- stock kept in several places pools (#280, docs/stock-and-lending.md#pools).
-- 073 is #219's and 074 #263's.
--
-- A row says the members of category_id are also members of implied_id:
-- its assets, and those of the categories below it, count in implied_id's
-- totals, filters and reports, as if they were in it. Nothing else comes
-- through: no scopes, fields, templates or suggested link types. Membership
-- through a link is worked out when it is needed, never stored, and a link
-- that would lead back to where it starts is refused. Each goes with
-- either category.

CREATE TABLE category_implies (
    category_id VARCHAR(36) NOT NULL,
    implied_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (category_id, implied_id),
    CONSTRAINT fk_category_implies_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE,
    CONSTRAINT fk_category_implies_implied FOREIGN KEY (implied_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE INDEX idx_category_implies_implied ON category_implies (implied_id);
