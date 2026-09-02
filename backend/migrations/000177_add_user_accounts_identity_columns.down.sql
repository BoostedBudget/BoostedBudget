-- #1569 round 11 task 3: revert the frozen identity columns on user_accounts.

DROP TRIGGER IF EXISTS trg_user_accounts_identity_immutable ON user_accounts;
DROP FUNCTION IF EXISTS enforce_user_accounts_identity_immutable();

ALTER TABLE user_accounts DROP CONSTRAINT IF EXISTS user_accounts_valid_identity_type;

ALTER TABLE user_accounts DROP COLUMN IF EXISTS identity_type;
ALTER TABLE user_accounts DROP COLUMN IF EXISTS identity_subtype;
