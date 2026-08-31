-- Issue #1569: account_status_history — the point-in-time liveness table.
--
-- Nothing today records whether an account was LIVE at a past date; every "as of date D" query
-- approximates with CURRENT state. Symptom: an account holding $1,000 thirty days ago and
-- disconnected since produces a $0 baseline and a 0% change for a period real money left. This
-- table is the durable fix: one row per (account, status) interval, half-open [valid_from,
-- valid_to), so "was account X live at instant T" becomes a single indexed range lookup instead
-- of a guess from current state.
--
-- This migration creates the TABLE ONLY. The backfill (populating history for accounts that
-- predate this table) and the trigger that keeps it current going forward both live in the next
-- migration, 000152 — in that order, deliberately: see 000152's header for why attaching the
-- trigger before the backfill runs would produce a duplicate open interval.
--
-- FK convention follows 000123-000126 / 000150 (this repo's actual idiom, not the textbook
-- EXCEPTION WHEN duplicate_object trick): drop-if-exists guarded by `IF EXISTS (SELECT ... FROM
-- pg_constraint)`, then ADD CONSTRAINT ... NOT VALID, then VALIDATE CONSTRAINT, each step guarded
-- so the migration is safe to re-run.
--
--   account_id -> accounts(id) ON DELETE CASCADE
--     Safe now that Task 2 (#1569) stopped hard-deleting accounts outside an explicit purge.
--     THREE paths delete an accounts row and therefore fire this CASCADE, not one:
--       - PurgeAccount (the explicit per-account purge, account_repository.go);
--       - PurgeLinkedItem, which ends in DELETE FROM linked_items and reaches accounts through
--         fk_accounts_linked_item_id ON DELETE CASCADE (000007);
--       - GDPR erasure, which is DELETE FROM users and reaches accounts through
--         fk_accounts_user_id ON DELETE CASCADE (000007).
--
--   user_id -> users(id) ON DELETE CASCADE
--     Required, not optional. GDPR erasure in this codebase runs entirely off the FK graph
--     rooted at `users` (one `DELETE FROM users`, 36 cascades — see
--     internal/repository/profile_repository.go). A history table without this FK would silently
--     retain personal data (account status + timing) after a completed erasure, and would need a
--     hand-maintained DELETE bolted onto profile_repository.go instead of falling out of the
--     existing cascade graph for free.

CREATE TABLE IF NOT EXISTS account_status_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL,
    user_id UUID NOT NULL,
    user_account_id UUID,
    status VARCHAR(20) NOT NULL CHECK (status IN ('active', 'replaced', 'expired', 'removed')),
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ,               -- NULL = still current (the open interval for this account)
    reason TEXT,
    provenance VARCHAR(16) NOT NULL DEFAULT 'recorded'
               CHECK (provenance IN ('recorded', 'backfilled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_ash_interval CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- At most one OPEN interval per account at a time. This is a backstop, not the primary ordering
-- guarantee — the trigger in 000152 is responsible for always closing the prior open interval
-- before opening a new one; this index exists to make a bug in that ordering fail loudly (a
-- unique-violation error) instead of silently producing two "current" rows for one account.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ash_open
    ON account_status_history (account_id) WHERE valid_to IS NULL;

-- Primary access pattern for an as-of-date lookup: "give me this account's intervals, most
-- recent first" (or "the interval that was open at/around instant T").
CREATE INDEX IF NOT EXISTS idx_ash_account_from
    ON account_status_history (account_id, valid_from DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'account_status_history'::regclass
          AND conname = 'fk_ash_account_id'
    ) THEN
        ALTER TABLE account_status_history
            ADD CONSTRAINT fk_ash_account_id
            FOREIGN KEY (account_id) REFERENCES accounts(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'account_status_history'::regclass
          AND conname = 'fk_ash_account_id'
          AND NOT convalidated
    ) THEN
        ALTER TABLE account_status_history VALIDATE CONSTRAINT fk_ash_account_id;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'account_status_history'::regclass
          AND conname = 'fk_ash_user_id'
    ) THEN
        ALTER TABLE account_status_history
            ADD CONSTRAINT fk_ash_user_id
            FOREIGN KEY (user_id) REFERENCES users(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'account_status_history'::regclass
          AND conname = 'fk_ash_user_id'
          AND NOT convalidated
    ) THEN
        ALTER TABLE account_status_history VALIDATE CONSTRAINT fk_ash_user_id;
    END IF;
END $$;

COMMENT ON TABLE account_status_history IS
$COMMENT$Point-in-time liveness record for accounts (Issue #1569). One row per (account, status)
interval, half-open [valid_from, valid_to): live starting AT valid_from, no longer live AT
valid_to. valid_to IS NULL means the interval is still open (current status). Populated by a
one-time backfill for pre-existing accounts (provenance='backfilled', see 000152) and kept current
by the record_account_status_change() trigger on accounts (provenance='recorded', also 000152).$COMMENT$;

COMMENT ON COLUMN account_status_history.valid_to IS
'Exclusive upper bound of this interval. NULL means still current. Half-open: live at valid_from, not live at valid_to.';

COMMENT ON COLUMN account_status_history.user_account_id IS
$COMMENT$The account's stable cross-re-link identity AS IT STOOD when this interval opened, copied by the record_account_status_change() trigger. NOT READ by anything today (every as-of liveness predicate keys on account_id); it exists so a future chain-level "was this user_account live at T" query does not have to join back to a mutable accounts row. NULLABLE and legitimately NULL for intervals opened before an account's identity was minted (Issue #1569) — backfillDownstreamUserAccountIDs (internal/database/backfill.go) repairs those, because the trigger fires on status changes only and would never rewrite them.$COMMENT$;

COMMENT ON COLUMN account_status_history.provenance IS
'''backfilled'' for rows synthesized once from pre-existing accounts.updated_at/replaced_at/removed_at columns (000152); ''recorded'' for rows written live by the accounts status trigger.';
