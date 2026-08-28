-- #1569 round 11 task 4: narrow the user_accounts match key to include the FROZEN identity
-- columns (identity_type, identity_subtype — migration 000177), and split any already-merged
-- chain whose physical accounts disagree on that identity.
--
-- WHY. Migration 000100's unique index (`unique_user_accounts_inst_mask_blind`) keys re-link
-- matching on (user_id, institution_id_blind_idx, mask_blind_idx) alone. ⚠⚠ ROUND 2 FIX: this
-- collision is NOT maskless-only, and treating it as such was itself a defect fixed this round
-- (see the Step 5 note below for the money bug it caused). TWO SEPARATE mechanisms force two
-- different real-world accounts onto one user_accounts row:
--   (a) NULLS NOT DISTINCT makes the index treat two NULL mask_blind_idx values as equal, so two
--       MASKLESS accounts at one institution collide — the textbook case is a brokerage and an
--       IRA at one broker (both `type = 'investment'`, differing only in `subtype`; there is no
--       `type = 'brokerage'`, see 000177's header and TestAccountTypeUniverseMatchesTheHoldingsGate).
--   (b) The index is a PLAIN unique constraint on any NON-NULL mask_blind_idx too — two MASKED
--       accounts that happen to share the same institution and mask (institutions do reuse an
--       account-number suffix across products; this needs no NULL-handling quirk at all) collide
--       exactly the same way, and FindByInstitutionAndMask's `mask_blind_idx = $3` exact-match
--       branch reuses the existing row for it.
-- Either shape is the root cause behind the recorded $60 -> $0 household rewrite this round exists
-- to fix: two real accounts sharing one chain key means only one of them can ever be "the" live
-- account for that chain, and — as this round's fix discovered — sharing one dedup FINGERPRINT
-- after a naive split reintroduces the same defect in the household aggregate specifically.
--
-- WHAT THIS MIGRATION DOES, IN ORDER, IN ONE TRANSACTION:
--   1. Swap the match-key unique index FIRST, for one that also includes
--      identity_type/identity_subtype. This MUST happen before the split inserts below: the OLD
--      index (institution+mask only) is exactly what forced the merge in the first place — for
--      EITHER mechanism (a) or (b) above — so minting a second row at the same institution+mask
--      while the old index is still live fails with a duplicate-key error against the very index
--      this migration exists to replace, whether the colliding pair is maskless or shares a
--      non-NULL mask. ✅ Probed against real Postgres — the first draft of this migration had
--      the swap last and failed exactly this way when replayed against rigged merged data.
--   2. For every already-merged user_accounts row whose physical accounts (`accounts` rows
--      pointing at it) disagree with the row's OWN identity_type/identity_subtype, mint one new
--      user_accounts row per additional (identity_type, identity_subtype) group. The ORIGINAL
--      row keeps the group matching its own identity_type/identity_subtype — which migration
--      000177's backfill already set to the EARLIEST physical account's type/subtype, so the
--      earliest account never moves.
--   3. Repoint every physical account in a moved group's `accounts.user_account_id` to its new
--      row.
--   4. Repoint every other table that carries a `user_account_id`-shaped column, deriving each
--      from accounts.user_account_id (now authoritative) via the PHYSICAL account_id/from_
--      account_id/to_account_id it names:
--      account_status_history.user_account_id, balance_snapshots.user_account_id,
--      holding_snapshots.user_account_id, holdings.user_account_id,
--      investment_transactions.user_account_id, savings_goals.user_account_id,
--      transactions.user_account_id, and transfer_suggestions.from_user_account_id /
--      transfer_suggestions.to_user_account_id.
--      ⚠⚠ ROUND 1 FIX: this list was WRONG in the first version of this migration. Grepping
--      information_schema.columns for column_name = 'user_account_id' misses TWO real referencing
--      columns (transfer_suggestions.from_user_account_id / to_user_account_id, different names)
--      and a SEPARATE FK-only enumeration misses TWO more (account_status_history.user_account_id
--      and holding_snapshots.user_account_id, which carry the column with NO foreign key
--      constraint at all). Neither method alone is complete; the TRUE surface is the UNION of
--      both, re-measured against information_schema for this fix:
--        SELECT table_name, column_name FROM information_schema.columns
--        WHERE column_name ILIKE '%user_account_id%'
--      UNION
--        SELECT tc.table_name, kcu.column_name FROM information_schema.table_constraints tc
--        JOIN information_schema.key_column_usage kcu USING (constraint_name, table_schema)
--        JOIN information_schema.constraint_column_usage ccu USING (constraint_name, table_schema)
--        WHERE tc.constraint_type = 'FOREIGN KEY' AND ccu.table_name = 'user_accounts';
--      That union is TEN TABLES, ELEVEN COLUMNS total: accounts.user_account_id (step 3, above),
--      the seven single-column tables named above (step 4), household_account_shares.
--      user_account_id (step 5, below), and transfer_suggestions' two columns
--      (from_user_account_id, to_user_account_id — step 4a, just below).
--      ⚠⚠ ROUND 4 FIX: this "ten tables, eleven columns" surface was STILL incomplete — a JSONB
--      reference (boosts.actions.transfer.from_user_account_id / to_user_account_id) is invisible
--      to BOTH information_schema queries above, since it lives inside a jsonb column's VALUE, not
--      as a column of its own. Re-derived by cross-checking migration 000100 (which already
--      rewrites this exact JSONB path on its own merge, proving it is a real, live reference —
--      see 000100's Step 3) and boost_execution_service.go's chain resolver (confirms it is read,
--      in preference to the raw physical id, at money-movement execution time). Repointed in
--      step 4b, just below. boosts.trigger_config.filters.user_account_id and
--      categorization_rules.conditions (type='account_id', value=a user_account_id) are ALSO
--      JSONB-embedded stable references this surface search cannot see, and are addressed by an
--      explicit, documented policy decision (leave unrepointed) rather than silently ignored —
--      see step 4b's own comment for the reasoning.
--   4a. transfer_suggestions is repointed the SAME self-correcting way as the plain-account_id
--      tables, using from_account_id/to_account_id — NOT invalidated or skipped. This is
--      deliberately NOT the same shape as task 2's ErrAmbiguousActiveAccount problem (resolving a
--      CHAIN key to ONE of several live physical accounts, which is genuinely ambiguous and where
--      task 2 made the boost path fall back to a pinned, ownership-verified id rather than guess).
--      Here the direction is the other way: from_account_id/to_account_id each already name ONE
--      specific physical account, and accounts.user_account_id is single-valued per account row —
--      there is exactly one correct chain for a suggestion's named account both before and after
--      the split, so re-deriving it is not a guess. A transfer suggestion whose from_account_id
--      IS NULL (the account was later removed; fk_transfer_suggestions_from_account_id is
--      ON DELETE SET NULL) is left untouched by this step, exactly like every other downstream
--      table already leaves an orphaned account_id-less row untouched — not a new gap this task
--      introduces.
--   5. Fan out EVERY household_account_shares grant — active AND closed — on a chain that
--      split, so a household member does not silently lose visibility of an account they could
--      already see, for any window that grant ever covered. (Round 4: closed grants are now
--      included too — see the ⚠⚠ ROUND 4 FIX note on step 5 itself. Fingerprint value is
--      superseded by step 6 below — see the ⚠⚠ ROUND 2/ROUND 3 notes.)
--      ⚠⚠ ROUND 2 FIX — A REAL MONEY BUG, CAUGHT BEFORE MERGE, NOT BY ME. The first version of
--      this step copied `physical_account_blind_idx` VERBATIM into the new share, reasoning
--      (WRONGLY) that every chain this migration splits is "by construction maskless" so the
--      column is always NULL anyway. That is false: mechanism (b) in the WHY section above (two
--      MASKED accounts sharing institution+mask) is a real, ordinary collision shape, and for it
--      `physical_account_blind_idx` is NON-NULL and — because it was computed from institution+
--      mask alone, the exact two columns that made the pre-split accounts collide — IDENTICAL
--      across every resulting sibling. `physical_account_blind_idx` is a claim that two
--      user_accounts rows name the SAME real physical account (see
--      HouseholdShareRepository.ListActiveUserAccountFingerprints and
--      analytics_repository.go's `COALESCE(physical_account_blind_idx, user_account_id::text)`
--      dedup fingerprint, e.g. lines ~608 and ~757). Copying it verbatim after a split makes that
--      claim FALSE: the split's whole point is that the siblings are now DIFFERENT real accounts.
--      The household CTEs would then treat the two new chains as duplicate copies of one joint
--      account and — combined with cloning `shared_at` verbatim too, which ties the
--      fingerprint-contested tiebreak — silently keep only ONE of them in every household read,
--      forever. That is exactly the $60 -> $0 class this branch exists to eliminate, reintroduced
--      by this migration's own fan-out.
--      ⚠⚠ ROUND 2's OWN FIX WAS ALSO INCOMPLETE, SUPERSEDED BY STEP 6 (ROUND 3). Round 2's first
--      attempt: give the fanned-out (new) chain a fingerprint suffixed with its own identity,
--      leave the surviving ORIGINAL row's share untouched (still plain). That keeps A's OWN two
--      split siblings from colliding with each other, but does not survive a SECOND user: probed
--      against real Postgres (two-user fixture — see Step 6's comment and the task report) and
--      confirmed to DOUBLE-COUNT a jointly-shared account whenever the co-owned sibling is the one
--      that happens to become the NEW (suffixed) chain, because the other owner's own,
--      never-touched fingerprint for the SAME real account stays in the OLD plain format and no
--      longer matches. This step's INSERT therefore no longer tries to compute the "right" value
--      at all — see Step 6, which normalizes every row's fingerprint (this newly-inserted one
--      included) in one pass, globally, after both this step and Step 2 have run.
--   6. Normalize EVERY household_account_shares.physical_account_blind_idx (the WHOLE table, not
--      just rows touched by this split) to include identity, so two independently-computed
--      fingerprints for the same real account always agree regardless of which side of a split
--      (if any) either owner's copy sits on. See the ⚠⚠ ROUND 3 FIX note on step 6 itself.
--
-- IDEMPOTENCY. Step 2's own WHERE clause is "a physical account disagrees with its
-- user_accounts row's identity" — once a chain is split, every remaining physical account under
-- the original row agrees with that row's identity_type/identity_subtype by construction, and
-- the new rows were minted with their own accounts' shared identity. So a second run of this
-- migration finds nothing left to split, and steps 3-5 likewise touch zero rows. Step 1 uses
-- IF NOT EXISTS / DROP INDEX IF EXISTS. Step 6 is idempotent by construction (see its own
-- comment) and runs unconditionally on every re-run regardless of whether anything else changed.
--
-- WHY NOT trg_user_accounts_identity_immutable. This migration only ever INSERTs new
-- user_accounts rows (with identity_type/identity_subtype set once at INSERT time, exactly like
-- application code does at mint) — it never UPDATEs identity_type/identity_subtype on an
-- existing row, so the trigger added in 000177 never fires here and needs no special handling.

-- =============================================================================
-- Step 1: Narrow the match key FIRST — see the header comment for why this must precede the
-- split. Two accounts at one institution (masked or maskless) can no longer collide onto one
-- user_accounts row when they disagree on identity_type or identity_subtype.
-- =============================================================================
DROP INDEX IF EXISTS unique_user_accounts_inst_mask_blind;
-- Also drop the NON-unique fallback index the down migration creates (round 4 fix, item 1) — a
-- down-then-up cycle must not leave it behind as stray, redundant index bloat.
DROP INDEX IF EXISTS idx_user_accounts_inst_mask_blind;

CREATE UNIQUE INDEX IF NOT EXISTS unique_user_accounts_inst_mask_identity_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
    NULLS NOT DISTINCT
    WHERE institution_id_blind_idx IS NOT NULL;

-- ⚠⚠ ROUND 2 FIX — a SECOND, independent constraint blocks a masked split, discovered probing
-- this fix against real Postgres. unique_user_account_identity (migration 000031) is a PLAIN
-- UNIQUE (user_id, institution_id, mask) predating encryption: institution_id/mask now store
-- CIPHERTEXT, and — per migration 000100's own header — AES-GCM's random nonce means two
-- INDEPENDENTLY ENCRYPTED rows for the same plaintext never produce equal ciphertext, so this
-- constraint has been practically inert for real inserts since blind indexes took over as the
-- actual dedup mechanism. Step 2 below is the one write path that is NOT an independent
-- encryption: it mints a new row by copying the SURVIVING row's institution_id/mask ciphertext
-- VERBATIM (a migration cannot re-encrypt — only the application holds the key), so for a MASKED
-- collision (mask NOT NULL) the new row's ciphertext is BYTE-IDENTICAL to the original's, which
-- this plain constraint (no NULLS NOT DISTINCT — that quirk only ever helped the maskless case)
-- rejects outright. Dropped here, not worked around, because it is superseded and no longer
-- guards anything the narrowed blind-index constraint above does not already guard correctly.
ALTER TABLE user_accounts DROP CONSTRAINT IF EXISTS unique_user_account_identity;

-- =============================================================================
-- Step 2: Identify split groups and mint new user_accounts rows.
-- =============================================================================
CREATE TEMP TABLE tmp_1569_t4_split_groups (
    old_ua_id uuid NOT NULL,
    g_type    varchar(50) NOT NULL,
    g_subtype varchar(50),
    new_ua_id uuid NOT NULL DEFAULT gen_random_uuid()
) ON COMMIT DROP;

INSERT INTO tmp_1569_t4_split_groups (old_ua_id, g_type, g_subtype)
SELECT DISTINCT
    a.user_account_id,
    a.type,
    a.subtype
FROM accounts a
JOIN user_accounts ua ON ua.id = a.user_account_id
WHERE a.user_account_id IS NOT NULL
  AND (a.type IS DISTINCT FROM ua.identity_type OR a.subtype IS DISTINCT FROM ua.identity_subtype);

-- The representative for each new row's PrimaryType/PrimarySubtype (the last-writer-wins pair,
-- distinct from IdentityType/IdentitySubtype) is the MOST RECENTLY created physical account in
-- that group — the best available proxy for "most recent link" once the original per-link sync
-- history cannot be recovered at split time. IdentityType/IdentitySubtype are the group's own
-- (g_type, g_subtype) — never the representative's derived value — because every account in the
-- group already agrees on type/subtype by construction of the grouping above.
WITH representative AS (
    SELECT DISTINCT ON (sg.old_ua_id, sg.g_type, sg.g_subtype)
        sg.new_ua_id,
        sg.g_type,
        sg.g_subtype,
        ua.user_id,
        ua.institution_id,
        ua.institution_id_blind_idx,
        ua.mask,
        ua.mask_blind_idx,
        ua.account_name_hint,
        ua.custom_color,
        ua.custom_icon,
        ua.custom_emoji,
        ua.display_order,
        ua.is_hidden,
        a.type    AS rep_type,
        a.subtype AS rep_subtype
    FROM tmp_1569_t4_split_groups sg
    JOIN user_accounts ua ON ua.id = sg.old_ua_id
    JOIN accounts a
        ON a.user_account_id = sg.old_ua_id
       AND a.type IS NOT DISTINCT FROM sg.g_type
       AND a.subtype IS NOT DISTINCT FROM sg.g_subtype
    ORDER BY sg.old_ua_id, sg.g_type, sg.g_subtype, a.created_at DESC, a.id DESC
)
INSERT INTO user_accounts
    (id, user_id, institution_id, institution_id_blind_idx, mask, mask_blind_idx,
     account_name_hint, custom_color, custom_icon, custom_emoji, display_order,
     is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
     created_at, updated_at)
