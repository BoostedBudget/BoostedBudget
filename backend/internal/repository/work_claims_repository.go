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
// conditional state-transition predicates (e.g. ClaimForDeletion's userRequestedDeletionClaimPredicate
// / retentionDeletionClaimPredicate, profile_repository.go) still
// establish CORRECTNESS ("is this user still eligible") and must be re-checked,
// inside the same call, after the claim is won and before any irreversible
// external work. See the required call shape in the #1413 design doc:
//
//	token, claimed, err := claims.Claim(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, 15*time.Minute)
//	if err != nil { return err }
//	if !claimed { return nil } // another worker holds it, or it's already done
//
//	ok, err := repo.SomeConditionalTransition(ctx, userID)
//	if err != nil || !ok {
//	    _, _ = claims.Release(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, token, "precondition lost")
//	    return err
//	}
//
//	if err := externalWork(ctx); err != nil {
//	    _, _ = claims.Release(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, token, err.Error())
//	    return err
//	}
//
//	completed, err := claims.Complete(ctx, ResourceTypeUser, userID, ActionPlaidDisconnectPastDue, token)
//	if err != nil {
//	    return err
//	}
//	if !completed {
//	    // We lost our lease while doing the external work: another worker's Claim
//	    // reclaimed this tuple and may have performed the SAME external side effect
//	    // concurrently. This must be visible, not silent — log at Error/Warn, page,
//	    // whatever the caller's convention is. Do NOT treat this as a benign race
//	    // the way claimed == false is; it means the at-most-once guarantee this
//	    // table exists to provide may have just been violated for this action.
//	    slog.Error("work claim lost mid-flight; external work may have run twice",
//	        "resource_type", ResourceTypeUser, "resource_id", userID, "action", ActionPlaidDisconnectPastDue)
//	}
//
// FENCING TOKEN (closes a gap flagged by Codex adversarial review, issue #1413):
// Claim/Complete/Release originally identified a claim only by (resourceType,
// resourceID, action), never by which lease INSTANCE Claim actually granted. If a
// claimant's external work ran LONGER than the lease it was granted — not
// crashed, just slow (a GC pause, a hung Plaid/Paddle call) — another caller could
// legitimately reclaim and start the SAME external work while the first was still
// in flight. The first claimant's eventual Complete/Release would then silently
// act on whichever lease instance currently occupied the row, not necessarily its
// own, and the record would show one execution when two actually happened.
//
// This is now closed rather than merely mitigated: Claim generates a fresh
// lease_token (a UUID) on every successful claim, including reclaims of an
// expired lease, and returns it to the caller. Complete and Release require that
// token to match the row's CURRENT lease_token — via "AND lease_token = $n" in
// their WHERE clause — to have any effect at all, and report via their ok return
// value whether they matched. A stale caller's Complete/Release is therefore a
// true no-op against the reclaimer's row: it cannot mark the reclaimer's lease
// finished, and it cannot touch the reclaimer's lease_expires_at or last_error.
// Lease sizing (choosing a duration generously longer than the expected external
// call) remains good operational practice to minimize how often this happens at
// all, but correctness no longer depends on it.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
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

	// ActionPlaidDisconnectUser and ActionPaddleCancelSubscription are OPERATION-keyed
	// side-effect actions (Issue #1413 Codex round-3 finding H1), not FLOW-keyed like the six
	// actions above. Four different flows (plaid_disconnect_past_due, past_due_grace_cancel,
	// subscription_cancelled_cleanup, account_deletion) each perform a Plaid disconnect and/or a
	// Paddle cancel for the same user, but hold DIFFERENT flow-claim rows — so those flow claims
	// alone do not stop two of them from calling Plaid/Paddle for the same user concurrently.
	//
	// These two actions close that gap: "one Plaid disconnect at a time for this user, whoever
	// wants it" / "one Paddle cancel at a time for this user, whoever wants it" — claimed INSIDE
	// a flow's own claim, immediately around the external call.
	//
	// IMPORTANT: unlike the six flow actions above (each conceptually "once ever" per user),
	// Plaid disconnect and Paddle cancel can legitimately recur across a user's lifetime
	// (reconnect, then a LATER dunning cycle disconnects again). Callers of these two actions
	// MUST Release (never Complete) once the external call finishes, win or lose — Complete's
	// "a completed claim is never re-granted" semantics would otherwise permanently disable the
	// operation for that user after its first success. See plaid_item_cleanup_service.go's
	// claimSideEffectAndRun for the enforced call shape.
	ActionPlaidDisconnectUser      = "plaid_disconnect_user"
	ActionPaddleCancelSubscription = "paddle_cancel_subscription"
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
// required call shape and the fencing-token contract.
type WorkClaimsRepository interface {
	// Claim attempts to exclusively reserve (resourceType, resourceID, action) for
	// the caller, with a lease of the given duration. Returns claimed=false when
	// another worker holds an unexpired, incomplete lease, or when the action was
	// already completed. A lease that has expired (crashed OR merely slow worker)
	// is reclaimable. A completed claim is never re-granted. lease must be
	// positive; Claim returns an error without touching the database otherwise.
	//
	// On success, token is a freshly generated fencing token identifying THIS
	// lease instance — including on a reclaim of an expired lease, where it is
	// always different from whatever token the previous (crashed/slow) holder
	// was given. token must be presented to Complete/Release; a caller that
	// forgets it can no longer act on this claim at all, by construction.
	//
	// claimed == false is a normal outcome, not an error — callers should log at
	// Info and return nil rather than treating it as a failure. On claimed ==
	// false, token is always empty.
	Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (token string, claimed bool, err error)

	// Complete marks the work durably finished, but only if token matches the
	// row's CURRENT lease_token. Idempotent for repeated calls with the SAME
	// valid token: the original completed_at is preserved (COALESCE) and ok
	// remains true. A completed claim is never re-granted by Claim.
	//
	// Returns ok=false (with err=nil) when no row matches (resourceType,
	// resourceID, action, token) — either because no claim was ever made for
	// that tuple (e.g. a caller bug/typo), or because token is stale: the
	// caller's lease was reclaimed by another worker while this one was still
	// doing external work. These two cases are deliberately not distinguished
	// by return value, because the caller-side response is the same either
	// way — do NOT treat the work as durably recorded — but a caller that gets
	// ok == false after actually performing the external side effect MUST log
	// that loudly: it means the effect may have just happened twice, which is
	// the one thing this table exists to prevent, and it must be visible
	// rather than silently swallowed.
	Complete(ctx context.Context, resourceType, resourceID, action, token string) (ok bool, err error)

	// Release abandons a claim so it becomes immediately reclaimable (rather
	// than waiting out the remaining lease), recording reason for diagnostics,
	// but only if token matches the row's CURRENT lease_token. Use this when
	// the external work failed and must be retried — never fall through to
	// treat the work as done.
	//
	// Returns ok=false (with err=nil) — a genuine no-op, nothing is written —
	// when no row matches (resourceType, resourceID, action, token): no claim
	// exists, the claim was already Completed by someone else (Release can
	// never resurrect completed work), or token is stale because another
	// worker already reclaimed this tuple. A stale worker's Release must not
	// disturb the reclaimer's in-progress lease or last_error, and it does
	// not: the WHERE clause excludes that row entirely.
	Release(ctx context.Context, resourceType, resourceID, action, token, reason string) (ok bool, err error)
}

