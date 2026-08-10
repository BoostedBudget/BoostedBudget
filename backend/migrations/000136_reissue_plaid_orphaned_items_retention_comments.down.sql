-- Reverse 000136 by restoring the two COMMENT texts as 000135 ORIGINALLY shipped them, verbatim --
-- the same convention 000129's down migration follows: "down" for a comment-only migration means
-- putting the previous wording back, not dropping the comment or the object it describes (both are
-- owned by 000135 and its own down migration).
--
-- ⚠ READ THIS BEFORE RUNNING IT. The text restored below is the OVERSTATED wording 000136 exists to
-- replace: it claims the retained credential "must not exist" past token_expires_at, which is a
-- guarantee this system does not make. Existence ends at the FIRST PURGE SWEEP at or after the
-- deadline -- normally within an hour, but that is a bound per sweep, not a ceiling. Only the token's
-- USE stops at the deadline exactly. Rolling this back therefore reinstates a known-false retention
-- claim in the database catalogue, which matters because that catalogue is what a controller or an
-- auditor reads. Roll back only as part of rolling 000136 out of the tree, and never as a way of
-- "restoring" the retention documentation.
--
-- Guarded exactly as up.sql is, so this is a no-op rather than an error on a database where 000135
-- was rolled back first and the table or column is gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = current_schema()
          AND table_name = 'plaid_orphaned_items'
    ) THEN
        COMMENT ON TABLE plaid_orphaned_items IS
        'Plaid Items that may still exist -- and may still bill -- at Plaid, whose local '
        'linked_items row was deleted by a GDPR erasure before removal could be proved. Written '
        'by DeleteUserCascade, inside the cascade transaction, for every linked_items row with '
        'plaid_removed_at IS NULL, BEFORE anything is deleted. This is a work list of outstanding '
        'vendor obligations, not an audit log: a row here means someone must still get this Item '
        'removed at Plaid. Deliberately carries NO user_id and no foreign key -- it must outlive '
        'the erased user and must not be a surviving pointer to them. Issues #1494, #1497. '
        'SINCE #1497 IT ALSO CARRIES A CREDENTIAL, BOUNDED: access_token_encrypted holds the '
        'erased user''s Plaid access token until token_expires_at (30 days from the erasure), so an '
        'automated retry can finish the removal; a sweep keyed on NOTHING BUT that deadline purges '
        'it whether or not the removal ever succeeded and whether or not the retry job ever ran. '
        'After the purge the row keeps no credential and no user PII, and is the admin dashboard''s '
        '"manual intervention required in Plaid" work item. 000134''s prose saying the token is '
        'deliberately not stored describes the position as of #1494 and is superseded -- see '
        '000135 for which of its two arguments failed and why.';
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'plaid_orphaned_items'
          AND column_name = 'token_expires_at'
    ) THEN
        COMMENT ON COLUMN plaid_orphaned_items.token_expires_at IS
        'HARD DEADLINE on the credential above: after this instant the token must not exist and must not '
        'be used. Set by DeleteUserCascade to orphaned_at + domain.OrphanedItemTokenRetention (30 days, '
        'matching the account-deletion grace period this system already operates -- NOT the 7-day C2 '
        'cooling-off window, which is a different clock for a different purpose). NOT NULL so that '
        '"retained indefinitely" is not expressible. DEFAULT NOW() so that a writer which forgets to set '
        'it produces an ALREADY-EXPIRED row -- i.e. the purge sweep takes the token on its next pass -- '
        'rather than an unbounded one; the default is a fail-safe, not the intended value. Kept after the '
        'purge as history: it records when the retention window closed. Issue #1497.';
    END IF;
END $$;
