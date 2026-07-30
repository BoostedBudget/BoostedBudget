-- Restores migration 000119's original COMMENT ON COLUMN text verbatim -- "down" here means
-- reverting the comment text this migration's up.sql corrected, not dropping the comment or the
-- column (the column itself remains owned by migration 000119 / its own down migration). Guarded
-- the same way as up.sql so it is a no-op rather than an error if the column is ever absent.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'users'
          AND column_name = 'plaid_cleanup_pending_at'
    ) THEN
        COMMENT ON COLUMN users.plaid_cleanup_pending_at IS
            'Set the instant a user transitions into a Plaid-disconnect-repair-eligible status '
            '(inactive/cancelled) with cleanup not yet complete; cleared on resubscribe/comp-grant or once '
            'a repair pass successfully disconnects the user''s Plaid items. NULL means nothing pending. '
            'Issue #1413 round-10 — replaces retention_started_at as the repair pass''s grace-window anchor.';
    END IF;
END $$;
