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
	// A completed claim is never re-granted.
	//
	// claimed == false is a normal outcome, not an error — callers should log at
	// Info and return nil rather than treating it as a failure.
	Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (bool, error)

	// Complete marks the work durably finished. Idempotent: calling it again after
	// the claim is already completed is a no-op (the original completion time is
	// preserved). A completed claim is never re-granted by Claim.
	Complete(ctx context.Context, resourceType, resourceID, action string) error

	// Release abandons a claim so it becomes immediately reclaimable (rather than
	// waiting out the remaining lease), recording reason for diagnostics. Use this
	// when the external work failed and must be retried — never fall through to
	// treat the work as done. Idempotent.
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
// success rather than merely error-free.
func (r *workClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET completed_at = COALESCE(completed_at, NOW())
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, resourceType, resourceID, action)
	if err != nil {
		return fmt.Errorf("complete %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return nil
}

// Release un-completes the claim (defensively — Release is meant to be called
// on in-flight claims only) and pushes lease_expires_at into the past so the
// very next Claim call reclaims it, rather than waiting out the original
// lease. Setting it to NOW() - 1 second rather than exactly NOW() avoids any
// risk of a same-instant race with an immediately following Claim's own NOW().
func (r *workClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET completed_at = NULL,
		    lease_expires_at = NOW() - INTERVAL '1 second',
		    last_error = $4
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
	`, resourceType, resourceID, action, reason)
	if err != nil {
		return fmt.Errorf("release %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return nil
}
