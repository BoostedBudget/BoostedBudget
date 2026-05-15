-- Migration: Backfill user_account_id keys in rules JSONB columns
-- Issue: #1109 - App-wide audit: enforce re-link account chain transparency
-- Purpose: Enrich rules.actions and rules.trigger_config JSONB with stable
--          *_user_account_id keys ALONGSIDE the existing *_account_id keys.
--          Original keys are preserved so this migration is safely reversible.
--
-- rules.actions shape (transfer_suggestion rules):
--   {"transfer": {"from_account_id": "uuid", "to_account_id": "uuid", "amount_type": "...", "amount": N}}
--   NOTE: actions are nested under the rule-type key ("transfer"), not at the top level.
--
-- rules.trigger_config.filters shape:
--   {"type": "event", "filters": {"account_id": "uuid", ...}}
--
-- Idempotency: each UPDATE only touches rows where the new key is not yet present.

-- =============================================================================
-- Step 1: Backfill from_user_account_id into rules.actions.transfer
--         Only processes transfer_suggestion rules that have transfer.from_account_id.
-- =============================================================================

UPDATE rules r
SET actions = jsonb_set(
    r.actions,
    '{transfer,from_user_account_id}',
    to_jsonb(a.user_account_id::text)
)
FROM accounts a
WHERE r.rule_type = 'transfer_suggestion'
  AND r.actions->'transfer' ? 'from_account_id'
  AND NOT (r.actions->'transfer' ? 'from_user_account_id')
  AND a.id::text = r.actions->'transfer'->>'from_account_id'
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 2: Backfill to_user_account_id into rules.actions.transfer
--         Only processes transfer_suggestion rules that have transfer.to_account_id.
-- =============================================================================

UPDATE rules r
SET actions = jsonb_set(
    r.actions,
    '{transfer,to_user_account_id}',
    to_jsonb(a.user_account_id::text)
)
FROM accounts a
WHERE r.rule_type = 'transfer_suggestion'
  AND r.actions->'transfer' ? 'to_account_id'
  AND NOT (r.actions->'transfer' ? 'to_user_account_id')
  AND a.id::text = r.actions->'transfer'->>'to_account_id'
  AND a.user_account_id IS NOT NULL;

-- =============================================================================
-- Step 3: Backfill user_account_id into rules.trigger_config filters
--         Only processes event-type triggers that have an account_id filter key.
-- =============================================================================

UPDATE rules r
SET trigger_config = jsonb_set(
    r.trigger_config,
    '{filters}',
    r.trigger_config->'filters' || jsonb_build_object('user_account_id', a.user_account_id::text)
)
FROM accounts a
WHERE r.trigger_config->>'type' = 'event'
  AND r.trigger_config->'filters' IS NOT NULL
  AND r.trigger_config->'filters' ? 'account_id'
  AND NOT (r.trigger_config->'filters' ? 'user_account_id')
  AND a.id::text = r.trigger_config->'filters'->>'account_id'
  AND a.user_account_id IS NOT NULL;
