package repository

// WorkClaimsRepository implements the durable work-claim mechanism described in
// migration 000114 (issue #1413, Codex review round 2). It replaces the
// self-assigning "conditional UPDATE" guard pattern (e.g. profile_repository.go's
// ClaimForDeletion) for destructive, at-most-once-in-flight actions.
//
// A guard proves a predicate held at one instant but reserves nothing: two callers
// can evaluate the same predicate, see the same state, and both proceed. This
// repository instead grants EXCLUSIVITY via a leased row in work_claims, backed by
// the work_claims_unique_active UNIQUE constraint on (resource_type, resource_id,
// action). Exactly one concurrent INSERT ... ON CONFLICT DO UPDATE can ever affect
// a row for a given tuple — Postgres serializes on the unique index itself, not on
// any application-level locking.
//
// This repository is a foundation only: it establishes exclusivity. The existing
// conditional state-transition predicates (e.g. claimForDeletionPredicate) still
// establish CORRECTNESS ("is this user still eligible") and must be re-checked,
// inside the same call, after the claim is won and before any irreversible
// external work. See the required call shape in the #1413 design doc:
//
//	claimed, err := claims.Claim(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, 15*time.Minute)
//	if err != nil { return err }
//	if !claimed { return nil } // another worker holds it, or it's already done
//
//	ok, err := repo.SomeConditionalTransition(ctx, userID)
//	if err != nil || !ok {
//	    _ = claims.Release(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, "precondition lost")
//	    return err
//	}
//
//	if err := externalWork(ctx); err != nil {
//	    _ = claims.Release(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, err.Error())
//	    return err
//	}
//
//	return claims.Complete(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue)
//
// KNOWN LIMITATION (flagged by Codex adversarial review, issue #1413): this API has no
// fencing token. Complete/Release identify a claim only by (resourceType, resourceID,
// action), not by the specific lease instance Claim granted. If a claimant's external
// work runs LONGER than the lease it was granted (not crashed, just slow — e.g. a GC
// pause or a hung network call), another caller may legitimately reclaim and start
// the same external work while the first is still in flight; if the first claimant
// then calls Complete/Release, it is acting on whichever lease instance currently
// occupies the row, not necessarily its own. This is a real, deliberate simplification
// of the design doc's frozen 3-method signature (Claim/Complete/Release, no token) —
// the mitigation is operational, not code-level: choose lease durations that are
// generously longer than the expected external call, and treat "still not done after
// the lease" as equivalent to "crashed." Release is hardened below to never resurrect
// a claim another worker already Completed, which closes the most damaging half of
// this gap (a stale Release can no longer un-complete finished work), but it does not
// close the "both workers run the external side effect" half. Callers with unusually
// slow or retried external calls (Paddle, Plaid) should size leases accordingly.

import (
	"context"
	"fmt"
	"time"
)

// Action constants for work_claims.action. Callers MUST use these rather than
// free-form strings — the migration's CHECK constraint enforces the same fixed
// set at the database level, so a typo here fails loudly instead of silently
// creating an unclaimed action.
const (
	ActionPlaidDisconnectPastDue       = "plaid_disconnect_past_due"
	ActionPastDueGraceCancel           = "past_due_grace_cancel"
	ActionAccountDeletion              = "account_deletion"
	ActionRetentionNotice30d           = "retention_notice_30d"
	ActionRetentionNotice7d            = "retention_notice_7d"
	ActionSubscriptionCancelledCleanup = "subscription_cancelled_cleanup"
)

// ResourceTypeUser is the work_claims.resource_type value for all current
// actions (all six claim on a user). The column stays a plain TEXT (not an
// enum/FK) so the table can cover non-user resources in the future.
//
// Always use this constant, never a hand-typed literal: unlike action,
// resource_type has no CHECK constraint (deliberately, for extensibility), so a
// typo (e.g. "users" instead of "user") would silently create a second,
// non-exclusive claim namespace instead of failing loudly.
const ResourceTypeUser = "user"

// WorkClaimsRepository grants exclusive, durable, leased claims on
// (resourceType, resourceID, action) tuples for destructive actions that must
// run at most once in flight. See the package doc comment above for the
// required call shape.
type WorkClaimsRepository interface {
	// Claim attempts to exclusively reserve (resourceType, resourceID, action) for
	// the caller, with a lease of the given duration. Returns claimed=false when
	// another worker holds an unexpired, incomplete lease, or when the action was
	// already completed. A lease that has expired (crashed worker) is reclaimable.
	// A completed claim is never re-granted. lease must be positive; Claim returns
	// an error without touching the database otherwise.
	//
	// claimed == false is a normal outcome, not an error — callers should log at
	// Info and return nil rather than treating it as a failure.
	Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (bool, error)

	// Complete marks the work durably finished. Idempotent: calling it again after
	// the claim is already completed is a no-op (the original completion time is
	// preserved). A completed claim is never re-granted by Claim. Returns an error
	// if no claim exists for (resourceType, resourceID, action) — Complete should
	// only ever be called after a successful Claim, so a "not found" result
	// usually means a mismatched resourceType/resourceID/action (e.g. a typo)
	// rather than a benign race.
	Complete(ctx context.Context, resourceType, resourceID, action string) error

	// Release abandons a claim so it becomes immediately reclaimable (rather than
	// waiting out the remaining lease), recording reason for diagnostics. Use this
	// when the external work failed and must be retried — never fall through to
	// treat the work as done. Idempotent, and a no-op (nil error) both when no
	// claim exists and when the claim was already Completed by someone else —
	// Release can never resurrect completed work.
	Release(ctx context.Context, resourceType, resourceID, action, reason string) error
}

