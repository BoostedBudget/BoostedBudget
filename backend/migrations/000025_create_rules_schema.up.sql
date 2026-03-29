-- Issue #34: Rules Schema & Migrations
-- Create automation rules infrastructure for BoostedBudget
-- Supports: Transfer suggestions, Budget alerts, Category rollover
-- Subject to subscription tier limits (free: 1, premium_lite: 5, premium: 15, premium+: unlimited)
-- This is SEPARATE from categorization_rules (Issue #346) which handles transaction auto-categorization

-- =============================================================================
-- PART 1: Create rules table
-- =============================================================================

CREATE TABLE IF NOT EXISTS rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,

    -- Rule identification and metadata
    name VARCHAR(100) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 0,  -- Higher = evaluated first

    -- Rule type (determines action structure validation)
    rule_type VARCHAR(30) NOT NULL,  -- 'transfer_suggestion', 'budget_alert', 'category_rollover'

    -- Trigger configuration (JSONB for flexibility)
    -- Event-based: {"type": "event", "event": "transaction_created", "filters": {...}}
    -- Schedule-based: {"type": "schedule", "cron": "0 0 1 * *", "timezone": "America/New_York"}
    trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Conditions that must be met for rule to fire
    -- Array format: [{"field": "account.balance", "operator": "gt", "value": 1000}, ...]
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    condition_logic VARCHAR(10) NOT NULL DEFAULT 'AND',  -- 'AND' or 'OR'

    -- Action to take when rule fires (structure depends on rule_type)
    -- Transfer: {"from_account_id": "uuid", "to_account_id": "uuid", "amount_type": "percentage", "amount": 10}
    -- Alert: {"notification_channels": ["email", "push"], "threshold_type": "percentage", "threshold_value": 80}
    -- Rollover: {"source_budget_category_id": "uuid", "rollover_type": "full", "cap_amount": null}
    actions JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Statistics tracking
    execution_count BIGINT NOT NULL DEFAULT 0,
    last_executed_at TIMESTAMP WITH TIME ZONE,
    last_successful_at TIMESTAMP WITH TIME ZONE,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_automation_rule_name_per_user UNIQUE (user_id, name)
);

-- Add CHECK constraint for rule_type values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_rule_type'
    ) THEN
        ALTER TABLE rules
            ADD CONSTRAINT valid_rule_type
            CHECK (rule_type IN ('transfer_suggestion', 'budget_alert', 'category_rollover'));
    END IF;
END $$;

-- Add CHECK constraint for condition_logic values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_rules_condition_logic'
    ) THEN
        ALTER TABLE rules
            ADD CONSTRAINT valid_rules_condition_logic
            CHECK (condition_logic IN ('AND', 'OR'));
    END IF;
END $$;

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE rules ADD CONSTRAINT fk_rules_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE rules VALIDATE CONSTRAINT fk_rules_user_id;
-- Performance indexes for rules table
CREATE INDEX IF NOT EXISTS idx_rules_user_id ON rules(user_id);
CREATE INDEX IF NOT EXISTS idx_rules_user_enabled_priority
    ON rules(user_id, priority DESC) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_rules_type ON rules(rule_type);
CREATE INDEX IF NOT EXISTS idx_rules_last_executed ON rules(last_executed_at DESC);

-- GIN indexes for JSONB columns
CREATE INDEX IF NOT EXISTS idx_rules_trigger_config ON rules USING gin(trigger_config);
CREATE INDEX IF NOT EXISTS idx_rules_conditions ON rules USING gin(conditions);
CREATE INDEX IF NOT EXISTS idx_rules_actions ON rules USING gin(actions);

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_rules_updated_at'
    ) THEN
        CREATE TRIGGER update_rules_updated_at
            BEFORE UPDATE ON rules
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 2: Create rule_executions table
-- =============================================================================

CREATE TABLE IF NOT EXISTS rule_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID NOT NULL,

    -- Execution status and timing
    status VARCHAR(20) NOT NULL DEFAULT 'pending',  -- 'pending', 'running', 'completed', 'failed'
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    execution_duration_ms INTEGER,  -- Performance tracking

    -- Context data (what triggered the execution)
    -- Event: {"event": "transaction_created", "transaction_id": "uuid", "amount": 150}
    -- Schedule: {"scheduled_time": "2024-01-01T00:00:00Z", "actual_time": "2024-01-01T00:00:01Z"}
    trigger_context JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Input/Output data for debugging and auditing
    input_data JSONB,   -- Data evaluated (account balances, budget state)
    output_data JSONB,  -- What was produced (suggestion details, alert sent)

    -- Error tracking
    error_code VARCHAR(50),
    error_message TEXT,

    -- Retry tracking (for failed executions)
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3
);

