-- Migration: Lift legacy account customization onto the user_accounts chain, and record WHY a
--            chain is hidden.
-- Issue: #1609 (Item 3, part 1 of 3) — "hiding an account reverts on refresh"
--
-- =============================================================================
-- WHY
-- =============================================================================
--
-- Account customization is WRITTEN to user_accounts (user_account_repository.go) but READ from
-- accounts (account_repository.go, which does not join user_accounts at all). So hiding, renaming,
-- recolouring or reordering an account appears to work and then reverts on refresh.
--
-- The follow-up work (task 3b) makes the read CHAIN-AUTHORITATIVE:
--
--     CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.<field> ELSE a.<field> END
--
-- deliberately NOT COALESCE: the update API writes NULL to mean "the user cleared this override"
-- (user_account_service.go — an empty nickname sets Nickname = nil), and COALESCE would resurrect
-- the accounts value the user just deleted. This matches domain/account.go, which already
-- documents user_accounts as the source of truth and the accounts columns as retained only for
-- backward compatibility.
--
-- The consequence, and the reason this migration is MANDATORY rather than tidy-up: once the
-- overlay ships, accounts.<field> STOPS BEING READ for any chained row. Every legacy nickname,
-- colour, icon, emoji, display order and — most damagingly — every legacy HIDE that only ever
-- landed on accounts would silently disappear. user_accounts.is_hidden is NOT NULL DEFAULT FALSE
-- (000031), so an un-backfilled chain row un-hides an account the user hid.
--
-- Three steps, deliberately in ONE migration (they are not independent):
--
--   Step 1 (M1)  is_hidden lifted up the chain
--   Step 2 (M1b) nickname / custom_color / custom_icon / custom_emoji / display_order lifted up
--   Step 3 (M2)  new hidden_source column
--   Step 4       hidden_source classified — READS the is_hidden Step 1 just wrote, which is why
--                it cannot be a separate migration: a machine that applied Step 1 and stopped
--                would be left with hidden chains carrying no cause, and task 3c treats a chain
--                with no recorded cause as un-hideable-on-relink.
--
-- =============================================================================
-- Step 1 (M1): lift a legacy accounts.is_hidden onto its chain row
-- =============================================================================
--
-- ⚠ FAN-OUT. One user_account has MANY accounts rows (000033: "Multiple accounts can point to the
-- same user_account" — that is the whole point of the re-link chain). An
-- `UPDATE ... FROM accounts` would pick an ARBITRARY joined row and lose the hide whenever the
-- planner happened to pick a visible sibling. EXISTS is a semi-join: it asks the only question
-- that has a deterministic answer — "is ANY member hidden?" — and cannot depend on which row the
-- planner reaches first.
--
-- The polarity is OR, not AND: a hide is a user intent expressed about the real-world account,
-- and the chain is that real-world account. (000100 used bool_and when MERGING two chains, a
-- different question: there, every member was itself a chain that had already answered.)
--
-- Idempotent: the `ua.is_hidden = FALSE` guard means a second application matches nothing.

UPDATE user_accounts ua
SET is_hidden  = TRUE,
    updated_at = now()
WHERE ua.is_hidden = FALSE
  AND EXISTS (
      SELECT 1
      FROM accounts a
      WHERE a.user_account_id = ua.id
        AND a.is_hidden = TRUE
  );

-- =============================================================================
-- Step 2 (M1b): lift the nullable customization fields onto the chain row
-- =============================================================================
--
-- ⚠ Chain siblings may DISAGREE (the user renamed the account, then the bank was re-linked and a
-- fresh accounts row was created without the nickname). The winner must be TOTALLY determined —
-- no tie left to planner chance — so the ordering is:
--
--     (a.status = 'active') DESC   the row the user is looking at today wins
--     a.updated_at DESC            then the most recently touched
--     a.id DESC                    then id, which is unique, so the order is total
--
-- The active-first leg is load-bearing rather than cosmetic: a REMOVED sibling is frequently the
-- most recently written row (the removal itself stamps updated_at), so updated_at alone would
-- routinely promote a dead row's stale label over the live one.
--
-- This mirrors the idiom 000100 already uses for exactly this problem:
--     (array_remove(array_agg(x ORDER BY ...), NULL))[1]
-- i.e. "the first NON-NULL value in the given total order", which array_agg + array_remove express
-- without a per-column DISTINCT ON.
--
-- Idempotent, and non-destructive: COALESCE(ua.<f>, ...) only fills HOLES. user_accounts is
-- already the source of truth, so a value it holds — including a deliberate NULL the user cleared
-- AFTER this migration ran — is never overwritten. (On the FIRST application a chain-level NULL is
-- indistinguishable from "never set"; that is the one-time cost of the lift, and it is the
-- direction that preserves data.)

WITH chain AS (
    SELECT
        a.user_account_id AS ua_id,
        (array_remove(array_agg(a.nickname      ORDER BY (a.status = 'active') DESC, a.updated_at DESC, a.id DESC), NULL))[1] AS nickname,
        (array_remove(array_agg(a.custom_color  ORDER BY (a.status = 'active') DESC, a.updated_at DESC, a.id DESC), NULL))[1] AS custom_color,
        (array_remove(array_agg(a.custom_icon   ORDER BY (a.status = 'active') DESC, a.updated_at DESC, a.id DESC), NULL))[1] AS custom_icon,
        (array_remove(array_agg(a.custom_emoji  ORDER BY (a.status = 'active') DESC, a.updated_at DESC, a.id DESC), NULL))[1] AS custom_emoji,
        (array_remove(array_agg(a.display_order ORDER BY (a.status = 'active') DESC, a.updated_at DESC, a.id DESC), NULL))[1] AS display_order
    FROM accounts a
    WHERE a.user_account_id IS NOT NULL
    GROUP BY a.user_account_id
)
UPDATE user_accounts ua
SET nickname      = COALESCE(ua.nickname,      chain.nickname),
    custom_color  = COALESCE(ua.custom_color,  chain.custom_color),
    custom_icon   = COALESCE(ua.custom_icon,   chain.custom_icon),
    custom_emoji  = COALESCE(ua.custom_emoji,  chain.custom_emoji),
    display_order = COALESCE(ua.display_order, chain.display_order),
    updated_at    = now()
FROM chain
WHERE ua.id = chain.ua_id
  -- Touch only rows that actually gain something, so a re-run is a genuine no-op rather than a
  -- full-table updated_at churn.
  AND (   (ua.nickname      IS NULL AND chain.nickname      IS NOT NULL)
       OR (ua.custom_color  IS NULL AND chain.custom_color  IS NOT NULL)
       OR (ua.custom_icon   IS NULL AND chain.custom_icon   IS NOT NULL)
       OR (ua.custom_emoji  IS NULL AND chain.custom_emoji  IS NOT NULL)
       OR (ua.display_order IS NULL AND chain.display_order IS NOT NULL));

-- =============================================================================
-- Step 3 (M2): add user_accounts.hidden_source
-- =============================================================================
--
-- TWO different things hide a chain and current state cannot tell them apart:
--
--   SYSTEM  account_service.go hides a chain when its last active account is deleted, so an
--           orphaned chain does not linger as a ghost.
--   USER    the user hid it via PATCH /api/v1/user-accounts/{id}.
--
-- normalizeExistingUserAccount un-hides on every re-link/MatchOrCreate. That is CORRECT for the
-- system case and DESTROYS a deliberate user hide in the other, and both cases have zero active
-- accounts at MatchOrCreate time, so no existing column can distinguish them. accounts.removed_reason
-- is not a substitute: it records why a PHYSICAL ACCOUNT was removed, not why the CHAIN was hidden
-- (000154, domain/account.go).
--
-- Task 3c will make the un-hide conditional on hidden_source = 'system'.
--
-- Nullable, no default: the column means "why is this chain hidden", so it has no value for a
-- chain that is not hidden. Adding it without a default is also the non-rewriting form.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_accounts' AND column_name = 'hidden_source'
    ) THEN
        ALTER TABLE user_accounts ADD COLUMN hidden_source TEXT;
    END IF;
