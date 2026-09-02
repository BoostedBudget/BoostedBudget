-- #1569 Task 10, fix round 1: give the history tables a way to say "and nothing else".
--
-- WHAT WAS WRONG
-- 000160's capture is an INSERT ... SELECT off the current-state table. That is a POSITIVE-ONLY
-- record: it can say "this position existed", and it has no way to say "this position no longer
-- does". Two measured consequences, both of which make a reconstruction OVER-report — the
-- dangerous direction, because an over-report looks like a complete answer:
--
--   1. A confirmed-complete liquidation clears the account's holdings, so the capture selects zero
--      rows and writes nothing. An earlier capture the SAME day therefore remains the account's
--      most recent record and is still read as "the day's positions". Measured: live holdings
--      totalled 0 while the day's history still totalled 5000.00. It never self-heals — the daily
--      sweep also selects zero rows.
--   2. The same applies to a liability that Plaid confirms is gone: liability_statements keeps
--      today's row saying it exists.
--
-- THE FIX: `present`. A capture now does two things instead of one — it marks every row it did
-- NOT re-observe for that (account, UTC day) as present = false, and then upserts the rows it did
-- observe as present = true. Reconstruction reads present rows only.
--
-- Nothing is deleted. This is deliberate and is what keeps the table append-only: a row marked
-- absent still carries the values, the timestamps and (for credit) the APR schedule that were last
-- known — which matters, because a wiped APR schedule is the specific loss this whole task exists
-- to prevent. `present = false` records that the thing stopped being reported, not that it never
-- happened.
--
-- DEFAULT TRUE, so every row 000160's capture already wrote keeps meaning exactly what it meant.

ALTER TABLE holding_snapshots
    ADD COLUMN IF NOT EXISTS present BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN holding_snapshots.present IS
    'FALSE once a capture for this (account, as_of_date) no longer observed this security — a '
    'position sold between two same-day captures, or an account whose holdings a '
    'confirmed-complete Plaid response cleared. Reconstruction of a day MUST filter on present; '
    'the row is retained rather than deleted so the last known values survive.';

ALTER TABLE liability_statements
    ADD COLUMN IF NOT EXISTS present BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN liability_statements.present IS
    'FALSE once liability_sync_service.go''s delete-on-absence path cleared this liability on this '
    'UTC day. The row keeps its details — including the credit APR schedule — so the clear records '
    'that the liability stopped being reported without erasing what it was.';

-- The reconstruction read is (user_id, as_of_date) filtered to present rows; the partial index
-- keeps absent rows out of it entirely. Named separately from 000160's full index, which
-- GetByAccountDateRange (the history browser, which deliberately returns absent rows too) uses.
CREATE INDEX IF NOT EXISTS idx_holding_snapshots_user_date_present
    ON holding_snapshots (user_id, as_of_date)
    WHERE present;
