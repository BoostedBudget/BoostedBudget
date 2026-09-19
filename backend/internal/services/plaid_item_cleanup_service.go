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

// ErrItemNoLongerEligible is RemoveItem's answer when its use-time eligibility recheck reports that
// this single item has stopped being a cleanup candidate — the user repaired the connection through
// Plaid Link update mode, its accounts came back, or its error cleared — after the caller selected
// it and before the Plaid /item/remove.
//
// It is the single-item analogue of ErrCleanupAborted, kept SEPARATE from it because the two say
// different things: ErrCleanupAborted means "a per-user pass left work outstanding" (its own doc
// comment enumerates the two ways that happens, both about a multi-item loop), whereas this means
// "there is no work to do for this item, and there was none by the time we looked". Folding them
// together would make errors.Is checks on either one ambiguous.
//
// Like ErrCleanupAborted it is neither success nor failure but an EXPECTED outcome: callers must not
// log it at Warn or count it against a failure metric, and must NOT treat it as permission to record
// anything about the Plaid side — nothing was sent to Plaid. ItemCleanupWorker.Work, the only
// caller, handles it exactly as it handles a claim declined at selection time: release the stale
// cleanup schedule and report success, returning an error only if that release fails.
//
// Never returned when the recheck itself errors: an undetermined eligibility is fail-closed and
// surfaces as a genuine, retryable error, because "we could not tell" is not "still eligible".
var ErrItemNoLongerEligible = errors.New("linked item is no longer a cleanup candidate: nothing was sent to Plaid")

// plaidDisconnectSideEffectLease is the lease requested for repository.ActionPlaidDisconnectUser.
// Shorter than the 15-minute FLOW-claim leases (accountDeletionClaimLease, the deleted pastDueClaimLease,
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

// ErrPlaidRemovalUnrecoverable is returned by cleanupItems (via RemoveAllUserItems,
// DisconnectAllUserItems, DisconnectAllUserItemsIfEligible) and by RemoveItem when the ONLY thing
// that went wrong in the pass is one or more items whose Plaid /item/remove can NEVER succeed with
// the credential we hold — and, critically, only AFTER that verdict has been durably recorded on
// every affected row (LinkedItemRepository.MarkPlaidRemovalFailed). Issue #1494.
//
// IT DOES NOT MEAN THE ITEMS ARE GONE. Plaid told us our token is unusable, not that the Item ceased
// to exist; it may still be live and still billing. What the sentinel means is precisely: "no further
// automated attempt with this credential can change the outcome, and that fact is now on the row."
//
// WHY IT IS SEPARATE FROM A GENUINE FAILURE, and it is the one distinction this whole change turns
// on. A genuine failure (outage, rate limit, DB error) means retry later, and every caller already
// treats any non-nil error that way. This one means retrying will not help — so a caller that must
// eventually converge may proceed, PROVIDED proceeding preserves the obligation. Exactly one caller
// does that: DeletionService.ExecuteDeletion, which treats this sentinel as permission to continue to
// DeleteUserCascade, whose C2 guard independently re-checks that each row really is marked and whose
// transaction copies every not-provably-removed row into plaid_orphaned_items before deleting
// anything. Two independent conditions, so a bug in this sentinel's plumbing alone cannot erase over
// an unrecorded Item.
//
// ⚠ NEVER return this sentinel when the recording write failed. An unmarked row is indistinguishable
// from a transient failure to every downstream predicate — C2 will (correctly) still block — so a
// pass in that state must report a genuine failure and be retried, not hand out permission to
// proceed. Both call sites append the recording failure to itemErrs for exactly this reason, and
// Test_RemoveAllUserItems_TerminalFailureRecordingFails_DoesNotGrantPermissionToProceed pins it.
//
// Callers with no convergence obligation (past-due dunning, the cancellation webhook,
// repairPlaidDisconnects) need no change: they treat any non-nil error as "not done, try again next
// run", which stays correct — an unremovable item is legitimately never done, and the cleanup sweep
// and stranded-cleanup backstop go on re-attempting it for as long as the row exists.
var ErrPlaidRemovalUnrecoverable = errors.New("plaid item removal is unrecoverable with the credential on file (recorded on the item; the Item may still exist and still bill)")

// plaidRemovalNoUsableTokenCode is the LOCAL sentinel recorded in
// linked_items.plaid_removal_error_code when the failure was not Plaid rejecting our token but our
// having no token to send at all. Not a Plaid error code — deliberately spelled in a shape Plaid's
// vocabulary does not use, so nobody mistakes it for one when reading the column or the orphan record.
const plaidRemovalNoUsableTokenCode = "NO_USABLE_ACCESS_TOKEN"

// plaidRemovalInvalidAccessTokenCode is Plaid's error_code for a token that cannot be used, quoted as
// a literal for the same reason isPlaidItemAlreadyGone quotes ITEM_NOT_FOUND: plaid-go v23 exposes no
// typed constant for it.
const plaidRemovalInvalidAccessTokenCode = "INVALID_ACCESS_TOKEN"

