-- Add index for filtering transactions by effective_pfc_detailed (Issue #1200)
-- effective_pfc_detailed is a generated column (see migration 000073)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_transactions_user_effective_pfc_detailed
    ON transactions (user_id, effective_pfc_detailed)
    WHERE effective_pfc_detailed IS NOT NULL;
