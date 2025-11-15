CREATE TABLE IF NOT EXISTS users (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Auth0 user ID (from JWT sub claim)
    auth0_user_id VARCHAR(255) NOT NULL UNIQUE,

    -- Basic user information
    email VARCHAR(255) NOT NULL UNIQUE,
    email_verified BOOLEAN DEFAULT FALSE,

    -- GDPR: Primary region for data residency
    -- Default to 'us-east-1' for MVP (single region)
    -- Future: 'eu-west-1', 'ap-southeast-1', etc.
    primary_region VARCHAR(50) NOT NULL DEFAULT 'us-east-1',

    -- Subscription information
    subscription_tier VARCHAR(50) NOT NULL DEFAULT 'free',
    -- Possible values: 'free', 'premium', 'premium_plus'

    subscription_status VARCHAR(50) NOT NULL DEFAULT 'active',
    -- Possible values: 'active', 'cancelled', 'past_due', 'unpaid'

    subscription_started_at TIMESTAMP WITH TIME ZONE,
    subscription_ends_at TIMESTAMP WITH TIME ZONE,

    -- Stripe customer ID (for payments)
    stripe_customer_id VARCHAR(255),

    -- Account status
    is_active BOOLEAN DEFAULT TRUE,
    is_deleted BOOLEAN DEFAULT FALSE,

    -- GDPR: Account deletion
    deletion_requested_at TIMESTAMP WITH TIME ZONE,
    deletion_scheduled_at TIMESTAMP WITH TIME ZONE,
    -- 30-day grace period before permanent deletion

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMP WITH TIME ZONE,

    -- Constraints
    CONSTRAINT valid_subscription_tier
        CHECK (subscription_tier IN ('free', 'premium', 'premium_plus')),
    CONSTRAINT valid_subscription_status
        CHECK (subscription_status IN ('active', 'cancelled', 'past_due', 'unpaid')),
    CONSTRAINT valid_primary_region
        CHECK (primary_region IN ('us-east-1', 'eu-west-1', 'ap-southeast-1'))
);
-- Indexes for common queries
CREATE INDEX idx_users_auth0_user_id ON users(auth0_user_id);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_primary_region ON users(primary_region);
CREATE INDEX idx_users_subscription_tier ON users(subscription_tier);
CREATE INDEX idx_users_created_at ON users(created_at);
CREATE INDEX idx_users_is_deleted ON users(is_deleted) WHERE is_deleted = TRUE;
CREATE INDEX idx_users_deletion_scheduled ON users(deletion_scheduled_at) WHERE deletion_scheduled_at IS NOT NULL;
-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- Trigger to automatically update updated_at
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- Comments for documentation
COMMENT ON TABLE users IS 'User accounts with GDPR-compliant data residency';
COMMENT ON COLUMN users.auth0_user_id IS 'Auth0 user identifier from JWT sub claim';
COMMENT ON COLUMN users.primary_region IS 'GDPR data residency region (us-east-1 for MVP)';
COMMENT ON COLUMN users.deletion_requested_at IS 'When user requested account deletion (GDPR right to erasure)';
COMMENT ON COLUMN users.deletion_scheduled_at IS 'When account will be permanently deleted (30-day grace period)';