END $$;

-- NOT VALID first, VALIDATE after Step 4, so the constraint is enforced for every write from this
-- point on (NOT VALID skips only the scan of pre-existing rows) while the backfill below is what
-- makes the pre-existing rows conform.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_accounts_valid_hidden_source'
    ) THEN
        ALTER TABLE user_accounts ADD CONSTRAINT user_accounts_valid_hidden_source
            CHECK (hidden_source IS NULL OR hidden_source IN ('user', 'system', 'unknown'))
            NOT VALID;
    END IF;
END $$;

-- =============================================================================
-- Step 4: classify the existing hides
-- =============================================================================
--
-- ⚠ THIS IS THE SUBTLE PART. The naive rule — "has an active account => 'user', otherwise
-- 'system'" — is WRONG. It labels a chain the user hid and later disconnected as 'system', and
-- task 3c will then silently un-hide it on the next re-link: the exact defect this batch exists to
-- fix, reintroduced by our own migration.
--
-- The truthful three-state answer:
--
--   is_hidden AND the chain has >= 1 'active' account  ->  'user'
--       Only a human hides a LIVE account; the system's hide fires when the LAST active account
--       goes away, so a live account and a system hide cannot coexist.
--
--   is_hidden AND no active account                    ->  'unknown'
--       Genuinely ambiguous, and 'unknown' is load-bearing rather than a placeholder: task 3c
--       un-hides ONLY 'system', so 'unknown' stays hidden forever unless the user says otherwise.
--       Leaving an account hidden that the system hid is a cosmetic annoyance the user can undo;
--       un-hiding an account the user hid is a privacy regression they cannot even detect.
--
--   NOT is_hidden                                      ->  NULL
--
-- `hidden_source IS NULL` in the WHERE is what makes a re-application safe AFTER task 3c ships:
-- a cause the application has since recorded is never re-derived from state.

