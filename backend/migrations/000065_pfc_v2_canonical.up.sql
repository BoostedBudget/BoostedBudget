-- Drop Plaid v1 legacy categorization and make PFCv2 + custom categories canonical.
-- Replaces JSONB display-string columns (category, personal_finance_category,
-- category_override, original_category) with typed TEXT columns.
-- Introduces a discriminated-union override model and typed budget/rule targets.
-- Custom categories remain a first-class co-equal alternative to PFC.
--
-- Pre-production only: no data backfill, no transition period.
-- Issue #1073

-- ==============================================================================
-- PART 0: Drop pfc_enum_to_title_case() helper from migration 000064
-- ==============================================================================

DROP FUNCTION IF EXISTS pfc_enum_to_title_case(TEXT);

-- ==============================================================================
-- PART 1: transactions — replace legacy categorization with typed PFC columns
-- ==============================================================================

-- 1a. Drop legacy categorization columns (PostgreSQL auto-drops dependent indexes/constraints)
ALTER TABLE transactions
    DROP COLUMN IF EXISTS category,
    DROP COLUMN IF EXISTS personal_finance_category,
    DROP COLUMN IF EXISTS category_override,
    DROP COLUMN IF EXISTS original_category;

-- 1b. Add typed PFC source columns (from Plaid)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'pfc_primary') THEN
        ALTER TABLE transactions ADD COLUMN pfc_primary TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'pfc_detailed') THEN
        ALTER TABLE transactions ADD COLUMN pfc_detailed TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'pfc_confidence_level') THEN
        ALTER TABLE transactions ADD COLUMN pfc_confidence_level TEXT;
    END IF;
END $$;

-- 1c. Add override discriminator columns
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'category_override_kind') THEN
        ALTER TABLE transactions ADD COLUMN category_override_kind TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'pfc_primary_override') THEN
        ALTER TABLE transactions ADD COLUMN pfc_primary_override TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'pfc_detailed_override') THEN
        ALTER TABLE transactions ADD COLUMN pfc_detailed_override TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'transactions' AND column_name = 'custom_category_override_id') THEN
        ALTER TABLE transactions ADD COLUMN custom_category_override_id UUID;
    END IF;
END $$;

-- 1d. CHECK: pfc_primary values (18 PFCv2 primaries)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_pfc_primary') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_pfc_primary
            CHECK (pfc_primary IS NULL OR pfc_primary IN (
                'BANK_FEES','ENTERTAINMENT','FOOD_AND_DRINK','GENERAL_MERCHANDISE',
                'GENERAL_SERVICES','GOVERNMENT_AND_NON_PROFIT','HOME_IMPROVEMENT',
                'INCOME','LOAN_DISBURSEMENTS','LOAN_PAYMENTS','MEDICAL','OTHER',
                'PERSONAL_CARE','RENT_AND_UTILITIES','TRANSFER_IN','TRANSFER_OUT',
                'TRANSPORTATION','TRAVEL'
            ));
    END IF;
END $$;

-- 1e. CHECK: pfc_detailed must start with pfc_primary + '_'
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_pfc_detailed_suffix') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_pfc_detailed_suffix
            CHECK (pfc_detailed IS NULL OR (pfc_primary IS NOT NULL AND pfc_detailed LIKE pfc_primary || '_%'));
    END IF;
END $$;

-- 1f. CHECK: pfc_confidence_level values
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_pfc_confidence_level') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_pfc_confidence_level
            CHECK (pfc_confidence_level IS NULL OR pfc_confidence_level IN ('VERY_HIGH','HIGH','MEDIUM','LOW','UNKNOWN'));
    END IF;
END $$;

-- 1g. CHECK: category_override_kind discriminator values
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_category_override_kind') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_category_override_kind
            CHECK (category_override_kind IS NULL OR category_override_kind IN ('pfc','custom'));
    END IF;
END $$;

-- 1h. CHECK: pfc_primary_override enum set (same 18 primaries)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_pfc_primary_override') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_pfc_primary_override
            CHECK (pfc_primary_override IS NULL OR pfc_primary_override IN (
                'BANK_FEES','ENTERTAINMENT','FOOD_AND_DRINK','GENERAL_MERCHANDISE',
                'GENERAL_SERVICES','GOVERNMENT_AND_NON_PROFIT','HOME_IMPROVEMENT',
                'INCOME','LOAN_DISBURSEMENTS','LOAN_PAYMENTS','MEDICAL','OTHER',
                'PERSONAL_CARE','RENT_AND_UTILITIES','TRANSFER_IN','TRANSFER_OUT',
                'TRANSPORTATION','TRAVEL'
            ));
    END IF;
END $$;

-- 1i. Multi-branch discriminator CHECK: each branch is fully specified.
--     category_source (who set override) is orthogonal to category_override_kind (which dimension).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_category_override_discriminator') THEN
        ALTER TABLE transactions ADD CONSTRAINT valid_category_override_discriminator
            CHECK (
                (category_override_kind IS NULL
                    AND pfc_primary_override IS NULL
                    AND pfc_detailed_override IS NULL
                    AND custom_category_override_id IS NULL)
                OR
                (category_override_kind = 'pfc'
                    AND pfc_primary_override IS NOT NULL
                    AND custom_category_override_id IS NULL)
                OR
                (category_override_kind = 'custom'
                    AND custom_category_override_id IS NOT NULL
                    AND pfc_primary_override IS NULL
                    AND pfc_detailed_override IS NULL)
            );
    END IF;
