// Package services provides business logic and external service integrations.
// This file implements PlaidItemCleanupService for managing Plaid item removal.
// Issue #668: Plaid Item Management for Cost Optimization
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/plaid/plaid-go/v23/plaid"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ErrSideEffectClaimBusy is returned when another flow currently holds the OPERATION-keyed
// side-effect claim this call needed (repository.ActionPlaidDisconnectUser or
// repository.ActionPaddleCancelSubscription) — e.g. a subscription-cancellation webhook and the
// past-due grace-cancel cron both trying to disconnect the SAME user's Plaid items, or cancel
// the same Paddle subscription, at the same instant. Issue #1413 H1: the FOUR flow actions
// (plaid_disconnect_past_due, past_due_grace_cancel, subscription_cancelled_cleanup,
// account_deletion) are keyed by WORKFLOW, so they never exclude each other; the side-effect
// claim is keyed by the OPERATION itself, so whichever flow is already mid-operation for this
// user wins, and every other flow must back off rather than proceed as though the work had
// already happened.
//
// Callers MUST treat this exactly like a lost flow claim: release their OWN flow claim (so a
// later attempt can retry) and propagate this upward rather than swallowing it or proceeding as
// though the side effect were done.
var ErrSideEffectClaimBusy = errors.New("side-effect claim held by another flow")

// ErrCleanupAborted is returned by cleanupItems (via any of its three callers — RemoveAllUserItems,
// DisconnectAllUserItems, DisconnectAllUserItemsIfEligible) whenever the pass finishes without
// attempting every live linked item for the user, for either of TWO distinct reasons:
//
//  1. The per-item eligibility recheck (DisconnectAllUserItemsIfEligible only) reports the user is
//     no longer eligible partway through the item loop — e.g. an admin granted a comp mid-run.
//     Issue #1413 round-14 H1 (Codex review): before this sentinel existed, that outcome returned
//     nil, indistinguishable from "every item was attempted and none remained".
//     repairPlaidDisconnects (retention_cleanup_check.go) treated any nil return as full completion
//     and called ClearPlaidCleanupPendingIfMatches — and round-13 (36455e78) had already stopped a
//     comp grant from touching the marker, so the CAS guard on that clear still matched and the
//     clear SUCCEEDED, permanently hiding a user with live linked items from the only code path
//     that ever disconnects them.
//  2. A linked item was created (by a path outside this call's side-effect claim — LinkAccounts is
//     the only production inserter, see account_service.go) AFTER cleanupItems's initial fetch, so
//     the loop never even saw it. Issue #1413 H1 residue (Codex review, PR #1443 follow-up): reason
//     1's fix computed abortedCount only over the originally-fetched slice, so a pass that
//     successfully attempted (or correctly skipped) every item it DID see still returned nil —
//     verifyNoNewLinkedItemAppeared (below) is what catches this reason.
//
// This is neither success nor failure — like ErrSideEffectClaimBusy, it is a correct, EXPECTED
// outcome (more work exists than this pass attempted, for a legitimate reason), not a Plaid error:
// callers must not log it at Warn or count it against a failure metric, for either reason. Some
// items may already have been disconnected (and are durably marked so via MarkItemsDisconnected)
// before the abort; others were deliberately never attempted. errors.Is-checkable so a caller can
// tell this apart from both a clean completion (nil) and a genuine per-item failure (the
// errors.Join-wrapped error cleanupItems returns when itemErrs is non-empty) — and, critically, so
// it knows NOT to treat this as "cleanup finished": for repairPlaidDisconnects specifically, the
// marker must be LEFT SET so the user is re-considered on a future run.
//
// Callers do not need to (and cannot cheaply) distinguish WHICH of the two reasons produced this
// sentinel — the correct action is identical either way: don't treat it as done, retry later.
//
// Never returned when the recheck itself errors (eligibility could not be determined at all), nor
// when verifyNoNewLinkedItemAppeared's own re-fetch fails (a genuine infrastructure error, not "new
// item found") — both of those branches are fail-closed and already surface as a genuine, non-nil
// error, which callers must treat as a real failure (Warn-logged, counted), not this benign
// sentinel.
var ErrCleanupAborted = errors.New("plaid item cleanup aborted before completion: some linked item(s) were not attempted")

