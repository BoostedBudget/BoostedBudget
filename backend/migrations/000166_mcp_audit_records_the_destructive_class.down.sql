-- Reverses 000166: narrow mcp_audit_log.operation_class back to ('read','write').
--
-- ⚠ LOSSY BY NECESSITY. Any row recorded while the wider constraint was in force may hold
-- 'destructive', which the narrow constraint rejects — so re-adding it would fail on real data.
-- Those rows are folded to 'write', their nearest truthful value under the old vocabulary
-- (a destructive operation is a mutating one), rather than deleted: an audit row must not
-- vanish on a schema rollback. The distinction between write and destructive is lost, which is
-- precisely what 000166 exists to preserve; roll back only if you mean that.

UPDATE mcp_audit_log SET operation_class = 'write' WHERE operation_class = 'destructive';

ALTER TABLE mcp_audit_log
    DROP CONSTRAINT IF EXISTS mcp_audit_log_operation_class_check;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'mcp_audit_log'::regclass
          AND conname = 'mcp_audit_log_operation_class_check'
    ) THEN
        -- squawk-ignore constraint-missing-not-valid
        ALTER TABLE mcp_audit_log
            ADD CONSTRAINT mcp_audit_log_operation_class_check
            CHECK (operation_class IN ('read', 'write'));
    END IF;
END $$;

-- ⚠ NOTHING AUTOMATED EXERCISES THIS FILE'S DATA PATH. scripts/test-migrations.sh compares
-- SCHEMA hashes across an up->down->up cycle, which this survives, but it never populates
-- mcp_audit_log — so the UPDATE above is covered only by manual verification (round 2 of #1569
-- Task 16: one 'destructive' row folded to 'write', constraint restored, re-up clean).
