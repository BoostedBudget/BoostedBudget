-- Migration: Add user_accounts.exclude_investment_sync — a per-chain opt-out from the investment
--            feed, for accounts Plaid reports through BOTH /transactions/sync and
--            /investments/transactions/get.
-- Issue: #1609 (Item 5) — "one real transaction shows twice in the activity feed"
--
-- =============================================================================
-- WHY
-- =============================================================================
--
-- A brokerage cash-management account (the reported case is a Morgan Stanley "Platinum CashPlus")
-- is returned by Plaid on BOTH product endpoints. Neither of our sync paths filters by account
-- type — both are item-scoped and accept every account Plaid returns
-- (investment_sync_service.go, transaction_sync_service.go) — so the same real-world movement
-- lands once in `transactions` and once in `investment_transactions`, and the unified activity
-- feed UNION ALLs them into two rows.
--
-- The fix is at the READ layer and this column is its switch. It is deliberately NOT an ingestion
-- filter and deliberately NOT a delete:
--
--   * Hard-deleting investment_transactions rows because a user reclassified an account is
--     SIDE-EFFECT DESTRUCTION, forbidden by
--     docs/decisions/2026-08-24-financial-history-retained-by-default.md. Those rows are
--     legitimate Plaid-reported history.
--   * Suppressing at ingestion loses the same data permanently and needs a full re-sync to undo.
--   * A read-layer filter is REVERSIBLE — toggle the flag back and the rows reappear — which is
--     also why this migration carries no backfill and needs no cleanup script. Turning the flag
--     on IS the cleanup.
--
-- The sync code already agrees: it explicitly stores retained rows and states that visibility
-- belongs in feed reads, not ingestion.
--
-- =============================================================================
-- WHY user_accounts AND NOT accounts
-- =============================================================================
--
-- user_accounts is the chain that survives a Plaid re-link (000033). accounts rows are minted
-- fresh on every re-link, so a flag stored there would silently revert the next time the bank was
-- reconnected — the exact defect class Item 3 of this same issue exists to fix. It is also where
-- every other per-account customization now lives (000185 lifted the legacy ones up the chain).
--
-- NOT NULL DEFAULT FALSE, so no read predicate has to reason about three-valued logic. The
-- default is the current behaviour, so an unmigrated expectation and a migrated one agree for
-- every existing row: this migration changes nothing that anyone can observe until a user sets
-- the flag.
--
-- ⚠ Adding a NOT NULL column WITH a constant DEFAULT is a metadata-only operation on PostgreSQL
-- 11+ (no table rewrite, no long ACCESS EXCLUSIVE hold), which is why it is safe to state the
-- default here rather than in the add/backfill/set-not-null dance an older server would need.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'exclude_investment_sync'
    ) THEN
        ALTER TABLE user_accounts
            ADD COLUMN exclude_investment_sync BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

COMMENT ON COLUMN user_accounts.exclude_investment_sync IS
$COMMENT$When TRUE, investment_transactions belonging to this chain are hidden from BOTH read surfaces -- the unified activity feed (transaction_repository.go GetByUserIDKeysetUnified) and the direct endpoint GET /api/v1/investments/investment-transactions (investment_transaction_repository.go GetByUserIDKeyset). Set by the user via PATCH /api/v1/user-accounts/{id} for a brokerage cash-management account that Plaid reports through BOTH /transactions/sync and /investments/transactions/get, which otherwise shows every movement twice (#1609 Item 5). This is a READ FILTER ONLY: no investment_transactions row is deleted, tombstoned or suppressed at ingestion, so clearing the flag makes exactly the same rows reappear. A row whose user_account_id is NULL (legacy; the column has been nullable since 000071) is attributed through accounts.user_account_id for its account_id, exactly as the chain-expansion reads already do, and stays VISIBLE when that yields no excluded chain -- never dropped for want of an attribution.$COMMENT$;

-- =============================================================================
-- Migration Complete
-- =============================================================================