// ClaimOutcome distinguishes WHY a ClaimOutcome call did or did not grant a fresh lease — the
// ambiguity Codex round-3 review flagged as finding H3 against the bare bool Claim returns.
//
// claimed == false from Claim conflates two situations that need OPPOSITE caller responses:
//   - another worker holds an unexpired, incomplete lease RIGHT NOW (retryable: if that worker
//     crashes, the lease will expire and the work becomes claimable again, but only if someone
//     retries the caller)
//   - the action already ran to completion for this tuple (terminal: there is nothing to retry)
//
// Callers that re-run on their own schedule regardless of outcome (e.g. a daily cron job) can
// safely treat both as "skip for now" — the next run tries again either way. A webhook is
// different: returning nil for a HeldByOther outcome gets the webhook marked processed, and
// nothing else will ever retry the work if the other worker crashed mid-lease. Such a caller
// MUST turn HeldByOther into a retryable error instead of a silent ack.
type ClaimOutcome int

const (
	// ClaimHeldByOther is the zero value deliberately: an uninitialized/zeroed ClaimOutcome must
	// never be mistaken for ClaimWon by a caller that forgets to check err. It means another
	// worker holds an unexpired, incomplete lease for this tuple right now.
	ClaimHeldByOther ClaimOutcome = iota
	// ClaimAlreadyCompleted means the action already ran to completion for this tuple — a
	// genuine, terminal skip. There is nothing to retry.
	ClaimAlreadyCompleted
	// ClaimWon means the caller now holds a fresh, exclusive lease; token is non-empty.
	ClaimWon
)

