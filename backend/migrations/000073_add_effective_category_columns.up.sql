-- Add effective_pfc_primary, effective_pfc_detailed (generated columns) and
-- effective_category_name (app-maintained) to the transactions table.
--
-- IMPORTANT: ADD COLUMN ... GENERATED ALWAYS AS ... STORED requires a full table
-- rewrite in PostgreSQL. This migration should be deployed during a maintenance window
-- or low-traffic period on large production tables. This is acknowledged in the design.
--
-- Issue #1123

-- ==============================================================================
-- PART 1: Add generated columns for effective PFC values
-- ==============================================================================

-- effective_pfc_primary: resolves the active PFC primary category, respecting overrides.
-- Returns NULL when category_override_kind='custom' (custom category takes precedence over PFC).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'effective_pfc_primary'
    ) THEN
        ALTER TABLE transactions ADD COLUMN effective_pfc_primary TEXT GENERATED ALWAYS AS (
            CASE
                WHEN category_override_kind = 'custom' THEN NULL
                WHEN category_override_kind = 'pfc'    THEN pfc_primary_override
                ELSE pfc_primary
            END
        ) STORED;
    END IF;
END $$;

-- effective_pfc_detailed: resolves the active PFC detailed category, respecting overrides.
-- Returns NULL when category_override_kind='custom'.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'effective_pfc_detailed'
    ) THEN
        ALTER TABLE transactions ADD COLUMN effective_pfc_detailed TEXT GENERATED ALWAYS AS (
            CASE
                WHEN category_override_kind = 'custom' THEN NULL
                WHEN category_override_kind = 'pfc'    THEN pfc_detailed_override
                ELSE pfc_detailed
            END
        ) STORED;
    END IF;
END $$;

-- ==============================================================================
-- PART 2: Add app-maintained effective_category_name column
-- ==============================================================================

-- effective_category_name: resolved category identifier for the effective category.
-- For PFC kind: the raw PFC enum string (e.g. 'FOOD_AND_DRINK'). Display conversion is frontend responsibility.
-- For custom kind: the custom category name from custom_categories.name.
-- NULL until backfilled below. App maintains this in sync with override changes.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'transactions' AND column_name = 'effective_category_name'
    ) THEN
        ALTER TABLE transactions ADD COLUMN effective_category_name TEXT;
    END IF;
END $$;

-- ==============================================================================
-- PART 3: Partial indexes on effective category columns
-- ==============================================================================

CREATE INDEX IF NOT EXISTS idx_transactions_effective_pfc_primary
    ON transactions (user_id, effective_pfc_primary)
    WHERE effective_pfc_primary IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_transactions_effective_category_name
    ON transactions (user_id, effective_category_name)
    WHERE effective_category_name IS NOT NULL;

-- ==============================================================================
-- PART 4: Batched backfill of effective_category_name
-- ==============================================================================
-- Backfills effective_category_name for all existing rows in batches of 1000
-- with SKIP LOCKED + pg_sleep(0.05) to avoid long-held table locks.
-- Idempotent: only touches rows where effective_category_name IS NULL.

DO $$
DECLARE
    rows_updated INT;
BEGIN
    LOOP
        WITH batch AS (
            SELECT id FROM transactions
            WHERE effective_category_name IS NULL
            LIMIT 1000
            FOR UPDATE SKIP LOCKED
        )
        UPDATE transactions t
        SET effective_category_name = CASE
            WHEN t.category_override_kind = 'custom' THEN
                (SELECT name FROM custom_categories WHERE id = t.custom_category_override_id AND user_id = t.user_id)
            WHEN t.category_override_kind = 'pfc' THEN
                t.pfc_primary_override
            ELSE
                t.pfc_primary
        END
        FROM batch WHERE t.id = batch.id;

        GET DIAGNOSTICS rows_updated = ROW_COUNT;
        EXIT WHEN rows_updated = 0;
        PERFORM pg_sleep(0.05);
    END LOOP;
END $$;

-- ==============================================================================
-- PART 5: Column comments
-- ==============================================================================

COMMENT ON COLUMN transactions.effective_pfc_primary IS
    'Generated: resolves active PFC primary category. NULL when category_override_kind=''custom''. '
    'Equals pfc_primary_override when kind=''pfc'', pfc_primary otherwise.';

COMMENT ON COLUMN transactions.effective_pfc_detailed IS
    'Generated: resolves active PFC detailed category. NULL when category_override_kind=''custom''. '
    'Equals pfc_detailed_override when kind=''pfc'', pfc_detailed otherwise.';

COMMENT ON COLUMN transactions.effective_category_name IS
    'App-maintained resolved category identifier. '
    'PFC kind: raw PFC enum string (e.g. FOOD_AND_DRINK); display conversion is a frontend concern. '
    'Custom kind: custom_categories.name for the assigned custom category. '
    'App must keep this in sync whenever category_override_kind or override columns change.';
