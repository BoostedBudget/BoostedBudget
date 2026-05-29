-- Issue #1203: transaction review state.
-- Every NEW transaction defaults to its Plaid PFC category AND reviewed=false
-- (unreviewed). It becomes reviewed=true when an auto-categorization rule applies
-- a category, when the user manually categorizes it, or via an explicit toggle.
-- The supporting partial index is created CONCURRENTLY in migration 000082.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'reviewed'
    ) THEN
        ALTER TABLE transactions ADD COLUMN reviewed BOOLEAN NOT NULL DEFAULT false;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'reviewed_at'
    ) THEN
        ALTER TABLE transactions ADD COLUMN reviewed_at TIMESTAMP WITH TIME ZONE;
    END IF;
END $$;

-- Backfill: grandfather ALL pre-existing transactions as reviewed so the
-- "unreviewed" inbox starts empty; only transactions synced after this migration
-- enter the inbox. Guarded by `reviewed = false` so it is idempotent.
-- Batched in a DO loop to limit lock contention on large tables.
DO $$
DECLARE
    updated_rows INT;
BEGIN
    LOOP
        UPDATE transactions
        SET reviewed = true,
            reviewed_at = COALESCE(category_override_date, created_at)
        WHERE ctid IN (
            SELECT ctid FROM transactions WHERE reviewed = false LIMIT 10000
        );
        GET DIAGNOSTICS updated_rows = ROW_COUNT;
        EXIT WHEN updated_rows = 0;
    END LOOP;
END $$;

COMMENT ON COLUMN transactions.reviewed IS
    'Whether the transaction has been triaged (true) or is awaiting review (false). New Plaid rows arrive unreviewed; rules/manual review/explicit toggle mark it reviewed (Issue #1203).';
COMMENT ON COLUMN transactions.reviewed_at IS
    'When the transaction was marked reviewed (NULL while unreviewed).';