// plaidDisconnectSideEffectLease is the lease requested for repository.ActionPlaidDisconnectUser.
// Shorter than the 15-minute FLOW-claim leases (accountDeletionClaimLease, pastDueClaimLease,
// subscriptionCancelledCleanupClaimLease): this claim only needs to outlive the Plaid calls in
// cleanupItems's loop, not any surrounding DB/CAS work the calling flow does before or after.
const plaidDisconnectSideEffectLease = 10 * time.Minute

// claimSideEffectAndRun claims the OPERATION-keyed side-effect action, runs fn, and ALWAYS
// releases the claim afterward — on success AND on failure — rather than completing it.
//
// Unlike the six FLOW actions (each conceptually "once ever" per user — a deletion happens once,
// a retention notice is sent once per retention cycle), a Plaid disconnect or a Paddle cancel can
// legitimately recur across a user's lifetime (reconnect, then a LATER dunning cycle disconnects
// again). Calling Complete here would permanently disable the operation for that user after its
// first success, because work_claims never re-grants a completed claim. Release always leaves the
// claim immediately reclaimable for the next legitimate need instead.
//
// Returns ErrSideEffectClaimBusy, without calling fn, when another flow currently holds the
// claim. Issue #1413 H1.
func claimSideEffectAndRun(ctx context.Context, claims repository.WorkClaimsRepository, action, userID string, lease time.Duration, fn func() error) error {
	if claims == nil {
		return fmt.Errorf("claimSideEffectAndRun(%s): work claims repository not configured", action)
	}
	token, claimed, err := claims.Claim(ctx, repository.ResourceTypeUser, userID, action, lease)
	if err != nil {
		return fmt.Errorf("failed to claim %s for user %s: %w", action, userID, err)
	}
	if !claimed {
		slog.Default().Info("side-effect claim held by another flow; backing off",
			"user_id", userID, "action", action)
		return ErrSideEffectClaimBusy
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, releaseErr := claims.Release(rctx, repository.ResourceTypeUser, userID, action, token, "side-effect attempt finished"); releaseErr != nil {
			slog.Default().Warn("failed to release side-effect claim", "user_id", userID, "action", action, "error", releaseErr)
		}
	}()
	return fn()
}

// isPlaidItemAlreadyGone reports whether err represents Plaid's ITEM_NOT_FOUND error — returned
// when calling an operation (including /item/remove) on an Item that no longer exists: it was
// previously removed via /item/remove, the user depermissioned/deleted the Item at their
// institution, or Plaid support deleted it following a data-deletion request. Verified against
// Plaid's public error reference (https://plaid.com/docs/errors/item/#item_not_found) — the
// error_code string is not a typed constant in plaid-go v23 (unlike Paddle's typed error vars),
// so it is compared as a literal here, the same way domain.ClassifyPlaidError already does for
// other Plaid error codes (internal/domain/transaction_sync.go).
//
// Treating this as "already gone" (success) is what makes a retried /item/remove call safe —
// Issue #1413 H2, the single highest-value change in this fix: without it, a retry after a
// reclaimed lease would re-attempt an item Plaid has already forgotten and surface a spurious
// failure instead of the no-op it actually is.
func isPlaidItemAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	var apiErr plaid.GenericOpenAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	plaidErr, convErr := plaid.ToPlaidError(apiErr)
	if convErr != nil {
		return false
	}
	return plaidErr.ErrorCode == "ITEM_NOT_FOUND"
}

