-- Create accounts table for storing individual bank accounts from Plaid
-- Phase 2.2: Account Linking Service
CREATE TABLE IF NOT EXISTS accounts (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    linked_item_id UUID NOT NULL REFERENCES linked_items(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Plaid account information
    plaid_account_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,              -- User-friendly name (e.g., "Chase Checking")
    official_name VARCHAR(255),              -- Bank's official name (e.g., "Chase Total Checking®")
    type VARCHAR(50) NOT NULL,               -- depository, credit, loan, investment, other
    subtype VARCHAR(50),                     -- checking, savings, credit card, mortgage, etc.
    mask VARCHAR(10),                        -- Last 4 digits of account number

    -- Balance information
    current_balance DECIMAL(15,2),
    available_balance DECIMAL(15,2),
    currency_code VARCHAR(3) DEFAULT 'USD',  -- ISO 4217 currency code

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_user_account UNIQUE (user_id, plaid_account_id),
    CONSTRAINT valid_account_type CHECK (type IN ('depository', 'credit', 'loan', 'investment', 'other'))
);

-- Indexes for performance
CREATE INDEX idx_accounts_user_id ON accounts(user_id);
CREATE INDEX idx_accounts_linked_item_id ON accounts(linked_item_id);
CREATE INDEX idx_accounts_plaid_account_id ON accounts(plaid_account_id);
CREATE INDEX idx_accounts_type ON accounts(type);

-- Trigger to automatically update updated_at
CREATE TRIGGER update_accounts_updated_at
    BEFORE UPDATE ON accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE accounts IS 'Individual bank accounts linked through Plaid';
COMMENT ON COLUMN accounts.type IS 'Account type: depository, credit, loan, investment, other';
COMMENT ON COLUMN accounts.subtype IS 'Account subtype: checking, savings, credit card, mortgage, etc.';
COMMENT ON COLUMN accounts.mask IS 'Last 4 digits of account number for display';