// terminalPlaidRemovalFailure classifies a failed /item/remove attempt as UNRECOVERABLE (retrying
// with the credential on file cannot change the outcome) or, by default, recoverable. Issue #1494.
//
// THE DEFAULT IS RECOVERABLE, AND THAT IS THE SAFETY PROPERTY. Anything unrecognised keeps being
// retried and keeps blocking the user's erasure — fail-closed. Calling a recoverable error terminal
// is the expensive mistake: it strands a live, still-billing Plaid Item in plaid_orphaned_items
// labelled hopeless, and (because the row is then erasable) removes the automated retry that would
// have fixed it. So this set is as narrow as it can be while still closing #1494, and every member is
// named with its argument:
//
//   - Plaid INVALID_ACCESS_TOKEN. This is a verdict on the CREDENTIAL, not on Plaid's availability:
//     the token is malformed, was minted for a different Plaid environment, or has been rotated. The
//     access token stored on the row is the only one we will ever have for this Item, so every future
//     attempt sends the same rejected value and receives the same answer. Distinct from ITEM_NOT_FOUND
//     (isPlaidItemAlreadyGone) in the one way that matters: ITEM_NOT_FOUND is Plaid saying the ITEM is
//     gone, which licenses plaid_removed_at; this says nothing whatever about the Item, which is why
//     it licenses only plaid_removal_failed_at. #1494 asks explicitly that these not be conflated.
//   - No usable access token at all (item.AccessToken == ""). There is nothing to send. plaidService.
//     RemoveItem's own guard rejects this before any HTTP call ("access token cannot be empty"), which
//     is why this is checked as a PROPERTY OF THE ROW rather than by matching that error's text: the
//     row's stored ciphertext is empty, no code path can repopulate it (the plaintext token existed
//     only inside the original Plaid Link exchange), and so no retry can succeed. NOT the same as a
//     token that fails to DECRYPT: that error surfaces from the repository's fetch, before any removal
//     is attempted, and never reaches this function — a key/rotation problem is an operations problem
//     and must stay recoverable.
//
// DELIBERATELY ABSENT: everything else, and two in particular. INVALID_API_KEYS is an operator
// misconfiguration that a config fix cures — treating it as terminal would strand every one of a
// deployment's Items on a typo. Rate limits, 5xx, and network errors are the textbook transient cases
// this must never claim.
//
// err is assumed to have already failed isPlaidItemAlreadyGone (both call sites check that first), so
// ITEM_NOT_FOUND cannot reach here; item is the row the attempt was made for, and a nil item or nil
// err is classified recoverable so a mis-wired caller fails closed.
func terminalPlaidRemovalFailure(item *domain.LinkedItem, err error) (code string, terminal bool) {
	if err == nil || item == nil {
		return "", false
	}
	if item.AccessToken == "" {
		return plaidRemovalNoUsableTokenCode, true
	}
	var apiErr plaid.GenericOpenAPIError
	if !errors.As(err, &apiErr) {
		return "", false
	}
	plaidErr, convErr := plaid.ToPlaidError(apiErr)
	if convErr != nil {
		return "", false
	}
	if plaidErr.ErrorCode == plaidRemovalInvalidAccessTokenCode {
		return plaidRemovalInvalidAccessTokenCode, true
	}
	return "", false
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
	//
	// recheck IS REQUIRED, NOT OPTIONAL, and that is the whole point of it being in the signature.
	// This method used to pass a literal nil to cleanupItems' eligibility parameter — a parameter
	// that exists for exactly this purpose and that DisconnectAllUserItemsIfEligible has always
	// supplied — so no per-item recheck ever happened on the hard-deletion path. That path removes
	// items for a user about to be irreversibly erased, and its one caller
	// (DeletionService.ExecuteDeletion) makes one authorisation read and then one Plaid round-trip
	// PER ITEM; a user who cancelled their deletion after item 1 still lost items 2..N. A nil
	// recheck is REFUSED with an error rather than quietly meaning "no recheck", because the defect
	// this closes was precisely a nil that read as intentional. Callers with genuinely nothing to
	// recheck should not be using this method.
	//
	// When recheck reports ineligible partway through, ErrCleanupAborted is returned and the items
	// not yet attempted are left connected — a legitimate partial completion. The caller must
	// distinguish that from a Plaid failure; see ExecuteDeletion's ErrCleanupAborted branch.
	RemoveAllUserItems(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error

	// RemoveItem calls Plaid /item/remove for a single linked item.
	// Used for individual item cleanup (orphaned items, subscription downgrade).
	//
	// Returns a non-nil error if:
	//   - the item is not found in the database, or the lookup otherwise fails;
	//   - the Plaid /item/remove call fails for any reason OTHER than ITEM_NOT_FOUND;
	//   - the per-item 'disconnected' marker could not be persisted.
	//
	// Issue #1486 follow-up: Plaid errors are NOT swallowed. Until this fix they were, justified as "graceful
	// degradation... the access token may already be invalid" — a rationale that was half right and
	// half catastrophic, and the half that was right is now the justification for the ITEM_NOT_FOUND
	// branch rather than for a blanket swallow:
	//
	//   - Right: an access token that is unusable because Plaid has FORGOTTEN the Item is a genuine
	//     no-op. That case is now identified precisely — isPlaidItemAlreadyGone, matching Plaid's
	//     ITEM_NOT_FOUND — and reported as SUCCESS. It is also what makes RETRYING this method safe,
	//     and it MUST NOT be regressed into a returned error: doing so turns a self-healing case
	//     into a permanent GDPR block (see the failure chain in
	//     Test_RemoveItem_ItemNotFoundOnRetry_ConvergesInsteadOfStranding).
	//   - Catastrophic: every OTHER Plaid failure (outage, rate limit, network) leaves the Item LIVE
	//     AND STILL BILLING while telling the caller removal succeeded. The only caller,
	//     ItemCleanupWorker.Work, then hard-deleted the local linked_items row; DeleteUserCascade's
	//     round-4 C2 guard (repository.uncleanedLinkedItemExistsClause) is an EXISTS over
	//     linked_items, so with the row gone the guard passed and a GDPR deletion completed while a
	//     live, still-billing Plaid Item survived with no local record it ever existed — verbatim the
	//     outcome that guard's own comment says it exists to prevent.
	//
	// On success (INCLUDING the already-gone case) TWO writes happen BEFORE returning, in this order,
	// and both are propagated rather than best-effort:
	//
	//  1. MarkPlaidRemoved stamps linked_items.plaid_removed_at (Issue #1495). This is the write that
	//     records "Plaid billing is stopped" for EVERY row shape, including a 'replaced' one, and it is
	//     the ONLY fact the three downstream predicates read — DeleteUserCascade's C2 guard
	//     (uncleanedLinkedItemExistsClause), ItemCleanupCheckWorker's pending-cleanup backstop
	//     (GetItemsPendingCleanup) and the cleanup sweep (cleanupCandidateSelectQuery). Recording it on
	//     the ROW matters because the caller's row Delete is a deliberate no-op whenever accounts still
	//     reference the item, so the row frequently survives.
	//  2. MarkItemsDisconnected then moves status to 'disconnected'. ErrNoLinkedItemsUpdated from THIS
	//     write is an idempotent no-op, not a failure (the row was already 'disconnected', or is
	//     'replaced' and must keep that status) — see the implementation.
	//
	// The ORDER is deliberate and is argued in full at the call site: if the stamp fails, status must
	// still be live so a retry re-attempts the item and reaches the stamp again. The reverse order
	// could leave a 'disconnected' row carrying no proof of removal, which nothing would ever
	// re-attempt.
	//
	// This REPLACES a caveat that used to live here (fix round 2, item 1) about MarkItemsDisconnected
	// being a zero-row no-op for a 'replaced' row, leaving nowhere to record that Plaid was done and
	// making the backstop re-enqueue such a row until the CALLER's ClearCleanupSchedule landed. That
	// gap is closed at its cause: plaid_removed_at is writable on a 'replaced' row, so the row leaves
	// the backstop's result set as soon as removal is recorded.
	// recheck is the USE-TIME eligibility re-verification, and it is REQUIRED — a nil is refused
	// before the repository is touched, not treated as "no recheck wanted" (Codex pass-5 Medium 2).
	// It is asked immediately after this method's own read of the row and immediately before the
	// Plaid call, and it must answer the caller's own selection question: whatever made this item a
	// candidate must still hold.
	//
	// WHY IT CANNOT LIVE IN THE CALLER. ItemCleanupWorker.Work already re-verifies with
	// LinkedItemRepository.ClaimItemForCleanup, but that claim is a conditional UPDATE whose
	// guarantee ends with its own statement — it is a re-verification, not a lease, as its doc
	// comment states — and this method then performs a SEPARATE read before presenting the access
	// token to Plaid. A connection repair (Plaid Link update mode) or an active-account write can
	// commit in that gap, so an item that just became healthy was still disconnected. The caller
	// cannot close a window that opens inside the callee.
	//
	// (false, nil) returns ErrItemNoLongerEligible and NOTHING is written or sent: no Plaid call, no
	// plaid_removed_at stamp, no status change. A recheck ERROR is fail-closed and surfaces as a
	// genuine, retryable error without the sentinel. What remains after this is the same irreducible
	// check-to-call window every other flow in this file documents and accepts: no further database
	// read can eliminate the final external-call boundary, and a lease would only trade this
	// recoverable exposure for persistent state that can be orphaned.
	RemoveItem(ctx context.Context, linkedItemID string, recheck func(ctx context.Context) (bool, error)) error

	// DisconnectAllUserItemsIfEligible calls Plaid /item/remove for all of the user's ACTIVE items,
	// then marks them as 'disconnected' in the database — preserving transaction history while
	// stopping Plaid billing (Issue #679, #680) — invoking recheck immediately before EACH item's
	// removal rather than once for the whole per-user call (Issue #1413 round-7). A disconnect makes
	// one Plaid call per linked item; a caller that only re-verifies eligibility once, before the
	// whole call, leaves every item after the first exposed to becoming stale mid-loop (e.g. the user
	// resubscribes, pays their invoice, or is comped between item 1 and item 2).
	//
	// recheck IS REQUIRED, NOT OPTIONAL. A nil is REFUSED with an error before the repository is
	// touched, exactly as RemoveAllUserItems and RemoveItem refuse theirs. Until Issue #1500 this
	// method had a nil-recheck twin, DisconnectAllUserItems, and two of the five entry points into
	// this loop still used it; the twin is DELETED rather than deprecated, because the failure mode
	// being closed is SILENCE — while "no per-item recheck" is expressible, a future caller gets it
	// by DEFAULT and nothing complains. Every caller of this loop (deletion via RemoveAllUserItems,
	// both past-due windows, the subscription-cancellation webhook, the retention-cleanup
	// Plaid-disconnect repair pass) now re-asks its own selection predicate per item; a caller with
	// genuinely nothing to re-ask should not be disconnecting a user's banks.
	//
	// Returns a non-nil error if the initial repository query fails, or if one or more items failed
	// to be removed from Plaid (each underlying error wrapped via errors.Join).
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
	// RemoveAllUserItems too, not just this method — the two share the same cleanupItems
	// implementation.
	DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error

	// SetWorkClaimsRepo wires the durable work-claims mechanism RemoveAllUserItems and
	// DisconnectAllUserItemsIfEligible require for EXCLUSIVITY on repository.ActionPlaidDisconnectUser
	// before any Plaid /item/remove call (Issue #1413 Codex round-3 finding H1). Mirrors
	// DeletionService/the deleted PastDueService/SubscriptionWebhookWorker's SetWorkClaimsRepo — like theirs,
	// this is NOT an optional capability: a nil claims repo makes both methods fail loudly rather
	// than silently disconnecting Plaid items with no cross-flow exclusivity at all.
	//
	// This single claim protects every caller of this shared service instance — deletion,
	// past-due dunning (both windows), the subscription-cancellation webhook, and trial
	// expiration all share ONE PlaidItemCleanupService instance in production (see main.go), so
	// wiring it here closes the H1 race for all of them at once rather than requiring each
	// caller to duplicate the claim/release ceremony around its own disconnect call.
	SetWorkClaimsRepo(repo repository.WorkClaimsRepository)

	// SetAccountRepo wires the account repository this service needs in order to transition an
	// item's accounts to 'removed' once its Plaid Item is gone. REQUIRED, not optional, and every
	// entry point refuses before touching Plaid when it is unset — see SetWorkClaimsRepo above for
	// the same argument.
	//
	// Issue #1569 Task 20 (A2). This service had no account repository at all, so removing a Plaid
	// Item stopped the syncing and left every account under it at status='active' forever: a frozen
	// balance rendered as a current one, with no "as of" date, reached by a routine background sweep
	// over items merely stuck in 'login_required'.
	SetAccountRepo(repo AccountRepoForCleanup)
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
	// MarkPlaidRemoved stamps linked_items.plaid_removed_at — the ONLY durable record that Plaid
	// /item/remove succeeded, and the only one that works for a 'replaced' row (Issue #1495). See
	// repository.LinkedItemRepository's doc comment for why MarkItemsDisconnected cannot carry
	// this fact, and RemoveItem/cleanupItems below for why this write must come FIRST.
	MarkPlaidRemoved(ctx context.Context, ids []string) error
	// MarkPlaidRemovalFailed records the OPPOSITE fact from MarkPlaidRemoved — "we can never remove
	// this Item with the credential on file" (Issue #1494). See repository.LinkedItemRepository's doc
	// comment for why the two facts need separate columns, and terminalPlaidRemovalFailure below for
	// which failures qualify.
	MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error
}

// plaidServiceForCleanup is a minimal interface for the Plaid operations
// needed by PlaidItemCleanupService.
type plaidServiceForCleanup interface {
	RemoveItem(ctx context.Context, accessToken string) error
}

// AccountRepoForCleanup is the one account-repository method this service needs: the bulk
// active -> removed transition for every account under a linked item. Issue #1569 Task 20.
//
// Deliberately narrow rather than repository.AccountRepository — that interface has upwards of
// forty methods, and a service whose only legitimate interest in accounts is "mark them removed"
// should not be handed the ability to purge one.
type AccountRepoForCleanup interface {
	MarkRemovedByLinkedItemID(ctx context.Context, linkedItemID string, reason domain.RemovalReason) error
}

// plaidItemCleanupService implements PlaidItemCleanupService
type plaidItemCleanupService struct {
	linkedItemRepo linkedItemRepoForCleanup
	plaidService   plaidServiceForCleanup
	claimsRepo     repository.WorkClaimsRepository // Issue #1413 H1: required, see SetWorkClaimsRepo
	accountRepo    AccountRepoForCleanup           // Issue #1569 Task 20: required, see SetAccountRepo
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

// SetAccountRepo wires the account repository this service requires. See the interface method's
// doc comment (Issue #1569 Task 20) for why an unset one is a refusal rather than a degradation.
func (s *plaidItemCleanupService) SetAccountRepo(repo AccountRepoForCleanup) {
	s.accountRepo = repo
}

// markAccountsRemoved transitions every still-active account under one linked item to 'removed'.
//
// Issue #1569 Task 20 (A2). Called from BOTH cleanup paths, at the same point in each: AFTER the
// plaid_removed_at stamp and BEFORE the 'disconnected' status write. That position is load-bearing
// for exactly the reason the stamp's own ordering comment gives — cleanupItems SKIPS 'disconnected'
// rows without re-attempting them, so any work still outstanding when that status lands is never
// done. Failing here therefore leaves the item live and retryable, and the retry re-enters with
// Plaid answering ITEM_NOT_FOUND (mapped to success) and MarkPlaidRemoved COALESCE-ing to a no-op,
// so the pass converges on one wasted API call.
//
// The reason is always RemovalReasonLinkRemoved and never varies per flow: see that constant, and
// migration 000167, for why the distinction between "swept", "subscription cancelled" and "erasure"
// deliberately does not reach this column.
//
// ⚠⚠ WHAT THIS TRANSITION DOES TO WHAT THE USER SEES, disclosed in full because a background job
// causing it is a large thing to do quietly (#1569 task 20, review round 1 — the first version of
// this disclosure mentioned only the button, which is the smallest of the three).
//
// apps/web/src/features/accounts/hooks/useActiveAccounts.ts's isInactive() returns true for
// 'removed', so a swept item's accounts, with no user action and no notification:
//
//   1. GAIN the "as of <date>" caption on their balance — the point of the change, and the defect
//      this fixes: the figure had stopped updating and was being rendered as current;
//   2. LEAVE the default Accounts list for the Closed toggle;
//   3. DROP OUT of every "now" total built from active accounts — current net worth and the
//      dashboard balance cards FALL by those accounts' last known balances.
//
// Historical series are NOT affected: they are built from balance_snapshots, which are retained,
// so past points keep the same values.
//
// (3) is the one worth arguing about, and it is accepted deliberately. The alternative is to keep
// counting a frozen figure in a total labelled "now", which is precisely the Critical this task
// exists to close — a number that is wrong and says it is current. A total that excludes an account
// nobody can sync is right; a total that includes a stale one is not. But "a background sweep
// silently changed my net worth" is a real support conversation, and if the product wants a
// notification on this transition, THIS is the line it hangs off. Nothing here sends one today.
func (s *plaidItemCleanupService) markAccountsRemoved(ctx context.Context, linkedItemIDs ...string) error {
	for _, id := range linkedItemIDs {
		if err := s.accountRepo.MarkRemovedByLinkedItemID(ctx, id, domain.RemovalReasonLinkRemoved); err != nil {
			s.logger.Error("Plaid item removed but marking its accounts removed failed; the accounts would otherwise keep showing a frozen balance as a live one",
				"item_id", id, "error", err)
			return fmt.Errorf("failed to mark accounts removed for item %s: %w", id, err)
		}
	}
	return nil
}

// RemoveAllUserItems implements PlaidItemCleanupService.RemoveAllUserItems
//
// Issue #1413 round-4 Critical finding C2: the item list is read INSIDE cleanupItems's claimed
// closure — see that function's doc comment for why reading it here, before the claim, would
// reopen the race a concurrent LinkAccounts call could exploit.
func (s *plaidItemCleanupService) RemoveAllUserItems(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		// Fail closed rather than degrading to the old nil-recheck behaviour. See the interface
		// method's doc comment: a nil here is the defect, not a configuration.
		return fmt.Errorf("RemoveAllUserItems: an eligibility recheck is required before removing items for a user about to be erased")
	}
	return s.cleanupItems(ctx, userID, s.linkedItemRepo.GetByUserID, recheck)
}

// RemoveItem implements PlaidItemCleanupService.RemoveItem — see that interface method's doc
// comment (Issue #1486 follow-up) for the full contract, and in particular for why ITEM_NOT_FOUND is success
// while every other Plaid error is a propagated failure.
func (s *plaidItemCleanupService) RemoveItem(ctx context.Context, linkedItemID string, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		// Fail closed, exactly as RemoveAllUserItems does: a nil here is the defect, not a
		// configuration, and refusing before the repository is touched keeps a caller from getting the
		// old unchecked behaviour back by passing nil to a required parameter.
		return fmt.Errorf("RemoveItem: an eligibility recheck is required before removing item %s from Plaid", linkedItemID)
	}
	if s.accountRepo == nil {
		// Fail closed BEFORE the irreversible Plaid call, for the same reason cleanupItems refuses an
		// unset claims repository: proceeding would remove the Item and leave its accounts asserting a
		// live balance they can no longer update, and nothing downstream would ever revisit them.
		return fmt.Errorf("plaidItemCleanupService: account repository not configured")
	}

	// Get the linked item
	item, err := s.linkedItemRepo.GetByID(ctx, linkedItemID)
	if err != nil {
		return fmt.Errorf("failed to get linked item %s: %w", linkedItemID, err)
	}

	// USE-TIME RE-VERIFICATION (Codex pass-5 Medium 2). This sits between the read above and the
	// Plaid call below — the latest point at which anything can still be learned — because the
	// caller's own claim (ClaimItemForCleanup) grants no exclusivity past its own statement and this
	// method's read is a separate transaction from it. See the interface method's doc comment for the
	// full argument, including why this is a recheck and not a lease.
	eligible, recheckErr := recheck(ctx)
	if recheckErr != nil {
		// Fail closed: "we could not tell" is not "still eligible", and /item/remove cannot be undone.
		// Deliberately NOT wrapped in ErrItemNoLongerEligible — an undetermined eligibility must be
		// retried, and that sentinel means "there was nothing to do".
		s.logger.Error("failed to re-verify item cleanup eligibility at the point of use, not removing item",
			"item_id", linkedItemID, "error", recheckErr)
		return fmt.Errorf("failed to re-verify cleanup eligibility for item %s: %w", linkedItemID, recheckErr)
	}
	if !eligible {
		// Nothing has been sent to Plaid and nothing is recorded — see ErrItemNoLongerEligible's doc
		// comment for what the caller must do with this (release its now-stale schedule and report
		// success), and why that is the SAME handling as a claim declined at selection time.
		s.logger.Info("linked item is no longer a cleanup candidate at the point of use; not calling Plaid",
			"item_id", linkedItemID, "plaid_item_id", item.ItemID)
		return fmt.Errorf("%w: item %s", ErrItemNoLongerEligible, linkedItemID)
	}

	if removeErr := s.removeItemInternal(ctx, item); removeErr != nil {
		if !isPlaidItemAlreadyGone(removeErr) {
			// Issue #1486 follow-up part B: PROPAGATE. Swallowing this reported "Plaid billing is stopped" to
			// ItemCleanupWorker.Work while the Item was still live and still billing, and the
			// worker's row Delete then erased the only local trace of it — after which
			// DeleteUserCascade's C2 guard had nothing left to see.
			//
			// ⚠ A FAILURE MODE THIS PROPAGATION CONVERTED, disclosed rather than fixed. If
			// /item/remove can NEVER succeed for this item — Plaid INVALID_ACCESS_TOKEN for a
			// malformed or wrong-environment token, or plaidService.RemoveItem's own "access token
			// cannot be empty" should decryption ever yield "" — then this error propagates on every
			// attempt, the job dead-letters after its inherited 5, ItemCleanupCheckWorker's backstop
			// re-enqueues it daily FOREVER, and uncleanedLinkedItemExistsClause holds
			// DeleteUserCascade at (false, nil) for that user PERMANENTLY. Pre-fix the row was simply
			// deleted and the erasure completed — over an Item that was, in this specific case,
			// genuinely unusable — so this is a converted failure mode, not a pre-existing one.
			//
			// It is accepted here because it is FAIL-CLOSED and because the alternative is worse: no
			// predicate available at this point can distinguish "token is permanently invalid, the
			// Item is beyond our reach" from "token is fine, Plaid is having an outage", and guessing
			// wrong in the other direction is exactly the orphaned-billing-Item bug this whole change
			// exists to close. What it needs is an operator escape hatch (alerting on a repeatedly
			// re-enqueued stranded item, plus an audited manual override), NOT a silent widening of
			// the success set. Do not add INVALID_ACCESS_TOKEN to isPlaidItemAlreadyGone to make this
			// go away without arguing the Plaid semantics explicitly first: ITEM_NOT_FOUND means the
			// ITEM is gone (billing stopped), whereas an invalid TOKEN says nothing about whether the
			// Item still exists or is still billable. Tracked as issue #1494.
			// Issue #1494 lands the escape hatch the paragraph above asked for, and it is narrower
			// than the "audited manual override" that issue proposed: a failure Plaid's own answer
			// proves cannot be retried away is RECORDED as such (plaid_removal_failed_at), which is
			// what lets C2 stop blocking that user's erasure forever — without ever claiming the Item
			// was removed. The ⚠ paragraph above is retained because its argument still holds for
			// every OTHER failure: nothing here widens isPlaidItemAlreadyGone, and an outage or a rate
			// limit still blocks, still retries, still fails closed.
			//
			// The error is STILL returned, unchanged in kind: this method's caller
			// (ItemCleanupWorker.Work) must keep failing, retrying and dead-lettering, because the row
			// still exists and re-attempting it is free of harm — the recorded verdict is permission
			// for the ERASURE to proceed, never permission to stop trying. If the recording write
			// itself fails, no sentinel is attached (see ErrPlaidRemovalUnrecoverable's ⚠ note): an
			// unrecorded verdict must not grant anyone permission to proceed.
			if code, terminal := terminalPlaidRemovalFailure(item, removeErr); terminal {
				if recErr := s.recordTerminalPlaidRemovalFailure(ctx, linkedItemID, code); recErr == nil {
					return fmt.Errorf("failed to remove Plaid item %s (%w, %s): %w",
						linkedItemID, ErrPlaidRemovalUnrecoverable, code, removeErr)
				}
			}
			s.logger.Error("failed to remove Plaid item; NOT reporting success",
				"item_id", linkedItemID,
				"plaid_item_id", item.ItemID,
				"error", removeErr,
			)
			return fmt.Errorf("failed to remove Plaid item %s: %w", linkedItemID, removeErr)
		}
		// Issue #1486 follow-up part A: ITEM_NOT_FOUND — Plaid has already forgotten this Item, so
		// /item/remove has nothing left to do and billing has already stopped. This is the ONLY
		// Plaid failure treated as success, and treating it so is a hard PREREQUISITE for the
		// propagation above: without it, a retry after a transient DB failure elsewhere in the
		// worker dead-letters with cleanup_scheduled_at still set, which no sweep can re-select and
		// the C2 guard reads as un-cleaned — a permanent block on that user's GDPR erasure. Mirrors
		// the identical branch cleanupItems has always had at its own removeItemInternal call site;
		// the asymmetry between the two was the defect.
		s.logger.Info("Plaid item already gone, treating as success",
			"item_id", linkedItemID,
			"plaid_item_id", item.ItemID,
		)
	}

	// Record durably that this item's Plaid side is done, matching cleanupItems's per-item
	// MarkItemsDisconnected. Propagated rather than best-effort: a retry re-runs the Plaid call and,
	// PROVIDED the removal itself can still succeed or answer ITEM_NOT_FOUND, lands here again — so
	// failing loudly costs one wasted API call and converges, whereas swallowing it leaves a row that
	// survives the caller's accounts-guarded Delete carrying neither the cleanup stamp nor
	// 'disconnected', which is the permanent-block state.
	//
	// That proviso is real, and the earlier version of this comment asserted convergence
	// unconditionally, which was wrong — see the permanently-failing-removal note on the error branch
	// above. This write's own convergence is not in doubt (a DB blip clears); the removal preceding it
	// may never succeed, and in that case nothing here converges.
	//
	// USER-VISIBLE SIDE EFFECT, disclosed (fix round 1, item 7a). linkedItemRepository.Delete — which
	// ItemCleanupWorker.Work calls next — is a deliberate no-op whenever any accounts row still
	// references this item, which is routine for the error/login_required/stale/revoked legs. So the
	// row commonly SURVIVES carrying 'disconnected', and the frontend renders that status as a
	// "Disconnected" badge titled "Account disconnected - resubscribe to reconnect"
	// (apps/web/src/features/accounts/AccountCard.tsx, components/AccountTable.tsx) — copy written for
	// the subscription-expiry flow (#679/#680), which is MISLEADING for orphaned-item cleanup, where
	// nothing about the user's subscription is at issue. Pre-fix those rows kept 'error'/'active'
	// instead, so this is new.
	//
	// Accepted here rather than papered over: 'disconnected' is still a true DATA statement about such
	// a row. Issue #1495 CORRECTS the reason this paragraph used to give, which was that "three
	// predicates depend on that meaning (uncleanedLinkedItemExistsClause, cleanupCandidateSelectQuery,
	// GetItemsPendingCleanup)". They no longer do — all three now read plaid_removed_at, precisely
	// because status could not express the fact for a 'replaced' row. So the argument against a
	// distinct status ("it would mean auditing all three predicates") is now WEAKER, not stronger: the
	// predicates are decoupled from status here. What remains true is that the defect is in the
	// frontend's COPY, not in the status, and that changing the status would still mean auditing every
	// scan site and every OTHER status-reading predicate (GetActiveItemsByUserID,
	// liveLinkedItemExistsClause, MarkItemsDisconnected's own guard). Still not a change to make as a
	// side effect of a billing fix — but if the badge copy is judged unacceptable, that ticket is now
	// cheaper than this comment used to claim.
	// Issue #1495: STAMP plaid_removed_at FIRST, before MarkItemsDisconnected, and the order is
	// load-bearing rather than stylistic.
	//
	// THE ORDER IS WHAT KEEPS THIS FROM BECOMING THE FIFTH "state marked, work never happened, mark
	// survives forever" defect on this branch. Consider the write failing right here, after a
	// successful Plaid removal:
	//
	//   - In THIS order, status is still live, so nothing downstream believes any work was done.
	//     ItemCleanupWorker.Work sees the error and retries; the retry re-reads the row, calls
	//     /item/remove again (Plaid answers ITEM_NOT_FOUND, which isPlaidItemAlreadyGone maps to
	//     SUCCESS), and reaches this line again. The DB blip clears and the stamp lands. The cost of
	//     the failure is one wasted API call; the state converges. Meanwhile
	//     uncleanedLinkedItemExistsClause still BLOCKS that user's erasure — fail-closed, which is
	//     the safe direction: we may not yet be able to prove Plaid is done, so we do not proceed to
	//     an irreversible hard delete.
	//   - In the REVERSE order it would be unrecoverable. MarkItemsDisconnected would have already
	//     written 'disconnected', and cleanupItems SKIPS 'disconnected' rows without calling Plaid,
	//     so no retry would ever reach this line again — leaving a row that claims cleanup happened,
	//     carries no proof of it, and permanently blocks that user's GDPR erasure. Exactly the
	//     failure shape the preceding four fixes on this branch each closed.
	//
	// The proviso from the paragraph above still applies unchanged: convergence depends on the Plaid
	// removal itself being able to succeed or answer ITEM_NOT_FOUND. If it can never succeed, nothing
	// here converges — see the permanently-failing-removal note on the error branch, and #1494.
	if stampErr := s.linkedItemRepo.MarkPlaidRemoved(ctx, []string{linkedItemID}); stampErr != nil {
		if errors.Is(stampErr, repository.ErrNoLinkedItemsUpdated) {
			// Zero rows here means something DIFFERENT from the MarkItemsDisconnected case below:
			// MarkPlaidRemoved has no status guard, so no live row can be filtered out of it. The only
			// way to match nothing is for the row to no longer exist — deleted concurrently between
			// this method's GetByID and now. Nothing left to record, and nothing left for any
			// predicate to read: uncleanedLinkedItemExistsClause is an EXISTS over linked_items, so an
			// absent row cannot block anything. Success, logged.
			s.logger.Warn("MarkPlaidRemoved matched zero rows — linked item row no longer exists",
				"item_id", linkedItemID,
				"error", stampErr,
			)
			return nil
		}
		s.logger.Error("Plaid removal succeeded but recording plaid_removed_at failed",
			"item_id", linkedItemID,
			"plaid_item_id", item.ItemID,
			"error", stampErr,
		)
		return fmt.Errorf("failed to record plaid_removed_at for item %s after Plaid removal: %w", linkedItemID, stampErr)
	}

	// Issue #1569 Task 20 (A2). BEFORE MarkItemsDisconnected — see markAccountsRemoved's doc comment
	// for why that position, and not after, is what keeps a failure here retryable.
	if acctErr := s.markAccountsRemoved(ctx, linkedItemID); acctErr != nil {
		return acctErr
	}

	if markErr := s.linkedItemRepo.MarkItemsDisconnected(ctx, []string{linkedItemID}); markErr != nil {
		if errors.Is(markErr, repository.ErrNoLinkedItemsUpdated) {
			// A legitimate no-op: the row was already 'disconnected' or 'replaced'. Same distinction
			// cleanupItems draws on this sentinel — escalating an idempotent zero-row UPDATE into a
			// hard failure would turn an already-converged state into a permanent retry loop, since
			// every retry would produce the identical zero-row result.
			//
			// ⚠ THIS IS THE BRANCH the 'replaced' caveat in the interface doc is about (fix round 2,
			// item 1), and the two cases it covers are NOT symmetric even though both are correctly
			// reported as success here:
			//
			//   - 'disconnected' — the row already records "Plaid side done", so it is inert on every
			//     downstream predicate, and nothing more is needed.
			//   - 'replaced' — the row records the RE-LINK, and there is nowhere left to record that
			//     Plaid is done, because status is the only slot and MarkItemsDisconnected must not
			//     overwrite a terminal soft-delete state (pinned by
			//     TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted). Since
			//     round 1, GetItemsPendingCleanup INCLUDES 'replaced', so this row stays selectable by
			//     the backstop until the caller's ClearCleanupSchedule succeeds.
			//
			// Returning nil is still right for both: the Plaid side genuinely IS done, and an error
			// here would stop the caller from ever reaching the clear that ends the loop — making the
			// permanent version of exactly the problem this note describes.
			s.logger.Warn("MarkItemsDisconnected updated zero rows — item was likely already disconnected/replaced",
				"item_id", linkedItemID,
				"error", markErr,
			)
			return nil
		}
		return fmt.Errorf("failed to persist 'disconnected' state for item %s after Plaid removal: %w", linkedItemID, markErr)
	}

	return nil
}