// PlaidItemCleanupService handles removal of Plaid items to minimize billing costs.
// It calls Plaid's /item/remove endpoint to invalidate access tokens and stop
// monthly subscription billing for Items.
//
// This service is used for:
// - User deletion: Remove all Plaid items before deleting user data
// - Orphaned item cleanup: Remove items in error state or not synced for extended periods
// - Subscription downgrade: Remove excess items when user loses premium access
// - Trial/subscription expiration: Disconnect items while preserving transaction history
//
// Issue #668: Plaid Item Management for Cost Optimization
// Issue #679, #680: Trial expiration
type PlaidItemCleanupService interface {
	// RemoveAllUserItems calls Plaid /item/remove for all of a user's linked items.
	// This should be called BEFORE deleting user data from the database to stop Plaid billing.
	//
	// Every item is attempted even after an earlier one fails — a single bad item must not
	// strand the rest of the user's items still-linked and still-billing. But this is NOT
	// best-effort: genuine per-item failures are aggregated and returned as a non-nil error.
	//
	// Issue #1413 finding H6: this function previously always returned nil ("graceful
	// degradation... deletion should proceed regardless"), which made its only production
	// caller's abort-on-failure logic (DeletionService.ExecuteDeletion) dead code — the
	// deletion cascade would proceed and clear the local Plaid item/token references even
	// after a failed /item/remove call, orphaning the still-active (still-billing) Plaid
	// Item with nothing left able to retry it. That caller needs strict semantics, and today
	// it is the ONLY caller, so the contract changed for it directly rather than adding a
	// parallel strict variant nobody else would use. If a FUTURE caller legitimately wants
	// best-effort semantics, make that an explicit, separate choice at that call site (e.g.
	// a dedicated method or an explicit parameter) — do not revert this function to always
	// returning nil, which would silently reopen H6.
	//
	// Returns a non-nil error if the initial repository query fails, or if one or more items
	// failed to be removed from Plaid (wrapping each underlying error via errors.Join). Issue
	// #1413 H1 residue (Codex review, PR #1443 follow-up): also returns ErrCleanupAborted
	// (errors.Is-checkable, not a Plaid error) if a NEW linked item was created for this user
	// after this call's initial fetch — see ErrCleanupAborted's doc comment.
	RemoveAllUserItems(ctx context.Context, userID string) error

	// RemoveItem calls Plaid /item/remove for a single linked item.
	// Used for individual item cleanup (orphaned items, subscription downgrade).
	//
	// Returns error if:
	// - Item is not found in the database
	// - Access token decryption fails
	//
	// Plaid API errors are logged but not returned (graceful degradation).
	// This is because the access token may already be invalid.
	RemoveItem(ctx context.Context, linkedItemID string) error

	// DisconnectAllUserItems calls Plaid /item/remove for all items, then marks
	// them as 'disconnected' in the database. This preserves transaction history
	// while stopping Plaid billing.
	//
	// Issue #679, #680: Used when trial/subscription expires to:
	// 1. Stop Plaid billing by calling /item/remove
	// 2. Preserve accounts and transactions by NOT deleting from database
	// 3. Mark items as 'disconnected' status
	//
	// Returns error only if the initial repository query fails.
	// Plaid API errors are logged but processing continues.
	// Issue #1413 H1 residue (Codex review, PR #1443 follow-up): also returns ErrCleanupAborted
	// (errors.Is-checkable, not a Plaid error) if a NEW linked item was created for this user
	// after this call's initial fetch — see ErrCleanupAborted's doc comment.
	DisconnectAllUserItems(ctx context.Context, userID string) error

	// DisconnectAllUserItemsIfEligible behaves exactly like DisconnectAllUserItems, except recheck
	// is invoked immediately before EACH item's removal, not once for the whole per-user call —
	// Issue #1413 round-7. DisconnectAllUserItems can make one Plaid call per linked item; a
	// caller that only re-verifies eligibility once, before the whole call, leaves every item
	// after the first exposed to becoming stale mid-loop (e.g. the user resubscribes between item
	// 1 and item 2). The retention-cleanup Plaid-disconnect repair pass
	// (RetentionCleanupCheckWorker.repairPlaidDisconnects) is this method's motivating caller: its
	// grace window makes a mid-loop resubscribe unlikely, but the recheck is cheap and bounds the
	// residual exposure to at most one item rather than all of them.
	//
	// recheck may be nil, in which case this behaves exactly like DisconnectAllUserItems (no
	// per-item check) — every other caller (deletion, past-due dunning, the
	// subscription-cancellation webhook, RemoveAllUserItems) is unaffected by this method's
	// existence.
	//
	// The first time recheck reports ineligible (false, nil), the loop stops immediately: no
	// further items are attempted, and this is a legitimate partial completion — but, as of Issue
	// #1413 round-14 H1 (Codex review), it is NOT reported as nil either: it returns
	// ErrCleanupAborted (errors.Is-checkable), so a caller cannot mistake "stopped early, work
	// remains" for "finished all items" and treat outstanding cleanup as done (e.g. clearing a
	// pending-cleanup marker while items are still live). If recheck itself fails (a non-nil
	// error — eligibility could not be determined at all), the loop also stops immediately,
	// fail-closed, and that error IS surfaced (distinct from, and always taking priority over,
	// ErrCleanupAborted — see cleanupItems's doc comment). Either way, items already removed
	// before the abort stay removed and durably marked.
	//
	// Separately (Issue #1413 H1 residue, Codex review, PR #1443 follow-up): even when every
	// originally-fetched item is attempted (no recheck-triggered abort) and none fail, this method
	// still returns ErrCleanupAborted — never nil — if a NEW linked item was created for this user
	// after the initial fetch (e.g. by LinkAccounts, which does not participate in this call's
	// side-effect claim). See ErrCleanupAborted's doc comment and
	// plaidItemCleanupService.verifyNoNewLinkedItemAppeared for the full contract. This applies to
	// all three methods on this interface, not just this one — RemoveAllUserItems and
	// DisconnectAllUserItems share the same cleanupItems implementation.
	DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error

	// SetWorkClaimsRepo wires the durable work-claims mechanism RemoveAllUserItems and
	// DisconnectAllUserItems require for EXCLUSIVITY on repository.ActionPlaidDisconnectUser
	// before any Plaid /item/remove call (Issue #1413 Codex round-3 finding H1). Mirrors
	// DeletionService/PastDueService/SubscriptionWebhookWorker's SetWorkClaimsRepo — like theirs,
	// this is NOT an optional capability: a nil claims repo makes both methods fail loudly rather
	// than silently disconnecting Plaid items with no cross-flow exclusivity at all.
	//
	// This single claim protects every caller of this shared service instance — deletion,
	// past-due dunning (both windows), the subscription-cancellation webhook, and trial
	// expiration all share ONE PlaidItemCleanupService instance in production (see main.go), so
	// wiring it here closes the H1 race for all of them at once rather than requiring each
	// caller to duplicate the claim/release ceremony around its own DisconnectAllUserItems call.
	SetWorkClaimsRepo(repo repository.WorkClaimsRepository)
}

