-- #1569 round 11 task 4: rollback.
--
-- HONESTY NOTE. This down migration CANNOT reverse the split performed by the up migration.
-- Merging user_accounts rows back together is lossy in general (two rows may have accrued
-- independent customizations, independent household_account_shares grants, and independent
-- downstream references since the split), and re-deriving "which rows used to be one chain" is
-- not possible once new rows have been minted with fresh IDs. This down migration therefore only
-- reverses the SCHEMA change (the unique index swap) — it does not attempt to merge split rows
-- back together, delete the rows this migration minted, or undo the household_account_shares
-- fan-out. Running this down migration leaves the split data in place under the OLD, wider match
-- key; if a subsequent Plaid re-link then hits the restored non-narrowed index, it can re-collide
-- two now-separate chains again exactly as before 000100 was narrowed — that is the pre-existing
-- (pre-#1569-task-4) behavior this rollback restores, not a new defect.
-- ⚠ ROUND 2 FIX: unique_user_account_identity (dropped by the up migration, Step 1) is
-- DELIBERATELY NOT RESTORED here. It is superseded by the blind-index constraint below and by
-- migration 000100's own documented finding that it is practically inert for real (independently
-- encrypted) inserts; restoring it could also fail outright if any split-minted row from the up
-- migration still shares byte-identical ciphertext with its origin row (the masked-collision
-- case), which this rollback has no way to undo. Consistent with the HONESTY NOTE above: this
-- down migration reverses schema, not the data changes the up migration made.
DROP INDEX IF EXISTS unique_user_accounts_inst_mask_identity_blind;

CREATE UNIQUE INDEX IF NOT EXISTS unique_user_accounts_inst_mask_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx)
    NULLS NOT DISTINCT
    WHERE institution_id_blind_idx IS NOT NULL;
