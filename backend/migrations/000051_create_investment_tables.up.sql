-- Create investment tables for Plaid Investments product
-- Issue #659: Plaid Investments: Holdings, Securities, Investment Transactions
--
-- Tables created in dependency order:
--   1. securities (shared reference data, not user-scoped)
--   2. holdings (user positions in securities)
--   3. investment_transactions (trades, dividends, fees)
-- Plus: add investments_last_synced_date to linked_items for incremental sync tracking

-- ============================================================
-- 1. securities
-- ============================================================
CREATE TABLE IF NOT EXISTS securities (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Plaid dedup key (shared across all users)
    plaid_security_id VARCHAR(255) NOT NULL,

    -- Instrument metadata
    ticker_symbol VARCHAR(50),
    name VARCHAR(500) NOT NULL,
    type VARCHAR(100),                              -- stock, etf, mutual fund, derivative, etc.
    isin VARCHAR(20),
    cusip VARCHAR(20),
    is_cash_equivalent BOOLEAN DEFAULT FALSE,

    -- Institutional pricing (overnight updates, not real-time)
    institution_price DECIMAL(18, 8),
    institution_price_as_of TIMESTAMP WITH TIME ZONE,
    close_price DECIMAL(18, 8),
    close_price_as_of DATE,

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_plaid_security_id UNIQUE (plaid_security_id)
);

-- Indexes for securities
CREATE INDEX IF NOT EXISTS idx_securities_plaid_security_id ON securities(plaid_security_id);
CREATE INDEX IF NOT EXISTS idx_securities_ticker_symbol ON securities(ticker_symbol);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_securities_updated_at'
    ) THEN
        CREATE TRIGGER update_securities_updated_at
            BEFORE UPDATE ON securities
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE securities IS 'Financial securities metadata from Plaid. Shared across all users, not user-scoped.';
COMMENT ON COLUMN securities.plaid_security_id IS 'Plaid-assigned security ID (globally unique per instrument)';
COMMENT ON COLUMN securities.institution_price IS 'Institutional price (overnight update, not real-time market data)';

