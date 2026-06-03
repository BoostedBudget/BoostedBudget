-- Rollback: Remove user_account_id from savings_goals
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency

-- =============================================================================
-- Drop FK constraint
-- =============================================================================

ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS fk_savings_goals_user_account_id;

-- =============================================================================
-- Drop index
-- =============================================================================

DROP INDEX IF EXISTS idx_savings_goals_user_account_id;

-- =============================================================================
-- Drop column
-- =============================================================================

ALTER TABLE savings_goals DROP COLUMN IF EXISTS user_account_id;
