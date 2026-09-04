package services

import (
	"context"
	"errors"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// auditActionAccountRemovedByProvider is the audit action for a Plaid-initiated (not user-initiated)
// account removal. Deliberately distinct from account_service.go's "account.removed", which means
// "the user asked us to disconnect this": the two answer different questions when a user later asks
// why an account vanished, and collapsing them would make the trail unable to tell "you removed it"
// apart from "your bank closed it."
const auditActionAccountRemovedByProvider = "account.removed_by_provider"

// reconcileRemovedAccounts walks internal accounts → Plaid's roster (the reverse of every other
// loop in this sync) and soft-removes the ones a bank has closed. Issue #1569, Task 5.
//
// The gap it closes: createBalanceSnapshots iterates PLAID's accounts and looks up internal ones,
// so an account Plaid has stopped returning is simply never visited. Soft-removal existed only on
// user-initiated disconnect (accountService.DisconnectAccount / DisconnectInstitution) — nothing
// handled Plaid dropping an account from a still-live item. The row stayed 'active' with whatever
// balance it last had, forever, inflating CURRENT net worth, and account_status_history never
// learned the account died (so the "as of <date>" history this branch builds silently degrades
// too). This is the mirror image of the bug #1569 was filed for, and worse: it corrupts the
// current number, not just the past ones.
//
// SAFETY. The only trigger is roster.ConfirmedAbsent, which is false for every account unless the
// /accounts/get call succeeded AND enumerated at least one account (see PlaidAccountRoster). A
// degraded, throttled or errored response therefore removes nothing — it cannot express
// "everything is gone", which is the one mistake here that would zero a user's whole portfolio.
//
// WRITE PATH. Reuses the existing soft-remove path (accountRepo.UpdateStatus → status='removed',
// removed_at stamped, history trigger records the interval) rather than inventing a third one. It
// cooperates with Task 2's status guard instead of fighting it: UpdateStatus refuses a non-active
// row, and both refusals are swallowed here — ErrAccountAlreadyRemoved (already exactly what we
// want) and ErrAccountAlreadyDisconnected ('replaced'/'expired' carry a MORE specific terminal
// reason that a provider sweep must not overwrite with the blanket 'removed').
//
// ALL-OR-NOTHING BOUND (fix round 1). A removal here is PERMANENT and INVISIBLE: nothing in this
// codebase ever writes an account back to 'active', there is no un-remove path or UI, and a
// wrongly-removed account silently leaves current net worth and stops receiving balance snapshots
// forever. So the one degenerate shape is refused outright: if EVERY active account under an item
// would be removed in a single run, none of them is. That case is not a plausible observation of
// reality. A bank closing every account at an institution shows up as an accounts array with
// nothing in it, which the completeness guard already refuses; getting here instead means Plaid
// returned a NON-EMPTY roster that matched none of our plaid_account_ids — i.e. the ids rotated or
// the response belongs to a different item, an anomaly, and this sync cannot create replacement
// rows to compensate. Single-account items are deliberately NOT exempted: for an item with one
// account, "complete roster, zero matches" is precisely the id-rotation shape rather than a
// closure, so exempting them would apply the bound exactly where it is most likely to be right.
// The cost of the bound is a genuine whole-institution closure staying stale until the user
// disconnects it — recoverable, unlike the alternative.
//
// Best-effort throughout: every failure is logged and the pass continues to the next account.
// A stale-but-live row is bad; abandoning the rest of the item's reconciliation is worse. Returns
// the number of accounts actually transitioned.
func (s *transactionSyncService) reconcileRemovedAccounts(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	roster PlaidAccountRoster,
	internalAccounts []domain.Account,
) int {
	if !roster.Complete() {
		// Logged once for the item, not once per account: this is a property of the response, and
		// the per-account questions below would all answer false anyway.
		observability.PlaidProviderAccountRemoval.WithLabelValues("skipped_incomplete_response").Inc()
		s.logger.Warn("skipping Plaid-initiated account reconciliation: accounts response is not trustworthy",
			"item_id", linkedItem.ID,
			"plaid_accounts_returned", roster.Len(),
		)
		return 0
	}

	// Pass 1: which of the item's active accounts is this response able to PROVE are gone?
	//
	// The ownership and blank-id filters come first, so an account this pass would never write to
	// is not counted as a removal candidate either — that keeps the all-or-nothing bound below
	// measuring what it claims to measure.
	eligible := make([]domain.Account, 0, len(internalAccounts))
	knownPlaidIDs := make([]string, 0, len(internalAccounts))
	for _, account := range internalAccounts {
		// Same ownership guard the liability and holdings syncs apply (#1537 F14): the accounts
		// come from GetByLinkedItemID, which is keyed on linked_item_id and is NOT user-scoped,
		// while the actor comes from linkedItem.UserID. A divergence between the two must never be
		// laundered into a status write against another user's row.
		if account.UserID != linkedItem.UserID {
			s.logger.Error("skipping an account that does not belong to the linked item's owner",
				"linked_item_id", linkedItem.ID,
				"account_id", account.ID,
				"account_user_id", account.UserID,
				"linked_item_user_id", linkedItem.UserID,
			)
			continue
		}
		if account.PlaidAccountID == "" {
			// No roster can speak to a row with no plaid_account_id; its absence proves nothing.
			continue
		}
		eligible = append(eligible, account)
		knownPlaidIDs = append(knownPlaidIDs, account.PlaidAccountID)
	}

	gone := roster.ConfirmedAbsentIn(knownPlaidIDs)
	if len(gone) == 0 {
		return 0
	}
	goneIDs := make(map[string]struct{}, len(gone))
	for _, id := range gone {
		goneIDs[id] = struct{}{}
	}

	candidates := make([]domain.Account, 0, len(gone))
	for _, account := range eligible {
		if _, ok := goneIDs[account.PlaidAccountID]; ok {
			candidates = append(candidates, account)
		}
	}

	// The all-or-nothing bound. See this function's doc comment for why there is no
	// single-account exemption.
	if len(candidates) == len(eligible) {
		observability.PlaidProviderAccountRemoval.WithLabelValues("refused_full_item_sweep").Add(float64(len(candidates)))
		s.logger.Error("refusing to remove EVERY active account under a linked item in one run: Plaid returned a complete roster that matched none of them, which is an anomaly rather than a mass closure",
			"item_id", linkedItem.ID,
			"active_accounts", len(internalAccounts),
			"eligible_accounts", len(eligible),
			"plaid_accounts_returned", roster.Len(),
		)
		return 0
	}

	// Pass 2: write.
	removed := 0
	for _, account := range candidates {
		// MarkRemovedByProvider, not UpdateStatus: same statement, same guards, but it stamps
		// removed_reason = 'provider_absent'. That column is the ONLY thing that later lets
		// restoreProviderRemovedAccounts tell this removal apart from a user disconnect, and a
		// user disconnect must never be auto-restored (Issue #1569 Task 5, fix round 2).
		if err := s.accountRepo.MarkRemovedByProvider(ctx, account.ID, account.UserID); err != nil {
			switch {
			case errors.Is(err, repository.ErrAccountAlreadyRemoved):
				// Already in the exact state this pass wants (a concurrent disconnect, or a
				// re-delivered webhook). Nothing happened, so nothing is counted or audit-logged.
				observability.PlaidProviderAccountRemoval.WithLabelValues("already_terminal").Inc()
				s.logger.Debug("account already removed, nothing to reconcile",
					"account_id", account.ID, "item_id", linkedItem.ID)
			case errors.Is(err, repository.ErrAccountAlreadyDisconnected):
				// 'replaced'/'expired' — a more specific terminal reason already recorded. Leave it.
				observability.PlaidProviderAccountRemoval.WithLabelValues("already_terminal").Inc()
				s.logger.Info("account already disconnected under a more specific status, leaving it unchanged",
					"account_id", account.ID, "item_id", linkedItem.ID)
			default:
				observability.PlaidProviderAccountRemoval.WithLabelValues("write_failed").Inc()
				s.logger.Warn("failed to mark Plaid-dropped account as removed (will retry next sync)",
					"account_id", account.ID, "item_id", linkedItem.ID, "error", err)
			}
			continue
		}

		removed++
		observability.PlaidProviderAccountRemoval.WithLabelValues("removed").Inc()
		s.logger.Info("marked account removed: Plaid no longer returns it for this item",
			"account_id", account.ID,
			"item_id", linkedItem.ID,
			"plaid_accounts_returned", roster.Len(),
		)

		// Audit-log the removal so a user asking "why did this disappear?" has an answer. The
		// existing user-initiated path does this too; the expiry path does not, which is precisely
		// the gap this repeats rather than copies. It is also the ONLY place the cause is recorded:
		// account_status_history's `reason` is derived from the status transition alone and so says
		// only 'removed' (migration 000153), deliberately claiming nothing about why.
		// Non-fatal: the account is already removed by this point, so an audit-write failure must
		// not undo or abort anything (same "already committed, don't abort" reasoning as
		// DisconnectAccount's audit call).
		if s.auditLogger != nil {
			if auditErr := s.auditLogger.Log(ctx, auditActionAccountRemovedByProvider, "account", account.ID); auditErr != nil {
				logAuditFailure(ctx, auditErr, auditActionAccountRemovedByProvider, account.UserID)
			}
		}
	}

	return removed
}

// auditActionAccountRestoredByProvider is the audit action for an automatic restore: an account
// this product had marked removed because Plaid stopped returning it has reappeared. Distinct from
// BOTH removal actions, so the trail reads as a sequence a human can follow —
// account.removed_by_provider then account.restored_by_provider — rather than a state that changed
// itself.
const auditActionAccountRestoredByProvider = "account.restored_by_provider"

// restoreProviderRemovedAccounts is the inverse of reconcileRemovedAccounts, and the reason that
// function's removals are no longer permanent. Issue #1569, Task 5, fix round 2.
//
// The hole it closes: nothing anywhere else in this codebase writes an account back to 'active'.
// There is no un-remove endpoint, no UI, and MatchOrCreate resolves user_accounts rather than
// creating account rows — so before this, a wrongly-removed account was out of current net worth
// forever, silently. A bank that reports an account again did not close it; the removal was wrong,
// or the closure was reversed. Either way the honest current state is 'active'.
//
// ⚠ THE ONE THING THIS MUST NEVER DO is resurrect an account the USER disconnected. The user asked
// for it gone; the bank continuing to report it is not a counter-argument. Two independent things
// enforce that, and the second one is the real guarantee:
//
//  1. this function only considers rows whose Account.RemovedReason is 'provider_absent'. A NULL
//     reason (every 'removed' row predating migration 000154) and a 'user_disconnect' one are both
//     skipped WITHOUT a repository call at all; and
//  2. accountRepo.RestoreProviderRemovedAccount re-checks the same predicate in SQL under FOR
//     UPDATE and refuses with ErrAccountNotRestorable otherwise.
//
// The repository's check is the AUTHORISATION — it is what makes the guarantee true even if this
// loop is bypassed or buggy, and it is the one the mutation tests target. The filter here is a
// second layer and an optimisation: without it, every account a user ever disconnected but still
// holds at the bank would take a SELECT ... FOR UPDATE round trip on every single sync, forever,
// to be told no every time.
//
// ⚠ The filter depends on Account.RemovedReason actually being populated, which exactly one read
// does (GetByLinkedItemIDIncludingRetained — the sync's own read, and this function's only caller's
// source). If a future caller passes accounts from a different query, RemovedReason is nil, this
// filter skips everything, and nothing is restored: the failure mode of the missing data is
// "restores stop happening", never "a user disconnect gets undone".
//
// Only ever runs on a confirmed-complete roster: an account "reappearing" in a partial or errored
// response proves nothing, exactly as its absence proves nothing on the removal side. There is
// deliberately no all-or-nothing bound here (the removal side has one) — restoring is the recovery
// direction, and a mass restore returns accounts to the state they were in before, which is at
// worst a re-removal on the next complete sync rather than an unrecoverable loss.
//
// ⚠ FLAPPING: DELIBERATELY NOT GUARDED. An account oscillating between present and absent would
// churn account_status_history intervals (two per cycle) and make net worth wobble. No guard is
// applied, and that is a judgement, not an oversight:
//
//   - Every guard that would stop the churn works by refusing to restore after N cycles — which
//     converts a churn problem into a PERMANENCE problem, the exact defect this function exists to
//     close, and it would strand the account in whichever phase the cap happened to fall on.
//   - The churn is bounded per run: at most one transition per account per sync, and each one is an
//     honest record of what the provider actually reported at that moment. A flapping history is a
//     true account of a flapping bank feed.
//   - It is already detectable: `removed` and `restored` climbing together on
//     plaid_provider_account_removal_total is precisely the flapping signature, and a persistent
//     one is a provider incident to escalate rather than something to paper over in this loop.
//
// If flapping is ever observed in practice, the right fix is upstream (a stability window on the
// REMOVAL side — require an account to be absent from N consecutive complete rosters before
// removing it), not a cap on recovery. That would be a change to reconcileRemovedAccounts, not
// here.
//
// Returns the number of accounts restored.
func (s *transactionSyncService) restoreProviderRemovedAccounts(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	roster PlaidAccountRoster,
	retainedAccounts []domain.Account,
) int {
	if !roster.Complete() {
		return 0
	}

	restored := 0
	for _, account := range retainedAccounts {
		if account.Status != domain.AccountStatusRemoved || account.PlaidAccountID == "" {
			continue
		}
		// Layer 1 of the two described above. nil (an unrecorded cause) is NOT provider-removed.
		if account.RemovedReason == nil || *account.RemovedReason != string(domain.RemovalReasonProviderAbsent) {
			observability.PlaidProviderAccountRemoval.WithLabelValues("restore_declined_not_provider").Inc()
			continue
		}
		// Ownership guard, same as the removal side: GetByLinkedItemIDIncludingRetained is keyed on
		// linked_item_id and is NOT user-scoped.
		if account.UserID != linkedItem.UserID {
			s.logger.Error("skipping restore of an account that does not belong to the linked item's owner",
				"linked_item_id", linkedItem.ID,
				"account_id", account.ID,
				"account_user_id", account.UserID,
				"linked_item_user_id", linkedItem.UserID,
			)
			continue
		}
		// Asked in the POSITIVE, deliberately. `Present` is the safe question — a false answer
		// means "Plaid did not mention this account", which is UNKNOWN — and here a false answer
		// leads to doing nothing at all. Writing it as `if !Present { continue }` would be the same
		// behaviour but the same SHAPE as the catastrophic loop the AST guard exists to catch, and
		// a reader scanning for that shape should not have to work out which of the two this is.
		if roster.Present(account.PlaidAccountID) && s.restoreReappearedAccount(ctx, linkedItem, account) {
			restored++
		}
	}

	return restored
}

// restoreReappearedAccount performs one restore and reports whether the account actually changed
// state. Split out of restoreProviderRemovedAccounts so that the decision loop above stays a short
// list of filters — see that function for the safety argument.
func (s *transactionSyncService) restoreReappearedAccount(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	account domain.Account,
) bool {
	if err := s.accountRepo.RestoreProviderRemovedAccount(ctx, account.ID, account.UserID); err != nil {
		if errors.Is(err, repository.ErrAccountNotRestorable) {
			// The routine answer, not a failure: a user-disconnected account (or a legacy row with
			// no recorded cause) that the bank still reports. Staying removed is correct.
			observability.PlaidProviderAccountRemoval.WithLabelValues("restore_declined_not_provider").Inc()
			s.logger.Debug("account reappeared but is not a provider-initiated removal, leaving it removed",
				"account_id", account.ID, "item_id", linkedItem.ID)
			return false
		}
		observability.PlaidProviderAccountRemoval.WithLabelValues("restore_failed").Inc()
		s.logger.Warn("failed to restore a provider-removed account that reappeared (will retry next sync)",
			"account_id", account.ID, "item_id", linkedItem.ID, "error", err)
		return false
	}

	observability.PlaidProviderAccountRemoval.WithLabelValues("restored").Inc()
	s.logger.Info("restored account: Plaid is returning it again after we marked it removed",
		"account_id", account.ID,
		"item_id", linkedItem.ID,
	)

	if s.auditLogger != nil {
		if auditErr := s.auditLogger.Log(ctx, auditActionAccountRestoredByProvider, "account", account.ID); auditErr != nil {
			logAuditFailure(ctx, auditErr, auditActionAccountRestoredByProvider, account.UserID)
		}
	}
	return true
}