// linkedItemRepoForCleanup is a minimal interface for the repository operations
// needed by PlaidItemCleanupService. This avoids importing the full repository package.
type linkedItemRepoForCleanup interface {
	GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)
	GetByID(ctx context.Context, id string) (*domain.LinkedItem, error)
	GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)
	MarkAllUserItemsDisconnected(ctx context.Context, userID string) error
	// MarkItemsDisconnected persists per-item cleanup outcome (Issue #1413 H2) — see
	// repository.LinkedItemRepository's doc comment for why this differs from
	// MarkAllUserItemsDisconnected.
	MarkItemsDisconnected(ctx context.Context, ids []string) error
}

// plaidServiceForCleanup is a minimal interface for the Plaid operations
// needed by PlaidItemCleanupService.
type plaidServiceForCleanup interface {
	RemoveItem(ctx context.Context, accessToken string) error
}

// plaidItemCleanupService implements PlaidItemCleanupService
type plaidItemCleanupService struct {
	linkedItemRepo linkedItemRepoForCleanup
	plaidService   plaidServiceForCleanup
	claimsRepo     repository.WorkClaimsRepository // Issue #1413 H1: required, see SetWorkClaimsRepo
	logger         *slog.Logger
}

// NewPlaidItemCleanupService creates a new PlaidItemCleanupService.
// Parameters:
//   - linkedItemRepo: Repository for querying linked items
//   - plaidService: Service for calling Plaid API
//
// Returns the cleanup service ready to use.
func NewPlaidItemCleanupService(
	linkedItemRepo linkedItemRepoForCleanup,
	plaidService plaidServiceForCleanup,
) PlaidItemCleanupService {
	return &plaidItemCleanupService{
		linkedItemRepo: linkedItemRepo,
		plaidService:   plaidService,
		logger:         slog.Default(),
	}
}

// SetWorkClaimsRepo wires the durable work-claims mechanism this service requires. See the
// PlaidItemCleanupService interface doc for why this is not treated as an optional capability.
func (s *plaidItemCleanupService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	s.claimsRepo = repo
}

