-- Create categorization rules tables
-- Issue #346: Custom Transaction Categorization Rules (Automated Category Assignment)
-- Rules can set categories (first-match-wins) and/or tags (all-match)

-- =============================================================================
-- PART 1: Create categorization_rules table
-- =============================================================================

CREATE TABLE IF NOT EXISTS categorization_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 0,  -- Higher = evaluated first

    -- Conditions (JSONB for flexible schema evolution)
    -- Example: [{"type": "merchant_contains", "value": "starbucks"}, {"type": "amount_less_than", "value": 20}]
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    condition_logic VARCHAR(10) NOT NULL DEFAULT 'AND',

    -- Actions (at least one must be set via constraint)
    target_category JSONB,  -- ["Food & Drink", "Coffee Shops"] or null
    target_tags JSONB DEFAULT '[]'::jsonb,  -- ["recurring", "small-purchase"]
    tag_behavior VARCHAR(20) NOT NULL DEFAULT 'add',

    -- Source tracking (AI integration ready - Issue #114)
    source VARCHAR(20) NOT NULL DEFAULT 'manual',

    -- Effectiveness metrics
    match_count BIGINT NOT NULL DEFAULT 0,
    override_count BIGINT NOT NULL DEFAULT 0,
    last_matched_at TIMESTAMP WITH TIME ZONE,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_rule_name_per_user UNIQUE (user_id, name)
);

-- Add constraint for condition_logic values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_condition_logic'
    ) THEN
        ALTER TABLE categorization_rules
            ADD CONSTRAINT valid_condition_logic
            CHECK (condition_logic IN ('AND', 'OR'));
    END IF;
END $$;

-- Add constraint for tag_behavior values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_tag_behavior'
    ) THEN
        ALTER TABLE categorization_rules
            ADD CONSTRAINT valid_tag_behavior
            CHECK (tag_behavior IN ('add', 'replace'));
    END IF;
END $$;

-- Add constraint for source values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_rule_source'
    ) THEN
        ALTER TABLE categorization_rules
            ADD CONSTRAINT valid_rule_source
            CHECK (source IN ('manual', 'ai', 'template'));
    END IF;
END $$;

-- Add constraint requiring at least one action (category or tags)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'has_action'
    ) THEN
        ALTER TABLE categorization_rules
            ADD CONSTRAINT has_action
            CHECK (target_category IS NOT NULL OR jsonb_array_length(target_tags) > 0);
    END IF;
END $$;

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE categorization_rules ADD CONSTRAINT fk_categorization_rules_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE categorization_rules VALIDATE CONSTRAINT fk_categorization_rules_user_id;
-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_cat_rules_user_enabled_priority
    ON categorization_rules(user_id, priority DESC) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_cat_rules_user_id ON categorization_rules(user_id);
CREATE INDEX IF NOT EXISTS idx_cat_rules_source ON categorization_rules(source);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_categorization_rules_updated_at'
    ) THEN
        CREATE TRIGGER update_categorization_rules_updated_at
            BEFORE UPDATE ON categorization_rules
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 2: Create rule_execution_history table (for debugging and Test Rule feature)
-- =============================================================================

CREATE TABLE IF NOT EXISTS rule_execution_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID NOT NULL,
    transaction_id UUID NOT NULL,
    action_type VARCHAR(20) NOT NULL,  -- 'category', 'tags', 'both'
    result VARCHAR(20) NOT NULL,  -- 'applied', 'skipped_manual'
    executed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- One execution per rule per transaction
    CONSTRAINT unique_rule_transaction UNIQUE (rule_id, transaction_id)
);

-- Add constraint for action_type values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_action_type'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT valid_action_type
            CHECK (action_type IN ('category', 'tags', 'both'));
    END IF;
END $$;

-- Add constraint for result values
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_execution_result'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT valid_execution_result
            CHECK (result IN ('applied', 'skipped_manual'));
    END IF;
END $$;

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE rule_execution_history ADD CONSTRAINT fk_rule_execution_history_rule_id FOREIGN KEY (rule_id) REFERENCES categorization_rules(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE rule_execution_history VALIDATE CONSTRAINT fk_rule_execution_history_rule_id;
ALTER TABLE rule_execution_history ADD CONSTRAINT fk_rule_execution_history_transaction_id FOREIGN KEY (transaction_id) REFERENCES transactions(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE rule_execution_history VALIDATE CONSTRAINT fk_rule_execution_history_transaction_id;
-- Indexes for rule execution history
CREATE INDEX IF NOT EXISTS idx_rule_exec_rule_id ON rule_execution_history(rule_id);
CREATE INDEX IF NOT EXISTS idx_rule_exec_transaction_id ON rule_execution_history(transaction_id);
CREATE INDEX IF NOT EXISTS idx_rule_exec_executed_at ON rule_execution_history(executed_at DESC);

-- =============================================================================
-- PART 3: Comments for documentation
-- =============================================================================

COMMENT ON TABLE categorization_rules IS 'User-defined rules for automatic transaction categorization. Subject to subscription tier limits.';
COMMENT ON COLUMN categorization_rules.name IS 'User-friendly rule name (max 100 chars), unique per user';
COMMENT ON COLUMN categorization_rules.priority IS 'Rule evaluation order. Higher priority = evaluated first. For categories, first match wins.';
COMMENT ON COLUMN categorization_rules.conditions IS 'JSONB array of condition objects: [{type, value, operator}]';
COMMENT ON COLUMN categorization_rules.condition_logic IS 'How conditions combine: AND (all must match) or OR (any can match)';
COMMENT ON COLUMN categorization_rules.target_category IS 'Category to assign on match (array of strings like Plaid categories)';
COMMENT ON COLUMN categorization_rules.target_tags IS 'Tags to add on match (array of tag names)';
COMMENT ON COLUMN categorization_rules.tag_behavior IS 'add (append to existing) or replace (clear rule-added tags first)';
COMMENT ON COLUMN categorization_rules.source IS 'Origin of rule: manual (user-created), ai (AI-suggested), template (from predefined)';
COMMENT ON COLUMN categorization_rules.match_count IS 'Number of transactions this rule has matched';
COMMENT ON COLUMN categorization_rules.override_count IS 'Number of times user manually overrode this rule category';

COMMENT ON TABLE rule_execution_history IS 'Audit log of rule executions for debugging and the Test Rule feature';
COMMENT ON COLUMN rule_execution_history.action_type IS 'What action was taken: category, tags, or both';
COMMENT ON COLUMN rule_execution_history.result IS 'Outcome: applied (rule was applied) or skipped_manual (user had manual category)';
