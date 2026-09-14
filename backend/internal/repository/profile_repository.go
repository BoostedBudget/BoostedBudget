package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrDuplicateEmail = errors.New("email already exists")

	// ErrRetentionClockPreconditionLost is returned by SetRetentionDates when the user row exists
	// but the caller's authorising condition (RetentionClockGuard) no longer held at the moment of
	// the write — someone else changed this user underneath the caller. Deliberately DISTINCT from
	// ErrUserNotFound: the two mean different things and only one of them is an error. "Someone else
	// changed this user" is a legitimate stop (the caller abandons the write and lets whatever flow
	// now owns this user proceed); "no such user" is a genuine failure worth retrying/alerting on.
	ErrRetentionClockPreconditionLost = errors.New("retention clock precondition lost: the user's authorising state changed concurrently")

	// ErrRetentionClockGuardUnspecified is returned when a caller passes the zero-value
	// RetentionClockGuard. The guard is REQUIRED and fails closed: an unspecified guard is rejected
	// rather than silently defaulting to an unguarded write, because a default that silently skips
	// the check is exactly how the original defect existed. A caller that genuinely needs no guard
	// must say so explicitly with RetentionClockUnguarded.
	ErrRetentionClockGuardUnspecified = errors.New("retention clock guard unspecified: pass RetentionClockRequiresStatus(...) or, explicitly, RetentionClockUnguarded")
)

// RetentionClockGuard is the caller-supplied authorising condition for a SetRetentionDates write:
// the subscription_status the row must STILL carry, evaluated against committed state inside the
// write's own statement, not in an earlier (and therefore non-atomic) read.
//
// It exists because SetRetentionDates's two production callers do NOT share one authorising
// condition — the cancellation webhook may arm the clock only while the user is still 'cancelled',
// while TrialService.ExpireTrial may arm it only for the user its own CAS
// (ExpireTrialIfNoOverride) just moved to 'inactive'. Hardcoding either condition inside the
// repository would be wrong for the other caller, so the condition is passed by the caller that
// knows it — the same way CancelIfPastDueOccurrenceWithRetention takes its expected past_due
// occurrence value rather than inventing one.
//
// The zero value is INVALID (ErrRetentionClockGuardUnspecified), so the unguarded write can only
// be reached by naming it.
type RetentionClockGuard string

// RetentionClockUnguarded explicitly opts out of the write-time authorisation check. No production
// caller uses it, and none should: every flow that arms this clock does so because of a state it
// just observed, and that state is exactly what must still hold at write time. It exists for test
// fixtures that arm the clock as SETUP for something else entirely (a deletion sweep, a repair
// pass, a cascade) and legitimately have no authorising transition to name.
const RetentionClockUnguarded RetentionClockGuard = "*unguarded*"

// RetentionClockRequiresStatus builds a guard requiring the row to still carry the given
// subscription_status when the retention-clock write executes.
func RetentionClockRequiresStatus(status domain.SubscriptionStatus) RetentionClockGuard {
	return RetentionClockGuard(status)
}

// ProfileRepository defines the interface for user profile data access
type ProfileRepository interface {
	// GetUserByClerkID retrieves a user by their Clerk user ID
	GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error)

	// GetUserByID retrieves a user by their internal UUID
	GetUserByID(ctx context.Context, id string) (*domain.User, error)

	// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
	GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error)

	// GetUserByEmail retrieves a user by email, checking both the blind index (encrypted rows)
	// and plaintext LOWER(email) (pre-encryption rows). Use this as a fallback when the
	// clerk_user_id blind index lookup fails for unmigrated rows (#1001).
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)

	// CreateUser creates a new user in the database
	CreateUser(ctx context.Context, user *domain.User) error

	// CreateUserTx creates a new user in the database within a transaction
	CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error

	// UpdateUser updates an existing user's information
	UpdateUser(ctx context.Context, user *domain.User) error

	// UpdateLastLogin updates the last_login_at timestamp for a user
	UpdateLastLogin(ctx context.Context, clerkUserID string) error

	// Paddle-related methods (Issue #45, #46: Paddle Payment Integration)

	// UpdatePaddleCustomerID links a Paddle customer ID to a user
	UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error

	// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error

	// Trial-related methods (Issue #679)

	// UpdateTrialStatus updates trial-related fields for a user
	UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error

	// GetEffectiveSubscriptionStatusByUserID retrieves the user's EFFECTIVE subscription
	// status — "active" when a non-expired manual tier override (admin comp) is in effect,
	// otherwise the raw subscription_status (Issue #1413: an admin comp must actually unlock
	// access, not just relabel the tier while every access gate keeps reading the stale,
	// untouched status). Used by read-only middleware (#930) to avoid a full user fetch on
	// every request.
	GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error)

	// Retention tracking methods (#930: 6-month read-only before auto-deletion)

	// SetRetentionDates (re)starts the GDPR retention clock for a user: retention_started_at and
	// retention_expires_at are computed entirely in Postgres — clock_timestamp() and
	// clock_timestamp() + make_interval(months => retentionMonths) — never via Go's
	// time.Now()/time.Time.AddDate, which disagrees with Postgres's make_interval on month-end
	// dates (Go normalizes month overflow forward; Postgres clamps to the target month's last day).
	// This mirrors AdminUserRepository.RemoveUserTierOverride's SQL, which already computed
	// retention_expires_at this way.
	// Callers no longer supply startedAt/expiresAt themselves — verified that neither existing
	// caller (subscription_webhook.go, trial_service.go) used those Go-computed values for anything
	// besides this one write.
	//
	// Issue #1413 round-10 correction: this used to claim "every writer of this column now shares one
	// semantics." That was FALSE while a landmine still existed: CancelIfPastDueWithRetention (the
	// non-occurrence predecessor superseded by round-4 finding H1) took a Go-computed expiry and had
	// ZERO production callers — the narrower PastDueProfileRepository interface past_due_service.go
	// actually consumes did not even declare it — so it was not live drift, but wiring it up would
	// have reintroduced the month-end over-retention bug. Issue #1442 deleted it, so the claim is
	// unqualified and accurate again: every writer of this column — SetRetentionDates (here),
	// RepairRetentionClock, CancelIfPastDueOccurrenceWithRetention, and
	// AdminUserRepository.RemoveUserTierOverride — clamps.
	//
	// TWO POPULATIONS OF THIS COLUMN EXIST, and that is a deliberate decision, not an oversight.
	// Rows written before commit 144b0e3a used Go's AddDate (month overflow normalized FORWARD); rows
	// after clamp. So a pre-existing row can carry a GDPR deletion deadline up to ~3 days LATER than a
	// new row with the same start date — only for clocks anchored on Mar 31, May 31, Aug 29/30/31,
	// Oct 31 or Dec 31 (at the configured N=6). We deliberately did NOT backfill:
	//   - every consumer (30d notice, 7d notice, GetUsersWithExpiredRetentionPage, the occurrence CAS)
	//     reads the row's OWN stored value, so an untouched row is internally consistent end to end —
	//     no notice is skipped and nobody is deleted early;
	//   - retention_notice_30d_sent_at / _7d_sent_at are SET-ONCE flags that gate re-send. ⚠ Since
	//     Issue #1569 T1 GetUsersWithExpiredRetentionPage DOES reference them
	//     (retentionNoticesDeliveredPredicate) — this bullet previously said it did not, which was
	//     accurate and was the C1 defect. The conclusion is unchanged and if anything stronger:
	//     shrinking a deadline after a notice had fired would delete an account on a SHORTER window
	//     than the email promised, with no new notice;
	//   - migrations 000119-000122 set this codebase's precedent for retroactively correcting a
	//     grace timestamp: only ever defer the harmful action, never hasten it (000120: "never a
	//     premature disconnect"), and both have deliberately no-op down migrations for that reason.
	//     A backfill here would do the opposite, for a strictly more consequential and irreversible
	//     action;
	//   - the population self-resolves: each row proceeds notice -> notice -> delete on its own stored
	//     deadline, or the user resubscribes and ClearRetentionDates nulls the field.
	//
	// Issue #1500 residual (Codex review, PR #1443 follow-up): the write is GUARDED. guard names the
	// authorising condition the caller observed, and it is re-evaluated inside this statement against
	// committed state — see RetentionClockGuard's own doc comment for why the condition is passed in
	// rather than hardcoded here. Returns ErrRetentionClockPreconditionLost when the row exists but
	// that condition no longer holds, ErrUserNotFound when there is no live row at all: two
	// deliberately distinguishable outcomes, only one of which is an error (see those errors' docs).
	SetRetentionDates(ctx context.Context, userID string, guard RetentionClockGuard, retentionMonths int) error

	// ClearRetentionDates clears all retention tracking fields (called on resubscription).
	ClearRetentionDates(ctx context.Context, userID string) error

	// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error

	// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error

	// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
	// and whose 30-day notice has not yet been sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
	// and whose 7-day notice has not yet been sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
	// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9 — backs the daily job
	// that expires lapsed admin comps).
	GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)

	// GetUsersWithActivePaddleSubscriptions returns users with a non-null paddle_subscription_id
	// and subscription_status not in ('inactive', 'cancelled')
	GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error)

	// Deletion-related methods

	// MarkForDeletion marks a user for deletion with a scheduled date
	MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error

	// CancelDeletion cancels a pending deletion request
	CancelDeletion(ctx context.Context, userID string) error

	// DeleteUserCascade performs application-level cascade deletion of all user data, but ONLY
	// after re-verifying, INSIDE the same transaction that performs the destructive statements,
	// that deletion is STILL authorised at the instant of the act — not merely at the earlier
	// ClaimForDeletion instant, which the external-cleanup window (Paddle cancel, Plaid
	// disconnect — potentially many seconds of network calls) can separate from this call by a
	// meaningful amount of time (Issue #1413 round-4 Critical finding C1).
	//
	// expectedDeletionRequestedAt must be the deletion_requested_at value the caller observed
	// for this user BEFORE calling ClaimForDeletion (nil for the retention-triggered path, which
	// never sets it). Re-verifying this EXACT value (not merely whether it is nil) is required
	// to catch the case Codex round-4 review flagged as the most serious residual in this task:
	// the authorisation predicate this method splices, deletionAuthorisationUnionPredicate, is
	// satisfied when a claim of EITHER kind would be authorised — and its RETENTION branch
	// (deletion_requested_at IS NULL plus no manual_tier_override at all) is satisfied by an
	// uncomped user who has just cancelled their own deletion request during the external-cleanup
	// window: cancelling sets deletion_requested_at to NULL, and "no override" was already true.
	// So the predicate alone is NOT sufficient here. Adding
	// `deletion_requested_at IS NOT DISTINCT FROM expectedDeletionRequestedAt` closes this: a
	// cancellation (non-null -> null) is a mismatch, and so is a same-instant cancel-then-new-
	// request (a new, later timestamp the original claim never verified) — both correctly abort,
	// leaving the user to be picked up by whichever flow's own schedule next applies to them.
	//
	// Issue #1440, T0 — this paragraph's CONCLUSION is unchanged; only its stated reason was
	// rewritten. It previously blamed `claimForDeletionPredicate` for treating "no
	// manual_tier_override at all" as sufficient authorization REGARDLESS of deletion_requested_at.
	// That constant no longer exists, and that rule is now false of BOTH of its replacements: the
	// retention predicate requires deletion_requested_at IS NULL, and the user-requested predicate
	// has no override clause at all. The exact-value check is still required, for the reason stated
	// above.
	//
	// Also refuses to proceed while any of the user's linked_items rows still need Plaid-side
	// cleanup (uncleanedLinkedItemExistsClause — `plaid_removed_at IS NULL` since Issue #1495; it
	// tested status until then, which trusted 'replaced' and so let this very outcome happen —
	// see that const's own comment) — Issue #1413 round-4 Critical finding C2: deleting the
	// local linked_items rows (Step 3 below) while any remain un-cleaned would orphan a live,
	// still-billing Plaid access token with no local record it ever existed. See
	// plaid_item_cleanup_service.go's cleanupItems for the companion fix (re-reading the item
	// list INSIDE the side-effect claim, immediately before cleanup, instead of relying on a
	// snapshot taken outside it) that narrows how an item could reach this check un-cleaned in
	// the first place.
	//
	// Returns (true, affectedOwnerIDs, nil) if the cascade ran and the user was deleted.
	// affectedOwnerIDs is the deduplicated set of OTHER (surviving) users whose transactions rows
	// this cascade rewrote in place — currently, the dangling cross-user custom-category-override
	// clear (see that UPDATE's own comment, GDPR round-6 finding P1). The caller MUST bump each
	// affected owner's cache scope AFTER this call returns (true, ...) — never before, since the
	// erasure this slice describes is only durable once this method has returned — or that owner's
	// warm analytics cache keeps serving the just-deleted user's data (e.g. their chosen custom
	// category name) until the cache namespace's TTL elapses.
	//
	// Returns (false, nil, nil) — a NORMAL, EXPECTED outcome, NOT an error — when either guard
	// fails: an admin granted/extended a comp, the user cancelled their own deletion request, or
	// un-cleaned Plaid items remain. The caller MUST release its work claim (never Complete) on a
	// (false, nil, nil) result and let the next scheduled run re-evaluate from scratch — it must
	// NOT treat this as an error.
	DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error)

	// RecordDeletionResidue writes the durable, queryable record that an account deletion performed
	// an IRREVERSIBLE EXTERNAL EFFECT and then did not complete — so the user still exists, and
	// something has been done to them that they did not ask for. Issue #1504.
	//
	// ⚠ ROUND 15 CHANGED WHEN THIS IS CALLED, AND THEREFORE WHAT A ROW MEANS WHILE A DELETION IS
	// STILL RUNNING. The caller now writes one record per irreversible external call, immediately
	// BEFORE issuing it, instead of inferring the whole thing from an in-memory flag once the run is
	// over. An end-of-run inference cannot describe a call whose acknowledgement was lost, a panic,
	// or a process that was killed mid-flight — and the flag it used only ever named the subscription
	// cancel, so the customer archive could not be recorded at all.
	//
	// The row therefore asserts "this call was ISSUED and the user is still here", which is the
	// strongest honest claim available from this side of a network boundary. It is not weaker in
	// practice: what an operator does with it — check Paddle, then reconcile — is the same either way.
	//
	// ⚠ THE LIFECYCLE IS NOW WRITE-THEN-RETRACT, and the retraction is not optional. DeleteUserCascade
	// DELETEs this user's rows in this namespace inside the same transaction that deletes the user
	// (see its own comment at that statement). A deletion that COMPLETES therefore leaves none, which
	// is what keeps the meaning above true and keeps both Prometheus gauges quiet on ordinary
	// deletions. Any future caller of this method must either be inside a flow that completes that
	// way, or own its own retraction.
	//
	// THE CASE IT EXISTS FOR is irreducible and cannot be engineered away: ExecuteDeletion gates
	// every irreversible external call on a fresh authorisation read, but if the user revokes their
	// deletion WHILE the Paddle subscription-cancel call is in flight, the cancel still lands. The
	// gate passed a moment earlier and a network call cannot be recalled. The user keeps their
	// account and every row of their data — with their subscription cancelled. Before this, the only
	// trace was an Info log line: nobody was told and nothing was queryable.
	//
	// It records; it does NOT remediate. Re-subscribing someone automatically, from a flow that has
	// just proven it does not know what state the world is in, is a worse failure than telling a
	// human. An operator finds these with:
	//
	//	SELECT resource_id, created_at FROM audit_logs
	//	 WHERE action LIKE 'user.deletion_residue.%' ORDER BY created_at DESC;
	//
	// WHY audit_logs AND NOT A NEW TABLE, and why keyed to the USER. plaid_orphaned_items is the
	// precedent for the OPPOSITE case — a record that must outlive an ERASED subject, which is why
	// it carries no user_id at all. This record is not that: its defining fact is that the deletion
	// did NOT complete, so the users row is still there and the operator's entire task is to find
	// that person. Keying it to the user is therefore the point, not a leak, and it means
	// DeleteUserCascade's erasure redaction (`SET changes = NULL WHERE resource_id = $1 OR
	// user_id = $1`) covers it if that user IS erased later — harmlessly, because by then the
	// cancellation this record reports has become the intended outcome. audit_logs also already
	// exists, is already erasure-aware, and needs no migration.
	//
	// ⚠ NOTHING IS WRITTEN TO `changes`. The effect is encoded in `action` — the column that is
	// queryable and that redaction does not touch — and the subscription id an operator needs is
	// still on the surviving users row, precisely because this record only exists when the user
	// survived. A payload here would be PII that redaction would null out anyway, i.e. exactly the
	// trap that disqualified audit_logs for plaid_orphaned_items.
	//
	// audit_logs.user_id is the ACTING party and is left NULL: this is a system action, matching the
	// column's own documented convention (migration 000003).
	RecordDeletionResidue(ctx context.Context, userID string, effect DeletionResidueEffect) error

	// CountUncleanedLinkedItems counts the user's linked_items rows that DeleteUserCascade's C2
	// guard would refuse to proceed past — those with neither a recorded Plaid removal nor a
	// recorded unrecoverable removal failure. Shares the guard's own predicate
	// (uncleanedLinkedItemPredicate) so the two can never disagree. Issue #1494.
	//
	// EXISTS FOR DIAGNOSIS, NOT FOR CONTROL FLOW AROUND THE GUARD. Its one caller is
	// DeletionService.ExecuteDeletion's Plaid-less branch: when plaidCleanupSvc is nil (a legitimate
	// deployment — main.go wires it only when Plaid credentials are configured), nothing can ever
	// record a removal, so a user WITH such items can never be erased and the cascade would return
	// (false, nil) with no statement of why, three layers from the real cause. The count turns that
	// into a named error that says how many items and what is missing. A user with ZERO such items is
	// unaffected and proceeds normally, which is why this counts the guard's exact population rather
	// than "has any linked item at all": refusing on an already-cleaned item would block deletions the
	// guard itself would have allowed.
	//
	// Do NOT use this as a pre-check to decide whether to call DeleteUserCascade: it runs outside the
	// cascade's transaction and its FOR UPDATE lock, so its answer can be stale by the time the
	// cascade runs. The guard inside the transaction is the authority; this is an explanation.
	//
	// ⚠ ROUND 15, D1 — THE SECOND CALLER, AND WHY IT DOES NOT VIOLATE THE PARAGRAPH ABOVE.
	// ExecuteDeletion's ErrPlaidRemovalUnrecoverable branch now consults this count too, to decide
	// whether to SKIP the irreversible Paddle work — NOT to decide whether to call the cascade. The
	// cascade is still called and its C2 guard is still the authority. Staleness is therefore
	// one-directional and harmless in both directions: a stale non-zero defers a run by one day (the
	// sweep is daily and the next pass re-reads), and a stale zero merely reproduces the behaviour
	// this branch had before the check existed. Neither can produce an erasure C2 would have refused,
	// because C2 is untouched. See that call site for the full argument.
	CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error)

	// HouseholdOwnerBlocksDeletion reports whether erasing userID would destroy a DIFFERENT user's
	// data through households.owner_user_id's CASCADE — the exact question DeleteUserCascade's own
	// in-transaction household guard asks, asked EARLY. Issue #1440 follow-up, round 15 finding D2.
	//
	// WHY IT EXISTS SEPARATELY FROM THE CASCADE'S GUARD, AND WHY THAT GUARD STAYS. The cascade's
	// guard is evaluated inside the deletion transaction, i.e. AFTER ExecuteDeletion has already
	// disconnected every Plaid Item and cancelled and archived the user's Paddle customer. Its own
	// doc comment records that this condition — unlike every other skip in that function — CANNOT
	// SELF-HEAL: nothing in the system will transfer the ownership, remove the member, or reconcile
	// the stale entitlement. So a retention-expired household owner had their banks disconnected and
	// their Paddle customer irreversibly archived, was then refused, and the next daily sweep
	// repeated the whole sequence — forever.
	//
	// This is a pure LOCAL-STATE precondition. It needs no external call and no lock, so it can be
	// asked before the first irreversible effect, which removes the entire class rather than
	// reporting it. It is a REORDER, not a new rule: the predicate is literally the same SQL const.
	//
	// ⚠ DEFENCE IN DEPTH — DO NOT REMOVE THE IN-CASCADE GUARD ON THE STRENGTH OF THIS ONE. This
	// query runs outside the cascade's transaction and its FOR UPDATE lock, so an invite accepted
	// between this call and the cascade is caught only by the guard inside it. This check shrinks the
	// window in which the harm is IRREVERSIBLE; the in-transaction guard is what makes the outcome
	// correct.
	HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error)

	// DeletionStillAuthorised reports whether a hard delete of userID is STILL authorised under
	// `kind`, judged against committed state at the moment it is asked, for the deletion episode
	// pinned by expectedDeletionRequestedAt.
	//
	// WHY THIS EXISTS SEPARATELY FROM ClaimForDeletion AND FROM DeleteUserCascade'S OWN GUARD.
	// ExecuteDeletion wins the claim, then performs Paddle and Plaid cleanup — several external
	// network calls, one per linked item on the Plaid side — and only afterwards reaches the
	// cascade's in-transaction re-check. CancelDeletion can clear deletion_requested_at and
	// deletion_scheduled_at anywhere inside that window without coordinating with the work claim
	// (the claim gives EXCLUSIVITY between flows; it says nothing about whether this flow's own
	// authorisation still holds). The cascade's guard then correctly refuses to delete — but by
	// then the subscription is cancelled, the Paddle customer is archived and anonymised, and the
	// bank connections are gone, for a user who explicitly asked to keep their account. This method
	// is what lets each irreversible external call be preceded by a fresh answer instead of a stale
	// snapshot.
	//
	// KIND-SPECIFIC, deliberately, where the cascade's guard uses the union of both kinds. The
	// cascade is not told which kind authorised the run and compensates with the occurrence pin;
	// ExecuteDeletion IS told, so asking the union here would re-open exactly the substitution
	// #1440 T0 closed at claim time — the retention rule authorises any uncomped user with no
	// outstanding request, which is precisely what a just-cancelled user looks like.
	//
	// READ-ONLY, and NOT a substitute for either write-side guard. It takes no lock, so its answer
	// can be stale the instant it returns; the claim and the cascade's FOR UPDATE re-check remain
	// the authorities. What it buys is that the WINDOW an irreversible external call is exposed to
	// shrinks from "the whole cleanup sequence" to "one call".
	//
	// A false return is a legitimate, expected outcome — not an error. An ERROR means authorisation
	// could not be established, which callers must treat as fail-closed: not knowing is not
	// permission to proceed.
	DeletionStillAuthorised(ctx context.Context, userID string, kind DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error)

	// Dunning: past_due tracking methods (Issue #898)

	// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
	// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
	SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error

	// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user (called on resubscription).
	ClearPastDueStartedAt(ctx context.Context, userID string) error

	// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
	// Used by the dunning cron job to find candidates for grace-expiry processing (Issue #898).
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)

	// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the
	// user is still 'trial' AND carries no currently-active manual tier override. Returns
	// (true, nil) if the flip happened, (false, nil) if an admin granted/extended a comp between
	// the caller's earlier HasActiveManualOverride() read and this call, or the user's status has
	// otherwise already moved on (Issue #1413 High finding H5, Codex review round 3 — the earlier
	// read-only check cannot, by itself, protect the write against a comp granted in that window).
	ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error)

	// ClaimForDeletion atomically verifies, at the moment of the claim, that a user is still
	// eligible for ExecuteDeletion's irreversible external cleanup (Paddle cancel, Plaid
	// disconnect) and cascade delete, UNDER THE AUTHORISATION `kind` NAMES:
	//
	//   - DeletionKindUserRequested: not already deleted, deletion_requested_at still set, and
	//     deletion_scheduled_at already elapsed. No override check — the ratified rule is that a
	//     user's own deletion request beats an admin comp.
	//   - DeletionKindRetention: not already deleted, no deletion request outstanding, and no
	//     manual tier override at all (live or lapsed-but-uncleared).
	//
	// Returns (true, nil) if the claim succeeded (safe to proceed with the destructive work), and
	// (false, nil) — a legitimate skip, NOT an error — whenever the authorisation no longer holds:
	// an admin granted/extended a comp on a retention candidate, or (Issue #1440, T0) the user
	// cancelled or re-scheduled the deletion request this caller was queued for. An unrecognised
	// kind is a genuine error and never a claim: this fails closed.
	//
	// Callers MUST pass the kind that matches the query which selected the user, and must never
	// derive it from the user's current row state — that state is precisely what a stale caller
	// cannot trust. See the implementation's doc comment for the enumeration of production callers
	// and their kinds.
	//
	// Ordering note for callers: this claim must run BEFORE any external cleanup, not folded
	// into DeleteUserCascade's own final write — Plaid/Paddle IDs must still be readable when
	// the external calls run, so the claim has to come first as its own step, or the external
	// work would already be irreversibly done by the time a later guard could catch a comp
	// granted in the race window.
	ClaimForDeletion(ctx context.Context, userID string, kind DeletionClaimKind) (bool, error)

	// CountRetentionDeletionsWithheld reports how many accounts the Issue #1569 T1 notice gate is
	// holding back from hard deletion, split into notice-pending (healthy: the clock is still
	// running) and withheld (the clock has run out, so no notice query can ever reach the user
	// again). See the implementation for the full rationale.
	CountRetentionDeletionsWithheld(ctx context.Context) (RetentionWithholdingCounts, error)

	// ⚠ GetRetentionDeletionsWithheldPage — the ENUMERATING sibling of the method above (Issue
	// #1584) — is DELIBERATELY NOT ON THIS INTERFACE, for the reason NewProfileRepository's own doc
	// comment gives about the occurrence-scoped methods #1413 P6 added: it has exactly one
	// consumer, operations.RetentionWithheldReader, which declares the two methods it needs and is
	// satisfied structurally by the concrete *profileRepository. Adding it here would ripple a stub
	// into every unrelated service's test double that implements this interface in full, buying
	// nothing — no caller wants this method through this interface.

	// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
	// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
	UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error

	// GetUsersNeedingRetentionClockRepair returns users who are read-only-status
	// (inactive/cancelled) but have NO retention clock started (retention_expires_at IS NULL) —
	// Issue #1413 H5 residual, belt-and-braces repair path. CancelIfPastDueOccurrenceWithRetention
	// closes the specific race on the past-due grace-cancel path going forward, but this query also
	// catches: (a) any row already stuck in this state from BEFORE that fix existed, and (b) any
	// other, currently-unknown path that might leave a user cancelled with no clock. Mirrors the
	// other retention queries' predicates (subscription_status IN ('inactive', 'cancelled'),
	// not deleted, no outstanding GDPR deletion request, no manual_tier_override at all — live or
	// lapsed) so a comped or already-deletion-pending user is never touched.
	GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error)

	// RepairRetentionClock sets the retention clock for a single user, but ONLY if
	// retention_expires_at is still NULL and the user is still read-only-status at the moment of
	// the write — a conditional UPDATE (not a claim) is sufficient here because the write is
	// purely additive and idempotent: once retention_expires_at is set, the WHERE clause itself
	// prevents any concurrent repair attempt from clobbering it, unlike the destructive external
	// side effects work_claims exists to guard. Returns (true, nil) if the clock was set, (false,
	// nil) if the user no longer matches (already repaired by a concurrent run, comp granted,
	// deletion requested, or status changed) since the candidate query ran — a legitimate skip,
	// not an error. retention_expires_at is computed in Postgres via clock_timestamp() +
	// make_interval(months => retentionMonths) — see the implementation's own doc comment
	// (Task NA2, Issue #1413 follow-up) for why this repair write must use the SAME arithmetic
	// semantics as the writers it backstops.
	RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error)

	// GetUsersNeedingPlaidDisconnectRepair returns users who are read-only-status
	// (inactive/cancelled) but still have at least one LIVE linked item — status NOT
	// 'disconnected'/'replaced', where a NULL status counts as live too (linked_items.status is
	// nullable; see migration 000013 — SQL's three-valued logic makes a bare `NOT IN (...)`
	// silently exclude a NULL-status row, which would be exactly the leak this pass exists to
	// prevent, so the predicate explicitly treats NULL as live) — AND whose plaid_cleanup_pending_at
	// marker is older than gracePeriod.
	//
	// Issue #1413 round-11 correction: this doc comment previously (incorrectly) described
	// retention_started_at as the grace anchor. That was true only through round-9; round-10
	// replaced it with the explicit plaid_cleanup_pending_at marker (see
	// domain.User.PlaidCleanupPendingAt's doc comment for the invariant this column maintains, and
	// this method's own implementation doc comment below for the full round-10 rationale for why
	// retention_started_at could not be made sound as this anchor). retention_started_at plays NO
	// role in this query at all now — do not reintroduce it here.
	//
	// Issue #1413 round-7 (Codex review): this is the ratified DEFER design, not an ordering fix.
	// TrialService.ExpireTrial no longer calls Plaid at all — no ordering of a status flip and an
	// irreversible Plaid call can be safe, because /subscription/checkout is deliberately
	// whitelisted for read-only users, so becoming read-only never stops a user from becoming
	// active again. Disconnecting Plaid is deferred entirely to this repair pass
	// (RetentionCleanupCheckWorker.repairPlaidDisconnects), which only selects a user once
	// gracePeriod has elapsed since plaid_cleanup_pending_at was set (atomically alongside the SAME
	// status flip that makes the user Plaid-ineligible per the invariant) — giving a genuine
	// resubscriber a window to become active again before Plaid is ever touched, at which point
	// they simply drop out of this query (their marker is cleared, per the invariant, the moment
	// they become Plaid-eligible again). A user who resubscribes WITHIN the grace window is
	// therefore never disconnected at all, not merely disconnected after a narrower window.
	//
	// Because this predicate keys on subscription_status + plaid_cleanup_pending_at (not on which
	// flow set them), it also catches a failed/never-ran disconnect from the cancellation and
	// past-due paths, not just trial expiry — every SET site listed in
	// domain.User.PlaidCleanupPendingAt's doc comment stamps this marker at the same moment it
	// makes the user Plaid-ineligible, so any read-only user with a live item past the grace window
	// is covered uniformly regardless of which writer put them there.
	//
	// Predicate otherwise mirrors GetUsersNeedingRetentionClockRepair's shape (subscription_status
	// IN ('inactive', 'cancelled'), not deleted, no outstanding GDPR deletion request) with ONE
	// deliberate divergence: override exclusion uses noActiveManualOverridePredicate ("live right
	// now"), NOT noManualOverrideAtAllPredicate ("live or lapsed") — Issue #1413 round-12 Fix 2. A
	// lapsed-but-uncleared override must NOT permanently exempt a user from this repair pass the way
	// it deliberately does for retention/deletion: deletion is irreversible so that query fails safe
	// by staying excluded indefinitely if the expiry job stalls, but a Plaid disconnect is
	// RECOVERABLE (the user relinks), so the correct fail-safe direction here is the opposite — see
	// this method's implementation doc comment (profile_repository.go) for the full reasoning.
	GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error)
}

// profileRepository implements ProfileRepository using PostgreSQL
type profileRepository struct {
	db                    DB
	enc                   *appcrypto.FieldEncryptor
	overrideTierValidator func(tierName string) bool
	postHogProjectID      string
	postHogRegion         string
	// auditLog writes DeleteUserCascade's terminal `user.deleted` entry inside the cascade's own
	// transaction (Issue #1507). Held as the interface rather than reaching for a package-level
	// helper so the write goes through the one documented entry point — LogInTx — whose doc comment
	// carries the #1506 warning a future implementer must not be able to miss.
	auditLog AuditLogRepository
}

// SetPostHogErasureTarget records which isolated PostHog project an erasure obligation belongs to.
// Missing configuration is represented explicitly as "unconfigured" so local GDPR deletion can
// still commit while the durable row remains visibly pending for an operator.
func (r *profileRepository) SetPostHogErasureTarget(projectID, region string) {
	if strings.TrimSpace(projectID) == "" {
		projectID = "unconfigured"
	}
	if strings.TrimSpace(region) == "" {
		region = "unconfigured"
	}
	r.postHogProjectID = projectID
	r.postHogRegion = region
}

// SetOverrideTierValidator injects a function that reports whether a manual_tier_override value
// names a real, currently-configured tier. GetEffectiveSubscriptionStatusByUserID uses it to
// deny "active" for a malformed override (Issue #1413 G2) — see that method's doc comment.
//
// Setter injection (matching this codebase's existing SetAnalytics() convention — see
// CLAUDE.md's Analytics Instrumentation section) rather than a NewProfileRepository parameter:
// NewProfileRepository has ~70 call sites across this codebase's tests, and adding a required
// constructor parameter would force every one of them to thread a tier validator through even
// when the test never touches GetEffectiveSubscriptionStatusByUserID.
//
// Nil-safe: if this is never called, GetEffectiveSubscriptionStatusByUserID skips validation
// entirely rather than denying every comp — see that method's doc comment for why "no validator"
// is deliberately treated as permissive, not fail-closed.
func (r *profileRepository) SetOverrideTierValidator(validator func(tierName string) bool) {
	r.overrideTierValidator = validator
}

// NewProfileRepository creates a new ProfileRepository instance.
//
// Returns the concrete *profileRepository type (not the ProfileRepository interface) — Issue
// #1413 round-4 finding P6. profileRepository grew occurrence-scoped sibling methods
// (ClearPastDueStartedAtIfMatches, CancelIfPastDueOccurrenceWithRetention,
// IsRetentionNotice30dEligibleForOccurrence/7d) that are needed only by past_due_service.go's and
// retention_cleanup_check.go's own narrow consumer interfaces, not by the broad canonical
// ProfileRepository interface every other service's test double also implements. Every existing
// call site (main.go, every _test.go in this package) already assigns via `:=` or passes the
// value straight into a function parameter, so this is a compile-compatible widening: the
// concrete type structurally satisfies ProfileRepository and every narrower consumer interface it
// is passed to. Returning the interface here would force EITHER the new methods onto the
// canonical interface (rippling boilerplate mock stubs across every unrelated service's test file
// that implements it in full) OR main.go to carry a second, separately-typed handle to the same
// repository — this return-type change avoids both at the cost of a one-line signature edit.
// Precedent: NewMerchantRepository, NewMerchantOverrideRepository, NewSystemHealthRepository, and
// NewDeadLetterJobRepository already return their unexported concrete pointer types in this same
// package.
// Compile-time proof that the concrete type satisfies the wide interface. This constructor has always
// returned the concrete type, so it never had the compiler's implicit check; added alongside the
// matching assertion in linked_item_repository.go (Issue #1469) so both repositories are covered.
var _ ProfileRepository = (*profileRepository)(nil)

func NewProfileRepository(db DB, enc *appcrypto.FieldEncryptor) *profileRepository {
	// auditLog is constructed unconditionally and from the same handle: DeleteUserCascade only ever
	// calls LogInTx, which writes through the transaction it is given and never touches this
	// receiver's db, so there is no configuration for a caller to get wrong or forget.
	return &profileRepository{db: db, enc: enc, auditLog: NewAuditLogRepository(db), postHogProjectID: "unconfigured", postHogRegion: "unconfigured"}
}

const userSelectQuery = `
	SELECT id, clerk_user_id, email, email_verified,
	       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
	       trial_started_at, trial_ends_at, had_trial,
	       paddle_customer_id, paddle_subscription_id, primary_region,
	       is_active, is_deleted,
	       deletion_requested_at, deletion_scheduled_at,
	       created_at, updated_at, last_login_at,
	       manual_tier_override, manual_tier_ends_at,
	       retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at,
	       timezone, timezone_source,
	       past_due_started_at,
	       plaid_cleanup_pending_at
	FROM users`

// queryUser is a helper that scans a user from a query row
func (r *profileRepository) queryUser(ctx context.Context, query string, args ...interface{}) (*domain.User, error) {
	var user domain.User
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
		&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
		&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
		&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
		&user.IsActive, &user.IsDeleted,
		&user.DeletionRequestedAt, &user.DeletionScheduledAt,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
		&user.ManualTierOverride, &user.ManualTierEndsAt,
		&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
		&user.Timezone, &user.TimezoneSource,
		&user.PastDueStartedAt,
		&user.PlaidCleanupPendingAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}
	user.ClerkUserID = decryptedClerkUserID

	decryptedEmail, err := r.enc.DecryptString(user.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	user.Email = decryptedEmail

	decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
	}
	user.PaddleCustomerID = decryptedPaddleCustomerID

	decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
	}
	user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

	return &user, nil
}