-- ============================================================
-- 2. holdings
-- ============================================================
CREATE TABLE IF NOT EXISTS holdings (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id UUID NOT NULL,
    account_id UUID NOT NULL,
    security_id UUID NOT NULL,

    -- Position data
    quantity DECIMAL(18, 8) NOT NULL,
    cost_basis DECIMAL(18, 2),
    institution_value DECIMAL(18, 2),
    institution_price DECIMAL(18, 8),

    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- A user can only hold each security once per account
    CONSTRAINT unique_account_security UNIQUE (account_id, security_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
-- Wrapped in DO blocks for idempotency (re-running after partial failure won't error)
DO $$ BEGIN
    ALTER TABLE holdings ADD CONSTRAINT fk_holdings_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holdings VALIDATE CONSTRAINT fk_holdings_user_id;
DO $$ BEGIN
    ALTER TABLE holdings ADD CONSTRAINT fk_holdings_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holdings VALIDATE CONSTRAINT fk_holdings_account_id;
DO $$ BEGIN
    ALTER TABLE holdings ADD CONSTRAINT fk_holdings_security_id FOREIGN KEY (security_id) REFERENCES securities(id) ON DELETE RESTRICT NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE holdings VALIDATE CONSTRAINT fk_holdings_security_id;

-- Indexes for holdings
CREATE INDEX IF NOT EXISTS idx_holdings_user_id ON holdings(user_id);
CREATE INDEX IF NOT EXISTS idx_holdings_account_id ON holdings(account_id);
CREATE INDEX IF NOT EXISTS idx_holdings_security_id ON holdings(security_id);

-- Trigger to auto-update updated_at
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_holdings_updated_at'
    ) THEN
        CREATE TRIGGER update_holdings_updated_at
            BEFORE UPDATE ON holdings
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Comments
COMMENT ON TABLE holdings IS 'User investment positions. Replaced in full on each sync (no delta).';
COMMENT ON COLUMN holdings.quantity IS 'Number of shares/units held';
COMMENT ON COLUMN holdings.cost_basis IS 'Total cost basis in account currency';
COMMENT ON COLUMN holdings.institution_value IS 'Current market value per institution pricing';

-- ============================================================
-- 3. investment_transactions
-- ============================================================
CREATE TABLE IF NOT EXISTS investment_transactions (
    -- Primary key
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Foreign keys
    user_id UUID NOT NULL,
    account_id UUID NOT NULL,
    security_id UUID,                              -- NULL for cash transactions

    -- Plaid dedup key
    plaid_investment_transaction_id VARCHAR(255) NOT NULL,

    -- Transaction data
    type VARCHAR(100) NOT NULL,                    -- buy, sell, dividend, cash, fee, transfer, etc.
    subtype VARCHAR(100),
    name VARCHAR(500) NOT NULL,
    quantity DECIMAL(18, 8),                       -- NULL for cash-only transactions
    amount DECIMAL(18, 2) NOT NULL,
    price DECIMAL(18, 8),                          -- Per-unit price at time of transaction
    fees DECIMAL(18, 2),
    date DATE NOT NULL,

    -- Timestamps (investment transactions are immutable, no updated_at)
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT unique_plaid_investment_transaction_id UNIQUE (plaid_investment_transaction_id)
);

-- Foreign key constraints (NOT VALID for zero-downtime, then VALIDATE)
-- Wrapped in DO blocks for idempotency (re-running after partial failure won't error)
DO $$ BEGIN
    ALTER TABLE investment_transactions ADD CONSTRAINT fk_inv_txn_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE investment_transactions VALIDATE CONSTRAINT fk_inv_txn_user_id;
DO $$ BEGIN
    ALTER TABLE investment_transactions ADD CONSTRAINT fk_inv_txn_account_id FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE investment_transactions VALIDATE CONSTRAINT fk_inv_txn_account_id;
DO $$ BEGIN
    ALTER TABLE investment_transactions ADD CONSTRAINT fk_inv_txn_security_id FOREIGN KEY (security_id) REFERENCES securities(id) ON DELETE RESTRICT NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
ALTER TABLE investment_transactions VALIDATE CONSTRAINT fk_inv_txn_security_id;

-- Indexes for investment_transactions
CREATE INDEX IF NOT EXISTS idx_inv_txn_user_id ON investment_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_inv_txn_account_id ON investment_transactions(account_id);
CREATE INDEX IF NOT EXISTS idx_inv_txn_security_id ON investment_transactions(security_id);
CREATE INDEX IF NOT EXISTS idx_inv_txn_date ON investment_transactions(date);
CREATE INDEX IF NOT EXISTS idx_inv_txn_user_date ON investment_transactions(user_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_inv_txn_account_date ON investment_transactions(account_id, date DESC);

-- Comments
COMMENT ON TABLE investment_transactions IS 'Investment account transactions (buys, sells, dividends, fees) from Plaid.';
COMMENT ON COLUMN investment_transactions.plaid_investment_transaction_id IS 'Plaid-assigned unique ID for idempotent upserts';
COMMENT ON COLUMN investment_transactions.type IS 'Transaction type: buy, sell, dividend, cash, fee, transfer, etc.';
COMMENT ON COLUMN investment_transactions.security_id IS 'NULL for cash-only transactions (deposits, withdrawals, fees without a security)';

-- ============================================================
-- 4. Add investments_last_synced_date to linked_items
-- ============================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'investments_last_synced_date'
    ) THEN
        ALTER TABLE linked_items ADD COLUMN investments_last_synced_date DATE;
        COMMENT ON COLUMN linked_items.investments_last_synced_date IS 'Date through which investment transactions have been synced. NULL = never synced (use 2-year lookback).';
    END IF;
END $$;
