-- Rollback for 000165: restore migration 000110's original comments verbatim.
-- Comments only — there is nothing structural to undo.

COMMENT ON TABLE household_account_shares IS
    'Grant table for household account visibility. One active row per '
    '(household_id, user_account_id). Keyed to user_accounts (survives Plaid relinks). '
    'owner_user_id is intentionally omitted — derived from user_accounts.user_id at query time. '
    'Part of issue #1365 — Multi-Member Household Accounts Phase 1.';

COMMENT ON COLUMN household_account_shares.visibility IS
    'shared: included in the household aggregate. '
    'hidden: member chose to exclude this account from the shared view.';

COMMENT ON COLUMN household_account_shares.shared_at IS
    'Timestamp when this grant was created (NOT NULL). '
    'revoked_at (when set) must be >= shared_at.';

COMMENT ON COLUMN household_account_shares.revoked_at IS NULL;
