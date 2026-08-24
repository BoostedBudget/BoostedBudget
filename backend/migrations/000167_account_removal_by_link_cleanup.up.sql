-- Issue #1569 Task 20 (A2): admit the THIRD cause of an account removal — this product removed the
-- Plaid link.
--
-- WHY A NEW VALUE RATHER THAN REUSING ONE OF THE TWO.
--
-- accounts.removed_reason (migration 000154) names WHO removed the account. It had two values, and
-- neither is true of an account whose Plaid Item was removed by one of this product's own
-- background flows:
--
--   'user_disconnect'  -- FALSE. The user never asked. The billing cleanup sweep selects an item
--                         merely for having been in 'login_required' longer than errorDays, or for
--                         not having synced in staleDays (repository.cleanupCandidateReasonPredicate);
--                         a subscription-cancellation disconnect is triggered by a Paddle webhook.
--   'provider_absent'  -- FALSE, and dangerously so. The provider did NOT drop these accounts; the
--                         bank still reports them. 'provider_absent' is the ONE value
--                         accountRepository.RestoreAccount accepts, so labelling these rows with it
--                         would make them auto-restorable the moment any later sync sees them —
--                         resurrecting accounts under a Plaid Item that no longer exists.
--
-- 'link_removed' states the fact and nothing more: the local Plaid link was removed by us, the
-- provider still has the accounts, and the user did not ask for this specific account to go. It is
-- deliberately NOT specialised further into "swept as stale" / "subscription cancelled" /
-- "erasure": WHY the link was removed is already recorded, per item, on linked_items
-- (status, cleanup_scheduled_at, plaid_removed_at, plaid_removal_failed_at) and in audit_logs.
-- Copying that distinction onto the account row would be the same false-cause mistake migration
-- 000153 removed from account_status_history.reason — a column that looks authoritative about an
-- intent it never observed.
--
-- RESTORE: 'link_removed' is not restorable, and gets that for free. RestoreAccount's predicate is
-- a plain equality against 'provider_absent' (account_repository.go), so every value other than
-- that one is refused without a code change. Pinned by
-- TestAccountRepository_RestoreAccount_RefusesLinkRemoved_Integration.
--
-- Idempotent: the CHECK is dropped and re-added unconditionally rather than guarded by a
-- pg_constraint EXISTS, because 000154's constraint of the SAME NAME already exists on every
-- migrated database and an EXISTS guard would therefore skip the widening entirely — the failure
-- mode that guard style is prone to when a migration EDITS a constraint instead of adding one.
-- DROP ... IF EXISTS followed by ADD is idempotent under re-application either way.

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_removed_reason_valid;

-- Adding this CHECK NOT VALID and then VALIDATE-ing would be the general rule, and it is what
-- 000154 did. It is unnecessary here and the two-step is skipped deliberately: the predicate is
-- strictly WIDER than the constraint dropped one statement above (same expression, one extra
-- admitted value), so every existing row already satisfies it and there is nothing for a
-- validation pass to find.
--
-- ⚠ CORRECTION (#1569 task 20, review round 1). This paragraph used to end "and the ACCESS
-- EXCLUSIVE lock is held for the same duration either way", which is FALSE. A validating
-- `ADD CONSTRAINT ... CHECK` holds ACCESS EXCLUSIVE for the whole table scan; `NOT VALID` takes it
-- only momentarily, and the later `VALIDATE CONSTRAINT` runs under SHARE UPDATE EXCLUSIVE, which
-- does not block reads or writes. The two-step is therefore strictly cheaper in lock terms on a
-- large table, and the sentence was inventing a justification the decision does not need.
--
-- The decision stands on the argument above it, unchanged: the widening is a superset, accounts is
-- small, and this migration already holds ACCESS EXCLUSIVE for the DROP one statement earlier.
ALTER TABLE accounts ADD CONSTRAINT accounts_removed_reason_valid
    CHECK (
        removed_reason IS NULL
        OR (status = 'removed' AND removed_reason IN ('user_disconnect', 'provider_absent', 'link_removed'))
    );

COMMENT ON COLUMN accounts.removed_reason IS
$COMMENT$Who removed this account, for rows with status = 'removed': 'user_disconnect' (the user asked, via DisconnectAccount/DisconnectInstitution), 'provider_absent' (Plaid stopped returning it from a confirmed-complete /accounts/get, i.e. the bank closed it), or 'link_removed' (one of this product's own flows removed the Plaid Item -- the orphaned-item cleanup sweep, a subscription-cancellation disconnect, or account erasure -- so the account can no longer sync even though the bank still reports it). NULL for every other status, and for 'removed' rows written before migration 000154. ONLY 'provider_absent' is eligible for automatic restore -- NULL and 'link_removed' are not, deliberately: a user disconnect must never be undone by a sync, and an account whose Plaid Item we destroyed has nothing left to sync against. The CAUSE lives here rather than in account_status_history.reason, which records the transition only (see migration 000153); WHY the link was removed lives on linked_items and in audit_logs, not here (see migration 000167).$COMMENT$;