// RemoveAllUserItems implements PlaidItemCleanupService.RemoveAllUserItems
//
// Issue #1413 round-4 Critical finding C2: the item list is read INSIDE cleanupItems's claimed
// closure — see that function's doc comment for why reading it here, before the claim, would
// reopen the race a concurrent LinkAccounts call could exploit.
func (s *plaidItemCleanupService) RemoveAllUserItems(ctx context.Context, userID string) error {
	return s.cleanupItems(ctx, userID, s.linkedItemRepo.GetByUserID, nil)
}

// RemoveItem implements PlaidItemCleanupService.RemoveItem
func (s *plaidItemCleanupService) RemoveItem(ctx context.Context, linkedItemID string) error {
	// Get the linked item
	item, err := s.linkedItemRepo.GetByID(ctx, linkedItemID)
	if err != nil {
		return fmt.Errorf("failed to get linked item %s: %w", linkedItemID, err)
	}

	// Remove the item (logs Plaid errors but doesn't return them)
	if err := s.removeItemInternal(ctx, item); err != nil {
		s.logger.Warn("failed to remove Plaid item",
			"item_id", linkedItemID,
			"plaid_item_id", item.ItemID,
			"error", err,
		)
		// Don't return Plaid errors - graceful degradation
	}

	return nil
}

// removeItemInternal handles the actual Plaid /item/remove call for a single item.
// Returns error if Plaid API fails.
func (s *plaidItemCleanupService) removeItemInternal(ctx context.Context, item *domain.LinkedItem) error {
	// Call Plaid /item/remove using the plaintext access token populated by the repo layer
	if err := s.plaidService.RemoveItem(ctx, item.AccessToken); err != nil {
		return fmt.Errorf("plaid RemoveItem failed for item %s: %w", item.ID, err)
	}

	s.logger.Info("successfully removed Plaid item",
		"item_id", item.ID,
		"plaid_item_id", item.ItemID,
	)

	return nil
}

// IsItemNotFoundError checks if an error indicates an item was not found.
// Used by cleanup workers to determine if cleanup can be skipped (item already removed).
func IsItemNotFoundError(err error) bool {
	return errors.Is(err, repository.ErrLinkedItemNotFound)
}

// DisconnectAllUserItems implements PlaidItemCleanupService.DisconnectAllUserItems
// Issue #679, #680: Trial expiration - disconnect items while preserving history
//
// Issue #1413 round-4 Critical finding C2: the item list is read INSIDE cleanupItems's claimed
// closure — see that function's doc comment.
func (s *plaidItemCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	return s.cleanupItems(ctx, userID, s.linkedItemRepo.GetActiveItemsByUserID, nil)
}

// DisconnectAllUserItemsIfEligible implements PlaidItemCleanupService.DisconnectAllUserItemsIfEligible
// — see the interface doc comment for the full contract (Issue #1413 round-7).
func (s *plaidItemCleanupService) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	return s.cleanupItems(ctx, userID, s.linkedItemRepo.GetActiveItemsByUserID, recheck)
}

// fetchLinkedItemsFn matches linkedItemRepoForCleanup.GetByUserID / GetActiveItemsByUserID —
// the two ways cleanupItems's callers select which items are in scope.
type fetchLinkedItemsFn func(ctx context.Context, userID string) ([]domain.LinkedItem, error)

