-- #1413: durable work-claim table for destructive side effects (Codex review round 2).
--
-- Round 1 remediation replaced "read -> check -> act" races with a conditional UPDATE
-- whose WHERE clause carried the authorising predicate, acting on RowsAffected() > 0
-- (see profile_repository.go ClaimForDeletion / ClaimPlaidDisconnectIfPastDue). Two of
-- those were self-assigning no-op writes purely to get an atomic row-level check.
--
-- That is an atomic GUARD, not a CLAIM: it proves the predicate held at that instant but
-- reserves nothing. Once it commits, a second caller evaluates the same predicate, sees
-- the same state, and also proceeds -- two workers can both disconnect the same user's
-- Plaid items, or both execute a GDPR deletion.
--
-- work_claims fixes this with a leased-claim table shared by every destructive action
-- (Plaid disconnect on past-due, past-due grace cancel, account deletion, retention
-- notices, subscription-cancelled cleanup). Exclusivity comes from the UNIQUE constraint
-- below, not from application logic:
--
--   INSERT ... ON CONFLICT (resource_type, resource_id, action) DO UPDATE
--     SET ... WHERE work_claims.completed_at IS NULL
--           AND work_claims.lease_expires_at < NOW()
--
-- Only one concurrent caller can ever have that INSERT-or-conditional-UPDATE actually
-- affect a row; Postgres serializes conflicting inserts on the unique index. A lease that
-- has expired (crashed worker) is reclaimable by the next caller. A completed claim is
-- never re-granted, because completed_at IS NULL is part of the reclaim predicate, not
-- because we deleted the row.
--
-- No per-action columns are added to users -- that would be five migrations and five
-- bespoke predicates for a mechanism that is otherwise identical across actions.
--
-- The claim is deliberately narrow: it establishes EXCLUSIVITY only. The existing
-- conditional state-transition predicates (e.g. claimForDeletionPredicate) still gate
-- CORRECTNESS ("is this user still eligible") and must be re-checked after the claim is
-- won, inside the same call, before any irreversible external work.

CREATE TABLE IF NOT EXISTS work_claims (
    id               BIGSERIAL   PRIMARY KEY,
    resource_type    TEXT        NOT NULL,
    resource_id      UUID        NOT NULL,
    action           TEXT        NOT NULL CHECK (action IN (
                                     'plaid_disconnect_past_due',
                                     'past_due_grace_cancel',
                                     'account_deletion',
                                     'retention_notice_30d',
                                     'retention_notice_7d',
                                     'subscription_cancelled_cleanup'
                                 )),
    claimed_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_expires_at TIMESTAMPTZ NOT NULL,
    completed_at     TIMESTAMPTZ,
    attempts         INT         NOT NULL DEFAULT 1,
    last_error       TEXT,

    CONSTRAINT work_claims_unique_active
        UNIQUE (resource_type, resource_id, action)
);

COMMENT ON TABLE work_claims IS
    'Leased exclusivity claims for destructive, at-most-once-in-flight actions on a '
    'resource (e.g. a user). Not a queue and not an audit log -- one row per '
    '(resource_type, resource_id, action) tuple, reused across attempts via '
    'INSERT ... ON CONFLICT DO UPDATE. Part of issue #1413, Codex review round 2.';

COMMENT ON COLUMN work_claims.resource_type IS
    'The kind of resource being claimed, e.g. ''user''. Generic (not a foreign key) '
    'because this table is shared across multiple destructive-action call sites.';

COMMENT ON COLUMN work_claims.resource_id IS
    'The resource''s primary key (e.g. users.id). Not a foreign key: work_claims must '
    'outlive the resource it references (a claim can legitimately be the last thing '
    'written before a user row is deleted) and may in future cover non-user resources.';

COMMENT ON COLUMN work_claims.action IS
    'One of a fixed set of destructive-action constants (see the CHECK constraint). '
    'No free-form strings at call sites -- callers use the Go action constants in '
    'internal/repository so a typo cannot silently create a new, unclaimed action.';

COMMENT ON COLUMN work_claims.claimed_at IS
    'When the current lease was (re-)acquired. Updated every time Claim wins, '
    'including reclaims of an expired lease.';

COMMENT ON COLUMN work_claims.lease_expires_at IS
    'The lease is reclaimable once NOW() passes this timestamp, so a crashed worker '
    'cannot block the action forever. Release sets this into the past so the claim '
    'becomes immediately reclaimable rather than waiting out the full lease.';

COMMENT ON COLUMN work_claims.completed_at IS
    'NULL while the work is unclaimed or in flight. Once set, the claim is durably '
    'finished and Claim never re-grants it -- this is what makes completion permanent, '
    'as opposed to merely un-leased.';

COMMENT ON COLUMN work_claims.attempts IS
    'Number of times this (resource_type, resource_id, action) has been claimed, '
    'including reclaims after an expired lease or an explicit Release. Starts at 1 on '
    'the first Claim.';

COMMENT ON COLUMN work_claims.last_error IS
    'Reason recorded by the most recent Release (e.g. an external API failure). '
    'NULL after a fresh Claim or a Complete. Diagnostic only -- not used in any '
    'predicate.';

COMMENT ON CONSTRAINT work_claims_unique_active ON work_claims IS
    'The exclusivity mechanism itself: Claim''s INSERT ... ON CONFLICT targets this '
    'constraint. Two concurrent claimants can never both win -- Postgres serializes '
    'on the backing unique index, so exactly one INSERT-or-conditional-UPDATE affects '
    'a row.';
