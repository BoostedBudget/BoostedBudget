-- Rollback: Remove user_account_id columns from transfer_suggestions
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency

-- =============================================================================
-- Drop FK constraints
-- =============================================================================

ALTER TABLE transfer_suggestions DROP CONSTRAINT IF EXISTS fk_transfer_sugg_from_user_account_id;
ALTER TABLE transfer_suggestions DROP CONSTRAINT IF EXISTS fk_transfer_sugg_to_user_account_id;

-- =============================================================================
-- Drop indexes
-- =============================================================================

DROP INDEX IF EXISTS idx_transfer_sugg_from_user_account_id;
DROP INDEX IF EXISTS idx_transfer_sugg_to_user_account_id;

-- =============================================================================
-- Drop columns
-- =============================================================================

ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS from_user_account_id;
ALTER TABLE transfer_suggestions DROP COLUMN IF EXISTS to_user_account_id;
