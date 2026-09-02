-- Rollback for 000167: narrow accounts_removed_reason_valid back to 000154's two values.
--
-- ⚠ LOSSY, AND IT REFUSES RATHER THAN DESTROYS. Narrowing the CHECK while 'link_removed' rows exist
-- would make the constraint unsatisfiable, so this migration must decide what happens to them. The
-- two silent options are both unacceptable:
--
--   * rewrite them to 'user_disconnect' -- a lie about the user's intent, and unrecoverable;
--   * rewrite them to 'provider_absent' -- a lie AND an authorisation change, since that is the one
--     value accountRepository.RestoreAccount accepts. Every such account would become eligible for
--     automatic resurrection under a Plaid Item that no longer exists.
--
-- So this rollback REFUSES when any 'link_removed' row exists, and says how to proceed. An operator
-- who genuinely wants to roll back must state, in their own statement, which of the two lies they
-- are willing to tell. Fail-closed is the same direction 000154's own down migration chose when it
-- documented itself as lossy rather than reversible.
--
-- Idempotent: on a database with no 'link_removed' rows this runs to completion and leaves exactly
-- 000154's constraint; re-running it is a no-op of the same shape.

DO $$
DECLARE
    n BIGINT;
BEGIN
    SELECT COUNT(*) INTO n FROM accounts WHERE removed_reason = 'link_removed';
    IF n > 0 THEN
        RAISE EXCEPTION
            'refusing to roll back 000167: % account row(s) carry removed_reason = ''link_removed'', '
            'which the 000154 constraint does not admit. Rewriting them to ''user_disconnect'' would '
            'falsify the user''s intent; rewriting them to ''provider_absent'' would additionally make '
            'them eligible for automatic restore under a Plaid Item that no longer exists. Decide '
            'explicitly and run the UPDATE yourself before retrying this rollback.', n;
    END IF;
END $$;

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_removed_reason_valid;

-- Restores 000154's exact predicate. NOT VALID + VALIDATE is omitted for the same reason the up
-- migration omits it, in reverse: the DO block above has already PROVEN no row violates this
-- narrower predicate, so a validation pass has nothing to find.
ALTER TABLE accounts ADD CONSTRAINT accounts_removed_reason_valid
    CHECK (
        removed_reason IS NULL
        OR (status = 'removed' AND removed_reason IN ('user_disconnect', 'provider_absent'))
    );

COMMENT ON COLUMN accounts.removed_reason IS
$COMMENT$Who removed this account, for rows with status = 'removed': 'user_disconnect' (the user asked, via DisconnectAccount/DisconnectInstitution) or 'provider_absent' (Plaid stopped returning it from a confirmed-complete /accounts/get, i.e. the bank closed it). NULL for every other status, and for 'removed' rows written before migration 000154. ONLY 'provider_absent' is eligible for automatic restore -- NULL is not, deliberately: a user disconnect must never be undone by a sync. The CAUSE lives here rather than in account_status_history.reason, which records the transition only (see migration 000153).$COMMENT$;
