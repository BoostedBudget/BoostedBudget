-- Issue #1569: backfill account_status_history for every pre-existing account, THEN attach the
-- trigger that keeps it current going forward.
--
-- ⚠ ORDERING IS MANDATORY, NOT STYLISTIC. The backfill below is idempotent via `WHERE NOT
-- EXISTS (...)`, which is only safe because nothing else is writing to this table while it runs.
-- If the trigger were attached first, a status write landing between 000151 and this backfill
-- would create the account's one-and-only history row itself; the backfill would then either
-- (a) see that row and (correctly) skip the account, silently losing the account's true earlier
-- history, or (b) not see it (per-status-branch guard, see below) and attempt to insert a second
-- row, colliding with uq_ash_open. The trigger is therefore the LAST statement in this file, run
-- only after every existing account has a backfilled row.
--
-- =============================================================================
-- Backfill shapes (one branch per current accounts.status)
-- =============================================================================
--
--   active   -> active   [created_at, infinity)
--   replaced -> active   [created_at, replaced_at)  +  replaced [replaced_at, infinity)
--   expired  -> active   [created_at, updated_at)   +  expired  [updated_at,  infinity)
--   removed  -> active   [created_at, removed_at)   +  removed  [removed_at,  infinity)
--
-- Each branch is idempotent per (account_id, status): the guard is
-- `NOT EXISTS (... WHERE h.account_id = a.id AND h.status = '<this branch''s status>')`, not a
-- blanket "does this account have ANY row" check. That distinction matters within a single run of
-- this migration: for a 'replaced' account we insert TWO rows (the 'active' prefix and the
-- 'replaced' terminal state) in two separate INSERT statements below, and a blanket
-- "any row exists" guard on the second statement would see the first statement's already-inserted
-- 'active' row (same transaction, same snapshot in this migration's autocommit-per-file
-- semantics — see .squawk.toml's assume_in_transaction note) and wrongly skip inserting the
-- terminal interval. Guarding per-status makes each INSERT idempotent on its own and makes a
-- second run of this migration a true no-op.
--
-- ⚠ EXPIRED USES accounts.updated_at, NOT linked_items.status_changed_at. This is the single most
-- likely way to introduce a NEW wrong number in this migration (flagged by two independent
-- reviews of the plan). status_changed_at marks the START of the 90-day fingerprint hold, not the
-- expiry — an account only becomes 'expired' once its item has been disconnected for >= 90 days
-- (internal/services/account_service.go ~:246-260). Using status_changed_at would date every
-- expired account's history transition ~90 days too early and silently drop its balance from an
-- entire quarter of historical baselines — exactly the class of bug this table exists to prevent,
-- reintroduced by the very migration meant to fix it. accounts.updated_at is written by the
-- expiry statement itself (internal/repository/account_repository.go, ExpireAccountsByFingerprint,
-- ~:1244-1253), so it is the correct (if approximate — see below) instant.
--
-- Because that expiry instant is only approximate — accounts.updated_at could since have been
-- overwritten by an unrelated update — every backfilled 'expired' terminal row is stamped
-- reason='backfill_expired_estimated' so it stays greppable forever as "this timestamp is our
-- best estimate, not an exact recorded instant." 'replaced' and 'removed' terminal rows use their
-- own dedicated, single-purpose timestamp columns (replaced_at, removed_at) that are ONLY ever
-- set by the transition itself, so those reasons ('relink', 'user_disconnect') are stated as fact,
-- matching what the live trigger below would derive for the same transition.
--
-- ⚠ ZERO-LENGTH INTERVAL GUARD. chk_ash_interval rejects valid_to <= valid_from. If
-- replaced_at/removed_at/updated_at is NULL, or (from bad historical data) <= created_at, we
-- cannot honestly claim the account was ever observably 'active' for a nonzero duration. The
-- deterministic handling: skip the 'active' prefix interval entirely in that case, and start the
-- terminal interval at created_at instead of the (missing/invalid) transition timestamp — i.e.
-- treat the account as having been in its terminal status for its whole recorded life. This
-- produces exactly one open interval per such account (satisfying uq_ash_open) and never a
-- zero-length one.

-- ---------------------------------------------------------------------------
-- active accounts: single open interval since creation
-- ---------------------------------------------------------------------------
INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, NULL, 'created', 'backfilled'
FROM accounts a
WHERE a.status = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active'
  );

