-- #1569 round 11 task 3: freeze a chain's identity on user_accounts, separate from the
-- last-writer-wins primary_type/primary_subtype pair.
--
-- WHY THIS EXISTS. The match key for re-link detection is
-- (user_id, institution_id_blind_idx, mask_blind_idx) — see migration 000100. That key can only be
-- narrowed safely using columns that never change after a chain is minted. primary_type and
-- primary_subtype (migration 000031) are explicitly documented "from most recent Plaid link" and
-- are OVERWRITTEN on every re-link (UserAccountRepository.UpdatePrimaryTypeAndSubtype). Keying
-- anything on them would mean a Plaid-reported subtype change at re-link silently breaks or merges
-- a chain — the exact double-counting bug this branch exists to prevent. This task does NOT change
-- the match key (that is a later task); it only adds the columns and populates them correctly, so
-- nothing here changes re-link matching behaviour yet.
--
-- WHAT "IDENTITY" MEANS FOR AN EXISTING (ALREADY-MERGED) ROW.
-- Before this migration, migration 000100 already merged distinct real-world accounts that shared
-- an institution and a NULL mask onto one user_accounts row (NULLS NOT DISTINCT). Such a row's own
-- primary_type/primary_subtype have ALREADY LOST the distinction between its members — they hold
-- whichever physical account's link wrote last. So this backfill reads identity_type/identity_subtype
-- from the `accounts` table directly (each physical account still carries its own type/subtype),
-- never from user_accounts.primary_type/primary_subtype.
--
-- A merged row can have physical accounts that DISAGREE on type (e.g. a brokerage and an IRA
-- sharing one user_accounts row because both had a NULL mask at the same institution). For such a
-- row, this backfill picks the EARLIEST-CREATED physical account (created_at ASC, id ASC — the same
-- deterministic tie-break migration 000100 and lookupUserAccountID already use to pick a survivor)
-- as the representative, and writes ITS type/subtype. This is a PROVISIONAL, NON-AUTHORITATIVE
-- value: it exists so the column is never NULL for a row that has at least one physical account, not
-- because the chosen account is "more correct" than its siblings. A future task that splits merged
-- chains back apart MUST re-derive each split's identity from `accounts` directly — never trust this
-- backfilled value on a pre-existing row as ground truth for which members belong together, since it
-- was never derived from a 1:1 mapping in the disagreeing case.
--
-- A user_accounts row with NO physical accounts pointing at it (an orphan customization row) gets
-- no identity_type/identity_subtype from this backfill and stays NULL — there is nothing to derive
-- it from.
--
-- COST ON A LARGE TABLE. The backfill UPDATE below is a single indexed join
-- (accounts.user_account_id has idx_accounts_user_account) scoped to `WHERE identity_type IS NULL`,
-- not a per-row loop, so its cost is proportional to the number of (user_accounts, accounts) rows
-- once, not to repeated full scans. Production currently has ZERO users (this is the cheapest this
-- migration will ever be to run), and local/QA user_accounts tables are small. If this table ever
-- grows large BEFORE this migration ships, the safe alternative is to split the backfill into a
-- separate, batched, resumable application-side job (the same shape as database/backfill.go) run
-- with the columns already nullable, rather than one unbounded UPDATE inside this migration.
--
-- WRITE-ONCE ENFORCEMENT. Application code writes identity_type/identity_subtype exactly once, at
-- mint (UserAccountService.MatchOrCreate's Create path, and database/backfill.go's
-- synthesiseUserAccountID) — see TestNoUpdateOfUserAccountsIdentityColumns
-- (user_account_identity_immutability_guard_test.go) for the AST-level guard on Go source. That
-- guard cannot see a future raw-SQL write path (a psql script, a different service), so this
-- migration ALSO adds a BEFORE UPDATE trigger that rejects any UPDATE changing either column. The
-- trigger is created AFTER the backfill UPDATE below, in the same transaction, so the one-time
-- historical correction (NULL -> value) is never itself blocked by the guarantee it is about to
-- start enforcing.

-- =============================================================================
-- Step 1: Add the columns (idempotent)
-- =============================================================================
ALTER TABLE user_accounts ADD COLUMN IF NOT EXISTS identity_type VARCHAR(50);
ALTER TABLE user_accounts ADD COLUMN IF NOT EXISTS identity_subtype VARCHAR(50);

COMMENT ON COLUMN user_accounts.identity_type IS
    '#1569 round 11 task 3: the FROZEN identity of this chain, written once at mint and never '
    'rewritten. NOT the same column as primary_type, which is last-writer-wins and is overwritten '
    'on every re-link. Enforced by trg_user_accounts_identity_immutable and by '
    'TestNoUpdateOfUserAccountsIdentityColumns. Nothing reads this column for matching yet.';
COMMENT ON COLUMN user_accounts.identity_subtype IS
    '#1569 round 11 task 3: see identity_type — same guarantee, for subtype.';

-- Mirrors user_accounts_valid_type's domain for primary_type (migration 000031); identity_type
-- carries the same valid Plaid account types.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_accounts_valid_identity_type'
    ) THEN
        ALTER TABLE user_accounts
            ADD CONSTRAINT user_accounts_valid_identity_type
            CHECK (identity_type IS NULL OR identity_type IN
                ('depository', 'credit', 'loan', 'investment', 'brokerage', 'other'));
    END IF;
END $$;

-- =============================================================================
-- Step 2: Backfill existing rows from `accounts`, NOT from user_accounts.primary_type/subtype
-- (see the header comment for why, and what a disagreeing merged row receives).
-- =============================================================================
WITH representative AS (
    SELECT DISTINCT ON (a.user_account_id)
        a.user_account_id,
        a.type,
        a.subtype
    FROM accounts a
    WHERE a.user_account_id IS NOT NULL
    ORDER BY a.user_account_id, a.created_at ASC, a.id ASC
)
UPDATE user_accounts ua
SET identity_type = representative.type,
    identity_subtype = representative.subtype
FROM representative
WHERE representative.user_account_id = ua.id
  AND ua.identity_type IS NULL;

-- =============================================================================
-- Step 3: Enforce immutability at the database level (created AFTER the backfill above, so the
-- one-time historical correction is never blocked by the trigger it is about to start enforcing).
-- =============================================================================
CREATE OR REPLACE FUNCTION enforce_user_accounts_identity_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.identity_type IS DISTINCT FROM OLD.identity_type
       OR NEW.identity_subtype IS DISTINCT FROM OLD.identity_subtype THEN
        RAISE EXCEPTION
            '#1569: user_accounts.identity_type/identity_subtype are frozen at mint and cannot be updated (id=%)',
            OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_user_accounts_identity_immutable ON user_accounts;
CREATE TRIGGER trg_user_accounts_identity_immutable
    BEFORE UPDATE ON user_accounts
    FOR EACH ROW
    EXECUTE FUNCTION enforce_user_accounts_identity_immutable();