// cleanupItems is the shared per-item Plaid-removal loop used by RemoveAllUserItems,
// DisconnectAllUserItems, and DisconnectAllUserItemsIfEligible (Issue #1413 H1/H2/round-4 C2/
// round-7):
//
//   - Claims repository.ActionPlaidDisconnectUser (the OPERATION-keyed side-effect claim)
//     BEFORE any external call, so at most one flow is ever mid-disconnect for this user at a
//     time, regardless of which FLOW claim (plaid_disconnect_past_due, past_due_grace_cancel,
//     subscription_cancelled_cleanup, account_deletion) is driving it. Returns
//     ErrSideEffectClaimBusy, without calling Plaid at all, if another flow already holds it.
//   - Issue #1413 round-4 Critical finding C2: fetch is called ONLY after the claim is won,
//     INSIDE the claimed closure — not before. Reading the item list before the claim (the
//     previous shape) left a window where LinkAccounts could create a NEW item, after the read
//     but before the claim, that this pass would never learn about and so would never send to
//     Plaid for removal. Fetching fresh, immediately before the loop below, closes that specific
//     ordering hazard: any item that exists by the time the claim is won is included.
//   - Skips items already marked 'disconnected' by a PRIOR attempt (durable per-item state, H2)
//     without calling Plaid again for them — a retry after a partial failure only attempts the
//     items still outstanding.
//   - Issue #1413 round-7: if recheck is non-nil, it is invoked immediately before EVERY
//     remaining item (including the first) — not just once for the whole call. The first time it
//     reports ineligible (false, nil), the loop stops without attempting any further items; this
//     is a legitimate partial completion, not a failure — but (round-14 H1) it is also not silent
//     success: the function returns ErrCleanupAborted (see that sentinel's doc comment) rather
//     than nil, so a caller cannot mistake "stopped early, work remains" for "nothing left to do".
//     If recheck itself errors, the loop also stops (fail-closed), and that error IS surfaced to
//     the caller — via the same errors.Join aggregation as a genuine per-item failure, taking
//     priority over ErrCleanupAborted (see the return logic at the bottom of this function).
//     recheck is nil for RemoveAllUserItems/DisconnectAllUserItems, preserving their exact
//     pre-round-7 loop behavior — but (see the next bullet) this does NOT mean those two never
//     return ErrCleanupAborted at all; they can, for the separate reason below.
//   - Issue #1413 H1 residue (Codex review, PR #1443 follow-up): AFTER the loop (or the "no items"
//     branch) finishes with no genuine per-item failure and no recheck-triggered abort, this
//     function re-fetches ONE more time and checks for any item whose ID was not part of the
//     ORIGINAL fetch above — i.e. one created (by a path outside this claim, such as LinkAccounts)
//     after cleanupItems started. If one is found, ErrCleanupAborted is returned here too, even
//     though there was no recheck to trigger the round-7/round-14 abort path — so, unlike the
//     stale pre-this-fix assumption, RemoveAllUserItems and DisconnectAllUserItems (recheck == nil)
//     CAN return ErrCleanupAborted, via this check specifically. See ErrCleanupAborted's doc
//     comment and verifyNoNewLinkedItemAppeared (below) for the full contract.
//   - Issue #1413 round-14 (Codex review): a residual, IRREDUCIBLE TOCTOU remains between a
//     per-item recheck reporting eligible and the actual Plaid /item/remove call two lines later
//     — a comp could be granted in that exact window and this item would still be disconnected.
//     No DB mechanism can close a gap that spans an external API call, and this is accepted
//     deliberately, not overlooked: unlike a hard-deletion TOCTOU (irreversible), a Plaid
//     disconnect is RECOVERABLE — the user simply relinks the account. See the recheck block
//     itself, below, for the exact line this window spans.
//   - Treats Plaid's ITEM_NOT_FOUND response (the item was already removed, at Plaid or by an
//     earlier attempt of this very operation) as success, not a failure — H2's idempotency fix,
//     the single highest-value change in this task.
//   - Persists per-item success (including "already gone") via MarkItemsDisconnected BEFORE
//     returning, so the durable state survives even when other items in the same batch failed.
//   - Aggregates genuine per-item failures into a non-nil returned error — every item attempted
//     before an abort is still attempted regardless of an earlier failure (a single bad item must
//     not strand the rest still-linked and still-billing), but the caller (DeletionService.
//     ExecuteDeletion in particular) needs to know when to abort.
func (s *plaidItemCleanupService) cleanupItems(ctx context.Context, userID string, fetch fetchLinkedItemsFn, recheck func(ctx context.Context) (bool, error)) error {
	if s.claimsRepo == nil {
		return fmt.Errorf("plaidItemCleanupService: work claims repository not configured")
	}

	return claimSideEffectAndRun(ctx, s.claimsRepo, repository.ActionPlaidDisconnectUser, userID, plaidDisconnectSideEffectLease, func() error {
		items, err := fetch(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get linked items for user %s: %w", userID, err)
		}

		// Issue #1413 H1 residue (Codex review, PR #1443 follow-up): every item ID seen on THIS
		// fetch, so the post-loop re-verification below can tell a genuinely NEW item — created
		// after this fetch by a path that does not participate in this call's side-effect claim —
		// apart from one this pass already knew about and handled. See that check's doc comment
		// for the full contract.
		knownItemIDs := make(map[string]struct{}, len(items))
		for _, item := range items {
			knownItemIDs[item.ID] = struct{}{}
		}

		if len(items) == 0 {
			s.logger.Info("no linked items to clean up for user", "user_id", userID)
			return s.verifyNoNewLinkedItemAppeared(ctx, userID, fetch, knownItemIDs)
		}

		s.logger.Info("cleaning up Plaid items for user",
			"user_id", userID,
			"item_count", len(items),
		)

		var successIDs []string
		var itemErrs []error
		var successCount, failCount, skippedCount int

	itemLoop:
		for _, item := range items {
			if item.Status == domain.LinkedItemStatusDisconnected {
				// Already handled by a prior attempt (H2 per-item durable state) — do not call
				// Plaid again for it.
				skippedCount++
				continue
			}
			if recheck != nil {
				eligible, recheckErr := recheck(ctx)
				if recheckErr != nil {
					s.logger.Warn("aborting Plaid item cleanup — eligibility recheck failed",
						"user_id", userID, "item_id", item.ID, "error", recheckErr)
					itemErrs = append(itemErrs, fmt.Errorf("eligibility recheck failed before item %s: %w", item.ID, recheckErr))
					break itemLoop
				}
				if !eligible {
					s.logger.Info("aborting remaining Plaid item cleanup — no longer eligible",
						"user_id", userID, "item_id", item.ID)
					break itemLoop
				}
				// Issue #1413 round-14 (Codex review): irreducible TOCTOU — recheck just reported
				// eligible, but the actual Plaid call is still two lines away. A comp granted in
				// this exact window is not caught: this item is disconnected anyway. Accepted
				// deliberately (see cleanupItems's doc comment) because a Plaid disconnect is
				// RECOVERABLE (the user relinks), unlike the irreversible operations (hard
				// deletion) this branch's other TOCTOU-adjacent guards exist to protect. No
				// tightening is attempted here — closing a gap that spans an external API call
				// would require either blocking Plaid on a DB lock (not possible) or re-reading
				// AFTER the call too (which only moves the gap, it does not close it).
			}
			if err := s.removeItemInternal(ctx, &item); err != nil {
				if isPlaidItemAlreadyGone(err) {
					s.logger.Info("Plaid item already gone, treating as success",
						"item_id", item.ID, "plaid_item_id", item.ItemID, "user_id", userID)
					successIDs = append(successIDs, item.ID)
					successCount++
					continue
				}
				s.logger.Warn("failed to remove Plaid item",
					"item_id", item.ID,
					"plaid_item_id", item.ItemID,
					"user_id", userID,
					"error", err,
				)
				failCount++
				itemErrs = append(itemErrs, err)
				continue
			}
			successIDs = append(successIDs, item.ID)
			successCount++
		}

		if len(successIDs) > 0 {
			if markErr := s.linkedItemRepo.MarkItemsDisconnected(ctx, successIDs); markErr != nil {
				// Losing the per-item durable marker reopens H2's "duplicate call on retry" risk
				// for these items — surface it rather than silently dropping it (a subsequent
				// retry would just re-attempt them; ITEM_NOT_FOUND being mapped to success limits
				// the actual damage to a wasted API call, not a real duplicate side effect, but a
				// caller should still know this failed).
				itemErrs = append(itemErrs, fmt.Errorf("failed to persist per-item cleanup state for %d item(s): %w", len(successIDs), markErr))
			}
		}

		abortedCount := len(items) - successCount - failCount - skippedCount
		s.logger.Info("Plaid item cleanup pass completed",
			"user_id", userID,
			"success_count", successCount,
			"fail_count", failCount,
			"skipped_already_clean", skippedCount,
			"aborted_by_recheck", abortedCount,
		)

		if len(itemErrs) > 0 {
			// A genuine per-item failure (or a recheck error, which is appended to itemErrs above)
			// always takes priority over ErrCleanupAborted below — the caller needs to know a real
			// failure occurred, not just that the pass stopped early.
			return fmt.Errorf("failed to remove %d of %d Plaid item(s) for user %s: %w",
				failCount, len(items), userID, errors.Join(itemErrs...))
		}
		if abortedCount > 0 {
			// Issue #1413 round-14 H1 (Codex review): abortedCount > 0 with itemErrs empty can only
			// mean the `!eligible` branch above hit `break itemLoop` — the recheck-error branch
			// always appends to itemErrs, which would have already returned above. This is a
			// legitimate partial completion (some items may have succeeded and are durably marked
			// via MarkItemsDisconnected just above), NOT a failure — but it must not be reported as
			// nil either, or a caller (repairPlaidDisconnects) would treat outstanding cleanup as
			// fully done and clear a marker while items remain live. See ErrCleanupAborted's doc
			// comment for the full contract.
			return fmt.Errorf("%w for user %s (%d of %d item(s) not attempted)",
				ErrCleanupAborted, userID, abortedCount, len(items))
		}
		return s.verifyNoNewLinkedItemAppeared(ctx, userID, fetch, knownItemIDs)
	})
}

