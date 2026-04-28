-- Rollback 000065_pfc_v2_canonical
-- Restores Plaid v1 legacy JSONB categorization columns and removes PFCv2 typed columns.
-- WARNING: Data stored in pfc_primary, pfc_detailed, override columns is lost on rollback.

-- ==============================================================================
-- PART 0: Restore pfc_enum_to_title_case() function from migration 000064
-- ==============================================================================

CREATE OR REPLACE FUNCTION pfc_enum_to_title_case(val TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
STRICT
AS $$
    SELECT initcap(replace(val, '_', ' '))
$$;

-- ==============================================================================
-- PART 1: transactions — restore legacy columns, drop PFCv2 columns
-- ==============================================================================

-- Drop new PFCv2 indexes
DROP INDEX IF EXISTS idx_transactions_user_date_pfc;
DROP INDEX IF EXISTS idx_transactions_user_date_custom;

-- Drop new FK and constraints
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS fk_transactions_custom_category_override;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_category_override_discriminator;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_pfc_primary_override;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_category_override_kind;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_pfc_confidence_level;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_pfc_detailed_suffix;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_pfc_primary;

-- Drop PFCv2 columns
ALTER TABLE transactions
    DROP COLUMN IF EXISTS custom_category_override_id,
    DROP COLUMN IF EXISTS pfc_detailed_override,
    DROP COLUMN IF EXISTS pfc_primary_override,
    DROP COLUMN IF EXISTS category_override_kind,
    DROP COLUMN IF EXISTS pfc_confidence_level,
    DROP COLUMN IF EXISTS pfc_detailed,
    DROP COLUMN IF EXISTS pfc_primary;

-- Restore legacy columns
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS category JSONB,
    ADD COLUMN IF NOT EXISTS personal_finance_category JSONB,
    ADD COLUMN IF NOT EXISTS category_override JSONB,
    ADD COLUMN IF NOT EXISTS original_category JSONB;

-- ==============================================================================
-- PART 2: budget_categories — restore JSONB category, drop typed columns
-- ==============================================================================

DROP INDEX IF EXISTS idx_budget_categories_budget_pfc;
DROP INDEX IF EXISTS idx_budget_categories_budget_custom;

ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS fk_budget_categories_custom_category;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS unique_budget_category_discriminated;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_discriminator;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_pfc_primary;
ALTER TABLE budget_categories DROP CONSTRAINT IF EXISTS valid_budget_category_kind;

ALTER TABLE budget_categories
    DROP COLUMN IF EXISTS custom_category_id,
    DROP COLUMN IF EXISTS pfc_detailed,
    DROP COLUMN IF EXISTS pfc_primary,
    DROP COLUMN IF EXISTS category_kind;

-- Restore legacy category column
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'budget_categories' AND column_name = 'category') THEN
        ALTER TABLE budget_categories ADD COLUMN category JSONB NOT NULL DEFAULT '[]'::jsonb;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS unique_budget_category ON budget_categories (budget_id, category);
CREATE INDEX IF NOT EXISTS idx_budget_categories_category ON budget_categories USING gin(category);

-- ==============================================================================
-- PART 3: categorization_rules — restore target_category, drop typed columns
-- ==============================================================================

ALTER TABLE categorization_rules DROP CONSTRAINT IF EXISTS fk_rules_target_custom_category;
ALTER TABLE categorization_rules DROP CONSTRAINT IF EXISTS valid_rule_target_kind;

ALTER TABLE categorization_rules
    DROP COLUMN IF EXISTS target_custom_category_id,
    DROP COLUMN IF EXISTS target_pfc_detailed,
    DROP COLUMN IF EXISTS target_pfc_primary,
    DROP COLUMN IF EXISTS target_kind;

ALTER TABLE categorization_rules
    ADD COLUMN IF NOT EXISTS target_category JSONB;

-- ==============================================================================
-- PART 4: custom_categories — restore parent_category, drop parent_pfc_primary
-- ==============================================================================

ALTER TABLE custom_categories DROP CONSTRAINT IF EXISTS valid_custom_category_parent_pfc;
ALTER TABLE custom_categories DROP COLUMN IF EXISTS parent_pfc_primary;
ALTER TABLE custom_categories ADD COLUMN IF NOT EXISTS parent_category VARCHAR(100);
