-- #1569 round 11 task 4: narrow the user_accounts match key to include the FROZEN identity
-- columns (identity_type, identity_subtype — migration 000177), and split any already-merged
-- chain whose physical accounts disagree on that identity.
--
-- WHY. Migration 000100's unique index (`unique_user_accounts_inst_mask_blind`) keys re-link
-- matching on (user_id, institution_id_blind_idx, mask_blind_idx) alone, with
-- NULLS NOT DISTINCT. Two maskless accounts at one institution are therefore FORCED onto one
-- user_accounts row even when they are different real-world accounts — the textbook case is a
-- brokerage and an IRA at one broker (both `type = 'investment'`, differing only in
-- `subtype`; there is no `type = 'brokerage'`, see 000177's header and
-- TestAccountTypeUniverseMatchesTheHoldingsGate). That merge is the root cause behind the
-- recorded $60 -> $0 household rewrite this round exists to fix: two real accounts sharing one
-- chain key means only one of them can ever be "the" live account for that chain.
--
-- WHAT THIS MIGRATION DOES, IN ORDER, IN ONE TRANSACTION:
--   1. Swap the match-key unique index FIRST, for one that also includes
--      identity_type/identity_subtype. This MUST happen before the split inserts below: the OLD
--      index (institution+mask only, NULLS NOT DISTINCT) is exactly what forced the merge in the
--      first place, so minting a second maskless row at the same institution while the old index
--      is still live fails with a duplicate-key error against the very index this migration
--      exists to replace. ✅ Probed against real Postgres — the first draft of this migration had
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
--   5. Fan out any ACTIVE (revoked_at IS NULL) household_account_shares grant on a chain that
--      split, so a household member does not silently lose visibility of an account they could
--      already see. The clone copies physical_account_blind_idx verbatim — that column is only
--      ever populated when the underlying account HAS a mask (see
--      HouseholdShareRepository.ListActiveUserAccountFingerprints), and every chain this
--      migration splits is, by construction, maskless (mask_blind_idx IS NULL — that is the only
--      way 000100's NULLS-NOT-DISTINCT index could have merged them in the first place), so the
--      cloned value is always NULL and the household_grants CTE's
--      `COALESCE(physical_account_blind_idx, user_account_id::text)` fallback keys the new share
--      on its own (fresh) user_account_id. No two live siblings can therefore ever collide onto
--      one dedup fingerprint by this migration's own action.
--
-- IDEMPOTENCY. Step 2's own WHERE clause is "a physical account disagrees with its
-- user_accounts row's identity" — once a chain is split, every remaining physical account under
-- the original row agrees with that row's identity_type/identity_subtype by construction, and
-- the new rows were minted with their own accounts' shared identity. So a second run of this
-- migration finds nothing left to split, and steps 3-5 likewise touch zero rows. Step 1 uses
-- IF NOT EXISTS / DROP INDEX IF EXISTS.
--
-- WHY NOT trg_user_accounts_identity_immutable. This migration only ever INSERTs new
-- user_accounts rows (with identity_type/identity_subtype set once at INSERT time, exactly like
-- application code does at mint) — it never UPDATEs identity_type/identity_subtype on an
-- existing row, so the trigger added in 000177 never fires here and needs no special handling.

-- =============================================================================
-- Step 1: Narrow the match key FIRST — see the header comment for why this must precede the
-- split. Two maskless accounts at one institution can no longer collide onto one user_accounts
-- row when they disagree on identity_type or identity_subtype.
-- =============================================================================
DROP INDEX IF EXISTS unique_user_accounts_inst_mask_blind;

CREATE UNIQUE INDEX IF NOT EXISTS unique_user_accounts_inst_mask_identity_blind
    ON user_accounts (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
    NULLS NOT DISTINCT
    WHERE institution_id_blind_idx IS NOT NULL;

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

-- =============================================================================
-- Step 5: Fan out active household_account_shares grants on a split chain, so a household
-- member does not silently lose visibility of an account they could already see. Only ACTIVE
-- (revoked_at IS NULL) grants are fanned out — there is no way to retroactively reconstruct which
-- physical account a CLOSED historical grant was "really" about once its members were merged, so
-- past household reads over a closed grant period are not rewritten by this migration.
-- ON CONFLICT DO NOTHING makes this step idempotent against the partial unique index
-- uq_household_account_shares_active (household_id, user_account_id) WHERE revoked_at IS NULL —
-- a re-run finds the split already applied (tmp_1569_t4_split_groups would be empty) so this is
-- belt-and-braces, not load-bearing.
-- =============================================================================
INSERT INTO household_account_shares
    (household_id, user_account_id, physical_account_blind_idx, visibility, shared_at)
SELECT DISTINCT
    has.household_id, sg.new_ua_id, has.physical_account_blind_idx, has.visibility, has.shared_at
FROM household_account_shares has
JOIN tmp_1569_t4_split_groups sg ON sg.old_ua_id = has.user_account_id
WHERE has.revoked_at IS NULL
ON CONFLICT DO NOTHING;

