-- Issue #1497: reverse 000135.
--
-- WHAT THIS DESTROYS, and it is the unusual case where the data loss is the POINT rather than the
-- hazard: dropping access_token_encrypted destroys every retained Plaid credential. That is a GDPR
-- IMPROVEMENT, not a regression — the credential is the only personal data 000135 introduced, and this
-- rollback erases all of it immediately instead of on its 30-day deadline.
--
-- THE ACTUAL HAZARD is the other four columns. Dropping resolved_at, retry_attempt_count, last_retry_at
-- and last_retry_error_code destroys the record of which stranded Items were already dealt with, so
-- every row in the table reverts to looking outstanding. Combined with the credential being gone, the
-- state after this rollback is exactly 000134's: a queryable list of Plaid Items that an operator must
-- finish by hand at Plaid, with no automated path. Nothing bills that was not billing before; nobody's
-- erasure is blocked; there is simply no retry any more. Export the table first if the resolution
-- history matters.
--
-- ⚠ ROLL BACK THE CODE TOO, and this is the failure mode to expect if you do not. DeleteUserCascade's
-- INSERT names access_token_encrypted and token_expires_at explicitly, so with these columns gone that
-- statement ERRORS — and because it is mandatory and inside the cascade transaction, EVERY account
-- deletion fails with "column does not exist" rather than degrading. Fail-closed and loud, not a leak,
-- but a broken deletion path all the same. Same shape as 000134's own down-migration warning, and for
-- the same reason: the cascade's INSERT is a hard dependency on this table's shape.
--
-- ban-drop-column is an ENABLED Squawk rule; .down.sql files are excluded from linting via
-- excluded_paths in .squawk.toml precisely so a rollback can contain the DROP that reverses its
-- .up.sql. This is that case.
--
-- Idempotent: DROP COLUMN IF EXISTS, and DROP CONSTRAINT IF EXISTS. The constraint is dropped
-- explicitly rather than left to fall with its columns, so that a partially-applied 000135 (constraint
-- present, columns already gone) still rolls back cleanly.

ALTER TABLE plaid_orphaned_items
    DROP CONSTRAINT IF EXISTS plaid_orphaned_items_purged_token_is_null;

ALTER TABLE plaid_orphaned_items
    DROP COLUMN IF EXISTS access_token_encrypted,
    DROP COLUMN IF EXISTS token_expires_at,
    DROP COLUMN IF EXISTS token_purged_at,
    DROP COLUMN IF EXISTS retry_attempt_count,
    DROP COLUMN IF EXISTS last_retry_at,
    DROP COLUMN IF EXISTS last_retry_error_code,
    DROP COLUMN IF EXISTS resolved_at;

-- Restore 000134's table comment verbatim, so a rolled-back schema does not keep describing a
-- credential it no longer holds. (Comments are not restored automatically by dropping columns.)
COMMENT ON TABLE plaid_orphaned_items IS
    'Plaid Items that may still exist -- and may still bill -- at Plaid, whose local '
    'linked_items row was deleted by a GDPR erasure before removal could be proved. Written '
    'by DeleteUserCascade, inside the cascade transaction, for every linked_items row with '
    'plaid_removed_at IS NULL, BEFORE anything is deleted. This is a work list of outstanding '
    'vendor obligations, not an audit log: a row here means someone must still get this Item '
    'removed at Plaid. Deliberately carries NO user_id and no foreign key -- it must outlive '
    'the erased user and must not be a surviving pointer to them. Issue #1494.';