SELECT
    r.new_ua_id, r.user_id, r.institution_id, r.institution_id_blind_idx, r.mask, r.mask_blind_idx,
    r.account_name_hint, r.custom_color, r.custom_icon, r.custom_emoji, r.display_order,
    r.is_hidden, r.rep_type, r.rep_subtype, r.g_type, r.g_subtype,
    now(), now()
FROM representative r;

-- =============================================================================
-- Step 3: Repoint the moved physical accounts to their new chain.
-- =============================================================================
UPDATE accounts a
SET user_account_id = sg.new_ua_id
FROM tmp_1569_t4_split_groups sg
WHERE a.user_account_id = sg.old_ua_id
  AND a.type IS NOT DISTINCT FROM sg.g_type
  AND a.subtype IS NOT DISTINCT FROM sg.g_subtype;

-- =============================================================================
-- Step 4: Repoint every other table carrying account_id + user_account_id, deriving from the
-- now-authoritative accounts.user_account_id (self-correcting: works whether the destination row
-- currently holds the old chain, is NULL, or is already correct).
-- ⚠ POLICY DECISION, DOCUMENTED (item 7): every one of these seven UPDATEs joins on
-- `dst.account_id = a.id`, so a retained-history row with a NULL account_id (balance_snapshots
-- and account_status_history both allow it — see balance_snapshot_repository.go's "THE THIRD
-- COALESCE ARM IS THE TRAP" comment for a documented, real example of an orphaned snapshot) is
-- NOT touched by this migration and CANNOT be, by any of these seven statements' own design: with
-- no physical account_id to anchor to, there is no way to know which split sibling such a row
-- "belongs" to — the physical anchor is genuinely irrecoverable, not merely unconsidered. Such a
-- row keeps whatever user_account_id it already had (the OLD, pre-split chain id, which still
-- names a real, valid row — the surviving original — so it does not point at nothing). This is
-- the SAME shape as, and no worse than, transfer_suggestions' documented from_account_id-IS-NULL
-- case below, and applies equally to every table in this step, not just one.
-- =============================================================================
UPDATE account_status_history dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE balance_snapshots dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE holding_snapshots dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE holdings dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE investment_transactions dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE savings_goals dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE transactions dst
SET user_account_id = a.user_account_id
FROM accounts a
WHERE dst.account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.user_account_id IS DISTINCT FROM a.user_account_id;

-- transfer_suggestions carries TWO user_account_id-shaped columns, each anchored to its OWN
-- physical account column (from_account_id / to_account_id) — repointed independently, exactly
-- like the tables above, per the ⚠⚠ ROUND 1 FIX note.
UPDATE transfer_suggestions dst
SET from_user_account_id = a.user_account_id
FROM accounts a
WHERE dst.from_account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.from_user_account_id IS DISTINCT FROM a.user_account_id;

UPDATE transfer_suggestions dst
SET to_user_account_id = a.user_account_id
FROM accounts a
WHERE dst.to_account_id = a.id
  AND a.user_account_id IS NOT NULL
  AND dst.to_user_account_id IS DISTINCT FROM a.user_account_id;

-- Step 4b:
-- ⚠⚠ ROUND 4 FIX (item 5) — boosts.actions.transfer JSONB carries the SAME shape as
-- transfer_suggestions: a stable chain reference (from_user_account_id / to_user_account_id,
-- Issue #1109, resolved at EXECUTION time by boost_execution_service.go's chain resolver in
-- PREFERENCE to the raw physical id) alongside the physical account_id it was minted from
-- (from_account_id / to_account_id). Migration 000100 already rewrote this exact JSONB path on a
-- loser->survivor merge, which is direct evidence it is a real, live, money-movement-relevant
-- reference — not a stale field. Left unrepointed, a transfer boost whose from_account_id names
-- the sibling that MOVED would still carry the OLD merged chain's from_user_account_id; at
-- execution the resolver would resolve that stale chain id to whatever is STILL live under it
-- (the account that stayed, not the one that moved) — a money-movement suggestion computed
-- against the WRONG source or destination account. Re-anchored the same self-correcting way as
-- transfer_suggestions, using the SAME physical from_account_id/to_account_id already stored
-- alongside each stable reference — this is not a guess, `accounts.user_account_id` is
-- single-valued per physical account both before and after the split.
UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,from_user_account_id}', to_jsonb(a.user_account_id::text))
FROM accounts a
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'from_account_id' = a.id::text
  AND a.user_account_id IS NOT NULL
  AND b.actions->'transfer'->'from_user_account_id' IS DISTINCT FROM to_jsonb(a.user_account_id::text);