END $$;

-- 1j. FK: transactions.custom_category_override_id → custom_categories(id)
--     ON DELETE RESTRICT: prevents deleting a custom_category while it is the active override,
--     preserving the discriminator invariant (kind='custom' + NULL ref would fail CHECK 1i).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_transactions_custom_category_override'
    ) THEN
        ALTER TABLE transactions
            ADD CONSTRAINT fk_transactions_custom_category_override
            FOREIGN KEY (custom_category_override_id)
            REFERENCES custom_categories(id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;
END $$;
ALTER TABLE transactions VALIDATE CONSTRAINT fk_transactions_custom_category_override;

-- 1k. Partial functional indexes for the two effective-category discriminator branches
CREATE INDEX IF NOT EXISTS idx_transactions_user_date_pfc
    ON transactions (user_id, date, COALESCE(pfc_primary_override, pfc_primary))
    WHERE category_override_kind IS NULL OR category_override_kind = 'pfc';

CREATE INDEX IF NOT EXISTS idx_transactions_user_date_custom
    ON transactions (user_id, date, custom_category_override_id)
    WHERE category_override_kind = 'custom';

-- ==============================================================================
-- PART 2: budget_categories — replace JSONB category with typed discriminated columns
-- ==============================================================================

-- 2a. Drop legacy category column (PostgreSQL auto-drops unique_budget_category and its GIN index)
ALTER TABLE budget_categories
    DROP COLUMN IF EXISTS category;

-- 2b. Add typed category columns
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'budget_categories' AND column_name = 'category_kind') THEN
        ALTER TABLE budget_categories ADD COLUMN category_kind TEXT NOT NULL DEFAULT 'pfc';
        -- Remove default after adding so future inserts must be explicit
        ALTER TABLE budget_categories ALTER COLUMN category_kind DROP DEFAULT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'budget_categories' AND column_name = 'pfc_primary') THEN
        ALTER TABLE budget_categories ADD COLUMN pfc_primary TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'budget_categories' AND column_name = 'pfc_detailed') THEN
        ALTER TABLE budget_categories ADD COLUMN pfc_detailed TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'budget_categories' AND column_name = 'custom_category_id') THEN
        ALTER TABLE budget_categories ADD COLUMN custom_category_id UUID;
    END IF;
END $$;

-- 2c. CHECK: category_kind values
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_budget_category_kind') THEN
        ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_kind
            CHECK (category_kind IN ('pfc','custom'));
    END IF;
END $$;

-- 2d. CHECK: pfc_primary enum set (when category_kind='pfc')
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_budget_category_pfc_primary') THEN
        ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_pfc_primary
            CHECK (pfc_primary IS NULL OR pfc_primary IN (
                'BANK_FEES','ENTERTAINMENT','FOOD_AND_DRINK','GENERAL_MERCHANDISE',
                'GENERAL_SERVICES','GOVERNMENT_AND_NON_PROFIT','HOME_IMPROVEMENT',
                'INCOME','LOAN_DISBURSEMENTS','LOAN_PAYMENTS','MEDICAL','OTHER',
                'PERSONAL_CARE','RENT_AND_UTILITIES','TRANSFER_IN','TRANSFER_OUT',
                'TRANSPORTATION','TRAVEL'
            ));
    END IF;
END $$;

-- 2e. Multi-branch discriminator CHECK
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_budget_category_discriminator') THEN
        ALTER TABLE budget_categories ADD CONSTRAINT valid_budget_category_discriminator
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

-- 2f. FK: budget_categories.custom_category_id → custom_categories(id)
--     ON DELETE CASCADE: removing a custom_category removes the budget entry targeting it.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_budget_categories_custom_category'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT fk_budget_categories_custom_category
            FOREIGN KEY (custom_category_id)
            REFERENCES custom_categories(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END $$;
ALTER TABLE budget_categories VALIDATE CONSTRAINT fk_budget_categories_custom_category;

-- 2g. New UNIQUE constraint (replaces old UNIQUE(budget_id, category))
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'unique_budget_category_discriminated'
    ) THEN
        ALTER TABLE budget_categories
            ADD CONSTRAINT unique_budget_category_discriminated
            UNIQUE (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id);
    END IF;
END $$;

-- 2h. Partial indexes for budget matching queries
CREATE INDEX IF NOT EXISTS idx_budget_categories_budget_pfc
    ON budget_categories (budget_id, pfc_primary, pfc_detailed)
    WHERE category_kind = 'pfc';

CREATE INDEX IF NOT EXISTS idx_budget_categories_budget_custom
    ON budget_categories (budget_id, custom_category_id)
    WHERE category_kind = 'custom';

-- ==============================================================================
-- PART 3: categorization_rules — replace JSONB target_category with typed columns
-- ==============================================================================

