-- Issue #1497 item 2, follow-up: reverse 000139, restoring 000137's weaker constraints exactly.
--
-- WHAT THIS GIVES BACK. After this rollback the database once again accepts an attestation whose
-- attester is '' or '   ', and a note that is '' or '   ' beside an attestation. Nothing is lost — no
-- column is dropped and no row is rewritten — but the "an attestation is never anonymous" guarantee
-- reverts to being a property of the application code rather than of the schema. That is the state
-- 000137 shipped in and the state 000139 exists to end, so roll this back only alongside code that
-- still refuses those values itself (plaidOrphanedItemRepository.RecordOperatorResolution and
-- AdminHandler.RecordPlaidOrphanedItemOperatorResolution both do, and neither is being rolled back
-- by this file).
--
-- NO DATA CAN BE MADE ILLEGAL BY THIS. The restored constraint is strictly weaker than the one it
-- replaces, so every row that satisfies 000139 satisfies 000137's version; the ADD CONSTRAINT here
-- cannot fail on existing data. That is the opposite of the usual rollback risk and is worth stating,
-- because it is why this rollback is safe to run at any time.
--
-- Idempotent: DROP CONSTRAINT IF EXISTS throughout, and the restored constraint is dropped
-- immediately before it is added, so a partially-applied 000139 rolls back cleanly either way.

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_operator_note_is_not_blank;

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_operator_attestation_is_attributed;

ALTER TABLE plaid_orphaned_items
    ADD CONSTRAINT plaid_orphaned_items_operator_attestation_is_attributed
    CHECK ((operator_resolved_at IS NULL) = (operator_resolved_by IS NULL));