UPDATE boosts b
SET actions = jsonb_set(b.actions, '{transfer,to_user_account_id}', to_jsonb(a.user_account_id::text))
FROM accounts a
WHERE b.boost_type = 'transfer_suggestion'
  AND b.actions->'transfer'->>'to_account_id' = a.id::text
  AND a.user_account_id IS NOT NULL
  AND b.actions->'transfer'->'to_user_account_id' IS DISTINCT FROM to_jsonb(a.user_account_id::text);

-- ⚠ POLICY DECISION, DOCUMENTED RATHER THAN LEFT SILENT: boosts.trigger_config.filters.
-- user_account_id (an event-trigger filter) and categorization_rules.conditions (type =
-- 'account_id', whose `value` is actually a user_account_id — see AccountIDEvaluator in
-- internal/rules/evaluator.go) are DELIBERATELY NOT repointed by this migration. Unlike the
-- transfer action above, neither stores a companion PHYSICAL account_id alongside the stable
-- chain reference: `filters` is an open `map[string]interface{}` with no guaranteed anchor key,
-- and a categorization_rules condition's `value` IS the user_account_id itself, with nothing else
-- to disambiguate which split sibling it meant. Guessing here would be exactly the kind of
-- unscoped resolution task 2's ErrAmbiguousActiveAccount refuses to do for a chain with several
-- live members — except worse, because there is no live-account signal to even attempt a guess
-- from. The untouched value still names a REAL, existing user_accounts row (the surviving
-- original) after the split, so it is not broken, only possibly narrower in scope than before:
-- a categorization rule or event filter that used to match transactions from BOTH merged
-- physical accounts now matches only the one that stayed on the original id. That is a
-- behavior change a user may need to notice and re-create a second rule for, not a silent
-- misdirection of money — the asymmetry that makes it acceptable to leave unfixed here, unlike
-- the transfer action's from/to accounts.

