-- #1569 Task 15 (T7): correct household_account_shares' comments to the semantics the code
-- now actually implements.
--
-- Nothing structural changes here. Migration 000110 already gave the table the right shape —
-- shared_at NOT NULL, nullable revoked_at, a partial unique index over open rows only — but
-- described it as a boolean grant that gets "withdrawn", and the reads matched that
-- description: `revoked_at IS NULL` answered "shared TODAY?" for every point-in-time query,
-- and SetShareVisibility overwrote `visibility` on the single open row. Between them a member
-- leaving, or merely hiding an account, retroactively erased the household's record of what
-- it had legitimately been shown.
--
-- A row is now the household's own audit of ONE state over ONE half-open interval
-- [shared_at, revoked_at): inclusive at shared_at, EXCLUSIVE at revoked_at, the same shape
-- account_status_history uses for [valid_from, valid_to). Closing an interval is the only
-- thing that ever happens to a row — the 30-day PurgeRemovedMembership hard delete is gone,
-- and GDPR erasure remains the only thing that removes one (ON DELETE CASCADE from
-- user_accounts).
--
-- COMMENT ON is idempotent by nature: it replaces whatever comment is there.

COMMENT ON TABLE household_account_shares IS
    'Household account-visibility grants, as an append-and-close INTERVAL log. Each row records '
    'ONE visibility state for ONE (household_id, user_account_id) over the half-open interval '
    '[shared_at, revoked_at) — inclusive at shared_at, EXCLUSIVE at revoked_at; revoked_at NULL '
    'means the state is still current. At most one OPEN row per (household_id, user_account_id) '
    '(uq_household_account_shares_active); closed rows are unconstrained and may overlap, so an '
    'as-of read must dedup to the interval in force (see householdAccountsCTEForBalancesAsOf). '
    'Keyed to user_accounts (survives Plaid relinks). owner_user_id is intentionally omitted — '
    'derived from user_accounts.user_id at query time. Rows are RETAINED past a member''s '
    'removal and past the 30-day membership purge: they are the household''s audit of what it '
    'was shown and when, not a copy of the departed member''s data, whose own financial rows are '
    'untouched either way. Erasure belongs to GDPR alone and reaches these rows by cascade from '
    'user_accounts. Issues #1365 (Phase 1) and #1569 (Task 15).';

COMMENT ON COLUMN household_account_shares.visibility IS
    'The state this INTERVAL records, not a mutable flag. '
    'shared: included in the household aggregate for [shared_at, revoked_at). '
    'hidden: excluded from the shared view for that same interval. '
    'Changing visibility CLOSES this row and opens a new one, so a past interval always keeps '
    'the visibility that was actually in force then (#1569 Task 15). Re-asserting the visibility '
    'a row already has is a no-op update on that row, never a new zero-length interval.';

COMMENT ON COLUMN household_account_shares.shared_at IS
    'Inclusive start of this interval (NOT NULL). An as-of read at exactly shared_at sees this row.';

COMMENT ON COLUMN household_account_shares.revoked_at IS
    'EXCLUSIVE end of this interval; NULL means still current. An as-of read at exactly '
    'revoked_at does NOT see this row — the boundary is [shared_at, revoked_at), matching '
    'account_status_history''s [valid_from, valid_to). Set either by RevokeSharesForUser '
    '(member removal, and the 30-day purge''s idempotent re-close) or by a visibility change '
    'closing this state to open the next. Constrained >= shared_at by '
    'chk_household_account_shares_revoked_at.';
