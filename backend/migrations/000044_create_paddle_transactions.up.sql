-- Create paddle_transactions table for payment audit trail
-- Records completed transactions and payment failures for auditing and reconciliation

CREATE TABLE IF NOT EXISTS paddle_transactions (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- User association
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Paddle identifiers
    transaction_id VARCHAR(255) NOT NULL UNIQUE, -- Paddle transaction ID (e.g., txn_01h...)
    subscription_id VARCHAR(255), -- Paddle subscription ID (e.g., sub_01h...), nullable for one-time payments
    customer_id VARCHAR(255), -- Paddle customer ID (e.g., ctm_01h...)

    -- Transaction details
    amount DECIMAL(10, 2) NOT NULL, -- Transaction amount (in currency units, e.g., 8.99 for $8.99)
    currency VARCHAR(3) NOT NULL DEFAULT 'USD', -- ISO 4217 currency code
    status VARCHAR(50) NOT NULL, -- Transaction status: completed, failed, refunded, etc.

    -- Payment method info (from Paddle payload)
    payment_method_type VARCHAR(50), -- card, paypal, etc.

    -- Billing details (for customer service and reconciliation)
    billing_period_start TIMESTAMP WITH TIME ZONE, -- Start of billing period
    billing_period_end TIMESTAMP WITH TIME ZONE, -- End of billing period

    -- Complete transaction payload (JSONB for flexibility)
    payload JSONB NOT NULL, -- Full transaction data from Paddle webhook

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for efficient querying
CREATE INDEX idx_paddle_transactions_user_id ON paddle_transactions(user_id);
CREATE INDEX idx_paddle_transactions_transaction_id ON paddle_transactions(transaction_id);
CREATE INDEX idx_paddle_transactions_subscription_id ON paddle_transactions(subscription_id);
CREATE INDEX idx_paddle_transactions_status ON paddle_transactions(status);
CREATE INDEX idx_paddle_transactions_created_at ON paddle_transactions(created_at DESC); -- Most recent first

-- Trigger to automatically update updated_at timestamp
CREATE TRIGGER update_paddle_transactions_updated_at
    BEFORE UPDATE ON paddle_transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE paddle_transactions IS 'Audit log of all Paddle payment transactions for financial reconciliation';
COMMENT ON COLUMN paddle_transactions.transaction_id IS 'Unique transaction ID from Paddle (e.g., txn_01h...)';
COMMENT ON COLUMN paddle_transactions.subscription_id IS 'Paddle subscription ID if this is a subscription payment';
COMMENT ON COLUMN paddle_transactions.amount IS 'Transaction amount in currency units (e.g., 8.99 for $8.99 USD)';
COMMENT ON COLUMN paddle_transactions.status IS 'Transaction status: completed, failed, refunded, etc.';
COMMENT ON COLUMN paddle_transactions.payload IS 'Complete transaction data as JSON for reconciliation and auditing';