-- =============================================================================
-- Step 5: Fan out EVERY household_account_shares grant — ACTIVE AND CLOSED (revoked) — on a
-- split chain, so a household member does not silently lose visibility of an account they could
-- already see, for ANY time window the grant ever covered.
-- ⚠⚠ ROUND 4 FIX (item 3, a policy call). The previous version of this step fanned out ONLY
-- active (revoked_at IS NULL) grants, reasoning that a CLOSED grant's historical window "cannot
-- be reconstructed" once its members were merged. That reasoning does not survive scrutiny: a
-- closed grant names an interval [shared_at, revoked_at) during which the WHOLE merged chain —
-- BOTH physical accounts — was visible to the household. Fanning that SAME interval onto both
-- resulting chains is not a guess, it is the exact, faithful reconstruction: during that window,
-- a household member who could see the chain could see both accounts behind it, so both accounts'
-- history belongs in that window's household reads. Restricting the fan-out to active grants left
-- a real historical undercount: a grant active in January and revoked in February leaves a
-- January household read seeing only the sibling that happened to keep the surviving row, on a
-- branch whose entire principle is that retained history stays queryable and correct. Fanning out
-- every grant, not just the live one, is the fix, and it is symmetric with how the rest of this
-- migration already treats history: repointing balances/transactions/holdings/snapshots
-- unconditionally, regardless of whether the account is still active.
-- ON CONFLICT DO NOTHING guards the (now much rarer) case of an exact duplicate — a closed
-- grant has no unique constraint against duplicates at all (uq_household_account_shares_active
-- only covers revoked_at IS NULL), so this is belt-and-braces for the active-grant sub-case, not
-- load-bearing for the closed one.
-- The fingerprint value inserted here is a throwaway placeholder (copied verbatim) — Step 6,
-- immediately below, is the ONLY thing that decides every row's final physical_account_blind_idx,
-- for every household_account_shares row in the table, not just the ones this step inserts.
-- =============================================================================
INSERT INTO household_account_shares
    (household_id, user_account_id, physical_account_blind_idx, visibility, shared_at, revoked_at)
