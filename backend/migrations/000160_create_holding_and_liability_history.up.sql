-- #1569 Task 10 (T6): append-only history for investment holdings and liabilities.
--
-- WHY THESE TABLES EXIST
-- Both current-state tables are destroyed and rewritten on every sync:
--   * holdings              — holdingRepository.ReplaceForAccount DELETEs then re-INSERTs per account.
--   * credit_liabilities /
--     mortgage_liabilities /
--     student_loan_liabilities — replaced per account, and DELETEd outright when Plaid stops
--     reporting that category (the "delete-on-absence" path).
-- So the *current* value of a position or an APR schedule is all that survives a sync. Change
-- institutions and the portfolio's and the loans' past simply does not exist any more.
--
-- WHY NOT SOFT-DELETE (deliberate, ruled on twice — see issue #1569)
-- Adding deleted_at to `holdings` would require a filter on every SUM(h.institution_value); one
-- miss double-counts a portfolio. Worse, portfolio_snapshot_repository.go's Upsert explicitly
-- depends on "under retention the holdings rows still exist" when it RE-computes a PAST day's
-- total. Moving holdings into a history table with a wiped current state would send those
-- recomputes to zero and reintroduce the exact bug that fix closed.
--
-- Therefore: current-state replace semantics are UNCHANGED. These two tables are written *in
-- addition to* the replace, inside the same transaction, never instead of it.
--
-- GRAIN / IDENTITY CONTRACT (consumed by task 11, which rebuilds portfolio_snapshots from
-- holding_snapshots rather than from current prices)
--   holding_snapshots:    one row per (account_id, security_id, as_of_date) UTC calendar day.
--   liability_statements: one row per (account_id, liability_type, as_of_date) UTC calendar day.
-- A second capture on the same UTC day UPDATEs that day's row rather than appending a new one, so
-- the table grows with elapsed days, not with sync frequency. No row is ever DELETEd by a sync:
-- "append-only" here means a past day is immutable, which is the property the thesis needs.
--
-- ⚠ SUPERSEDED BY MIGRATIONS 000161 AND 000162 — the two paragraphs this replaces taught two rules that are
-- now wrong, and both were wrong in the dangerous direction (they made a reconstruction
-- OVER-report, which looks like a complete answer). Corrected here rather than left standing,
-- because a stale instruction in the file a task-11 reader opens first is worse than no
-- instruction. 000162 re-issues the corrected COMMENT ON COLUMN into the database itself, since
-- editing this file does nothing for a database already migrated past it.
--
-- WRONG #1 — "filter on MAX(captured_at) to get the day's final position set." An INSERT ... SELECT
-- writes nothing when it observes nothing, so a confirmed-complete liquidation left the MORNING's
-- capture holding the day's maximum captured_at and being returned as that day's positions
-- (measured: live holdings 0, day's history 5000.00). captured_at is provenance, not a selection
-- key. 000161 adds `present`: a capture marks everything it did not re-observe absent, so the
-- present rows for an (account, day) ARE its last capture, by construction, and reconstruction
-- filters on present with no dedup rule at all.
--
-- WRONG #2 (the liveness paragraph) — "rows only exist for days an account was actually synced, so a reconstruction needs no
-- account_status_history overlap test of its own." Accounts are SOFT-removed and the only
-- DELETE FROM holdings is in ReplaceForAccount, so a disconnected account's holdings live on and
-- the daily sweep re-captured them every day (measured: history 1500.00 over 2 rows against
-- Upsert's 1000.00 over 1). The capture now applies the SAME account_status_history overlap test
-- Upsert applies. Reconstruction still needs no overlap test of its OWN — but only because the
-- capture already applied one, which is the opposite of what this used to say.
--
-- GDPR: both tables carry user_id with an ON DELETE CASCADE FK to users, which is the path
-- DeleteUserCascade relies on (it issues one DELETE FROM users and lets the FK graph do the work).

-- =============================================================================
-- 1. holding_snapshots
-- =============================================================================

CREATE TABLE IF NOT EXISTS holding_snapshots (
    id                UUID           PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id           UUID           NOT NULL,
    account_id        UUID           NOT NULL,
    user_account_id   UUID,
    security_id       UUID           NOT NULL,

    as_of_date        DATE           NOT NULL,
    captured_at       TIMESTAMPTZ    NOT NULL DEFAULT now(),

    quantity          DECIMAL(18, 8) NOT NULL,
    cost_basis        DECIMAL(18, 2),
    institution_value DECIMAL(18, 2),
    institution_price DECIMAL(18, 8),

    created_at        TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ    NOT NULL DEFAULT now()
);

COMMENT ON TABLE holding_snapshots IS
    'Append-only history of investment positions (#1569 T6). Written in the SAME transaction as '
    'holdings'' full-replace, in addition to it — the holdings table keeps its replace semantics '
    'unchanged. One row per (account_id, security_id, as_of_date) UTC day; a later capture on the '
    'same day overwrites that day''s row and refreshes captured_at. Nothing here is ever deleted '
    'by a sync, so a disconnected institution''s positions stay queryable.';

COMMENT ON COLUMN holding_snapshots.as_of_date IS
    'UTC calendar date of the capture, computed as (now() AT TIME ZONE ''UTC'')::date inside the '
    'capturing statement so it does not depend on the session TimeZone setting.';

-- ⚠ Re-issued corrected by 000162, for databases already migrated past this file. Kept in sync
-- here so a FRESH database gets the right text immediately rather than only after 000162 runs.
COMMENT ON COLUMN holding_snapshots.captured_at IS
    'Provenance of this row: the transaction timestamp of the capture that last wrote it. NOT a '
    'selection key. Reconstruction of a day''s positions filters on `present`, not on '
    'MAX(captured_at) — see the column comment on present.';

COMMENT ON COLUMN holding_snapshots.user_account_id IS
    'Copied from holdings.user_account_id — the stable chain-expansion key (#1109) that survives '
    'a re-link, so history spans an institution change.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_holding_snapshots_account_security_date
    ON holding_snapshots (account_id, security_id, as_of_date);

CREATE INDEX IF NOT EXISTS idx_holding_snapshots_user_date
    ON holding_snapshots (user_id, as_of_date);

CREATE INDEX IF NOT EXISTS idx_holding_snapshots_account_date
    ON holding_snapshots (account_id, as_of_date);

DO $$ BEGIN
    ALTER TABLE holding_snapshots
        ADD CONSTRAINT fk_holding_snapshots_user_id
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holding_snapshots VALIDATE CONSTRAINT fk_holding_snapshots_user_id;

DO $$ BEGIN
    ALTER TABLE holding_snapshots
        ADD CONSTRAINT fk_holding_snapshots_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holding_snapshots VALIDATE CONSTRAINT fk_holding_snapshots_account_id;

-- RESTRICT mirrors holdings.fk_holdings_security_id: securities are shared reference data and are
-- never deleted by any application path, so RESTRICT is a tripwire, not an operational hazard.
DO $$ BEGIN
    ALTER TABLE holding_snapshots
        ADD CONSTRAINT fk_holding_snapshots_security_id
        FOREIGN KEY (security_id) REFERENCES securities(id) ON DELETE RESTRICT NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holding_snapshots VALIDATE CONSTRAINT fk_holding_snapshots_security_id;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_holding_snapshots_updated_at'
    ) THEN
        CREATE TRIGGER update_holding_snapshots_updated_at
            BEFORE UPDATE ON holding_snapshots
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- 2. liability_statements
-- =============================================================================
--
-- One table for all three liability types rather than three parallel history tables: the three
-- current-state tables share only a handful of columns and diverge into ~25 type-specific ones
-- each. The columns a later reader actually filters, sorts or charts on are promoted to typed
-- columns; the complete source row (including the credit APR schedule, which is a child table and
-- is the specific thing a partial Plaid response wipes today) is preserved verbatim in `details`
-- so nothing is lost to the projection.

CREATE TABLE IF NOT EXISTS liability_statements (
    id                        UUID          PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id                   UUID          NOT NULL,
    account_id                UUID          NOT NULL,
    liability_type            VARCHAR(32)   NOT NULL,

    as_of_date                DATE          NOT NULL,
    captured_at               TIMESTAMPTZ   NOT NULL DEFAULT now(),

    -- Promoted fields (NULL where the source type has no such column).
    is_overdue                BOOLEAN,
    last_payment_amount       DECIMAL(18, 2),
    last_payment_date         DATE,
    last_statement_issue_date DATE,
    last_statement_balance    DECIMAL(18, 2),
    minimum_payment_amount    DECIMAL(18, 2),
    next_payment_due_date     DATE,
    interest_rate_percentage  DECIMAL(8, 4),

    -- Full source row, plus "aprs" for credit. Never NULL; '{}' would mean "captured nothing".
    details                   JSONB         NOT NULL,

    created_at                TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT chk_liability_statements_type
        CHECK (liability_type IN ('credit', 'mortgage', 'student_loan'))
);

COMMENT ON TABLE liability_statements IS
    'Append-only history of Plaid liability state (#1569 T6). Written in the SAME transaction as '
    'each liability table''s full-replace, in addition to it. One row per '
    '(account_id, liability_type, as_of_date) UTC day. Nothing here is deleted by a sync, so the '
    'delete-on-absence path in liability_sync_service.go can no longer erase an APR schedule''s past.';

COMMENT ON COLUMN liability_statements.details IS
    'to_jsonb() of the complete source liability row, plus an "aprs" array for liability_type = '
    '''credit'' (credit_liability_aprs is a child table, and the APR schedule is precisely what a '
    'partial Plaid response used to wipe). Promoted columns above are duplicated here on purpose: '
    'the projection is for querying, `details` is the record of truth.';

COMMENT ON COLUMN liability_statements.captured_at IS
    'Transaction timestamp of the capture that last wrote this row. See holding_snapshots.captured_at.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_liability_statements_account_type_date
    ON liability_statements (account_id, liability_type, as_of_date);

CREATE INDEX IF NOT EXISTS idx_liability_statements_user_date
    ON liability_statements (user_id, as_of_date);

DO $$ BEGIN
    ALTER TABLE liability_statements
        ADD CONSTRAINT fk_liability_statements_user_id
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE liability_statements VALIDATE CONSTRAINT fk_liability_statements_user_id;

DO $$ BEGIN
    ALTER TABLE liability_statements
        ADD CONSTRAINT fk_liability_statements_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE liability_statements VALIDATE CONSTRAINT fk_liability_statements_account_id;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_liability_statements_updated_at'
    ) THEN
        CREATE TRIGGER update_liability_statements_updated_at
            BEFORE UPDATE ON liability_statements
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;
