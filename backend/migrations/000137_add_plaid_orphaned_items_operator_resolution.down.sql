-- Issue #1497 item 2: reverse 000137.
--
-- WHAT THIS DESTROYS, and unlike 000135's rollback the loss is a pure loss rather than a GDPR
-- improvement: dropping these three columns destroys every operator attestation — who said they had
-- removed a stranded Plaid Item by hand, when, and what they said they did. The Items themselves are
-- unaffected (an attestation never changed anything at Plaid; it only recorded a human's claim), and
-- so is the retained credential, which these columns never touched.
--
-- THE STATE AFTER THIS ROLLBACK is that every attested row reverts to reporting
-- 'manual_intervention_required' — because that is what it genuinely is once the claim is gone, an
-- outstanding Item with no evidence about it either way. So the failure direction is toward MORE work
-- being reported rather than less, which is the correct direction for a compliance work list: an
-- operator may redo something already done, but nothing rots silently behind a green label. Export the
-- three columns first if the attestation history matters — it is the only record of who acted.
--
-- ⚠ ROLL BACK THE CODE TOO, and here is what happens if you do not. plaidOrphanedItemRepository's
-- shared orphanSelectColumns names all three columns, so EVERY read of this table — the admin listing,
-- the retry sweep's page query, the /admin/health state derivation and the Prometheus monitoring pass
-- — fails with "column does not exist". The retry sweep and the monitoring pass would then error out
-- each run (loudly, and River retries them), and the admin surface would 500. The PURGE sweep is the
-- exception and that is by design: PurgeExpiredTokens names no column but the credential and the
-- deadline, so the retention guarantee 000135 rests on keeps working through this rollback, as it is
-- meant to keep working through everything. Same shape of warning as 000134's and 000135's, and for
-- the same reason: this table's readers share one column list.
--
-- ban-drop-column is an ENABLED Squawk rule; .down.sql files are excluded from linting via
-- excluded_paths in .squawk.toml precisely so a rollback can contain the DROP that reverses its
-- .up.sql. This is that case.
--
-- Idempotent: DROP CONSTRAINT IF EXISTS and DROP COLUMN IF EXISTS. The constraints are dropped
-- explicitly rather than left to fall with their columns, so a partially-applied 000137 (constraints
-- present, columns already gone, or vice versa) still rolls back cleanly.

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_operator_attestation_is_attributed;

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_operator_note_needs_an_attestation;

ALTER TABLE plaid_orphaned_items
    DROP COLUMN IF EXISTS operator_resolved_at,
    DROP COLUMN IF EXISTS operator_resolved_by,
    DROP COLUMN IF EXISTS operator_resolution_note;
