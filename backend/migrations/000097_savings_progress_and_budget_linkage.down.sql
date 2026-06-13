-- Issue #1300: Reverse savings progress tracking + budget↔savings linkage migration.
-- NOTE: This is destructive — the savings_goal_contributions and
-- savings_goal_category_detach tables (and all data in them) are permanently dropped.

-- =============================================================================
-- PART 4 reversal: budget_categories — remove savings_goal_id, auto_created, managed_by_goal_id
-- =============================================================================

-- Restore 5-column unique index
DROP INDEX IF EXISTS unique_budget_category_discriminated;
DROP INDEX IF EXISTS idx_budget_categories_budget_savings;

CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category_discriminated
    ON budget_categories (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id)
    NULLS NOT DISTINCT;

-- Restore discriminator CHECK to original pfc/custom-only form
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_discriminator'
    ) THEN
        ALTER TABLE budget_categories DROP CONSTRAINT valid_budget_category_discriminator;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_discriminator'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT valid_budget_category_discriminator
            CHECK (
                (category_kind = 'pfc'
                    AND pfc_primary IS NOT NULL
                    AND custom_category_id IS NULL)
                OR
                (category_kind = 'custom'
                    AND custom_category_id IS NOT NULL
                    AND pfc_primary IS NULL
                    AND pfc_detailed IS NULL)
            );
    END IF;
END $$;

-- Restore kind CHECK to pfc/custom only
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_kind'
    ) THEN
        ALTER TABLE budget_categories DROP CONSTRAINT valid_budget_category_kind;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_budget_category_kind'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT valid_budget_category_kind
            CHECK (category_kind IN ('pfc', 'custom'));
    END IF;
END $$;

-- Drop savings-related FKs and columns
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS fk_budget_categories_managed_by_goal;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS fk_budget_categories_savings_goal;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS managed_by_goal_id;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS auto_created;
ALTER TABLE budget_categories DROP COLUMN IF EXISTS savings_goal_id;

-- =============================================================================
-- PART 3 reversal: drop savings_goal_category_detach tombstone table
-- =============================================================================

DROP TABLE IF EXISTS savings_goal_category_detach;

-- =============================================================================
-- PART 2 reversal: drop savings_goal_contributions table
-- =============================================================================

DROP TABLE IF EXISTS savings_goal_contributions;

-- =============================================================================
-- PART 1 reversal: savings_goals — remove progress tracking columns
-- =============================================================================

ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS valid_savings_goal_basis_requires_account;
ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS valid_savings_goal_percentage_discriminator;
ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS valid_savings_goal_account_percentage;
ALTER TABLE savings_goals DROP CONSTRAINT IF EXISTS valid_savings_goal_progress_basis;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS balance_as_of;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS last_calculated_at;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS account_percentage;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS progress_basis;