// queryUsers is a helper that scans multiple users from a query
func (r *profileRepository) queryUsers(ctx context.Context, query string, args ...interface{}) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
			&user.PlaidCleanupPendingAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, &user)
	}
	return users, rows.Err()
}

// GetUserByClerkID retrieves a user by their Clerk user ID
func (r *profileRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(clerkUserID)
	return r.queryUser(ctx, userSelectQuery+" WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByID retrieves a user by their internal UUID
func (r *profileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return r.queryUser(ctx, userSelectQuery+" WHERE id = $1 AND is_deleted = FALSE", id)
}

// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
func (r *profileRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(subscriptionID)
	return r.queryUser(ctx, userSelectQuery+" WHERE paddle_subscription_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByEmail retrieves a user by email using a dual lookup:
// 1. By blind index (for encrypted rows post-migration)
// 2. By LOWER(email) plaintext (for unmigrated rows from before encryption)
// This prevents duplicate accounts when encrypt-migrate hasn't been run yet (#1001).
func (r *profileRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	normalized := appcrypto.NormalizeEmailForBlindIndex(email)
	return r.queryUser(ctx,
		userSelectQuery+" WHERE (email_blind_idx = $1 OR LOWER(email) = $2) AND is_deleted = FALSE",
		r.enc.BlindIndex(normalized), normalized)
}

// CreateUser creates a new user in the database
func (r *profileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	// Generate UUID if not already set
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = r.db.Exec(ctx, query,
		user.ID,
		encryptedClerkUserID,
		encryptedEmail,
		user.EmailVerified,
		user.SubscriptionTier,
		user.SubscriptionStatus,
		user.SubscriptionStartedAt,
		user.SubscriptionEndsAt,
		user.TrialStartedAt,
		user.TrialEndsAt,
		user.HadTrial,
		encryptedPaddleCustomerID,
		encryptedPaddleSubscriptionID,
		user.PrimaryRegion,
		user.IsActive,
		user.IsDeleted,
		user.DeletionRequestedAt,
		user.DeletionScheduledAt,
		user.CreatedAt,
		user.UpdatedAt,
		user.LastLoginAt,
		emailBlindIdx,
		clerkUserIDBlindIdx,
		paddleSubscriptionIDBlindIdx,
	)

	return r.checkDuplicateError(err)
}

// checkDuplicateError maps database constraint errors to domain errors
func (r *profileRepository) checkDuplicateError(err error) error {
	if err == nil {
		return nil
	}
	if (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")) &&
		(strings.Contains(err.Error(), "unique_users_email_blind") || strings.Contains(err.Error(), "email")) {
		return ErrDuplicateEmail
	}
	return err
}

// insertPrefixIndicesTx inserts prefix blind indices for a user in a single multi-row statement.
// ON CONFLICT DO NOTHING ensures idempotency — safe to call even if rows already exist.
// Returns immediately (no-op) when blindIndices is empty.
func (r *profileRepository) insertPrefixIndicesTx(
	ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
	if len(blindIndices) == 0 {
		return nil
	}
	// Single multi-row INSERT avoids per-row round trips and pgx.SendBatch footguns.
	// Parameterised as: INSERT INTO ... VALUES ($1, $2), ($1, $3), ... ON CONFLICT DO NOTHING
	// $1 is reused for user_id across all rows; $2..$N are the prefix blind indices.
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}

// CreateUserTx creates a new user in the database within a transaction
func (r *profileRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = tx.Exec(ctx, query,
		user.ID, encryptedClerkUserID, encryptedEmail, user.EmailVerified,
		user.SubscriptionTier, user.SubscriptionStatus, user.SubscriptionStartedAt, user.SubscriptionEndsAt,
		user.TrialStartedAt, user.TrialEndsAt, user.HadTrial,
		encryptedPaddleCustomerID, encryptedPaddleSubscriptionID, user.PrimaryRegion, user.IsActive, user.IsDeleted,
		user.DeletionRequestedAt, user.DeletionScheduledAt,
		user.CreatedAt, user.UpdatedAt, user.LastLoginAt,
		emailBlindIdx, clerkUserIDBlindIdx, paddleSubscriptionIDBlindIdx,
	)
	if err := r.checkDuplicateError(err); err != nil {
		return err
	}

	// Insert prefix blind indices so admin starts-with search works (#1178)
	indices := r.enc.EmailPrefixBlindIndices(user.Email)
	return r.insertPrefixIndicesTx(ctx, tx, user.ID, indices)
}

// UpdateUser updates an existing user's information
// Only updates allowed fields: email, email_verified, primary_region, timezone, timezone_source
// Subscription fields are managed separately through payment webhooks
func (r *profileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now()

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	query := `
		UPDATE users
		SET email = $1,
		    email_verified = $2,
		    primary_region = $3,
		    updated_at = $4,
		    email_blind_idx = $5,
		    timezone = $6,
		    timezone_source = $7
		WHERE id = $8
	`

	ct, err := r.db.Exec(ctx, query,
		encryptedEmail, user.EmailVerified, user.PrimaryRegion, user.UpdatedAt, emailBlindIdx,
		user.Timezone, user.TimezoneSource, user.ID,
	)

	if err != nil {
		return r.checkDuplicateError(err)
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
func (r *profileRepository) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	encryptedEmail, err := r.enc.EncryptString(plainEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(plainEmail))
	now := time.Now()

	query := `
		UPDATE users
		SET email = $1,
		    email_blind_idx = $2,
		    email_verified = false,
		    updated_at = $3
		WHERE id = $4
	`
	ct, err := tx.Exec(ctx, query, encryptedEmail, emailBlindIdx, now, userID)
	if err != nil {
		return r.checkDuplicateError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	// Delete stale prefix rows, then insert rows for the new email.
	// CRITICAL: This code appends AFTER the UPDATE users above, maintaining the
	// lock-ordering invariant (users row locked first → prefix rows second),
	// which prevents deadlocks with the startup backfill.
	if _, err := tx.Exec(ctx,
		"DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("delete old prefix indices: %w", err)
	}
	indices := r.enc.EmailPrefixBlindIndices(plainEmail)
	return r.insertPrefixIndicesTx(ctx, tx, userID, indices)
}

// UpdateLastLogin updates the last_login_at timestamp for a user
func (r *profileRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	now := time.Now()

	blindIdx := r.enc.BlindIndex(clerkUserID)

	query := `
		UPDATE users
		SET last_login_at = $1,
		    updated_at = $2
		WHERE clerk_user_id_blind_idx = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, blindIdx)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdatePaddleCustomerID links a Paddle customer ID to a user
// Issue #45: Paddle Payment Service Implementation
func (r *profileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	now := time.Now()

	encryptedPaddleCustomerID, err := r.enc.EncryptString(paddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}

	query := `
		UPDATE users
		SET paddle_customer_id = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, encryptedPaddleCustomerID, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks.
// Applies status normalization (#933) as a defense-in-depth backstop before DB write.
// When subscriptionID is nil, preserves existing values (not included in SET clause).
func (r *profileRepository) UpdateSubscriptionFromPaddle(
	ctx context.Context,
	userID, tier, status string,
	startedAt, endsAt *time.Time,
	subscriptionID *string,
) error {
	// #933: Normalize status at repo layer as defense-in-depth backstop.
	status = domain.NormalizeSubscriptionStatus(status)

	// CI-observed clock-skew fix: through the fix below, `updated_at` AND the
	// plaid_cleanup_pending_at COALESCE both used a single Go-computed `now := time.Now()` passed as
	// a query parameter — the ONE writer of this column that did not use a Postgres-native clock,
	// unlike every sibling writer (CancelIfPastDueOccurrenceWithRetention, ExpireTrialIfNoOverride,
	// UpdateTrialStatus below, removeUserTierOverrideUpdate). That inconsistency is exactly what
	// failed TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker in CI: the
	// test fences a "before" bound via `SELECT NOW()` (correctly, per this file's own established
	// convention) and then asserts this method's freshly-stamped marker is not before that bound — but
	// the marker's ACTUAL value came from the test RUNNER's clock (this method's Go `now`), not
	// Postgres's, so a Postgres container clock lagging the runner's could make the fenced comparison
	// fail even though nothing was actually wrong. Writing a Postgres clock function directly in the
	// SQL below, like every other writer already does, removes the runner's clock from this column's
	// value entirely — the stamped timestamp and every test fence built from `SELECT NOW()` now share
	// one clock family, always.
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this method's own
	// plaid_cleanup_pending_at COALESCE for the SAME transaction-start-vs-statement-execution risk
	// StampMissingPlaidCleanupMarkers's own doc comment describes in full: this is a single,
	// user-id-scoped autocommit UPDATE, and a row a concurrent transaction already holds locked (e.g.
	// an admin's RemoveUserTierOverride call, mid-transaction, on the SAME row) could in principle
	// make this statement block and then still write a pre-wait NOW(). At the time, this was left as
	// NOW(), NOT clock_timestamp(), deliberately: a bare NOW()->clock_timestamp() substitution in a
	// plain UPDATE does NOT actually fix this (StampMissingPlaidCleanupMarkers's doc comment records
	// the empirical proof) — PostgreSQL evaluates the SET-list target expressions against the
	// pre-lock-wait row version regardless of which clock function is named. The genuine fix requires
	// restructuring into a `WITH candidate AS (SELECT ... FOR ... UPDATE) UPDATE ... FROM candidate`
	// shape so the lock acquisition happens as its own earlier step, and was scoped out of that round
	// as a documented follow-up.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up:
	// restructured into exactly that shape, for the SAME reason and against the SAME empirical proof
	// as StampMissingPlaidCleanupMarkers's own fix (see that method's doc comment for the full
	// mechanism). `FOR NO KEY UPDATE`, not `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's own
	// Finding 1 correction for why `FOR UPDATE` here would risk the exact same cross-user deadlock
	// against a concurrent admin_user_repository.go audit-log insert (this row can equally be an
	// audit actor). The CTE selects only `id` — every SET-list expression that reads an "old" column
	// value (`subscription_status` for the eligibility check below, `plaid_cleanup_pending_at` for the
	// COALESCE) reads it via the plain, unaliased `users` table reference in the outer UPDATE, exactly
	// as StampMissingPlaidCleanupMarkers's own restructure does for `updated_at`/`plaid_cleanup_pending_at`.
	// This is deliberately NOT a theoretical claim: verified directly against a real database (not
	// reasoned about) that this target-table column reference reflects the value AS OF the moment the
	// CTE's `FOR NO KEY UPDATE` actually acquires the lock (i.e. post-wait, current committed state),
	// the same way `clock_timestamp()` does — never a value frozen at this statement's own pre-wait
	// scan. `updated_at` keeps NOW() regardless — plain ordering, never compared as an age, not at
	// risk either way.
	//
	// This method's own CAS/existence-check semantics are unchanged by the restructure: the CTE's
	// WHERE clause (`id = $N AND is_deleted = FALSE`) is EXACTLY the predicate the bare UPDATE used —
	// no condition moved, none added — so RowsAffected() still reports 0 in precisely the same
	// circumstance (missing or soft-deleted target user) this method's own ErrUserNotFound check
	// below already depends on.
	//
	// Issue #1413 round-10: maintain plaid_cleanup_pending_at atomically with THIS SAME
	// subscription_status write, rather than in a later, separate call that could independently
	// fail or drift out of sync with the status it's supposed to describe — see
	// GetUsersNeedingPlaidDisconnectRepair's doc comment for why this column, not
	// retention_started_at, is now the repair pass's sole grace-window anchor. A transition INTO
	// the repair pass's target set (inactive/cancelled) starts the marker — but only if it isn't
	// ALREADY running (COALESCE): a redelivered/duplicate webhook reporting the same terminal
	// status again must not keep pushing the anchor forward, or a steady trickle of retries could
	// indefinitely defer the grace window. A transition to any OTHER status (active, trialing —
	// and, moot in practice since processSubscriptionPastDue only ever reaches past_due from
	// active/trialing, past_due too) clears it: that status is not one this repair pass targets,
	// so nothing can legitimately be "pending" against it.
	//
	// Issue #1413 round-12 Fix 4 (Medium): the eligible branch's CASE gates the COALESCE-start on
	// the row's OLD (pre-write) subscription_status NOT already being repair-eligible
	// (`NOT (plaidDisconnectRepairEligibleStatusPredicate)`, evaluated against the row as it stood
	// before this SET clause — Postgres resolves every right-hand-side column reference in an
	// UPDATE's SET list against the pre-update row, never a sibling assignment in the same
	// statement). This distinguishes a GENUINE transition into ineligibility (old status was
	// active/trialing/past_due — the marker legitimately starts now) from a REDUNDANT terminal
	// event for a user ALREADY ineligible (old status was already inactive/cancelled — e.g. a
	// second, distinct cancellation webhook, or subscription.updated re-reporting the same terminal
	// status). The bug this closes: after repairPlaidDisconnects completes cleanup and clears the
	// marker (ClearPlaidCleanupPendingIfMatches), a LATER, DISTINCT terminal webhook for the SAME
	// still-cancelled episode used to reach the old bare `COALESCE(plaid_cleanup_pending_at, $N)` —
	// with the marker now NULL, that COALESCE fired and manufactured a BRAND NEW marker timestamped
	// AFTER the episode actually began, for cleanup that was already finished. The ELSE branch (not
	// a genuine transition) leaves plaid_cleanup_pending_at completely untouched — whatever it
	// currently is (NULL after a completed repair, or still running) is exactly what the invariant
	// says it should be, and only a real transition is licensed to change it.
	// CancelIfPastDueOccurrenceWithRetention/ExpireTrialIfNoOverride
	// need no equivalent guard: their own WHERE clauses already require the OLD status to be
	// 'past_due'/'trial' (never already repair-eligible), so every successful write there is a
	// transition into ineligibility by construction.
	//
	// Issue #1413 round-13: this eligible branch NO LONGER gates on whether the row currently
	// carries a live manual tier override — round-11 Fix 4 added exactly that gate (checking
	// hasActiveManualOverrideSQLExpr live, in this same atomic UPDATE, so a comped user's
	// cancellation would leave the marker NULL instead of starting it), and round-13's
	// investigation found it was the SAME class of bug as the round-13 P0 (admin-grant nulling the
	// marker): it tied this column's SET-time value to "is there a live override RIGHT NOW" — a
	// wall-clock-dependent fact that can flip from true to false with NO writer running. If Paddle
	// cancels a user who already carries a live comp (a real path: processSubscriptionCanceled can
	// fire for a comped user), the marker stayed NULL under the old gate; if that comp then lapsed
	// with the daily comp-expiry job degraded or never run, NOTHING would ever re-stamp the
	// marker — an identical permanent, invisible leak to the admin-grant P0, just reached via a
	// different caller ordering (comp granted BEFORE the cancellation, rather than after). Per the
	// restated invariant (domain.User.PlaidCleanupPendingAt's doc comment), this column tracks only
	// the durable, override-independent fact "a raw-status ineligibility episode began, cleanup
	// outstanding" — so a genuine transition into inactive/cancelled now ALWAYS starts/preserves
	// the marker, comped or not. CURRENT eligibility (raw status AND live override) is evaluated
	// separately, entirely at query time, by GetUsersNeedingPlaidDisconnectRepair's own
	// noActiveManualOverridePredicate — which still correctly excludes a user with a currently-live
	// comp from the repair pass, marker or no marker.
	plaidMarkerEligible := domain.SubscriptionStatus(status).IsEligibleForPlaidDisconnectRepair()

	var query string
	var args []interface{}

	if subscriptionID != nil {
		// Overwrite both paddle_subscription_id columns.
		enc, err := r.enc.EncryptString(*subscriptionID)
		if err != nil {
			return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
		}
		blind := r.enc.BlindIndex(*subscriptionID)
		if plaidMarkerEligible {
			query = `WITH candidate AS (SELECT id FROM users WHERE id = $7 AND is_deleted = FALSE FOR NO KEY UPDATE) UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END FROM candidate WHERE users.id = candidate.id`
		} else {
			query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $7 AND is_deleted = FALSE`
		}
		args = []interface{}{tier, status, startedAt, endsAt, enc, blind, userID}
	} else {
		// Preserve existing paddle_subscription_id values (omit from SET clause).
		if plaidMarkerEligible {
			query = `WITH candidate AS (SELECT id FROM users WHERE id = $5 AND is_deleted = FALSE FOR NO KEY UPDATE) UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END FROM candidate WHERE users.id = candidate.id`
		} else {
			query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $5 AND is_deleted = FALSE`
		}
		args = []interface{}{tier, status, startedAt, endsAt, userID}
	}

	ct, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// MarkForDeletion marks a user for deletion with a scheduled date
func (r *profileRepository) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = $1,
		    deletion_scheduled_at = $2,
		    updated_at = $3
		WHERE id = $4 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, scheduledAt, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// CancelDeletion cancels a pending deletion request.
//
// Issue #1413 Task JA (live production defect, PR #1443 round-7 gap analysis, highest severity):
// also sets plaid_cleanup_pending_at = NULL. GetUsersNeedingPlaidDisconnectRepair,
// StampMissingPlaidCleanupMarkers, and ClearCompletedPlaidCleanupMarkers (all three, this file) filter
// deletion_requested_at IS NULL — so while a deletion request is pending, plaid_cleanup_pending_at is
// completely frozen: no backstop can touch it, and the repair candidate query cannot see the user at
// all, no matter how stale the marker gets. Before this fix, CancelDeletion cleared only
// deletion_requested_at/deletion_scheduled_at, making the user visible to the candidate query again
// with whatever marker value had been frozen for however long the deletion request sat open — for a
// lapsed user with a live linked item, that marker is typically already far past the 72-hour grace
// window the instant they become visible again. The very next repairPlaidDisconnects pass (which runs
// on every process boot, RunOnStart: true — see retention_cleanup_check.go) would then disconnect
// every one of that user's bank connections with ZERO grace, at the exact moment they signalled they
// wanted to keep the account by cancelling the deletion.
//
// NULL, not NOW() (Task JA — this choice matters more than the edit itself): NULL delegates the
// decision to StampMissingPlaidCleanupMarkers, which only re-stamps a fresh NOW() if an episode is
// GENUINELY still open (ineligible status AND a live linked item AND no active comp — its own doc
// comment, this file). Writing NOW() directly here would unconditionally stamp a marker even when no
// episode is actually open (e.g. the user resubscribed while their deletion request was pending),
// manufacturing exactly the phantom-marker rows issue #1453 tracks. This is the subsystem's "two
// facts, two sources" rule, restated for this writer: this column records only that an episode BEGAN;
// StampMissingPlaidCleanupMarkers is the sole place that decides whether one is currently open. A NULL
// marker can never itself trigger a disconnect (GetUsersNeedingPlaidDisconnectRepair requires
// plaid_cleanup_pending_at IS NOT NULL), so this intermediate state fails safe even before the next
// backstop pass runs.
//
// Restores a FULL grace window within a single repairPlaidDisconnects pass, not after a delay:
// StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers both run before the candidate
// query on every pass (repairPlaidDisconnects, retention_cleanup_check.go) — so in the very same pass,
// Stamp writes a fresh clock_timestamp() for this NULL marker (episode still open: ineligible status,
// live item, no comp) and the candidate query's own GREATEST(...) < clock_timestamp() - grace
// comparison then evaluates false against that just-written value. The user is not selected, and a
// clean 72-hour grace window starts from this cancellation instant, not from whatever the frozen
// value used to be.
//
// See TestCancelDeletion_Integration_ClearsStaleMarkerRestoresFullGraceWindow
// (profile_repository_deletion_cancel_marker_integration_test.go) for the regression proof.
func (r *profileRepository) CancelDeletion(ctx context.Context, userID string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    plaid_cleanup_pending_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// DeletionSweepCursor is a keyset-pagination position for GetUsersScheduledForDeletionPage,
// captured from the last row of the previous page — (ScheduledAt, UserID), matching that page's
// ORDER BY (deletion_scheduled_at, id) ASC. A nil cursor starts from the beginning.
type DeletionSweepCursor struct {
	ScheduledAt time.Time
	UserID      string
}

// GetUsersScheduledForDeletionPage returns up to `limit` users whose deletion_scheduled_at has
// passed, ordered by (deletion_scheduled_at, id) ASC for deterministic keyset pagination — Batch
// S2 Task 4 (Issue #1440/#1448 follow-up; same defect class as #1469/ItemCleanupCheck).
//
// The unbounded GetUsersScheduledForDeletion this replaced (deleted in #1488, once #1482 removed
// its last caller) had NO LIMIT and ordered by deletion_scheduled_at alone (no id tie-breaker, so
// ties were not even deterministically ordered). That was harmless while this
// query only fed a soft, reversible flow, but #1440 turned it into a REAL hard delete, and #1448
// registered its caller with RunOnStart: true — so an unbounded result set is now unbounded
// destructive work triggered on process start, with no deterministic order to reason about.
//
// A bare `LIMIT n` alone would trade one bug for a worse one: since this predicate does not care
// WHY a user hasn't been deleted yet (only deletion_scheduled_at/is_deleted), a permanently- or
// long-stuck oldest row (e.g. an admin comp continuously renewed after each scheduled deletion
// date, or a linked_items cleanup that never finishes — see DeleteUserCascade's own guards) would
// occupy a slot in the oldest-first page FOREVER, starving every user ranked past the page size
// out of every single run. Keyset pagination fixes this: the caller (ScheduledDeletionCheckWorker)
// loops pages within ONE run, advancing the cursor past whatever it just saw — including past a
// stuck row — until a page comes back short (exhausted) OR the caller's total-processed safety
// cap (maxUsersPerDeletionSweep, queue/jobs/scheduled_deletion_check.go) is reached. Hitting the
// cap logs a WARN naming how many users were processed and stops that run early WITHOUT
// scheduling anything new. Since Issue #1477 the next periodic run's pagination RESUMES from the
// timestamp that capped run stopped at, rather than starting over from the oldest still-eligible
// row — see the paragraph below and repository/sweep_cursor_repository.go. (Through #1477 it did
// start over, and the users a run had already enqueued were cheaply re-skipped via
// AccountDeletionUniqueOpts' dedup until they were actually deleted, at which point the row is
// hard-deleted — DeleteUserCascade's `DELETE FROM users` below — and no longer matches this query
// at all. That dedup is still what makes a re-visited row cheap; it is simply no longer the only
// thing that lets a later run reach users past an earlier run's cap.)
//
// That rate-based framing describes only the SLOW case, and it was not the whole story. Until
// Issue #1477 the failure mode above the cap was STRUCTURAL, not a matter of arrival rate: every
// run restarted pagination from a nil cursor, so if the number of eligible rows NOT actually
// completing deletion (stuck in AccountDeletionWorker, or otherwise never reaching the point where
// DeleteUserCascade removes them from this query's result set) exceeded maxUsersPerDeletionSweep
// (queue/jobs/scheduled_deletion_check.go), every future sweep spent its ENTIRE budget re-fetching
// those same oldest stuck rows and NEVER reached anything ranked past the cap, for as long as they
// kept failing. That was permanent starvation of every row past the cap, not eventual drainage.
//
// #1477 closed it: ScheduledDeletionCheckWorker now PERSISTS the timestamp a capped run stopped at
// (the sweep_cursors table — repository/sweep_cursor_repository.go) and passes it back here as the
// next run's `after`, so a permanently-failing head no longer occupies every run's budget. The
// replacement guarantee is BOUNDED, not absolute — every candidate is reached within one full
// cycle of runs rather than promptly, and rows that become eligible behind the current watermark
// wait for the wrap. It is stated once, in full, in repository/sweep_cursor_repository.go's
// header; do not restate it here as "no starvation".
func (r *profileRepository) GetUsersScheduledForDeletionPage(ctx context.Context, after *DeletionSweepCursor, limit int) ([]*domain.User, error) {
	if limit <= 0 {
		limit = 500
	}
	if after == nil {
		query := userSelectQuery + `
			WHERE deletion_scheduled_at IS NOT NULL
			  AND deletion_scheduled_at <= $1
			  AND is_deleted = FALSE
			ORDER BY deletion_scheduled_at ASC, id ASC
			LIMIT $2
		`
		return r.queryUsers(ctx, query, time.Now(), limit)
	}
	query := userSelectQuery + `
		WHERE deletion_scheduled_at IS NOT NULL
		  AND deletion_scheduled_at <= $1
		  AND is_deleted = FALSE
		  AND (deletion_scheduled_at, id) > ($2, $3)
		ORDER BY deletion_scheduled_at ASC, id ASC
		LIMIT $4
	`
	return r.queryUsers(ctx, query, time.Now(), after.ScheduledAt, after.UserID, limit)
}

// DeleteUserCascade performs a REAL hard delete of the users row, guarded by a re-verification of
// eligibility INSIDE this same transaction — see the interface doc comment for the full Issue
// #1413 round-4 C1/C2 rationale, unchanged by this method's Issue #1440 rewrite.
//
// Issue #1440: this used to explicitly DELETE five tables (boosts, transactions, linked_items,
// accounts, notifications) and then SOFT-delete the users row (SET is_deleted = TRUE). Because
// the users row survived, NOT ONE of the schema's foreign keys into users ever fired — the five
// explicit DELETEs merely duplicated five of the (as of this writing) 37 ON DELETE CASCADE
// relationships into users in Go, by hand, and the other 32 user-scoped tables silently retained
// personal data after every "completed" GDPR deletion. This is also how the bug that opened this
// issue shipped in the first place: three of the original explicit DELETEs named tables
// (recurring_patterns, user_preferences, user_sessions) that never existed in any migration —
// aspirational leftovers from this method's very first draft (commit cffde2b7, written against a
// pre-Clerk/pre-Paddle placeholder schema) — so the whole transaction aborted and every real
// GDPR deletion failed silently. See TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned
// for the regression test this shipped without: it seeds a row in every user-scoped table this
// schema has and asserts every one is gone, instead of checking a hard-coded handful.
//
// The fix: stop duplicating the FK graph in Go. Redact audit-log PII (see the dedicated block
// below), then issue one real `DELETE FROM users`, and let the schema's foreign keys do the
// erasure — the single source of truth for which tables are user-scoped, incapable of silently
// drifting out of sync with the Go code the way the five explicit DELETEs did.
func (r *profileRepository) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error) {
	// Start a transaction for atomicity
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	// affectedOwnerIDs collects, deduplicated, every OTHER user whose row this transaction mutates
	// below (currently: the dangling-cross-user-category-override clear). See that UPDATE's own
	// comment for why this exists: the comment right above it — correctly — argues the REWRITTEN
	// value is not PII, which is true and irrelevant, because it is not the rewritten value that
	// leaks; it is the OLD one, still sitting in that owner's warm analytics cache. Populated only
	// on the success path; every guard-abort and error return above leaves it nil.
	//
	// Why budget_categories.custom_category_id (CASCADE) and categorization_rules.
	// target_custom_category_id (SET NULL) do NOT need the same tracking (Issue #1531 slice H4):
	// this user's own custom_categories rows cascade away later in this same DELETE FROM users
	// statement, which would trigger those two FKs for any OTHER user's budget_categories/
	// categorization_rules row that references one — exactly the shape of leak this comment
	// documents for the RESTRICT case above. Issue #1540 (budget_service.go,
	// categorization_rule_service.go) closed that at write time: a budget_categories or
	// categorization_rules row can now only ever reference a custom_categories row owned by the
	// SAME user, so the CASCADE/SET NULL this DELETE triggers can only ever touch the DELETED
	// user's own rows — which are cascading away in this same statement regardless (budgets.user_id
	// and categorization_rules.user_id both CASCADE from users), and whose cache is purged by
	// purgeCacheForDeletedUser, not this affectedOwnerIDs mechanism. custom_category_override_id
	// needed the mechanism above ONLY because it lives on transactions, a table with NO trigger
	// enforcing same-user ownership against custom_categories (unlike custom_categories.parent_id's
	// own two-level trigger) — that gap is exactly what #1440's RESTRICT-clearing UPDATE above
	// works around. This argument holds only if no cross-user row pre-dates #1540; see #1531 slice
	// H3 for that audit.
	var affectedOwnerIDs []string

	// Issue #1440 round-3 Finding 7 (Codex review, coordinator's #1440 follow-up brief): lock any
	// household this user owns BEFORE locking the user row below, matching household_service.go's
	// own codebase-wide invariant — AcceptInvite's own doc comment (step 4, "lockAndGetUser on the
	// owner") states it explicitly: "No path locks a users row before a households row ... never a
	// cycle." That invariant held until the round-2 household-owner guard (Issue #1440 Critical
	// finding) was added to THIS method: it locks the USER row first (the eligibility recheck's
	// `FOR UPDATE` just below), and only touches the household row implicitly, much later, when the
	// final `DELETE FROM users` cascades into households via owner_user_id's CASCADE FK. Two
	// transactions acquiring the SAME two rows in OPPOSITE orders is a textbook deadlock:
	// AcceptInvite locks household then wants the owner's user row (step 1 then step 4); this method
	// used to lock the user row then (via its cascading DELETE) want the household. Verified
	// empirically (TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_
	// NoDeadlockWithConcurrentAcceptInvite): the pre-fix order genuinely deadlocks — "ERROR: deadlock
	// detected (SQLSTATE 40P01)" — against a transaction that reproduces AcceptInvite's exact lock
	// sequence.
	//
	// households.owner_user_id is UNIQUE (migration 000106) — a user owns at most one household, so
	// this is a single-row lookup, not a scan. A non-locking `SELECT ... FOR NO KEY UPDATE` on the
	// household row (if one exists; zero rows is a normal, common no-op for a non-owner) reorders
	// this method's OWN lock acquisition to household-then-user, matching AcceptInvite, closing the
	// cycle at its root rather than at one specific interleaving of it.
	//
	// `FOR NO KEY UPDATE`, not `FOR UPDATE` — deliberately, to avoid reintroducing the OTHER deadlock
	// class this codebase has already been bitten by once (StampMissingPlaidCleanupMarkers's ROUND 23
	// correction, this file, ~line 2545): a `FOR UPDATE` lock on the household row would conflict with
	// a concurrent `FOR KEY SHARE` request an unrelated INSERT referencing this household (e.g. a
	// household_members or household_invites row) would need for its own FK check, which this method
	// does not need to preclude — this transaction never updates a key column on the household row
	// itself (it only ever gets removed via CASCADE from the DELETE below, which takes its own
	// necessary lock at that time regardless of what mode was held earlier). `FOR NO KEY UPDATE` still
	// conflicts with a concurrent `FOR UPDATE` request — which is exactly what AcceptInvite's/
	// RemoveMember's/OffboardExcessMembers'/ForceDisband's own household-row locks all use — so this
	// still correctly blocks any of THOSE from proceeding concurrently while this guard is being
	// decided; it only stops conflicting with the narrower, unrelated FK-driven `FOR KEY SHARE` case.
	var lockedHouseholdID *string
	err = tx.QueryRow(ctx, `SELECT id FROM households WHERE owner_user_id = $1 FOR NO KEY UPDATE`, userID).Scan(&lockedHouseholdID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, nil, fmt.Errorf("DeleteUserCascade: lock owned household: %w", err)
	}

	// Issue #1413 round-4 Critical finding C1: re-verify eligibility INSIDE this transaction,
	// immediately before any destructive statement — closing the window between ClaimForDeletion
	// (which the external cleanup above this call in ExecuteDeletion can separate from here by
	// many seconds) and this cascade. FOR UPDATE locks the user row so a concurrent CancelDeletion
	// or comp-grant must either commit before this SELECT runs (and be seen by it) or block until
	// this transaction ends (by which point the outcome here is already decided and committed).
	//
	// Uses deletionAuthorisationUnionPredicate — "a claim of EITHER kind would still be
	// authorised" — which preserves the comp-vs-GDPR-request precedence rule exactly as
	// ClaimForDeletion enforces it: a user's own outstanding GDPR deletion request still beats a
	// live comp here. Issue #1440, T0: that constant is now COMPOSED from the two kind-specific
	// predicates rather than being a hand-written third variant, so it cannot drift away from what
	// ClaimForDeletion actually enforces. See its doc comment for why the union plus the
	// exact-value check below is equivalent to the kind-specific rule in every reachable case, and
	// therefore why this method does not need to be told the kind.
	//
	// ADDITIONALLY requires deletion_requested_at to be EXACTLY the value the caller observed
	// before calling ClaimForDeletion (IS NOT DISTINCT FROM handles the NULL case) — this is the
	// part no authorisation predicate can provide by itself: the retention branch of the union
	// treats "no manual_tier_override at all" as sufficient authorization for a user with no
	// outstanding request, so an uncomped user who explicitly cancels their own deletion request
	// between the claim and this point would otherwise still pass it (cancelling makes
	// deletion_requested_at NULL, and "no override" was already true) — this is the C1 finding's
	// most serious case: an explicitly-cancelled deletion must not still happen.
	var stillEligible bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1 AND is_deleted = FALSE
			  AND deletion_requested_at IS NOT DISTINCT FROM $2
	`+deletionAuthorisationUnionPredicate+`
			FOR UPDATE
		)
	`, userID, expectedDeletionRequestedAt).Scan(&stillEligible)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: eligibility re-check: %w", err)
	}
	if !stillEligible {
		// Legitimate, expected abort — NOT an error. The deferred tx.Rollback(ctx) above discards
		// everything; the caller must release its work claim and let the next scheduled run
		// re-evaluate from scratch.
		return false, nil, nil
	}

	// Issue #1413 round-4 Critical finding C2: refuse to delete the local linked_items rows (the
	// only pointer to a live Plaid access token — Step 3 below deletes them without ever calling
	// Plaid itself, by design, since that call is DeletionService's job before this method runs)
	// while ANY item still needs Plaid-side cleanup. The FOR UPDATE lock just taken above also
	// blocks a concurrent LinkAccounts INSERT for this user from committing until this
	// transaction ends (linked_items.user_id's FK check requires a FOR KEY SHARE lock on this
	// exact users row, which conflicts with FOR UPDATE) — so this check sees any item created
	// before this point that the caller's Plaid cleanup pass could not have known about. An item
	// whose INSERT commits AFTER this transaction's own commit is a residual this check cannot
	// close (see plaid_item_cleanup_service.go's re-read-inside-claim fix for why that window is
	// already as narrow as this branch makes it) — but this check must not make that window any
	// WIDER by proceeding while a KNOWN un-cleaned item still exists.
	var uncleanedItemsExist bool
	err = tx.QueryRow(ctx, `SELECT `+uncleanedLinkedItemExistsClause, userID).Scan(&uncleanedItemsExist)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: un-cleaned Plaid item check: %w", err)
	}
	if uncleanedItemsExist {
		// Legitimate, expected abort — NOT an error. Preferring to abort and retry over silently
		// orphaning a live Plaid token: an undeleted user is recoverable, an orphaned Plaid item
		// is not.
		return false, nil, nil
	}

	// Issue #1440 Critical finding (Codex review): re-verify the household-owner guard INSIDE this
	// same transaction, immediately before the destructive DELETE — the same principle finding
	// C1/C2 above already established for eligibility and Plaid cleanup, now applied to household
	// ownership. #1367's guard (domain.ErrHouseholdHasActiveMembers) lived ONLY in
	// DeletionService.RequestDeletion — the user-INITIATED request path. ExecuteDeletion's other
	// two callers (the retention-triggered deleteExpiredAccounts, and the legacy
	// DeletionWorker/AccountDeletionWorker paths) call this method directly, with NO owner guard at
	// all: a household owner whose retention clock expires while still owning an active household
	// would reach the DELETE below unguarded. households.owner_user_id CASCADEs the household row
	// away, taking every OTHER member's household_members row with it, and — via
	// subscription_entitlements.payer_user_id CASCADE — every OTHER beneficiary's entitlement too.
	// That is irreversible destruction of a DIFFERENT user's data, not just the deletion target's
	// own.
	//
	// householdOwnerBlocksDeletionClause is deliberately BROADER than the #1367 rule
	// (CountActiveMembers(householdID) > 1) alone: it also blocks on any subscription_entitlements
	// row this household funds (household_id = h.id) for someone OTHER than the owner, regardless
	// of that beneficiary's current household_members.status. RemoveMember
	// (household_repository.go) only flips a member's status to 'removed' — its own doc comment
	// says it "Never touches Plaid Items", and it likewise never touches subscription_entitlements
	// — so a just-removed member's owner-funded entitlement can outlive their active membership row
	// entirely. An active-member-count-only guard would miss that residual; this does not.
	//
	// Unlike the comp/deletion-request-cancelled guard above (a normal, expected (false, nil) skip
	// that resolves itself on the next scheduled run), this condition CANNOT self-heal: nothing else
	// in the system will transfer ownership, remove the member, or reconcile the stale entitlement.
	// A silent (false, nil) skip here would let a GDPR deletion deadline go unmet forever with no
	// visible signal. So this returns a genuine, non-nil error instead — every ExecuteDeletion
	// caller already logs a non-nil error at Error level, and the two River-job callers additionally
	// retry it (eventually surfacing in the dead-letter queue the System Health Dashboard already
	// monitors). observability.HouseholdOwnerDeletionBlockedTotal gives an operator a dedicated,
	// alertable signal distinct from ordinary transient failures — this situation requires a human
	// to actually resolve the household, not just retry.
	var householdOwnerBlocksDeletion bool
	err = tx.QueryRow(ctx, `SELECT `+householdOwnerBlocksDeletionClause, userID).Scan(&householdOwnerBlocksDeletion)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: household owner guard check: %w", err)
	}
	if householdOwnerBlocksDeletion {
		observability.HouseholdOwnerDeletionBlockedTotal.Inc()
		return false, nil, domain.ErrHouseholdHasActiveMembers
	}

	// #1650: capture every PostHog distinct ID while the local identity rows still exist. The
	// canonical users.clerk_user_id, historical user_identities Clerk IDs, and the internal UUID
	// formerly used by backend producers are encrypted together below. No email is copied.
	identityRows, err := tx.Query(ctx, `
		SELECT clerk_user_id FROM users WHERE id = $1
		UNION ALL
		SELECT clerk_user_id FROM user_identities WHERE user_id = $1`, userID)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: read PostHog identities: %w", err)
	}
	var encryptedClerkIDs []string
	for identityRows.Next() {
		var clerkID string
		if scanErr := identityRows.Scan(&clerkID); scanErr != nil {
			identityRows.Close()
			return false, nil, fmt.Errorf("DeleteUserCascade: scan PostHog identity: %w", scanErr)
		}
		encryptedClerkIDs = append(encryptedClerkIDs, clerkID)
	}
	if rowsErr := identityRows.Err(); rowsErr != nil {
		identityRows.Close()
		return false, nil, fmt.Errorf("DeleteUserCascade: read PostHog identities: %w", rowsErr)
	}
	identityRows.Close()
	erasureEnvelope, err := buildPostHogErasureEnvelope(r.enc, userID, encryptedClerkIDs)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: build PostHog erasure obligation: %w", err)
	}

	// Issue #1440 Finding 2 (Codex review): clear any DANGLING cross-user override BEFORE the hard
	// delete below. transactions.custom_category_override_id -> custom_categories is ON DELETE
	// RESTRICT (deliberately — see migration 000065: "prevents deleting a custom_category while it
	// is the active override", a real, everyday product rule enforced by
	// custom_category_repository.go, not something to weaken via a schema change just for this
	// method). custom_categories.user_id -> users is CASCADE, so this user's own custom_categories
	// rows are about to disappear as part of the DELETE below.
	//
	// The user's OWN transactions (also cascading away in the very same DELETE FROM users
	// statement) never trip this RESTRICT — empirically verified: Postgres's RI check for RESTRICT
	// evaluates once every row this single statement cascades away has actually been removed, not
	// per-row as each cascade step runs, so an override pointing at a category that is ALSO being
	// deleted in this same statement is never seen as "still referenced". The one case that DOES
	// reach a real, reproducible failure: transactions.custom_category_override_id has NO trigger
	// enforcing same-user ownership (unlike custom_categories.parent_id's own
	// enforce_custom_category_two_levels trigger), so a DIFFERENT, non-deleted user's transaction
	// can reference this user's custom_categories row as its override — that transaction is NOT
	// part of this statement's cascade, so the RESTRICT fires for real and the whole deletion
	// fails. Clearing it here, unconditionally, ahead of time, closes that residual regardless of
	// which user owns the referencing transaction.
	//
	// category_override_kind must be cleared in the SAME statement, not just
	// custom_category_override_id: valid_category_override_discriminator (migration 000065) forbids
	// kind='custom' with a NULL override_id, so nulling only the FK column here would trade one
	// constraint violation for another.
	//
	// Round-3 addition (Issue #1440 follow-up, Codex review, Finding 3, High): also reset
	// effective_category_name to pfc_primary in this SAME statement. effective_category_name is an
	// app-maintained column (migration 000073) — unlike effective_pfc_primary/effective_pfc_detailed
	// (GENERATED ALWAYS AS, migration 000073:15-30), it does NOT recompute itself when
	// category_override_kind/custom_category_override_id change, so clearing only those two columns
	// left effective_category_name holding the DELETED user's custom category NAME on the surviving
	// OTHER user's transaction — personal data (a name the deleted user chose and typed) surviving
	// erasure, in a column this codebase treats as PII (docs/security/posthog-pii-masking.md;
	// data-ph-mask is applied to it on the frontend). Migration 000073 places no CHECK constraint
	// tying effective_category_name to category_override_kind, so setting it here is schema-safe.
	// `effective_category_name = pfc_primary` mirrors custom_category_repository.go's
	// DeleteWithReassignment — the ordinary, already-shipped code path for "a custom category was
	// deleted out from under a transaction that used it as an override" (same-user case) — so a
	// cross-user dangling override left by THIS method resolves to the exact same, already-expected
	// value: the effective category reverts to the transaction's own underlying PFC primary, matching
	// what effective_pfc_primary's generated-column CASE will independently compute for the same row
	// now that category_override_kind is NULL (ELSE pfc_primary branch) — the two columns stay in
	// agreement, not just individually non-PII.
	//
	// GDPR round-6 finding P1: everything above is about the NEW value this UPDATE writes, and it is
	// right that the new value (pfc_primary, the transaction's own underlying category) is not PII.
	// But the row this UPDATE touches can belong to a DIFFERENT, surviving user — the whole point of
	// "cross-user dangling override" — and that surviving owner's warm analytics cache (keyed on
	// THEIR user/household scope, not the deleted user's) may already hold the OLD
	// effective_category_name: the deleted user's custom category NAME, which the docstring above
	// this method's cache-purge call site (purgeCacheForDeletedUser) never bumps, because it only
	// knows about the deleted user, their Clerk ID, and their own household. Without a bump keyed on
	// the SURVIVING owner, that owner's cache keeps serving the just-erased user's chosen category
	// name for up to the analytics namespace's TTL. RETURNING user_id below hands the caller the
	// exact set of rows this statement rewrote, so it can bump precisely those owners' cache scopes
	// AFTER this transaction commits (never inside it — a cache bump is not part of the erasure and
	// must not be able to roll it back or delay it).
	// #1590: THE CLEAR IS NOW RECORDED, for the surviving owners only.
	//
	// Everything above says why this statement must run and why the value it writes is right. What
	// it did not say is that the row it touches can belong to a DIFFERENT, SURVIVING user, and that
	// user's category override, override kind and effective name were destroyed with no trail — in a
	// codebase whose whole thesis is that a user's own work is retained. Three other statements that
	// overwrite the same columns (UpdateCategory, BulkUpdateCategory, and since #1578 the settlement
	// move) already record what they overwrote; this one carried an allowlist exemption instead.
	//
	// The fix is the SAME capture CTE those three use, under a fourth change_source, 'erasure'
	// (migration 000172). No new plumbing: the surviving owner was already in hand here, because
	// RETURNING user_id is what feeds affectedOwnerIDs for the cache bump.
	//
	// ⚠ ONLY SURVIVING OWNERS ARE RECORDED — that is what the `surviving` CTE is for. The erased
	// user's own transactions are cleared by this same statement and hard-deleted moments later by
	// the DELETE FROM users below, and transaction_annotation_history.user_id CASCADEs from users, so
	// a row written for them would be destroyed inside this same database transaction. Recording it
	// would cost writes and prove nothing.
	//
	// ⚠ AND THAT SAME CASCADE IS WHY B'S ROW SURVIVES A'S ERASURE. The recorded user_id is inherited
	// from the UPDATE's own RETURNING — never re-derived, never the erased user — so the history row
	// hangs off B, whom this statement does not delete. Erasing B later still destroys it, which is
	// the direction GDPR requires. Both are pinned:
	// TestErasureClear_SurvivingUsersAnnotationIsRecorded_Integration and
	// TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned.
	//
	// ⚠ THE SEVERITY IS DEFENCE IN DEPTH, and it is worth keeping the reason visible rather than
	// letting the next reader infer a live bug from the length of this comment. All five writers of
	// transactions.custom_category_override_id refuse a cross-owner value today (the two category
	// endpoints via services/transaction_service.go, the two settlement moves via their
	// twin.user_id = s.user_id predicate, and CreateBatch via the rule target that
	// assertTargetCategoryOwnedAndLive validates). Nothing in the DATABASE enforces it — there is no
	// trigger — so the row remains schema-reachable, and this records the loss if one ever exists.
	//
	// tombstone-exempt(transactions prior): the prior alias is the PRE-IMAGE of the very row this
	// statement is updating (prior.id = t.id), not an independent read. The target is deliberately
	// unfiltered (see the write: exemption below) and repeating a filter on prior would capture a
	// DIFFERENT set of rows from the one the UPDATE touches.
	clearDanglingOverridesSQL := `
		WITH cleared AS (
			-- tombstone-exempt(write:transactions t): GDPR ERASURE (#1569 T8 write-target audit). A tombstoned
			-- transaction still holds an FK to the category about to be hard-deleted, so skipping it
			-- would trip fk_transactions_custom_category_override and abort the erasure.
			UPDATE transactions t
			SET custom_category_override_id = NULL, category_override_kind = NULL, effective_category_name = t.pfc_primary
			-- tombstone-exempt(transactions prior): the PRE-IMAGE of the row being updated
			-- (prior.id = t.id), joined so the capture below reads what this statement destroys.
			FROM transactions prior
			WHERE prior.id = t.id
			  AND t.custom_category_override_id IN (
				-- tombstone-exempt(custom_categories): GDPR ERASURE (#1569 Task 9). A tombstoned category is still a row
				-- and is about to be hard-deleted by the DELETE FROM users below; a surviving user's
				-- transaction pointing at it would trip fk_transactions_custom_category_override
				-- exactly as it would for a live one. Filtering here would reintroduce the residual
				-- this statement exists to close. Pinned by
				-- TestCategoryTombstone_GDPRErasureStillDestroysIt_Integration.
				SELECT id FROM custom_categories WHERE user_id = $1)
			RETURNING ` + annotationCaptureReturning("t") + `
		),
		surviving AS (
			-- The erased user's own rows are excluded here rather than in the INSERT, because the
			-- final SELECT below still needs the FULL set: a row belonging to the erased user is not
			-- worth a history entry, but the caller's cache bookkeeping reads every owner.
			SELECT * FROM cleared WHERE captured_user_id <> $1::uuid
		)` + annotationCaptureInsert(domain.TransactionAnnotationChangeErasure, "surviving") + `
		SELECT captured_user_id FROM cleared
	`

	// The lock predicate is the UPDATE's own, verbatim. annotationLockTargets requires that: a lock
	// over a smaller set than the write touches leaves rows unlocked and reintroduces the race the
	// lock exists to close.
	const clearDanglingOverridesLock = `custom_category_override_id IN (
		-- tombstone-exempt(custom_categories): same reason as the statement it locks for — the
		-- category is about to be hard-deleted whether or not it is tombstoned.
		SELECT id FROM custom_categories WHERE user_id = $1)`

	seenOwner := map[string]bool{userID: true} // the deleted user's own cache is purged separately; never re-bump it here
	if err := runCapturingQuery(ctx, tx,
		clearDanglingOverridesLock, []any{userID},
		clearDanglingOverridesSQL, []any{userID},
		func(rows pgx.Rows) error {
			for rows.Next() {
				var ownerID string
				if scanErr := rows.Scan(&ownerID); scanErr != nil {
					return fmt.Errorf("DeleteUserCascade: scan dangling custom category override owner: %w", scanErr)
				}
				if !seenOwner[ownerID] {
					seenOwner[ownerID] = true
					affectedOwnerIDs = append(affectedOwnerIDs, ownerID)
				}
			}
			return nil
		}); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: clear dangling custom category overrides: %w", err)
	}

	// Issue #1440 round-3 Finding 4 (Codex review): null out four ON DELETE NO ACTION references
	// (the default delete rule for a bare REFERENCES clause with no explicit ON DELETE) that can
	// legally hold a CROSS-USER reference, none of it enforced by any trigger:
	//
	//   - accounts.replaced_by_id -> accounts(id)               (migration 000033:43-48)
	//   - linked_items.replaced_by_id -> linked_items(id)       (migration 000032:35-40)
	//   - accounts.user_account_id -> user_accounts(id)         (migration 000033:19-24)
	//   - transactions.user_account_id -> user_accounts(id)     (migration 000034:16-21)
	//
	// This user's own accounts/linked_items/user_accounts rows are about to cascade away via their
	// own user_id -> users CASCADE FKs (part of the DELETE FROM users below). NO ACTION's
	// referential-integrity check has the SAME end-of-statement timing as RESTRICT (see the dangling
	// custom-category-override clear above for the empirically-verified mechanism): a same-user
	// reference is satisfied for free because both the referencing and referenced row cascade away
	// together in the SAME statement, but a DIFFERENT user's surviving row that still points at one
	// of this user's about-to-vanish rows makes the whole `DELETE FROM users` statement fail with a
	// foreign key violation. Migration 000100 (:276-312) is direct evidence this is reachable in
	// production, not a theoretical edge case: its own comments for accounts.user_account_id and
	// transactions.user_account_id both say "ON DELETE NO ACTION — must re-point before deleting
	// losers", and it re-points BOTH columns before removing a duplicate user_accounts row for
	// exactly this reason.
	//
	// Nulling unconditionally (not scoped to "only when the referencing row belongs to someone
	// else") is deliberate and harmless: all four columns are nullable (no NOT NULL constraint in
	// any of the migrations above), and a same-user reference would cascade away in this same
	// statement regardless of whether it is nulled first.
	if _, err = tx.Exec(ctx, `
		UPDATE accounts SET replaced_by_id = NULL
		WHERE replaced_by_id IN (SELECT id FROM accounts WHERE user_id = $1)
	`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: clear dangling accounts.replaced_by_id references: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE linked_items SET replaced_by_id = NULL
		WHERE replaced_by_id IN (SELECT id FROM linked_items WHERE user_id = $1)
	`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: clear dangling linked_items.replaced_by_id references: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE accounts SET user_account_id = NULL
		WHERE user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $1)
	`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: clear dangling accounts.user_account_id references: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		-- tombstone-exempt(write:transactions): GDPR ERASURE, same reason as the override clear above --
		-- a tombstoned row's dangling user_account_id would abort the cascade (#1569 T8).
		UPDATE transactions SET user_account_id = NULL
		WHERE user_account_id IN (SELECT id FROM user_accounts WHERE user_id = $1)
	`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: clear dangling transactions.user_account_id references: %w", err)
	}

	// Redact audit-log PII BEFORE the hard delete below. audit_logs.user_id (the ACTING admin —
	// see admin_user_repository.go:390-393's doc comment: "audit_logs.user_id -> the ACTING
	// ADMIN; resource_id -> the TARGET") already gets nulled for free by its own ON DELETE SET
	// NULL FK once the DELETE below runs. But audit_logs.resource_id — the column that actually
	// names the SUBJECT of an admin action, e.g. a tier-override target — is a bare UUID column
	// with NO foreign key at all (migration 000003:17): it would otherwise keep pointing at this
	// user's id, WITH its PII-bearing `changes` payload intact, forever. Redact the payload for
	// BOTH columns here — resource_id is the one this actually matters for; user_id is belt-and-
	// braces given the FK already handles it, and harmless to include if that FK is ever changed.
	// Per the product decision: redact the payload, KEEP the row — this preserves the tamper-
	// evident audit trail GDPR itself expects while removing the personal data it carries.
	//
	// Must run in THIS transaction, before the DELETE below (the SET NULL FK fires as part of
	// that DELETE — after it, `user_id = $1` would no longer match anything, only `resource_id`
	// still would). Deliberately a single flat UPDATE with no extra row-locking: it only touches
	// audit_logs rows, so it cannot widen the users-row lock this transaction already holds from
	// the eligibility recheck above (FOR UPDATE) — see StampMissingPlaidCleanupMarkers's ROUND 23
	// doc comment (~line 2440) for the cross-user deadlock class (SQLSTATE 40P01, an audit-log
	// INSERT's FK check requesting FOR KEY SHARE on a row already held FOR UPDATE elsewhere) that
	// widening a lock window here would risk.
	_, err = tx.Exec(ctx, `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, userID)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: redact audit log PII: %w", err)
	}

	// ROUND 15, TIER A: RETRACT THIS USER'S DELETION-RESIDUE RECORDS. This is the other half of
	// making the residue mechanism durable, and without it that mechanism would be unusable.
	//
	// Since round 15, DeletionService records each irreversible external Paddle call BEFORE it is
	// issued rather than inferring it from an in-memory flag at the end of the run (see
	// recordIssuedExternalEffect, deletion_service.go, for why an end-of-run inference cannot survive
	// a lost acknowledgement, a panic, or process death). A residue record asserts one thing:
	// "irreversible external work was done to a user WHO IS STILL HERE". The moment this transaction
	// commits that assertion is false — the user is gone and the cancellation was the intended
	// outcome — so the record must go with them.
	//
	// ⚠ IT MUST BE THIS TRANSACTION, NOT A BEST-EFFORT CALL AFTER IT. Both Prometheus gauges alert on
	// `increase(deletion_residue_records[15m])`, so a window in which a completed deletion's rows are
	// still visible is a false alarm on every ordinary deletion of a paying user — and a crash inside
	// that window would leave a permanent one, for a user nobody can look up. Doing it here means no
	// reader ever observes the intermediate state at all: the rows and the user disappear together or
	// neither does.
	//
	// ⚠ DELETE, NOT REDACT. The redaction above deliberately KEEPS rows: they are the tamper-evident
	// GDPR trail. A residue row is not part of that trail — it carries no `changes` payload by
	// construction and describes no action taken against the user's data. It is an OPERATOR WORK ITEM,
	// and this statement is the only thing that ever closes one. Note the ordering is immaterial to
	// the redaction (residue rows have nothing to redact); it is placed here to sit with the other
	// audit_logs statement rather than for any dependency.
	//
	// Matched by the same literal prefix the operator query, the gauges and migration 000138's partial
	// index use — via deletionResidueActionPrefix, so it cannot drift away from what is written.
	if _, err = tx.Exec(ctx,
		`DELETE FROM audit_logs WHERE resource_id = $1 AND action LIKE $2`,
		userID, deletionResidueActionPrefix+"%"); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: retract deletion-residue records: %w", err)
	}

	// Issue #1494: PRESERVE THE OBLIGATION BEFORE DESTROYING ITS ONLY RECORD.
	//
	// The DELETE below removes this user's linked_items rows (via user_id's ON DELETE CASCADE). Every
	// marker those rows carry — plaid_removed_at, plaid_removal_failed_at, the Plaid Item id itself —
	// goes with them. For a row that was PROVED removed that is correct and complete: Plaid billing
	// has stopped and nothing is owed. For any row we could NOT prove removed, the Item may still
	// exist and may still bill, and after this statement no part of this system would know it ever
	// existed. That is the exact outcome the C2 guard above exists to prevent, and the guard alone
	// cannot prevent it any more: since #1494 it deliberately stops blocking on a row marked
	// plaid_removal_failed_at, so that a permanently-unremovable Item cannot block a GDPR erasure
	// forever. THIS INSERT IS THE PRICE OF THAT ESCAPE. Without it the escape is a silent leak.
	//
	// ORDER AND ATOMICITY, both load-bearing. It runs BEFORE the delete and in the SAME transaction,
	// so there is no interleaving in which the linked_items rows are gone and the record is not
	// written: a failure here returns an error, the deferred rollback discards everything, and the
	// user is still deletable on a later attempt. The reverse order (or a separate transaction, or a
	// best-effort log-and-continue) would reintroduce the "irreversible step taken, durable record
	// missing, nothing left to retry from" shape that four earlier fixes on this branch each closed.
	// Pinned by TestDeleteUserCascade_Integration_OrphanRecordInsertFailure_AbortsTheWholeCascade.
	//
	// SCOPE: `plaid_removed_at IS NULL`, deliberately WIDER than the guard's own predicate. The guard
	// waves through rows marked plaid_removal_failed_at; this copies those AND any row that is merely
	// unresolved. In a cascade that actually proceeds the two sets coincide (the guard would have
	// blocked otherwise), so the difference is only reachable if a future change loosens the guard
	// further — in which case recording MORE than is strictly required is the safe direction, and the
	// only one that cannot silently lose an Item.
	//
	// CIPHERTEXT IS COPIED VERBATIM. item_id / institution_id / institution_name are encrypted at
	// rest by the application layer (linked_item_repository.go's encryptLinkedItemPIIFields), and a
	// SQL statement has no access to the key. Copying the encrypted values is therefore both the only
	// option here and the right one: the destination columns are named *_encrypted and readers decrypt
	// through the same FieldEncryptor. The access token is copied on the same terms since Issue #1497
	// (it was not, before — see the note on the INSERT below and migration 000135). See migration
	// 000134 for why this table carries no user_id at all.
	//
	// Issue #1497: THE ACCESS TOKEN IS NOW COPIED TOO, WITH A DEADLINE. Migration 000134's prose said
	// it deliberately was not, on the argument that this table's population is exactly the population
	// whose token Plaid has already rejected. That argument is true of one leg and false of the table:
	// the `plaid_removed_at IS NULL` scope above also copies 'replaced' re-link leftovers, whose
	// /item/remove was NEVER ATTEMPTED, and for those a retry is the only automated path that has ever
	// existed. See migration 000135 for the full reversal and the GDPR position it rests on.
	//
	// THE DEADLINE IS COMPUTED HERE, IN GO, from domain.OrphanedItemTokenRetention rather than as
	// `NOW() + INTERVAL '30 days'` in the SQL, for two reasons: the constant is the single place the
	// retention period is stated (and is pinned by a test), and a SQL interval literal would be a second
	// copy of it that nothing could keep in step.
	//
	// NULLIF(access_token_encrypted, '') because linked_items.access_token_encrypted is NOT NULL and can
	// legitimately hold '' (the NO_USABLE_ACCESS_TOKEN case, which is one of the two terminal verdicts
	// that lands rows here). Storing '' would make the row look credential-bearing to every predicate
	// that tests `IS NOT NULL` — including the retry selection, which would then present an empty token
	// to Plaid on every run for thirty days. NULL is the honest encoding of "no credential".
	//
	// Ciphertext, verbatim, like the other four columns — access_token_encrypted is produced by
	// FieldEncryptor in linked_item_repository.go and this statement has no key.
	//
	// ON CONFLICT DO NOTHING keys off plaid_orphaned_items_unique_linked_item, so this statement can
	// never be the one that fails a retried cascade.
	if _, err = tx.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (
			linked_item_id, plaid_item_id_encrypted, institution_id_encrypted,
			institution_name_encrypted, linked_item_status, last_error_code, terminal_since,
			access_token_encrypted, token_expires_at
		)
		SELECT id, item_id, institution_id, institution_name, status,
		       plaid_removal_error_code, plaid_removal_failed_at,
		       NULLIF(access_token_encrypted, ''), $2
		FROM linked_items
		WHERE user_id = $1 AND plaid_removed_at IS NULL
		ON CONFLICT (linked_item_id) DO NOTHING
	`, userID, time.Now().UTC().Add(domain.OrphanedItemTokenRetention)); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: preserve orphaned Plaid item records: %w", err)
	}

	// Hard-delete the user row. 37 foreign keys into users are ON DELETE CASCADE and 2 are ON
	// DELETE SET NULL (audit_logs.user_id, household_invites.accepted_user_id) — every user-scoped
	// table these cover is cleaned up by the schema itself rather than by an incomplete,
	// hand-maintained list of DELETEs in Go.
	//
	// A handful of tables are user-scoped WITHOUT any FK to users at all (dead_letter_jobs,
	// idempotency_keys, mcp_audit_log.actor_user_id) — handled by explicit statements above/below
	// this comment, per-table, since the schema cannot enforce their cleanup automatically. See each
	// statement's own doc comment for its disposition (erase vs. retain-and-reason).
	//
	// RESTRICT/NO ACTION note (Issue #1440 follow-up, Codex review "Finding 2"): a prior audit of
	// this method claimed "ZERO FKs into users are RESTRICT/NO ACTION" and treated that as proof
	// this DELETE cannot fail on referential integrity. That claim was about direct FKs INTO users —
	// true — but did not account for the TRANSITIVE closure: custom_categories.parent_id (self-FK,
	// RESTRICT) and transactions.custom_category_override_id -> custom_categories (RESTRICT) sit one
	// hop further out. Empirically verified (see this file's companion tests): when every
	// referencing AND referenced row is cascaded away together within this SAME DELETE FROM users
	// statement, Postgres's RI check for both RESTRICT and NO ACTION is satisfied — it evaluates
	// after all of this statement's own cascaded deletes complete, not per-row as each cascade step
	// runs. The one residual that DOES reach a real, reproducible failure: a DIFFERENT (non-deleted)
	// user's transaction referencing THIS user's custom_categories row as
	// custom_category_override_id — schema-legal (no trigger enforces same-user on that column,
	// unlike custom_categories.parent_id's own trigger), and not covered by this statement's own
	// cascade since that other transaction is not being deleted. Cleared defensively just above (see
	// that UPDATE's own doc comment) before this DELETE runs, rather than relying on FK delete-rule
	// changes — converting either RESTRICT to CASCADE would change real, everyday single-category
	// deletion behavior elsewhere in the app (custom_category_repository.go's reparent-then-delete
	// flow, and the "can't delete a category still used as a transaction override" product rule),
	// which is out of scope for a GDPR account-deletion fix.
	//
	// Cross-user CASCADE consequences (columns other than user_id: households.owner_user_id,
	// household_invites.inviter_user_id, referrals.referrer_id/referred_id,
	// subscription_entitlements.payer_user_id) were re-audited for this follow-up:
	//   - households.owner_user_id: guarded INSIDE this same transaction just above
	//     (householdOwnerBlocksDeletionClause), not merely upstream in
	//     DeletionService.RequestDeletion. A prior version of this comment claimed
	//     RequestDeletion's #1367 guard alone made this safe — that was wrong: RequestDeletion
	//     never runs on the retention-triggered or legacy-worker ExecuteDeletion paths, both of
	//     which call this method directly. See the guard block above for the full rationale.
	//   - subscription_entitlements.payer_user_id: the composite FK
	//     fk_subscription_entitlements_payer_is_owner (migration 000112) makes payer_user_id EQUAL
	//     the household's owner_user_id by construction, so this column is covered by the SAME
	//     guard above — including its broader-than-#1367 check for an entitlement whose beneficiary
	//     (user_id) is no longer an active member (see the guard's own doc comment).
	//   - household_invites.inviter_user_id / referrals.referrer_id/referred_id: unchanged from the
	//     original audit — these only remove the relationship/invite row itself, never another
	//     user's own `users` row or their independently-owned data, so no guard is needed for them.
	// #1650: transactional outbox. This INSERT is in the exact transaction that commits the local
	// hard delete and the terminal audit row. It deliberately precedes the users DELETE and the
	// table deliberately has no users FK, so an insert failure restores all local data while a
	// successful insert survives the cascade. ON CONFLICT is delivery idempotency, not permission
	// to omit the write: the blind-index key names this one user-erasure generation without storing
	// the internal UUID in plaintext.
	if _, err = tx.Exec(ctx, `
		INSERT INTO posthog_erasure_outbox
			(idempotency_key, identities_encrypted, project_id, region, state, phase)
		VALUES ($1, $2, $3, $4, 'pending', 'person_delete')
		ON CONFLICT (idempotency_key) DO NOTHING`,
		erasureEnvelope.IdempotencyKey, erasureEnvelope.Ciphertext,
		r.postHogProjectID, r.postHogRegion); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: persist PostHog erasure obligation: %w", err)
	}

	ct, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: delete user: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// Unreachable in practice — the eligibility recheck above already confirmed this row
		// exists, under a FOR UPDATE lock held for the remainder of this transaction. Defensive,
		// not an expected outcome.
		return false, nil, nil
	}

	// Issue #1440 Finding 3 (Codex review): erase the three tables that carry a user_id (or
	// actor_user_id) column with NO foreign key to users at all — the schema's own CASCADE graph
	// (the whole mechanism this method exists to rely on) cannot reach them. Per-table disposition:
	//
	//   - dead_letter_jobs: operational record of a failed River job. args/errors are jsonb and can
	//     embed arbitrary PII from the original job payload or failure message. A dead-lettered job
	//     for a user who no longer exists has no remaining operational value — nobody will "retry"
	//     it — so this is erased outright, not redacted-and-kept like audit_logs (there is no other
	//     user's audit interest riding on this row the way there is on an ADMIN's audit_logs.user_id
	//     row about a DIFFERENT user).
	//   - idempotency_keys: response_body (jsonb) caches a full API response for retry
	//     deduplication. Already short-lived by design (expires_at defaults to now()+24h), but
	//     erased here rather than left to that TTL — GDPR erasure should not depend on a cache
	//     expiring on its own schedule.
	//   - mcp_audit_log: actor_user_id directly identifies the deleted user. Unlike audit_logs (kept
	//     + redacted, because an admin's audit_logs.user_id row documents an action against a
	//     DIFFERENT user whose own security-audit interest must survive the admin's deletion),
	//     mcp_audit_log.actor_user_id is always the deleted user's OWN MCP tool-call history — no
	//     other user's audit trail depends on this specific row — so it is erased outright.
	//
	// budget_period_events.user_id is ALSO a bare TEXT column with no FK to users, but is
	// deliberately NOT listed here: every row's budget_id is a NOT NULL, ON DELETE CASCADE FK to
	// budgets(id), so it is already cleaned up transitively via that table's own CASCADE (see
	// TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned's dedicated assertion).
	//
	// Round-3 reordering (Issue #1440 follow-up, Codex review, Finding 5, High): these three DELETEs
	// used to run BEFORE `DELETE FROM users` above. None of these three tables has any FK to users,
	// so the FOR UPDATE lock this transaction holds on the users row (the eligibility recheck, far
	// above) never blocks a concurrent writer from inserting into any of them — an in-flight MCP
	// audit call (mcp_audit.go's RecordAuditEntry, a separate autocommit statement resolving the
	// actor's identity fresh, independent of this transaction entirely), an idempotency write (whose
	// advisory lock is per (user_id, key) — idempotency_repository.go — and shares nothing with this
	// transaction's user-row lock), or a River dead-letter insert (queue/river/dead_letter.go) can
	// all land at any point during this transaction's lifetime, completely unblocked by anything held
	// here. Running these deletes early left the ENTIRE remainder of this method (the household
	// guard re-check, the cross-user override/reference clears above, and the DELETE FROM users
	// cascade itself — the slowest step, cascading roughly 40 tables) as an open window during which
	// such a write could land AFTER this delete already ran and AFTER commit — surviving undetected,
	// silently, forever. Moving these three deletes to run immediately before COMMIT, once the user
	// row (and everything FK-linked to it) is already gone, shrinks that window to essentially the
	// time between these three statements and the COMMIT round trip — the best reduction achievable
	// without inventing new locking infrastructure these unrelated tables were never designed to
	// need. It does NOT eliminate the race: this transaction cannot take a lock on a table that has
	// no relationship to users at all, so a write landing in the few remaining milliseconds before
	// COMMIT is still possible in principle. Rather than leave that residual silent, a self-healing/
	// observability backstop closes it the same way this codebase already closes every other
	// residual it cannot fully lock out (see StampMissingPlaidCleanupMarkers/
	// ClearCompletedPlaidCleanupMarkers's self-healing pattern, and monitorMalformedTierOverrides'
	// observability pattern, both profile_repository.go/retention_cleanup_check.go): see
	// GetOrphanedFKlessUserScopedRowCounts (below) and RetentionCleanupCheckWorker's
	// monitorOrphanedFKlessRows, which reports (and, for idempotency_keys, is ALSO already backstopped
	// by IdempotencyCleanupWorker's independent 24-hour TTL sweep — jobs/idempotency_cleanup.go — so
	// that column self-heals within one day even if this fix were entirely absent) any row in these
	// three tables whose user_id/actor_user_id no longer names a real user, on every scheduled pass,
	// rather than leaving a residual GDPR-erasure gap silent and undiscoverable.
	// ⚠ ROUND 12, FINDING AB-3 — THE ONE SHAPE THE BACKSTOP ABOVE DOES NOT SEE. RECORDED, NOT FIXED.
	// The paragraph above is about a row INSERTED into one of these three tables after its DELETE has
	// run; GetOrphanedFKlessUserScopedRowCounts finds those. A dead-letter REPLAY escapes both.
	//
	// DeadLetterService.Retry (services/dead_letter_service.go) claims its dead_letter_jobs row and
	// inserts the replacement river_job in ONE transaction. If that transaction is open when this
	// statement runs, this statement BLOCKS on the row lock the claim holds — while this transaction
	// already holds the users row FOR UPDATE — and nothing bounds the wait: there is no
	// statement_timeout and no lock_timeout set anywhere in this codebase (verified repo-wide; the only
	// timeout mentioned anywhere is Postgres's own deadlock_timeout, in test commentary). It is NOT a
	// deadlock cycle — the replay never asks for anything this transaction holds — so Postgres will not
	// break it. It is a one-way wait lasting as long as the replay does.
	//
	// If the replay commits first, this DELETE then erases its dead_letter_jobs row and the erasure
	// completes — but the river_job it inserted lives in a table this method does not touch at all (0
	// occurrences of `river_job` in this file, against 17 `DELETE FROM` statements naming 5 tables) and
	// stays runnable.
	//
	// ⚠ WHAT ACTUALLY SURVIVES, STATED PRECISELY BECAUSE THE FIRST DESCRIPTION OF THIS OVERSTATED IT.
	// NOT resurrected PII. This codebase deliberately keeps PII out of job args and says so
	// (queue/jobs/email_change_propagation.go: "Email is intentionally NOT stored here to avoid
	// plaintext PII in the river_job table"), and no args type carries an email, name or address. What
	// survives is an internal UUID referencing nothing, inside a job that will find no user and no-op.
	// Real, small, and pre-existing — it is the WAIT and the orphaned runnable job that are new.
	//
	// WHY NOTHING IS DONE ABOUT IT HERE, having been assessed. The obvious guard — the replay declining
	// to enqueue for a user whose deletion is in flight — does not close it: the failing interleaving is
	// the one where the replay commits FIRST, so any check it makes against its own snapshot sees a user
	// who is still there. The guard that WOULD close it is the replay taking a CONFLICTING lock on the
	// users row (FOR SHARE / FOR KEY SHARE) before enqueueing, and that is not safe: the replay would
	// hold the dead_letter_jobs row and want users, while this transaction holds users and wants that
	// dead_letter_jobs row — a genuine lock cycle, i.e. trading an unbounded wait for exactly the
	// SQLSTATE 40P01 class this file has spent several rounds engineering out. Sweeping river_job from
	// this transaction is a design change on the most sensitive path in this codebase and is
	// deliberately not made. The residual is therefore recorded rather than closed. The same note is
	// written from the other side at DeadLetterService.Retry.
	if _, err = tx.Exec(ctx, `DELETE FROM dead_letter_jobs WHERE user_id = $1`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: erase dead_letter_jobs: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id = $1`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: erase idempotency_keys: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM mcp_audit_log WHERE actor_user_id = $1`, userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: erase mcp_audit_log: %w", err)
	}

	// Issue #1507: THE TERMINAL AUDIT ENTRY, WRITTEN IN THE TRANSACTION IT DESCRIBES.
	//
	// This used to be ExecuteDeletion's own `s.auditLogger.Log(ctx, "user.deleted", ...)`, issued
	// AFTER this transaction had committed and with its error routed to a log line. So the one
	// entry that says "this person's data was erased" was the one entry that could be missing while
	// the erasure it describes was permanent and unretriable — the exact inversion of what an audit
	// trail is for, and the same "irreversible step taken, durable record missing" shape as the
	// plaid_orphaned_items INSERT above. It is now the last statement before COMMIT: either both
	// the erasure and its record land, or neither does.
	//
	// WHAT A FAILURE HERE DOES: IT ABORTS THE DELETION. Deliberate, and the harder of the two
	// choices to defend, so the reasoning is stated rather than implied.
	//
	//   - DATABASE STATE is rolled back, so the erasure is deferred rather than denied. Unlike every
	//     other audit write in this codebase, this one is inside a transaction that has not
	//     committed, so there IS something to roll back. The deferred tx.Rollback restores the users
	//     row and everything FK-linked to it, ExecuteDeletion's error path releases the work claim,
	//     and the deletion is genuinely re-attempted: AccountDeletionWorker returns the error to
	//     River (retry with backoff) and ScheduledDeletionCheckWorker/RetentionCleanupCheckWorker
	//     re-select the user on their next sweep, since nothing about their eligibility changed.
	//   - ⚠ BUT "NOTHING IS LOST" WOULD BE FALSE, AND THE HONEST VERSION MATTERS. By the time this
	//     statement runs, ExecuteDeletion has ALREADY disconnected the user's Plaid items and may
	//     already have cancelled their Paddle subscription — external, irreversible, and NOT undone
	//     by this rollback. So a failure here leaves a user who exists but has been partially
	//     externally erased, and a persistently-failing audit INSERT (an INSERT privilege revoked on
	//     audit_logs while UPDATE/DELETE survive, a new CHECK or RLS policy, a trigger — audit-
	//     specific modes that do NOT take the COMMIT with them, and exactly what
	//     TestDeleteUserCascade_Integration_AuditWriteFailureAbortsTheWholeCascade injects) would
	//     gate that user's erasure for as long as it lasts. That is a real cost of this choice and
	//     it is not hypothetical.
	//   - IT IS STILL THE RIGHT CHOICE, because the two failure directions are not symmetric. Abort
	//     leaves a LOUD, RECOVERABLE state: every attempt returns an error, the user still exists and
	//     is still a data subject with an intact record, and an operator who restores the write can
	//     complete the erasure. "Proceed and log" leaves a SILENT, UNRECOVERABLE one: the person is
	//     gone and nothing durable says it happened, with nothing left to retry from — which is
	//     precisely the defect #1507 exists to close, and re-adopting it inside the transaction would
	//     make this fix cosmetic. A blocked erasure is an operational incident someone can fix; an
	//     unrecorded erasure is permanent.
	//   - AND "PROCEED AND LOG" IS NOT ACTUALLY AVAILABLE HERE ANYWAY, which is worth stating because
	//     it is the thing a future reader will assume it can reach for. A failed statement leaves
	//     this transaction in the aborted state; every subsequent statement, including the COMMIT
	//     below, then fails too. Verified by mutation: replacing this `return` with `err = nil` does
	//     not produce "user erased, no audit row" — it produces a COMMIT that reports
	//     "commit unexpectedly resulted in rollback", i.e. the same abort with a worse error
	//     message. Making the failure genuinely survivable would take a SAVEPOINT around this
	//     INSERT, which is deliberately NOT done: it would reconstruct the pre-#1507 defect inside
	//     the transaction that was supposed to have closed it.
	//   - ⚠ ONE CONSEQUENCE THAT IS PRE-EXISTING RATHER THAN NEW, but which this statement widens the
	//     entrance to: an ERROR from this method (as opposed to a (false, nil) refusal) deliberately
	//     records no Paddle-cancel residue — see ExecuteDeletion's own comment, "a run that is still
	//     expected to complete records nothing". That rule was written for transient errors. Under a
	//     PERSISTENT audit failure the run is not in fact expected to complete, and the residue goes
	//     unrecorded. Recording residue for every cascade error would contradict the documented rule
	//     for the transient case, so it is left alone here and noted instead.
	//
	// Pinned in both directions by
	// TestDeleteUserCascade_Integration_AuditWriteFailureAbortsTheWholeCascade (state rolled back,
	// not merely an error returned) and its mandatory sibling
	// TestDeleteUserCascade_Integration_AuditEntryIsWrittenExactlyOncePerDeletion. Note that the
	// sibling calls THIS method directly, so it proves "the cascade writes exactly one row"; the
	// separate claim that no SECOND row is written by the service layer is proved end-to-end by
	// TestExecuteDeletion_Integration_RetentionCandidate_IsDeleted, which runs ExecuteDeletion.
	//
	// AFTER `DELETE FROM users`, NOT BEFORE, for two independent reasons: the entry records a
	// completed erasure, and the redaction UPDATE far above (`SET changes = NULL WHERE
	// resource_id = $1 ...`) runs earlier, so this row's NULL `changes` is NULL because nothing was
	// written to it — not because it was redacted. It survives the DELETE because audit_logs.user_id
	// is the only FK to users and it is written NULL, while resource_id has no FK at all.
	//
	// ⚠ LOCK ORDER IS UNPERTURBED, and that is a claim about this statement specifically. With
	// user_id NULL the FK's constraint trigger short-circuits before it touches users at all (MATCH
	// SIMPLE treats a NULL key as satisfied), so this INSERT takes a RowExclusiveLock on audit_logs
	// and NO row lock on users, households, or anything else — it can neither extend nor reorder the locks
	// this transaction already holds (households FOR NO KEY UPDATE, then users FOR UPDATE), so the
	// AcceptInvite invariant documented at the household lock above still holds unchanged. That is
	// true ONLY while the actor is NULL: see LogInTx's #1506 warning, which is where a future
	// implementer adding an actor will trip over it.
	if err = r.auditLog.LogInTx(ctx, tx, "user.deleted", "user", userID); err != nil {
		return false, nil, fmt.Errorf("DeleteUserCascade: record the terminal audit entry: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return false, nil, err
	}

	return true, affectedOwnerIDs, nil
}

// CountUncleanedLinkedItems implements ProfileRepository.CountUncleanedLinkedItems — see that
// interface method's doc comment (Issue #1494) for why this exists and what it must not be used for.
//
// Shares uncleanedLinkedItemPredicate with the C2 guard verbatim rather than restating it. A COUNT is
// used instead of the guard's EXISTS because the whole value of this call is the NUMBER it puts in an
// operator-facing error message; the extra cost is a count over rows a single user_id equality has
// already narrowed to at most a handful.
func (r *profileRepository) CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM linked_items WHERE user_id = $1 AND `+uncleanedLinkedItemPredicate,
		userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountUncleanedLinkedItems: %w", err)
	}
	return count, nil
}

// HouseholdOwnerBlocksDeletion implements ProfileRepository.HouseholdOwnerBlocksDeletion — see that
// interface method's doc comment (round 15, D2) for why this exists and what it must not replace.
//
// Shares householdOwnerBlocksDeletionClause with DeleteUserCascade's in-transaction guard VERBATIM,
// for the same reason CountUncleanedLinkedItems shares uncleanedLinkedItemPredicate: a hand-written
// second spelling of the same question is how #1444 (Site C) happened, and the copy that diverged was
// the one nobody re-read. If this answers differently from the guard, the only permissible cause is
// that the underlying rows changed in between.
func (r *profileRepository) HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error) {
	var blocks bool
	if err := r.db.QueryRow(ctx, `SELECT `+householdOwnerBlocksDeletionClause, userID).Scan(&blocks); err != nil {
		return false, fmt.Errorf("HouseholdOwnerBlocksDeletion: %w", err)
	}
	return blocks, nil
}

// DeletionResidueEffect names the irreversible external effect an account deletion had ALREADY
// performed by the time it stopped without deleting the user (Issue #1504). It is a fixed, closed
// vocabulary rather than a free-form string because it becomes part of audit_logs.action, which is
// what an operator queries on: a typo'd or ad-hoc value would be a record nobody finds.
type DeletionResidueEffect string

// DeletionResiduePaddleSubscriptionCancelled is the IRREDUCIBLE effect this vocabulary was built
// for: the Paddle subscription-cancel call was issued and the deletion then stopped (revocation
// caught by the customer-archive gate, the cascade's own guard refusing, or any of the ways a run
// simply ends), so the user keeps their account with their subscription cancelled.
//
// ⚠ ROUND 15 WIDENED IT FROM "LANDED" TO "ISSUED", deliberately. It used to be written only after
// CancelSubscriptionImmediately had RETURNED SUCCESSFULLY, which silently excluded the case where
// Paddle acted and the response was lost (finding A2) — the ambiguity is unresolvable from this side,
// and reporting nothing resolved it in the one direction that costs a real user a real subscription.
//
// The Plaid leg has no counterpart here on purpose: a disconnect is RECOVERABLE — the user relinks —
// and it is already reported to the user in-product. This constant is for the effect nobody can undo
// without a human intervening.
const DeletionResiduePaddleSubscriptionCancelled DeletionResidueEffect = "paddle_subscription_cancelled"

// DeletionResiduePaddleSubscriptionCancelledCascadeFailed is the SAME harm as the constant above —
// account kept, subscription irreversibly cancelled at Paddle — reached the third way: the cascade
// returned an ERROR rather than declining, and the error is not one this codebase recognises as
// transient (see cascadeErrorIsRecognisedTransient, deletion_service.go).
//
// WHY IT IS A SEPARATE KIND RATHER THAN THE SAME ONE. Both rows describe the same loss to the same
// user, but they hand an operator two different jobs. A `paddle_subscription_cancelled` row means the
// deletion is OVER (the user revoked it, or the cascade's guard refused): the account is stable and
// the only task is to restore the subscription. A `..._cascade_failed` row means the deletion is
// still STUCK and will re-run — the underlying database fault has to be fixed as well, and until it
// is, every retry re-cancels nothing but re-writes this record. Collapsing the two would hide the
// second task behind the first.
//
// It shares deletionResidueActionPrefix, so the operator query, the partial index (migration 000138)
// and both Prometheus gauges pick it up with no change: the vocabulary widened, the namespace did not.
const DeletionResiduePaddleSubscriptionCancelledCascadeFailed DeletionResidueEffect = "paddle_subscription_cancelled_cascade_failed"

// DeletionResiduePaddleCustomerArchived is the effect the code that performs it calls "THE ONE
// ACTION NOTHING CAN UNDO" (cleanupPaddleResources, deletion_service.go): the Paddle customer is
// archived and its email overwritten with deleted_<id>@deleted.local. A cancelled subscription can
// be re-created; an anonymised Paddle customer cannot be recovered by us or by Paddle.
//
// ⚠ ROUND 15, FINDING A1 — WHY IT DID NOT EXIST UNTIL NOW, AND WHY THAT WAS THE ROOT DEFECT. Both
// constants above name the SUBSCRIPTION, and the service's whole recording mechanism keyed off a
// single in-memory `subscriptionCancelled` bool set only by the cancel. The archive set no flag and
// had no vocabulary, so an archive-WITHOUT-a-cancel could not be recorded by any path — and that is
// not a corner: it is the deterministic shape of the retention sweep meeting an ex-subscriber
// (subscription_status='cancelled', paddle_customer_id still set) whose Plaid removal failed
// terminally. The archive lands, the cascade's C2 guard then refuses for seven days, and an Info log
// was the entire trace of an unrecoverable act.
//
// It is a SEPARATE kind rather than a reuse of the subscription constants because it hands an
// operator a different job: there is no subscription to restore, and there is nothing to restore at
// Paddle at all. What they can do is reconcile — the local users row survived and still names the
// customer id, so the person can be contacted and a fresh Paddle customer created if they resubscribe.
//
// Shares deletionResidueActionPrefix, so the operator query, migration 000138's partial index
// (predicate `action LIKE 'user.deletion_residue.%'`) and both Prometheus gauges pick it up with NO
// migration: the vocabulary widened, the namespace did not. Verified, not assumed — see
// TestRecordDeletionResidue_Integration_CustomerArchived_IsCountedByTheExistingNamespace.
const DeletionResiduePaddleCustomerArchived DeletionResidueEffect = "paddle_customer_archived"

// DeletionResiduePaddleCustomerArchivedCascadeFailed is to DeletionResiduePaddleCustomerArchived
// exactly what DeletionResiduePaddleSubscriptionCancelledCascadeFailed is to
// DeletionResiduePaddleSubscriptionCancelled: the same loss, reached the way that means the deletion
// is STILL STUCK rather than over — the cascade returned an error this codebase does not recognise as
// transient (cascadeErrorIsRecognisedTransient, deletion_service.go).
//
// ⚠ ROUND 15, TIER E ITEM 8 — WHY IT WAS MISSING, AND WHY THE GAP WAS NOT COSMETIC. The "stuck" row
// was gated on `effects.subscriptionCancel`, and the previous batch recorded that as deliberate: the
// constant NAMES the subscription, so an archive-only run "simply does not get the second row". That
// reasoning is correct about the constant and wrong about the operator. The two constants encode two
// different JOBS — "the deletion is over, restore what was lost" versus "the deletion is stuck, fix
// the database fault too, and until you do every retry rewrites this" — and an archive-only run that
// fails non-transitively is stuck in precisely the same way. The landing record and
// reportSurvivingResidue's Error log do report the loss, but neither distinguishes stuck from over,
// which is the whole reason the second kind exists. An operator filtering for stuck deletions found
// every ex-subscriber's and none of the archive-only ones.
//
// The fix is a parallel constant rather than widening the subscription one, because widening it
// would put "the subscription was cancelled" in an operator-facing action for a run in which no
// subscription was cancelled — the mislabelling the gate was, rightly, avoiding.
//
// Shares deletionResidueActionPrefix, so the operator query, migration 000138's partial index and
// both Prometheus gauges pick it up with NO migration, for the same reason its three siblings do.
const DeletionResiduePaddleCustomerArchivedCascadeFailed DeletionResidueEffect = "paddle_customer_archived_cascade_failed"

// deletionResidueActionPrefix is the audit_logs.action namespace these records live in. Kept as one
// constant so the operator query in RecordDeletionResidue's doc comment
// (action LIKE 'user.deletion_residue.%') cannot drift away from what is actually written.
const deletionResidueActionPrefix = "user.deletion_residue."

// RecordDeletionResidue implements ProfileRepository.RecordDeletionResidue — see that interface
// method's doc comment for why this is an audit_logs row keyed to the user, and why it carries no
// `changes` payload (Issue #1504).
func (r *profileRepository) RecordDeletionResidue(ctx context.Context, userID string, effect DeletionResidueEffect) error {
	if effect == "" {
		// Refuse rather than write 'user.deletion_residue.' — a record whose action says nothing is
		// worse than none, because it still looks like the operator has been told something.
		return fmt.Errorf("RecordDeletionResidue: an effect is required for user %s", userID)
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, $1, 'user', $2)`,
		deletionResidueActionPrefix+string(effect), userID)
	if err != nil {
		return fmt.Errorf("RecordDeletionResidue: %w", err)
	}
	return nil
}

// DeletionClaimKind names WHICH of the two mutually exclusive authorisations a hard-deletion claim
// is being made under. It is declared by the CALLER — never derived from the row's current state —
// because the whole point of the split (Issue #1440, T0) is that a stale caller's authorisation
// must be re-checked against the rule it was actually issued under, and current row state is
// exactly the thing that may have changed underneath it.
//
// There are precisely two production authorisations, and they are distinguished by which query
// selected the user (see ClaimForDeletion's doc comment for the enumeration of callers):
//
//   - DeletionKindUserRequested — the user asked to be deleted (DeletionService.RequestDeletion →
//     MarkForDeletion → GetUsersScheduledForDeletionPage → ScheduledDeletionCheckWorker enqueues
//     AccountDeletionArgs → AccountDeletionWorker → ExecuteDeletion).
//   - DeletionKindRetention — nobody asked; the retention clock on a long-inactive account expired
//     (GetUsersWithExpiredRetentionPage → RetentionCleanupCheckWorker.deleteExpiredAccounts →
//     ExecuteDeletion, an in-process call, NOT a River job).
type DeletionClaimKind string

const (
	// DeletionKindUserRequested is the authorisation carried by a user's own GDPR deletion
	// request whose grace period has elapsed.
	DeletionKindUserRequested DeletionClaimKind = "user_requested"
	// DeletionKindRetention is the authorisation carried by an expired data-retention clock on an
	// account nobody asked to delete.
	DeletionKindRetention DeletionClaimKind = "retention"
)

// userRequestedDeletionClaimPredicate authorises a hard delete on the strength of the user's OWN
// outstanding GDPR deletion request.
//
// The clause that actually closes Issue #1440's T0 defect is `deletion_scheduled_at <= NOW()`. It
// does BOTH jobs, because CancelDeletion NULLs deletion_scheduled_at as well as
// deletion_requested_at:
//
//   - Cancellation: the column is NULL, `NULL <= NOW()` is NULL, and a NULL in a WHERE clause is
//     not true — so the claim is refused. Before the split, a cancelled user was authorised
//     anyway, through the retention rule's "no manual tier override at all" clause, which is true
//     of every ordinary uncomped user whether they cancelled or not. Cancelling therefore did not
//     reliably stop the deletion — data loss that inverts GDPR intent. Proved by
//     TestExecuteDeletion_Integration_EpisodeCancelled_StaleJobDoesNotDelete (internal/services)
//     and TestClaimForDeletion_Integration_UserRequested_Cancelled_NotClaimed.
//   - Cancel-then-re-request: the SECOND episode's window is still open, so a job queued for the
//     first one no-ops instead of deleting immediately. deletion_requested_at cannot express this
//     at all — MarkForDeletion overwrites it, so a stale job reads the NEW value and sees nothing
//     wrong. Proved by TestExecuteDeletion_Integration_EpisodeReRequested_StaleJobDoesNotDeleteEarly
//     and TestClaimForDeletion_Integration_UserRequested_ReRequestedNotYetDue_NotClaimed.
//
// The other two clauses are honestly NOT independently load-bearing today, and this comment says so
// rather than implying otherwise — mutation-testing each clause separately is what established it:
//
//   - `deletion_requested_at IS NOT NULL` is DEFENCE IN DEPTH, not the cancellation guard. Removing
//     it alone changes no current behaviour, because CancelDeletion clears both columns together.
//     It exists so that a future writer which cleared only ONE of the two — the exact partial-state
//     bug this codebase has already shipped twice in adjacent code — cannot turn into a wrongful
//     hard delete. Pinned by
//     TestClaimForDeletion_Integration_UserRequested_RequestClearedButScheduleRemains_NotClaimed,
//     which constructs that (currently unreachable) partial state directly.
//   - `deletion_scheduled_at IS NOT NULL` is strictly redundant with the comparison below it, for
//     the NULL-is-not-true reason above. It is kept only because it states the intent in a form a
//     reader does not have to reason about three-valued logic to verify.
//
// Deliberately NO manual-tier-override check: the ratified product rule is that a user's own GDPR
// deletion request beats an admin comp, so a comped user who has explicitly asked to be deleted
// must still be deleted. Adding an override check here would illegally strand them — a GDPR
// compliance problem, not just a bug. Pinned by
// TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims.
const userRequestedDeletionClaimPredicate = `
	  AND deletion_requested_at IS NOT NULL
	  AND deletion_scheduled_at IS NOT NULL
	  AND deletion_scheduled_at <= NOW()
`

// retentionDeletionClaimPredicate authorises a hard delete on the strength of an expired retention
// clock, for an account nobody asked to delete.
//
// It re-verifies EVERY condition that made the user a candidate in the first place, against
// committed state at claim time: `deletion_requested_at IS NULL`, noManualOverrideAtAllPredicate,
// and — since #1440 T0-retention — expiredRetentionClockPredicate, which is the very text
// GetUsersWithExpiredRetentionPage selects on (spliced, not restated, so the two cannot drift).
//
// #1440 T0-retention (Codex, High) is what expiredRetentionClockPredicate closes here, and the
// defect was reachable rather than theoretical. This predicate used to check only the deletion-request and override rules, and
// both of those stay TRUE for a user who RESUBSCRIBES: the reactivation webhook path
// (UpdateSubscriptionFromPaddle with an active status, then ClearRetentionDates — see
// subscription_webhook.go) writes subscription_status = 'active' and NULLs retention_started_at,
// retention_expires_at and both notice stamps, and touches neither deletion_requested_at nor
// manual_tier_override. The retention sweep selects a PAGE of candidates and then processes it row
// by row in process (GetUsersWithExpiredRetentionPage →
// RetentionCleanupCheckWorker.deleteExpiredAccounts → ExecuteDeletion), so the window between
// selection and any one row's claim is real and unbounded. A resubscription inside that window
// therefore used to end with ExecuteDeletion cancelling the customer's brand-new subscription
// (cleanupPaddleResources acts on the row's raw status, which is now 'active') and hard-deleting
// them. This is the same structural flaw as Issue #1440's T0 — an authorisation not re-checked
// against the rule it was issued under — in the sibling path T0 did not touch.
//
// NOW() rather than a caller-supplied instant, deliberately, matching
// userRequestedDeletionClaimPredicate's `deletion_scheduled_at <= NOW()` and the candidate query's
// own `retention_expires_at <= NOW()`. Three reasons, in order of weight: (1) this is a write-side
// guard inside a conditional UPDATE, and the convention this repo settled on in #1466 is that
// read-side response predicates take the threaded asOf while write guards keep the transaction
// clock — the "a write that no longer reflects reality must not win" rule wants the DATABASE's
// committed now, not a Go clock captured before the I/O; (2) ClaimForDeletion is not given an
// instant and does not need one, so threading one would add a parameter and a second clock
// convention to a predicate whose sibling already uses NOW(); (3) this same constant is spliced
// into deletionAuthorisationUnionPredicate, whose consumer (DeleteUserCascade) binds its own
// positional parameters — a bind param here would have to be threaded through two unrelated
// statements' argument lists, which is exactly how a predicate acquires a drift hazard.
//
// noManualOverrideAtAllPredicate — "no override AT ALL, live or already lapsed" — not
// noActiveManualOverridePredicate ("live right now"). Issue #1413 Critical finding C1: a comp
// pauses the retention clock while live and is only re-armed by the independently scheduled
// manual_tier_override_expiry_check job, so a comp that lapsed moments before that job clears the
// column would pass a live-only check and be hard-deleted in the ordering gap between the two
// jobs. That is honestly a delay, not a guaranteed bound: it holds only while
// manual_tier_override_expiry_check keeps succeeding daily — that job's own failures are the thing
// to watch. See noManualOverrideAtAllPredicate's own doc comment for the full race, and
// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed /
// TestClaimForDeletion_Integration_ClearedOverride_Claims for the real-DB proof that it is a delay
// and not a permanent exemption.
//
// `deletion_requested_at IS NULL` is a TIGHTENING relative to the pre-split predicate, which
// treated an outstanding request as an additional reason to allow a retention claim. A user who
// requests deletion between the retention candidate query and this claim is entitled to their full
// grace period and their cancellation window; the retention path must not short-circuit either.
// Such a user is picked up by the user-requested path once their own window elapses.
const retentionDeletionClaimPredicate = `
	  AND deletion_requested_at IS NULL
	` + expiredRetentionClockPredicate + retentionNoticesDeliveredPredicate + noManualOverrideAtAllPredicate

// expiredRetentionClockPredicate is the "this account's retention clock has actually run out, and
// the account is still unsubscribed" test — the pair of facts that makes a user a retention
// hard-delete candidate at all.
//
// Shared verbatim by GetUsersWithExpiredRetentionPage (candidate selection) and
// retentionDeletionClaimPredicate (the claim that authorises the irreversible act), in the manner
// uncleanedLinkedItemPredicate is shared by its two consumers, so the two can never disagree.
// Drift between exactly these two was #1440 T0-retention: the candidate query required both
// conditions and the claim required neither, so a resubscription between them was not noticed.
// Restating this text in either place instead of splicing it re-opens that defect.
//
// Which of the three conjuncts is load-bearing, established by removing each one separately rather
// than assumed — the same honesty the userRequestedDeletionClaimPredicate comment above applies to
// its own clauses:
//
//   - `retention_expires_at <= NOW()` and the status test are each independently load-bearing.
//     Removing the first lets a RESTARTED retention window be deleted before it elapses
//     (TestExecuteDeletion_Integration_RetentionExpiryInFuture_NotDeleted reddens alone); removing
//     the second lets an ACTIVE, paying subscriber whose stale expired clock survived a
//     best-effort ClearRetentionDates failure be deleted
//     (TestExecuteDeletion_Integration_RetentionResubscribedClockClearFailed_NotDeleted reddens
//     alone).
//   - `retention_expires_at IS NOT NULL` is NOT independently load-bearing today, and removing it
//     alone reddens nothing: `NULL <= NOW()` is NULL, a NULL in a WHERE clause is not true, so the
//     comparison already refuses a cleared clock. It is kept because it states the intent in a form
//     a reader does not have to reason about three-valued logic to verify, and because the candidate
//     query it is shared with genuinely needs it for the same reason. Do not read its presence as
//     the thing that protects a resubscribed user — the comparison is.
//
// NOT applied to GetUsersNeedingRetention30dNotice/7dNotice, which need a DIFFERENT clock test
// (`retention_expires_at > NOW()` plus an interval window — the notice is sent BEFORE expiry, so
// their bound is the opposite of this one) even though they share the status half. Nor to
// GetUsersNeedingRetentionClockRepair, whose whole population is `retention_expires_at IS NULL`.
// Sharing this constant with any of them would be a false generalisation.
const expiredRetentionClockPredicate = `
	  AND retention_expires_at IS NOT NULL
	  AND retention_expires_at <= NOW()
	  AND subscription_status IN ('inactive', 'cancelled')
`

// retentionNoticesDeliveredPredicate is the Issue #1569 T1 gate: BOTH promised pre-deletion notices
// must have actually been recorded before a retention hard-delete is a candidate or a claim.
//
// WHY IT EXISTS. apps/web/src/legal/PrivacyPolicy.tsx promises a cancelled user notice at 30 days
// and again at 7 days before their data is destroyed. Nothing enforced that promise: this pair of
// columns was referenced by the notice queries only, never by the destruction path, while
// cmd/server/main.go passes no EmailService to RetentionCleanupCheckWorker in any environment — so
// canSendRetentionNotice returned false before the work-claim was even taken and the markers stayed
// NULL forever. The net effect was a hard delete at the end of the window with zero warnings sent.
// The comment above userSelectQuery said outright that this query "does not reference them at all";
// that was accurate, and it was the defect.
//
// BOTH, not either. The 7-day final warning is a separate promise from the 30-day one, so a user who
// received only one has not received what the policy states. Each of the four marker combinations
// is pinned by TestRetentionNoticeGate_Integration_UnwarnedUserIsNotADeletionCandidate, including
// the both-recorded case that proves this is a gate and not a permanent halt.
//
// ⚠ THIS GATE CAN HOLD AN ACCOUNT INDEFINITELY, and that is deliberate but must never be silent.
// With no email service wired the markers can never be stamped, so an expired account is retained
// rather than deleted — the safe direction to fail, per the #1569 ruling, but one that trades a
// silent destruction for a silent retention if nothing reports it. CountRetentionDeletionsWithheld
// below, published by RetentionCleanupCheckWorker's Step 2b as
// observability.RetentionDeletionsWithheld, is the other half of this fix and is not optional.
//
// SAFE TO RE-CHECK AT CLAIM TIME (retentionDeletionClaimPredicate splices this too, so the two can
// never drift the way they did in #1440 T0-retention). The markers are not strictly monotonic —
// ClearRetentionDates and admin_user_repository.go's re-arm both NULL them — but neither can leave
// a row that this gate then refuses unfairly. ClearRetentionDates NULLs retention_expires_at in the
// SAME statement, which expiredRetentionClockPredicate already refuses. The admin re-arm is the
// subtler one and is worth spelling out so nobody re-derives it: it sets a FUTURE
// retention_expires_at, which WOULD restore the notice sweep's reach — and it fires only for a user
// carrying a manual_tier_override, whom noManualOverrideAtAllPredicate excludes from this query
// anyway. So the re-check can only ever agree with a refusal another clause has already made; it
// can never turn a legitimate candidate into a false refusal.
const retentionNoticesDeliveredPredicate = `
	  AND retention_notice_30d_sent_at IS NOT NULL
	  AND retention_notice_7d_sent_at IS NOT NULL
`

// retentionNoticesMissingPredicate is the EXACT logical negation of
// retentionNoticesDeliveredPredicate above — the rows that predicate refuses.
//
// Exact, not approximately so, and this is worth stating because a "reason" that is true of most
// cases and asserted of all of them is how six false claims shipped on this branch. `IS NULL` and
// `IS NOT NULL` are TOTAL predicates over a nullable timestamptz: neither ever evaluates to NULL,
// so De Morgan applies without the three-valued-logic caveat that would break the same rewrite over
// `=`. NOT (a IS NOT NULL AND b IS NOT NULL) ≡ (a IS NULL OR b IS NULL), for every row, always.
//
// It is written out rather than derived because SQL has no negation operator over a spliced
// fragment; TestRetentionWithheldEnumeration_Integration_AgreesWithTheCountAndIsDisjointFromTheSweep
// is what actually holds the two to being complements — it asserts, per seeded expired account,
// that it is a deletion candidate XOR an enumerated withheld row.
const retentionNoticesMissingPredicate = `
	  AND (retention_notice_30d_sent_at IS NULL OR retention_notice_7d_sent_at IS NULL)
`

// retentionWithheldPopulationPredicate selects the accounts the Issue #1569 T1 notice gate is
// HOLDING BACK from hard deletion: every condition GetUsersWithExpiredRetentionPage requires,
// except that at least one promised pre-deletion notice was never recorded.
//
// ⚠ ONE CONSTANT, TWO CONSUMERS, DELIBERATELY (Issue #1584). CountRetentionDeletionsWithheld
// reports how many there are; GetRetentionDeletionsWithheldPage enumerates which ones. Before this
// constant existed the count restated the predicate inline, and a second consumer restating it
// again is exactly the drift that produced #1440 T0-retention — a dashboard reading "N withheld"
// above a list that does not contain N rows is worse than either surface alone, because it makes
// an operator distrust both. Splicing expiredRetentionClockPredicate and
// noManualOverrideAtAllPredicate (rather than restating them) keeps the pair tied to the sweep they
// are the complement of; `is_deleted = FALSE` and `deletion_requested_at IS NULL` are restated
// exactly as GetUsersWithExpiredRetentionPage itself restates them.
//
// ⚠ WHY A COMPED ACCOUNT IS EXCLUDED even though its notices are also unrecorded: the sweep does
// not want to delete it in the first place (noManualOverrideAtAllPredicate), so the notice gate is
// not what is holding it. Listing it here would misattribute the reason and put a healthy comp on
// an operator's exception list.
//
// Begins with WHERE, so it is spliced as the whole clause and callers append only ordering,
// keyset and limit.
const retentionWithheldPopulationPredicate = `
		WHERE is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + expiredRetentionClockPredicate + noManualOverrideAtAllPredicate + retentionNoticesMissingPredicate

// deletionAuthorisationUnionPredicate is satisfied when a claim of EITHER kind would be authorised.
// It is composed from the two kind predicates above rather than restated, so it cannot drift away
// from them.
//
// Its only consumer is DeleteUserCascade's in-transaction re-check, which — unlike ClaimForDeletion
// — is not told which kind authorised the run. It does not need to be: it ALSO requires
// deletion_requested_at IS NOT DISTINCT FROM the exact value the caller observed, which pins the
// episode. Combined with the union above, that is equivalent to the kind-specific rule in every
// reachable case:
//
//   - a user-requested run has a non-NULL expected value, so the exact-value check forces the
//     union's first branch (the second requires NULL) and thus re-checks deletion_scheduled_at;
//   - a retention run has a NULL expected value, so it forces the second branch and thus re-checks
//     the override rule AND — since #1440 T0-retention — the retention clock and subscription status
//     (expiredRetentionClockPredicate), which is what stops a resubscription landing in the
//     external-cleanup window from being hard-deleted by the cascade after passing an earlier claim.
//     That tightening reached this re-check for free, which is the entire point of composing this
//     constant from the two kind predicates instead of restating them; verified by
//     TestDeleteUserCascade_Integration_RetentionResubscribedAfterClaim_NotDeleted, which fails if
//     the composition is replaced by a restatement that omits it.
//
// Nothing can change deletion_scheduled_at without also changing deletion_requested_at, so pinning
// deletion_requested_at pins the whole episode, including its schedule.
//
// The full writer set is FOUR statements, not two — an earlier version of this comment said "the
// only two writers … verified by grep over this file", which was an incomplete enumeration: grepping
// for `deletion_scheduled_at =` finds UPDATEs and misses INSERT column lists. Enumerated properly
// (every occurrence of either column name across non-test Go plus every migration):
//
//   - CreateUser (INSERT, this file) — writes both, adjacent bind params from the same domain.User.
//   - CreateUserTx (INSERT, this file) — likewise.
//   - MarkForDeletion (UPDATE, this file) — sets both.
//   - CancelDeletion (UPDATE, this file) — NULLs both.
//
// The invariant survives the correction: all four write both columns in a single statement, so the
// pair is never observable half-updated. The two INSERTs cannot produce a mid-episode row either —
// they create the row, so there is no prior episode for a stale claim to be holding. No migration
// writes either column (they only declare, index, or filter on them), and there is no column
// DEFAULT or trigger on either.
const deletionAuthorisationUnionPredicate = `
	  AND (
	    (TRUE` + userRequestedDeletionClaimPredicate + `)
	    OR (TRUE` + retentionDeletionClaimPredicate + `)
	  )
`

// deletionClaimPredicateFor maps a DeletionClaimKind to its SQL predicate, failing CLOSED on an
// unrecognised kind: an unknown authorisation must never be treated as permission to hard-delete a
// user, and the zero value of DeletionClaimKind ("") deliberately falls into this branch so a
// caller that forgets to set it gets an error rather than a deletion.
func deletionClaimPredicateFor(kind DeletionClaimKind) (string, error) {
	switch kind {
	case DeletionKindUserRequested:
		return userRequestedDeletionClaimPredicate, nil
	case DeletionKindRetention:
		return retentionDeletionClaimPredicate, nil
	default:
		return "", fmt.Errorf("unknown deletion claim kind %q", string(kind))
	}
}

// ClaimForDeletion atomically verifies, at the moment of the claim, that a user is still
// eligible for ExecuteDeletion's irreversible work UNDER THE AUTHORISATION `kind` NAMES. See the
// ProfileRepository interface doc for the full rationale and ordering requirement (Issue #1413
// Critical finding C2, Codex review round 2). This is a self-assigning no-op write (updated_at
// only) purely to obtain an atomic, row-level check via a single UPDATE ... WHERE statement,
// mirroring ClaimPlaidDisconnectIfPastDue's shape — without adding a new "claimed" column.
//
// Issue #1440, T0 — why `kind` exists, and why it cannot be inferred here: this method used to
// take only a userID and evaluate the UNION of both authorisations, which meant a caller acting on
// a user-requested deletion could be authorised by the RETENTION rule instead. That is not a
// theoretical mismatch: the retention rule allows any uncomped user, so a job queued for a
// deletion request the user had since cancelled sailed straight through it. Inferring the kind from
// the row's current state cannot fix that, because the cancelled row looks exactly like a retention
// candidate — the authorisation has to come from the caller, which knows which query selected this
// user.
//
// The two production callers, both reached via DeletionService.ExecuteDeletion:
//
//   - AccountDeletionWorker (queue/jobs/account_deletion.go), the only worker for the
//     `account_deletion` River job kind, enqueued only by ScheduledDeletionCheckWorker
//     (queue/jobs/scheduled_deletion_check.go) from GetUsersScheduledForDeletionPage
//     → DeletionKindUserRequested.
//   - RetentionCleanupCheckWorker.deleteExpiredAccounts (queue/jobs/retention_cleanup_check.go),
//     an in-process call from GetUsersWithExpiredRetentionPage → DeletionKindRetention.
//
// There used to be a third caller, workers.DeletionWorker — a ticker-based worker that was never
// wired into main.go. Issue #1482 deleted it; these two are now the complete caller set.
func (r *profileRepository) ClaimForDeletion(ctx context.Context, userID string, kind DeletionClaimKind) (bool, error) {
	predicate, err := deletionClaimPredicateFor(kind)
	if err != nil {
		return false, fmt.Errorf("ClaimForDeletion: %w", err)
	}
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET updated_at = NOW()
		WHERE id = $1 AND is_deleted = FALSE
	`+predicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("ClaimForDeletion: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// DeletionStillAuthorised implements ProfileRepository.DeletionStillAuthorised — see that interface
// method's doc comment for why a read-only, kind-specific recheck exists alongside ClaimForDeletion
// and DeleteUserCascade's in-transaction guard.
//
// THE PREDICATE IS SPLICED, NOT RESTATED. It is the same deletionClaimPredicateFor(kind) text
// ClaimForDeletion enforces, plus DeleteUserCascade's own occurrence pin
// (`deletion_requested_at IS NOT DISTINCT FROM $2`). Restating either here would create a third
// hand-written variant of an authorisation rule — which is how #1440 T0 happened: the candidate query
// and the claim drifted apart, and a resubscription between them went unnoticed. A recheck that can
// disagree with the claim it is rechecking is worse than no recheck, because it reads as protection.
//
// NO `FOR UPDATE`, and no transaction. This is a read taken between external API calls, not a write
// guard; taking a row lock here would hold it across a Plaid or Paddle round-trip, which is exactly
// what DeleteUserCascade's lock is careful not to do. The spliced predicate's NOW() calls therefore
// read the statement's own clock, which is the fresh answer this method exists to provide.
func (r *profileRepository) DeletionStillAuthorised(ctx context.Context, userID string, kind DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error) {
	predicate, err := deletionClaimPredicateFor(kind)
	if err != nil {
		// Fail closed on an unrecognised kind, for the same reason deletionClaimPredicateFor does: an
		// unknown authorisation must never read as permission to proceed to an irreversible call.
		return false, fmt.Errorf("DeletionStillAuthorised: %w", err)
	}
	var authorised bool
	err = r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1 AND is_deleted = FALSE
			  AND deletion_requested_at IS NOT DISTINCT FROM $2
	`+predicate+`
		)
	`, userID, expectedDeletionRequestedAt).Scan(&authorised)
	if err != nil {
		return false, fmt.Errorf("DeletionStillAuthorised: %w", err)
	}
	return authorised, nil
}

// UpdateTrialStatus updates trial-related fields for a user
// Issue #679: Trial service and auto-grant logic
//
// Issue #1413 round-11 Fix 2 (High): also clears plaid_cleanup_pending_at when status makes the
// user Plaid-eligible — this method's only production caller (TrialService.GrantTrial) always
// passes SubscriptionStatusTrial, which is never repair-eligible, so this always clears in
// practice. Deriving the CASE from the `status` parameter (mirroring
// UpdateSubscriptionFromPaddle's own status-driven clearing), rather than hardcoding NULL, keeps
// this method honest against the invariant (domain.User.PlaidCleanupPendingAt's doc comment) even
// if a future caller ever passes a different status.
//
// The bug this closes: granting a trial to a cancelled user with an outstanding marker used to
// leave the STALE marker in place — trial is Plaid-eligible, so the invariant requires NULL here.
// The stale marker then survived until this NEW trial itself later expired, at which point
// ExpireTrialIfNoOverride's COALESCE(plaid_cleanup_pending_at, NOW()) preserved the OLD
// timestamp instead of starting a fresh grace window — the user could be disconnected immediately
// against an anchor from before this trial ever existed, rather than getting the grace period a
// fresh ineligibility episode is entitled to.
//
// CI-observed clock-skew fix, same defect and same fix as UpdateSubscriptionFromPaddle's own doc
// comment describes in full: `updated_at` and the plaid_cleanup_pending_at COALESCE both used to
// share a single Go-computed `now := time.Now()` query parameter instead of a Postgres-native
// clock — this method's own sibling instance of the identical bug, caught by the same audit, fixed
// the same way, for the same reason (every fence a test builds via `SELECT NOW()` must share
// Postgres's clock with whatever this method actually stamps, never the Go runner's).
//
// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this method's own marker
// COALESCE for the same transaction-start-vs-statement-execution risk as every sibling writer of
// this column; a bare substitution does not actually fix anything for a single, non-CTE UPDATE
// (verified empirically, see StampMissingPlaidCleanupMarkers's doc comment), and the real fix
// (restructuring into a locked CTE) was scoped out as a documented follow-up at the time.
//
// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
// Fixed "for consistency," per that finding: this method is latent in production (its only caller,
// TrialService.GrantTrial, always passes SubscriptionStatusTrial, which takes the ELSE/clearing
// branch below, never this COALESCE branch — see this method's own round-11 note above), but a
// future caller could reach it, and leaving one documented-but-unfixed sibling next to four other
// now-fixed ones is exactly the kind of trap this branch has already had to clean up once. Same
// shape, same FOR NO KEY UPDATE lock mode (never FOR UPDATE — see StampMissingPlaidCleanupMarkers's
// Finding 1 correction for the cross-user deadlock that mode would risk), same verified-fresh
// target-table column read for the COALESCE, as UpdateSubscriptionFromPaddle's own ROUND 23 fix —
// see that method's doc comment for the full mechanism. This method's WHERE clause (id = $5 AND
// is_deleted = FALSE) moves into the CTE unchanged, so RowsAffected() still reports 0 in exactly
// the same circumstance (missing/soft-deleted target) this method's own ErrUserNotFound check
// depends on.
func (r *profileRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	plaidMarkerEligible := domain.SubscriptionStatus(status).IsEligibleForPlaidDisconnectRepair()

	var query string
	if plaidMarkerEligible {
		query = `
			WITH candidate AS (
				SELECT id FROM users WHERE id = $5 AND is_deleted = FALSE FOR NO KEY UPDATE
			)
			UPDATE users
			SET subscription_status = $1,
			    subscription_tier = 'premium',
			    trial_started_at = $2,
			    trial_ends_at = $3,
			    had_trial = $4,
			    updated_at = NOW(),
			    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
			FROM candidate
			WHERE users.id = candidate.id
		`
	} else {
		query = `
			UPDATE users
			SET subscription_status = $1,
			    subscription_tier = 'premium',
			    trial_started_at = $2,
			    trial_ends_at = $3,
			    had_trial = $4,
			    updated_at = NOW(),
			    plaid_cleanup_pending_at = NULL
			WHERE id = $5 AND is_deleted = FALSE
		`
	}

	ct, err := r.db.Exec(ctx, query, status, trialStartedAt, trialEndsAt, hadTrial, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetEffectiveSubscriptionStatusByUserID retrieves the EFFECTIVE subscription status for a
// user — "active" when a non-expired manual tier override (admin comp) is in effect,
// otherwise the raw subscription_status (Issue #1413). Used by the read-only middleware
// (#930) for efficient per-request status checks.
//
// The override predicate is computed in Go via domain.User.EffectiveStatusWithActiveOverride
// rather than in SQL, so this row-level read shares exactly one definition of "is the override
// active" with every other caller of ComputeEffectiveStatus/ComputeEffectiveTier.
//
// Issue #1413 G2 — malformed override fails closed on THIS access path only: manual_tier_override
// is an unconstrained VARCHAR(50) with no CHECK constraint (a migration adding one would fail on
// any existing bad row, and tolerating such rows safely at read time is the whole point here), so
// its value can be garbage — a legacy row, a direct SQL write, or a write made while the tier
// config was nil (write-time validation in admin_user_service.go can be bypassed by all three).
// When an active override is in effect, its value is checked against the injected
// overrideTierValidator (see SetOverrideTierValidator) before "active" is trusted; a rejected
// override falls back to the RAW subscription_status and is logged at warn (a silent denial here
// would be indistinguishable from the original #1413 bug from the user's point of view).
//
// Deliberately NOT applied to the retention/deletion queries (noManualOverrideAtAllPredicate,
// below): "fail closed" means the OPPOSITE thing on that axis — a malformed override must still
// block deletion, not merely access, so those queries keep treating ANY non-empty override,
// valid tier name or not, as "hands off". Unifying the two would either reopen this access hole
// or start hard-deleting accounts whose only defect is a bad string in this column.
//
// If overrideTierValidator is nil (never wired via SetOverrideTierValidator), validation is
// skipped and an active override grants "active" unconditionally — the pre-G2 behavior. This is
// deliberate, not an oversight: main.go loads the tier config once at process startup and calls
// os.Exit(1) if that fails, so in production the validator is either wired for the entire process
// lifetime or the server never starts — there is no "config unavailable at read time" case to
// handle for the real deployment. Failing closed on a nil validator instead (denying every comp
// whenever some future construction path forgets to call SetOverrideTierValidator) would turn a
// wiring oversight into an instant outage for every currently-comped user, which is its own
// class of bug this fix is not trying to introduce.
//
// NOTE: this rename (from GetSubscriptionStatusByUserID) is deliberate, not cosmetic — the
// old name described a raw column read; this method returns a computed value. Keeping the
// old name would risk a future "simplification" silently reverting to the raw column.
func (r *profileRepository) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	var u domain.User
	err := r.db.QueryRow(ctx,
		`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`,
		userID,
	).Scan(&u.SubscriptionStatus, &u.ManualTierOverride, &u.ManualTierEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", err
	}

	status, overrideValue, hasOverride := u.EffectiveStatusWithActiveOverride()
	if hasOverride && r.overrideTierValidator != nil && !r.overrideTierValidator(overrideValue) {
		slog.WarnContext(ctx, "rejecting manual tier override on access path: value does not name a configured tier",
			"user_id", userID, "manual_tier_override", overrideValue)
		return u.SubscriptionStatus, nil
	}

	return status, nil
}

// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
// Issue #680: Trial expiration cron job
func (r *profileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at <= $1
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, time.Now())
}

// GetUsersWithExpiringTrials returns users whose trial ends within N days
// Issue #680: Trial expiration notification (3 days before)
func (r *profileRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	now := time.Now()
	threshold := now.Add(time.Duration(withinDays) * 24 * time.Hour)

	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at > $1
		  AND trial_ends_at <= $2
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, now, threshold)
}

// GetUsersWithActivePaddleSubscriptions returns users with active Paddle subscriptions
// Issue #762: Paddle admin tools reconciliation
func (r *profileRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + ` WHERE paddle_subscription_id_blind_idx IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// SetRetentionDates (re)starts the GDPR retention clock for a user: retention_started_at and
// retention_expires_at are computed entirely in Postgres via clock_timestamp() and
// clock_timestamp() + make_interval(months => $1) — see this method's doc comment on the
// ProfileRepository interface for why (Go's AddDate disagrees with Postgres's make_interval on
// month-end dates). Called when a user's subscription is cancelled or trial expires (#930).
//
// Issue #1500 residual (Codex review, PR #1443 follow-up): THIS WRITE IS ITS OWN GUARD. It used to
// key on `id = $2 AND is_deleted = FALSE` alone, which left a read-then-write race its callers'
// Go-side gates could narrow but never close: the cancellation webhook re-checks authorisation
// immediately before calling this (cancellationCleanupStillAuthorised, subscription_webhook.go),
// but an activation webhook committing between that read and this UPDATE still stamped six-month
// cancellation retention/deletion dates onto a now-active, paying customer. No read-then-write gate
// can close that — only the write itself can, by re-evaluating the authorising condition against
// committed state as part of the same statement. Same shape, and for the same stated reason, as
// CancelIfPastDueOccurrenceWithRetention: "this method's WHERE clause is its OWN correctness
// mechanism".
//
// The condition comes from the caller (guard), because the two production callers do not share one
// — see RetentionClockGuard's doc comment.
//
// The candidate CTE takes `FOR NO KEY UPDATE` before the status is compared, matching
// CancelIfPastDueOccurrenceWithRetention/ExpireTrialIfNoOverride's own ROUND 23 shape: a locked
// SELECT follows the row's update chain, so the status compared here is the row version that exists
// once any concurrent writer's lock is RELEASED, not the pre-wait one this statement first scanned.
// (Never `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's Finding 1 correction for the
// cross-user deadlock that mode would risk.)
//
// The two zero-rows-affected outcomes are reported distinguishably, in ONE round trip: `target`
// (existence of a live row) and `updated` (the write actually applying) are returned side by side,
// so "someone else changed this user" (ErrRetentionClockPreconditionLost — a legitimate stop) can
// never be mistaken for "no such user" (ErrUserNotFound — a genuine failure). A follow-up existence
// SELECT would have been a second, unsynchronised read of a row this very race is about.
//
// The Go-side gates that callers run BEFORE this call are not redundant and must not be removed:
// they short-circuit work (the per-item Plaid disconnect loop) that no SQL predicate can stop. The
// gate stops the work; this WHERE clause stops the write.
//
// Deliberately NOT part of the guard: the comp/manual-override conjunct the cancellation webhook's
// own gate also checks. A comp does not change subscription_status, so it is a genuinely separate
// question, and it is not one both callers share (the trial path checks it against its own CAS
// instead). Scoping this predicate to the status keeps one meaning for one parameter; the override
// question stays with the callers that ask it, plus the retention deletion claim's own re-check.
func (r *profileRepository) SetRetentionDates(ctx context.Context, userID string, guard RetentionClockGuard, retentionMonths int) error {
	if guard == "" {
		return ErrRetentionClockGuardUnspecified
	}
	// nil means "no status condition" — reached only via the explicitly-named RetentionClockUnguarded.
	var expectedStatus *string
	if guard != RetentionClockUnguarded {
		s := string(guard)
		expectedStatus = &s
	}

	var userExists, updated bool
	err := r.db.QueryRow(ctx, `
		WITH target AS (
			SELECT id, subscription_status FROM users
			WHERE id = $2 AND is_deleted = FALSE
			FOR NO KEY UPDATE
		),
		candidate AS (
			SELECT id FROM target
			WHERE $3::text IS NULL OR subscription_status = $3::text
		),
		updated AS (
			UPDATE users
			SET retention_started_at = clock_timestamp(),
			    retention_expires_at = clock_timestamp() + make_interval(months => $1::int),
			    updated_at = clock_timestamp()
			FROM candidate
			WHERE users.id = candidate.id
			RETURNING users.id
		)
		SELECT EXISTS (SELECT 1 FROM target), EXISTS (SELECT 1 FROM updated)
	`,
		retentionMonths, userID, expectedStatus,
	).Scan(&userExists, &updated)
	if err != nil {
		return fmt.Errorf("SetRetentionDates: %w", err)
	}
	if updated {
		return nil
	}
	if !userExists {
		return ErrUserNotFound
	}
	return ErrRetentionClockPreconditionLost
}

// ClearRetentionDates clears all retention tracking fields on resubscription (#930). Issue #1413
// round-10: also clears plaid_cleanup_pending_at — this is one of the call sites representing
// "user becomes Plaid-eligible again", so nothing should be left pending against the retention
// clock this call is tearing down. (UpdateSubscriptionFromPaddle's own status-driven clearing
// already covers every existing caller of this method atomically with the same status write this
// method usually follows — this is deliberate belt-and-braces, not the sole mechanism: clearing to
// NULL is idempotent no matter how many paths do it.)
//
// Issue #1413 round-11 Fix 1 (BLOCKER): the plaid_cleanup_pending_at clear is now CONDITIONAL on
// the row's LIVE subscription_status at the moment of this write — reusing
// plaidDisconnectRepairEligibleStatusPredicate, the exact same "is this status Plaid-repair-
// eligible" check UpdateSubscriptionFromPaddle's own SET clause uses. Per the invariant
// (domain.User.PlaidCleanupPendingAt's doc comment), the marker must be non-NULL whenever the
// CURRENT status is inactive/cancelled — an unconditional clear here can no longer be trusted to
// mean "the user is Plaid-eligible now", because this call can arrive LATE relative to a NEWER
// transition that already re-established ineligibility with its own fresh marker.
//
// The blocker this closes: an "active" webhook updates the user, then pauses before reaching this
// call; a NEWER cancellation webhook lands first, writing subscription_status='cancelled' plus a
// FRESH marker (UpdateSubscriptionFromPaddle's own COALESCE); that cancellation's Plaid cleanup
// fails and the webhook is retried/dead-lettered; the STALE "active" webhook then resumes and
// reaches this call. Unconditionally nulling the marker here would erase the fresh cancellation's
// marker — GetUsersNeedingPlaidDisconnectRepair requires a non-NULL marker, so the repair pass
// would never see this user again, leaving them cancelled with a live, still-billing Plaid item
// indefinitely. Checking the LIVE status (not the status this stale caller last observed) closes
// this: by the time the stale call finally runs, the row's actual status is 'cancelled', so the
// conditional clear leaves the marker untouched — "a write that no longer reflects reality must
// not win".
//
// retention_started_at/retention_expires_at/the two notice-sent flags are deliberately left
// UNCONDITIONAL — out of this fix's scope. The identical race can transiently clear a fresher
// retention clock too, but that class of loss is self-healing via
// GetUsersNeedingRetentionClockRepair/RepairRetentionClock (belt-and-braces repair pass, Issue
// #1413 round-3 finding H5 residual), unlike the marker, which has NO equivalent backstop query —
// GetUsersNeedingPlaidDisconnectRepair's own predicate REQUIRES the marker to already be
// non-NULL, so a lost marker can never be repaired by re-selecting the user. That asymmetry — one
// field's loss is permanent, the other's is a same-day repair-job blip — is why only the marker
// gets a conditional guard here, not the whole statement. See the round-11 report/PR for this
// scope decision.
func (r *profileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = NULL,
		    retention_expires_at = NULL,
		    retention_notice_30d_sent_at = NULL,
		    retention_notice_7d_sent_at = NULL,
		    plaid_cleanup_pending_at = CASE
		        WHEN `+plaidDisconnectRepairEligibleStatusPredicate+` THEN plaid_cleanup_pending_at
		        ELSE NULL
		    END,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// PlaidCleanupMarkerClearMaxAttempts and PlaidCleanupMarkerClearRetryDelay bound the retry loop
// every ClearPlaidCleanupPendingIfMatches call site uses after a Plaid disconnect completes
// synchronously — Issue #1413 Finding 3 (Codex review, PR #1443 follow-up).
//
// Before this fix, the SAME two numbers (3 attempts, 50ms base delay) existed as two independent,
// hand-copied definitions: queue/jobs/retention_cleanup_check.go's own unexported
// plaidCleanupMarkerClearMaxAttempts/plaidCleanupMarkerClearRetryDelay (which subscription_webhook.go
// — same package — also references directly, so THAT pair was never actually duplicated), and a
// second, genuinely independent pair in services/past_due_service.go, whose own doc comment said so
// explicitly: "Duplicated here (not imported) rather than shared: retention_cleanup_check.go's
// constants are unexported in a different package (queue/jobs vs services)." Codex measured the
// runtime difference this queue/jobs's context-aware exponential backoff vs. past_due_service.go's
// flat time.Sleep loop produces as cosmetic (~150ms vs ~100ms total, at most a 100ms shutdown delay)
// — the actual risk Finding 3 identifies is DRIFT between two independently-maintained copies of the
// same two numbers, not the timing shape. That is the exact failure mode that produced this branch's
// main.go wiring bug (a helper corrected while a hand-written duplicate went stale), so it is worth
// closing here too, proportionately: collapse the constants to ONE literal definition, leave every
// retry LOOP's shape untouched.
//
// This lives in `repository`, not `services` or `queue/jobs`, because `services` cannot import
// `queue/jobs` without inverting the existing dependency (queue/jobs already imports services — see
// retention_cleanup_check.go's own import of this package's sibling), while both `queue/jobs` and
// `services` already import `repository` (this is the package that defines
// ClearPlaidCleanupPendingIfMatches itself, the call every one of these retry loops wraps). Both
// queue/jobs (retention_cleanup_check.go, and by extension subscription_webhook.go in the same
// package) and services (past_due_service.go) now define their own unexported, same-named constant
// as a plain alias to these exported values (`= repository.PlaidCleanupMarkerClearMaxAttempts`, etc.)
// rather than a second independent literal — so every existing internal reference and test in both
// packages keeps working unchanged, but there is now exactly ONE place either NUMBER can be written
// down, and a future edit to one can no longer silently leave the other stale.
const (
	PlaidCleanupMarkerClearMaxAttempts = 3
	PlaidCleanupMarkerClearRetryDelay  = 50 * time.Millisecond
)

// ClearPlaidCleanupPending unconditionally clears the plaid_cleanup_pending_at marker for a user —
// Issue #1413 round-10.
//
// Issue #1413 round-11 correction: this method's own prior doc comment argued unconditional
// clearing was safe because "there is no ordering in which this call can silently erase a marker a
// concurrent, still-in-flight transition is relying on" — that reasoning was WRONG, and is exactly
// the round-11 Fix 3 finding: an admin can grant a comp (clearing the marker) and then remove it
// (setting a FRESH marker for a NEW ineligibility episode) SEQUENTIALLY, entirely between a
// repairPlaidDisconnects worker's candidate-selection read and its later unconditional call to
// this method — no concurrency required, just an intervening pair of writes this method cannot
// see. Calling this unconditionally at that point erases the new episode's marker, discarding its
// entire grace window. This method is kept for callers that genuinely have no discriminator to
// compare against (there are none in this codebase as of round-11 — every production caller now
// uses ClearPlaidCleanupPendingIfMatches below) and for direct repository-level testing; DO NOT
// wire a new production caller to this method without first re-reading
// ClearPlaidCleanupPendingIfMatches's doc comment to confirm an occurrence-scoped clear isn't what
// you actually need.
func (r *profileRepository) ClearPlaidCleanupPending(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users SET plaid_cleanup_pending_at = NULL, updated_at = $1
		WHERE id = $2`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearPlaidCleanupPendingIfMatches is ClearPlaidCleanupPending's occurrence-scoped sibling —
// Issue #1413 round-11 Fix 3, mirroring ClearPastDueStartedAtIfMatches's established CAS shape
// (past_due_service.go's exact solution to the same occurrence-identity problem, applied here to
// plaid_cleanup_pending_at rather than past_due_started_at).
//
// ONLY clears the marker when it still equals expected — the value the caller observed when it
// selected this user as a repair candidate (GetUsersNeedingPlaidDisconnectRepair). Without this,
// repairPlaidDisconnects's final "cleanup succeeded, clear the marker" step would call the
// unconditional ClearPlaidCleanupPending above and could erase a DIFFERENT, newer ineligibility
// episode's marker — one that started (via a comp grant-then-removal, or any other
// clear-then-reset sequence) entirely within the window between this worker's candidate selection
// and this call, with the worker never having re-verified the marker's IDENTITY, only its
// presence. See domain.User.PlaidCleanupPendingAt's doc comment for the invariant this closes:
// "its value is the instant the CURRENT ineligibility episode began — never an earlier episode's
// [and never a later one's]".
//
// Returns (true, nil) when cleared, (false, nil) when the marker had already moved on to a new
// episode since the caller's snapshot was taken (a legitimate, expected skip — the NEW episode's
// marker is exactly what must survive untouched, so a future repair run can evaluate it on its own
// schedule), and (false, ErrUserNotFound) if the user genuinely no longer exists — mirroring
// ClearPastDueStartedAtIfMatches's three-way return contract exactly.
func (r *profileRepository) ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	var userExists, cleared bool
	err := r.db.QueryRow(ctx, `
		WITH existing AS (
			SELECT 1 FROM users WHERE id = $1
		), updated AS (
			UPDATE users SET plaid_cleanup_pending_at = NULL, updated_at = NOW()
			WHERE id = $1 AND plaid_cleanup_pending_at = $2
			RETURNING 1
		)
		SELECT EXISTS(SELECT 1 FROM existing), EXISTS(SELECT 1 FROM updated)
	`, userID, expected).Scan(&userExists, &cleared)
	if err != nil {
		return false, fmt.Errorf("ClearPlaidCleanupPendingIfMatches: %w", err)
	}
	if !userExists {
		return false, ErrUserNotFound
	}
	return cleared, nil
}

// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_30d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_7d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IsRetentionNotice30dEligibleForOccurrence re-verifies, at the moment a caller already holds an
// EXCLUSIVE work_claims claim on repository.ActionRetentionNotice30d, that a user is still
// eligible for the 30-day pre-deletion notice AND that the row's CURRENT retention_expires_at
// still equals the exact occurrence the caller claimed (Issue #1413 round-4 finding M1). Its
// predecessor, IsRetentionNotice30dEligible (deleted by Issue #1442 for having zero production
// callers), re-verified window/sent-state/status/override but NOT the occurrence discriminator:
// retention_cleanup_check.go's processRetentionNotices claims using the CANDIDATE's
// retention_expires_at (E1), but the row can have been RE-ARMED to a DIFFERENT value (E2) by the
// time this re-check runs — resubscribe (ClearRetentionDates), then cancel again, restarting the
// clock from a later point. With a short (e.g. one-month) retention configuration, two workers
// can hold E1 and E2 and BOTH see the current clock as eligible (E2 is ALSO genuinely in-window,
// unsent, uncomped right now) — the user gets a DUPLICATE notice. Requiring
// `retention_expires_at = expectedRetentionExpiresAt` closes this: the check only passes for the
// EXACT occurrence the caller claimed.
//
// Returns (true, nil) when it is safe to send the notice email right now, (false, nil) when any
// one of window/sent-state/status/override/occurrence has changed since the candidate query ran —
// a legitimate skip, never an error. This method performs NO write — see the ProfileRepository
// interface doc for why the claim / eligibility-check / send / stamp sequence is deliberately
// split this way (Issue #1413 Medium finding M9, Codex review round 3).
//
// Consequence is milder than H1/H3 (this branch already accepts at-least-once for notices over
// never-warn — see processRetentionNotices' doc comment), but the same discipline applies: the
// eligibility check must validate the discriminator it claimed.
func (r *profileRepository) IsRetentionNotice30dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	// retention_expires_at - INTERVAL '30 days' below is calendar-day arithmetic on a timestamptz —
	// the same session-time-zone hazard class as make_interval(months => N) above (Issue #1461):
	// an interval's CALENDAR components (years/months/weeks/days) are evaluated in the SESSION time
	// zone, preserving local wall-clock time across the subtraction rather than moving the absolute
	// instant by an exact duration; its FIXED-DURATION components (hours/mins/secs) are exact and
	// session-independent, so this is not equivalent to INTERVAL '720 hours' once a DST boundary
	// sits between the two ends. This boundary is deterministic in production only because
	// database.NewPool (pgx.go) pins every pooled connection's session TimeZone to UTC — a
	// connection that reached this query from outside that pool would shift the 30-day pre-deletion
	// notice window by the local DST offset. See
	// TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary.
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at = $2
			  AND retention_expires_at - INTERVAL '30 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_30d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID, expectedRetentionExpiresAt).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice30dEligibleForOccurrence: %w", err)
	}
	return eligible, nil
}

// IsRetentionNotice7dEligibleForOccurrence is IsRetentionNotice30dEligibleForOccurrence's 7-day
// sibling. See that method's doc for the full rationale.
func (r *profileRepository) IsRetentionNotice7dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	// retention_expires_at - INTERVAL '7 days' below is the same session-TZ-dependent calendar-day
	// arithmetic as IsRetentionNotice30dEligibleForOccurrence's INTERVAL '30 days' above — see that
	// method's comment for the full explanation and why the UTC pool pin (pgx.go) is what makes it
	// safe today.
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at = $2
			  AND retention_expires_at - INTERVAL '7 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_7d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID, expectedRetentionExpiresAt).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice7dEligibleForOccurrence: %w", err)
	}
	return eligible, nil
}

// manualTierOverrideNonEmptyClause is the bare "does this row carry ANY override value at all"
// test — Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up): factored out as its own
// minimal constant so GetUsersNeedingPlaidDisconnectRepair's GREATEST guard (below) can reuse it
// instead of hand-copying a third rewording of a check noActiveManualOverridePredicate and
// admin_user_repository.go's should_rearm_retention/should_mark_plaid_cleanup_pending already embed
// inline. As of Issue #1413 Finding 4 (Low, Codex review, PR #1443 second follow-up round),
// noActiveManualOverridePredicate below also splices this constant in rather than restating its
// own copy of the same "IS NOT NULL AND <> ''" text, closing the drift risk the original version of
// this comment flagged (a change to one copy silently not reaching the other); the
// should_rearm_retention/should_mark_plaid_cleanup_pending Go-side inline copies are untouched —
// this substitution is scoped to this one production SQL string. The substitution is text-only:
// `manual_tier_override IS NOT NULL` and `AND manual_tier_override <> ''` used to sit on their own
// lines inside noActiveManualOverridePredicate's raw string; splicing this single-line constant in
// their place changes only whitespace layout (a newline+indent becomes a space) — the token
// sequence, and therefore the generated SQL Postgres actually parses, is identical. Verified via
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo, which still passes unchanged.
// Says nothing about expiry — an empty-string override and a NULL override are the SAME "no real
// comp" fact this predicate exists to test; manual_tier_ends_at is meaningless without a genuine,
// non-empty manual_tier_override to pair it with (see the GREATEST guard's own doc comment below).
const manualTierOverrideNonEmptyClause = `manual_tier_override IS NOT NULL AND manual_tier_override <> ''`

// noActiveManualOverridePredicate excludes users with a currently-active admin manual tier
// override ("comp") from set-based retention queries (Issue #1413). A comped user must never
// be warned about, or auto-deleted for, retention expiry.
//
// This is the SQL-side twin of domain.User.HasActiveManualOverride() — that Go predicate
// governs row-level reads, this one governs set-based queries where fetching every candidate
// row just to filter in Go would be wasteful. The two must agree on every case; see
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo (a real-database test) for the
// guard that keeps them from drifting apart.
//
// Boundary note: `manual_tier_ends_at >= NOW()` deliberately matches Go's
// `!ends_at.Before(now)` (i.e. "not yet expired" is true AT the expiry instant, not only
// strictly before it). Using `>` here would disagree with the Go predicate at the exact
// expiry instant.
//
// As of the Issue #1413 C1 fix this predicate had no production caller — the three retention
// queries below use noManualOverrideAtAllPredicate instead (see that constant's comment for why),
// and TestNoActiveManualOverridePredicate_Integration_ParityWithGo /
// TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary kept exercising it purely
// so the exact production string wouldn't silently drift from a hand-copied restatement. As of
// the Issue #1413 Codex review round 3 fix (High findings H5/H6), it now has real production
// callers too — CancelIfPastDueOccurrenceWithRetention and ExpireTrialIfNoOverride splice this
// constant into a conditional UPDATE's WHERE clause, per the claim-then-act pattern (a third former
// caller, ClaimPlaidDisconnectIfPastDue, was deleted in round 4 — see past_due_service.go's H6a
// fix, which replaced its self-assigning-update "claim" with a real work_claims claim; a fourth,
// CancelIfPastDueWithRetention — the non-occurrence predecessor of CancelIfPastDueOccurrenceWithRetention
// above — was deleted by Issue #1442 for having zero production callers): this is the one place the
// "still live right now" rule (as opposed to noManualOverrideAtAllPredicate's "not even a lapsed
// one") is actually the correct rule to enforce, because these are point-in-time transitions
// guarded against a comp granted in a race window, not the retention-notice/hard-delete queries'
// cross-job-ordering hazard.
//
// Issue #1413 Finding 4 (Low, Codex review, PR #1443 second follow-up round): the non-empty-override
// half of this predicate (`manual_tier_override IS NOT NULL AND manual_tier_override <> ''`) used to
// be hand-restated here, duplicating manualTierOverrideNonEmptyClause's own copy of the identical
// text — a real drift risk (if one were edited and not the other, cleanup could ignore an override
// in this final predicate while its end date still deferred cleanup via the GREATEST guard below).
// Now splices manualTierOverrideNonEmptyClause in directly; see that constant's own doc comment for
// why this is a text-only (whitespace-only) change with no effect on the generated SQL. This
// predicate's live-override RULE is unchanged in every other respect.
const noActiveManualOverridePredicate = `
	  AND NOT (
	      ` + manualTierOverrideNonEmptyClause + `
	      AND (manual_tier_ends_at IS NULL OR manual_tier_ends_at >= NOW())
	  )
`

// noManualOverrideAtAllPredicate excludes a user with ANY non-empty manual_tier_override —
// live OR already-lapsed — from the retention notice/hard-delete queries (Issue #1413 Critical
// finding C1, Codex review).
//
// Why this must differ from noActiveManualOverridePredicate above: a comp *pauses* the
// retention clock while live, and is re-armed by the separate, independently-scheduled
// manual_tier_override_expiry_check daily job the moment the comp lapses. That job has NO
// ordering guarantee relative to the retention_cleanup_check job that runs these queries. If
// retention selection used the "only exclude while still live" rule
// (noActiveManualOverridePredicate), a user whose comp lapsed moments before the expiry job
// re-arms their clock would be selected here using a stale retention_expires_at from BEFORE the
// comp even started — hard-deleted with no 30-day/7-day warning ever sent, in the race window
// between the two jobs.
//
// Excluding on ANY non-empty override, live or lapsed, never causes a wrongful deletion — but the
// delay this buys is bounded ONLY by manual_tier_override_expiry_check actually running
// successfully every day, not by anything in this predicate. That job is registered with
// PeriodicJobOpts{RunOnStart: true} (Issue #1413 P0 fix — see
// ManualTierOverrideExpiryCheckPeriodicOpts's own doc comment, manual_tier_override_expiry_check.go,
// for why RunOnStart:false left this job possibly UNENQUEUED for arbitrarily long on a repo that
// deploys several times a day, and why RunOnStart:true is only safe paired with its InsertOpts'
// dedup). So a process restart or leader election now triggers an immediate, deduplicated
// catch-up run rather than silently deferring to the next daily schedule — the "runs zero times
// ever" failure mode this comment used to warn about is closed. The residual bound this comment
// still describes is narrower: a failed run's retries eventually land in the dead-letter queue
// with NO automatic replay (Issue #1413 Critical finding C1's "unbounded exclusion" follow-up). If
// the job's retries are exhausted and it dead-letters, this predicate keeps excluding the user
// indefinitely — "never deleted", not "delayed by a day" or "one job cycle" — until someone
// notices the dead-lettered job and replays it. See manual_tier_override_expiry_check.go's Work()
// for the failure metric/log this honesty depends on staying observable. See
// TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared for proof this is a delay
// WHILE THE EXPIRY JOB KEEPS RUNNING, not a permanent exemption by itself, and
// TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp (and its 7d/expired
// siblings) for proof an ordinary never-comped user is unaffected.
//
// Deliberately a SEPARATE predicate from noActiveManualOverridePredicate, which stays exactly as
// it was — it is the SQL twin of domain.User.HasActiveManualOverride() (row-level reads) and a
// parity test guards that pairing. Two predicates, two different jobs, two different correctness
// requirements: one governs "is the comp live right now", the other governs "is it safe to
// notice/delete this account".
const noManualOverrideAtAllPredicate = `
	  AND (manual_tier_override IS NULL OR manual_tier_override = '')
`

// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
// and who have not yet received a 30-day pre-deletion warning (#930).
//
// retention_expires_at - INTERVAL '30 days' in the WHERE clause below is the same
// session-TZ-dependent calendar-day arithmetic as
// IsRetentionNotice30dEligibleForOccurrence's identical expression (see that method's comment for
// the full explanation) — deterministic only because database.NewPool (pgx.go) pins the session
// TimeZone to UTC.
func (r *profileRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '30 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_30d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
// and who have not yet received a 7-day pre-deletion warning (#930).
//
// retention_expires_at - INTERVAL '7 days' below is the same session-TZ-dependent calendar-day
// arithmetic as GetUsersNeedingRetention30dNotice's INTERVAL '30 days' above — see
// IsRetentionNotice30dEligibleForOccurrence's comment for the full explanation.
func (r *profileRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '7 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_7d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// RetentionSweepCursor is a keyset-pagination position for GetUsersWithExpiredRetentionPage,
// captured from the last row of the previous page — (ExpiresAt, UserID), matching that page's
// ORDER BY (retention_expires_at, id) ASC. A nil cursor starts from the beginning.
//
// Deliberately a SIBLING of DeletionSweepCursor rather than a reuse of it. The two are structurally
// identical (a timestamp plus a user id) but name DIFFERENT columns: DeletionSweepCursor.ScheduledAt
// is deletion_scheduled_at, RetentionSweepCursor.ExpiresAt is retention_expires_at. Sharing one type
// would make it a compile-time-legal mistake to feed a cursor harvested from one sweep into the
// other sweep's query — which would silently skip or repeat rows in a HARD-DELETE path — and would
// force one of the two field names to lie about which column it holds. The duplication is four
// lines; the type confusion it prevents is a data-loss bug.
const retentionSweepDefaultPageSize = 500

type RetentionSweepCursor struct {
	ExpiresAt time.Time
	UserID    string
}

// GetUsersWithExpiredRetentionPage returns up to `limit` users whose retention_expires_at has
// passed, ordered by (retention_expires_at, id) ASC for deterministic keyset pagination — Issue
// #1478. Mirrors GetUsersScheduledForDeletionPage above; the WHERE clause is the unbounded
// GetUsersWithExpiredRetention's, unchanged, plus the keyset predicate. (That unbounded method was
// deleted in #1488 once this one had taken over its last caller.)
//
// Why this existed: GetUsersWithExpiredRetention had no LIMIT, and its single caller,
// RetentionCleanupCheckWorker.deleteExpiredAccounts (queue/jobs/retention_cleanup_check.go),
// consumed the whole slice and called DeletionService.ExecuteDeletion(..., DeletionKindRetention)
// once per row — a REAL hard delete (DeleteUserCascade's `DELETE FROM users`), in-process, with no
// intervening queue. RetentionCleanupCheckPeriodicOpts sets RunOnStart: true, so the first run
// after every deploy performed however many hard deletions the table happened to hold. That is the
// same defect class #1440/#1448 opened for the scheduled-deletion sweep, on a job whose per-row
// action is strictly less recoverable.
//
// Why keyset pagination and not a bare LIMIT: this predicate does not care WHY a user has not been
// deleted yet, only that retention_expires_at has passed and no override/deletion-request/soft-delete
// excludes them. A row whose ExecuteDeletion keeps failing (a claim it cannot acquire, a
// DeleteUserCascade guard it cannot satisfy) stays eligible, so under a bare `LIMIT n` the same
// oldest n rows would occupy every page of every run and nothing ranked past them would ever be
// looked at. The keyset cursor lets the caller step PAST whatever it just saw — including past a
// stuck row — within ONE run.
//
// The guarantee this buys, stated bounded by the cap (do NOT restate it as "no starvation" — that
// claim shipped on maxUsersPerDeletionSweep and is only true below the cap): within a single sweep,
// the caller advances across pages until the repository returns a short page (exhausted) or the
// caller's total-processed cap (maxUsersPerRetentionSweep, queue/jobs/retention_cleanup_check.go)
// is reached. Across sweeps, since Issue #1477, the caller PERSISTS the timestamp a capped sweep
// stopped at (the sweep_cursors table — repository/sweep_cursor_repository.go) and passes it back
// here as the next sweep's `after`. So:
//   - Below the cap the within-run guarantee above is unchanged: each sweep steps past stuck rows
//     and later rows are reached in the same run.
//   - At or above the cap, a set of stuck oldest rows no longer consumes every later sweep's whole
//     budget. Before #1477 it did, and nothing ranked past the cap was EVER reached for as long as
//     they kept failing — permanent starvation, independent of arrival rate.
//   - The replacement guarantee is BOUNDED, not absolute (every candidate is reached within one
//     full cycle of sweeps, which says nothing about the per-row action succeeding, nor about
//     wall-clock latency, nor about rows that become eligible behind the current watermark). It is
//     stated once, in full, in repository/sweep_cursor_repository.go's header. Do not restate it
//     here as "no starvation".
//
// Successful rows do not have this problem: ExecuteDeletion ends in DeleteUserCascade's
// `DELETE FROM users`, after which the row does not match this query at all.
func (r *profileRepository) GetUsersWithExpiredRetentionPage(ctx context.Context, after *RetentionSweepCursor, limit int) ([]*domain.User, error) {
	if limit <= 0 {
		limit = retentionSweepDefaultPageSize
	}

	// The retention/status half is SPLICED from expiredRetentionClockPredicate rather than written
	// out here, so retentionDeletionClaimPredicate — which re-verifies it at claim time — cannot
	// drift away from what this query actually selected (#1440 T0-retention). See that constant.
	const predicate = `
		WHERE is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + expiredRetentionClockPredicate + retentionNoticesDeliveredPredicate

	var query string
	var args []interface{}
	if after == nil {
		query = userSelectQuery + predicate + noManualOverrideAtAllPredicate + `
			ORDER BY retention_expires_at ASC, id ASC
			LIMIT $1
		`
		args = []interface{}{limit}
	} else {
		query = userSelectQuery + predicate + `
			  AND (retention_expires_at, id) > ($1, $2)
		` + noManualOverrideAtAllPredicate + `
			ORDER BY retention_expires_at ASC, id ASC
			LIMIT $3
		`
		args = []interface{}{after.ExpiresAt, after.UserID, limit}
	}

	users, err := r.queryUsers(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersNeedingRetentionClockRepair implements the Issue #1413 H5 residual repair path — see
// the interface doc comment for the full rationale. Mirrors GetUsersWithExpiredRetentionPage's
// predicate shape except retention_expires_at IS NULL instead of IS NOT NULL/expired.
func (r *profileRepository) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY updated_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// RepairRetentionClock implements the Issue #1413 H5 residual repair write — see the interface
// doc comment for why a conditional UPDATE (not a work_claims claim) is sufficient here.
//
// retention_expires_at is computed entirely in Postgres, clock_timestamp() + make_interval(months
// => $3) — Task NA2 (Issue #1413 follow-up on PR #1443): this method used to accept a Go-computed
// expiresAt time.Time built via now.AddDate(0, retentionMonths, 0) in
// retention_cleanup_check.go's repairRetentionClocks, which disagrees with Postgres's
// make_interval whenever the anchor's day-of-month does not exist N months later. This matters
// MORE here than for an ordinary writer: this method exists specifically to repair rows OTHER
// writers left with a NULL retention_expires_at, so if it computed the expiry using a DIFFERENT
// semantics than the writers it backstops, a "repaired" row would land on a DIFFERENT deadline
// than a fresh write from those writers would have produced — the repair pass itself becoming a
// second source of drift, not a cure for the first. Matches SetRetentionDates and
// AdminUserRepository.RemoveUserTierOverride's SQL exactly. retention_started_at stays a plain
// Go-computed value ($2, unchanged) — per removeUserTierOverrideUpdate's own doc comment
// (admin_user_repository.go), retention_started_at is a paired audit field nothing computes an
// age from or compares to NOW()/clock_timestamp() anywhere; only retention_expires_at is ever
// read back as a deadline.
func (r *profileRepository) RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = $2,
		    retention_expires_at = clock_timestamp() + make_interval(months => $3::int),
		    updated_at = NOW()
		WHERE id = $1
		  AND retention_expires_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate,
		userID, startedAt, retentionMonths)
	if err != nil {
		return false, fmt.Errorf("RepairRetentionClock: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// plaidDisconnectRepairEligibleStatusPredicate is the SQL twin of
// domain.SubscriptionStatus.IsEligibleForPlaidDisconnectRepair() — Issue #1413 round-8 High 4
// (Codex review). Deliberately NARROWER than the read-only notion used by the retention-notice/
// hard-delete queries above (which also don't include past_due, but for a different family of
// predicates entirely): 'past_due' is read-only, but must never be immediately Plaid-disconnected
// by THIS repair pass — it has its own dunning timeline (past_due_service.go,
// PastDueConfig.PlaidDisconnectDays) that exists precisely to give a paying-but-behind user time
// before Plaid access is touched. This branch already has a precedent for this exact failure class
// — an override predicate existing in both SQL and Go, kept honest by a parity test
// (noActiveManualOverridePredicate / domain.User.HasActiveManualOverride(), guarded by
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo) — this constant and
// TestPlaidDisconnectRepairEligibleStatus_Integration_ParityWithGo
// (profile_repository_plaid_disconnect_repair_integration_test.go) do the same here: one shared
// definition of "which statuses are eligible" instead of a third divergent copy.
const plaidDisconnectRepairEligibleStatusPredicate = `subscription_status IN ('inactive', 'cancelled')`

// liveLinkedItemExistsClause is the EXISTS predicate for "this user has at least one still-live
// linked item" — not 'disconnected' or 'replaced', with a NULL status explicitly treated as live
// too (round-7 fix; see GetUsersNeedingPlaidDisconnectRepair's doc comment below for the
// NULL-status rationale this clause must preserve). Correlates against the unaliased `users` table
// at least five call sites SELECT FROM — GetUsersNeedingPlaidDisconnectRepair just below, and
// admin_user_repository.go's removeUserTierOverrideSelectPrior — so all must keep using this SAME
// Go constant, not a second hand-copied SQL literal, so "live" cannot silently drift apart between
// them. A second, independently-worded copy is exactly how Issue #1413 round-14 finding M1 slipped
// in: removeUserTierOverrideSelectPrior's should_mark_plaid_cleanup_pending stamped the marker for
// ANY prior override on an inactive/cancelled user, with no check on whether a live item actually
// remained — re-arming a phantom marker for an episode whose cleanup had already fully completed.
const liveLinkedItemExistsClause = `EXISTS (
	SELECT 1 FROM linked_items
	WHERE linked_items.user_id = users.id
	  AND (linked_items.status IS NULL OR linked_items.status NOT IN ('disconnected', 'replaced'))
)`

// GetUsersNeedingPlaidDisconnectRepair implements the Issue #1413 round-7 grace-window repair
// path — see the interface doc comment for the full rationale. Mirrors
// GetUsersNeedingRetentionClockRepair's predicate shape, plus:
//   - a grace-window check on plaid_cleanup_pending_at (Issue #1413 round-10) — the ONLY anchor
//     this query uses. Rounds 8 and 9 demonstrated retention_started_at cannot be made sound as
//     this grace-window anchor: it is written by several independent paths, its clearing on
//     resubscribe can fail and be swallowed by a logging-only caller, and it is never cleared once
//     a disconnect actually completes (it has to keep counting toward the separate 6-month
//     retention/deletion clock regardless of Plaid state) — so a value written by a PRIOR cycle
//     can silently survive a resubscribe-then-recancel and be mistaken for the CURRENT cycle's
//     age. plaid_cleanup_pending_at cannot suffer this: it means exactly "a raw-status
//     ineligibility episode began at this instant, cleanup outstanding" — a durable,
//     override-independent fact (see domain.User.PlaidCleanupPendingAt's doc comment) — and is
//     NULL whenever nothing is pending (see ExpireTrialIfNoOverride/
//     CancelIfPastDueOccurrenceWithRetention/UpdateSubscriptionFromPaddle/
//     UpdateTrialStatus for where it's set/cleared). A NULL marker is explicitly excluded — this pass must never
//     invent work from a derived signal.
//   - an EXISTS check for at least one linked item that is still "live" — not 'disconnected' or
//     'replaced', the same definition LinkedItemRepository.GetActiveItemsByUserID uses, EXCEPT a
//     NULL status is explicitly treated as live too (round-7 fix): a bare `status NOT IN (...)`
//     evaluates to SQL's UNKNOWN (not TRUE) for a NULL status, silently excluding that row — when
//     linked_items.status was still nullable (migration 000013 added a DEFAULT 'active' but no NOT
//     NULL constraint) a live item with a NULL status would otherwise never be repaired, precisely
//     the leak this pass exists to prevent. Migration 000126 (Issue #1444) has since made the
//     column NOT NULL, so no new NULL can arise; the arm is kept as defence in depth — it costs
//     nothing, and it keeps this clause identical to its three siblings
//     (liveLinkedItemExistsClause, anyLinkedItemExistsClause, uncleanedLinkedItemExistsClause),
//     whose divergence is what caused the round-7 leak in the first place.
//
// Issue #1413 round-12 Fix 2: uses noActiveManualOverridePredicate ("live right now"), NOT
// noManualOverrideAtAllPredicate ("not even a lapsed one") — deliberately the OPPOSITE choice from
// every retention/deletion query above. Those queries must fail safe toward NEVER hard-deleting a
// user whose comp merely lapsed uncleaned (deletion is irreversible), so they exclude on ANY
// override, live or lapsed, and stay excluded until manual_tier_override_expiry_check actually
// clears the column — an unbounded delay is the acceptable failure mode there. A Plaid disconnect
// is different in kind: it is RECOVERABLE (the user relinks), so the fail-safe direction here is
// the other way — a lapsed-but-uncleared override must not permanently exempt a user from this
// repair pass, or a stalled/dead-lettered expiry job would leave their bank connection live and
// billable indefinitely.
//
// Issue #1413 round-13/14: this predicate is the ONLY place CURRENT override state (live vs.
// lapsed vs. absent, a WALL-CLOCK fact) enters this query's reasoning — per the restated invariant
// (domain.User.PlaidCleanupPendingAt's doc comment), no SET/CLEAR site conditions the marker's
// VALUE on whether an override is presently active, which is what round-13 actually fixed (round-11
// through round-12 had tied the marker to "is there a live override right now", requiring a LATER
// writer to catch the instant it lapsed — see that doc comment for the full round-13 root cause).
//
// Round-14 correction (Issue #1413 L4, Codex review): this comment previously claimed NO SET/CLEAR
// site references manual_tier_override "in any form any more" — false. AdminUserRepository's
// removeUserTierOverrideUpdate (admin_user_repository.go, shared by RemoveUserTierOverride/
// RemoveUserTierOverrideIfMatches) still reads manual_tier_override, as a write-time "was a comp
// present at all, and is it being removed right now" gate for its should_mark_plaid_cleanup_pending
// backstop CASE (further narrowed, as of round-14 finding M1, by liveLinkedItemExistsClause — see
// that CASE's own doc comment). That is a different thing from what round-13 closed: it is not a
// live-vs-lapsed distinction, and it does not require any later writer to keep it correct — the CASE
// only fires exactly once, synchronously, in the same statement that clears the override column.
//
// CURRENT eligibility — "should this user actually be disconnected right now" — is entirely this
// query's job: plaidDisconnectRepairEligibleStatusPredicate plus this
// noActiveManualOverridePredicate, evaluated fresh on every run against live, current row state.
// That split (a durable write-time fact plus a live read-time predicate) is precisely what closes
// the round-13 P0: a comp granted over an already-marked user survives the grant (the marker is
// untouched), and once that comp lapses — with or without the daily expiry job ever running — this
// predicate simply stops excluding the row on the very next scheduled run. Do NOT "unify" this with
// noManualOverrideAtAllPredicate — see TestRetentionQueries_Integration_MalformedOverride_StillExcluded
// and this predicate's own doc comment for why the retention/deletion queries must stay as they are.
//
// Issue #1413 Finding 1 (Medium, Codex review, PR #1443 follow-up, headline finding of this round):
// the split above (a durable write-time marker plus a live read-time override predicate) closed
// round-13's P0 for CURRENT ELIGIBILITY, but left a second, narrower gap in the GRACE WINDOW itself.
// A comp SUSPENDS eligibility while live, but plaid_cleanup_pending_at keeps aging regardless — round-13
// deliberately left it untouched by a grant, so it can already be arbitrarily old by the time a LATER
// comp lapses. No writer runs when a comp simply lapses on its own clock (noActiveManualOverridePredicate
// is a read-time fact, not a write), so round-10's removal-path anchor
// (removeUserTierOverrideUpdate, admin_user_repository.go, ROUND 19/20) never reaches this: it only
// re-anchors the marker when someone actually calls RemoveUserTierOverride/RemoveUserTierOverrideIfMatches.
// Concretely: cancellation stamps the marker at T0; an admin comps the user (marker untouched, per
// round-13); the comp lapses at T_lapse with NOTHING written; before the expiry job ever runs, this
// query would see plaid_cleanup_pending_at = T0 — possibly already older than gracePeriod — and select
// the user for disconnect, even though they have been read-only (post-lapse) for far less than
// gracePeriod. The user is disconnected almost the instant they actually lose access, having been
// granted almost none of their entitled grace window.
//
// The fix: anchor the grace comparison on
// GREATEST(plaid_cleanup_pending_at, CASE WHEN manualTierOverrideNonEmptyClause THEN
// manual_tier_ends_at ELSE NULL END), not plaid_cleanup_pending_at alone — the SAME rule
// removeUserTierOverrideUpdate's own lapse-anchor CASE already expresses on the writer side (ROUND
// 19 there: anchor on manual_tier_ends_at when it is the more recent loss-of-access instant, never
// NOW()/"today" for a comp that lapsed long ago). Both sites now say "grace runs from when access
// was actually lost," from two different angles — a query predicate here, a write-time anchor
// there — and are cross-referenced so they cannot drift apart; if this GREATEST expression ever
// changes, re-check removeUserTierOverrideUpdate's CASE (and vice versa) for whether the underlying
// rule they're both expressing has actually changed too.
//
// NULL semantics, stated once, precisely, for every GREATEST-related case below: PostgreSQL's
// GREATEST() ignores NULL arguments and returns the greatest of the remaining non-NULL ones,
// returning NULL only when EVERY argument is NULL (this is NOT the same rule as `+` or plain
// comparison operators, which propagate a single NULL to the whole expression) — verified directly
// against Postgres (not merely reasoned about; see TestPlaidDisconnectRepair_Integration_* below).
// Every case that follows reduces to an instance of this one rule.
//
// Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up) added the CASE guard above
// (manualTierOverrideNonEmptyClause) after finding a FIFTH, malformed row shape none of the original
// four cases covered: manual_tier_override set to the empty string, so noActiveManualOverridePredicate's
// own non-empty check does NOT exclude this row (an empty string never satisfies that inequality
// test), paired with a
// non-NULL manual_tier_ends_at (e.g. a stale value left over from a prior, unrelated override that
// was cleared incompletely, or any other reachable combination — this package's own tests
// deliberately exercise independent combinations of these two columns, and there is no DB
// constraint tying them together). Without the guard, GREATEST would still pick up that ends_at
// value even though it does not represent any real, current comp, and a value set far in the future
// would defer this user's eligibility for repair indefinitely. The guard makes manual_tier_ends_at
// participate in GREATEST ONLY when manualTierOverrideNonEmptyClause holds — i.e. only when the
// override is a real, non-empty value, mirroring noActiveManualOverridePredicate's own non-empty
// test rather than re-deriving a third copy of it (see manualTierOverrideNonEmptyClause's own doc
// comment for why that constant, not noActiveManualOverridePredicate itself, is what's reused here).
// See TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt.
//
// Five cases were reasoned through against noActiveManualOverridePredicate and the CASE guard above,
// immediately below this GREATEST clause in the query, and confirmed against a real database (not
// just reasoned about, since PostgreSQL's GREATEST/NULL interaction is exactly the kind of thing an
// error here would be silent about):
//  1. manual_tier_ends_at in the FUTURE, override non-empty (a live comp): noActiveManualOverridePredicate
//     already excludes this row outright (manual_tier_override non-empty AND ends_at >= NOW()) — the
//     row never reaches the GREATEST comparison at all, so GREATEST cannot wrongly defer anything here.
//  2. manual_tier_ends_at NULL, override non-empty (a permanent, non-expiring comp):
//     noActiveManualOverridePredicate's "ends_at IS NULL OR ends_at >= NOW()" branch treats a NULL
//     end as still-active, so this row is ALSO excluded before the GREATEST comparison runs.
//  3. manual_tier_ends_at NULL, NO override (the ordinary case — the overwhelming majority of rows):
//     not excluded by noActiveManualOverridePredicate, so THIS is the case where a silent GREATEST/NULL
//     regression would be catastrophic — see the NULL-semantics paragraph above: `GREATEST(x, NULL) = x`,
//     so every ordinary user's comparison correctly reduces to plaid_cleanup_pending_at alone (the whole
//     marker being NULL would already have failed the earlier `plaid_cleanup_pending_at IS NOT NULL`
//     clause). Every pre-existing test in this file that has no override at all (the large majority)
//     is a live regression check for this case on every run.
//  4. A lapsed comp OLDER than the marker, override non-empty (manual_tier_ends_at predates
//     plaid_cleanup_pending_at — e.g. an old, never-cleared comp from a PRIOR, unrelated episode):
//     the CASE guard passes ends_at through (override is non-empty), and GREATEST picks the marker
//     (the later of the two), so this must, and does, behave identically to the pre-Finding-3,
//     marker-only comparison — see TestPlaidDisconnectRepair_Integration_LapsedCompOlderThanMarker_UnaffectedByGreatest
//     (profile_repository_plaid_disconnect_repair_integration_test.go).
//  5. manual_tier_override set to the empty string, manual_tier_ends_at set to ANY value (the
//     Finding 3 malformed shape): the CASE guard's manualTierOverrideNonEmptyClause test is false
//     (an empty string never satisfies the non-empty inequality test), so manual_tier_ends_at is
//     passed to GREATEST as NULL regardless of what it actually
//     contains — GREATEST reduces to plaid_cleanup_pending_at alone, per the SAME NULL-semantics rule
//     as case 3 above. See TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt.
//
// The bug ROUND 22 closed is a sixth, implicit case none of the above: manual_tier_ends_at in the
// past AND more recent than the marker, override non-empty — see
// TestPlaidDisconnectRepair_Integration_LapsedCompRestartsGraceFromLapseNotFromMarker for that
// scenario reproduced end to end through production writers.
//
// GetOutstandingPlaidCleanupStats (job_health_repository.go), the sibling backlog gauge, deliberately
// does NOT get this same GREATEST treatment — it measures "how much cleanup is outstanding and how
// long has it been outstanding", not "who is past grace right now", and its own doc comment already
// states it must keep counting a comped user's backlog for the ENTIRE time a comp is live (a gauge
// that went blind to that population would hide a chronic problem for as long as the comp lasts).
// Anchoring it on GREATEST(..., manual_tier_ends_at) would UNDERSTATE that backlog's age for exactly
// the population the gauge exists to keep visible, so the two are deliberately NOT aligned on this
// axis — see that method's own doc comment for the full reasoning.
//
// CI-observed clock-skew fix (fourth failure of this exact class on this branch): through the fix
// below, this method computed its grace-window cutoff in GO — `cutoff := time.Now().Add(-gracePeriod)`
// — and compared it against plaid_cleanup_pending_at, a column every writer stamps from POSTGRES's
// OWN clock (`NOW()`, or — for removeUserTierOverrideUpdate's still-live branch and
// StampMissingPlaidCleanupMarkers's global-scan heal, as of Issue #1413 Finding 2, Codex review, PR
// #1443 follow-up — `clock_timestamp()` instead, for the subset of writers actually exposed to a
// concurrent-lock-wait backdating risk in a way a bare NOW()->clock_timestamp() substitution could
// genuinely close; see StampMissingPlaidCleanupMarkers's own doc comment for why that fix needed
// more than a function-name swap, and for which OTHER writers of this column were audited and
// deliberately left on NOW() pending a larger follow-up). CI runs Postgres in a separate container
// whose clock can lag or lead the test runner's by
// single-digit milliseconds; mixing the two clocks in one comparison meant every test exercising a
// near-zero grace boundary inherited that skew, no matter how carefully the TEST fenced its own
// assertions — the mixing was in this production query, not in any test. Two tests failed in CI on
// this exact defect: TestMigration000121_LapsedOverrideRowNowRepaired (calls this method with
// gracePeriod=0 immediately after a migration stamps the column via Postgres's own NOW(); a lagging
// Go clock could put `cutoff` before the stamped value, so `plaid_cleanup_pending_at < cutoff` was
// false) and TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker (a
// DIFFERENT manifestation of the same root cause — see UpdateSubscriptionFromPaddle's own doc
// comment).
//
// The fix moves the ENTIRE comparison into Postgres: `clock_timestamp() - make_interval(secs => $1)`
// computes the cutoff using a Postgres-native clock, so the two sides are never subject to
// inter-process/inter-container skew, no matter how tight the grace period. (Issue #1413 Finding 2:
// this comparison itself was `NOW()`-based from round-10 through round-20 — switched to
// clock_timestamp() alongside the GREATEST fix above, both landing in the same round; see
// StampMissingPlaidCleanupMarkers's doc comment for why a plain autocommit SELECT does not strictly
// NEED clock_timestamp() the way a lock-waiting UPDATE does, and why using it here anyway is still
// the right call — consistency with every write-side anchor it is compared against, not a
// correctness requirement of this particular read.) The exported signature (gracePeriod
// time.Duration) is unchanged — callers are unaffected; only the number that crosses the wire
// changes, from a pre-computed absolute timestamp to a plain float64 of seconds, which
// make_interval's `secs double precision` parameter accepts directly (mirrors
// removeUserTierOverrideUpdate's own established make_interval(months => $N) precedent,
// admin_user_repository.go). Sub-second grace periods (several tests here call this with exactly
// zero) lose no precision — Duration.Seconds() is a float64, and 0 maps to 0 exactly.
func (r *profileRepository) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE ` + plaidDisconnectRepairEligibleStatusPredicate + `
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND GREATEST(
		        plaid_cleanup_pending_at,
		        CASE WHEN ` + manualTierOverrideNonEmptyClause + ` THEN manual_tier_ends_at ELSE NULL END
		      ) < clock_timestamp() - make_interval(secs => $1)
		  AND ` + liveLinkedItemExistsClause + `
	` + noActiveManualOverridePredicate + `
		ORDER BY updated_at ASC
	`
	users, err := r.queryUsers(ctx, query, gracePeriod.Seconds())
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// StampMissingPlaidCleanupMarkers is Issue #1413's self-healing backstop for
// plaid_cleanup_pending_at — round 16, added after five prior review rounds each closed one way the
// marker could end up NULL while a user still has a live linked item, and opened another, because
// every attempt tried to make some WRITER maintain the marker correctly. Writers cannot: they race
// with the linked_items lifecycle they are trying to summarise (see removeUserTierOverrideSelectPrior's
// doc comment, admin_user_repository.go, and domain.User.PlaidCleanupPendingAt's doc comment, for the
// two concrete races round 16 stops trying to close by writing more carefully — a re-fetch that just
// moves its own window, and a CTE snapshot that cannot serialize against an independent, autocommit
// INSERT).
//
// This stops fixing writers and instead heals: run once at the start of EVERY repairPlaidDisconnects
// pass (retention_cleanup_check.go), before the candidate query, this is a single atomic UPDATE that
// stamps clock_timestamp() for any user who is ineligible-status, has at least one live linked item,
// and has a NULL marker. A writer that misses a stamp — for any reason, present or future, including
// a race no one has found yet — self-heals on the very next repair pass instead of leaking the
// user's bank connection forever.
//
// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up): this used to write NOW()
// (transaction_timestamp()), on the argument that a single autocommit UPDATE has no EARLIER
// lock-acquiring statement in the same (implicit) transaction, so there was supposedly no wait for
// NOW() to be staled across. That argument is wrong, and the correction matters beyond this one
// call site: NOW()/transaction_timestamp() is fixed at the transaction's BEGIN, which for a bare
// autocommit statement is essentially "when the statement was submitted" — BEFORE Postgres acquires
// the row locks needed to actually perform the UPDATE, not after. If this statement's own WHERE
// clause matches a row a CONCURRENT transaction already holds locked (e.g. removeUserTierOverrideUpdate's
// FOR UPDATE, admin_user_repository.go, mid-removal on the very same row), THIS statement blocks
// waiting for that lock — and once unblocked, NOW() still reflects the pre-wait instant, not the
// moment the row was actually written. Concretely: this backstop starts at T0, blocks behind a
// concurrent removal holding the row for (say) 73 hours, then finally writes plaid_cleanup_pending_at
// = NOW() = T0 — the very next repair pass sees a marker already 73 hours stale and disconnects with
// no meaningful grace at all. The fix: clock_timestamp() — real statement-execution wall time,
// re-evaluated fresh when the statement actually runs, not when its enclosing (here: implicit,
// one-statement) transaction began. This is the SAME fix, and the SAME corrected reasoning, as
// removeUserTierOverrideUpdate's own plaid_cleanup_pending_at CASE (ROUND 20, admin_user_repository.go)
// — the "autocommit statements are exempt" argument that used to distinguish this call site from
// that one was the error; there is no such exemption for ANY statement that can block on a lock,
// autocommit or not.
//
// Why this shape, against the four properties that matter here:
//
//  1. GetUsersNeedingPlaidDisconnectRepair is left COMPLETELY UNCHANGED. This preserves
//     TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoLiveItems outright: the heal requires a
//     live item — the SAME liveLinkedItemExistsClause the candidate query already requires — so a
//     user with no live items is never stamped and never selected. No pinned test needs
//     reinterpreting, and which users the candidate query ACTS on never changes.
//  2. It is ONE atomic statement, so it cannot itself race: a concurrent linked_items INSERT either
//     commits before this UPDATE's own snapshot (and that row is healed right now) or after it (and
//     it is healed on the NEXT run instead) — there is no window in between where running this
//     statement can itself observe or produce a torn, half-updated result.
//  3. The existing grace-window logic is untouched: a healed user's plaidDisconnectRepairGracePeriod
//     runs from the heal (this statement's own clock_timestamp() — see the Finding 2 correction
//     below for why NOW() would have been wrong here), exactly like every other SET site.
//  4. Reuses the SAME shared Go constants plaidDisconnectRepairEligibleStatusPredicate and
//     liveLinkedItemExistsClause the candidate query, the backlog gauge (GetOutstandingPlaidCleanupStats,
//     job_health_repository.go), and migration 000121 all already use — copied verbatim there since a
//     migration cannot import Go code — instead of a fourth hand-copied restatement of either, so the
//     heal, the candidate query, the backlog gauge, and the migration cannot silently drift apart on
//     "which statuses are eligible" or "what counts as a live item". noActiveManualOverridePredicate
//     is also reused from the SAME Go constant the candidate query uses, but is NOT part of migration
//     000121's WHERE clause — that migration deliberately omits any override filter at all (a
//     one-time historical backfill has no "next run" to rely on, so it stamps every eligible row
//     regardless of comp state); see the override paragraph below for why this backstop, which DOES
//     have a next run, can safely defer a currently-comped user to it instead.
//
// The consequence, stated plainly rather than left implicit: a marker cleared too early (or never
// stamped in the first place) does NOT get its ORIGINAL episode's grace window resumed — it gets a
// FRESH plaidDisconnectRepairGracePeriod starting from whenever this backstop next runs and notices
// the gap. In the worst case (the gap opens moments after this pass just ran), that is up to one
// ADDITIONAL full grace window of delay before the user is actually disconnected. That is strictly
// better than the alternative it replaces — never, permanently — and it is the accepted price of
// healing by re-detection rather than trying to preserve a timestamp a writer never got the chance to
// record correctly in the first place.
//
// Includes noActiveManualOverridePredicate deliberately: this mirrors GetUsersNeedingPlaidDisconnectRepair's
// own predicate set exactly (modulo the marker/grace-window gates this backstop's whole job is to
// populate), so it only ever creates a marker for a population the candidate query would ITSELF
// eventually act on — never for a currently-comped user the candidate query would never touch
// regardless of marker state. A currently-comped user's marker is instead re-armed the ordinary way
// once the comp is actually removed (removeUserTierOverrideUpdate's plaid_cleanup_pending_at CASE —
// ROUND 18/19/20, admin_user_repository.go; NOT a COALESCE, which this CASE stopped using at ROUND
// 18 precisely because a COALESCE-preserved stale value is what Finding 1, above, closes) or, if the
// comp lapses and is never explicitly removed, by the NEXT run of this same backstop once
// noActiveManualOverridePredicate stops excluding them.
//
// Called from exactly one place: RetentionCleanupCheckWorker.repairPlaidDisconnects
// (retention_cleanup_check.go), at the very start of every pass — so a user healed in THIS pass can
// also be selected for repair in the SAME pass if they happen to already be past the grace window.
//
// CAUTION — this is a global-scan WRITE, unlike every other method in this file: its WHERE clause
// carries no user-id parameter at all. Unlike a global-scan READ (which can only produce a confusing
// false test result), this can actually MUTATE another test's fixture row if that row happens to
// satisfy this predicate at the moment this runs — see this package's own integration tests for this
// method, which give every fixture a unique, immediately-`t.Cleanup`'d row for exactly this reason.
// This blast radius is intentional and load-bearing for production correctness (the whole point is to
// catch a row NO scoped query would ever be told to look at) — but it means this method must never be
// invoked from a shared test-setup helper unrelated tests also use, only from a fixture the calling
// test fully owns end-to-end.
//
// Deliberately NOT part of the exported ProfileRepository interface above: this is needed only by
// RetentionCleanupCheckWorker's own narrow RetentionProfileRepository consumer interface
// (retention_cleanup_check.go), not by the broad canonical interface every OTHER service's test
// double also implements in full — adding it there would force a new stub method onto every one of
// those unrelated mocks for a capability only one caller needs. Mirrors this file's existing
// precedent for ClearPastDueStartedAtIfMatches, CancelIfPastDueOccurrenceWithRetention, and the two
// occurrence-scoped retention-notice-eligibility methods (see NewProfileRepository's own doc comment).
func (r *profileRepository) StampMissingPlaidCleanupMarkers(ctx context.Context) (int, error) {
	// ROUND 21 correction (Issue #1413 Finding 2, Codex review, PR #1443 follow-up): a bare
	// `UPDATE ... SET plaid_cleanup_pending_at = clock_timestamp() WHERE ...` is NOT enough, and this
	// was verified empirically (against a real database, not reasoned about) while building this
	// method's own regression test. PostgreSQL evaluates an UPDATE's SET-list target expressions —
	// INCLUDING a volatile call like clock_timestamp() — against the row version obtained by the
	// scan BEFORE any row-lock wait; if a candidate row is concurrently locked by another
	// transaction, this statement blocks, but once unblocked it does NOT re-evaluate clock_timestamp()
	// against the moment the lock was actually released — the value already computed pre-wait is what
	// gets written. A bare substitution of NOW() for clock_timestamp() therefore leaves the EXACT same
	// backdating bug this fix exists to close, just with a different function name.
	//
	// The fix that actually works, mirroring removeUserTierOverrideUpdate's own already-correct shape
	// (admin_user_repository.go): acquire the row locks EXPLICITLY, as their own separate step, via a
	// `SELECT ... FOR NO KEY UPDATE` CTE — THAT step is what genuinely blocks and waits. Only once it
	// resolves (lock already held by this transaction) does the outer UPDATE run, evaluating
	// clock_timestamp() fresh, with nothing left to wait for — so it correctly reflects the
	// post-wait, statement-execution instant. This is still ONE round trip (one r.db.Exec call, one
	// implicit autocommit transaction) — the CTE only changes the ORDER PostgreSQL performs the lock
	// acquisition and the target-list evaluation in, not the transaction shape.
	//
	// ROUND 23 correction (Issue #1413 Finding 1, High, Codex review, PR #1443 follow-up): ROUND 21
	// used `FOR UPDATE` for the candidate lock. The lock MODE, not just the restructuring, was wrong,
	// and produces a real, reproducible cross-user deadlock against admin_user_repository.go's comp
	// removal path. Concrete cycle:
	//
	//  1. User B is ineligible-status with a live linked item and a NULL marker — an ordinary Stamp
	//     candidate — so this CTE's (old) `FOR UPDATE` locks B.
	//  2. This statement's candidate scan (with multiple qualifying rows and, pre-fix, no defined lock
	//     order — see the ORDER BY correction below) reaches user A, whose row is already locked by a
	//     concurrent admin_user_repository.go RemoveUserTierOverride/RemoveUserTierOverrideIfMatches
	//     call (its own "prior" CTE's `FOR UPDATE`). This statement blocks, waiting for A.
	//  3. That removal transaction, still holding A, proceeds to insert its audit_logs row — and the
	//     acting admin recorded as that row's actor happens to BE user B (audit_logs.user_id has no
	//     "must have an active subscription" constraint — an admin's own account can independently be
	//     inactive/cancelled with a live linked item, which is exactly how B qualified as a Stamp
	//     candidate at all; see the note at the end of this comment for why that is a separate,
	//     intentional contract, not something to "fix" here).
	//  4. audit_logs.user_id has a foreign key to users.id. Inserting a row that references B triggers
	//     Postgres's referential-integrity check, which requests a `FOR KEY SHARE` lock on B — the
	//     weakest lock mode, meant to coexist with everything except a lock that could change B's key.
	//  5. `FOR UPDATE` (this statement's OLD lock mode on B, step 1) conflicts with `FOR KEY SHARE`
	//     (step 4's request) — PostgreSQL's conflict table has NO row/column pair where these coexist.
	//     Now: this statement holds B and waits on A (step 2); the removal holds A and waits on B (step
	//     4). Classic deadlock cycle. PostgreSQL's deadlock detector eventually aborts one side with a
	//     serialization failure — in a billing-adjacent path, an aborted transaction is not a
	//     theoretical concern.
	//
	// The fix: `FOR NO KEY UPDATE` instead of `FOR UPDATE` for this statement's candidate lock. It is
	// the semantically correct mode regardless of the deadlock — this statement changes NO key column
	// (only plaid_cleanup_pending_at and updated_at), so `FOR UPDATE`'s key-change guarantee was never
	// needed here. And per PostgreSQL's row-lock conflict table, `FOR NO KEY UPDATE` does NOT conflict
	// with `FOR KEY SHARE` — so with this fix, step 5 above never blocks: the removal's audit insert on
	// B proceeds immediately even while this statement still holds B, breaking the cycle at its root
	// rather than papering over one specific interleaving of it. Every CTE added for Issue #1413
	// Finding 2 (the four sibling COALESCE(plaid_cleanup_pending_at, ...) writers restructured in this
	// same round — UpdateSubscriptionFromPaddle, UpdateTrialStatus,
	// CancelIfPastDueOccurrenceWithRetention, ExpireTrialIfNoOverride) uses `FOR NO KEY UPDATE` for
	// this identical reason: any of those four rows could equally be an audit actor, so `FOR UPDATE`
	// there would reproduce this exact cycle four more times over.
	//
	// Verified via a real database (internal/repository/profile_repository_plaid_disconnect_repair_integration_test.go,
	// TestStampMissingPlaidCleanupMarkers_Integration_DeadlockCycleWithConcurrentAuditActor and its
	// sibling doc comment) that this statement's `FOR NO KEY UPDATE` does not conflict with a
	// concurrent `FOR KEY SHARE` request on the same row, closing the cycle above at its root.
	//
	// Note on step 3 above: this repository does not, and must not, require an audit_logs actor to
	// have an active subscription — that would make routine admin actions fail based on the ADMIN's
	// OWN unrelated billing state, which is not a real precondition of being allowed to act as an
	// admin. The reachability of "the acting admin's own account is itself a Stamp candidate" is a
	// consequence of that (correct, unrelated) design choice, not a defect to close here.
	//
	// ROUND 23 (Issue #1413 Finding 1, High, Codex review, PR #1443 follow-up) also adds `ORDER BY id`
	// to this CTE's candidate scan. Without it, when more than one row qualifies, PostgreSQL's lock
	// acquisition order for a multi-row `FOR ... UPDATE`/`FOR NO KEY UPDATE` scan is whatever order the
	// underlying scan happens to produce (unspecified, and can differ run to run, e.g. under a
	// different query plan or after a VACUUM). Two concurrent Stamp passes could then lock the SAME two
	// qualifying rows in OPPOSITE orders — pass 1 locks row X then waits on row Y, while pass 2 locks Y
	// then waits on X — an ordinary lock-ordering deadlock between two instances of the SAME statement,
	// independent of the cross-repository cycle above. `ORDER BY id` gives every invocation of this
	// statement the same total lock order (ascending id), so two concurrent passes can only ever
	// contend for the SAME next row, never deadlock against each other waiting in opposite directions.
	//
	// Blocking (this statement's own design) vs. `SKIP LOCKED`: deliberately blocking, not skipping.
	// A locked row this pass skips would still self-heal on the NEXT scheduled run (per this method's
	// own "up to one additional grace window of delay" consequence, discussed below) — so skipping
	// would not be UNSAFE. But the realistic contention this statement meets is another SHORT-LIVED
	// transaction (an admin comp removal, a single-row CAS writer) that releases its lock in
	// milliseconds to at most a few seconds, not a chronic, indefinitely-held lock. Blocking lets THIS
	// pass finish healing that row once the brief contention clears, rather than unconditionally
	// deferring it to tomorrow's run for no reason — maximizing how much this pass actually heals, at
	// the cost of an occasional short wait, now that ROUND 23's `FOR NO KEY UPDATE` + `ORDER BY id`
	// have closed the two ways that wait could turn into an unbounded deadlock instead of an ordinary,
	// short block. `SKIP LOCKED` would only be preferable if a row could plausibly stay locked for the
	// ENTIRE remainder of a run (e.g. some other pathological long-held transaction) — that would be a
	// bug in whatever holds the lock that long, not something this statement should paper over by
	// silently skipping the row it exists specifically to catch.
	ct, err := r.db.Exec(ctx, `
		WITH candidates AS (
			SELECT id FROM users
			WHERE `+plaidDisconnectRepairEligibleStatusPredicate+`
			  AND plaid_cleanup_pending_at IS NULL
			  AND is_deleted = FALSE
			  AND deletion_requested_at IS NULL
			  AND `+liveLinkedItemExistsClause+`
			`+noActiveManualOverridePredicate+`
			ORDER BY id
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET plaid_cleanup_pending_at = clock_timestamp(), updated_at = NOW()
		FROM candidates
		WHERE users.id = candidates.id
	`)
	if err != nil {
		return 0, fmt.Errorf("StampMissingPlaidCleanupMarkers: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

// anyLinkedItemExistsClause is the EXISTS predicate for "this user has at least one linked_items
// row at all" — live or not. Deliberately separate from liveLinkedItemExistsClause (no status
// filter here): ClearCompletedPlaidCleanupMarkers below needs to distinguish "zero rows ever"
// (the #1453 allowance — a never-linked-Plaid cancellation or genuine migration residue, both
// legitimate and indistinguishable at the row level) from "rows exist, but none are live" (a real,
// completed episode whose marker should have been cleared). Correlates against the unaliased
// `users` table, matching liveLinkedItemExistsClause's own convention.
const anyLinkedItemExistsClause = `EXISTS (
	SELECT 1 FROM linked_items
	WHERE linked_items.user_id = users.id
)`

// uncleanedLinkedItemExistsClause is DeleteUserCascade's guard predicate for "this user still has
// a linked_items row that needs Plaid-side cleanup" — Issue #1444 (Site C): the prior spelling
// (`status <> 'disconnected'`) was a fourth, independently-worded copy of this same "is this item
// live" question, and it was wrong in two ways. First, NULL-unsafe: SQL's three-valued logic makes
// `NULL <> 'disconnected'` evaluate to UNKNOWN, not TRUE, so a genuinely NULL-status row (schema-
// legal — linked_items.status has no NOT NULL constraint) was silently excluded from the EXISTS
// check, letting DeleteUserCascade proceed past its own guard and hard-delete the only local
// pointer to what may still be a live, billing Plaid access token. Second, it diverged from the
// rest of this file's "live" definition by treating 'replaced' items as still-uncleaned, when
// isLiveLinkedItemStatus/liveLinkedItemExistsClause already establish that a 'replaced' item (Issue
// #171 re-link) no longer needs Plaid-side cleanup. This clause reuses that SAME "live" status list
// — NOT 'disconnected' or 'replaced', NULL treated as active — parameterized on $1 (a plain
// user_id) rather than correlated against `users.id` like liveLinkedItemExistsClause/
// anyLinkedItemExistsClause, because DeleteUserCascade's guard query is not already joined to
// `users`.
// Issue #1495 — THE STATUS TEST IS GONE, replaced by plaid_removed_at IS NULL, and this is a
// deliberate TIGHTENING, not a rewording. The paragraph above is preserved because its NULL-safety
// history is still the reason this const exists at all, but its second half was WRONG: it asserted
// that 'replaced' items "no longer need Plaid-side cleanup". They do. The re-link path
// (services/account_service.go, "re-link detected, soft-deleting old linked item") sets the OLD
// item's status to 'replaced' WITHOUT ever calling Plaid /item/remove, so a 'replaced' Item is
// still live and still billing — and because 'replaced' was the one status this guard TRUSTED, a
// GDPR erasure could complete over exactly such an Item, hard-deleting the only local pointer to
// it. That is verbatim the outcome this guard's call-site comment says it exists to prevent,
// reached through the guard itself. Two independent reviewers rated it Critical.
//
// It now FAILS CLOSED on the one fact that actually answers the question: block while
// plaid_removed_at IS NULL, regardless of status. Only a recorded, successful Plaid removal
// (LinkedItemRepository.MarkPlaidRemoved, or migration 000132's explicitly-assumed backfill of
// pre-existing 'disconnected' rows) lets the cascade past this point.
//
// NULL-SAFETY, and why the sibling `status IS NULL OR` arm is not merely dropped but MOOT here:
// `plaid_removed_at IS NULL` is an IS NULL test, which is two-valued — it returns TRUE or FALSE,
// never UNKNOWN — so the three-valued-logic hole that made `status <> 'disconnected'` silently
// exclude NULL-status rows cannot arise. The sibling predicates that still READ status
// (liveLinkedItemExistsClause here, cleanupCandidateSelectQuery in linked_item_repository.go) keep
// their NULL-safe spelling for the original reason; this one has nothing left to make NULL-safe.
// Do NOT "restore symmetry" by re-adding a status arm: any status arm here is an OR-ed escape from
// a guard whose entire job is to have none.
//
// WIDENED POPULATION, disclosed. Items whose Plaid removal can never succeed now block their
// user's erasure, and this change ADDS 'replaced' rows to that population. Accepted: fail-closed is
// the correct direction for an irreversible hard delete, production has zero users today, and
// issue #1494 tracks the operator escape hatch (alerting plus an audited manual override).
//
// Issue #1494 — THE PERMANENT-BLOCK ESCAPE, and it is the narrowest one available. The paragraph
// above described the accepted cost: an item whose /item/remove can NEVER succeed (a token that is
// invalid or absent, not a Plaid outage) blocks its user's GDPR erasure FOREVER. That cost is no
// longer accepted, and the way out is NOT a wider notion of "removed" — nothing here now believes an
// unremovable Item is gone. It is a SECOND, separately-recorded fact: plaid_removal_failed_at (see
// LinkedItemRepository.MarkPlaidRemovalFailed and services.terminalPlaidRemovalFailure). A row carrying
// it stops blocking — but only once it has carried it for 7 days, and only because the cascade has a
// durable place to put the obligation instead. Both conditions live in uncleanedLinkedItemPredicate
// below; read its comment before touching either, since the window is what stops one misconfigured
// deployment from erasing over every Item it holds.
//
// ⚠ THIS PREDICATE IS ONLY SAFE IN THE PRESENCE OF THE plaid_orphaned_items WRITE. DeleteUserCascade
// copies every row matching `plaid_removed_at IS NULL` into plaid_orphaned_items, in the SAME
// transaction, BEFORE any delete (see the call site). Remove or reorder that INSERT and this
// predicate becomes a silent leak: erasure completing over a live, billing Plaid Item with no record
// anywhere — the precise outcome the #1495 tightening above exists to prevent, reached from the
// other side. The two changes are one change; pinned by
// TestDeleteUserCascade_Integration_OrphanRecordInsertFailure_AbortsTheWholeCascade.
//
// Note what is deliberately NOT here: no status arm (see the NULL-safety paragraph above), and no
// reading of plaid_removal_failed_at by any predicate that SELECTS cleanup work. A terminally-marked
// row keeps being re-attempted by the sweep and the backstop for as long as it exists — this clause
// grants permission to PROCEED, never permission to STOP TRYING. See migration 000133.
const uncleanedLinkedItemExistsClause = `EXISTS (
	SELECT 1 FROM linked_items WHERE user_id = $1 AND ` + uncleanedLinkedItemPredicate + `
)`

// uncleanedLinkedItemPredicate is the row-level test shared by uncleanedLinkedItemExistsClause (the
// C2 guard) and CountUncleanedLinkedItems (DeletionService's diagnosis for a Plaid-less deployment,
// Issue #1494). Extracted so the two can never drift: the guard decides whether the cascade may
// proceed, and the count explains to an operator why it would not. A hand-written second copy of
// this predicate would be the fifth independently-worded spelling of a "is this item outstanding"
// question in this file — #1444 (Site C) was caused by exactly that, and the wording that diverged
// was the one nobody re-read.
//
// Reads as: we have neither proved this Item's Plaid removal succeeded, nor OBSERVED for long enough
// that we can never achieve it. Only in that state is there still recoverable work owed, and only then
// must an irreversible erasure wait.
//
// THE COOLING-OFF WINDOW IS THE POINT OF THE SECOND CLAUSE, and it exists because the terminal verdict
// is an inference, not a fact Plaid stated. Plaid returns INVALID_ACCESS_TOKEN both for a token that is
// genuinely dead AND when a token is used against the wrong Plaid environment or client_id — a property
// of the DEPLOYMENT's configuration, curable by an ops fix, and identical in kind to INVALID_API_KEYS,
// which services.terminalPlaidRemovalFailure deliberately refuses to treat as terminal. Unblocking on
// first sight would therefore let ONE misconfigured deploy mark every Item in the estate hopeless and
// then erase over all of them — irreversibly, since the erasure destroys the token.
//
// Requiring the verdict to be 7 days old converts the inference into an OBSERVATION. Nothing stops
// retrying during that week (no work-selecting predicate reads plaid_removal_failed_at — see migration
// 000133), so the cleanup sweep, the stranded-cleanup backstop and the deletion flow each keep making
// real /item/remove calls daily. A config problem is fixed inside the window and the row converges on
// plaid_removed_at with no orphan record at all; a genuinely dead credential answers identically every
// day for a week and only then passes. That is also what makes migration 000133's self-healing claim
// true rather than aspirational: for a user being deleted, the erasure would otherwise consume the
// verdict on the same pass that produced it, leaving the retries no time to occur.
//
// 7 days is a judgement, and here is its basis: a Plaid environment/credential misconfiguration is an
// operations incident measured in hours to days, while a dead token persists indefinitely; and the
// deletion this delays has already waited out a 30-day grace period, so a further week on a pathological
// row is a far smaller harm than erasing over a recoverable, still-billing Item. The window is written
// as an INTERVAL literal rather than a parameter because both readers of this predicate are guards, not
// tunables — changing it should be a code change with this argument re-read.
//
// The comparison is between two timestamptz values, so it is an absolute-instant comparison and immune
// to the session-time-zone hazard .claude/skills/db-migration-guide documents for migrations.
const uncleanedLinkedItemPredicate = `(
	plaid_removed_at IS NULL
	AND (plaid_removal_failed_at IS NULL OR plaid_removal_failed_at > now() - INTERVAL '7 days')
)`

// householdOwnerBlocksDeletionClause is DeleteUserCascade's guard predicate for "deleting this
// user would destroy another real user's data via households.owner_user_id's CASCADE" — Issue
// #1440 Critical finding (Codex review). See the guard's call site (above, in DeleteUserCascade)
// for the full rationale; this const only holds the SQL.
//
// Two independent conditions, OR'd together:
//  1. The #1367 rule: at least one ACTIVE, NON-OWNER household_members row.
//  2. Broader than #1367: any CURRENTLY EFFECTIVE subscription_entitlements row this household
//     funds (household_id = h.id) for someone OTHER than the owner (se.user_id <> h.owner_user_id),
//     regardless of that beneficiary's current household_members.status — closing the residual
//     where RemoveMember flipped a member to 'removed' without also touching their
//     owner-funded entitlement.
//
// Round-3 correction (Issue #1440 follow-up, Codex review, coordinator's own brief — Finding 1 and
// Finding 6, both High):
//
//   - Finding 1: condition 2 used to have NO filter on effective_until/effective_from — matching
//     ANY subscription_entitlements row ever written for this household, live or long since closed.
//     But member removal (household_service.go's removeMemberTx) only EXPIRES the departing
//     member's entitlement — entRepo.ExpireActiveEntitlement sets effective_until = now() and
//     entitlement_repository.go deliberately RETAINS the historical row (see
//     ExpireActiveEntitlement/ExpireActiveHouseholdEntitlement's own doc comments — both a
//     bookkeeping UPDATE, never a DELETE). So a household with only its owner currently active,
//     plus a FORMER member's now-closed entitlement, hit ErrHouseholdHasActiveMembers FOREVER —
//     the ordinary, everyday shape of "a member left at some point," not a contrived edge case —
//     and that owner could never complete a GDPR deletion. Fixed by requiring
//     `se.effective_until IS NULL AND se.effective_from <= now()`, the EXACT predicate
//     entitlement_repository.go's own GetActiveEntitlement already uses to distinguish a live
//     entitlement from historical residue — this guard must agree with that definition, not invent
//     its own.
//   - Finding 6: condition 1 used to be `COUNT(*) > 1` (the owner's own active row plus at least
//     one other) — silently DEPENDING on an invariant the schema does not enforce: that
//     household_members always contains an 'active' row for the owner. There are ZERO
//     constraints tying household_members.user_id to households.owner_user_id (migration
//     000107:12-15 documents this as a service-layer invariant only; :94-102 enforces uniqueness,
//     not existence). A schema-valid household with one active NON-OWNER member, NO active owner
//     row at all, and no entitlement passed `COUNT(*) > 1` (count exactly 1) — silently permitting
//     the owner's deletion, and households.owner_user_id's CASCADE (migration 000106:54-62)
//     destroyed that OTHER real user's household_members row. Fixed by counting active members
//     `WHERE hm.user_id <> h.owner_user_id` directly — the guard no longer depends on the owner's
//     own membership row existing at all.
//
// Correlates against households.owner_user_id = $1 directly (not via a household_members join for
// the owner's own row) since households.owner_user_id IS the user_id being deleted — mirrors
// uncleanedLinkedItemExistsClause's own $1-parameterized style, for the same reason (this guard
// query is not already joined to `users`).
const householdOwnerBlocksDeletionClause = `EXISTS (
	SELECT 1 FROM households h
	WHERE h.owner_user_id = $1
	  AND (
	    (SELECT COUNT(*) FROM household_members hm WHERE hm.household_id = h.id AND hm.status = 'active' AND hm.user_id <> h.owner_user_id) > 0
	    OR EXISTS (
	      SELECT 1 FROM subscription_entitlements se
	      WHERE se.household_id = h.id AND se.user_id <> h.owner_user_id
	        AND se.effective_until IS NULL AND se.effective_from <= now()
	    )
	  )
)`

// ClearCompletedPlaidCleanupMarkers is StampMissingPlaidCleanupMarkers's symmetric counterpart —
// Issue #1413 Finding 1 (Codex review, PR #1443 follow-up, headline finding): the self-healing
// backstop round 16 added only heals ONE of the two ways plaid_cleanup_pending_at can violate
// domain.User.PlaidCleanupPendingAt's invariant. It stamps a marker that is wrongly NULL (episode
// open, nothing recorded); it does nothing for a marker wrongly left SET after its episode's
// cleanup has already completed. That second direction was, until this fix, believed harmless —
// several comments across this branch called it "inert" or "untidy residue" the candidate query's
// own live-item EXISTS clause filters out forever. That belief was WRONG, and verified wrong by a
// concrete five-step scenario:
//
//  1. repairPlaidDisconnects disconnects a user's items successfully, but all
//     plaidCleanupMarkerClearMaxAttempts marker-clear retries fail (a genuine, if rare, DB blip
//     lasting longer than the retry loop's total backoff window). The old marker timestamp T
//     survives, non-NULL, on a user with zero LIVE linked items.
//  2. That user has no live item, so GetUsersNeedingPlaidDisconnectRepair's own EXISTS clause never
//     selects them again — nothing revisits T. Without this method, T would sit there indefinitely.
//  3. An admin grants a comp. The user is effectively active again and relinks a bank — a genuinely
//     NEW live linked item now exists, but T (the OLD episode's timestamp) is still what is
//     recorded.
//  4. The admin removes the comp. AT THE TIME this scenario was written, removeUserTierOverrideUpdate's
//     plaid_cleanup_pending_at CASE used COALESCE(u.plaid_cleanup_pending_at, NOW()) (admin_user_repository.go),
//     which preserved the stale T instead of starting a fresh episode, because the column was never
//     NULL to begin with.
//  5. The next repairPlaidDisconnects pass now sees a live item and a marker (T) already older than
//     plaidDisconnectRepairGracePeriod — and disconnects the newly-linked bank IMMEDIATELY, with
//     ZERO grace period. The grace window exists precisely so a lapsed user has time to recover;
//     this path silently skips it entirely for a bank connection that is only hours old.
//
// Round-18 correction (admin_user_repository.go, removeUserTierOverrideSelectPrior's own doc
// comment): step 4's COALESCE was independently REMOVED by a later round, for a different reason
// (a stale marker resurrected across an ordinary ongoing-episode restart, no Clear-failure required
// at all) — should_mark_plaid_cleanup_pending's CASE now overwrites UNCONDITIONALLY (the
// manual_tier_ends_at anchor when lapsed, clock_timestamp() otherwise; see that file's ROUND
// 18/19/20 notes), so step 4 as literally described can no longer happen via THIS writer regardless
// of whether this method ever runs. This method is KEPT anyway, not made redundant by that fix: it
// still closes the population step 4 never depended on a comp for at all — a stale marker with zero
// live items that a comp is NEVER granted over, which sits uncorrected indefinitely with nothing
// else in this codebase ever revisiting it (see the asymmetry paragraph below, and
// removeUserTierOverrideSelectPrior's own "Reassessing ClearCompletedPlaidCleanupMarkers" note for
// the identical conclusion reached independently from the writer side).
//
// The fix restores the invariant's SECOND half at runtime, not just on paper:
// assertPlaidMarkerInvariant (profile_repository_plaid_marker_invariant_integration_test.go) has,
// since round 16, already classified "marker set, ineligible status, at least one linked_items row,
// none of them live" as a violation — nothing before this method ever enforced it. Symmetric with
// StampMissingPlaidCleanupMarkers:
//
//   - Stamp: ineligible status, live linked item, marker IS NULL -> set NOW().
//   - Clear (this method): ineligible status, marker IS NOT NULL, at least one linked_items row,
//     no LIVE linked_items row -> set NULL.
//
// The "at least one linked_items row" condition is MANDATORY, not optional — this is the #1453
// allowance, unchanged: a user with ZERO linked_items rows at all (an ordinary never-linked-Plaid
// cancellation, or genuine 000119/000120 migration residue — indistinguishable at the row level,
// both legitimate, both reachable) keeps whatever marker they have. Clearing those would silently
// change behavior for a population this branch has deliberately, repeatedly left alone (see
// anyLinkedItemExistsClause's own doc comment). This is why the WHERE clause below uses
// anyLinkedItemExistsClause AND NOT liveLinkedItemExistsClause, rather than a single hand-derived
// "COUNT(live) = 0" expression that could not tell the two zero-linked-item populations apart.
//
// Deliberately does NOT include noActiveManualOverridePredicate, unlike StampMissingPlaidCleanupMarkers
// above — this is the one place this method is NOT a mirror of its sibling, and the asymmetry is
// load-bearing, not an oversight. Stamp excludes a currently-comped user because stamping only ever
// arms a marker for a population the candidate query would itself eventually act on, and the
// candidate query already excludes a live comp — deferring to "re-arm the ordinary way once the comp
// is removed" is correct there because the column starts from NULL either way. Clear has the
// opposite obligation: it corrects a FACT already true about linked_items (cleanup for this episode
// is done) regardless of whether an override happens to be live right now — see
// domain.User.PlaidCleanupPendingAt's doc comment: manual_tier_override is deliberately not a term
// in this invariant at all. Belt-and-braces against removeUserTierOverrideUpdate's own writer-side
// fix (ROUND 18, admin_user_repository.go), not merely redundant with it: that CASE only ever runs
// AT a removal, gated on a LIVE linked item existing right then; a stale marker on a user who is
// comped and STAYS comped (no removal ever happens, or one happens with no live item present yet)
// gets no help from that CASE at all, and would sit uncorrected for as long as that holds if this
// WHERE clause excluded a currently-comped user too. If this WHERE clause excluded a currently-comped
// user, a comp granted in the narrow window between a failed clear (step 1) and this backstop's next
// scheduled pass would suppress the heal for as long as the comp stays active.
//
// Same four properties as StampMissingPlaidCleanupMarkers, restated for the clear direction: (1)
// GetUsersNeedingPlaidDisconnectRepair is completely unchanged — this method only ever nulls a
// marker on a user that query's own live-item EXISTS clause would already exclude, so no candidate
// this backstop could ever select is affected; (2) one atomic UPDATE, so it cannot itself race —
// the same argument StampMissingPlaidCleanupMarkers's own doc comment makes; (3) a user this method
// clears gets a genuinely FRESH plaidDisconnectRepairGracePeriod the next time an episode opens for
// them (via Stamp, or via removeUserTierOverrideUpdate's plaid_cleanup_pending_at CASE — which, as
// of ROUND 18, overwrites unconditionally rather than COALESCE-preserving whatever this method left
// behind — now seeing NULL), never the OLD episode's stale grace window resumed; (4) reuses
// anyLinkedItemExistsClause and
// liveLinkedItemExistsClause — the SAME shared Go constants the candidate query, the backstop's own
// Stamp half, and admin_user_repository.go's removeUserTierOverrideSelectPrior already use — so
// "what counts as live" and "what counts as ever-linked" cannot silently drift between this method
// and any of them.
//
// Called from exactly one place, immediately after StampMissingPlaidCleanupMarkers: see
// RetentionCleanupCheckWorker.repairPlaidDisconnects's own doc comment (retention_cleanup_check.go)
// for why running this alongside Stamp, at the very start of EVERY pass, is what actually closes the
// scenario above within one backstop interval rather than leaving it open indefinitely.
//
// Deliberately two separate statements rather than one combined `CASE`-driven UPDATE touching both
// directions atomically: the two populations (marker IS NULL vs. marker IS NOT NULL) are disjoint by
// construction, so there is no row either statement could observe differently depending on run
// order, and this is a periodic healer re-run daily, not a transaction that must commit both halves
// together or not at all — each statement is independently idempotent and independently correct.
// Two statements read as two named, separately-countable operations (`healed` vs `cleared` in
// repairPlaidDisconnects's own log lines) rather than one CASE expression a reader has to mentally
// split back into two before they can even tell how many rows moved in which direction — clearer to
// read and clearer to count, at the acceptable cost of the pass as a WHOLE no longer being one atomic
// unit (each half already was, and always was, meant to stand alone).
//
// Same CAUTION as StampMissingPlaidCleanupMarkers above: this is a global-scan WRITE with no user-id
// parameter in its WHERE clause — every integration test fixture for this method must own its row
// end-to-end (unique, `t.Cleanup`'d), never rely on a shared test-setup helper.
//
// Deliberately NOT part of the exported ProfileRepository interface, for the same reason as
// StampMissingPlaidCleanupMarkers: needed only by RetentionCleanupCheckWorker's own narrow
// RetentionProfileRepository consumer interface (retention_cleanup_check.go).
func (r *profileRepository) ClearCompletedPlaidCleanupMarkers(ctx context.Context) (int, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET plaid_cleanup_pending_at = NULL, updated_at = NOW()
		WHERE `+plaidDisconnectRepairEligibleStatusPredicate+`
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND `+anyLinkedItemExistsClause+`
		  AND NOT `+liveLinkedItemExistsClause+`
	`)
	if err != nil {
		return 0, fmt.Errorf("ClearCompletedPlaidCleanupMarkers: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

// ClearMarkersOnEligibleStatus is the THIRD self-healing backstop — Issue #1455 — closing the
// plaid_cleanup_pending_at invariant's OTHER branch. domain.User.PlaidCleanupPendingAt's doc
// comment states the invariant in two halves:
//
//  1. status NOT ineligible (not inactive/cancelled) ⇒ marker must be NULL.
//  2. status ineligible ⇒ marker non-NULL exactly while an episode is open and cleanup has not
//     completed.
//
// StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers above both self-heal half
// 2 — but BOTH are gated on plaidDisconnectRepairEligibleStatusPredicate, so neither one EVER
// inspects a row whose status is not inactive/cancelled. Half 1 therefore had NO runtime backstop
// at all until this method: if a row ever ended up with a non-ineligible status AND a non-NULL
// marker, nothing in this codebase would ever repair it.
//
// Why this matters — the concrete failure this closes, restated from the issue: PR #1443 deleted
// the only writer that could produce this state (ProfileRepository.UpdateSubscriptionStatus, which
// wrote subscription_status without touching the marker and had zero production callers), which
// removes the IMMEDIATE trap but not the durable recurrence guard — nothing stops some FUTURE
// status-only writer from recreating the identical gap, and the invariant sweep enumerates known
// writers by hand, so a new one is invisible to it by default. If that ever happens: a stale marker
// survives on an active row at timestamp T; months later the user cancels;
// removeUserTierOverrideUpdate's should_mark_plaid_cleanup_pending CASE only overwrites the marker
// when a LIVE linked item exists at that exact moment — a cancellation with no comp in play at all
// takes an entirely different write path (UpdateSubscriptionFromPaddle et al.), and NONE of those
// COALESCE the marker (round 18 removed the one COALESCE that used to exist) but they also don't
// unconditionally overwrite an already-non-NULL value the way the comp-removal CASE does — so a
// pre-existing stale T can survive a raw cancellation-status write untouched, and the repair pass
// then sees a marker already older than the 72-hour grace window and disconnects the user's bank
// connections IMMEDIATELY, with ZERO grace. The grace window exists precisely so a lapsed user can
// recover; this path silently skips it.
//
// SHAPE: flat UPDATE, NOT StampMissingPlaidCleanupMarkers's `FOR NO KEY UPDATE` CTE dance — and this
// is a deliberate correction of the issue's own suggested implementation, not an oversight. Read
// StampMissingPlaidCleanupMarkers's doc comment in full: that CTE exists ONLY because that statement
// writes the VOLATILE clock_timestamp() into its SET list, and Postgres evaluates a volatile
// target-list expression against the pre-lock-wait row version — so the lock has to be acquired as
// its own explicit step (`FOR NO KEY UPDATE`) before the value is computed, or a concurrently-locked
// row gets a backdated timestamp. This method, like ClearCompletedPlaidCleanupMarkers right above
// it, writes only a CONSTANT (NULL) — there is no volatile expression for a lock-wait to backdate,
// so the CTE's entire reason for existing does not apply here. Adding one anyway would not be extra
// safety; it would needlessly re-open the exact lock-mode hazard that CTE's own doc comment spends
// several paragraphs warning about (a `FOR UPDATE`/`FOR NO KEY UPDATE` scan can conflict with a
// concurrent `FOR KEY SHARE` audit-log FK check and deadlock — SQLSTATE 40P01 — see
// StampMissingPlaidCleanupMarkers's ROUND 23 correction for the full reproduced cycle). A flat
// `UPDATE ... WHERE ...`, exactly ClearCompletedPlaidCleanupMarkers's shape, is both simpler and
// strictly safer here.
//
// JUDGEMENT CALL — does this exclude a currently-comped user? NO, deliberately, mirroring
// ClearCompletedPlaidCleanupMarkers's asymmetry with Stamp rather than Stamp's own exclusion.
// Reasoning: Stamp excludes an actively-comped user because STAMPING only ever arms a marker for a
// population the repair CANDIDATE query (GetUsersNeedingPlaidDisconnectRepair) would itself
// eventually act on, and that candidate query already excludes a live comp via
// noActiveManualOverridePredicate — deferring to "re-arm the ordinary way once the comp is removed"
// is correct there because the column starts from NULL either way, so nothing is lost by waiting.
// This method has the OPPOSITE obligation: like Clear, it corrects a FACT that is already true
// regardless of anything else — "this row's raw status is not ineligible" — and
// domain.User.PlaidCleanupPendingAt's own doc comment is explicit that manual_tier_override is NOT
// a term in this invariant at all ("the marker records only the durable fact ... a function of raw
// subscription_status and of whether live linked items exist, and of nothing else"). Branch 1 does
// not say "status not ineligible, unless comped" — it says "status not ineligible ⇒ NULL", full
// stop. Excluding a comped user here would leave exactly the corrupted state this method exists to
// close sitting untouched for as long as the comp stays active, for no compensating benefit (there
// is no future candidate-query action being deferred to, the way there is for Stamp) — so exclusion
// would be strictly worse with nothing gained. See
// TestClearMarkersOnEligibleStatus_Integration_ClearsRegardlessOfActiveOverride.
//
// Deliberately does NOT filter on deletion_requested_at, unlike its two siblings — this predicate
// is exactly the three clauses Issue #1455 specifies (status, marker non-NULL, is_deleted = FALSE):
// a user mid-GDPR-deletion-request can still have any subscription_status, and this invariant is
// defined purely in terms of raw subscription_status (see the doc comment above) — it has no
// dependency on the separate deletion pipeline, so gating on it here would be an unrequested,
// unmotivated restriction rather than genuine defense-in-depth.
//
// Reuses plaidDisconnectRepairEligibleStatusPredicate (negated) — the SAME shared Go constant every
// other status-eligibility check in this file already uses — rather than hand-writing a fifth
// spelling of "which statuses are eligible".
//
// Same CAUTION as its two siblings: a global-scan WRITE with no user-id parameter in its WHERE
// clause. Every integration test fixture for this method must own its row end-to-end (unique,
// t.Cleanup'd), never rely on a shared test-setup helper.
//
// Deliberately NOT part of the exported ProfileRepository interface, for the same reason as its two
// siblings: needed only by RetentionCleanupCheckWorker's own narrow RetentionProfileRepository
// consumer interface (retention_cleanup_check.go).
func (r *profileRepository) ClearMarkersOnEligibleStatus(ctx context.Context) (int, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET plaid_cleanup_pending_at = NULL, updated_at = NOW()
		WHERE NOT (`+plaidDisconnectRepairEligibleStatusPredicate+`)
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND is_deleted = FALSE
	`)
	if err != nil {
		return 0, fmt.Errorf("ClearMarkersOnEligibleStatus: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

// expiredManualTierOverridePredicate selects users with a manual tier override that has
// strictly passed (Issue #1413 Task 9 — the daily job that expires lapsed admin comps,
// performing the exact same clear-override/re-arm-retention/audit transition
// AdminUserRepository.RemoveUserTierOverride (Issue #1414 / Task 7) performs for
// admin-initiated removal, rather than duplicating that logic here).
//
// Boundary note: strictly past (`<`, not `<=`) to agree with
// domain.User.HasActiveManualOverride()'s `!ends_at.Before(now)` semantics — an override ending
// exactly now is still ACTIVE per the read path, so this predicate must not select it (see
// TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary).
//
// manual_tier_ends_at IS NOT NULL additionally excludes PERMANENT overrides (no expiry date
// set) — those never lapse on their own and must only ever be cleared by an explicit admin
// removal.
//
// Issue #1413 round-5 gap G7 (PR #1443 gap analysis) — deliberately NOT a strict complement of
// domain.User.HasActiveManualOverride() / hasActiveManualOverrideAt(), unlike
// noActiveManualOverridePredicate's genuine parity with that same Go method. They agree on the
// override-value checks (`IS NOT NULL AND <> ''` mirrors the Go method's nil/empty guard) and on
// the expiry boundary (SQL `< NOW()` and Go `Before(now)` are both strict, so an exact tie is
// "not expired" on both sides). They diverge on TWO independent cases:
//
//  1. A NULL manual_tier_ends_at. This predicate's `manual_tier_ends_at IS NOT NULL` excludes
//     such a row outright (a permanent override never lapses, so it is never a candidate here),
//     while HasActiveManualOverride() treats a NULL end date as an indefinite, still-live comp —
//     i.e. "active", not "expired".
//  2. `is_deleted = FALSE`, which has NO counterpart in the Go method at all — that method never
//     looks at is_deleted. So a soft-deleted user with a lapsed override is "expired" to Go but
//     is simply not selected here.
//
// Both are correct for what each asks: this predicate answers "has this override's clock run out
// on a row this job should act on", the Go method answers "is this override in effect right now".
//
// Do NOT write a naive SQL/Go parity test asserting "predicate selects <=> Go reports expired"
// for this pair (of the kind that correctly exists for noActiveManualOverridePredicate, see
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo) — it would fail on both of the
// correct, intentional divergences above, and the failure would look exactly like a real bug,
// tempting a "fix" that breaks permanent overrides instead. An honest parity test is possible
// only when scoped to BOTH non-NULL manual_tier_ends_at AND is_deleted = FALSE; scoping to the
// expiry column alone is NOT sufficient and still fails on soft-deleted rows. (The two-divergence
// correction is Codex's round-6 Medium finding on the round-5 gap analysis; the original wording
// here claimed a single divergence and understated the required scope.) See
// ManualTierOverrideExpiryCheckWorker.Work's doc comment (manual_tier_override_expiry_check.go)
// for the Go-side half of this note.
const expiredManualTierOverridePredicate = `
	  manual_tier_override IS NOT NULL
	  AND manual_tier_override <> ''
	  AND manual_tier_ends_at IS NOT NULL
	  AND manual_tier_ends_at < NOW()
	  AND is_deleted = FALSE
`

// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9).
func (r *profileRepository) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE ` + expiredManualTierOverridePredicate + `
		ORDER BY manual_tier_ends_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// malformedTierOverrideWhereClause is GetUsersWithMalformedTierOverride's candidate scope: every
// row carrying a non-empty manual_tier_override, ordered for a stable, reproducible scan. Validity
// (does the value name a currently-configured tier) is NOT checked in SQL at all — see that
// method's own doc comment for why.
const malformedTierOverrideWhereClause = ` WHERE manual_tier_override IS NOT NULL AND manual_tier_override <> '' AND is_deleted = FALSE ORDER BY id`

// GetUsersWithMalformedTierOverride is Issue #1453's monitoring query: it returns every user whose
// manual_tier_override is non-empty but does NOT name a currently-configured tier — the population
// migration 000127's one-time backfill cleared, and the population any FUTURE writer that bypasses
// admin_user_service.go's write-time validation (SetOverrideTierValidator / tierConfig.GetTier)
// could still reintroduce.
//
// Deliberately reuses r.overrideTierValidator — the SAME injected validator
// GetEffectiveSubscriptionStatusByUserID already uses on the read-access path (see
// SetOverrideTierValidator's doc comment) — rather than hand-copying config/subscription_tiers.yaml's
// tier list into a SQL literal. This is the reason Issue #1453 rejected a CHECK constraint (schema
// duplicating application config, drifting the next time a tier is added/renamed) but accepted a
// migration backfill (a one-time point-in-time cleanup that never runs again, so staleness
// afterward is a non-issue): an ONGOING monitoring query has the identical staleness problem a
// perpetual CHECK constraint would have if it hand-copied the tier list, so it must not do that
// either. Reusing the validator means "configured" can never drift from what every other
// enforcement seam in this codebase already treats as valid.
//
// Returns (nil, nil) — not an empty slice — when no validator has been wired
// (SetOverrideTierValidator never called). This is a monitoring aid, not an enforcement path, and
// "unknown whether any rows are malformed" is a materially different claim from "zero malformed
// rows found" — silently reporting the latter when the real answer is the former would hide a
// misconfigured caller behind a false-negative "all clear" the first time someone checks this query.
func (r *profileRepository) GetUsersWithMalformedTierOverride(ctx context.Context) ([]*domain.User, error) {
	if r.overrideTierValidator == nil {
		return nil, nil
	}
	candidates, err := r.queryUsers(ctx, userSelectQuery+malformedTierOverrideWhereClause)
	if err != nil {
		return nil, err
	}
	malformed := make([]*domain.User, 0, len(candidates))
	for _, u := range candidates {
		if u.ManualTierOverride != nil && !r.overrideTierValidator(*u.ManualTierOverride) {
			malformed = append(malformed, u)
		}
	}
	return malformed, nil
}

// OrphanedFKlessUserScopedRowCounts is GetOrphanedFKlessUserScopedRowCounts's result — the current
// count, per table, of rows whose user_id/actor_user_id no longer names any real user.
type OrphanedFKlessUserScopedRowCounts struct {
	DeadLetterJobs  int64
	IdempotencyKeys int64
	MCPAuditLog     int64
}

// GetOrphanedFKlessUserScopedRowCounts is Issue #1440 round-3 Finding 5's self-healing/
// observability backstop (Codex review, coordinator's #1440 follow-up brief): reports, per table,
// how many rows in the three user-scoped tables with NO foreign key to users at all
// (dead_letter_jobs, idempotency_keys, mcp_audit_log — see DeleteUserCascade's own doc comment for
// why the schema cannot enforce their cleanup automatically) currently reference a
// user_id/actor_user_id that no longer exists in `users`.
//
// This state can ONLY arise from a concurrent writer's insert landing in the narrow window between
// DeleteUserCascade's own explicit erasure of these three tables and its transaction's commit (see
// that method's own "round-3 reordering" doc comment for the full race — none of these tables has
// any FK to users, so the FOR UPDATE lock DeleteUserCascade holds on the users row never blocks a
// concurrent insert into any of them). Reordering DeleteUserCascade's own erasure to run
// immediately before commit shrinks that window to (in practice) milliseconds, but cannot close it
// entirely — this method exists so that residual is OBSERVABLE (RetentionCleanupCheckWorker's
// monitorOrphanedFKlessRows reports it every scheduled pass) rather than silent and undiscoverable,
// mirroring monitorMalformedTierOverrides' own observability-not-repair pattern for a different
// residual this codebase already accepted it cannot fully prevent at write time.
//
// idempotency_keys is additionally (and independently) self-healing regardless of this method:
// IdempotencyCleanupWorker (jobs/idempotency_cleanup.go) sweeps every row past its 24-hour
// expires_at TTL daily, so an orphaned row in that table self-resolves within one day even without
// this backstop. dead_letter_jobs and mcp_audit_log have no equivalent TTL sweep, so an orphaned row
// in either persists until an operator acts on what this method surfaces.
//
// A single round-trip UNION-free query (one for each table, scalar-subquery style) — this is a
// monitoring check run once per scheduled pass, not a hot path, so the three independent COUNT
// scans are an acceptable trade for a single, simple query shape. dead_letter_jobs.user_id is
// VARCHAR (not UUID — see migration 000030:179), so it is compared against users.id cast to text;
// idempotency_keys.user_id and mcp_audit_log.actor_user_id are both UUID NOT NULL and compare
// directly.
func (r *profileRepository) GetOrphanedFKlessUserScopedRowCounts(ctx context.Context) (OrphanedFKlessUserScopedRowCounts, error) {
	var counts OrphanedFKlessUserScopedRowCounts
	err := r.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM dead_letter_jobs d
			 WHERE d.user_id IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id::text = d.user_id)),
			(SELECT COUNT(*) FROM idempotency_keys i
			 WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = i.user_id)),
			(SELECT COUNT(*) FROM mcp_audit_log m
			 WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = m.actor_user_id))
	`).Scan(&counts.DeadLetterJobs, &counts.IdempotencyKeys, &counts.MCPAuditLog)
	if err != nil {
		return OrphanedFKlessUserScopedRowCounts{}, fmt.Errorf("GetOrphanedFKlessUserScopedRowCounts: %w", err)
	}
	return counts, nil
}

// RetentionWithholdingCounts is the population Issue #1569 T1's gate is holding back, split into
// the states an operator has to be able to tell apart. See CountRetentionDeletionsWithheld.
type RetentionWithholdingCounts struct {
	// WithheldNoticeMissing is the count of users the hard-delete sweep is REFUSING because at
	// least one promised pre-deletion notice was never recorded: every condition of
	// GetUsersWithExpiredRetentionPage's predicate holds EXCEPT
	// retentionNoticesDeliveredPredicate. Every one of these accounts is held indefinitely, because
	// the notice queries cannot reach a user whose clock has already run out (see below).
	WithheldNoticeMissing int
	// NoticePending is the HEALTHY population and is deliberately disjoint from the one above: a
	// user whose retention clock is still RUNNING with a notice marker still NULL. Reported so the
	// alert on the withheld count can be shown not to be firing on ordinary cancellations.
	NoticePending int
}

// CountRetentionDeletionsWithheld reports how many accounts the Issue #1569 T1 notice gate is
// holding back from hard deletion, and how many are merely awaiting a notice that is still due. It
// is the observability half of that gate and it is not optional:
// retentionNoticesDeliveredPredicate converts "deleted with no warning ever sent" into "retained
// indefinitely", and a refusal nobody can see is how an account accumulates in silence.
//
// ⚠ WHY THE CLOCK SPLITS THE POPULATION, and why an alert that ignores it is worthless.
// `retention_notice_30d_sent_at IS NULL` means two completely different things:
//
//   - retention_expires_at > NOW(): the notice sweep can still reach this user.
//     GetUsersNeedingRetention30dNotice and GetUsersNeedingRetention7dNotice both bound their
//     population by exactly that comparison, so the marker is simply not due yet. Healthy, and the
//     ordinary state of every cancelled account. Counted as NoticePending, never alerted on.
//   - retention_expires_at <= NOW(): those same two queries can NEVER select this user again, their
//     bound being the opposite one. The missing notice will not arrive by any existing path, so the
//     gate holds the row forever. Counted as WithheldNoticeMissing, and this is the alert.
//
// ⚠ ONE REASON THIS COUNT DELIBERATELY CANNOT SEE: a withheld user with NO EMAIL ADDRESS on file.
// canSendRetentionNotice (queue/jobs/retention_cleanup_check.go) checks exactly that, on the
// DECRYPTED value, and it is a genuinely distinct reason a notice can never be sent. No SQL
// predicate here can reproduce it, and the first draft of this method that tried
// (`email IS NULL OR email = ''`) was silently dead code: users.email stores CIPHERTEXT, and
// FieldEncryptor.EncryptString returns "ENC$"+ct for EVERY input INCLUDING the empty string
// (crypto/field_encryptor.go), so an addressless encrypted row never matches an empty-string test.
// The deterministic email_blind_idx COULD discriminate, as GetUserByEmail does — but migration
// 000061 puts a FULL UNIQUE index on that column, so at most ONE row in the entire database can
// carry the empty address, and a Prometheus reason label whose population is capped at one is not
// worth its cardinality. Such a user is therefore counted in WithheldNoticeMissing like any other:
// correct for the alert (it IS withheld, and the alert fires), merely imprecise for the diagnosis.
//
// ⚠ SINCE ISSUE #1584 THE DIAGNOSIS IS AVAILABLE ELSEWHERE, and nothing in the paragraph above is
// weakened by it. GetRetentionDeletionsWithheldPage enumerates this same population through
// queryUsers, which DECRYPTS email for every row it returns, so it reports HasEmailOnFile at
// exactly the point canSendRetentionNotice observes it. That is a difference in KIND, not in
// cleverness: it is a per-row read rather than a labelled aggregate, so neither the ciphertext
// problem nor the UNIQUE-index cardinality argument applies to it. This method still cannot
// separate the reason, and still should not try.
//
// The process-level half of the reason (is an email service wired at all?) is likewise invisible to
// SQL and is applied by the caller — see
// RetentionCleanupCheckWorker.monitorRetentionDeletionsWithheld.
//
// ⚠ WHAT IS SPLICED AND WHAT IS NOT, stated precisely because the first version of this comment
// claimed the whole predicate was spliced when only one constant was:
//   - The WITHHELD half — the alerted number — is now the WHOLE clause spliced, as
//     retentionWithheldPopulationPredicate (Issue #1584), which itself splices
//     expiredRetentionClockPredicate and noManualOverrideAtAllPredicate verbatim from the sweep and
//     restates only `is_deleted = FALSE` / `deletion_requested_at IS NULL`, exactly as
//     GetUsersWithExpiredRetentionPage itself restates them. It shares that constant with
//     GetRetentionDeletionsWithheldPage, so this number and the list an operator reads beneath it
//     cannot disagree.
//   - The PENDING half CANNOT splice expiredRetentionClockPredicate: its clock bound is the
//     OPPOSITE one (`> NOW()`), which is the very reason that constant's own doc comment gives for
//     not applying it to GetUsersNeedingRetention30dNotice/7dNotice either. It DOES splice
//     retentionNoticesMissingPredicate, which is common to both halves. Its status test is
//     restated, as those two queries also restate it. A status added to
//     expiredRetentionClockPredicate would therefore reach the withheld count but not the pending
//     count — acceptable, because pending is never alerted on, but do not read the two halves as
//     equally protected.
//
// One round trip, two counting subqueries, no row loading: this runs on every scheduled pass and
// the population it reports should normally be empty.
func (r *profileRepository) CountRetentionDeletionsWithheld(ctx context.Context) (RetentionWithholdingCounts, error) {
	var counts RetentionWithholdingCounts
	const query = `
		SELECT
			(SELECT COUNT(*) FROM users ` + retentionWithheldPopulationPredicate + `),
			(SELECT COUNT(*) FROM users
			  WHERE is_deleted = FALSE
			    AND deletion_requested_at IS NULL
			    AND retention_expires_at IS NOT NULL
			    AND retention_expires_at > NOW()
			    AND subscription_status IN ('inactive', 'cancelled')
			` + noManualOverrideAtAllPredicate + retentionNoticesMissingPredicate + `)
	`
	if err := r.db.QueryRow(ctx, query).Scan(&counts.WithheldNoticeMissing, &counts.NoticePending); err != nil {
		return RetentionWithholdingCounts{}, fmt.Errorf("CountRetentionDeletionsWithheld: %w", err)
	}
	return counts, nil
}

// RetentionWithheldAccount is one account the Issue #1569 T1 notice gate is holding back, as
// GetRetentionDeletionsWithheldPage returns it.
//
// ⚠ IT CARRIES NO EMAIL ADDRESS, ONLY WHETHER THERE IS ONE. This projection is built for an admin
// surface, and an operator deciding what to do about a stuck account needs to know that an address
// exists, never what it is. Returning the decrypted address would put a plaintext email into an
// HTTP response and into whatever logs sit behind it, for no decision it would help anyone make.
type RetentionWithheldAccount struct {
	UserID string
	// SubscriptionStatus is 'inactive' or 'cancelled' — expiredRetentionClockPredicate admits no
	// other value. Returned anyway so the surface reports what was actually read rather than what
	// the predicate is believed to allow.
	SubscriptionStatus string
	// RetentionExpiresAt is in the PAST for every row here, by construction. It is the operator's
	// severity ordering (how long has this account been stuck?) and the keyset pagination key.
	RetentionExpiresAt time.Time
	// Notice30dSentAt / Notice7dSentAt say WHICH promise was not kept. At least one is nil in every
	// row — that is what makes the row withheld — and both being nil is the ordinary case, since no
	// EmailService is wired in any environment today.
	Notice30dSentAt *time.Time
	Notice7dSentAt  *time.Time
	// HasEmailOnFile is the FOURTH withholding reason, and this is the only surface in the codebase
	// that can report it. See the method doc.
	HasEmailOnFile bool
}

// GetRetentionDeletionsWithheldPage ENUMERATES the accounts CountRetentionDeletionsWithheld merely
// counts — Issue #1584, closing the selection gap the TODO in
// RetentionCleanupCheckWorker.monitorRetentionDeletionsWithheld named.
//
// THE STATE IT REACHES. Issue #1569 T1 gated the hard-delete sweep on both promised pre-deletion
// notices having been recorded (retentionNoticesDeliveredPredicate), and BOTH notice queries bound
// their population by `retention_expires_at > NOW()` (GetUsersNeedingRetention30dNotice and its 7d
// sibling). An account whose clock runs out with a marker still NULL therefore cannot be deleted
// BY THE RETENTION SWEEP, or noticed by the current notice queries. The metric calls that state
// notice_impossible_window_passed.
//
// ⚠ SCOPED TO THE SWEEP ON PURPOSE — an earlier draft said "it can never be deleted", which is
// globally false and falsely reassuring. User-requested GDPR erasure is the OTHER authorisation
// and it is untouched: userRequestedDeletionClaimPredicate requires `deletion_requested_at IS NOT
// NULL`, retentionWithheldPopulationPredicate requires that same column to be NULL, so the two
// populations are disjoint by construction — a withheld account that requests erasure leaves this
// enumeration in the same statement that makes it a user-requested deletion candidate, and
// services/deletion_service.go's ExecuteDeletion routes it by kind rather than treating every
// deletion as a retention deletion. Nothing in #1569 T1 blocks a user's own erasure request, and
// writing otherwise here would put a GDPR refusal one confident reader away. Proved by
// TestRetentionWithheldEnumeration_Integration_UserRequestedDeletionStillDeletesAWithheldAccount.
//
// The population is INACTIVE OR CANCELLED, not cancelled alone — expiredRetentionClockPredicate's
// `subscription_status IN ('inactive', 'cancelled')`, spliced (not restated) into the shared
// predicate below, so the count and this enumeration cannot disagree about who is in it.
//
// ⚠ CORRECTING #1584'S OWN PREMISE, because it is half stale and re-deriving it from the ticket
// would waste a round: the ticket says "no existing query can SELECT these users at all". They have
// been COUNTED, by reason, since T1 — CountRetentionDeletionsWithheld. What did not exist is a read
// that returns the ROWS, which is what an operator needs to act on any of the three drain options
// the ticket lists. That is what this method is, and it is all it is.
//
// ⚠ WHAT THIS DELIBERATELY DOES NOT DO. It sends nothing, extends no clock, waives no notice and
// makes NO WRITE of any kind. Choosing between "send late against a re-armed retention_expires_at",
// "record a waiver and allow deletion" and "notify out of band" is a product decision that is
// parked until after go-live with beta; shipping any of them inside this read would be taking that
// decision by implication. A pure read is the part that was blocked on nothing.
//
// ⚠ THE ONE THING THIS READ CAN SEE THAT THE COUNT PROVABLY CANNOT: whether the account has an
// address on file. CountRetentionDeletionsWithheld's doc comment sets out, correctly, why no SQL
// predicate can single that out — users.email is ciphertext, EncryptString("") is still "ENC$"+ct,
// and email_blind_idx (the one column that could) carries migration 000061's FULL UNIQUE index,
// capping the population at one row database-wide and making it worthless as a metric label. None
// of that is weakened here, because this read does not use a predicate: it goes through queryUsers,
// which DECRYPTS email for every row, and reports the state post-decryption at exactly the point
// canSendRetentionNotice (queue/jobs/retention_cleanup_check.go) observes it — `user.Email == ""`.
// Per-row, not aggregated, so the cardinality argument does not reach it either. Pinned by
// TestRetentionWithheldEnumeration_Integration_ReportsAnAddresslessAccountTheCountCannotSee, whose
// fixture is built through CreateUserTx so the column really does hold EncryptString("") ciphertext
// — the shape a fixture writing a bare empty string into the column could not produce, which is
// how a dead predicate once shipped green on this branch.
//
// PREDICATE SHARED WITH THE COUNT, not restated: retentionWithheldPopulationPredicate. The number
// on a dashboard and the list beneath it must be the same population or an operator can trust
// neither.
//
// KEYSET PAGINATION on (retention_expires_at, id) ASC, mirroring GetUsersWithExpiredRetentionPage
// and reusing its RetentionSweepCursor. The tie-break on id is load-bearing rather than decorative:
// this population is a BACKLOG, so its rows are very likely to share one expiry, and a cursor
// keyed on the timestamp alone would silently drop every row after the first of each tied group.
// TestRetentionWithheldEnumeration_Integration_KeysetPaginationCoversTiedExpiries seeds five rows
// on one expiry for exactly that reason.
func (r *profileRepository) GetRetentionDeletionsWithheldPage(ctx context.Context, after *RetentionSweepCursor, limit int) ([]RetentionWithheldAccount, error) {
	if limit <= 0 {
		limit = retentionSweepDefaultPageSize
	}

	var query string
	var args []interface{}
	if after == nil {
		query = userSelectQuery + retentionWithheldPopulationPredicate + `
			ORDER BY retention_expires_at ASC, id ASC
			LIMIT $1
		`
		args = []interface{}{limit}
	} else {
		query = userSelectQuery + retentionWithheldPopulationPredicate + `
			  AND (retention_expires_at, id) > ($1, $2)
			ORDER BY retention_expires_at ASC, id ASC
			LIMIT $3
		`
		args = []interface{}{after.ExpiresAt, after.UserID, limit}
	}

	users, err := r.queryUsers(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("GetRetentionDeletionsWithheldPage: %w", err)
	}

	accounts := make([]RetentionWithheldAccount, 0, len(users))
	for _, u := range users {
		// expiredRetentionClockPredicate requires retention_expires_at IS NOT NULL, so this is
		// never nil for a row this query returned. Guarded anyway rather than dereferenced blind:
		// the predicate is spliced from another declaration, and a nil-pointer panic in a
		// monitoring read is a worse failure than a skipped row.
		if u.RetentionExpiresAt == nil {
			continue
		}
		accounts = append(accounts, RetentionWithheldAccount{
			UserID:             u.ID,
			SubscriptionStatus: u.SubscriptionStatus,
			RetentionExpiresAt: *u.RetentionExpiresAt,
			Notice30dSentAt:    u.RetentionNotice30dSentAt,
			Notice7dSentAt:     u.RetentionNotice7dSentAt,
			// The SAME test canSendRetentionNotice applies, on the SAME decrypted value. Written
			// as the identical expression rather than as a helper call because the two live in
			// different packages and the jobs-side one is a method on the worker; if that check
			// ever grows (a syntactic validity test, say), this must grow with it or this column
			// starts reporting a reason the sender does not act on.
			HasEmailOnFile: u.Email != "",
		})
	}
	return accounts, nil
}

// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = $2, updated_at = NOW()
		WHERE id = $1 AND past_due_started_at IS NULL`,
		userID, t,
	)
	if err != nil {
		return err
	}
	// Either not found or already set — both are acceptable (idempotent).
	return nil
}

// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user.
// Called on resubscription so the grace period resets cleanly.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = NULL, updated_at = NOW()
		WHERE id = $1`,
		userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearPastDueStartedAtIfMatches is ClearPastDueStartedAt's occurrence-scoped sibling (Issue
// #1413 round-4 finding H3). ClearPastDueStartedAt above is UNCONDITIONAL (WHERE id = $1) —
// correct for subscription_webhook.go's recovery-path callers, where Paddle telling us the
// subscription is no longer past_due IS the authorization. It is WRONG for
// processGraceExpiry's auto-cancel step (past_due_service.go), which captured a SPECIFIC dunning
// cycle's timestamp earlier in its own run: sequence — the CAS (CancelIfPastDueOccurrenceWithRetention)
// succeeds and the flow reaches this clear step; meanwhile the user recovers, then re-enters
// past_due, and the webhook stamps a NEW past_due_started_at (SetPastDueStartedAt) for the NEW
// cycle; the ORIGINAL flow, still holding its stale snapshot, then reaches an unconditional clear
// and would erase the NEW cycle's timestamp too. Because GetUsersInPastDue requires
// past_due_started_at IS NOT NULL, that user would never be selected again — stuck past-due
// forever with no dunning.
//
// The fix is a compare-and-swap: only clear when past_due_started_at STILL equals expected, the
// value the caller actually acted on.
//
// Distinguishes two zero-rows-affected causes that a single bool cannot tell apart:
//   - The user genuinely does not exist: (false, ErrUserNotFound), matching the plain
//     ClearPastDueStartedAt's existing contract — this is an anomaly worth surfacing as an error.
//   - The user exists but past_due_started_at has already moved on to a newer cycle: (false, nil)
//     — a legitimate, expected skip, NOT an error. The newer cycle's timestamp is exactly what
//     must survive untouched so a future dunning run can still select and process it; treating
//     this as an error would make a caller retry/alert on what is actually the fix working
//     correctly.
//
// Implemented as one round trip: the `existing` CTE independently proves whether the row exists
// at all, while the `updated` CTE performs the actual CAS write; a data-modifying CTE referenced
// exactly once always executes exactly once, so there is no double-write hazard here.
//
// NOTE: this is a NEW, separate method rather than a signature change to ClearPastDueStartedAt —
// subscription_webhook.go's three recovery-path callers need the unconditional version's
// semantics (no "expected occurrence" to compare against there), and per the #1413 P6 task's
// scope fence, that file is owned by a concurrent task. Once that surface settles, folding these
// back into one CAS-shaped method (with the unconditional case expressed as "no expectation
// supplied") would be a reasonable follow-up cleanup.
func (r *profileRepository) ClearPastDueStartedAtIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	var userExists, cleared bool
	err := r.db.QueryRow(ctx, `
		WITH existing AS (
			SELECT 1 FROM users WHERE id = $1
		), updated AS (
			UPDATE users SET past_due_started_at = NULL, updated_at = NOW()
			WHERE id = $1 AND past_due_started_at = $2
			RETURNING 1
		)
		SELECT EXISTS(SELECT 1 FROM existing), EXISTS(SELECT 1 FROM updated)
	`, userID, expected).Scan(&userExists, &cleared)
	if err != nil {
		return false, fmt.Errorf("ClearPastDueStartedAtIfMatches: %w", err)
	}
	if !userExists {
		return false, ErrUserNotFound
	}
	return cleared, nil
}

// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
// Called by the daily dunning cron job to find candidates for grace-expiry or Plaid-disconnect processing.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'past_due'
		  AND past_due_started_at IS NOT NULL
		  AND is_deleted = FALSE
		ORDER BY past_due_started_at ASC
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
			&user.PlaidCleanupPendingAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		return []domain.User{}, nil
	}
	return users, nil
}

// CancelIfPastDueOccurrenceWithRetention atomically flips subscription_status to 'cancelled' AND
// starts the retention clock (retention_started_at, retention_expires_at) in the same UPDATE,
// occurrence-scoped (Issue #1413 round-4 finding H1). Its predecessor, CancelIfPastDueWithRetention
// (deleted by Issue #1442 for having zero production callers), CASed only on still-'past_due' and
// no currently-active manual override (noActiveManualOverridePredicate) — necessary but not
// sufficient once a worker can be racing a NEWER past_due cycle for the SAME user: a batch snapshot
// captures cycle A's past_due_started_at, the user recovers, then re-enters past_due as cycle B (a
// genuinely NEW past_due_started_at) — cycle B is ALSO 'past_due' with no override, so that
// predicate would happily flip it even though the worker only ever verified cycle A's grace
// period, not cycle B's. Adding `AND past_due_started_at = expectedPastDueStartedAt` closes this:
// the CAS now only succeeds for the EXACT cycle the caller claimed and re-verified.
//
// Returns (true, nil) when the flip happened, (false, nil) whenever the predicate misses for ANY
// reason — recovered, comped, or a different cycle now occupies the row. All three are the same
// "someone else's business, not an error" outcome for this caller: past_due_service.go's
// processGraceExpiry Releases (never Completes) its claim and lets a future run handle whatever
// cycle is actually current. Issue #898: CAS guard against recovery race condition. Issue #1413
// High finding H6 (Codex review round 3): predicate extended with noActiveManualOverridePredicate —
// a prior read-only HasActiveManualOverride() check in the caller cannot, by itself, protect this
// write, since the two are not atomic.
//
// retention_expires_at is computed entirely in Postgres, clock_timestamp() + make_interval(months
// => $3) — Task NA2 (Issue #1413 follow-up on PR #1443): this method used to accept a Go-computed
// retentionExpires time.Time built via now.AddDate(0, retentionMonths, 0) in past_due_service.go,
// which disagrees with Postgres's make_interval whenever the anchor's day-of-month does not exist
// N months later (Go normalizes the overflow FORWARD; Postgres clamps to the target month's last
// day instead) — the wrong direction for a GDPR deletion deadline. Matches SetRetentionDates and
// AdminUserRepository.RemoveUserTierOverride's SQL exactly, so every writer of this column shares
// one semantics — see SetRetentionDates' interface doc comment for why the pre-144b0e3a row
// population was deliberately not backfilled. (Round 10 narrowed this claim to writers with a
// production caller while a dead sibling, CancelIfPastDueWithRetention, still existed on the old
// Go-computed semantics; Issue #1442 deleted that sibling, so the unqualified claim holds again.)
//
// Round-10 note on the clock pairing, so it is not mistaken for an oversight: retention_started_at is
// supplied by the caller while retention_expires_at is computed here from clock_timestamp() at write
// time, so in a BATCH caller (past_due_check.go captures one time.Now() before its per-user loop, and
// each iteration makes Plaid/Paddle calls before reaching this write) a later row's window is N months
// plus the batch's elapsed time. That is the same class of skew removeUserTierOverrideUpdate's doc
// comment already accepts for lock waits — "affects only what the started-at audit trail SAYS, never
// what any query DOES with it" — but the batch magnitude (potentially minutes, not milliseconds) is
// larger than that precedent analysed. Filed as a follow-up rather than fixed here: the obvious fix,
// hoisting one clock_timestamp() into a `WITH t AS (SELECT clock_timestamp())` CTE, is NOT safe in
// this method, because a trivial CTE like that can be evaluated BEFORE the sibling candidate CTE's
// FOR NO KEY UPDATE lock wait completes — reintroducing exactly the pre-lock-wait-row-version hazard
// ROUND 23 restructured this statement to fix. Trading microseconds of column skew for seconds of
// lock-wait skew is the wrong direction.
//
// retention_started_at stays a plain Go-computed value ($2, unchanged) — per
// removeUserTierOverrideUpdate's own doc comment (admin_user_repository.go), retention_started_at
// is a paired audit field nothing computes an age from or compares to NOW()/clock_timestamp()
// anywhere; only retention_expires_at is ever read back as a deadline, so basing it on a
// DIFFERENT, fresher clock than retention_started_at is the established, already-reviewed pattern
// here, not a new inconsistency.
func (r *profileRepository) CancelIfPastDueOccurrenceWithRetention(ctx context.Context, userID string, expectedPastDueStartedAt, retentionStarted time.Time, retentionMonths int) (bool, error) {
	// Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with this SAME CAS write —
	// see GetUsersNeedingPlaidDisconnectRepair's doc comment. COALESCE guards against a retried
	// call re-pushing an already-running marker forward.
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this marker COALESCE
	// too, for the same reason UpdateSubscriptionFromPaddle's own doc comment (this file) explains
	// in full: a bare substitution would not actually fix anything here, and the real fix was
	// scoped into the same follow-up at the time.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
	// This method's WHERE clause is its OWN correctness mechanism: `past_due_started_at = $4` is
	// exactly the extra guard this method exists to add (see this method's own H1 doc comment above)
	// against a worker racing a NEWER past_due cycle for the same user, so it moves into the CTE's
	// SELECT alongside the other two conditions, UNCHANGED and in full —
	// `subscription_status = 'past_due'`, `past_due_started_at = $4`, and
	// noActiveManualOverridePredicate, nothing added, nothing removed. Same `FOR NO KEY UPDATE` lock
	// mode and same verified-fresh target-table COALESCE read as UpdateSubscriptionFromPaddle's own
	// ROUND 23 fix — see that method's doc comment for the full mechanism.
	//
	// RowsAffected() semantics are unchanged: past_due_service.go's processGraceExpiry reads the
	// returned bool to decide between Completing (flipped) and Releasing (not flipped) its claim —
	// the join with the CTE still produces zero rows in EXACTLY the circumstances (id not found,
	// status no longer 'past_due', a DIFFERENT past_due cycle now occupies the row, or a
	// currently-active override) the bare UPDATE's WHERE clause used to.
	ct, err := r.db.Exec(ctx, `
		WITH candidate AS (
			SELECT id FROM users
			WHERE id = $1 AND subscription_status = 'past_due' AND past_due_started_at = $4
		`+noActiveManualOverridePredicate+`
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET subscription_status = 'cancelled',
		    retention_started_at = $2,
		    retention_expires_at = clock_timestamp() + make_interval(months => $3::int),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp()),
		    updated_at = NOW()
		FROM candidate
		WHERE users.id = candidate.id
	`,
		userID, retentionStarted, retentionMonths, expectedPastDueStartedAt)
	if err != nil {
		return false, fmt.Errorf("CancelIfPastDueOccurrenceWithRetention: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the user is
// still 'trial' and carries no currently-active manual tier override. Returns (true, nil) if the
// flip happened, (false, nil) if an admin granted/extended a comp between the caller's earlier
// HasActiveManualOverride() read and this call, or the user's status has otherwise already moved
// on. Issue #1413 High finding H5, Codex review round 3: follows CancelIfPastDueOccurrenceWithRetention's
// shape — the predicate that authorises the transition lives in the UPDATE's WHERE clause,
// evaluated against committed state at the moment of the write, not in an earlier, non-atomic
// read-then-check.
func (r *profileRepository) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	// Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with this SAME CAS write —
	// see GetUsersNeedingPlaidDisconnectRepair's doc comment. COALESCE guards against a retried
	// call re-pushing an already-running marker forward (defensive; the WHERE clause already
	// makes this a one-shot transition per trial).
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this marker COALESCE
	// too, for the same reason as UpdateSubscriptionFromPaddle's own doc comment (this file)
	// explains in full: a bare substitution would not actually fix anything here, and the real fix
	// was scoped into the same follow-up at the time.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
	// This method's WHERE clause (`subscription_status = 'trial'` plus noActiveManualOverridePredicate)
	// is its own CAS/correctness mechanism — see this method's own H5 doc comment above: it authorises
	// the transition against committed state at write time, closing the race a caller's earlier,
	// non-atomic HasActiveManualOverride() read cannot. Both conditions move into the locked CTE's
	// SELECT unchanged, nothing added, nothing removed. Same `FOR NO KEY UPDATE` lock mode (never
	// `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's Finding 1 correction for the cross-user
	// deadlock that mode would risk) and same verified-fresh target-table COALESCE read as
	// CancelIfPastDueOccurrenceWithRetention's own ROUND 23 fix — see that method's doc comment for
	// the full mechanism.
	//
	// RowsAffected() semantics are unchanged: this method's own callers use the returned bool exactly
	// as CancelIfPastDueOccurrenceWithRetention's callers do — the join with the CTE still produces zero rows
	// in EXACTLY the circumstances (id not found, status no longer 'trial', or a currently-active
	// override — i.e. an admin granted/extended a comp between the caller's HasActiveManualOverride()
	// read and this call) the bare UPDATE's WHERE clause used to.
	ct, err := r.db.Exec(ctx, `
		WITH candidate AS (
			SELECT id FROM users
			WHERE id = $1 AND subscription_status = 'trial'
		`+noActiveManualOverridePredicate+`
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
		FROM candidate
		WHERE users.id = candidate.id
	`,
		userID)
	if err != nil {
		return false, fmt.Errorf("ExpireTrialIfNoOverride: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}
