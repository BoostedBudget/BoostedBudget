-- Issue #1497: give plaid_orphaned_items a BOUNDED, PURPOSE-BOUND copy of the Plaid access token, so
-- that an Item stranded by a GDPR erasure can actually be removed by this system instead of only by an
-- operator typing into Plaid's dashboard.
--
-- ⚠ THIS REVERSES A DECISION 000134 STATED IN PROSE, AND THAT FILE'S "WHAT IS DELIBERATELY NOT STORED:
-- the access token" PARAGRAPH IS NOW SUPERSEDED. It is left in place because an applied migration is a
-- historical record and editing one is how checksums and reviewers get lied to; read it as "the
-- position as of #1494", not as current policy. Its two arguments and what changed:
--
--   1. It argued the population reaching this table is exactly the population whose token is known
--      unusable (INVALID_ACCESS_TOKEN / NO_USABLE_ACCESS_TOKEN), so the credential would be retained
--      to perform an operation already proven impossible. THAT IS TRUE OF ONE LEG AND FALSE OF THE
--      TABLE. DeleteUserCascade's INSERT selects `plaid_removed_at IS NULL` — deliberately wider than
--      the C2 guard's predicate, as its own comment says — so it also copies 'replaced' re-link
--      leftovers, whose /item/remove was NEVER ATTEMPTED and whose token has never been rejected by
--      anything. For those rows a retry is not futile; it is the only automated path that exists.
--   2. It argued a bank-connection credential belonging to an erased user is a larger retention
--      commitment than an opaque item id, and that 000134's narrow GDPR argument would not cover it.
--      THAT REMAINS TRUE, and it is why this migration does not simply add a nullable column: the
--      retention is given a DEADLINE IN THE SCHEMA (token_expires_at, NOT NULL, so "retained
--      indefinitely" is not expressible) and an unconditional purge sweep keyed on nothing but that
--      deadline. The defensibility comes entirely from the bound, not from the purpose -- so the bound
--      is stated as it is actually kept: unusable from the deadline exactly, destroyed by the first
--      sweep at or after it (at most domain.OrphanedItemTokenPurgeInterval, 1 hour, later). See the
--      token_expires_at column comment; do not restate this as "cannot exist past the deadline".
--
-- THE GDPR POSITION, restated because it CHANGED with this migration and a table comment is not the
-- place a controller should have to discover that (see docs/decisions/2026-08-01-gdpr-erasure-hard-
-- delete.md § "Plaid orphaned items: bounded credential retention after erasure" for the record):
--
--   * WHAT is retained: one Plaid access token, encrypted at rest, for an Item belonging to a user
--     whose personal data has otherwise been erased. It is PERSONAL DATA. It is not anonymised and
--     cannot be — a token is a CAPABILITY, not an identifier: hashing or masking it stops it working,
--     which destroys the only reason to keep it, and a token that still works is a live pointer back
--     to the individual through Plaid's own records. Pseudonymised at best, and pseudonymised data is
--     still personal data. Do not propose "store it anonymised" again.
--   * WHY: to terminate a vendor billing relationship we are obliged to end and cannot end without it.
--     Storage limitation (Art. 5(1)(e)) permits purpose-bound, time-limited retention; this is that.
--   * HOW LONG: 30 days from the erasure (domain.OrphanedItemTokenRetention), the same boundary as the
--     account-deletion grace period this system already documents and operates
--     (config.DeletionGracePeriodDays' default) — a number the codebase can defend rather than a fresh
--     arbitrary one. ⚠ It is NOT derived from the 7-day C2 cooling-off window and must not be
--     conflated with it: that window bounds how long an ERASURE WAITS, this one bounds how long a
--     CREDENTIAL OUTLIVES an erasure. Two clocks, two purposes.
--   * THE PURGE FIRES REGARDLESS OF OUTCOME. Not "when the retries give up", not "when the Item is
--     removed" — when the deadline passes, whatever state anything else is in. A row whose retries
--     never ran, whose retry job was never registered, or whose Item is still live and still billing
--     loses its token on schedule and becomes an operator's problem at Plaid. That is the whole
--     bargain: the retention is bounded by a clock, not by success.
--     ⚠ REGARDLESS OF OUTCOME, NOT INSTANTANEOUSLY. The purge is a periodic sweep running every
--     domain.OrphanedItemTokenPurgeInterval (1 hour), so the credential is destroyed by the FIRST
--     sweep at or after the deadline — at most an hour past it. Its USE stops at the deadline itself,
--     enforced separately and exactly at every read (see the token_expires_at column comment). Both
--     halves are real; they are not the same strength, and saying otherwise would overstate the one
--     guarantee this feature's GDPR defensibility rests on.
--   * WHAT SURVIVES the purge: the record, indefinitely — plaid_item_id_encrypted, institution,
--     status, error codes, timestamps. No credential and no user PII. That is 000134's original,
--     unchanged position, and it is what the admin "manual intervention required in Plaid" state reads.
--
-- LOCK COST AND SQUAWK, from the semantics rather than from a green lint run (which in this repo is
-- not evidence — .squawk.toml carries a 13-entry excluded_rules list, and 000133/000134 both say so):
--
--   * ADD COLUMN with a constant DEFAULT is metadata-only on PostgreSQL 11+ (no table rewrite); the
--     ACCESS EXCLUSIVE lock is held for the catalogue update only. NOW() is a stable function
--     evaluated once for the existing-rows default, which is why token_expires_at can be NOT NULL
--     without a rewrite. `adding-not-nullable-field` is an ENABLED rule and it fires on NOT NULL
--     WITHOUT a default; token_expires_at has one, deliberately (see its column comment).
--   * The CHECK constraint is added and validated in one step. `constraint-missing-not-valid` is in
--     excluded_rules, so the linter would not have objected either way — the reason it is safe is that
--     this table holds single digits of rows (it takes a permanently-unremovable Plaid Item AND a
--     completed erasure to make one), so the validating scan is a scan of nothing. If this table ever
--     grows to a size where that matters, that growth is itself the alarming signal 000134 named.
--   * No new index. 000134's argument is unchanged and now applies to two more scans (the retry
--     selection and the purge): a full scan of single digits of rows costs less than an index costs to
--     maintain, in I/O and in prose. Both sweeps are additionally capped per run.
--
-- Idempotent: ADD COLUMN IF NOT EXISTS throughout, and the CHECK constraint is guarded by a
-- pg_constraint lookup (ADD CONSTRAINT has no IF NOT EXISTS). COMMENTs are unconditional, which is
-- safe — setting a comment twice leaves the same comment.

ALTER TABLE plaid_orphaned_items
    ADD COLUMN IF NOT EXISTS access_token_encrypted TEXT,
    ADD COLUMN IF NOT EXISTS token_expires_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS token_purged_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS retry_attempt_count    INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_retry_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_retry_error_code  VARCHAR(100),
    ADD COLUMN IF NOT EXISTS resolved_at            TIMESTAMPTZ;

-- The one invariant worth spending a constraint on: A ROW THAT SAYS ITS TOKEN WAS PURGED HAS NO TOKEN.
-- Every "purge happened" claim in this system — the admin state machine, the purge sweep's own log
-- line, the retry selection's refusal to touch a purged row — reads token_purged_at rather than
-- re-inspecting the ciphertext. Without this constraint a single buggy UPDATE that set the timestamp
-- and forgot the NULL would make every one of those claims false, silently and durably, which is
-- exactly the failure shape (mark written, work not done, mark believed forever) that four earlier
-- fixes on this branch each closed by hand. Here the database can simply refuse.
--
-- Deliberately NOT the converse: a NULL token with NULL token_purged_at is legal and expected — every
-- row written before this migration is in that state, and never having held a credential is not the
-- same fact as having had one purged.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'plaid_orphaned_items_purged_token_is_null'
          AND conrelid = 'plaid_orphaned_items'::regclass
    ) THEN
        ALTER TABLE plaid_orphaned_items
            ADD CONSTRAINT plaid_orphaned_items_purged_token_is_null
            CHECK (token_purged_at IS NULL OR access_token_encrypted IS NULL);
    END IF;