type workClaimsRepository struct {
	db DB
}

// NewWorkClaimsRepository creates a postgres-backed WorkClaimsRepository.
func NewWorkClaimsRepository(db DB) WorkClaimsRepository {
	return &workClaimsRepository{db: db}
}

// Claim's exclusivity comes entirely from the ON CONFLICT target: work_claims_unique_active.
// On no conflict (fresh tuple), the INSERT proceeds and the row count is 1 — claimed.
// On conflict, the DO UPDATE's WHERE is evaluated against the existing row:
//   - completed_at IS NULL AND lease_expires_at < NOW() (lease expired, not completed) → update
//     applies, row count 1 — reclaimed.
//   - otherwise (still leased, or completed) → no update happens, row count 0 — not claimed.
//
// Concurrently, Postgres locks the conflicting row for the duration of each attempt, so
// exactly one concurrent caller's statement can ever see row count 1 for a given tuple.
func (r *workClaimsRepository) Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		return false, fmt.Errorf("claim %s/%s/%s: lease must be positive, got %s", resourceType, resourceID, action, lease)
	}
	ct, err := r.db.Exec(ctx, `
		INSERT INTO work_claims (resource_type, resource_id, action, claimed_at, lease_expires_at, completed_at, attempts, last_error)
		VALUES ($1, $2, $3, NOW(), NOW() + make_interval(secs => $4), NULL, 1, NULL)
		ON CONFLICT (resource_type, resource_id, action) DO UPDATE
		SET claimed_at = NOW(),
		    lease_expires_at = NOW() + make_interval(secs => $4),
		    attempts = work_claims.attempts + 1,
		    last_error = NULL
		WHERE work_claims.completed_at IS NULL
		  AND work_claims.lease_expires_at < NOW()
	`, resourceType, resourceID, action, lease.Seconds())
	if err != nil {
		return false, fmt.Errorf("claim %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return ct.RowsAffected() > 0, nil
}

// Complete marks the claim durably finished. COALESCE preserves the original
// completed_at on repeat calls, making this a true no-op after the first
// success rather than merely error-free. last_error is cleared so a claim that
// was previously Released with a failure reason doesn't retain a stale reason
// after it eventually succeeds. Returns an error if no row matched — Complete
// is only ever expected to be called immediately after a successful Claim, so
// "no matching claim" signals a caller bug (e.g. an action typo) rather than a
// benign outcome, unlike Release below.
func (r *workClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action string) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET completed_at = COALESCE(completed_at, NOW()),
		    last_error = NULL
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, resourceType, resourceID, action)
	if err != nil {
		return fmt.Errorf("complete %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("complete %s/%s/%s: no matching claim (was Claim ever called for this exact resource_type/resource_id/action?)", resourceType, resourceID, action)
	}
	return nil
}

// Release pushes lease_expires_at into the past so the very next Claim call
// reclaims it, rather than waiting out the original lease, and records reason
// for diagnostics. Setting it to NOW() - 1 second rather than exactly NOW()
// avoids any risk of a same-instant race with an immediately following Claim's
// own NOW().
//
// The WHERE clause's "AND completed_at IS NULL" is the fix for a real defect an
// adversarial review surfaced: without it, a stale/late Release call from a
// worker that already lost its lease (see the KNOWN LIMITATION doc comment
// above) could silently un-complete a claim a different worker had already
// finished, resurrecting work that must never be re-granted. With the guard,
// Release is a no-op (nil error, not an error) both when no claim exists at all
// and when the claim was already completed — deliberately not surfaced as an
// error, since Release's return value is meant to be discarded by callers per
// the design doc's call shape (`_ = claims.Release(...)`), and "someone else
// already finished it" is not a caller bug worth flagging the way Complete's
// "never claimed" case is.
func (r *workClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET lease_expires_at = NOW() - INTERVAL '1 second',
		    last_error = $4
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
		  AND completed_at IS NULL
	`, resourceType, resourceID, action, reason)
	if err != nil {
		return fmt.Errorf("release %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return nil
}
