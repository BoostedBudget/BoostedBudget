-- Issue #1497 follow-up: RE-ISSUE two COMMENTs that were edited into 000135 AFTER it had already
-- been applied to running databases.
--
-- WHY THIS FILE EXISTS AT ALL. 000135 originally described the credential retention deadline as a
-- HARD DEADLINE past which the token "must not exist". That overstated the guarantee. The real bound
-- has two halves of DIFFERENT STRENGTH: the token's USE stops at the deadline exactly (re-checked
-- against a fresh clock on every path that could present it to Plaid), while its EXISTENCE ends at
-- the first purge sweep at or after the deadline -- normally within an hour, but that is a bound PER
-- SWEEP and not an absolute ceiling. Both halves were rewritten in place inside 000135's own
-- .up.sql, which is legitimate there because that commit is not an ancestor of origin/main and no
-- production database has run it -- but golang-migrate never re-runs an applied version, so every
-- QA/staging database that had already applied 000135 still carries the OLD, OVERSTATED text in its
-- pg_description catalogue. This migration is the catch-up: it re-issues the two corrected comments
-- so the catalogue and the repository agree everywhere, on databases old and new.
--
-- ⚠ THE TWO COMMENT BODIES BELOW ARE COPIED VERBATIM FROM 000135 AS IT NOW STANDS. They are not a
-- summary and must never become one. If 000135's wording is ever corrected again, either re-issue it
-- here in a NEW migration or update BOTH sites in lockstep -- a divergence between them would put
-- two different retention claims in the tree, which is the exact failure this file is fixing.
--
-- ⚠ DO NOT RE-COMPRESS THE TWO HALVES INTO ONE. "The token is gone at most an hour after the
-- deadline" is the paraphrase that keeps reappearing and it is wrong twice over: it drops the exact
-- boundary on USE, and it converts a per-sweep bound into an absolute ceiling. A sweep that does not
-- run or does not drain (per-run cap, downtime, leader election, worker starvation, a failed run
-- before River retries it) extends existence past that hour. The GDPR defensibility of this feature
-- rests on the one guarantee that IS exact, so the two must stay distinct.
--
-- SCOPE: comments only. No column, constraint, index, default or datum is touched, so there is no
-- lock beyond the catalogue update COMMENT itself takes and nothing for Squawk's table-rewrite,
-- not-null or constraint rules to fire on. The other five column comments 000135 sets were never
-- changed and are deliberately NOT re-issued -- re-issuing unchanged text would make it harder for
-- the next reader to see which two claims actually moved.
--
-- Idempotent: COMMENT is inherently repeatable (setting the same comment twice leaves the same
-- comment), and both statements are guarded on the object still existing so this is a clean no-op
-- rather than an error on a database where 000135 was rolled back.

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
        'automated retry can finish the removal; an hourly sweep keyed on NOTHING BUT that deadline '
        'purges it at the first run at or after the deadline, whether or not the removal ever '
        'succeeded and whether or not the retry job ever ran (the token stops being USABLE at the '
        'deadline itself -- see the token_expires_at column comment for the two halves). '
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
        'RETENTION DEADLINE on the credential above, in two parts of DIFFERENT STRENGTH -- keeping them '
        'distinct is the point, because an earlier version of this comment claimed both and only one '
        'holds. (1) THE TOKEN MUST NOT BE USED FROM THIS INSTANT, exactly: every path that could present '
        'it to Plaid re-checks this deadline against a fresh clock and refuses an expired value '
        '(domain.TokenIsLive is strict at the boundary, GetRetryablePage selects token_expires_at > asOf, '
        'and the retry sweep re-checks per row immediately before each Plaid call), so this half does not '
        'wait for any purge. The admin List read deliberately does NOT filter on the deadline -- it must '
        'show expired rows, that being the whole manual-intervention work list -- but it cannot leak the '
        'credential either: PlaidOrphanedItemAdminView has no token field and must never gain one. (2) '
        'THE TOKEN '
        'CEASES TO EXIST AT THE FIRST PURGE SWEEP AT OR AFTER THIS INSTANT -- normally '
        'domain.OrphanedItemTokenPurgeInterval (1 hour) later. That is a bound PER SWEEP, not an absolute '
        'ceiling: it is exceeded whenever a sweep does not run or does not drain (the 1,000-row per-run '
        'cap, downtime, leader election, worker starvation, a failed run before River retries it). The '
        'purge is unconditional and independent of whether Plaid is configured, which is the load-bearing '
        'property -- but unconditional is neither instantaneous nor a guarantee of execution, and this '
        'comment states the bound as it is actually kept rather than overstating it. '
        'Set by DeleteUserCascade to orphaned_at + domain.OrphanedItemTokenRetention (30 days, '
        'matching the account-deletion grace period this system already operates -- NOT the 7-day C2 '
        'cooling-off window, which is a different clock for a different purpose). NOT NULL so that '
        '"retained indefinitely" is not expressible. DEFAULT NOW() so that a writer which forgets to set '
        'it produces an ALREADY-EXPIRED row -- i.e. the purge sweep takes the token on its next pass -- '
        'rather than an unbounded one; the default is a fail-safe, not the intended value. Kept after the '
        'purge as history: it records when the retention window closed. Issue #1497.';
    END IF;
END $$;
