-- Migration: Create dashboard preferences table for customizable widget layouts
-- Issue #354: Cash Flow Analysis + Customizable Dashboard

-- Create the dashboard preferences table
CREATE TABLE IF NOT EXISTS user_dashboard_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    layout JSONB NOT NULL DEFAULT '[]',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    CONSTRAINT unique_user_dashboard UNIQUE (user_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE user_dashboard_preferences ADD CONSTRAINT fk_user_dashboard_preferences_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE user_dashboard_preferences VALIDATE CONSTRAINT fk_user_dashboard_preferences_user_id;
-- Index for fast user lookup
CREATE INDEX IF NOT EXISTS idx_dashboard_preferences_user_id
ON user_dashboard_preferences(user_id);

-- Performance index for cash flow queries on transactions
-- This optimizes the cash flow aggregation queries that filter by user_id, date range, and pending status
CREATE INDEX IF NOT EXISTS idx_transactions_cashflow
ON transactions (user_id, date, pending)
WHERE pending = false;

-- Add comment for documentation
COMMENT ON TABLE user_dashboard_preferences IS 'Stores user dashboard widget layout preferences for the customizable dashboard';
COMMENT ON COLUMN user_dashboard_preferences.layout IS 'JSONB array of widget configurations with position and size';
COMMENT ON COLUMN user_dashboard_preferences.version IS 'Layout schema version for future migrations';
