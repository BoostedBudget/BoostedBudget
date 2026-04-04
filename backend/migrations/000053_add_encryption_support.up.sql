-- #964: Add encryption support
-- Widen VARCHAR columns to TEXT for all fields that will be encrypted
-- (encrypted base64 values are larger than the original plaintext)
-- Add blind index columns for equality-lookup fields
-- Convert opaque JSONB blob columns to TEXT for encrypted blob storage

-- ============================================================
-- Widen VARCHAR columns → TEXT for encrypted fields
-- ============================================================

-- mortgage_liabilities
ALTER TABLE mortgage_liabilities ALTER COLUMN account_number TYPE TEXT;
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_street TYPE TEXT;
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_city TYPE TEXT;
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_region TYPE TEXT;
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_postal_code TYPE TEXT;
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_country TYPE TEXT;

-- student_loan_liabilities
ALTER TABLE student_loan_liabilities ALTER COLUMN account_number TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_street TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_city TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_region TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_postal_code TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_country TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN payment_reference_number TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN guarantor TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN loan_name TYPE TEXT;
ALTER TABLE student_loan_liabilities ALTER COLUMN sequence_number TYPE TEXT;

-- accounts
ALTER TABLE accounts ALTER COLUMN plaid_account_id TYPE TEXT;
ALTER TABLE accounts ALTER COLUMN name TYPE TEXT;
ALTER TABLE accounts ALTER COLUMN official_name TYPE TEXT;
ALTER TABLE accounts ALTER COLUMN mask TYPE TEXT;

-- user_accounts
ALTER TABLE user_accounts ALTER COLUMN institution_id TYPE TEXT;
ALTER TABLE user_accounts ALTER COLUMN mask TYPE TEXT;
ALTER TABLE user_accounts ALTER COLUMN account_name_hint TYPE TEXT;

-- users
ALTER TABLE users ALTER COLUMN email TYPE TEXT;
ALTER TABLE users ALTER COLUMN clerk_user_id TYPE TEXT;
ALTER TABLE users ALTER COLUMN paddle_customer_id TYPE TEXT;
ALTER TABLE users ALTER COLUMN paddle_subscription_id TYPE TEXT;

-- user_identities
ALTER TABLE user_identities ALTER COLUMN email TYPE TEXT;
ALTER TABLE user_identities ALTER COLUMN clerk_user_id TYPE TEXT;
ALTER TABLE user_identities ALTER COLUMN provider_user_id TYPE TEXT;

-- linked_items
ALTER TABLE linked_items ALTER COLUMN item_id TYPE TEXT;
ALTER TABLE linked_items ALTER COLUMN institution_id TYPE TEXT;
ALTER TABLE linked_items ALTER COLUMN institution_name TYPE TEXT;

-- webhook_events
ALTER TABLE webhook_events ALTER COLUMN item_id TYPE TEXT;

-- paddle_transactions
ALTER TABLE paddle_transactions ALTER COLUMN transaction_id TYPE TEXT;
ALTER TABLE paddle_transactions ALTER COLUMN subscription_id TYPE TEXT;
ALTER TABLE paddle_transactions ALTER COLUMN customer_id TYPE TEXT;

-- notifications
ALTER TABLE notifications ALTER COLUMN action_url TYPE TEXT;

-- ============================================================
-- Add blind index columns (for equality lookups on encrypted fields)
-- ============================================================

ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS item_id_blind_idx TEXT;
ALTER TABLE linked_items ADD COLUMN IF NOT EXISTS institution_id_blind_idx TEXT;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS external_transaction_id_blind_idx TEXT;
ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS item_id_blind_idx TEXT;
ALTER TABLE paddle_transactions ADD COLUMN IF NOT EXISTS transaction_id_blind_idx TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_blind_idx TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS clerk_user_id_blind_idx TEXT;
ALTER TABLE user_accounts ADD COLUMN IF NOT EXISTS institution_id_blind_idx TEXT;
ALTER TABLE user_accounts ADD COLUMN IF NOT EXISTS mask_blind_idx TEXT;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS plaid_account_id_blind_idx TEXT;

-- ============================================================
-- Create indexes on blind index columns
-- ============================================================

CREATE INDEX IF NOT EXISTS idx_linked_items_item_id_blind ON linked_items (item_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_linked_items_institution_id_blind ON linked_items (institution_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_transactions_ext_id_blind ON transactions (external_transaction_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_webhook_events_item_id_blind ON webhook_events (item_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_paddle_transactions_txn_id_blind ON paddle_transactions (transaction_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_users_email_blind ON users (email_blind_idx);
CREATE INDEX IF NOT EXISTS idx_users_clerk_id_blind ON users (clerk_user_id_blind_idx);
CREATE INDEX IF NOT EXISTS idx_user_accounts_inst_mask_blind ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx);
CREATE INDEX IF NOT EXISTS idx_accounts_plaid_id_blind ON accounts (plaid_account_id_blind_idx);

-- ============================================================
-- Convert JSONB blob columns to TEXT for encrypted blob storage
-- These columns are opaque blobs (no SQL predicates against them)
-- ============================================================

ALTER TABLE transactions ALTER COLUMN external_metadata TYPE TEXT USING external_metadata::TEXT;
ALTER TABLE webhook_events ALTER COLUMN payload TYPE TEXT USING payload::TEXT;
ALTER TABLE paddle_webhook_events ALTER COLUMN payload TYPE TEXT USING payload::TEXT;
ALTER TABLE paddle_transactions ALTER COLUMN payload TYPE TEXT USING payload::TEXT;
ALTER TABLE audit_logs ALTER COLUMN changes TYPE TEXT USING changes::TEXT;

-- audit_logs: ip_address must be TEXT to store encrypted ciphertext (INET cannot hold base64)
ALTER TABLE audit_logs ALTER COLUMN ip_address TYPE TEXT USING ip_address::TEXT;

-- user_identities: add blind index for clerk_user_id lookup
ALTER TABLE user_identities ADD COLUMN IF NOT EXISTS clerk_user_id_blind_idx TEXT;
CREATE INDEX IF NOT EXISTS idx_user_identities_clerk_id_blind ON user_identities (clerk_user_id_blind_idx);

-- users: add blind index for paddle_subscription_id lookup
ALTER TABLE users ADD COLUMN IF NOT EXISTS paddle_subscription_id_blind_idx TEXT;
CREATE INDEX IF NOT EXISTS idx_users_paddle_subscription_id_blind ON users (paddle_subscription_id_blind_idx);