// verifyNoNewLinkedItemAppeared is cleanupItems's post-loop guard against the H1 residue (Issue
// #1413, Codex review, PR #1443 follow-up): round-14's ErrCleanupAborted fix closed the case where
// a mid-loop recheck aborts partway through the ORIGINALLY-fetched items slice, but abortedCount is
// still computed only over that same slice. An item created by a path that does not participate in
// cleanupItems's side-effect claim — LinkAccounts (account_service.go) is the only production
// inserter into linked_items, confirmed by an audit of every repository.LinkedItemRepository
// consumer — AFTER the fetch at the top of cleanupItems is invisible to everything above: the loop
// (or the "no items" branch) completes having never even seen it, itemErrs stays empty, and
// abortedCount stays 0. Returning nil at that point would tell the caller "fully done" while that
// item is still live and was never attempted.
//
// repairPlaidDisconnects (retention_cleanup_check.go) is the concrete consumer this protects: a nil
// return there clears plaid_cleanup_pending_at — the ONLY marker that makes a read-only user a
// repair candidate again — permanently hiding the new item from the one code path that ever
// disconnects it.
//
// Called ONLY from the two return paths that would otherwise report nil (the "no items" branch and
// the final return of the main path) — a genuine per-item failure or a recheck-triggered abort must
// still take priority and return before ever reaching this, per cleanupItems's existing precedence
// rule.
//
// Re-fetching (rather than diffing against something cheaper) is deliberate: every item from the
// ORIGINAL fetch has, by the time this runs, either been durably marked 'disconnected' (a fresh
// success) or already carried that status (skipped) — so any item this second fetch returns whose
// ID is NOT in knownItemIDs (the original fetch's ID set) must be one this pass never attempted.
//
// Reuses ErrCleanupAborted rather than a new sentinel: every existing caller already treats it
// generically as "not actually done, do not clear the marker, this is benign and expected" —
// repairPlaidDisconnects's errors.Is(disconnectErr, ErrCleanupAborted) branch does not distinguish
// WHY the loop didn't cover every item, and RemoveAllUserItems/DisconnectAllUserItems's callers
// (deletion, past-due dunning, the cancellation webhook) already treat any non-
// ErrSideEffectClaimBusy error as "abort this attempt, retry next run" — exactly the safe behavior
// for a newly-appeared live item this pass never touched. No caller-side change is required.
//
// A failure of THIS re-fetch itself (a genuine infrastructure error, not "new item found") is
// surfaced as a plain wrapped error — deliberately NOT ErrCleanupAborted — so it is NOT mistaken
// for the benign signal and is treated (and counted) as a real failure by every caller.
func (s *plaidItemCleanupService) verifyNoNewLinkedItemAppeared(ctx context.Context, userID string, fetch fetchLinkedItemsFn, knownItemIDs map[string]struct{}) error {
	freshItems, err := fetch(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to verify no new linked items appeared for user %s after cleanup: %w", userID, err)
	}
	for _, item := range freshItems {
		if _, known := knownItemIDs[item.ID]; known {
			continue
		}
		s.logger.Info("Plaid item cleanup: a new linked item appeared after the initial fetch and was never attempted — leaving cleanup marked incomplete",
			"user_id", userID, "item_id", item.ID)
		return fmt.Errorf("%w for user %s (a linked item was created after cleanup's initial fetch and was never attempted)",
			ErrCleanupAborted, userID)
	}
	return nil
}
