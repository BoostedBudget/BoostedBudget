-- Rollback: Remove *_user_account_id keys added to rules JSONB columns
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
--
-- Removes the additive keys from rules.actions.transfer and rules.trigger_config.filters.
-- Original from_account_id / to_account_id / account_id keys are left untouched.

-- =============================================================================
-- Remove from_user_account_id and to_user_account_id from rules.actions.transfer
-- =============================================================================

UPDATE rules
SET actions = jsonb_set(
    actions,
    '{transfer}',
    (actions->'transfer') - 'from_user_account_id' - 'to_user_account_id'
)
WHERE rule_type = 'transfer_suggestion'
  AND actions->'transfer' IS NOT NULL
  AND ((actions->'transfer' ? 'from_user_account_id') OR (actions->'transfer' ? 'to_user_account_id'));

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