SELECT DISTINCT
    has.household_id, sg.new_ua_id, has.physical_account_blind_idx, has.visibility, has.shared_at,
    has.revoked_at
FROM household_account_shares has
JOIN tmp_1569_t4_split_groups sg ON sg.old_ua_id = has.user_account_id
ON CONFLICT DO NOTHING;

-- =============================================================================
-- Step 6: Normalize EVERY household_account_shares.physical_account_blind_idx (the whole table,
-- not just rows this migration's split touched) to a pure, deterministic function of its OWN
-- chain's CURRENT (institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype).
--
-- ⚠⚠ ROUND 3 FIX — a SECOND cross-user money bug, found by probing a two-user scenario the
-- coordinator described (not merely asserted): User A's own masked collision (accounts P and Q,
-- same institution+mask) splits into two chains. WHICHEVER of P/Q happens to match the
-- surviving/original row's identity (an accident of which physical account was created earliest
-- — see Step 2) keeps a PLAIN, unsuffixed fingerprint; the other gets a suffixed one (Step 5's
-- original design, round 2). That is sufficient to keep A's own two chains from colliding with
-- EACH OTHER, but is NOT sufficient across users: if User B jointly co-owns the SAME physical
-- account as the one that happened to become A's NEW (suffixed) chain, B's own, never-split
-- fingerprint for that account — always computed as plain institution+mask by
-- HouseholdShareRepository.ListActiveUserAccountFingerprints — no longer matches A's suffixed
-- one, so the two real copies of ONE joint account stop deduping and are DOUBLE-COUNTED in every
-- household total. Probed directly: rigged exactly this two-user fixture (A's P+Q masked
-- collision, engineered so P is the sibling that moves to the new chain, plus B's own genuine
-- joint share of P with a plain, pre-split-format fingerprint) and read the real
-- GetSnapshotsAsOfForHousehold production query — it returned DOUBLE the correct total,
-- confirming the coordinator's predicted failure mode exactly.
-- (Both directions were probed: A's Q chain — which kept the ORIGINAL row's plain fingerprint —
-- also collided with B's unrelated plain-fingerprint share for P, since plain format carries no
-- identity at all, so two DIFFERENT real accounts (A's Q and B's P) could tie on it too.)
--
-- THE FIX: stop trying to keep exactly one sibling "plain" and hope it is the one some other
-- user's share happens to need. Instead, ALWAYS include identity in the fingerprint, for every
-- share, every chain, everywhere — the same narrowing already applied to the user_accounts match
-- key itself (Step 1). Two independently-computed identity-aware fingerprints for the literal
-- same real account always agree (Plaid reports the same type/subtype to whichever user links
-- it), while A's own P and Q chains remain pairwise distinct because they disagree on identity by
-- construction of the split. This is why the fix must be GLOBAL, not scoped to split-affected
-- rows: for A's identity-aware P-fingerprint to match B's, B's PRE-EXISTING share must ALSO be
-- rewritten to the identity-aware format in this same migration — leaving B's row in the old
-- plain format would just move the mismatch, not close it. household_share_repository.go's
-- ListActiveUserAccountFingerprints is updated in the SAME change to compute this identity-aware
-- format for every future share, so no window exists where some shares use the old format and
-- others the new one.
--
-- IDEMPOTENT BY CONSTRUCTION: this recomputes every value from its chain's current authoritative
-- columns rather than mutating a stored value, so re-running it is a no-op (same inputs, same
-- output) rather than needing a "already migrated" guard.
-- =============================================================================
UPDATE household_account_shares has
SET physical_account_blind_idx = CASE
    WHEN ua.mask_blind_idx IS NULL THEN NULL
    ELSE ua.institution_id_blind_idx || ua.mask_blind_idx || ':' ||
         -- ⚠⚠ ROUND 4 FIX: COALESCE(x, '') is NOT injective — identity_subtype = NULL (no
         -- subtype at all) and identity_subtype = '' (an empty-but-present subtype, which Step 2's
         -- own grouping treats as a DIFFERENT identity via IS DISTINCT FROM) would COALESCE to the
         -- SAME '' and collide onto the SAME fingerprint, silently dropping one from household
         -- totals — the exact defect this migration exists to eliminate, reintroduced by an
         -- encoding bug rather than a logic bug. Tagging each field with a presence marker ('N' =
         -- SQL NULL, 'S' = a real, possibly-empty string) makes NULL and '' produce DIFFERENT
         -- encoded values no matter what. Applied to both columns for symmetry (identity_type is
         -- NOT NULL for any account-derived identity today, but an orphan row's identity_type can
         -- independently be NULL — see the WHERE clause in Step 1 — so the same injectivity
         -- concern applies there too, defensively).
         (CASE WHEN ua.identity_type IS NULL THEN 'N' ELSE 'S' || ua.identity_type END) || ':' ||
         (CASE WHEN ua.identity_subtype IS NULL THEN 'N' ELSE 'S' || ua.identity_subtype END)
END
FROM user_accounts ua
WHERE ua.id = has.user_account_id
  -- ⚠⚠ ROUND 4 FIX (item 6): skip rows already at their target value, so a re-run does not
  -- unconditionally rewrite every row in the table (and, via the shares table's own
  -- updated_at-bumping trigger, mutate every row's audit timestamp) when nothing actually needs
  -- to change. IS DISTINCT FROM handles the NULL-vs-NULL "unchanged" case correctly.
  AND has.physical_account_blind_idx IS DISTINCT FROM (CASE
        WHEN ua.mask_blind_idx IS NULL THEN NULL
        ELSE ua.institution_id_blind_idx || ua.mask_blind_idx || ':' ||
             (CASE WHEN ua.identity_type IS NULL THEN 'N' ELSE 'S' || ua.identity_type END) || ':' ||
             (CASE WHEN ua.identity_subtype IS NULL THEN 'N' ELSE 'S' || ua.identity_subtype END)
      END);

