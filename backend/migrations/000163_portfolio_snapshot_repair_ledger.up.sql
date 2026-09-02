-- #1569 Task 11 (S6/E4): the ledger that records what the portfolio repair did, and what it
-- REFUSED to do.
--
-- WHY A TABLE AND NOT AN audit_logs ROW
-- The repair rewrites recorded financial history, so "which days did you touch?" has to be
-- answerable months later. audit_logs stores exactly (action, resource_type, resource_id) with a
-- deliberately NULL user_id (#1506/#1507 deadlock class) — it has nowhere to put the before/after
-- values, and no way at all to record a REFUSAL or its reason. Half of what an operator needs to
-- audit this repair is the list of days it declined to touch and why, so the trail has to be able
-- to say "examined, refused, no coverage". That is this table.
--
-- WHY REFUSALS ARE ROWS AND NOT SILENCE
-- The single binding constraint on this repair is that a day with no holding_snapshots rows is
-- LEFT ALONE and never zeroed (task 10, ruling R36): a day can lack rows because it predates the
-- history tables, or because a partial sync skipped the account and the daily sweep never ran. A
-- zero written there is the same corruption the repair exists to remove, wearing a different hat.
-- Recording the refusal makes "left alone" an observable decision instead of an absence.
--
-- IDEMPOTENCE
-- One row per (user_id, snapshot_date) examined. The repair skips days already in this table, so
-- a second run is a no-op — and because the source rows for a past day are immutable, a forced
-- re-examination recomputes the same numbers and writes nothing.

CREATE TABLE IF NOT EXISTS portfolio_snapshot_repairs (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    snapshot_date               DATE NOT NULL,

    -- repaired            — the day had full coverage and the stored aggregate disagreed; rewritten.
    -- verified            — full coverage and the stored aggregate already matched; nothing written.
    -- refused_no_coverage — state (a): no holding_snapshots rows at all for that day.
    -- refused_partial_coverage — state (b): rows for some live holdings-bearing accounts, not all.
    outcome                     VARCHAR(32) NOT NULL,
    reason                      TEXT NOT NULL,

    -- The coverage arithmetic behind the outcome, so a refusal can be re-derived without rerunning.
    expected_account_count      INTEGER NOT NULL,
    observed_account_count      INTEGER NOT NULL,

    -- What the row said before. Always recorded, including for refusals — this is the only place
    -- the pre-repair value survives, since the repair overwrites in place.
    previous_total_value        NUMERIC(18,2) NOT NULL,
    previous_total_cost_basis   NUMERIC(18,2) NOT NULL,
    previous_total_gain_loss    NUMERIC(18,2) NOT NULL,
    previous_total_gain_loss_pct NUMERIC(10,4),

    -- The ACCEPTED reconstruction. NULL on every refusal, where no reconstruction was accepted —
    -- repeating the previous value there would read like a write that never happened. On a
    -- 'verified' row these equal the previous_* columns: that equality IS the evidence the day
    -- already agreed with its history, and nothing was written (not even updated_at).
    repaired_total_value        NUMERIC(18,2),
    repaired_total_cost_basis   NUMERIC(18,2),
    repaired_total_gain_loss    NUMERIC(18,2),
    repaired_total_gain_loss_pct NUMERIC(10,4),

    examined_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- squawk-ignore prefer-robust-stmts
ALTER TABLE portfolio_snapshot_repairs
    DROP CONSTRAINT IF EXISTS chk_portfolio_snapshot_repairs_outcome;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE portfolio_snapshot_repairs
    ADD CONSTRAINT chk_portfolio_snapshot_repairs_outcome
    CHECK (outcome IN ('repaired', 'verified', 'refused_no_coverage', 'refused_partial_coverage'));

-- One examination per (user, day): this is what makes the repair skippable and therefore
-- idempotent, and it is also what stops a re-run from appending a second, contradictory verdict.
CREATE UNIQUE INDEX IF NOT EXISTS uq_portfolio_snapshot_repairs_user_date
    ON portfolio_snapshot_repairs (user_id, snapshot_date);

-- "Which days did the repair actually rewrite?" is the first question anyone asks this table, and
-- without an index it is a seq scan over a row per user per day. snapshot_date is the second column
-- so the same index serves "what did it repair, and when".
-- squawk-ignore require-concurrent-index-creation
-- Not CONCURRENTLY: the table is CREATEd in this same migration and is therefore guaranteed empty
-- and unreferenced here, so there is no lock to avoid and CONCURRENTLY cannot run in a transaction.
CREATE INDEX IF NOT EXISTS idx_portfolio_snapshot_repairs_outcome
    ON portfolio_snapshot_repairs (outcome, snapshot_date);

COMMENT ON TABLE portfolio_snapshot_repairs IS
    'One row per portfolio_snapshots day the #1569 repair EXAMINED — repaired, verified, or '
    'refused. Refusals are recorded deliberately: a day with no holding_snapshots coverage must be '
    'left alone rather than zeroed, and this table is where that decision is observable.';

COMMENT ON COLUMN portfolio_snapshot_repairs.expected_account_count IS
    'Accounts recorded as live on this day (account_status_history overlap) that are known to bear '
    'holdings — evidenced by any holding_snapshots row on any day, or by a current holdings row. '
    'Current holdings are used ONLY to identify the account set, never to value anything: there is '
    'no price-history table, so valuing a past day from current holdings would overwrite recorded '
    'history with today''s prices.';

COMMENT ON COLUMN portfolio_snapshot_repairs.observed_account_count IS
    'Live accounts with at least one holding_snapshots row for this day, counted WITHOUT filtering '
    'on present. Presence of a row means the capture observed the account that day; present tells '
    'you what it held. A day whose rows are all present = false is a confirmed-empty day and is '
    'repairable to zero — which is not the same as a day with no rows at all.';
