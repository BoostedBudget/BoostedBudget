-- #964: Revert encryption support schema changes
-- Note: Data decryption must be run separately before applying this migration.
-- Run: backend/cmd/encrypt-migrate/main.go --decrypt before migrating down.

-- ============================================================
-- Revert JSONB blob columns from TEXT back to JSONB
-- (only safe after running the decrypt tool)
-- ============================================================

-- Revert user_identities clerk_user_id blind index
DROP INDEX IF EXISTS idx_user_identities_clerk_id_blind;
ALTER TABLE user_identities DROP COLUMN IF EXISTS clerk_user_id_blind_idx;

-- Revert users paddle_subscription_id blind index
DROP INDEX IF EXISTS idx_users_paddle_subscription_id_blind;
ALTER TABLE users DROP COLUMN IF EXISTS paddle_subscription_id_blind_idx;

-- Revert audit_logs ip_address from TEXT back to INET
ALTER TABLE audit_logs ALTER COLUMN ip_address TYPE INET USING ip_address::INET;

ALTER TABLE audit_logs ALTER COLUMN changes TYPE JSONB USING changes::JSONB;
ALTER TABLE paddle_transactions ALTER COLUMN payload TYPE JSONB USING payload::JSONB;
ALTER TABLE paddle_webhook_events ALTER COLUMN payload TYPE JSONB USING payload::JSONB;
ALTER TABLE webhook_events ALTER COLUMN payload TYPE JSONB USING payload::JSONB;
ALTER TABLE transactions ALTER COLUMN external_metadata TYPE JSONB USING external_metadata::JSONB;

-- ============================================================
-- Drop blind index columns and indexes
-- ============================================================

DROP INDEX IF EXISTS idx_accounts_plaid_id_blind;
DROP INDEX IF EXISTS idx_user_accounts_inst_mask_blind;
DROP INDEX IF EXISTS idx_users_clerk_id_blind;
DROP INDEX IF EXISTS idx_users_email_blind;
DROP INDEX IF EXISTS idx_paddle_transactions_txn_id_blind;
DROP INDEX IF EXISTS idx_webhook_events_item_id_blind;
DROP INDEX IF EXISTS idx_transactions_ext_id_blind;
DROP INDEX IF EXISTS idx_linked_items_institution_id_blind;
DROP INDEX IF EXISTS idx_linked_items_item_id_blind;

ALTER TABLE accounts DROP COLUMN IF EXISTS plaid_account_id_blind_idx;
ALTER TABLE user_accounts DROP COLUMN IF EXISTS mask_blind_idx;
ALTER TABLE user_accounts DROP COLUMN IF EXISTS institution_id_blind_idx;
ALTER TABLE users DROP COLUMN IF EXISTS clerk_user_id_blind_idx;
ALTER TABLE users DROP COLUMN IF EXISTS email_blind_idx;
ALTER TABLE paddle_transactions DROP COLUMN IF EXISTS transaction_id_blind_idx;
ALTER TABLE webhook_events DROP COLUMN IF EXISTS item_id_blind_idx;
ALTER TABLE transactions DROP COLUMN IF EXISTS external_transaction_id_blind_idx;
ALTER TABLE linked_items DROP COLUMN IF EXISTS institution_id_blind_idx;
ALTER TABLE linked_items DROP COLUMN IF EXISTS item_id_blind_idx;

-- ============================================================
-- Revert TEXT columns back to VARCHAR
-- (values must already be decrypted before running this)
-- ============================================================

ALTER TABLE notifications ALTER COLUMN action_url TYPE VARCHAR(2048);

ALTER TABLE paddle_transactions ALTER COLUMN customer_id TYPE VARCHAR(255);
ALTER TABLE paddle_transactions ALTER COLUMN subscription_id TYPE VARCHAR(255);
ALTER TABLE paddle_transactions ALTER COLUMN transaction_id TYPE VARCHAR(255);

ALTER TABLE webhook_events ALTER COLUMN item_id TYPE VARCHAR(255);

ALTER TABLE linked_items ALTER COLUMN institution_name TYPE VARCHAR(255);
ALTER TABLE linked_items ALTER COLUMN institution_id TYPE VARCHAR(255);
ALTER TABLE linked_items ALTER COLUMN item_id TYPE VARCHAR(255);

ALTER TABLE user_identities ALTER COLUMN provider_user_id TYPE VARCHAR(255);
ALTER TABLE user_identities ALTER COLUMN clerk_user_id TYPE VARCHAR(255);
ALTER TABLE user_identities ALTER COLUMN email TYPE VARCHAR(255);

ALTER TABLE users ALTER COLUMN paddle_subscription_id TYPE VARCHAR(255);
ALTER TABLE users ALTER COLUMN paddle_customer_id TYPE VARCHAR(255);
ALTER TABLE users ALTER COLUMN clerk_user_id TYPE VARCHAR(255);
ALTER TABLE users ALTER COLUMN email TYPE VARCHAR(255);

ALTER TABLE user_accounts ALTER COLUMN account_name_hint TYPE VARCHAR(255);
ALTER TABLE user_accounts ALTER COLUMN mask TYPE VARCHAR(10);
ALTER TABLE user_accounts ALTER COLUMN institution_id TYPE VARCHAR(255);

ALTER TABLE accounts ALTER COLUMN mask TYPE VARCHAR(10);
ALTER TABLE accounts ALTER COLUMN official_name TYPE VARCHAR(255);
ALTER TABLE accounts ALTER COLUMN name TYPE VARCHAR(255);
ALTER TABLE accounts ALTER COLUMN plaid_account_id TYPE VARCHAR(255);

ALTER TABLE student_loan_liabilities ALTER COLUMN sequence_number TYPE VARCHAR(100);
ALTER TABLE student_loan_liabilities ALTER COLUMN loan_name TYPE VARCHAR(255);
ALTER TABLE student_loan_liabilities ALTER COLUMN guarantor TYPE VARCHAR(255);
ALTER TABLE student_loan_liabilities ALTER COLUMN payment_reference_number TYPE VARCHAR(255);
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_country TYPE VARCHAR(10);
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_postal_code TYPE VARCHAR(20);
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_region TYPE VARCHAR(100);
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_city TYPE VARCHAR(255);
ALTER TABLE student_loan_liabilities ALTER COLUMN servicer_address_street TYPE VARCHAR(500);
ALTER TABLE student_loan_liabilities ALTER COLUMN account_number TYPE VARCHAR(100);

ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_country TYPE VARCHAR(10);
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_postal_code TYPE VARCHAR(20);
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_region TYPE VARCHAR(100);
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_city TYPE VARCHAR(255);
ALTER TABLE mortgage_liabilities ALTER COLUMN property_address_street TYPE VARCHAR(500);
ALTER TABLE mortgage_liabilities ALTER COLUMN account_number TYPE VARCHAR(100);
