-- 064_loan_reminders.sql: loan reminders by email (#210,
-- docs/stock-and-lending.md#loan-reminders).
--
-- loan_reminders are the reminders sent for each loan: "due" before it is
-- due, and "overdue-0", "overdue-1" and so on, the day it becomes overdue
-- and weekly after. Each is claimed by inserting it, so servers sharing the
-- database send it once. They go with their loan.
--
-- users.loan_reminders is whether a borrower gets them: on until they turn
-- it off under Your account.

ALTER TABLE users ADD COLUMN loan_reminders BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE loan_reminders (
    loan_id VARCHAR(36) NOT NULL REFERENCES asset_loans(id) ON DELETE CASCADE,
    reminder VARCHAR(16) NOT NULL,
    sent_at TIMESTAMP NOT NULL,
    PRIMARY KEY (loan_id, reminder)
);