END $$;

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

COMMENT ON COLUMN plaid_orphaned_items.access_token_encrypted IS
    'ENCRYPTED (FieldEncryptor, the same path and key as the linked_items.access_token_encrypted '
    'column it is copied verbatim from) Plaid access token for this Item, retained ONLY so an '
    'automated /item/remove retry can run after the user''s erasure. PERSONAL DATA, and not '
    'anonymisable -- a token is a capability, not an identifier. NULL means no credential is held, '
    'which is either "purged" (token_purged_at IS NOT NULL) or "never retained" (rows written before '
    'migration 000135, and any row whose source linked_items.access_token_encrypted was itself '
    'empty). NEVER read this column without checking token_expires_at: past the deadline the value is '
    'garbage-in-waiting, and the retry selection refuses it independently of the purge sweep having '
    'run. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.token_expires_at IS
    'RETENTION DEADLINE on the credential above, in two parts of DIFFERENT STRENGTH -- keeping them '
    'distinct is the point, because an earlier version of this comment claimed both and only one '
    'holds. (1) THE TOKEN MUST NOT BE USED FROM THIS INSTANT, exactly: every reader re-checks the '
    'deadline against a fresh clock and refuses an expired value (domain.TokenIsLive is strict at the '
    'boundary, GetRetryablePage selects token_expires_at > asOf, and the retry sweep re-checks per row '
    'immediately before each Plaid call), so this half does not wait for any purge. (2) THE TOKEN '
    'CEASES TO EXIST AT THE FIRST PURGE SWEEP AT OR AFTER THIS INSTANT -- so at most '
    'domain.OrphanedItemTokenPurgeInterval (1 hour) later, and longer only if a single run''s 1,000-row '
    'cap is reached and defers the remainder to the next run. The purge is unconditional and '
    'independent of whether Plaid is configured, which is the load-bearing property; it is not '
    'instantaneous, and this comment states the bound numerically rather than overstating it. '
    'Set by DeleteUserCascade to orphaned_at + domain.OrphanedItemTokenRetention (30 days, '
    'matching the account-deletion grace period this system already operates -- NOT the 7-day C2 '
    'cooling-off window, which is a different clock for a different purpose). NOT NULL so that '
    '"retained indefinitely" is not expressible. DEFAULT NOW() so that a writer which forgets to set '
    'it produces an ALREADY-EXPIRED row -- i.e. the purge sweep takes the token on its next pass -- '
    'rather than an unbounded one; the default is a fail-safe, not the intended value. Kept after the '
    'purge as history: it records when the retention window closed. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.token_purged_at IS
    'When access_token_encrypted was set to NULL, and the flag every "the credential is gone" claim '
    'in this system reads. Set on BOTH purge paths -- a successful removal (purge immediately, the '
    'purpose is spent) and expiry (purge unconditionally, regardless of retry state, attempt count or '
    'resolution). NULL on a row that never held a credential, which is why this is not simply '
    '"access_token_encrypted IS NULL": never-retained and purged are different facts. The CHECK '
    'constraint plaid_orphaned_items_purged_token_is_null makes the pairing unfalsifiable. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.retry_attempt_count IS
    'How many automated /item/remove retries this record has had since the erasure. Shown in the '
    'admin dashboard so an operator can tell a row that is being worked from one that is not. NOT a '
    'give-up counter and NOT an input to any predicate: nothing in this system stops retrying on a '
    'count, and nothing purges on one -- only token_expires_at ends the retrying, so a high count is '
    'information, never a state. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.last_retry_at IS
    'When the most recent automated retry ran, successful or not. NULL means no retry has ever run '
    'for this record -- which, on a row whose token is live and unexpired, is the signal that the '
    'retry job is not doing its job. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.last_retry_error_code IS
    'The Plaid error_code from the most recent FAILED retry, or NULL if no retry has failed. '
    'Deliberately SEPARATE from last_error_code, which 000134 defines as the code at the moment of '
    'erasure: overwriting that would destroy the diagnostic that explains why the row exists at all. '
    'Fixed-vocabulary diagnostic, not user data; carries the local sentinel '
    '''ORPHAN_TOKEN_UNDECRYPTABLE'' when the retained ciphertext could not be decrypted (a key '
    'rotation), which no retry can ever recover from. Issue #1497.';

COMMENT ON COLUMN plaid_orphaned_items.resolved_at IS
    'When this Item was PROVED removed at Plaid by an automated retry -- the /item/remove that '
    'succeeded, or that came back ITEM_NOT_FOUND, which means the same thing. NULL means outstanding. '
    'A resolved row is kept as history and is never a work item again; it is the only state in this '
    'table that means "nobody needs to do anything". 000134 deliberately shipped WITHOUT this column '
    'because nothing would have written it; the retry sweep added by #1497 is that writer. Note it '
    'says nothing about the credential -- the purge is tracked separately by token_purged_at, on '
    'purpose, so that "resolved" can never be the reason a token is believed gone. Issue #1497.';
