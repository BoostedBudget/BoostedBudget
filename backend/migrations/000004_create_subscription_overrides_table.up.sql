-- Create user_subscription_overrides table for custom tier limits
-- Used for: enterprise deals, add-on purchases, promotional upgrades
-- Allows per-user custom limits that override base tier limits

CREATE TABLE IF NOT EXISTS user_subscription_overrides (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL,
    resource_type VARCHAR(100) NOT NULL,  -- 'accounts', 'rules', 'executions_per_day', etc.
    limit_value INTEGER NOT NULL,          -- -1 for unlimited
    reason TEXT,                           -- 'enterprise_deal', 'addon_purchase', 'promo_code_XYZ'
    expires_at TIMESTAMP,                  -- NULL for permanent overrides
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Foreign key to users table
    CONSTRAINT fk_user_subscription_overrides_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,

    -- Unique constraint: one override per user per resource type
    CONSTRAINT unique_user_resource_override
        UNIQUE (user_id, resource_type)
);

-- Indexes for performance
CREATE INDEX idx_user_subscription_overrides_user_id
    ON user_subscription_overrides(user_id);

CREATE INDEX idx_user_subscription_overrides_expires_at
    ON user_subscription_overrides(expires_at)
    WHERE expires_at IS NOT NULL;

-- Comments for documentation
COMMENT ON TABLE user_subscription_overrides IS 'Custom subscription limits per user for enterprise deals, add-ons, and promotions';
COMMENT ON COLUMN user_subscription_overrides.resource_type IS 'Type of resource: accounts, rules, executions_per_day, api_calls_per_month, data_export_requests_per_month';
COMMENT ON COLUMN user_subscription_overrides.limit_value IS 'Custom limit value, use -1 for unlimited';
COMMENT ON COLUMN user_subscription_overrides.reason IS 'Why this override was applied: enterprise_deal, addon_purchase, promo_XXXX';
COMMENT ON COLUMN user_subscription_overrides.expires_at IS 'When this override expires, NULL for permanent overrides';