// String renders outcome for logging.
func (o ClaimOutcome) String() string {
	switch o {
	case ClaimWon:
		return "won"
	case ClaimAlreadyCompleted:
		return "already_completed"
	case ClaimHeldByOther:
		return "held_by_other"
	default:
		return "unknown"
	}
}

// WorkClaimsOutcomeRepository extends WorkClaimsRepository with ClaimOutcome, which
// disambiguates why a claim attempt did not win a fresh lease (issue #1413, Codex round-3
// finding H3).
//
// This is a SEPARATE interface — embedding WorkClaimsRepository rather than widening it in
// place — so every existing implementation and test double of WorkClaimsRepository (several of
// which were under concurrent edit elsewhere on this branch when H3 was fixed) keeps compiling
// with zero changes. The concrete *workClaimsRepository satisfies both interfaces; production
// wiring opts a specific caller into the wider view via NewWorkClaimsOutcomeRepository only
// where that caller's own logic actually depends on the HeldByOther/AlreadyCompleted
// distinction (currently: the subscription-cancellation webhook, subscription_webhook.go).
type WorkClaimsOutcomeRepository interface {
	WorkClaimsRepository

	// ClaimOutcome behaves exactly like Claim (same exclusivity guarantee, same lease
	// semantics), but on a lost claim reports WHY via outcome instead of collapsing both cases
	// into a bare false. token is non-empty only when outcome == ClaimWon.
	ClaimOutcome(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (token string, outcome ClaimOutcome, err error)
}

type workClaimsRepository struct {
	db DB
}

// NewWorkClaimsRepository creates a postgres-backed WorkClaimsRepository.
func NewWorkClaimsRepository(db DB) WorkClaimsRepository {
	return &workClaimsRepository{db: db}
}

// NewWorkClaimsOutcomeRepository creates the same postgres-backed repository as
// NewWorkClaimsRepository, but returns it through the wider WorkClaimsOutcomeRepository view so
// the caller can use ClaimOutcome. Kept as a second constructor (rather than changing
// NewWorkClaimsRepository's return type) purely for call-site clarity about which callers need
// outcome disambiguation — the two constructors return the same concrete type and can safely
// share one underlying *pgxpool.Pool / work_claims table.
func NewWorkClaimsOutcomeRepository(db DB) WorkClaimsOutcomeRepository {
	return &workClaimsRepository{db: db}
}

// Claim's exclusivity comes entirely from the ON CONFLICT target: work_claims_unique_active.
// On no conflict (fresh tuple), the INSERT proceeds and the row count is 1 — claimed.
// On conflict, the DO UPDATE's WHERE is evaluated against the existing row:
//   - completed_at IS NULL AND lease_expires_at < NOW() (lease expired, not completed) → update
//     applies, row count 1 — reclaimed, with a freshly generated lease_token.
//   - otherwise (still leased, or completed) → no update happens, row count 0 — not claimed.
//
// Concurrently, Postgres locks the conflicting row for the duration of each attempt, so
// exactly one concurrent caller's statement can ever see row count 1 for a given tuple.
//
// The token is generated in Go (not via a SQL-side default or RETURNING) because the
// caller needs to know it unconditionally BEFORE learning whether the claim was won —
// it is simply discarded (returned as "") when RowsAffected() is 0.
func (r *workClaimsRepository) Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, bool, error) {
	if lease <= 0 {
		return "", false, fmt.Errorf("claim %s/%s/%s: lease must be positive, got %s", resourceType, resourceID, action, lease)
	}
	token := uuid.New().String()
	ct, err := r.db.Exec(ctx, `
		INSERT INTO work_claims (resource_type, resource_id, action, claimed_at, lease_expires_at, completed_at, attempts, last_error, lease_token)
		VALUES ($1, $2, $3, NOW(), NOW() + make_interval(secs => $4), NULL, 1, NULL, $5)
		ON CONFLICT (resource_type, resource_id, action) DO UPDATE
		SET claimed_at = NOW(),
		    lease_expires_at = NOW() + make_interval(secs => $4),
		    attempts = work_claims.attempts + 1,
		    last_error = NULL,
		    lease_token = $5
		WHERE work_claims.completed_at IS NULL
		  AND work_claims.lease_expires_at < NOW()
	`, resourceType, resourceID, action, lease.Seconds(), token)
	if err != nil {
		return "", false, fmt.Errorf("claim %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	if ct.RowsAffected() == 0 {
		return "", false, nil
	}
	return token, true, nil
}

// ClaimOutcome is Claim's exclusivity mechanism (same ON CONFLICT ... WHERE, same lease
// semantics — see Claim's doc comment above) plus one further distinction on a lost claim:
// WHY it was lost. A single statement determines both in one round trip, atomically:
//
//   - "attempt" is exactly Claim's INSERT ... ON CONFLICT DO UPDATE, with RETURNING added so a
//     won claim (fresh insert OR reclaim of an expired lease) surfaces its own lease_token.
//   - "existing" reads the row's pre-statement state (same snapshot Claim's WHERE clause would
//     have evaluated against) purely to tell an unexpired-and-incomplete lease apart from a
//     completed one when "attempt" did NOT win.
//
// This must stay one statement: reading "existing" via a separate round trip after a failed
// Claim would race against a concurrent Complete/Release landing in between, misreporting a
// since-completed claim as HeldByOther or vice versa. Within one statement, both CTEs and the
// final SELECT share Postgres's single per-statement snapshot, so there is no such window.
//
// The final SELECT has no FROM clause (just scalar subqueries) deliberately: a FROM
// work_claims clause would, for a genuinely fresh tuple, see zero rows under that same
// snapshot rule (the INSERT's own row isn't visible to a sibling read in the same statement),
// which would make the CASE expression evaluate against no rows at all and return nothing —
// exactly the tuple this method most needs to report ClaimWon for.
func (r *workClaimsRepository) ClaimOutcome(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, ClaimOutcome, error) {
	if lease <= 0 {
		return "", ClaimHeldByOther, fmt.Errorf("claim outcome %s/%s/%s: lease must be positive, got %s", resourceType, resourceID, action, lease)
	}
	token := uuid.New().String()
	var state string
	// sql.NullString (not *string): pgxmock's Scan only recognizes the database/sql
	// Scanner interface as a fallback for a NULL column value, not a bare pointer-to-
	// pointer — see TestWorkClaimsRepository_ClaimOutcome_HeldByOther/_AlreadyCompleted,
	// where "lease_token" comes back NULL. Real pgx supports both; this keeps the unit
	// tests (pgxmock) and the live-DB integration tests scanning the same way.
	var wonToken sql.NullString
	err := r.db.QueryRow(ctx, `
		WITH attempt AS (
			INSERT INTO work_claims (resource_type, resource_id, action, claimed_at, lease_expires_at, completed_at, attempts, last_error, lease_token)
			VALUES ($1, $2, $3, NOW(), NOW() + make_interval(secs => $4), NULL, 1, NULL, $5)
			ON CONFLICT (resource_type, resource_id, action) DO UPDATE
			SET claimed_at = NOW(),
			    lease_expires_at = NOW() + make_interval(secs => $4),
			    attempts = work_claims.attempts + 1,
			    last_error = NULL,
			    lease_token = $5
			WHERE work_claims.completed_at IS NULL
			  AND work_claims.lease_expires_at < NOW()
			RETURNING lease_token
		),
		existing AS (
			SELECT completed_at
			FROM work_claims
			WHERE resource_type = $1 AND resource_id = $2 AND action = $3
		)
		SELECT
			CASE
				WHEN EXISTS (SELECT 1 FROM attempt) THEN 'won'
				WHEN EXISTS (SELECT 1 FROM existing WHERE completed_at IS NOT NULL) THEN 'already_completed'
				ELSE 'held_by_other'
			END,
			(SELECT lease_token FROM attempt)
	`, resourceType, resourceID, action, lease.Seconds(), token).Scan(&state, &wonToken)
	if err != nil {
		return "", ClaimHeldByOther, fmt.Errorf("claim outcome %s/%s/%s: %w", resourceType, resourceID, action, err)
	}

	switch state {
	case "won":
		if !wonToken.Valid {
			// Defensive: RETURNING lease_token should never be NULL for a row we just
			// wrote it into ourselves. Surface loudly rather than returning a false ClaimWon
			// with an empty token, which a caller would treat as a usable lease.
			return "", ClaimHeldByOther, fmt.Errorf("claim outcome %s/%s/%s: won but RETURNING lease_token was NULL", resourceType, resourceID, action)
		}
		return wonToken.String, ClaimWon, nil
	case "already_completed":
		return "", ClaimAlreadyCompleted, nil
	default:
		return "", ClaimHeldByOther, nil
	}
}

// Complete marks the claim durably finished, but only for the row whose CURRENT
// lease_token equals token — the fencing check. COALESCE preserves the original
// completed_at on repeat calls with the same valid token, making this a true
// no-op after the first success rather than merely error-free. last_error is
// cleared so a claim that was previously Released with a failure reason doesn't
// retain a stale reason after it eventually succeeds.
//
// ok is false, with no error, whenever the tuple+token doesn't match a row —
// whether that's "never claimed" or "lease was reclaimed out from under us."
// See the interface doc comment for why callers must treat ok == false as
// significant rather than benign.
func (r *workClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action, token string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET completed_at = COALESCE(completed_at, NOW()),
		    last_error = NULL
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3 AND lease_token = $4
	`, resourceType, resourceID, action, token)
	if err != nil {
		return false, fmt.Errorf("complete %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return ct.RowsAffected() > 0, nil
}

// Release pushes lease_expires_at into the past so the very next Claim call
// reclaims it, rather than waiting out the original lease, and records reason
// for diagnostics — but only for the row whose CURRENT lease_token equals
// token. Setting it to NOW() - 1 second rather than exactly NOW() avoids any
// risk of a same-instant race with an immediately following Claim's own NOW().
//
// The WHERE clause's "AND completed_at IS NULL" guard predates the fencing
// token and is kept deliberately even though the token check now makes most of
// its original failure mode unreachable (a stale caller's OLD token can no
// longer match a row a reclaimer has since completed, because Claim
// regenerated lease_token on reclaim before that completion). It remains
// cheap, defense-in-depth insurance against ever resurrecting a completed
// claim, which the design explicitly calls out as a hardening to preserve.
//
// ok is false, with no error, whenever the tuple+token doesn't match a row —
// no claim exists at all, the claim was already completed by someone else, or
// token is stale because another worker's Claim has since reclaimed this
// tuple. In every one of those cases nothing is written: a stale Release must
// not disturb the reclaimer's in-progress lease_expires_at or last_error, and
// excluding the row entirely from the WHERE clause is what guarantees that.
func (r *workClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, token, reason string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE work_claims
		SET lease_expires_at = NOW() - INTERVAL '1 second',
		    last_error = $5
		WHERE resource_type = $1 AND resource_id = $2 AND action = $3
		  AND lease_token = $4
		  AND completed_at IS NULL
	`, resourceType, resourceID, action, token, reason)
	if err != nil {
		return false, fmt.Errorf("release %s/%s/%s: %w", resourceType, resourceID, action, err)
	}
	return ct.RowsAffected() > 0, nil
}