-- Add CHECK constraint for status values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_execution_status'
    ) THEN
        ALTER TABLE rule_executions
            ADD CONSTRAINT valid_execution_status
            CHECK (status IN ('pending', 'running', 'completed', 'failed'));
    END IF;
END $$;

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE rule_executions ADD CONSTRAINT fk_rule_executions_rule_id FOREIGN KEY (rule_id) REFERENCES rules(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE rule_executions VALIDATE CONSTRAINT fk_rule_executions_rule_id;
-- Performance indexes for rule_executions table
CREATE INDEX IF NOT EXISTS idx_rule_executions_rule_id ON rule_executions(rule_id);
CREATE INDEX IF NOT EXISTS idx_rule_executions_started_at ON rule_executions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_rule_executions_status ON rule_executions(status);

-- Partial index for failed executions (debugging)
CREATE INDEX IF NOT EXISTS idx_rule_executions_failed
    ON rule_executions(rule_id, started_at DESC) WHERE status = 'failed';

-- Partial index for pending executions (processing queue)
CREATE INDEX IF NOT EXISTS idx_rule_executions_pending
    ON rule_executions(started_at) WHERE status = 'pending';

-- =============================================================================
-- PART 3: Create transfer_suggestions table
-- =============================================================================

CREATE TABLE IF NOT EXISTS transfer_suggestions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,

    -- Rule reference (nullable - suggestions can be manual or from rules)
    -- SET NULL on rule/execution delete to preserve historical suggestions
    rule_id UUID,
    rule_execution_id UUID,

    -- Accounts involved (SET NULL on account delete to preserve history)
    from_account_id UUID,
    to_account_id UUID,

    -- Transfer details
    amount DECIMAL(15,2) NOT NULL,
    currency_code VARCHAR(3) NOT NULL DEFAULT 'USD',  -- ISO 4217

    -- Suggestion context
    reason TEXT,  -- Why this transfer is suggested
    source VARCHAR(20) NOT NULL DEFAULT 'rule',  -- 'rule', 'manual', 'ai'

    -- Status tracking
    status VARCHAR(20) NOT NULL DEFAULT 'pending',  -- 'pending', 'accepted', 'rejected', 'expired'
    expires_at TIMESTAMP WITH TIME ZONE,  -- When suggestion auto-expires

    -- User action tracking
    user_action_at TIMESTAMP WITH TIME ZONE,  -- When user accepted/rejected
    user_notes TEXT,  -- Optional notes from user

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Add CHECK constraint for status values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_suggestion_status'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_status
            CHECK (status IN ('pending', 'accepted', 'rejected', 'expired'));
    END IF;
END $$;

-- Add CHECK constraint for positive amount (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'positive_suggestion_amount'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT positive_suggestion_amount
            CHECK (amount > 0);
    END IF;
END $$;

-- Add CHECK constraint for source values (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'valid_suggestion_source'
    ) THEN
        ALTER TABLE transfer_suggestions
            ADD CONSTRAINT valid_suggestion_source
            CHECK (source IN ('rule', 'manual', 'ai'));
    END IF;
END $$;

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE transfer_suggestions ADD CONSTRAINT fk_transfer_suggestions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_suggestions_user_id;
ALTER TABLE transfer_suggestions ADD CONSTRAINT fk_transfer_suggestions_rule_id FOREIGN KEY (rule_id) REFERENCES rules(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_suggestions_rule_id;
ALTER TABLE transfer_suggestions ADD CONSTRAINT fk_transfer_suggestions_rule_execution_id FOREIGN KEY (rule_execution_id) REFERENCES rule_executions(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_suggestions_rule_execution_id;
ALTER TABLE transfer_suggestions ADD CONSTRAINT fk_transfer_suggestions_from_account_id FOREIGN KEY (from_account_id) REFERENCES accounts(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_suggestions_from_account_id;
ALTER TABLE transfer_suggestions ADD CONSTRAINT fk_transfer_suggestions_to_account_id FOREIGN KEY (to_account_id) REFERENCES accounts(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE transfer_suggestions VALIDATE CONSTRAINT fk_transfer_suggestions_to_account_id;
-- Performance indexes for transfer_suggestions table
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_user_id ON transfer_suggestions(user_id);
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_rule_id ON transfer_suggestions(rule_id);
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_status ON transfer_suggestions(status);
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_expires_at ON transfer_suggestions(expires_at);
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_created_at ON transfer_suggestions(created_at DESC);

-- Partial index for pending suggestions (active queue)
CREATE INDEX IF NOT EXISTS idx_transfer_sugg_pending
    ON transfer_suggestions(user_id, created_at DESC) WHERE status = 'pending';

-- Trigger to automatically update updated_at timestamp
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_transfer_suggestions_updated_at'
    ) THEN
        CREATE TRIGGER update_transfer_suggestions_updated_at
            BEFORE UPDATE ON transfer_suggestions
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- =============================================================================
-- PART 4: Comments for documentation
-- =============================================================================

-- Rules table comments
COMMENT ON TABLE rules IS 'User-defined automation rules for transfers, alerts, and rollovers. Subject to subscription tier limits. Issue #34.';
COMMENT ON COLUMN rules.name IS 'Rule name (max 100 chars), unique per user';
COMMENT ON COLUMN rules.priority IS 'Rule evaluation order. Higher priority = evaluated first.';
COMMENT ON COLUMN rules.rule_type IS 'Type of automation: transfer_suggestion, budget_alert, category_rollover';
COMMENT ON COLUMN rules.trigger_config IS 'JSONB trigger configuration. Event-based: {type, event, filters}. Schedule-based: {type, cron, timezone}';
COMMENT ON COLUMN rules.conditions IS 'JSONB array of conditions: [{field, operator, value}]';
COMMENT ON COLUMN rules.condition_logic IS 'How conditions combine: AND (all must match) or OR (any can match)';
COMMENT ON COLUMN rules.actions IS 'JSONB action configuration, structure depends on rule_type';
COMMENT ON COLUMN rules.execution_count IS 'Number of times this rule has been executed';
COMMENT ON COLUMN rules.last_executed_at IS 'When this rule was last triggered (regardless of success)';
COMMENT ON COLUMN rules.last_successful_at IS 'When this rule last completed successfully';

-- Rule executions table comments
COMMENT ON TABLE rule_executions IS 'Execution history for automation rules. Used for debugging, auditing, and retry logic. Issue #34.';
COMMENT ON COLUMN rule_executions.status IS 'Execution status: pending (queued), running (processing), completed (success), failed (error)';
COMMENT ON COLUMN rule_executions.trigger_context IS 'JSONB context about what triggered this execution';
COMMENT ON COLUMN rule_executions.input_data IS 'JSONB snapshot of data evaluated during execution';
COMMENT ON COLUMN rule_executions.output_data IS 'JSONB result of the execution (suggestion created, alert sent, etc.)';
COMMENT ON COLUMN rule_executions.error_code IS 'Machine-readable error code for failed executions';
COMMENT ON COLUMN rule_executions.error_message IS 'Human-readable error description for failed executions';
COMMENT ON COLUMN rule_executions.retry_count IS 'Number of retry attempts for failed executions';
COMMENT ON COLUMN rule_executions.execution_duration_ms IS 'Execution time in milliseconds for performance monitoring';

-- Transfer suggestions table comments
COMMENT ON TABLE transfer_suggestions IS 'Money movement suggestions (no actual transfers per IMPORTANT rule). Can be from rules, manual, or AI. Issue #34.';
COMMENT ON COLUMN transfer_suggestions.rule_id IS 'Rule that created this suggestion (NULL if manual or rule deleted)';
COMMENT ON COLUMN transfer_suggestions.from_account_id IS 'Source account for suggested transfer (NULL if account deleted)';
COMMENT ON COLUMN transfer_suggestions.to_account_id IS 'Destination account for suggested transfer (NULL if account deleted)';
COMMENT ON COLUMN transfer_suggestions.amount IS 'Suggested transfer amount (must be positive)';
COMMENT ON COLUMN transfer_suggestions.reason IS 'Explanation of why this transfer is suggested';
COMMENT ON COLUMN transfer_suggestions.source IS 'Origin: rule (automated), manual (user-created), ai (AI-suggested)';
COMMENT ON COLUMN transfer_suggestions.status IS 'Status: pending (awaiting action), accepted (user confirmed), rejected (dismissed), expired (timed out)';
COMMENT ON COLUMN transfer_suggestions.expires_at IS 'When this suggestion automatically expires (NULL for no expiration)';
COMMENT ON COLUMN transfer_suggestions.user_action_at IS 'When user accepted or rejected this suggestion';
COMMENT ON COLUMN transfer_suggestions.user_notes IS 'Optional notes from user about their action';