// recordTerminalPlaidRemovalFailure persists the "unremovable with the credential on file" verdict
// for one item, and returns nil ONLY when that verdict is durably recorded (or provably no longer
// needed). Issue #1494.
//
// Its return value is what both call sites use to decide whether they may report
// ErrPlaidRemovalUnrecoverable — i.e. hand a caller permission to proceed to an irreversible erasure —
// so the rule is: no record, no permission. A genuine write failure is returned and the pass reports a
// normal failure instead, which leaves C2 blocking and brings the retry back here.
//
// ErrNoLinkedItemsUpdated is NOT a failure, and its two causes are both benign (see
// MarkPlaidRemovalFailed's guard): the row no longer exists — an EXISTS-based guard cannot block on an
// absent row, and there is no Item record left to preserve — or the row already carries
// plaid_removed_at, i.e. some other attempt PROVED the removal succeeded, which is strictly better
// news than the verdict being written here and already unblocks everything.
func (s *plaidItemCleanupService) recordTerminalPlaidRemovalFailure(ctx context.Context, itemID, code string) error {
	if err := s.linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{itemID}, code); err != nil {
		if errors.Is(err, repository.ErrNoLinkedItemsUpdated) {
			s.logger.Warn("MarkPlaidRemovalFailed matched zero rows — item row is gone, or removal was already recorded as successful",
				"item_id", itemID, "error_code", code, "error", err)
			return nil
		}
		s.logger.Error("failed to record an unrecoverable Plaid removal failure; treating the pass as a retryable failure so the erasure stays blocked",
			"item_id", itemID, "error_code", code, "error", err)
		return err
	}
	s.logger.Error("Plaid item removal is unrecoverable with the credential on file; recorded on the item",
		"item_id", itemID, "error_code", code)
	return nil
}