-- ---------------------------------------------------------------------------
-- replaced accounts: active prefix (when nonzero-length) + replaced terminal
-- ---------------------------------------------------------------------------
INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.replaced_at, 'created', 'backfilled'
FROM accounts a
WHERE a.status = 'replaced'
  AND a.replaced_at IS NOT NULL AND a.replaced_at > a.created_at
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active'
  );

INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'replaced',
       CASE WHEN a.replaced_at IS NOT NULL AND a.replaced_at > a.created_at
            THEN a.replaced_at ELSE a.created_at END,
       NULL, 'relink', 'backfilled'
FROM accounts a
WHERE a.status = 'replaced'
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'replaced'
  );

-- ---------------------------------------------------------------------------
-- expired accounts: active prefix (when nonzero-length) + expired terminal (estimated instant)
-- ---------------------------------------------------------------------------
INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.updated_at, 'created', 'backfilled'
FROM accounts a
WHERE a.status = 'expired'
  AND a.updated_at IS NOT NULL AND a.updated_at > a.created_at
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active'
  );

INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'expired',
       CASE WHEN a.updated_at IS NOT NULL AND a.updated_at > a.created_at
            THEN a.updated_at ELSE a.created_at END,
       NULL, 'backfill_expired_estimated', 'backfilled'
FROM accounts a
WHERE a.status = 'expired'
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'expired'
  );

-- ---------------------------------------------------------------------------
-- removed accounts: active prefix (when nonzero-length) + removed terminal
-- ---------------------------------------------------------------------------
INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.removed_at, 'created', 'backfilled'
FROM accounts a
WHERE a.status = 'removed'
  AND a.removed_at IS NOT NULL AND a.removed_at > a.created_at
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active'
  );

INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
SELECT a.id, a.user_id, a.user_account_id, 'removed',
       CASE WHEN a.removed_at IS NOT NULL AND a.removed_at > a.created_at
            THEN a.removed_at ELSE a.created_at END,
       NULL, 'user_disconnect', 'backfilled'
FROM accounts a
WHERE a.status = 'removed'
  AND NOT EXISTS (
      SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'removed'
  );

