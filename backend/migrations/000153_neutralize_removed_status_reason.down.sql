-- Rollback for 000153: restore 000152's function body verbatim, including its
-- 'user_disconnect' label for active -> removed.
--
-- ⚠ This rollback re-introduces a false claim for any row written after 000153 by the
-- provider-initiated path (transactionSyncService.reconcileRemovedAccounts): those transitions
-- will once again be stamped 'user_disconnect' going forward. Rows already written as 'removed'
-- are left alone -- a rollback cannot recover a distinction it is undoing, and inventing a cause
-- for them would be the exact defect 000153 removed. audit_logs remains the authoritative record
-- of WHY in either direction (account.removed vs account.removed_by_provider).
--
-- Idempotent: CREATE OR REPLACE FUNCTION, triggers untouched.

CREATE OR REPLACE FUNCTION record_account_status_change() RETURNS TRIGGER AS $$
DECLARE
    ts TIMESTAMPTZ;
    derived_reason TEXT;
BEGIN
    ts := clock_timestamp();

    IF TG_OP = 'INSERT' THEN
        derived_reason := 'created';
    ELSIF OLD.status = 'active' AND NEW.status = 'replaced' THEN
        derived_reason := 'relink';
    ELSIF OLD.status = 'active' AND NEW.status = 'expired' THEN
        derived_reason := 'fingerprint_expiry';
    ELSIF OLD.status = 'active' AND NEW.status = 'removed' THEN
        derived_reason := 'user_disconnect';
    ELSE
        derived_reason := 'status_change:' || OLD.status || '_to_' || NEW.status;
    END IF;

    UPDATE account_status_history
        SET valid_to = ts
        WHERE account_id = NEW.id AND valid_to IS NULL;

    INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, reason, provenance)
        VALUES (NEW.id, NEW.user_id, NEW.user_account_id, NEW.status, ts, derived_reason, 'recorded');

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