UPDATE user_accounts ua
SET hidden_source = CASE
        WHEN EXISTS (
            SELECT 1 FROM accounts a
            WHERE a.user_account_id = ua.id
              AND a.status = 'active'
        ) THEN 'user'
        ELSE 'unknown'
    END,
    updated_at = now()
WHERE ua.is_hidden = TRUE
  AND ua.hidden_source IS NULL;

-- A visible chain has no hide, so it can have no cause. Explicit rather than implied, so the
-- invariant survives a re-application over data written by later code.
UPDATE user_accounts ua
SET hidden_source = NULL,
    updated_at    = now()
WHERE ua.is_hidden = FALSE
  AND ua.hidden_source IS NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'user_accounts_valid_hidden_source' AND NOT convalidated
    ) THEN
        ALTER TABLE user_accounts VALIDATE CONSTRAINT user_accounts_valid_hidden_source;
    END IF;
END $$;

-- =============================================================================
-- Step 5: documentation
-- =============================================================================

COMMENT ON COLUMN user_accounts.hidden_source IS
$COMMENT$Why this chain is hidden, for rows with is_hidden = TRUE; NULL when is_hidden = FALSE. 'user' -- a human hid it via PATCH /api/v1/user-accounts/{id}. 'system' -- hidden automatically because the chain lost its last active account, so it would otherwise linger as a ghost. 'unknown' -- migration 000185 could not tell the two apart (the chain was already hidden and had no active account when it ran). ONLY 'system' is eligible for the automatic un-hide in normalizeExistingUserAccount: 'unknown' is deliberately conservative, because un-hiding an account the user hid is undetectable to them, whereas leaving a system hide in place is an annoyance they can undo. NOT derivable from accounts.removed_reason, which records why a PHYSICAL ACCOUNT was removed rather than why the CHAIN was hidden (migration 000154).$COMMENT$;

-- =============================================================================
-- Migration Complete
-- =============================================================================