-- 3a. Drop legacy target_category column
ALTER TABLE categorization_rules
    DROP COLUMN IF EXISTS target_category;

-- 3b. Add typed target columns
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'categorization_rules' AND column_name = 'target_kind') THEN
        ALTER TABLE categorization_rules ADD COLUMN target_kind TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'categorization_rules' AND column_name = 'target_pfc_primary') THEN
        ALTER TABLE categorization_rules ADD COLUMN target_pfc_primary TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'categorization_rules' AND column_name = 'target_pfc_detailed') THEN
        ALTER TABLE categorization_rules ADD COLUMN target_pfc_detailed TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'categorization_rules' AND column_name = 'target_custom_category_id') THEN
        ALTER TABLE categorization_rules ADD COLUMN target_custom_category_id UUID;
    END IF;
END $$;

-- 3c. CHECK: target_kind values (NULL = no category action on this rule)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_rule_target_kind') THEN
        ALTER TABLE categorization_rules ADD CONSTRAINT valid_rule_target_kind
            CHECK (target_kind IS NULL OR target_kind IN ('pfc','custom','clear'));
    END IF;
END $$;

-- 3d. FK: categorization_rules.target_custom_category_id → custom_categories(id)
--     ON DELETE SET NULL: rule continues to exist but loses its target (user must re-configure).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_rules_target_custom_category'
    ) THEN
        ALTER TABLE categorization_rules
            ADD CONSTRAINT fk_rules_target_custom_category
            FOREIGN KEY (target_custom_category_id)
            REFERENCES custom_categories(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;
ALTER TABLE categorization_rules VALIDATE CONSTRAINT fk_rules_target_custom_category;

-- ==============================================================================
-- PART 4: custom_categories — drop legacy parent_category, add parent_pfc_primary
-- ==============================================================================

ALTER TABLE custom_categories
    DROP COLUMN IF EXISTS parent_category;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'custom_categories' AND column_name = 'parent_pfc_primary') THEN
        ALTER TABLE custom_categories ADD COLUMN parent_pfc_primary TEXT;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.check_constraints WHERE constraint_name = 'valid_custom_category_parent_pfc') THEN
        ALTER TABLE custom_categories ADD CONSTRAINT valid_custom_category_parent_pfc
            CHECK (parent_pfc_primary IS NULL OR parent_pfc_primary IN (
                'BANK_FEES','ENTERTAINMENT','FOOD_AND_DRINK','GENERAL_MERCHANDISE',
                'GENERAL_SERVICES','GOVERNMENT_AND_NON_PROFIT','HOME_IMPROVEMENT',
                'INCOME','LOAN_DISBURSEMENTS','LOAN_PAYMENTS','MEDICAL','OTHER',
                'PERSONAL_CARE','RENT_AND_UTILITIES','TRANSFER_IN','TRANSFER_OUT',
                'TRANSPORTATION','TRAVEL'
            ));
    END IF;
END $$;

-- ==============================================================================
-- PART 5: Column comments
-- ==============================================================================

COMMENT ON COLUMN transactions.pfc_primary IS 'Plaid PFCv2 primary enum (e.g. FOOD_AND_DRINK). NULL for manual transactions and rare responses without PFC.';
COMMENT ON COLUMN transactions.pfc_detailed IS 'Plaid PFCv2 detailed enum. Must be prefixed with pfc_primary + ''_''.';
COMMENT ON COLUMN transactions.pfc_confidence_level IS 'Plaid PFCv2 confidence: VERY_HIGH / HIGH / MEDIUM / LOW / UNKNOWN.';
COMMENT ON COLUMN transactions.category_override_kind IS 'Discriminator for override dimension: NULL=no override, pfc=PFC override, custom=custom category. Orthogonal to category_source which tracks who made the change.';
COMMENT ON COLUMN transactions.pfc_primary_override IS 'Override PFC primary. Set only when category_override_kind=''pfc''.';
COMMENT ON COLUMN transactions.pfc_detailed_override IS 'Override PFC detailed. Set only when category_override_kind=''pfc''.';
COMMENT ON COLUMN transactions.custom_category_override_id IS 'FK to custom_categories. Set only when category_override_kind=''custom''. RESTRICT prevents deleting active override targets.';
COMMENT ON COLUMN transactions.category_source IS 'Who set the override: plaid (default/none), rule (automation), manual (user). Orthogonal to category_override_kind.';

COMMENT ON COLUMN budget_categories.category_kind IS 'Discriminator: pfc or custom. Determines which matching sub-columns are active.';
COMMENT ON COLUMN budget_categories.pfc_primary IS 'PFC primary enum to match against. Required when category_kind=''pfc''.';
COMMENT ON COLUMN budget_categories.pfc_detailed IS 'Optional PFC detailed refinement. Only when category_kind=''pfc''.';
COMMENT ON COLUMN budget_categories.custom_category_id IS 'FK to custom_categories. Required when category_kind=''custom''. CASCADE removes budget entry when custom category is deleted.';

COMMENT ON COLUMN custom_categories.parent_pfc_primary IS 'Optional PFCv2 primary hint for icon/color fallback resolution. Does not restrict usage.';