-- =============================================================================
-- Trigger: record_account_status_change() — attached LAST, after every existing account has a
-- backfilled row (see the ordering warning at the top of this file).
-- =============================================================================
--
-- Fires AFTER INSERT OR UPDATE OF status ON accounts, FOR EACH ROW. Four mechanics, each
-- load-bearing (verified against this codebase by an adversarial review — do not "simplify" any
-- of these away):
--
--   1. No-op guard, via `WHEN (OLD.status IS DISTINCT FROM NEW.status)` on the UPDATE trigger
--      (not inside the function body). `UPDATE OF status` fires even when the new value equals
--      the old one (UpdateStatus in internal/repository/account_repository.go always sets status,
--      whether or not it changed); without this guard a same-value update would close the current
--      open interval and immediately reopen an identical one at the same instant, i.e.
--      valid_to = valid_from, which chk_ash_interval correctly rejects. This is implemented as
--      TWO triggers below (one AFTER INSERT, one AFTER UPDATE OF status) rather than one combined
--      trigger, because a WHEN clause cannot reference TG_OP (it is only visible inside the
--      trigger function body) and PostgreSQL rejects a WHEN clause that references OLD on a
--      trigger whose event list includes INSERT (there is no OLD row on insert) — confirmed
--      empirically against this project's own Postgres 17 instance while writing this migration.
--      Both triggers share the same function; the function's own `IF TG_OP = 'INSERT'` branch
--      covers the insert case (see mechanic #4 below).
--   2. clock_timestamp(), NOT now(). now() is fixed for the whole transaction; a writer that
--      waited on this row's lock behind another transaction would otherwise stamp its history
--      entry with a timestamp from BEFORE it actually ran, writing history out of order.
--   3. Close the old interval BEFORE opening the new one (two separate statements, in that
--      order). uq_ash_open is a backstop for a bug in this ordering, not a substitute for it.
--   4. `reason` is derived from the actual (OLD.status -> NEW.status) transition, matching the
--      only two production status writers plus the initial insert:
--        INSERT                     -> 'created'
--        active   -> replaced       -> 'relink'                  (accountRepository.UpdateStatus)
--        active   -> expired        -> 'fingerprint_expiry'      (ExpireAccountsByFingerprint)
--        active   -> removed        -> 'user_disconnect'         (UpdateStatus / MarkRemovedByLinkedItemID)
--        anything else (future)     -> 'status_change:<old>_to_<new>' — a safe, greppable fallback
--                                       instead of silently mislabeling a transition nobody has
--                                       described yet.
--
-- Why a trigger rather than doing this from Go: the two production status writers
-- (accountRepository.UpdateStatus, ExpireAccountsByFingerprint) are autocommit `r.db.Exec` calls
-- with no application-level transaction wrapping them — but a trigger runs INSIDE that single
-- statement's own transaction, so the status change and its history row commit or roll back
-- together with no extra plumbing. A trigger also automatically covers CreateBatch (which omits
-- the status column and relies on its DEFAULT 'active'), MarkRemovedByLinkedItemID's bulk
-- multi-row UPDATE (FOR EACH ROW fires once per affected row), and any future writer, including
-- migrations, without each one needing to remember to also write history. accounts already
-- carries an analogous per-row trigger (update_accounts_updated_at, 000007), so this is not a new
-- mechanism in this codebase.

CREATE OR REPLACE FUNCTION record_account_status_change() RETURNS TRIGGER AS $$
DECLARE
    ts TIMESTAMPTZ;
    derived_reason TEXT;
BEGIN
    ts := clock_timestamp();

    IF TG_OP = 'INSERT' THEN
        derived_reason := 'created';
    ELSIF OLD.status = 'active' AND NEW.status = 'replaced' THEN
        derived_reason := 'relink';
    ELSIF OLD.status = 'active' AND NEW.status = 'expired' THEN
        derived_reason := 'fingerprint_expiry';
    ELSIF OLD.status = 'active' AND NEW.status = 'removed' THEN
        derived_reason := 'user_disconnect';
    ELSE
        derived_reason := 'status_change:' || OLD.status || '_to_' || NEW.status;
    END IF;

    -- Close BEFORE open (mechanic #3). No-op on INSERT (nothing to close yet).
    UPDATE account_status_history
        SET valid_to = ts
        WHERE account_id = NEW.id AND valid_to IS NULL;

    INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, reason, provenance)
        VALUES (NEW.id, NEW.user_id, NEW.user_account_id, NEW.status, ts, derived_reason, 'recorded');

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'account_status_history_insert_trigger'
    ) THEN
        CREATE TRIGGER account_status_history_insert_trigger
            AFTER INSERT ON accounts
            FOR EACH ROW
            EXECUTE FUNCTION record_account_status_change();
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'account_status_history_update_trigger'
    ) THEN
        CREATE TRIGGER account_status_history_update_trigger
            AFTER UPDATE OF status ON accounts
            FOR EACH ROW
            WHEN (OLD.status IS DISTINCT FROM NEW.status)
            EXECUTE FUNCTION record_account_status_change();
    END IF;
END $$;
