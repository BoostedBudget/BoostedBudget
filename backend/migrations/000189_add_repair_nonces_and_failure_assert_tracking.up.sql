-- Migration: Bind the reconnect repair to a server-minted nonce (#1620), record when a linked
--            item's failure was last ASSERTED (#1623), and create two columns P4-T3 will wire
--            (#1613 investments-skipped-at-link, and an investment-only portfolio total).
-- Issue: #1609 P4-T2 (parts 1-2), #1613 P4-T3 (parts 3-4, created here but deliberately unwired)
--
-- =============================================================================
-- 1. linked_item_repair_nonces — #1620
-- =============================================================================
--
-- POST /api/v1/plaid/reconnect (repairLinkedItem) marks a broken linked item 'active' again. Until
-- this migration its ONLY inputs were an authenticated user and a Plaid item_id — and a Plaid
-- item_id is not a secret (ExchangePublicToken already returns it to the client). Any owner of a
-- broken item could therefore mark it healthy without ever opening Plaid Link, which silences the
-- "reconnect your bank" notification while the connection stays dead.
--
-- ⚠ WHAT THIS DOES AND DOES NOT PROVE — do not overstate it anywhere downstream.
-- A nonce CANNOT prove Plaid Link actually succeeded. Real proof would need either the
-- `public_token` Link returns (forbidden here: exchanging it calls AccountService.LinkAccounts and
-- creates a DUPLICATE linked_items row — the bug commit 5d75eab0 exists to fix) or a Plaid
-- LOGIN_REPAIRED webhook (asynchronous, and not sent for every repairable status). The exact
-- guarantee this table buys is:
--
--     the repair call presents a nonce THIS server minted, for THIS item, for THIS user,
--     exactly ONCE, inside the minting Link token's own validity window.
--
-- That is strictly more than "an authenticated owner said so": it forces the caller through
-- POST /api/v1/plaid/reconnect-link-token, which is itself gated on ownership AND a repairable
-- status, and it makes a replayed or shared repair call fail.
--
-- Shape copied from household_invites (migration 000109): a random 32-byte token is returned RAW
-- to the client exactly once and only its sha256 hex digest is persisted, so a database reader
-- cannot mint a repair. Burning is a conditional UPDATE whose RowsAffected IS the authorization
-- decision (household_invite_repository.go MarkAccepted) — never a read-then-write.
--
-- ⚠ Deliberately the DATABASE and not Redis. main.go boots with a NoopCache when Redis is absent
-- or unreachable, so a Redis-backed nonce would either fail open (every repair accepted) or fail
-- closed (no repair possible) depending on deploy configuration, silently.
--
-- =============================================================================
-- 2. linked_items.last_failure_asserted_at — #1623
-- =============================================================================
--
-- A delayed LOGIN_REPAIRED webhook (or a delayed repair call) must not clear a failure that was
-- asserted AFTER the event it reports. The cross-status case is already handled — HandleLoginRepaired
-- passes only 'login_required' to RepairStatus — but the SAME-status case is not:
-- login_required -> fixed -> login_required AGAIN, then the FIRST failure's webhook lands and
-- clears the second.
--
-- ⚠ linked_items.status_changed_at CANNOT be used for this, and that is the whole point of adding a
-- column. TransitionErrorStatus's predicate is `... AND status <> $1`, so a re-asserted SAME-status
-- failure matches ZERO rows and writes NOTHING: status_changed_at never advances, and the second
-- failure looks exactly as old as the first. This column is therefore written by a SEPARATE,
-- UNCONDITIONAL statement on every assert, including a duplicate one.
--
-- ⚠ It lives on linked_items and NOT on linked_item_repair_nonces on purpose: a failure can be
-- re-asserted on an item that never minted a nonce (the webhook path always can), and on the nonce
-- table the fix would be dead for every failure with no repair in flight.
--
-- NULLable: every row that exists today has no assert on record, and there is nothing to backfill
-- it from (status_changed_at means something else, per the paragraph above). NULL reads as "no
-- failure has been asserted since this column existed", which the comparisons treat as "not newer".
--
-- =============================================================================
-- 3-4. Created for P4-T3, deliberately UNWIRED
-- =============================================================================
--
-- Nothing reads or writes linked_items.investments_skipped_at_link or
-- portfolio_snapshots.total_value_investment_only yet, and neither is backfilled. Their defaults
-- (FALSE / NULL) are the honest description of every existing row: we do not know whether
-- investments were skipped at link time for items linked before this migration, and no snapshot
-- written before it carries a type-gated total.

-- =============================================================================
-- Step 1: linked_item_repair_nonces
-- =============================================================================

CREATE TABLE IF NOT EXISTS linked_item_repair_nonces (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    linked_item_id           UUID        NOT NULL,
    user_id                  UUID        NOT NULL,
    token_hash               TEXT        NOT NULL,
    expires_at               TIMESTAMPTZ NOT NULL,
    used_at                  TIMESTAMPTZ,
    last_failure_asserted_at TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE linked_item_repair_nonces IS
$COMMENT$One row per update-mode Plaid Link token minted by POST /api/v1/plaid/reconnect-link-token (#1620). Presenting the raw token back to POST /api/v1/plaid/reconnect is a precondition for clearing the item's error state -- an ADDITION to the ownership and repairable-status checks, never a replacement for either. This table holds NO financial history: every row is a short-lived, single-use authorization artifact for one repair attempt, scoped to one item, and carries no amount, balance, transaction, date-series or provider record. Deleting a row destroys nothing a user could ever ask to see, which is why the CASCADE from linked_items below is correct rather than side-effect destruction under docs/decisions/2026-08-24-financial-history-retained-by-default.md.$COMMENT$;

COMMENT ON COLUMN linked_item_repair_nonces.token_hash IS
    'SHA-256 hex digest of the one-time repair nonce. The plaintext nonce is returned to the '
    'client exactly once, in the reconnect-link-token response body, and is never persisted.';

COMMENT ON COLUMN linked_item_repair_nonces.expires_at IS
$COMMENT$Copied verbatim from the Plaid link_token's own `expiration`, not invented locally. The nonce is useless without the Link token minted beside it, so the Plaid-authoritative expiry is the correct and tightest bound; a longer window would let a nonce outlive the Link session it authorizes.$COMMENT$;

COMMENT ON COLUMN linked_item_repair_nonces.used_at IS
$COMMENT$Set by the single conditional UPDATE that burns the nonce (LinkedItemRepairNonceRepository.Burn). NULL means unburned. The burn's RowsAffected is the authorization decision itself, so two concurrent repair calls presenting the same nonce cannot both succeed; a burn matching zero rows is a REFUSAL, never an error to swallow.$COMMENT$;

COMMENT ON COLUMN linked_item_repair_nonces.last_failure_asserted_at IS
$COMMENT$Snapshot of linked_items.last_failure_asserted_at taken when this nonce was minted (#1623). The burn refuses if the item''s live value is no longer IS NOT DISTINCT FROM this one, i.e. a failure was asserted after the user started this repair -- so a repair begun against failure A cannot silently clear a later failure B. NULL is a real, matching value (an item with no assert on record), which is why the comparison is IS NOT DISTINCT FROM and not `=`.$COMMENT$;

-- Foreign keys. ON DELETE CASCADE on both: a nonce is meaningless without its item, and it is not
-- financial history (see the table comment), so cascading destroys nothing retained. The user_id
-- cascade also keeps the table reachable from `users` for GDPR erasure, which
-- docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md requires of every table.
DO $$ BEGIN
    ALTER TABLE linked_item_repair_nonces
        ADD CONSTRAINT fk_linked_item_repair_nonces_linked_item_id
        FOREIGN KEY (linked_item_id) REFERENCES linked_items(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE linked_item_repair_nonces VALIDATE CONSTRAINT fk_linked_item_repair_nonces_linked_item_id;

DO $$ BEGIN
    ALTER TABLE linked_item_repair_nonces
        ADD CONSTRAINT fk_linked_item_repair_nonces_user_id
        FOREIGN KEY (user_id) REFERENCES users(id)
        ON DELETE CASCADE
        NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE linked_item_repair_nonces VALIDATE CONSTRAINT fk_linked_item_repair_nonces_user_id;

-- The burn looks a nonce up by token_hash alone and applies every other predicate in the same
-- statement, so this index is both the lookup path and the "one row per nonce" guarantee.
CREATE UNIQUE INDEX IF NOT EXISTS uq_linked_item_repair_nonces_token_hash
    ON linked_item_repair_nonces (token_hash);

-- Supports the FK's own cascade check and any future "does this item have a repair in flight?"
-- read without a sequential scan.
CREATE INDEX IF NOT EXISTS idx_linked_item_repair_nonces_linked_item_id
    ON linked_item_repair_nonces (linked_item_id);

-- Expiry sweep: find spent/expired nonces. No sweep exists yet; this index is what makes adding
-- one a one-line job rather than a table scan.
CREATE INDEX IF NOT EXISTS idx_linked_item_repair_nonces_expires_at
    ON linked_item_repair_nonces (expires_at);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_linked_item_repair_nonces_updated_at'
    ) THEN
        CREATE TRIGGER update_linked_item_repair_nonces_updated_at
            BEFORE UPDATE ON linked_item_repair_nonces
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- Step 2: linked_items.last_failure_asserted_at (#1623)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'last_failure_asserted_at'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN last_failure_asserted_at TIMESTAMPTZ;
    END IF;
END $$;

COMMENT ON COLUMN linked_items.last_failure_asserted_at IS
$COMMENT$Server-stamped instant at which a failure was last ASSERTED for this item, by any producer (ITEM webhook or sync worker), INCLUDING an assert that did not change status. Distinct from status_changed_at, which only advances when the status actually changes -- TransitionErrorStatus's `AND status <> $1` predicate makes a same-status re-assert match zero rows, so status_changed_at cannot order two consecutive failures of the SAME kind (#1623). Written unconditionally by LinkedItemRepository.MarkFailureAsserted, outside that predicated UPDATE. Read by HandleLoginRepaired, which refuses to clear a failure asserted AFTER the webhook it is acting on was received, and snapshotted into linked_item_repair_nonces at mint so an in-flight repair cannot clear a later failure. Always stamped with the DATABASE clock (now()), never a Go time.Now(), because both readers compare it against another server-side timestamp. NULL means no failure has been asserted since this column existed; both readers treat NULL as "not newer".$COMMENT$;

-- =============================================================================
-- Step 3: linked_items.investments_skipped_at_link (#1613, P4-T3 — UNWIRED)
-- =============================================================================
--
-- Adding a NOT NULL column WITH a constant DEFAULT is metadata-only on PostgreSQL 11+ (no table
-- rewrite, no long ACCESS EXCLUSIVE hold), so the default is stated inline.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'investments_skipped_at_link'
    ) THEN
        ALTER TABLE linked_items
            ADD COLUMN investments_skipped_at_link BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

COMMENT ON COLUMN linked_items.investments_skipped_at_link IS
$COMMENT$RESERVED FOR #1613 P4-T3 -- created by migration 000189 and deliberately NOT WIRED: as of that migration nothing in the codebase reads or writes this column, and it is not backfilled. Intended meaning: TRUE when Plaid granted no investments product for this Item at link time, so the investment sync was never enqueued for it. FALSE is the correct value for every pre-existing row only in the sense that we do not know -- do not read it as evidence that investments WERE granted until P4-T3 lands and starts writing it.$COMMENT$;

-- =============================================================================
-- Step 4: portfolio_snapshots.total_value_investment_only (#1613, P4-T3 — UNWIRED)
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'portfolio_snapshots' AND column_name = 'total_value_investment_only'
    ) THEN
        ALTER TABLE portfolio_snapshots
            ADD COLUMN total_value_investment_only NUMERIC(18, 2);
    END IF;
END $$;

COMMENT ON COLUMN portfolio_snapshots.total_value_investment_only IS
$COMMENT$RESERVED FOR #1613 P4-T3 -- created by migration 000189 and deliberately NOT WIRED: as of that migration nothing reads or writes this column, and it is deliberately NOT backfilled. Intended meaning: the same total as total_value but restricted to accounts of type ''investment''. NULLable on purpose -- a snapshot written before this migration has no type-gated total and none can be reconstructed, so P4-T3 will render DayChange UNAVAILABLE for the first day rather than compute a wrong one from a mixed-type total. Never coalesce this to 0: 0 and "unknown" are different answers here.$COMMENT$;

-- =============================================================================
-- Migration Complete
-- =============================================================================
