-- Rollback for 000189_add_repair_nonces_and_failure_assert_tracking (#1609 P4-T2, #1613 P4-T3).
--
-- Every object this drops was created by the up migration and holds no financial history:
--
--   * linked_item_repair_nonces rows are short-lived single-use authorization artifacts for one
--     repair attempt (see the up migration's table comment). Dropping the table loses in-flight
--     repair nonces only — a user whose nonce vanishes re-opens /accounts/reconnect and mints a
--     new one, which is the same recovery path an expired nonce already has.
--   * linked_items.last_failure_asserted_at is derived ordering metadata; the failure itself lives
--     in status/error_code/error_message/last_error_at, none of which this migration touched.
--   * investments_skipped_at_link and total_value_investment_only were never written by anything
--     (created UNWIRED for P4-T3), so dropping them can lose nothing at all.
--
-- ⚠ Rolling this back re-opens both defects it closed: repair reverts to accepting a bare
-- {item_id} from any authenticated owner (#1620), and a delayed clear can again erase a
-- same-status re-asserted failure (#1623). The Go code must be rolled back with it.
--
-- Idempotent: IF EXISTS everywhere, so a re-run over an already-rolled-back schema is a no-op.

DROP TRIGGER IF EXISTS update_linked_item_repair_nonces_updated_at ON linked_item_repair_nonces;
DROP INDEX IF EXISTS idx_linked_item_repair_nonces_expires_at;
DROP INDEX IF EXISTS idx_linked_item_repair_nonces_linked_item_id;
DROP INDEX IF EXISTS uq_linked_item_repair_nonces_token_hash;
DROP TABLE IF EXISTS linked_item_repair_nonces CASCADE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'portfolio_snapshots' AND column_name = 'total_value_investment_only'
    ) THEN
        ALTER TABLE portfolio_snapshots DROP COLUMN total_value_investment_only;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'investments_skipped_at_link'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN investments_skipped_at_link;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linked_items' AND column_name = 'last_failure_asserted_at'
    ) THEN
        ALTER TABLE linked_items DROP COLUMN last_failure_asserted_at;
    END IF;
END $$;
