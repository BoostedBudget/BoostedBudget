-- #1569 task 7.9 follow-up: 000149 and 000155 were edited IN PLACE, after they had already been
-- applied, to correct a false biconditional claim in their COMMENT ON COLUMN text ("status = 'X' <=>
-- X_at IS NOT NULL"). Editing an applied migration file only changes what a FRESH database gets --
-- every database that already ran 000149/000155 (local freshcheck, staging, production) still
-- carries the old, false comment in pg_description, because golang-migrate never re-runs an applied
-- version. This migration re-issues the CORRECTED comment text as a new forward step so already-
-- migrated databases converge on it too.
--
-- Text below is copied verbatim from the current (corrected) 000149/000155 up files -- do not
-- re-word it here independently of those files, or the two will drift apart again.
--
-- The actual, one-directional constraint shape (all three of accounts_replaced_at_consistency,
-- accounts_removed_at_consistency, accounts_expired_at_consistency):
--
--     status = 'X'  =>  X_at IS NOT NULL,  and nothing more.
--
-- The converse is deliberately NOT enforced: status holds one value at a time, so a biconditional on
-- all three timestamp columns would make them mutually exclusive -- a row could record at most one
-- lifecycle event ever -- and any path out of a terminal state (RestoreProviderRemovedAccount is
-- one) would be forced by the schema to erase the earlier timestamp. On an issue whose thesis is
-- that history is retained by default, mandating that erasure in DDL is the wrong thing to lock in.
--
-- Idempotent by construction: COMMENT ON COLUMN replaces any existing comment on that column outright
-- (there is no "already commented" state to guard against), so no IF NOT EXISTS / DO $$ wrapper is
-- needed here -- running this file twice, or a thousand times, converges on the same pg_description
-- row every time.

COMMENT ON COLUMN accounts.removed_at IS
$COMMENT$Timestamp when this account stopped being connected (status = 'removed'). NOT necessarily a user action: 'removed' has THREE causes and removed_reason (migration 000154) records which one -- 'user_disconnect' (the user asked, via DisconnectAccount or DisconnectInstitution), 'provider_absent' (a confirmed-complete Plaid /accounts/get stopped returning it, i.e. the bank closed it) or 'link_removed' (one of this product's own flows removed the account's Plaid Item). A NULL reason predates 000154 and is treated as a user disconnect. Never NULL while status = 'removed' -- accounts_removed_at_consistency requires it. Left NULL by every Go writer that moves the row to another status, but that direction is a code convention, NOT a constraint: the CHECK is one-directional (see 000155's Step 3 header). Distinct from replaced_at (re-link) and the TTL clock behind 'expired' (linked_items.status_changed_at).$COMMENT$;

COMMENT ON COLUMN accounts.expired_at IS
$COMMENT$Timestamp when the 90-day fingerprint TTL was released, moving this account to status =
'expired' (AccountRepository.ExpireAccountsByFingerprint). Never NULL while status = 'expired' --
accounts_expired_at_consistency requires it. Left NULL by every Go writer that moves the row to
another status, but that direction is a code convention, NOT a constraint: the CHECK is
one-directional on purpose (see the Step 3 header). Distinct
from removed_at (user disconnect / provider absence) and replaced_at (re-link). Pre-existing
'expired' rows (this status predates this column, migration 000062) were backfilled from
updated_at, the same proxy migration 000152 already relies on for this gap -- not a precise
timestamp of the release itself, but the best available one.$COMMENT$;
