-- Recreate idx_household_account_shares_active_visible exactly as migration 000110:109 defined it.
--
-- Restoring it does not make it usable again -- see the .up.sql for why no query's WHERE clause
-- implies its predicate. This exists so the migration is reversible, not because the rollback
-- restores anything a reader would benefit from.

CREATE INDEX IF NOT EXISTS idx_household_account_shares_active_visible
    ON household_account_shares (household_id, user_account_id)
    WHERE revoked_at IS NULL AND visibility = 'shared';
