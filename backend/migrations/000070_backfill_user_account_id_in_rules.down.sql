-- Rollback: Remove *_user_account_id keys added to rules JSONB columns
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
--
-- Removes the additive keys from rules.actions and rules.trigger_config.filters.
-- Original from_account_id / to_account_id / account_id keys are left untouched.

-- =============================================================================
-- Remove from_user_account_id and to_user_account_id from rules.actions
-- =============================================================================

UPDATE rules
SET actions = actions - 'from_user_account_id' - 'to_user_account_id'
WHERE rule_type = 'transfer_suggestion'
  AND (actions ? 'from_user_account_id' OR actions ? 'to_user_account_id');

-- =============================================================================
-- Remove user_account_id from rules.trigger_config.filters
-- =============================================================================

UPDATE rules
SET trigger_config = jsonb_set(
    trigger_config,
    '{filters}',
    (trigger_config->'filters') - 'user_account_id'
)
WHERE trigger_config->>'type' = 'event'
  AND trigger_config->'filters' IS NOT NULL
  AND trigger_config->'filters' ? 'user_account_id';
