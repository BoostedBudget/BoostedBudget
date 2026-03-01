-- Add category override and tag support to transactions
-- Issue #345: Manual Transaction Category & Tag Editing
-- FREE for all tiers - no paywalls on editing functionality

-- =============================================================================
-- PART 1: Add category override columns to transactions table
-- =============================================================================

-- Add category override fields (for user-edited categories)
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS category_override JSONB,
    ADD COLUMN IF NOT EXISTS category_source VARCHAR(20) DEFAULT 'plaid',
    ADD COLUMN IF NOT EXISTS category_override_date TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS original_category JSONB;

-- Add tags fields (for user and rule-added tags)
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS tags JSONB DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS tags_source JSONB DEFAULT '[]'::jsonb;

-- Add constraint for category_source values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_category_source'
    ) THEN
        ALTER TABLE transactions
            ADD CONSTRAINT valid_category_source
            CHECK (category_source IN ('plaid', 'rule', 'manual'));
    END IF;
END $$;

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_transactions_category_source ON transactions(category_source);
CREATE INDEX IF NOT EXISTS idx_transactions_tags ON transactions USING gin(tags);

-- =============================================================================
-- PART 2: Create user_tags table (FREE for all tiers)
-- =============================================================================

CREATE TABLE IF NOT EXISTS user_tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    color VARCHAR(7), -- Hex color like #FF5733
    usage_count INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Each user can have unique tag names
    CONSTRAINT unique_user_tag_name UNIQUE(user_id, name)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE user_tags ADD CONSTRAINT fk_user_tags_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE user_tags VALIDATE CONSTRAINT fk_user_tags_user_id;
-- Indexes for user_tags
CREATE INDEX IF NOT EXISTS idx_user_tags_user_id ON user_tags(user_id);
CREATE INDEX IF NOT EXISTS idx_user_tags_name ON user_tags(user_id, name);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_user_tags_updated_at'
    ) THEN
        CREATE TRIGGER update_user_tags_updated_at
            BEFORE UPDATE ON user_tags
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 3: Create custom_categories table (FREE for all tiers)
-- =============================================================================

CREATE TABLE IF NOT EXISTS custom_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    parent_category VARCHAR(100), -- Maps to Plaid parent category
    icon VARCHAR(50),
    color VARCHAR(7), -- Hex color like #FF5733
    usage_count INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Each user can have unique category names
    CONSTRAINT unique_user_category_name UNIQUE(user_id, name)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE custom_categories ADD CONSTRAINT fk_custom_categories_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE custom_categories VALIDATE CONSTRAINT fk_custom_categories_user_id;
-- Indexes for custom_categories
CREATE INDEX IF NOT EXISTS idx_custom_categories_user_id ON custom_categories(user_id);
CREATE INDEX IF NOT EXISTS idx_custom_categories_name ON custom_categories(user_id, name);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_custom_categories_updated_at'
    ) THEN
        CREATE TRIGGER update_custom_categories_updated_at
            BEFORE UPDATE ON custom_categories
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 4: Comments for documentation
-- =============================================================================

COMMENT ON COLUMN transactions.category_override IS 'User-selected category override (array of strings like original category)';
COMMENT ON COLUMN transactions.category_source IS 'Source of current category: plaid (original), rule (auto-applied), manual (user-edited)';
COMMENT ON COLUMN transactions.category_override_date IS 'Timestamp when category was last manually edited';
COMMENT ON COLUMN transactions.original_category IS 'Preserved original Plaid category (set on first manual edit)';
COMMENT ON COLUMN transactions.tags IS 'Array of tag names applied to this transaction';
COMMENT ON COLUMN transactions.tags_source IS 'Array of objects tracking each tag source: {tag, source, rule_id}';

COMMENT ON TABLE user_tags IS 'User-defined tags for transaction organization. FREE for all subscription tiers.';
COMMENT ON COLUMN user_tags.name IS 'Tag name (max 100 chars), unique per user';
COMMENT ON COLUMN user_tags.color IS 'Hex color code for visual organization (e.g., #FF5733)';
COMMENT ON COLUMN user_tags.usage_count IS 'Number of transactions using this tag';

COMMENT ON TABLE custom_categories IS 'User-defined custom categories. FREE for all subscription tiers.';
COMMENT ON COLUMN custom_categories.parent_category IS 'Optional mapping to standard Plaid parent category';
COMMENT ON COLUMN custom_categories.icon IS 'Icon identifier for UI display';
COMMENT ON COLUMN custom_categories.usage_count IS 'Number of transactions using this category';
