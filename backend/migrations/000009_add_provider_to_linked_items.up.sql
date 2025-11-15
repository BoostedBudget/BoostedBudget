-- Add provider column to linked_items for multi-provider support
-- Phase 2.3: Transaction Sync Worker - Provider abstraction
-- Issue #22: https://github.com/BoostedBudget/BoostedBudget/issues/22

-- Add provider column with default 'plaid' for existing records
ALTER TABLE linked_items
ADD COLUMN provider VARCHAR(50) NOT NULL DEFAULT 'plaid';

-- Add constraint for valid providers
-- Supports: plaid (current), teller, finicity, yodlee (future), manual (future)
ALTER TABLE linked_items
ADD CONSTRAINT valid_provider CHECK (provider IN ('plaid', 'teller', 'finicity', 'yodlee', 'manual'));

-- Create index for provider-based queries (filtering items by provider type)
CREATE INDEX idx_linked_items_provider ON linked_items(provider);

-- Documentation
COMMENT ON COLUMN linked_items.provider IS 'Transaction data provider: plaid (current), teller/finicity/yodlee (future providers), or manual (user-created accounts)';
