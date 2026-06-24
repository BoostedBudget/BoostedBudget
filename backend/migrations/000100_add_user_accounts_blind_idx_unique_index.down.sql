-- #1334: Rollback — restore non-unique blind-index index, drop unique index.
-- NOTE: Deleted duplicate user_account rows are NOT restored (data loss is intentional;
-- they were logical duplicates caused by the encryption nonce change).

DROP INDEX IF EXISTS unique_user_accounts_inst_mask_blind;

CREATE INDEX IF NOT EXISTS idx_user_accounts_inst_mask_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx);