// markItemsDisconnectedWhenStamped is cleanupItems's guarded MarkItemsDisconnected call (Issue
// #1495). It exists only so the "don't write 'disconnected' when the plaid_removed_at stamp genuinely
// failed" rule is a single expression rather than an indentation level wrapped around forty lines of
// existing sentinel handling.
//
// Returns nil when stamped is false — the caller has ALREADY appended the stamp failure to itemErrs,
// so the pass fails and retries; adding a second error for a write that was deliberately not
// attempted would only obscure the real cause.
func (s *plaidItemCleanupService) markItemsDisconnectedWhenStamped(ctx context.Context, ids []string, stamped bool) error {
	if !stamped {
		return nil
	}
	return s.linkedItemRepo.MarkItemsDisconnected(ctx, ids)
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

// DisconnectAllUserItemsIfEligible implements PlaidItemCleanupService.DisconnectAllUserItemsIfEligible
// — see the interface doc comment for the full contract (Issue #1413 round-7, Issue #1500).
//
// Issue #679, #680: trial/subscription expiration — disconnect items while preserving history.
//
// Issue #1413 round-4 Critical finding C2: the item list is read INSIDE cleanupItems's claimed
// closure — see that function's doc comment for why reading it here, before the claim, would reopen
// the race a concurrent LinkAccounts call could exploit.
func (s *plaidItemCleanupService) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		// Fail closed, exactly as RemoveAllUserItems and RemoveItem do: a nil here is the defect, not
		// a configuration. See the interface method's doc comment (Issue #1500).
		return fmt.Errorf("DisconnectAllUserItemsIfEligible: an eligibility recheck is required before disconnecting user %s's Plaid items", userID)
	}
	return s.cleanupItems(ctx, userID, s.linkedItemRepo.GetActiveItemsByUserID, recheck)
}

