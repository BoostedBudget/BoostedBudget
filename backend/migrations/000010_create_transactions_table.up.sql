-- Create transactions table for storing financial transactions from all providers
-- Phase 2.3: Transaction Sync Worker
-- Issue #22: https://github.com/BoostedBudget/BoostedBudget/issues/22
--
-- Design: Provider-agnostic schema supporting Plaid (current), manual transactions (future),
-- and other providers like Teller, Finicity, Yodlee (future)

CREATE TABLE IF NOT EXISTS transactions (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id UUID NOT NULL,
    account_id UUID NOT NULL,
    linked_item_id UUID NOT NULL,

    -- Provider abstraction (generic for all sources)
    source VARCHAR(50) NOT NULL, -- 'plaid', 'manual', 'teller', 'finicity', 'yodlee'
    external_transaction_id VARCHAR(255), -- Provider's unique ID (NULL for manual transactions)
    external_metadata JSONB, -- Provider-specific data (e.g., Plaid's logo_url, website, etc.)

    -- Core transaction data (standardized across all providers)
    amount DECIMAL(15,2) NOT NULL,
    date DATE NOT NULL,
    authorized_date DATE,
    name VARCHAR(500) NOT NULL, -- Transaction description
    merchant_name VARCHAR(255),

    -- Categorization (standardized format)
    category JSONB, -- Array of category strings
    personal_finance_category JSONB, -- Detailed categorization with confidence scores

    -- Payment details
    payment_channel VARCHAR(50), -- online, in store, other
    pending BOOLEAN DEFAULT false,

    -- User customization (for manual transactions or edits)
    user_notes TEXT,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    -- Unique constraint: source + external_transaction_id must be unique per user
    -- For manual transactions, external_transaction_id is NULL, so UUID ensures uniqueness
    CONSTRAINT unique_source_transaction UNIQUE (source, external_transaction_id, user_id),
    CONSTRAINT valid_source CHECK (source IN ('plaid', 'teller', 'finicity', 'yodlee', 'manual'))
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE transactions VALIDATE CONSTRAINT fk_transactions_user_id;
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE transactions VALIDATE CONSTRAINT fk_transactions_account_id;
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_linked_item_id FOREIGN KEY (linked_item_id) REFERENCES linked_items(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE transactions VALIDATE CONSTRAINT fk_transactions_linked_item_id;
-- Indexes for performance
CREATE INDEX idx_transactions_user_id ON transactions(user_id);
CREATE INDEX idx_transactions_account_id ON transactions(account_id);
CREATE INDEX idx_transactions_linked_item_id ON transactions(linked_item_id);
CREATE INDEX idx_transactions_date ON transactions(date DESC); -- Most common query: recent transactions
CREATE INDEX idx_transactions_source ON transactions(source); -- Filter by provider
CREATE INDEX idx_transactions_external_id ON transactions(external_transaction_id); -- Lookup by provider ID
CREATE INDEX idx_transactions_pending ON transactions(pending) WHERE pending = true; -- Filter pending transactions

-- Trigger to automatically update updated_at timestamp
CREATE TRIGGER update_transactions_updated_at
    BEFORE UPDATE ON transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE transactions IS 'Financial transactions from all sources: Plaid (API), manual user input, and future providers (Teller, Finicity, Yodlee)';
COMMENT ON COLUMN transactions.source IS 'Data source: plaid (current), manual (future), teller/finicity/yodlee (future providers)';
COMMENT ON COLUMN transactions.external_transaction_id IS 'Provider unique transaction ID. NULL for manual transactions. Used for deduplication and updates.';
COMMENT ON COLUMN transactions.external_metadata IS 'Provider-specific data stored as JSON (e.g., Plaid logo_url, website, location data)';
COMMENT ON COLUMN transactions.amount IS 'Transaction amount. Positive for credits, negative for debits (per Plaid convention)';
COMMENT ON COLUMN transactions.user_notes IS 'User-added notes or customizations (for manual transactions or annotations)';
