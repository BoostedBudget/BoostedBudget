-- Rollback #1631 M8a fix round 2 (I-1).
--
-- Dropping this re-overloads currency_code's NULL, so GetPortfolioSummary again cannot tell a
-- mixed prior snapshot from an unlabelled one and will again compute an unflagged cross-currency
-- day change. That is the pre-000198 behaviour this migration reverses, and it is the accepted
-- cost of a rollback; the column cannot be reconstructed afterwards except by a repair pass over
-- holding_snapshots.

ALTER TABLE portfolio_snapshots DROP COLUMN IF EXISTS is_mixed_currency;
