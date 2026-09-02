-- Issue #1569, Task 16 (F3): mcp_audit_log.operation_class must be able to record the
-- destructive capability tier.
--
-- The MCP capability manifest gained a third class above `write` for operations that
-- permanently destroy stored rows (purgeSavingsGoal, purgeAccount, deleteTag, ...). Migration
-- 000084 constrained operation_class to ('read','write'), so an agent's deleteTag was logged
-- indistinguishably from its createTag. The audit log is exactly where an operator goes to ask
-- "did an agent destroy anything?", so a tier the log cannot express is a tier that cannot be
-- audited.
--
-- Idempotent: the constraint is dropped by name and re-added, both guarded. 000084 created it
-- inline and unnamed, so PostgreSQL named it mcp_audit_log_operation_class_check.

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
        -- A NOT VALID + VALIDATE split is the safe pattern for a large table, but mcp_audit_log
        -- is small and, more to the point, this constraint is strictly WIDER than the one just
        -- dropped: every existing row already satisfies it, so the validation scan finds nothing
        -- to reject. Splitting it would add a second statement for no reduction in lock time.
        ALTER TABLE mcp_audit_log
            ADD CONSTRAINT mcp_audit_log_operation_class_check
            CHECK (operation_class IN ('read', 'write', 'destructive'));
    END IF;
END $$;
