-- Drop accounts table and all associated objects
DROP TRIGGER IF EXISTS update_accounts_updated_at ON accounts;
DROP INDEX IF EXISTS idx_accounts_type;
DROP INDEX IF EXISTS idx_accounts_plaid_account_id;
DROP INDEX IF EXISTS idx_accounts_linked_item_id;
DROP INDEX IF EXISTS idx_accounts_user_id;
DROP TABLE IF EXISTS accounts CASCADE;
