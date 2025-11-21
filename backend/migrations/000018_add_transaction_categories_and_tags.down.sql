-- Rollback: Remove category override and tag support
-- Issue #345: Manual Transaction Category & Tag Editing

-- =============================================================================
-- PART 1: Drop custom_categories table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_custom_categories_updated_at ON custom_categories;

-- Drop indexes
DROP INDEX IF EXISTS idx_custom_categories_name;
DROP INDEX IF EXISTS idx_custom_categories_user_id;

-- Drop table
DROP TABLE IF EXISTS custom_categories CASCADE;

-- =============================================================================
-- PART 2: Drop user_tags table
-- =============================================================================

-- Drop trigger first
DROP TRIGGER IF EXISTS update_user_tags_updated_at ON user_tags;

-- Drop indexes
DROP INDEX IF EXISTS idx_user_tags_name;
DROP INDEX IF EXISTS idx_user_tags_user_id;

-- Drop table
DROP TABLE IF EXISTS user_tags CASCADE;

-- =============================================================================
-- PART 3: Remove columns from transactions table
-- =============================================================================

-- Drop indexes first
DROP INDEX IF EXISTS idx_transactions_tags;
DROP INDEX IF EXISTS idx_transactions_category_source;

-- Drop constraint
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS valid_category_source;

-- Drop columns
ALTER TABLE transactions
    DROP COLUMN IF EXISTS tags_source,
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS original_category,
    DROP COLUMN IF EXISTS category_override_date,
    DROP COLUMN IF EXISTS category_source,
    DROP COLUMN IF EXISTS category_override;
