-- Migration: Backfill user_accounts from existing accounts
-- Issue: #171 - Plaid Re-Link Handling
-- Purpose: Create user_account entries for each unique (user_id, institution_id, mask) combination
--
-- This migration creates stable user_account records from existing accounts.
-- The matching key (user_id, institution_id, mask) allows re-link detection.
--
-- Note: This is a DATA migration. No DDL changes.

-- =============================================================================
-- Backfill user_accounts table
-- =============================================================================

-- Insert unique account combinations into user_accounts
-- Uses ON CONFLICT to be idempotent (can run multiple times safely)
INSERT INTO user_accounts (
    id,
    user_id,
    institution_id,
    mask,
    account_name_hint,
    primary_type,
    primary_subtype,
    created_at,
    updated_at
)
SELECT
    -- Generate deterministic UUIDs for idempotency
    -- Using uuid_generate_v5 with namespace + key ensures same input = same UUID
    uuid_generate_v5(
        'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid,  -- namespace UUID (arbitrary)
        a.user_id::text || ':' || li.institution_id || ':' || COALESCE(a.mask, 'NO_MASK')
    ) AS id,
    a.user_id,
    li.institution_id,
    a.mask,
    a.name AS account_name_hint,
    a.type AS primary_type,
    a.subtype AS primary_subtype,
    MIN(a.created_at) AS created_at,
    MAX(a.updated_at) AS updated_at
FROM accounts a
JOIN linked_items li ON a.linked_item_id = li.id
GROUP BY
    a.user_id,
    li.institution_id,
    a.mask,
    a.name,
    a.type,
    a.subtype
ON CONFLICT (user_id, institution_id, mask) DO UPDATE SET
    -- Update type/subtype if they've changed (shouldn't happen but good to be safe)
    primary_type = COALESCE(EXCLUDED.primary_type, user_accounts.primary_type),
    primary_subtype = COALESCE(EXCLUDED.primary_subtype, user_accounts.primary_subtype),
    updated_at = NOW();

-- =============================================================================
-- Migration Complete
-- =============================================================================
-- Verify with: SELECT COUNT(*) FROM user_accounts;