// fetchLinkedItemsFn matches linkedItemRepoForCleanup.GetByUserID / GetActiveItemsByUserID —
// the two ways cleanupItems's callers select which items are in scope.
type fetchLinkedItemsFn func(ctx context.Context, userID string) ([]domain.LinkedItem, error)

// cleanupItems is the shared per-item Plaid-removal loop used by RemoveAllUserItems and
// DisconnectAllUserItemsIfEligible — its only two callers as of Issue #1500 (Issue #1413 H1/H2/
// round-4 C2/round-7):
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
//   - Issue #1413 round-7: recheck is invoked immediately before EVERY remaining item (including
//     the first) — not just once for the whole call. The first time it reports ineligible
//     (false, nil), the loop stops without attempting any further items; this is a legitimate
//     partial completion, not a failure — but (round-14 H1) it is also not silent success: the
//     function returns ErrCleanupAborted (see that sentinel's doc comment) rather than nil, so a
//     caller cannot mistake "stopped early, work remains" for "nothing left to do". If recheck
//     itself errors, the loop also stops (fail-closed), and that error IS surfaced to the caller —
//     via the same errors.Join aggregation as a genuine per-item failure, taking priority over
//     ErrCleanupAborted (see the return logic at the bottom of this function).
//   - Issue #1500: recheck is REQUIRED and both public entry points refuse a nil before reaching
//     here. The guard below is the third, last-resort copy of that refusal, kept because this
//     function's whole contract above is written on the assumption that a recheck exists — a nil
//     reaching it would silently restore the exact "one authorisation read covers N Plaid calls"
//     behaviour #1500 deleted. It is deliberately unreachable from the two callers, not dead: the
//     cost of it being wrong is a defect no test would notice.
//   - Issue #1413 H1 residue (Codex review, PR #1443 follow-up): AFTER the loop (or the "no items"
//     branch) finishes with no genuine per-item failure and no recheck-triggered abort, this
//     function re-fetches ONE more time and checks for any item whose ID was not part of the
//     ORIGINAL fetch above — i.e. one created (by a path outside this claim, such as LinkAccounts)
//     after cleanupItems started. If one is found, ErrCleanupAborted is returned here too, even
//     though the recheck never reported ineligible — so ErrCleanupAborted has TWO causes and a
//     caller that wants to tell them apart must re-ask its own predicate (every caller does; see
//     ExecuteDeletion's branch for the canonical shape). See ErrCleanupAborted's doc comment and
//     verifyNoNewLinkedItemAppeared (below) for the full contract.
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
//   - Persists per-item success (including "already gone") via MarkPlaidRemoved and then
//     MarkItemsDisconnected (Issue #1495 — in that order; see the call site) BEFORE
//     returning, so the durable state survives even when other items in the same batch failed.
//   - Aggregates genuine per-item failures into a non-nil returned error — every item attempted
//     before an abort is still attempted regardless of an earlier failure (a single bad item must
//     not strand the rest still-linked and still-billing), but the caller (DeletionService.
//     ExecuteDeletion in particular) needs to know when to abort.
func (s *plaidItemCleanupService) cleanupItems(ctx context.Context, userID string, fetch fetchLinkedItemsFn, recheck func(ctx context.Context) (bool, error)) error {
	if s.claimsRepo == nil {
		return fmt.Errorf("plaidItemCleanupService: work claims repository not configured")
	}
	// Issue #1569 Task 20 (A2): same fail-closed rule as RemoveItem's, and stated before the claim is
	// taken so a misconfiguration cannot burn a lease.
	if s.accountRepo == nil {
		return fmt.Errorf("plaidItemCleanupService: account repository not configured")
	}
	// Issue #1500: see the doc comment's "recheck is REQUIRED" bullet for why this third copy of the
	// refusal exists even though both entry points already made it.
	if recheck == nil {
		return fmt.Errorf("plaidItemCleanupService.cleanupItems: an eligibility recheck is required before removing user %s's Plaid items", userID)
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
		// terminalErrs holds per-item failures that are UNRECOVERABLE and have been durably recorded
		// as such (Issue #1494). Kept apart from itemErrs because the two demand opposite things of the
		// caller: itemErrs means "retry, this may yet work", terminalErrs means "retrying cannot help,
		// and the obligation is on the row". A recording failure is appended to itemErrs, never here —
		// see ErrPlaidRemovalUnrecoverable's ⚠ note. Both are counted in failCount, so abortedCount's
		// arithmetic below is unchanged by this split.
		var terminalErrs []error
		var successCount, failCount, skippedCount int

	itemLoop:
		for _, item := range items {
			if item.Status == domain.LinkedItemStatusDisconnected {
				// Already handled by a prior attempt (H2 per-item durable state) — do not call
				// Plaid again for it.
				skippedCount++
				continue
			}
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
			if err := s.removeItemInternal(ctx, &item); err != nil {
				if isPlaidItemAlreadyGone(err) {
					s.logger.Info("Plaid item already gone, treating as success",
						"item_id", item.ID, "plaid_item_id", item.ItemID, "user_id", userID)
					successIDs = append(successIDs, item.ID)
					successCount++
					continue
				}
				// Issue #1494: an unrecoverable failure is recorded on the row and tracked apart from
				// the retryable ones. It still counts as a failure (failCount, and no MarkPlaidRemoved
				// stamp — nothing here believes the Item is gone); what changes is only what the
				// CALLER is told at the end of the pass. If the recording write fails, it falls
				// through to the ordinary retryable path below, deliberately: an unrecorded verdict
				// must leave C2 blocking and the pass retryable.
				if code, terminal := terminalPlaidRemovalFailure(&item, err); terminal {
					recErr := s.recordTerminalPlaidRemovalFailure(ctx, item.ID, code)
					failCount++
					if recErr != nil {
						itemErrs = append(itemErrs, fmt.Errorf("failed to record the unrecoverable removal failure for item %s (%s): %w", item.ID, code, recErr))
					} else {
						terminalErrs = append(terminalErrs, fmt.Errorf("item %s cannot be removed (%s): %w", item.ID, code, err))
					}
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
			// Issue #1495: stamp plaid_removed_at BEFORE MarkItemsDisconnected, for the same
			// order-is-load-bearing reason spelled out at RemoveItem's call site — if this write fails,
			// status must still be live so the retry re-attempts the item and reaches this line again.
			// The reverse order would leave 'disconnected' rows with no proof of removal, which
			// cleanupItems's own skip branch above then declines to re-attempt, permanently.
			//
			// THIS CALL IS ALSO WHAT KEEPS DELETION WORKING AT ALL, and it is not merely symmetry with
			// RemoveItem: DeleteUserCascade's C2 guard now demands plaid_removed_at on every one of the
			// user's rows, and cleanupItems (via RemoveAllUserItems) is the deletion flow's ONLY Plaid
			// cleanup path — RemoveItem is the per-item worker path. Without this call, tightening C2
			// would block every account deletion, not just the 'replaced' ones.
			stamped := true
			if stampErr := s.linkedItemRepo.MarkPlaidRemoved(ctx, successIDs); stampErr != nil {
				if errors.Is(stampErr, repository.ErrNoLinkedItemsUpdated) {
					// Zero rows means every id vanished from linked_items between the fetch and now
					// (MarkPlaidRemoved has no status guard that could filter a live row out — see its
					// doc comment). An absent row cannot block anything: uncleanedLinkedItemExistsClause
					// is an EXISTS over linked_items. Logged, not escalated, for the same reason the
					// MarkItemsDisconnected no-op below is not escalated.
					s.logger.Warn("MarkPlaidRemoved matched zero rows — linked item row(s) no longer exist",
						"user_id", userID, "ids_count", len(successIDs), "error", stampErr)
				} else {
					// GENUINE failure: do NOT fall through to MarkItemsDisconnected. Writing
					// 'disconnected' now would produce the exact unrecoverable state the ordering above
					// exists to prevent — a row asserting cleanup happened, carrying no proof of it, that
					// this loop's own skip branch then refuses to re-attempt forever. Leaving status live
					// is what makes the retry come back here.
					stamped = false
					itemErrs = append(itemErrs, fmt.Errorf("failed to record plaid_removed_at for %d item(s): %w", len(successIDs), stampErr))
				}
			}
			// Issue #1569 Task 20 (A2). Same position and same argument as RemoveItem's call: after the
			// stamp, before the 'disconnected' status this loop's own skip branch reads. Gated on
			// `stamped` for the reason markItemsDisconnectedWhenStamped is — when the stamp genuinely
			// failed the pass is already going to be retried, and the retry will reach here again.
			if stamped {
				if acctErr := s.markAccountsRemoved(ctx, successIDs...); acctErr != nil {
					itemErrs = append(itemErrs, acctErr)
					stamped = false
				}
			}

			if markErr := s.markItemsDisconnectedWhenStamped(ctx, successIDs, stamped); markErr != nil {
				if errors.Is(markErr, repository.ErrNoLinkedItemsUpdated) {
					// Issue #1413 Medium finding (PR #1443 Codex review round 4): a legitimate
					// no-op, not a genuine per-item failure — every id in successIDs was already
					// in a terminal state ('disconnected' or 'replaced') by the time this write
					// ran (e.g. a lone already-'replaced' item swept in by RemoveAllUserItems's
					// GetByUserID fetch, which does not filter by status). Logged for
					// observability so an operator or a test CAN still detect it via errors.Is on
					// the repository call directly, but escalating it into itemErrs here would
					// turn an idempotent, expected no-op into a hard per-item failure — exactly
					// the false-positive-abort failure mode #1453 (verifyNoNewLinkedItemAppeared,
					// below) exists to avoid reproducing.
					s.logger.Warn("MarkItemsDisconnected updated zero rows — every id was likely already disconnected/replaced",
						"user_id", userID, "ids_count", len(successIDs), "error", markErr)
				} else {
					// Losing the per-item durable marker reopens H2's "duplicate call on retry" risk
					// for these items — surface it rather than silently dropping it (a subsequent
					// retry would just re-attempt them; ITEM_NOT_FOUND being mapped to success limits
					// the actual damage to a wasted API call, not a real duplicate side effect, but a
					// caller should still know this failed).
					itemErrs = append(itemErrs, fmt.Errorf("failed to persist per-item cleanup state for %d item(s): %w", len(successIDs), markErr))
				}
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
			//
			// Issue #1494: it also takes priority over ErrPlaidRemovalUnrecoverable, and that
			// precedence is the important half. Any retryable failure in the pass means the pass is not
			// finished, so no caller may treat it as permission to proceed to an irreversible erasure —
			// even if some OTHER item in the same pass was correctly judged unremovable. The terminal
			// errors are folded in for diagnosis, and doing so is safe: an entry in terminalErrs wraps
			// the underlying Plaid error only — ErrPlaidRemovalUnrecoverable is attached at the final
			// return below and nowhere else, so no errors.Is check can find the sentinel on this path.
			itemErrs = append(itemErrs, terminalErrs...)
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
		if newItemErr := s.verifyNoNewLinkedItemAppeared(ctx, userID, fetch, knownItemIDs); newItemErr != nil {
			// Issue #1494: this check keeps its priority over the unrecoverable-removal sentinel below,
			// and the ordering is not cosmetic. A live item that appeared after the initial fetch was
			// never attempted at all, so the pass is genuinely incomplete — reporting the sentinel
			// instead would tell DeletionService it may proceed to an irreversible erasure over an item
			// nobody has even tried to remove and nothing has recorded a verdict for.
			return newItemErr
		}
		if len(terminalErrs) > 0 {
			// Issue #1494: every item that failed did so unrecoverably, AND every one of those verdicts
			// is durably on its row (a recording failure would have gone to itemErrs and returned
			// above). Only now may a caller with a convergence obligation proceed — see
			// ErrPlaidRemovalUnrecoverable's doc comment for who does, and for the two independent
			// conditions that still stand between this return and an actual erasure.
			return fmt.Errorf("%w: %d of %d Plaid item(s) for user %s: %w",
				ErrPlaidRemovalUnrecoverable, len(terminalErrs), len(items), userID, errors.Join(terminalErrs...))
		}
		return nil
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
// Issue #1413 #1453 (Codex review, PR #1443 follow-up): a new ID alone does NOT mean a live item
// was missed — RemoveAllUserItems's GetByUserID fetch has no status filter at all, so this second
// fetch can surface an item that was created AND ALREADY disconnected/replaced by some other path
// before this re-fetch ran. That is phantom residue (#1453), not a leak — no live item was ever
// unattended — so the comparison below is status-aware (isLiveLinkedItemStatus): only a new ID
// that is ALSO still live triggers ErrCleanupAborted. Getting this wrong in the other direction is
// the higher-stakes bug: RemoveAllUserItems is DeletionService's pre-cascade call, and
// DeletionService aborts the ENTIRE GDPR deletion on any non-nil error from it — a false abort here
// would block a user's deletion over residue that was never actually outstanding.
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
		if !isLiveLinkedItemStatus(item.Status) {
			// Issue #1413 #1453 (Codex review, PR #1443 follow-up): a new ID alone is NOT enough
			// to conclude a live item was missed. RemoveAllUserItems's underlying fetch
			// (GetByUserID) returns items in ANY status, so this second fetch can just as easily
			// surface an item that was created AND ALREADY disconnected/replaced by some other
			// path before this re-fetch ever ran — phantom residue (tracked separately as #1453),
			// not a leak. Aborting cleanup here would falsely trip ErrCleanupAborted even though
			// no live item remains — and DeletionService aborts a GDPR deletion on ANY non-nil
			// error from this call chain, so a false positive here is a compliance failure, not
			// mere untidiness. Only a LIVE new item (below) still triggers the abort.
			s.logger.Info("Plaid item cleanup: a new item ID appeared after the initial fetch but is not live (already disconnected/replaced) — not treated as outstanding work",
				"user_id", userID, "item_id", item.ID, "status", item.Status)
			continue
		}
		s.logger.Info("Plaid item cleanup: a new linked item appeared after the initial fetch and was never attempted — leaving cleanup marked incomplete",
			"user_id", userID, "item_id", item.ID)
		return fmt.Errorf("%w for user %s (a linked item was created after cleanup's initial fetch and was never attempted)",
			ErrCleanupAborted, userID)
	}
	return nil
}

// isLiveLinkedItemStatus reports whether status represents a still-live linked item — i.e. NOT
// 'disconnected' or 'replaced' — the Go-side mirror of profile_repository.go's
// liveLinkedItemExistsClause (`status IS NULL OR status NOT IN ('disconnected', 'replaced')`;
// read-only for this task, see that constant's doc comment for the canonical SQL form and #1444's
// rationale for the NULL arm). Used exclusively by verifyNoNewLinkedItemAppeared above to decide
// whether a newly-appeared item ID is actually outstanding work or merely phantom residue (#1453).
//
// The SQL clause's `status IS NULL` arm has no Go-side counterpart here: every real repository
// scan path this service fetches through (GetByUserID, GetActiveItemsByUserID —
// linked_item_repository.go) already normalizes a NULL column to domain.LinkedItemStatusActive
// via normalizeLinkedItemStatus before a domain.LinkedItem value exists at all, so a genuinely
// NULL status is not a reachable Go-level input to this function; only the SQL clause needs that
// explicit arm.
func isLiveLinkedItemStatus(status domain.LinkedItemStatus) bool {
	return status != domain.LinkedItemStatusDisconnected && status != domain.LinkedItemStatusReplaced
}
