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
		if err := s.accountRepo.UpdateStatus(ctx, account.ID, account.UserID, domain.AccountStatusRemoved, nil); err != nil {
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
