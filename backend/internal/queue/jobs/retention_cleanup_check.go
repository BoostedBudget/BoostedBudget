// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check — 6-month auto-deletion of inactive/cancelled accounts.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// retentionNoticeClaimLease is the lease duration requested for both ActionRetentionNotice30d and
// ActionRetentionNotice7d work claims (Issue #1413 Medium finding M9, Codex review round 3).
// Shorter than the 15-minute lease used by claims guarding a Plaid/Paddle call (ExecuteDeletion,
// PastDueService, SubscriptionWebhookWorker): sending a notice email has no slow external cleanup
// step behind it, so 5 minutes is generous while still keeping a crashed worker's claim quickly
// reclaimable.
const retentionNoticeClaimLease = 5 * time.Minute

// plaidDisconnectRepairGracePeriod is the round-7 grace window (Issue #1413 Codex review): a
// read-only user — plaid_cleanup_pending_at (Issue #1413 round-11 correction: NOT
// retention_started_at, which this comment incorrectly named through round-9; see
// domain.User.PlaidCleanupPendingAt's doc comment for the invariant and
// GetUsersNeedingPlaidDisconnectRepair's doc comment for the round-10 rationale), set at the same
// moment they became Plaid-ineligible — is not selected for Plaid disconnect by
// repairPlaidDisconnects until this much time has passed. This is what makes "resubscribe within
// the window" a non-event rather than a narrowed race: a user who becomes active again before the
// window elapses has their marker cleared (per the invariant) and simply falls out of
// GetUsersNeedingPlaidDisconnectRepair's predicate, never touched.
//
// 72 hours (3 days), chosen as a deliberate middle ground:
//   - Longer is safer for a genuine resubscriber: more time to notice their trial/subscription
//     lapsed and complete checkout (whitelisted at internal/huma/middleware/read_only.go) before
//     their banks are touched at all.
//   - Longer also means a non-paying user keeps a live, billable Plaid connection for longer with
//     no corresponding subscription — the cost (and data-minimization concern) this whole repair
//     pass exists to bound.
//
// RetentionCleanupCheckWorker runs roughly daily (RetentionCleanupCheckUniqueOpts's 23-hour
// dedup period), so any grace value in the 24-72h range resolves to about the same number of
// worker ticks in practice; 72h leaves comfortable headroom for a slow notice-then-pay flow
// without meaningfully changing how many daily runs elapse before disconnect. This mirrors the
// same grace-then-cleanup shape past_due_service.go already uses (PastDueConfig.PlaidDisconnectDays,
// 14 days) and the retention/deletion notice windows (30d/7d) — but is deliberately much shorter
// than either, since a lapsed trial/subscription is a harder terminal state than past_due dunning
// (no pending payment retry) and the primary goal here is closing a payment race, not running a
// full second dunning cycle.
const plaidDisconnectRepairGracePeriod = 72 * time.Hour

// plaidCleanupMarkerClearMaxAttempts bounds how many times repairPlaidDisconnects retries
// ClearPlaidCleanupPendingIfMatches after a successful Plaid disconnect, before giving up and
// counting it as a genuine per-user failure — Issue #1413 Finding 4 (Codex review, PR #1443): a
// single unretried failure here used to leave the marker set FOREVER, because the disconnect that
// just succeeded removed the very live linked item GetUsersNeedingPlaidDisconnectRepair's own
// EXISTS clause requires to ever reselect this user again. Retrying a handful of times, in the same
// process, immediately after the failure, closes the common case (a transient DB blip) for a call
// that is a simple conditional UPDATE, not a remote/rate-limited API call.
//
// Issue #1413 Finding 3 (Codex review, PR #1443 follow-up): this value, and
// plaidCleanupMarkerClearRetryDelay just below, are no longer independent literals — both now alias
// repository.PlaidCleanupMarkerClearMaxAttempts / repository.PlaidCleanupMarkerClearRetryDelay (see
// that pair's own doc comment, profile_repository.go, for the full rationale). Before this fix,
// services/past_due_service.go carried a second, genuinely independent copy of these same two
// numbers, with its own doc comment explicitly acknowledging the duplication. Aliasing here — rather
// than qualifying every reference in this file and subscription_webhook.go as
// repository.PlaidCleanupMarkerClearMaxAttempts — keeps every existing unqualified reference in both
// files (and their tests) compiling unchanged, while ensuring there is exactly one place either
// number is actually written down as a literal.
const plaidCleanupMarkerClearMaxAttempts = repository.PlaidCleanupMarkerClearMaxAttempts

// plaidCleanupMarkerClearRetryDelay is the BASE delay clearPlaidCleanupMarkerWithRetry (this file)
// backs off from between attempts — Issue #1413 round 16 (a later Codex review round on PR #1443):
// three attempts at a FIXED 50ms (the original Finding 4 fix) assumes the underlying failure clears
// within ~100ms total, which is far shorter than a real database failover or a connection-pool
// exhaustion event can take. Backing off (50ms, 100ms, ... — see plaidCleanupMarkerClearBackoff)
// gives a slower-clearing transient failure a better chance to resolve before this loop gives up,
// at the cost of a slightly longer worst-case per-user delay — an acceptable trade for a step that
// already isolates one user's failure from every other (see repairPlaidDisconnects's per-user error
// isolation, and the errorCount doc comment below for why a slower loop here does not block
// anything time-sensitive).
//
// NOTE: this constant name and value are UNCHANGED — subscription_webhook.go (a sibling task's
// owned file, not touched by this change) has its own, separate marker-clear retry loop
// (processSubscriptionCanceled's Finding-4 fix) that also references this exact constant with a
// plain, non-backed-off time.Sleep. Renaming or repurposing it here would have silently broken that
// file's build; only this file's OWN retry loop (clearPlaidCleanupMarkerWithRetry) is changed to
// back off and to respect context cancellation. See the Finding 3 note on
// plaidCleanupMarkerClearMaxAttempts above: the underlying LITERAL now lives in the repository
// package; this name is a same-package alias to it, unchanged for every caller in this package.
const plaidCleanupMarkerClearRetryDelay = repository.PlaidCleanupMarkerClearRetryDelay

// plaidCleanupMarkerClearBackoff returns the delay before retry attempt (attempt+1), given that
// `attempt` (1-based) just failed — exponential backoff from plaidCleanupMarkerClearRetryDelay
// (50ms, 100ms, ... — for plaidCleanupMarkerClearMaxAttempts=3 this is only ever called with
// attempt=1, giving a single 50ms pause before the final attempt; written generally so raising
// plaidCleanupMarkerClearMaxAttempts does not silently reintroduce a flat-delay retry). Used only by
// this file's clearPlaidCleanupMarkerWithRetry — subscription_webhook.go's own retry loop (a
// separate, sibling-owned copy of the same Finding-4 fix) still uses a flat
// plaidCleanupMarkerClearRetryDelay sleep and is unchanged by this function's existence.
func plaidCleanupMarkerClearBackoff(attempt int) time.Duration {
	return plaidCleanupMarkerClearRetryDelay * time.Duration(1<<uint(attempt-1)) //nolint:gosec // attempt is always small and non-negative (loop-bounded)
}

// expiredRetentionPageSize bounds each GetUsersWithExpiredRetentionPage query (Issue #1478).
// deleteExpiredAccounts loops pages of this size within a single run, so this bounds only how many
// rows one query call materialises — NOT how many users one run hard-deletes. That total is bounded
// separately by maxUsersPerRetentionSweep below. 500 matches scheduledDeletionCheckPageSize; there
// is no reason for the two sweeps' page sizes to differ, since a page's cost is a row fetch either
// way.
const expiredRetentionPageSize = 500

// maxUsersPerRetentionSweep bounds the TOTAL number of users deleteExpiredAccounts hard-deletes
// across every page fetched within ONE Work() run.
//
// 1,000 — deliberately 50x SMALLER than maxUsersPerDeletionSweep (50,000), which bounds the sibling
// scheduled-deletion sweep. The two numbers are not comparable, because the per-row action is not
// comparable: ScheduledDeletionCheckWorker's loop body is a single River enqueue (cheap, and the
// actual deletion happens later in AccountDeletionWorker, still gated by ExecuteDeletion's claim),
// whereas this loop body IS the deletion — a synchronous ExecuteDeletion per row, which acquires a
// work claim, disconnects Plaid, and runs DeleteUserCascade's `DELETE FROM users` against 44
// cascading foreign keys spanning 42 tables directly, 48 transitively (measured by walking
// pg_constraint on the live schema, #1569 T8 — the "~37" this comment used to quote was stale).
// Nothing between this loop and irreversible data loss.
//
// Derivation of 1,000:
//   - Upper side: 1,000 is a ceiling **per sweep invocation**, and deliberately not described as a
//     per-day one. River's ByPeriod (RetentionCleanupCheckUniqueOpts, 23h) rounds insert times into
//     FIXED buckets rather than enforcing a rolling window — this file already documents that
//     semantics for the Completed-state fix (see RetentionCleanupCheckUniqueOpts's doc comment, which
//     talks about "the current ByPeriod bucket"). Two sweeps landing either side of a bucket boundary
//     can therefore execute close together, so the real short-window worst case is ~2,000 hard
//     deletions in well under 24 hours, not 1,000 per day. An earlier version of this comment claimed
//     a per-day ceiling, which the same file's own bucket semantics contradict.
//     Even at that doubled worst case the bound holds for its purpose: for a paid-only product with
//     no free tier whose candidates are accounts inactive/cancelled for a full retention period
//     (6 months by default, cfg.RetentionPeriodMonths), 1,000 expiring per sweep is already far above
//     any plausible steady-state volume, so the cap does not throttle correct operation.
//   - Lower side: it has to be small enough that a WRONG candidate predicate — the failure mode
//     that actually produced this branch's only CRITICAL finding, a cancelled user being
//     hard-deleted — destroys a bounded number of accounts on the RunOnStart tick after a deploy
//     rather than the whole table, leaving a day and a WARN log for a human to intervene.
//   - At expiredRetentionPageSize=500 that is 2 page round-trips, so the cap is reached by page
//     boundary arithmetic, not by an awkward mid-page truncation in the common case.
//
// Hitting the cap does NOT reschedule or retry anything: it stops THIS run's deletion step early and
// logs a WARN naming how many users were processed. The periodic registration re-runs the job on its
// own cadence, and since Issue #1477 that run RESUMES from the timestamp this run stopped at
// (sweep_cursors — repository/sweep_cursor_repository.go) rather than restarting from a nil cursor.
// That is what removes the pre-#1477 failure this comment used to describe, in which a set of stuck
// oldest rows larger than this cap consumed every later sweep's whole budget and nothing past the
// cap was EVER reached. The replacement guarantee is bounded, not absolute — it is stated once, with
// its bound, in repository/sweep_cursor_repository.go's header. Do not restate it as "no starvation".
const maxUsersPerRetentionSweep = 1000

// RetentionCleanupCheckArgs contains the arguments for the retention cleanup check job.
// This job runs daily to:
//  1. Send 30-day pre-deletion warning emails
//  2. Send 7-day pre-deletion warning emails
//  3. Auto-delete users whose retention period has expired
type RetentionCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (RetentionCleanupCheckArgs) Kind() string { return "retention_cleanup_check" }

// RetentionProfileRepository defines the repository methods needed for retention cleanup.
type RetentionProfileRepository interface {
	// GetUsersNeedingRetention30dNotice returns users whose retention expires within 30 days and 30d notice not sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersNeedingRetention7dNotice returns users whose retention expires within 7 days and 7d notice not sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersWithExpiredRetentionPage returns up to `limit` users whose retention_expires_at has
	// passed and who are not deleted/pending, ordered deterministically for keyset pagination
	// (Issue #1478). after == nil starts from the beginning; pass the cursor derived from the last
	// row of the previous page to continue.
	//
	// This replaces the unbounded GetUsersWithExpiredRetention(ctx) on this interface. It is
	// declared HERE and on the concrete repository only — deliberately NOT on the wide
	// repository.ProfileRepository interface, which several test files' mocks implement. See
	// repository.profileRepository.GetUsersWithExpiredRetentionPage's doc comment for why a bare
	// LIMIT is not sufficient and for the precise, cap-bounded statement of what the keyset cursor
	// does and does not guarantee.
	GetUsersWithExpiredRetentionPage(ctx context.Context, after *repository.RetentionSweepCursor, limit int) ([]*domain.User, error)

	// GetUsersNeedingRetentionClockRepair returns users who are read-only-status
	// (inactive/cancelled) but have NO retention clock started (retention_expires_at IS NULL) —
	// Issue #1413 round-3 finding H5 residual, belt-and-braces repair path. See
	// RepairRetentionClock's doc comment for the write side.
	GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error)

	// RepairRetentionClock sets the retention clock for a single user, but ONLY if
	// retention_expires_at is still NULL and the user is still read-only-status at the moment of
	// the write. This is a plain conditional UPDATE, not a work_claims claim — the write is purely
	// additive and idempotent (once set, the WHERE clause itself prevents a concurrent repair from
	// clobbering it), unlike the destructive external side effects work_claims exists to guard.
	//
	// retentionMonths replaces a Go-computed expiresAt time.Time (Task NA2, Issue #1413 follow-up
	// on PR #1443): retention_expires_at is now computed entirely in Postgres via clock_timestamp()
	// + make_interval(months => retentionMonths), matching every other writer of this column —
	// this repair path exists specifically to fix rows OTHER writers left NULL, so it must use the
	// SAME arithmetic semantics those writers use, or repairing a row would land it on a DIFFERENT
	// deadline than a fresh write would have produced.
	RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error)

	// StampMissingPlaidCleanupMarkers is Issue #1413 round 16's self-healing backstop for
	// plaid_cleanup_pending_at — see its full doc comment on profileRepository
	// (profile_repository.go) for the complete rationale (five prior review rounds each closed one
	// way this marker could end up NULL while a user still has a live linked item, and opened
	// another, because every attempt tried to make some WRITER maintain it correctly; writers race
	// with the linked_items lifecycle they are trying to summarise, and cannot win that race by
	// writing more carefully). Called by repairPlaidDisconnects (below) at the very start of every
	// pass, before the candidate query: a single atomic UPDATE that stamps NOW() for any user who is
	// ineligible-status, has a live linked item, and has a NULL marker. Returns the number of rows
	// healed (for logging only — repairPlaidDisconnects does not otherwise branch on it) and a
	// non-nil error only on a genuine database failure, which repairPlaidDisconnects treats as
	// non-fatal (logged, not retried here) — the backstop's own idempotency means the NEXT pass
	// retries it for free.
	//
	// Deliberately NOT part of the broad canonical ProfileRepository interface
	// (profile_repository.go) — see NewProfileRepository's own doc comment and
	// StampMissingPlaidCleanupMarkers's for why this, like several other occurrence/repair-scoped
	// methods, belongs only to this narrower consumer interface.
	StampMissingPlaidCleanupMarkers(ctx context.Context) (int, error)

	// ClearCompletedPlaidCleanupMarkers is StampMissingPlaidCleanupMarkers's symmetric counterpart —
	// Issue #1413 Finding 1 (Codex review, PR #1443 follow-up, headline finding). See its full doc
	// comment on profileRepository (profile_repository.go) for the complete rationale, including the
	// verified five-step scenario in which a marker left set after its episode's cleanup completed
	// disconnects a freshly-relinked bank with ZERO grace period. Called by repairPlaidDisconnects
	// (below) immediately after StampMissingPlaidCleanupMarkers, at the very start of every pass: a
	// single atomic UPDATE that nulls the marker for any user who is ineligible-status, has the marker
	// set, has AT LEAST ONE linked_items row, and has NO live linked_items row. That last two-part
	// condition is mandatory: a user with ZERO linked_items rows at all (the #1453 allowance) keeps
	// their marker untouched. Returns the number of rows cleared (for logging only) and a non-nil
	// error only on a genuine database failure, which repairPlaidDisconnects treats as non-fatal
	// (logged, not retried here) — like its Stamp sibling, this backstop's own idempotency means the
	// NEXT pass retries it for free.
	//
	// Deliberately NOT part of the broad canonical ProfileRepository interface, for the same reason as
	// StampMissingPlaidCleanupMarkers.
	ClearCompletedPlaidCleanupMarkers(ctx context.Context) (int, error)

	// ClearMarkersOnEligibleStatus is Issue #1455's THIRD self-healing backstop — closing the
	// plaid_cleanup_pending_at invariant's OTHER branch. Stamp/Clear above both self-heal "status
	// ineligible ⇒ marker reflects outstanding cleanup", but BOTH are gated on
	// plaidDisconnectRepairEligibleStatusPredicate, so neither ever inspects a row whose status is
	// NOT inactive/cancelled — leaving "status NOT ineligible ⇒ marker must be NULL" with no runtime
	// backstop at all. See its full doc comment on profileRepository (profile_repository.go) for the
	// complete rationale, including why this is a flat UPDATE (not Stamp's `FOR NO KEY UPDATE` CTE —
	// this writes only a constant, exactly like Clear) and the judgement call on NOT excluding a
	// currently-comped user (mirroring Clear's asymmetry with Stamp, not Stamp's own exclusion).
	// Called by repairPlaidDisconnects (below) immediately after ClearCompletedPlaidCleanupMarkers,
	// at the very start of every pass. Returns the number of rows cleared (for logging only) and a
	// non-nil error only on a genuine database failure, treated as non-fatal like its two siblings.
	//
	// Deliberately NOT part of the broad canonical ProfileRepository interface, for the same reason as
	// its two siblings.
	ClearMarkersOnEligibleStatus(ctx context.Context) (int, error)

	// GetUsersNeedingPlaidDisconnectRepair returns users who are read-only-status
	// (inactive/cancelled), whose plaid_cleanup_pending_at marker (Issue #1413 round-10) is older
	// than gracePeriod, and who still have at least one LIVE linked item — Issue #1413 round-7
	// repair path. See ProfileRepository.GetUsersNeedingPlaidDisconnectRepair's doc comment
	// (profile_repository.go) for the full rationale: TrialService.ExpireTrial never calls Plaid at
	// all (no ordering of a status flip and an irreversible Plaid call can be safe, since
	// /subscription/checkout is whitelisted for read-only users) — this repair pass, run only after
	// the grace window elapses, is the ONLY place a read-only user's Plaid items are disconnected.
	// repairPlaidDisconnects (below) is the consumer, mirroring repairRetentionClocks's shape for
	// the retention clock. round-10 replaced the grace anchor (previously retention_started_at,
	// which rounds 8/9 showed cannot be made sound) with the explicit plaid_cleanup_pending_at
	// marker — see domain.User.PlaidCleanupPendingAt's doc comment.
	GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error)

	// ClearPlaidCleanupPendingIfMatches clears a user's plaid_cleanup_pending_at marker ONLY IF it
	// still equals expected — Issue #1413 round-11 Fix 3 (replaces the unconditional
	// ClearPlaidCleanupPending this interface used through round-10). Called by
	// repairPlaidDisconnects once DisconnectAllUserItemsIfEligible actually succeeds, passing the
	// SAME marker value the candidate query selected on: an unconditional clear at this point could
	// erase a DIFFERENT, newer ineligibility episode's marker (e.g. a comp granted then removed
	// entirely within the window between candidate selection and this call) — discarding that
	// episode's grace window entirely. A (false, nil) result is a normal, expected skip (the
	// marker already moved on to a new episode — leave it for that episode's own future run), not
	// an error; it is logged, not escalated, mirroring ClearPastDueStartedAtIfMatches's contract.
	ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error)

	// GetUserByID retrieves a user by their internal UUID — used by repairPlaidDisconnects to
	// re-verify a candidate is STILL read-only-status and uncomped. Unlike RepairRetentionClock,
	// the repair action here is an external Plaid call with no atomic DB guard of its own, so this
	// re-read is what stands in for that guard — without it, a stale candidate who resubscribed
	// since the query ran would have their genuinely-live Plaid items disconnected, reproducing the
	// exact harm this repair pass exists to fix via a different code path. Issue #1413 round-7: this
	// re-read is now invoked PER ITEM (via DisconnectAllUserItemsIfEligible's recheck callback), not
	// just once before the whole per-user disconnect call — a disconnect fans out into one Plaid
	// call per linked item, so a single check before the loop began would leave every item after the
	// first exposed to a resubscribe happening mid-loop.
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)

	// IsRetentionNotice30dEligibleForOccurrence re-verifies, at the moment the caller already
	// holds an exclusive work_claims claim on repository.ActionRetentionNotice30d, that a user is
	// still eligible for the 30-day notice: still in the notice window, still unsent, carrying no
	// manual_tier_override at all (live or lapsed) — the same rule GetUsersNeedingRetention30dNotice's
	// own query enforces — AND that retention_expires_at still equals expectedRetentionExpiresAt,
	// the SAME value the caller claimed (Issue #1413 round-4 finding M1). Without that last check,
	// a stale candidate could claim E1's occurrence while the row has since been re-armed to E2
	// (resubscribe, then cancel again) — with a short retention configuration, E2 can ALSO be
	// genuinely in-window right now, so a worker holding the stale E1 claim would report eligible
	// and send a DUPLICATE notice alongside whatever worker is legitimately processing E2. Returns
	// (true, nil) when safe to send the notice email now, (false, nil) when any condition
	// (including the occurrence) changed since the candidate query ran — a legitimate skip, never
	// an error. Performs NO write. Issue #1413 Medium finding M9 (Codex review round 3): replaces
	// the old ClaimRetentionNotice30dSent, which stamped the *_sent_at marker BEFORE the send was
	// even attempted, so a failed send left the marker set and the user was silently never warned
	// again.
	IsRetentionNotice30dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error)
	// IsRetentionNotice7dEligibleForOccurrence is IsRetentionNotice30dEligibleForOccurrence's
	// 7-day sibling.
	IsRetentionNotice7dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error)

	// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent. Callers MUST only
	// invoke this AFTER the notice email has been sent successfully (Issue #1413 M9) — never
	// before, and never on a skipped/failed send.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error
	// SetRetentionNotice7dSent is SetRetentionNotice30dSent's 7-day sibling.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error

	// GetUsersWithMalformedTierOverride is Issue #1453's monitoring query: returns every user whose
	// manual_tier_override is non-empty but does not name a currently-configured tier. Migration
	// 000127 backfilled the known population to NULL as a one-time cleanup; this monitoring pass
	// (monitorMalformedTierOverrides below) catches a FUTURE writer reintroducing the class — one
	// that bypasses admin_user_service.go's write-time validation entirely (a legacy row, a direct
	// SQL write, or a write made while tierConfig was nil). See its full doc comment on
	// profileRepository (profile_repository.go) for why it reuses the SAME injected
	// overrideTierValidator every other validation seam already uses, rather than a hand-copied SQL
	// tier list, and why it returns (nil, nil) rather than an empty slice when no validator has been
	// wired.
	GetUsersWithMalformedTierOverride(ctx context.Context) ([]*domain.User, error)

	// GetOrphanedFKlessUserScopedRowCounts is Issue #1440 round-3 Finding 5's monitoring query
	// (Codex review, coordinator's #1440 follow-up brief): reports, per table, how many rows in
	// dead_letter_jobs/idempotency_keys/mcp_audit_log — the three user-scoped tables with NO foreign
	// key to users at all — currently reference a user_id/actor_user_id that no longer names any
	// real user. See its full doc comment on profileRepository (profile_repository.go) for the
	// complete rationale: this state can only arise from a concurrent writer racing
	// DeleteUserCascade's own erasure of these tables in the narrow window before that transaction
	// commits, a residual DeleteUserCascade's own row lock cannot close because none of these three
	// tables has any FK to users at all.
	//
	// Deliberately NOT part of the broad canonical ProfileRepository interface, for the same reason
	// as its siblings above.
	GetOrphanedFKlessUserScopedRowCounts(ctx context.Context) (repository.OrphanedFKlessUserScopedRowCounts, error)

	// CountRetentionDeletionsWithheld is Issue #1569 T1's monitoring query, called by
	// monitorRetentionDeletionsWithheld at Step 2b of every Work() pass. It reports the accounts
	// the notice gate is refusing to hard-delete, split from the healthy population whose retention
	// clock is still running. See its full doc comment on profileRepository
	// (profile_repository.go) for why that split is the whole value of the metric.
	//
	// Deliberately NOT part of the broad canonical ProfileRepository interface, for the same reason
	// as its siblings above.
	CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error)
}

// RetentionDeletionService defines the deletion methods needed for retention cleanup.
type RetentionDeletionService interface {
	// ExecuteDeletion executes the full deletion process for a user under the caller-declared
	// authorisation kind. This worker always passes repository.DeletionKindRetention — see
	// deleteExpiredAccounts.
	ExecuteDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) error
}

// RetentionEmailService defines the email methods needed for retention notices.
type RetentionEmailService interface {
	// SendRetentionNoticeEmail sends a retention notice email to a user.
	SendRetentionNoticeEmail(ctx context.Context, toEmail, subject, body string) error
}

// RetentionPlaidCleanup defines the Plaid cleanup operation needed by repairPlaidDisconnects —
// Issue #1413 round-7 grace-window repair path. Narrow, single-method interface mirroring this
// codebase's existing per-consumer convention (PastDuePlaidCleanup, PlaidItemCleanupForWebhook)
// rather than depending on the full services.PlaidItemCleanupService.
// DisconnectAllUserItemsIfEligible is itself idempotent and self-claiming
// (repository.ActionPlaidDisconnectUser, via services.claimSideEffectAndRun) —
// repairPlaidDisconnects does not need its own additional claim around this call.
type RetentionPlaidCleanup interface {
	// DisconnectAllUserItemsIfEligible calls Plaid /item/remove for all items then marks them as
	// 'disconnected' (preserves transaction history) — but re-invokes recheck immediately before
	// EACH item's removal, aborting the remainder the first time it reports ineligible. Issue #1413
	// round-7: repairPlaidDisconnects re-verifies read-only status per item, not once per user,
	// since disconnect fans out into one Plaid call per linked item and a user can resubscribe
	// mid-loop. See services.PlaidItemCleanupService.DisconnectAllUserItemsIfEligible's doc
	// comment for the full contract.
	DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error
}

// RetentionCleanupCheckWorker processes retention cleanup check jobs.
// It handles the daily lifecycle of inactive/cancelled accounts:
//   - 30d before expiry: warn user via email
//   - 7d before expiry: final warning via email
//   - After expiry: auto-delete account data
type RetentionCleanupCheckWorker struct {
	river.WorkerDefaults[RetentionCleanupCheckArgs]

	profileRepo     RetentionProfileRepository
	deletionService RetentionDeletionService
	emailService    RetentionEmailService
	// plaidCleanup backs repairPlaidDisconnects (Issue #1413 round-7 grace-window repair path).
	// Wired via SetPlaidCleanup() after Plaid is configured.
	//
	// Round-11 through the CI finding fixed below made opposite claims about what a nil value here
	// means, and it is worth reading both: round-11 (through round-9's own review) said a nil value
	// does NOT "simply skip the repair pass" — it was made a hard configuration error
	// (errPlaidCleanupNotConfigured) that failed the WHOLE job, specifically to catch a deployment
	// that silently ran this pass with no Plaid cleanup wired up. That reasoning held as long as this
	// job only ran on its daily timer. Once RetentionCleanupCheckPeriodicOpts set RunOnStart: true
	// (see that function's own doc comment), this job started running at EVERY process boot in EVERY
	// environment — including QA/E2E, which legitimately has no Plaid service configured at all. A
	// misconfiguration detector that fires on a genuinely correct QA configuration is not a detector
	// anymore; it is recurring noise that fails the whole job (retention-clock repair, GDPR notices,
	// and expired-account deletion included) on every deploy, and CI's E2E job showed exactly that:
	// this error repeating at ERROR level across retries toward the dead-letter queue. A nil
	// plaidCleanup is now a QUIET skip of the Plaid-disconnect repair pass specifically (Warn-logged,
	// counted via observability.PlaidCleanupServiceNotConfiguredTotal so a sustained PRODUCTION rate
	// is still visible and alertable) — see repairPlaidDisconnects's own doc comment for the nil-check
	// itself, which now runs AFTER the backstop (Stamp/Clear), not before: the backstop is pure SQL
	// with no Plaid dependency, and keeping plaid_cleanup_pending_at correct is valuable regardless of
	// whether this particular pass can act on it. This field itself still cannot panic on a nil call
	// (repairPlaidDisconnects checks before ever calling through it).
	plaidCleanup RetentionPlaidCleanup
	// claimsRepo backs the durable work-claims mechanism send30dNotices/send7dNotices require for
	// EXCLUSIVITY on ActionRetentionNotice30d/7d before a notice email is sent (Issue #1413 Medium
	// finding M9, Codex review round 3). See SetWorkClaimsRepo — like
	// DeletionService/PastDueService/SubscriptionWebhookWorker's claims repos, this is required,
	// not optional.
	claimsRepo      repository.WorkClaimsRepository
	retentionMonths int
	logger          *slog.Logger
	// expiredRetentionPageSize / maxUsersPerRetentionSweep bound deleteExpiredAccounts (Issue
	// #1478). See the two constants of the same names for the rationale and the derivation.
	expiredRetentionPageSize  int
	maxUsersPerRetentionSweep int
	// plaidOrphanedItemRepo backs the orphaned-Plaid-Item alerting pass, Step 0e (Issue #1497 item
	// 4). Optional/nil-safe, and the nil case SKIPS rather than publishing zeroes.
	plaidOrphanedItemRepo OrphanedPlaidItemLister
	// sweepCursors persists where a capped deleteExpiredAccounts run stopped so the NEXT run
	// resumes there instead of restarting from the oldest expired row (Issue #1477).
	// Optional/nil-safe: a nil store reproduces the pre-#1477 behaviour exactly. See
	// sweep_resume.go and repository/sweep_cursor_repository.go.
	sweepCursors sweepCursorStore
}

// OrphanedPlaidItemLister is the ONE verb monitorOrphanedPlaidItems needs from
// repository.PlaidOrphanedItemRepository. Issue #1497 item 4.
//
// ⚠ NARROW ON PURPOSE, for the reason services.orphanRetryRepo excludes PurgeExpiredTokens: this is
// an observability pass and must be structurally incapable of writing. Handing it MarkResolved or
// PurgeExpiredTokens would make "a monitoring step destroyed a retained credential" a bug that is
// merely absent rather than one that cannot compile. List is also the read that FILTERS NOTHING —
// resolved and purged rows both appear — which is exactly what a derivation over all three
// AdminStates requires.
type OrphanedPlaidItemLister interface {
	List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error)
}

// RetentionCleanupCheckWorkerDeps contains the dependencies for RetentionCleanupCheckWorker.
type RetentionCleanupCheckWorkerDeps struct {
	ProfileRepo     RetentionProfileRepository
	DeletionService RetentionDeletionService
	EmailService    RetentionEmailService
	RetentionMonths int
	Logger          *slog.Logger
	// ExpiredRetentionPageSize overrides expiredRetentionPageSize (Issue #1478). Zero/unset uses
	// the production default. Exists so tests can exercise the multi-page keyset-cursor loop
	// without seeding hundreds of fixture users — production should never set this.
	ExpiredRetentionPageSize int
	// MaxUsersPerRetentionSweep overrides maxUsersPerRetentionSweep (Issue #1478). Zero/unset uses
	// the production default. Exists so tests can exercise the total-processed safety cap without
	// seeding thousands of fixture users — production should never set this.
	MaxUsersPerRetentionSweep int
	// PlaidOrphanedItemRepo backs the orphaned-Plaid-Item alerting pass, Step 0e (Issue #1497 item
	// 4). Optional/nil-safe: an unwired repo SKIPS the pass and publishes nothing, rather than
	// publishing zeroes that would read as a clean bill of health — see monitorOrphanedPlaidItems.
	PlaidOrphanedItemRepo OrphanedPlaidItemLister
	// SweepCursors persists deleteExpiredAccounts's cross-run resume position (Issue #1477).
	// Optional and nil-safe: unwired, every run restarts from the oldest expired row, which is
	// exactly the pre-#1477 starvation-prone behaviour — production MUST wire it (main.go).
	SweepCursors repository.SweepCursorRepository
}

// NewRetentionCleanupCheckWorker creates a new retention cleanup check worker.
func NewRetentionCleanupCheckWorker(deps RetentionCleanupCheckWorkerDeps) *RetentionCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	w := &RetentionCleanupCheckWorker{
		profileRepo:               deps.ProfileRepo,
		deletionService:           deps.DeletionService,
		emailService:              deps.EmailService,
		retentionMonths:           intOrDefault(deps.RetentionMonths, 6), // Default: 6-month retention period
		logger:                    logger,
		expiredRetentionPageSize:  intOrDefault(deps.ExpiredRetentionPageSize, expiredRetentionPageSize),
		maxUsersPerRetentionSweep: intOrDefault(deps.MaxUsersPerRetentionSweep, maxUsersPerRetentionSweep),
		plaidOrphanedItemRepo:     deps.PlaidOrphanedItemRepo,
	}
	// Guarded rather than assigned directly, for the reason given in
	// NewScheduledDeletionCheckWorker: "unwired means nil, and nil means do not persist" stays true
	// by inspection.
	if deps.SweepCursors != nil {
		w.sweepCursors = deps.SweepCursors
	}
	return w
}

// SetWorkClaimsRepo wires the durable work-claims mechanism send30dNotices/send7dNotices require
// for EXCLUSIVITY on ActionRetentionNotice30d/7d before a notice email is sent (Issue #1413 Medium
// finding M9, Codex review round 3). Mirrors DeletionService/PastDueService/
// SubscriptionWebhookWorker's SetWorkClaimsRepo — like theirs, this is NOT an optional capability:
// a nil claims repo makes the notice-sending path fail loudly (logged, per-user, as an error) for
// every candidate rather than silently reverting to the old conflated claim-marker behavior.
func (w *RetentionCleanupCheckWorker) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	w.claimsRepo = repo
}

// SetPlaidCleanup wires the Plaid cleanup collaborator repairPlaidDisconnects requires — Issue
// #1413 round-6 repair path. Called after Plaid is configured (main.go), mirroring
// TrialService.SetPlaidCleanup/PastDueService.SetPlaidCleanup's late-binding convention.
//
// Issue #1413 round-11 said an unset plaidCleanup was a hard configuration error that failed the
// WHOLE retention_cleanup_check job — correct reasoning at the time, but it stopped being correct
// once RetentionCleanupCheckPeriodicOpts set RunOnStart: true and this job started running at every
// process boot in every environment, including QA/E2E, which legitimately never configures Plaid.
// A later CI finding (fixed alongside Issue #1413 Finding 1) restored a QUIET skip: never calling
// this setter now makes repairPlaidDisconnects Warn-log and skip ONLY the Plaid-disconnect repair
// pass (retention-clock repair, GDPR notices, and expired-account deletion all still run), while
// still incrementing observability.PlaidCleanupServiceNotConfiguredTotal so a sustained PRODUCTION
// rate remains visible and alertable — see repairPlaidDisconnects's own doc comment for the nil
// check itself and why it now runs AFTER the self-healing backstop, not before.
func (w *RetentionCleanupCheckWorker) SetPlaidCleanup(c RetentionPlaidCleanup) {
	w.plaidCleanup = c
}

// releaseClaimOnFailure releases a work claim and logs any release failure. Mirrors
// PastDueService/SubscriptionWebhookWorker's helper of the same name.
//
// userID and resourceID are both taken (rather than deriving one from the other) because, since
// Issue #1413 P5, resourceID is a composite "{userID}:{occurrence discriminator}" built by
// repository.OccurrenceResourceID — userID stays available for clean, greppable log fields while
// resourceID is what actually goes into the Release call.
func (w *RetentionCleanupCheckWorker) releaseClaimOnFailure(ctx context.Context, userID, resourceID, action, token, reason string) {
	if _, releaseErr := w.claimsRepo.Release(ctx, repository.ResourceTypeUser, resourceID, action, token, reason); releaseErr != nil {
		w.logger.Warn("failed to release work claim", "user_id", userID, "resource_id", resourceID, "action", action, "reason", reason, "error", releaseErr)
	}
}

// Work processes a retention cleanup check job.
//
// The worker:
//  1. Finds users needing 30-day notice → sends email → marks notice sent
//  2. Finds users needing 7-day notice → sends email → marks notice sent
//  3. Finds users with expired retention → calls ExecuteDeletion for each
//
// Partial deletion failures cause the job to return an error so River retries the run.
func (w *RetentionCleanupCheckWorker) Work(ctx context.Context, job *river.Job[RetentionCleanupCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting retention cleanup check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			// Issue #1413 Task P4: generic last-success heartbeat — lets an alert fire on
			// "this job has not succeeded in > 48h", which jobs_completed_total{status="failed"}
			// alone cannot: that counter never increments at all if the job stops running
			// entirely (crashes before Work() runs, or sits dead-lettered with no replay).
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	// Step 0: Repair any read-only-status user whose retention clock never started — Issue #1413
	// round-3 finding H5 residual, belt-and-braces backstop. Run BEFORE the notice/deletion steps
	// below so a user repaired in this very pass can become eligible for a notice in the SAME
	// run rather than waiting for tomorrow's. Non-fatal like steps 1/2 — a failure here must not
	// block deletion processing, which is the more time-sensitive step.
	//
	// Issue #1413 round-10: no longer has an ordering dependency with Step 0b (repairPlaidDisconnects)
	// — Step 0b's grace window is now anchored on the plaid_cleanup_pending_at marker (set
	// atomically with the subscription_status transition itself), not on retention_started_at,
	// which this step repairs. The two repair passes are independent.
	if repairErr := w.repairRetentionClocks(ctx, job.ID); repairErr != nil {
		w.logger.Error("failed to repair retention clocks",
			"job_id", job.ID,
			"error", repairErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 0b: Disconnect Plaid for any read-only-status user past the round-7 grace window —
	// Issue #1413 round-7 (Codex review). This is the ONLY place a read-only user's Plaid items
	// are disconnected at all; TrialService.ExpireTrial never calls Plaid (see that function's doc
	// comment for why no ordering of a status flip and an irreversible Plaid call can be safe).
	//
	// A missing Plaid cleanup collaborator no longer fails the whole job (through round-10 Fix 1 it
	// did) — see repairPlaidDisconnects's own doc comment and the RetentionCleanupCheckWorker.plaidCleanup
	// field comment above for why: RunOnStart: true means this job now runs at every process boot in
	// every environment, including QA/E2E, which legitimately has no Plaid service configured, and a
	// misconfiguration detector that fails the whole job (GDPR notices and deletion included) on every
	// correct QA deploy is not a detector, it is noise. repairPlaidDisconnects now handles that case
	// internally (Warn-logged, observability.PlaidCleanupServiceNotConfiguredTotal incremented,
	// returns nil) — so every repairPlaidDisconnects failure that DOES still reach here is a genuine,
	// non-configuration failure (e.g. a transient candidate-fetch error) and stays non-fatal, matching
	// Step 0/1/2's existing per-step resilience.
	if repairErr := w.repairPlaidDisconnects(ctx, job.ID); repairErr != nil {
		w.logger.Error("failed to repair Plaid disconnects",
			"job_id", job.ID,
			"error", repairErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 0c: Issue #1453 monitoring pass — report the current count of users whose
	// manual_tier_override does not name a configured tier. See monitorMalformedTierOverrides's own
	// doc comment for the full rationale. Non-fatal for the same reason every other step here is:
	// this is an observability check, not a repair, and must never block GDPR notices or expired-
	// account deletion.
	if monitorErr := w.monitorMalformedTierOverrides(ctx, job.ID); monitorErr != nil {
		w.logger.Error("failed to check for malformed manual_tier_override rows",
			"job_id", job.ID,
			"error", monitorErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 0d: Issue #1440 round-3 Finding 5 monitoring pass — report the current count, per table,
	// of rows in dead_letter_jobs/idempotency_keys/mcp_audit_log whose user_id/actor_user_id no
	// longer names any real user. See monitorOrphanedFKlessRows's own doc comment for the full
	// rationale. Non-fatal for the same reason every other step here is: this is an observability
	// check, not a repair, and must never block GDPR notices or expired-account deletion.
	if monitorErr := w.monitorOrphanedFKlessRows(ctx, job.ID); monitorErr != nil {
		w.logger.Error("failed to check for orphaned FK-less user-scoped rows",
			"job_id", job.ID,
			"error", monitorErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 0e: Issue #1497 item 4 monitoring pass — publish how many plaid_orphaned_items rows are
	// in each admin state, and the worst UNRESOLVED retry_attempt_count. See
	// monitorOrphanedPlaidItems's own doc comment for why this pass lives here rather than in the
	// retry sweep (the manual-intervention population is invisible to that sweep by construction).
	// Non-fatal for the same reason Steps 0c/0d are: this is an observability check that makes no
	// write, and must never block GDPR notices or expired-account deletion.
	if monitorErr := w.monitorOrphanedPlaidItems(ctx, job.ID); monitorErr != nil {
		w.logger.Error("failed to check the orphaned-Plaid-Item queue",
			"job_id", job.ID,
			"error", monitorErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 1: Send 30-day pre-deletion warnings
	if notice30Err := w.send30dNotices(ctx, job.ID); notice30Err != nil {
		w.logger.Error("failed to process 30d retention notices",
			"job_id", job.ID,
			"error", notice30Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 2: Send 7-day pre-deletion warnings
	if notice7Err := w.send7dNotices(ctx, job.ID); notice7Err != nil {
		w.logger.Error("failed to process 7d retention notices",
			"job_id", job.ID,
			"error", notice7Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 2b: Issue #1569 T1 monitoring pass — report how many accounts Step 3 is REFUSING to
	// delete because a promised pre-deletion notice was never recorded, and why. Runs immediately
	// before Step 3 because it measures exactly what Step 3 is about to decline to do. Non-fatal
	// for the same reason Steps 0c/0d/0e are: this is an observability check that makes no write,
	// and must never block GDPR notices or expired-account deletion.
	if monitorErr := w.monitorRetentionDeletionsWithheld(ctx, job.ID); monitorErr != nil {
		w.logger.Error("failed to count retention hard-deletions withheld for want of a pre-deletion notice",
			"job_id", job.ID,
			"error", monitorErr,
		)
		// Don't fail job — continue with other steps
	}

	// Step 3: Auto-delete users with expired retention
	// Propagate partial failure so River retries — failed users will be picked up again
	if deleteErr := w.deleteExpiredAccounts(ctx, job.ID); deleteErr != nil {
		w.logger.Error("failed to process expired retention deletions",
			"job_id", job.ID,
			"error", deleteErr,
		)
		// errors.Join (not a plain overwrite) so a Step 0b configuration-error already recorded in
		// err above is never silently discarded by a Step 3 failure on the same run — both must
		// surface, and errors.Join(nil, deleteErr) is just deleteErr when err is still nil.
		err = errors.Join(err, deleteErr)
	}

	w.logger.Info("retention cleanup check completed",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return err
}

// retentionDeletionItem is one line of the enumeration both retention notices carry: a phrase the
// user reads, and the set of live tables that phrase claims to cover.
//
// Issue #1581. The notices exist to be the "published retention policy" that
// docs/decisions/2026-08-24-financial-history-retained-by-default.md Decision 3 requires before an
// AUTOMATIC destroyer is permitted at all. That justification is only as good as the enumeration:
// #1569 newly RETAINS a large amount of financial history past a disconnect, a goal deletion and a
// budget deletion, which means this sweep is newly the thing that DESTROYS it. Naming three
// categories while destroying fifty-odd tables invalidated the branch's own argument.
//
// The `tables` field is not decoration. TestRetentionNoticeEnumerationCoversEverythingDeleted
// re-derives what DeleteUserCascade actually destroys — the transitive ON DELETE CASCADE closure
// from `users` in the LIVE schema, plus the explicit `DELETE FROM`s in DeleteUserCascade's own
// source — and fails when any destroyed table is not claimed by some phrase here. A new migration
// that hangs a cascading table off `users` therefore cannot land without someone TOUCHING this
// list.
//
// ⚠ Precisely that, and no more: the guard checks COVERAGE and STALENESS, not TRUTHFULNESS. Adding
// a table name to an existing item's `tables` turns the suite green with zero words of copy
// changed — falsified deliberately by a reviewer with a probe table live. Nothing mechanical can
// decide whether a phrase honestly describes the rows it claims; that judgement is the reviewer's,
// and the two most elastic phrases (the internal-records item and "saved automation recipes") are
// the ones to re-read first.
//
// ⚠ Twelve of these thirteen phrases used to be pinned by nothing at all — issue #1593. Apart from
// the audit_logs item, which was singled out by name, every assertion on `phrase` was either a
// non-emptiness check or require.Contains(body, item.phrase) — and `body` is RENDERED from these
// phrases, so it contains them by construction. An assertion that renders from the source it
// checks cannot detect a change to that source: item 2's phrase was replaced with the outright lie
// "nothing at all, we keep your balance history forever" and the whole package stayed green
// (re-confirmed here before the fix). The pin is now testdata/retention_notice.golden — a reviewed snapshot of both rendered bodies plus
// the phrase -> tables mapping — that pins all thirteen, backed by the per-item `mustMention` floor
// below. If you edit a phrase, expect TestRetentionNoticeMatchesReviewedGolden to fail; that is the
// point, and its failure message says what to check before promoting the new text.
//
// ⚠ Still not truthfulness. The golden forces a HUMAN to look at a copy change to a legal notice;
// it cannot decide whether the new words honestly describe the rows. Neither can mustMention. That
// judgement has no mechanical substitute and is not going to get one.
type retentionDeletionItem struct {
	phrase string
	tables []string
	// mustMention are terms the phrase must still contain, matched case-insensitively by
	// TestEveryRetentionDeletionItemNamesItsSubject (#1593). Each must be at least three
	// characters and appear in no more than three of the thirteen phrases, so that a term is
	// capable of failing and capable of telling THIS item apart from its siblings.
	//
	// ⚠ This is a floor, not a truth check, and it is the deliberately WEAK half of the #1593 pin.
	// Measured, not guessed: for twelve of the thirteen items a "we keep this forever" inversion
	// retains every term here and is caught by the GOLDEN alone. Item 13 is the exception, and
	// only because its term is the QUALIFICATION rather than a subject noun — which is the better
	// pattern to copy when a phrase has something falsifiable to hold onto.
	//
	// Its one job the golden cannot do is SURVIVE a thoughtless golden regeneration: it lives here,
	// next to the phrase, and nothing regenerates it. If you reword a phrase, update these in the
	// same edit — and prefer a term the inverted sentence could not keep.
	mustMention []string
}

// retentionDeletionItems is the enumeration. Order is the order the user reads it in: the money
// first, the settings last.
//
// Wording is deliberately CATEGORY-level rather than table-level — the user has never heard of
// `holding_snapshots`. What the guard requires is that every destroyed table is honestly covered by
// some phrase, not that each one gets its own bullet; a phrase may therefore claim several tables,
// but it may not claim a table a reader would not recognise in it.
//
// ⚠ `audit_logs` is the one case where the notice must NOT claim plain destruction, and an earlier
// draft of this list got it backwards. `audit_logs.user_id` is ON DELETE SET NULL — which is why
// the table is absent from the CASCADE closure — and DeleteUserCascade deletes only the
// deletion-residue rows (`resource_id = $1 AND action LIKE $2`, profile_repository.go) and blanks
// `changes` on the rest. Every other audit row SURVIVES, still carrying action, resource_type,
// resource_id, ip_address, user_agent, request_id, error_message and created_at.
//
// The earlier draft reasoned that understating what survives is the safe direction for a warning.
// That holds only under the warning-about-LOSS reading. Under the erasure-PROMISE reading — the one
// a regulator applies to a deletion notice — over-claiming erasure is the unsafe direction: the
// user would be told their IP addresses and user agents are gone when they are retained. Item 13
// and the categorical sentence are both qualified accordingly, and the qualification also carries
// the privacy policy's own carve-outs (§8.2 anonymised retention, §8.3 90-day encrypted backups,
// §8.4 legal retention), which an unqualified "everything" silently dropped.
var retentionDeletionItems = []retentionDeletionItem{
	{
		// #1620: linked_item_repair_nonces belongs here rather than under the internal-records
		// item. It is not a job or an activity record — it is the short-lived artifact of a
		// reconnection the user themselves started from the "Reconnect Bank" button, so "any
		// reconnection you had started" is a thing they can actually recognise. It is destroyed
		// outright (ON DELETE CASCADE from both users and linked_items), with nothing retained,
		// so plain destruction is the honest claim; none of audit_logs' SET NULL qualification
		// applies.
		phrase:      "your bank connections and every account on them, including accounts you have already closed or disconnected, the record of when each one was connected, disconnected or closed, and any reconnection you had started",
		tables:      []string{"linked_items", "accounts", "user_accounts", "account_status_history", "linked_item_repair_nonces"},
		mustMention: []string{"bank connections", "accounts"},
	},
	{
		phrase:      "your day-by-day balance history for every account",
		tables:      []string{"balance_snapshots"},
		mustMention: []string{"balance history"},
	},
	{
		phrase:      "your transactions, together with every category, note, tag and merchant name you set on them, and the record of those edits",
		tables:      []string{"transactions", "transaction_annotation_history", "user_tags", "merchant_name_overrides", "custom_categories"},
		mustMention: []string{"transactions", "tag", "merchant"},
	},
	{
		phrase:      "your investment holdings and their day-by-day snapshots, every buy and sell recorded against them, and your portfolio value history",
		tables:      []string{"holdings", "holding_snapshots", "holding_observations", "investment_transactions", "portfolio_snapshots", "portfolio_snapshot_repairs"},
		mustMention: []string{"holdings", "portfolio"},
	},
	{
		phrase:      "your credit card, mortgage and student loan records, including their statements and APR schedules",
		tables:      []string{"credit_liabilities", "credit_liability_aprs", "liability_statements", "mortgage_liabilities", "student_loan_liabilities"},
		mustMention: []string{"credit card", "mortgage", "student loan", "statements"},
	},
	{
		phrase: "your budgets, their categories, their period history and every rollover",
		// Issue #1630 Task B7: budget_hierarchy_audits carries its own user_id CASCADE (same
		// shape as portfolio_snapshot_repairs above) — it is the audit ledger for the budgets this
		// same phrase already names, not a separate kind of record.
		tables:      []string{"budgets", "budget_categories", "budget_group_members", "budget_period_events", "rollover_history", "budget_hierarchy_audits"},
		mustMention: []string{"budgets", "rollover"},
	},
	{
		phrase:      "your savings goals and every contribution you logged against them by hand",
		tables:      []string{"savings_goals", "savings_goal_contributions", "savings_goal_category_detach"},
		mustMention: []string{"savings goals", "contribution"},
	},
	{
		phrase:      "your boosts and the record of every boost that ran",
		tables:      []string{"boosts", "boost_executions"},
		mustMention: []string{"boosts"},
	},
	{
		phrase:      "your automation rules, the record of every rule that ran, and your transfer suggestions",
		tables:      []string{"categorization_rules", "rule_execution_history", "transfer_suggestions"},
		mustMention: []string{"automation rules", "transfer suggestions"},
	},
	{
		phrase:      "your membership of a household, its invitations, its sharing consents, and the accounts shared into it (and the household itself, if you are its only member)",
		tables:      []string{"households", "household_members", "household_invites", "household_share_consents", "household_account_shares"},
		mustMention: []string{"household"},
	},
	{
		phrase:      "your subscription, billing and payment records, and your referrals",
		tables:      []string{"subscription_entitlements", "paddle_transactions", "user_subscription_overrides", "referrals", "referral_codes"},
		mustMention: []string{"subscription", "billing", "referrals"},
	},
	{
		phrase:      "your profile and sign-in identities, notifications, dashboard layout, privacy settings and saved automation recipes",
		tables:      []string{"users", "user_identities", "user_email_prefix_indices", "notifications", "user_dashboard_preferences", "privacy_settings", "mcp_recipes"},
		mustMention: []string{"profile", "notifications", "dashboard", "privacy settings"},
	},
	{
		phrase:      "the internal job and activity records tied to your account — deleted, or stripped of their contents and disconnected from you where we are required to keep a record",
		tables:      []string{"audit_logs", "dead_letter_jobs", "idempotency_keys", "mcp_audit_log"},
		mustMention: []string{"required to keep a record"},
	},
}

// retentionNoticeEnumeration renders retentionDeletionItems into the notice bodies. Rendering it
// rather than hand-writing it twice is what keeps the 30-day and 7-day notices from drifting apart,
// and what keeps either of them from drifting from the table coverage the guard checks.
func retentionNoticeEnumeration() string {
	phrases := make([]string, 0, len(retentionDeletionItems))
	for _, item := range retentionDeletionItems {
		phrases = append(phrases, item.phrase)
	}
	return "\n- " + strings.Join(phrases, "\n- ") + "\n"
}

// retentionNotice30dBody / retentionNotice7dBody are the notice bodies, exposed as package-level
// values so a test can read the exact text that is sent rather than a paraphrase of it.
var retentionNotice30dBody = "Your account data is preserved in read-only mode. In 30 days, unless you resubscribe, " +
	"we will permanently delete everything in your BoostedBudget account, except where the law requires us to keep " +
	"a record. This cannot be undone, and there is no way to restore it afterwards. In full, we will delete:\n" + retentionNoticeEnumeration() +
	"\nIf you want to keep any of this, resubscribe at boostedbudget.com/pricing to restore full access."

var retentionNotice7dBody = "This is your final warning. In 7 days, we will permanently delete everything in your " +
	"BoostedBudget account, except where the law requires us to keep a record. This cannot be undone, and there is " +
	"no way to restore it afterwards. In full, we will delete:\n" + retentionNoticeEnumeration() +
	"\nIf you want to keep any of this, resubscribe at boostedbudget.com/pricing within the next 7 days."

// retentionNoticeSpec parameterizes processRetentionNotices over the 30-day/7-day notice
// variants, which differ only in: the work_claims action, the log/email copy, the eligibility
// re-check, and which *_sent_at column gets stamped after a successful send.
type retentionNoticeSpec struct {
	action     string
	noticeName string // for log messages, e.g. "30d"
	subject    string
	body       string
	// isEligible takes the CLAIMED retention_expires_at occurrence as its third argument (Issue
	// #1413 round-4 finding M1) — processRetentionNotices passes *user.RetentionExpiresAt, the
	// SAME value the candidate query returned and the claim key was built from, so the check can
	// require the row's CURRENT retention_expires_at to still match it.
	isEligible func(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error)
	markSent   func(ctx context.Context, userID string) error
}

// send30dNotices finds users needing 30-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send30dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention30dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 30d notice: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing 30d retention notice", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing 30d retention notice", "job_id", jobID, "user_count", len(users))

	sentCount, errorCount, skippedCount := w.processRetentionNotices(ctx, jobID, users, retentionNoticeSpec{
		action:     repository.ActionRetentionNotice30d,
		noticeName: "30d",
		subject:    "Your BoostedBudget data will be deleted in 30 days",
		// Issue #1569/#1581: names what gets deleted, item by item, instead of the vague "your
		// data" — see retentionDeletionItems for why the list is derived rather than written here.
		body:       retentionNotice30dBody,
		isEligible: w.profileRepo.IsRetentionNotice30dEligibleForOccurrence,
		markSent:   w.profileRepo.SetRetentionNotice30dSent,
	})

	w.logger.Info("30d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount, "skipped", skippedCount)
	return nil
}

// send7dNotices finds users needing 7-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send7dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention7dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 7d notice: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing 7d retention notice", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing 7d retention notice", "job_id", jobID, "user_count", len(users))

	sentCount, errorCount, skippedCount := w.processRetentionNotices(ctx, jobID, users, retentionNoticeSpec{
		action:     repository.ActionRetentionNotice7d,
		noticeName: "7d",
		subject:    "Final notice: BoostedBudget data deletion in 7 days",
		// Issue #1569/#1581: same disclosure sharpening as the 30-day notice above.
		body:       retentionNotice7dBody,
		isEligible: w.profileRepo.IsRetentionNotice7dEligibleForOccurrence,
		markSent:   w.profileRepo.SetRetentionNotice7dSent,
	})

	w.logger.Info("7d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount, "skipped", skippedCount)
	return nil
}

// processRetentionNotices implements the M9-corrected claim -> re-verify -> send -> stamp ->
// complete sequence shared by the 30-day and 7-day retention notices (Issue #1413 Medium finding
// M9, Codex review round 3):
//
//  1. Belt-and-braces HasActiveManualOverride() short-circuit against the candidate snapshot —
//     cheap, but NOT the source of correctness (see the claim/re-verify below for that).
//  2. Skip WITHOUT claiming when there is demonstrably nothing to send (no email service
//     configured, or the user has no address on file) — claiming and immediately releasing would
//     just churn the work_claims table for no benefit.
//  3. Claim spec.action for EXCLUSIVITY. Nothing user-visible has happened yet.
//  4. Re-verify eligibility via spec.isEligible (still in window, still unsent, still uncomped)
//     against the freshest possible read — the claim gives exclusivity, this gives correctness.
//  5. Send the notice email.
//  6. ONLY on send success, stamp *_sent_at via spec.markSent, then mark the claim Complete.
//
// On any failure (claim error, ineligible, send error, or a failure to stamp *_sent_at after a
// successful send), the claim is Released with the failure reason instead of Completed — the
// *_sent_at column stays NULL, so the next daily run retries. This is the fix for the bug this
// task exists to close: the old code stamped *_sent_at BEFORE sending, so a failed send left the
// marker set and the user was silently never warned again before their data was deleted.
//
// Residual, by design: if the email PROVIDER accepts the message but SendRetentionNoticeEmail
// still returns an error (or spec.markSent itself fails right after a genuinely successful send),
// the claim is Released and the next run retries — which re-sends the email. At-least-once is the
// deliberate choice over never-warn: a duplicate warning email is recoverable, a silently-skipped
// one is not.
func (w *RetentionCleanupCheckWorker) processRetentionNotices(ctx context.Context, jobID int64, users []*domain.User, spec retentionNoticeSpec) (sentCount, errorCount, skippedCount int) {
	for _, user := range users {
		// #1413: belt-and-braces — a comped user grants access and must not be warned about, or
		// slated for, deletion. Cheap short-circuit against the candidate list snapshot; the claim
		// + re-verify below is the real enforcement.
		//
		// Deliberately HasActiveManualOverride (live-only), NOT HasAnyManualOverride — even though
		// both the candidate queries (GetUsersNeedingRetention30d/7dNotice) and the authoritative
		// re-verify just below (spec.isEligible, i.e. IsRetentionNotice30d/7dEligibleForOccurrence)
		// apply the all-overrides rule. Mirrors deletion_service.go's ExecuteDeletion guard: this
		// cheap check only short-circuits the case it can be certain about (a currently-live comp).
		// A LAPSED-but-uncleared override is deliberately left to fall through to the claim + the
		// authoritative re-verify below, which excludes it on its own. Both routes reach the same
		// outcome — no notice sent — but routing the lapsed case through the authoritative
		// re-verify keeps the DB predicate the single source of truth and keeps it exercised.
		// Widening this check to HasAnyManualOverride would short-circuit before the authoritative
		// re-verify ever ran, masking any future divergence between the two. Pinned by
		// TestRetentionCleanupCheck_LapsedComp_30dNotice_ReachesAuthoritativeReVerify.
		if user.HasActiveManualOverride() {
			w.logger.Info("skipping "+spec.noticeName+" retention notice for actively comped user",
				"job_id", jobID, "user_id", user.ID)
			continue
		}

		// Issue #1413 M9: prefer not to claim at all when there is demonstrably nothing to send —
		// claiming and immediately releasing would just churn the work_claims table for no
		// benefit.
		if !w.canSendRetentionNotice(jobID, spec.noticeName, user) {
			skippedCount++
			continue
		}

		if w.claimsRepo == nil {
			w.logger.Error("processRetentionNotices: work claims repository not configured",
				"job_id", jobID, "user_id", user.ID, "notice", spec.noticeName)
			errorCount++
			continue
		}
		// Issue #1413 P5: the claim key must identify not just WHO (user.ID) and WHAT (spec.action)
		// but WHICH retention OCCURRENCE — otherwise a user who is warned once, later resubscribes
		// (clearing the clock), and cancels again would find this (user, action) pair already
		// permanently completed from the FIRST retention cycle, and the second cycle's notice would
		// never fire before hard deletion. retention_expires_at is the retention-cycle
		// discriminator: it is set once when the clock starts and only changes when the clock is
		// re-armed (a fresh cancellation after a resubscribe), which is exactly the re-arm case
		// this fix must let notices fire again for.
		resourceID, err := repository.OccurrenceResourceID(user.ID, repository.FormatOccurrenceTimestamp(user.RetentionExpiresAt))
		if err != nil {
			w.logger.Error("processRetentionNotices: missing retention occurrence discriminator",
				"job_id", jobID, "user_id", user.ID, "notice", spec.noticeName, "error", err)
			errorCount++
			continue
		}
		token, claimed, err := w.claimsRepo.Claim(ctx, repository.ResourceTypeUser, resourceID, spec.action, retentionNoticeClaimLease)
		if err != nil {
			w.logger.Warn("failed to claim "+spec.noticeName+" retention notice",
				"job_id", jobID, "user_id", user.ID, "error", err)
			errorCount++
			continue
		}
		if !claimed {
			// Another worker holds this lease, or the action was already completed. Legitimate
			// outcome, not an error.
			w.logger.Info("skipping "+spec.noticeName+" retention notice — claim not granted (comped since query, already sent, or a concurrent worker holds it)",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		// Re-verify eligibility against the freshest possible read, immediately after winning the
		// claim and before the email actually goes out. *user.RetentionExpiresAt is the SAME
		// occurrence the claim above was keyed on (Issue #1413 round-4 finding M1) — guaranteed
		// non-nil here since OccurrenceResourceID above would have already errored (and `continue`d)
		// on a nil RetentionExpiresAt.
		eligible, err := spec.isEligible(ctx, user.ID, *user.RetentionExpiresAt)
		if err != nil {
			w.releaseClaimOnFailure(ctx, user.ID, resourceID, spec.action, token, err.Error())
			w.logger.Warn("failed to re-verify "+spec.noticeName+" retention notice eligibility",
				"job_id", jobID, "user_id", user.ID, "error", err)
			errorCount++
			continue
		}
		if !eligible {
			w.releaseClaimOnFailure(ctx, user.ID, resourceID, spec.action, token, "precondition lost since claim")
			w.logger.Info("skipping "+spec.noticeName+" retention notice — precondition lost since claim (comped, already sent, or window closed)",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		if sendErr := w.emailService.SendRetentionNoticeEmail(ctx, user.Email, spec.subject, spec.body); sendErr != nil {
			w.releaseClaimOnFailure(ctx, user.ID, resourceID, spec.action, token, sendErr.Error())
			w.logger.Warn("failed to send "+spec.noticeName+" retention notice",
				"job_id", jobID, "user_id", user.ID, "error", sendErr)
			errorCount++
			continue
		}

		// Only stamp *_sent_at AFTER the send actually succeeded — this ordering is the entire
		// M9 fix.
		if markErr := spec.markSent(ctx, user.ID); markErr != nil {
			// The email went out but we failed to record it. Release (not Complete) so the next
			// run retries — see the residual note in the doc comment above: this risks a
			// duplicate send, the deliberate at-least-once trade-off.
			w.releaseClaimOnFailure(ctx, user.ID, resourceID, spec.action, token, markErr.Error())
			w.logger.Error("sent "+spec.noticeName+" retention notice but failed to record it — next run will retry and may re-send",
				"job_id", jobID, "user_id", user.ID, "error", markErr)
			errorCount++
			continue
		}

		completed, err := w.claimsRepo.Complete(ctx, repository.ResourceTypeUser, resourceID, spec.action, token)
		if err != nil {
			w.logger.Error("failed to mark "+spec.noticeName+" retention notice claim complete",
				"job_id", jobID, "user_id", user.ID, "action", spec.action, "error", err)
		} else if !completed {
			// The lease was reclaimed by another worker while we were mid-flight — the fencing-
			// token scenario. The notice may have been sent twice; this must be visible, not
			// silent (per the #1413 ambiguity resolution for Complete returning ok=false).
			w.logger.Error(spec.noticeName+" retention notice claim lease lost mid-flight — notice may have been sent twice",
				"job_id", jobID, "user_id", user.ID, "action", spec.action)
		}

		sentCount++
	}
	return sentCount, errorCount, skippedCount
}

// canSendRetentionNotice reports whether a notice email can actually be sent for user right now,
// logging why not otherwise. Issue #1413 M9: called BEFORE claiming so a demonstrably-unsendable
// notice (no email service configured, no address on file) never touches work_claims at all —
// claiming and immediately releasing would just churn the table for no benefit. Prior to this fix,
// both cases still stamped the *_sent_at marker despite nothing being sent.
func (w *RetentionCleanupCheckWorker) canSendRetentionNotice(jobID int64, noticeName string, user *domain.User) bool {
	if w.emailService == nil {
		w.logger.Info("skipping "+noticeName+" retention notice: no email service configured",
			"job_id", jobID, "user_id", user.ID)
		return false
	}
	if user.Email == "" {
		w.logger.Warn("user has no email, skipping "+noticeName+" retention notice",
			"job_id", jobID, "user_id", user.ID)
		return false
	}
	return true
}

// repairRetentionClocks finds users stuck read-only-status with no retention clock and repairs
// them — Issue #1413 round-3 finding H5 residual. CancelIfPastDueWithRetention
// (past_due_service.go) closes the specific race on the past-due grace-cancel path going
// forward by making the CAS and the retention write one atomic statement, but this repair query
// also catches: (a) any row already stuck in this state from before that fix existed, and (b)
// any other, currently-unknown path that might leave a user cancelled/inactive with a NULL
// clock. retentionMonths matches the same duration used everywhere else in this worker.
func (w *RetentionCleanupCheckWorker) repairRetentionClocks(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetentionClockRepair(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing retention clock repair: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing retention clock repair", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing retention clock repair", "job_id", jobID, "user_count", len(users))

	// Task NA2 (Issue #1413 follow-up on PR #1443): retention_expires_at used to be computed here
	// via now.AddDate(0, w.retentionMonths, 0) — Go's month arithmetic normalizes overflow FORWARD,
	// disagreeing with the Postgres make_interval semantics every other retention_expires_at writer
	// now uses, including SetRetentionDates/RemoveUserTierOverride, the very writers this repair
	// pass exists to backstop. w.retentionMonths is passed straight through so RepairRetentionClock
	// can compute the deadline entirely in Postgres instead — a repaired row must land on the SAME
	// deadline a fresh write from those writers would have produced.
	now := time.Now()
	var repairedCount, skippedCount, errorCount int
	for _, user := range users {
		// Belt-and-braces: a comped user must never have a retention clock started for them,
		// even though the candidate query already excludes any override (live or lapsed) via
		// noManualOverrideAtAllPredicate. Mirrors the same defensive pattern used throughout this
		// worker and past_due_service.go.
		if user.HasAnyManualOverride() {
			w.logger.Info("skipping retention clock repair for user with a manual tier override (live or lapsed)",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		repaired, repairErr := w.profileRepo.RepairRetentionClock(ctx, user.ID, now, w.retentionMonths)
		if repairErr != nil {
			w.logger.Warn("failed to repair retention clock",
				"job_id", jobID, "user_id", user.ID, "error", repairErr)
			errorCount++
			continue
		}
		if !repaired {
			// Already repaired by a concurrent run, or the user's status/clock changed since the
			// candidate query ran — a legitimate skip, not an error.
			w.logger.Info("retention clock repair skipped — precondition lost since query",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		w.logger.Info("retention clock repaired", "job_id", jobID, "user_id", user.ID)
		repairedCount++
	}

	w.logger.Info("retention clock repair processed",
		"job_id", jobID, "repaired", repairedCount, "skipped", skippedCount, "errors", errorCount)
	return nil
}

// repairPlaidDisconnects finds read-only-status users, past the round-7 grace window
// (plaidDisconnectRepairGracePeriod) as measured from their plaid_cleanup_pending_at marker
// (Issue #1413 round-10), with at least one live linked item, and disconnects Plaid — Issue #1413
// round-7 (Codex review). This is the ONLY place a read-only user's Plaid items are ever
// disconnected: TrialService.ExpireTrial no longer calls Plaid at all, because no ordering of a
// status flip and an irreversible Plaid call can be safe — /subscription/checkout is deliberately
// whitelisted for read-only users, so becoming read-only never stops a user from becoming active
// again. Deferring behind a grace window instead of ordering around the race turns "resubscribed
// before disconnect" into a non-event: such a user simply drops out of
// GetUsersNeedingPlaidDisconnectRepair's predicates (status, and — since a resubscribe clears the
// marker — plaid_cleanup_pending_at) before this pass ever reaches them.
//
// w.plaidCleanup being nil no longer fails this function or the job (through round-10 Fix 1 it did,
// via the errPlaidCleanupNotConfigured sentinel) — see the RetentionCleanupCheckWorker.plaidCleanup
// field's own doc comment for why that changed. The self-healing backstop (Stamp/Clear, below) still
// runs unconditionally either way — it is pure SQL with no Plaid dependency — and only the candidate
// query and the per-user disconnect loop are actually skipped when Plaid is unconfigured, logged at
// Warn with observability.PlaidCleanupServiceNotConfiguredTotal incremented so a sustained rate in
// PRODUCTION (where Plaid must always be configured) stays visible and alertable, without failing
// the whole job on every QA/E2E boot the way the old hard-fail did.
//
// round-10 replaced the grace-window anchor (previously retention_started_at, which rounds 8/9
// demonstrated cannot be made sound as a Plaid-disconnect signal: it is written by several
// independent paths, its clearing on resubscribe can fail silently, and it is never cleared once a
// disconnect actually completes, since it also has to keep counting toward the separate 6-month
// retention/deletion clock) with the explicit plaid_cleanup_pending_at marker — see
// GetUsersNeedingPlaidDisconnectRepair's own doc comment (profile_repository.go) for the full
// rationale. This function no longer reads RetentionStartedAt at all; the ordering dependency the
// prior design had on repairRetentionClocks (Step 0) running first no longer applies here, since
// the marker (not retention_started_at) is what the candidate query and the per-item recheck below
// both key on.
//
// Because the query keys on subscription_status + plaid_cleanup_pending_at (not on which flow set
// them), this pass also catches a failed/never-ran disconnect from the cancellation and past-due
// paths, not just trial expiry — a repeat DisconnectAllUserItemsIfEligible call for an
// already-disconnected item is harmless (idempotent: ITEM_NOT_FOUND is mapped to success, and the
// per-item durable 'disconnected' marker skips items already handled by a prior attempt).
func (w *RetentionCleanupCheckWorker) repairPlaidDisconnects(ctx context.Context, jobID int64) error {
	// Issue #1413 round 16: the self-healing backstop runs FIRST, before the w.plaidCleanup nil check
	// and before the candidate query, on every single pass — see StampMissingPlaidCleanupMarkers's own
	// doc comment (profile_repository.go) for the full rationale and the four properties this
	// ordering and shape are chosen to satisfy.
	//
	// Round 17 (Codex review) corrected how a failure here is reported. It still does NOT fail this
	// step or the job — returning an error would skip the 30-day/7-day GDPR pre-deletion notices and
	// deleteExpiredAccounts for EVERY user, and that isolation is deliberate. But the original
	// handling (a Warn, excluded from errorCount and from every metric) was wrong for THIS call
	// specifically, and the distinction is worth stating because it is easy to get backwards:
	//
	// A per-user failure below is self-limiting — that user keeps a non-NULL marker, so the candidate
	// query still selects them next pass and the retry is free. That is why `return nil` is right
	// there. Nothing, however, covers for the backstop: it is the ONLY thing that returns a
	// NULL-marker/live-item user to visibility, and such a user is otherwise invisible to the
	// candidate query forever, meaning their bank connections are never disconnected. A persistently
	// broken backstop is therefore a silent data-retention failure, and it must not be detectable
	// only as an ABSENCE of "healed" log lines while Work() goes on recording a successful heartbeat.
	// Hence: Error (not Warn) plus a dedicated counter to alert on.
	if healedCount, healErr := w.profileRepo.StampMissingPlaidCleanupMarkers(ctx); healErr != nil {
		observability.PlaidCleanupBackstopFailuresTotal.Inc()
		w.logger.Error("plaid_cleanup_pending_at self-healing backstop FAILED — any user with a live linked item and a NULL marker stays invisible to the repair candidate query until a later pass heals them; a sustained failure rate here means their Plaid items are never disconnected",
			"job_id", jobID, "error", healErr)
	} else if healedCount > 0 {
		w.logger.Info("plaid_cleanup_pending_at self-healing backstop stamped missing markers",
			"job_id", jobID, "healed_count", healedCount)
	}

	// Issue #1413 Finding 1 (Codex review, PR #1443 follow-up, headline finding): the symmetric
	// counterpart to the Stamp call just above — see ClearCompletedPlaidCleanupMarkers's own doc
	// comment (profile_repository.go) for the full five-step scenario this closes and why it must run
	// on EVERY pass, not just when a candidate is found. In short: a marker left set after its
	// episode's cleanup already completed (e.g. every clearPlaidCleanupMarkerWithRetry attempt below
	// failing after a successful disconnect) is NOT harmless residue the candidate query's live-item
	// EXISTS clause quietly filters out forever — several comments across this branch said exactly
	// that, and they were wrong. At the time this was written, a comp granted and later removed over
	// that stale marker resurrected it via removeUserTierOverrideUpdate's COALESCE
	// (admin_user_repository.go) instead of starting a fresh episode, so a freshly-relinked bank
	// inherited an already-expired grace window and got disconnected with zero notice. ROUND 18
	// (admin_user_repository.go, removeUserTierOverrideSelectPrior's own doc comment) independently
	// removed that COALESCE — should_mark_plaid_cleanup_pending's CASE now overwrites unconditionally
	// — so THIS specific comp-involving path no longer depends on this method having run first
	// either. This method is kept regardless: it still closes the population that scenario never
	// depended on a comp for at all — a stale marker with zero live items that no comp is ever
	// granted over, which nothing else in this codebase revisits. Running this clear on every pass —
	// same as the Stamp half — is what closes THAT gap within one backstop interval instead of
	// leaving it open indefinitely.
	//
	// Deliberately a SEPARATE non-fatal failure branch, not folded into the Stamp block above: mirrors
	// StampMissingPlaidCleanupMarkers's own handling exactly (a persistently failing clear is also a
	// silent, hard-to-detect drift from the invariant, so it gets the same Error-plus-counter
	// treatment), but the two counts are logged under distinct field names (healed_count vs.
	// cleared_count) so an operator reading these logs can tell which direction of the invariant is
	// misbehaving, not just that "the backstop" is unhappy.
	if clearedCount, clearErr := w.profileRepo.ClearCompletedPlaidCleanupMarkers(ctx); clearErr != nil {
		observability.PlaidCleanupBackstopFailuresTotal.Inc()
		w.logger.Error("plaid_cleanup_pending_at stale-marker backstop FAILED — a marker left set after its episode's cleanup completed will not be cleared, so a user with no live items stays uncorrected indefinitely; see ClearCompletedPlaidCleanupMarkers's doc comment",
			"job_id", jobID, "error", clearErr)
	} else if clearedCount > 0 {
		w.logger.Info("plaid_cleanup_pending_at stale-marker backstop cleared completed-episode markers",
			"job_id", jobID, "cleared_count", clearedCount)
	}

	// Issue #1455: the THIRD backstop, closing the invariant's OTHER branch (status NOT ineligible
	// ⇒ marker must be NULL) — see ClearMarkersOnEligibleStatus's own doc comment
	// (profile_repository.go) for the full rationale, including the comped-user judgement call.
	// Runs immediately after the Clear half above, at the very start of every pass, unconditionally
	// — same non-fatal, Error-plus-counter treatment as its two siblings: this backstop is the ONLY
	// thing that ever repairs a non-ineligible-status row whose marker was wrongly left set, and a
	// persistently failing instance of it is a silent grace-window bypass (a later cancellation's
	// write could inherit the stale marker and skip the 72-hour grace period entirely), not a routine
	// event.
	if branch1ClearedCount, branch1Err := w.profileRepo.ClearMarkersOnEligibleStatus(ctx); branch1Err != nil {
		observability.PlaidCleanupBackstopFailuresTotal.Inc()
		w.logger.Error("plaid_cleanup_pending_at branch-1 backstop FAILED — a marker left set on a non-ineligible-status row stays stale indefinitely, and a later status transition could inherit a marker already past the 72-hour grace window, disconnecting the user's banks with zero notice; see ClearMarkersOnEligibleStatus's doc comment",
			"job_id", jobID, "error", branch1Err)
	} else if branch1ClearedCount > 0 {
		w.logger.Info("plaid_cleanup_pending_at branch-1 backstop cleared markers on non-ineligible-status rows",
			"job_id", jobID, "cleared_count", branch1ClearedCount)
	}

	// A CI finding on this branch's head (fixed alongside Issue #1413 Finding 1, not from the Codex
	// review itself): through round-10 Fix 1, a nil w.plaidCleanup failed this function AND the whole
	// job (errPlaidCleanupNotConfigured) — deliberately, to make a missing Plaid dependency loud
	// rather than a silent "disconnect nobody, report healthy" run. That reasoning stopped holding
	// once RetentionCleanupCheckPeriodicOpts set RunOnStart: true: this job now runs at every process
	// boot in every environment, including QA/E2E, which legitimately never configures Plaid at all —
	// so the hard-fail fired on every correct QA deploy, burying GDPR-mandated retention-clock repair
	// and 30d/7d notices under a repeating ERROR-level failure riding the SAME job toward the dead
	// letter queue. This check now runs AFTER the backstop above (deliberately — Stamp/Clear are pure
	// SQL with no Plaid dependency, so keeping plaid_cleanup_pending_at correct is worth doing even on
	// a pass that cannot act on it) and returns nil so Work() still runs Steps 1-3 (30d/7d notices,
	// expired-account deletion) — mirroring deleteExpiredAccounts's own established pattern for a
	// missing DELETION_TOKEN_SECRET dependency just below in this same file. The signal is not lost:
	// observability.PlaidCleanupServiceNotConfiguredTotal is a SEPARATE counter from
	// PlaidCleanupBackstopFailuresTotal (that one means the backstop's own SQL failed, a narrower and
	// different fact) — a sustained non-zero rate in PRODUCTION, where Plaid must always be
	// configured, is exactly as alertable as the old hard failure was, without taking the rest of this
	// job down in an environment where it is expected.
	if w.plaidCleanup == nil {
		observability.PlaidCleanupServiceNotConfiguredTotal.Inc()
		w.logger.Warn("skipping Plaid-disconnect repair — no Plaid cleanup service configured; retention-clock repair, 30d/7d GDPR notices, and expired-account deletion still ran this pass",
			"job_id", jobID)
		return nil
	}

	users, err := w.profileRepo.GetUsersNeedingPlaidDisconnectRepair(ctx, plaidDisconnectRepairGracePeriod)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing Plaid-disconnect repair: %w", err)
	}
	if len(users) == 0 {
		w.logger.Info("no users needing Plaid-disconnect repair", "job_id", jobID)
		return nil
	}
	w.logger.Info("found users needing Plaid-disconnect repair", "job_id", jobID, "user_count", len(users))

	var repairedCount, skippedCount, errorCount, abortedCount int
	for _, user := range users {
		// Belt-and-braces: a user with a LIVE comp must never have their Plaid items disconnected,
		// even though the candidate query already excludes one via noActiveManualOverridePredicate.
		//
		// Issue #1413 round-12 Fix 2: deliberately HasActiveManualOverride(), NOT HasAnyManualOverride()
		// — unlike repairRetentionClocks's belt-and-braces guard just above (which correctly stays on
		// HasAnyManualOverride(), matching that repair's noManualOverrideAtAllPredicate candidate
		// query), a LAPSED override must NOT skip Plaid-disconnect repair: deletion must fail safe by
		// staying excluded indefinitely if the comp-expiry job stalls (irreversible), but a Plaid
		// disconnect is recoverable (the user relinks), so leaving a lapsed-comp user's bank
		// connection live and billable forever is the wrong default here. See
		// GetUsersNeedingPlaidDisconnectRepair's doc comment (profile_repository.go) for the full
		// reasoning this guard must match.
		if user.HasActiveManualOverride() {
			w.logger.Info("skipping Plaid-disconnect repair for user with a currently-active manual tier override",
				"job_id", jobID, "user_id", user.ID)
			skippedCount++
			continue
		}

		userID := user.ID

		// Belt-and-braces: GetUsersNeedingPlaidDisconnectRepair's own WHERE clause guarantees
		// PlaidCleanupPendingAt IS NOT NULL on every real candidate — this should be impossible in
		// production. Guarded explicitly (rather than dereferencing unconditionally below) so a
		// candidate list that somehow violates its own contract degrades to a logged skip, not a
		// panic.
		if user.PlaidCleanupPendingAt == nil {
			w.logger.Warn("Plaid-disconnect repair candidate missing plaid_cleanup_pending_at — skipping (violates GetUsersNeedingPlaidDisconnectRepair's own predicate)",
				"job_id", jobID, "user_id", userID)
			skippedCount++
			continue
		}

		// Issue #1413 round-11 Fix 3: the marker VALUE this worker selected on, captured ONCE and
		// threaded into both isStillEligible (every per-item recheck) and the final clear call
		// below — the same occurrence-identity pattern already established for work_claims
		// (repository.OccurrenceResourceID / FormatOccurrenceTimestamp) and for past_due_service.go's
		// reReadStillPastDue (which requires past_due_started_at to still match, not merely be
		// non-nil). GetUsersNeedingPlaidDisconnectRepair's WHERE clause guarantees
		// PlaidCleanupPendingAt is non-nil on every candidate it returns.
		expectedMarker := *user.PlaidCleanupPendingAt
		expectedOccurrence := repository.FormatOccurrenceTimestamp(user.PlaidCleanupPendingAt)

		// isStillEligible re-verifies, against the freshest possible read, that userID is still
		// eligible-status, uncomped, AND still on the EXACT ineligibility episode this worker
		// selected — not merely that SOME marker is present. Unlike RepairRetentionClock's
		// conditional UPDATE (which gets this for free from its own WHERE clause), the repair
		// action here — a Plaid /item/remove call — has no atomic DB guard, so a stale candidate
		// who resubscribed (or was comped) since the query ran must be caught explicitly;
		// otherwise this repair pass would disconnect a genuinely-active user's live Plaid items,
		// reproducing the exact harm it exists to fix via a different code path.
		//
		// Issue #1413 round-7: passed as DisconnectAllUserItemsIfEligible's recheck callback, so it
		// is invoked PER ITEM (not just once before the whole call) — a disconnect fans out into
		// one Plaid call per linked item, so a single upfront check would leave every item after
		// the first exposed to a resubscribe happening mid-loop.
		//
		// Issue #1413 round-8 High 4 (Codex review): uses IsEligibleForPlaidDisconnectRepair(), NOT
		// IsReadOnlySubscription() — the latter also returns true for 'past_due', but the repair SQL
		// (GetUsersNeedingPlaidDisconnectRepair) selects only 'inactive'/'cancelled'. Using the wider
		// read-only definition here let an old inactive/cancelled candidate that transitioned into a
		// NEW past_due cycle before this re-read still pass and be disconnected immediately —
		// bypassing past_due's own dunning timeline (past_due_service.go). See
		// domain.SubscriptionStatus.IsEligibleForPlaidDisconnectRepair's doc comment for the full
		// rationale and the parity test that keeps this in agreement with the SQL predicate.
		//
		// Issue #1413 round-11 Fix 3 (replaces the round-10 "PlaidCleanupPendingAt != nil"
		// PRESENCE-only check): the prior check confirmed only that SOME marker was set, not that
		// it was still THIS episode's marker. That let a stale worker act on a completely different
		// (fresher) episode: an old candidate is selected; an admin grants a comp (clearing the
		// marker); the comp is later removed (setting a FRESH marker for the new episode via
		// COALESCE(NULL, NOW())); the stale worker's presence-only recheck then saw a non-nil
		// marker and disconnected immediately, then cleared the NEW episode's marker too — losing
		// its entire grace window. Comparing FormatOccurrenceTimestamp(current.PlaidCleanupPendingAt)
		// against expectedOccurrence closes this: a mismatch (including current being nil, which
		// formats to "") is a normal skip, not an error — the next scheduled run evaluates whatever
		// episode is actually current, on its own merits and its own grace window.
		isStillEligible := func(rctx context.Context) (bool, error) {
			current, err := w.profileRepo.GetUserByID(rctx, userID)
			if err != nil {
				return false, fmt.Errorf("failed to re-read user %s before Plaid-disconnect repair: %w", userID, err)
			}
			if !domain.SubscriptionStatus(current.SubscriptionStatus).IsEligibleForPlaidDisconnectRepair() {
				return false, nil
			}
			// Issue #1413 Task JA (live production defect, PR #1443 round-7 gap analysis): a GDPR
			// deletion request filed in the race window between GetUsersNeedingPlaidDisconnectRepair's
			// candidate selection and this worker's per-item Plaid call must abort the repair, exactly
			// like a comp granted or a resubscribe in that same window does below. Without this check,
			// this candidate would have its bank connections disconnected here even though
			// GetUsersNeedingPlaidDisconnectRepair's own `deletion_requested_at IS NULL` filter
			// (profile_repository.go) would have excluded them outright had the deletion request been
			// on the row before the candidate query ran at all — an inconsistency between what the
			// candidate query would select and what this re-verify allows through. Mirrors
			// CancelDeletion's own fix (profile_repository.go) for the same underlying invariant:
			// deletion_requested_at gates this subsystem everywhere else, and this re-check closes the
			// one place it did not.
			if current.DeletionRequestedAt != nil {
				return false, nil
			}
			// Issue #1413 round-12 Fix 2: HasActiveManualOverride() (live-only), not
			// HasAnyManualOverride() — see the belt-and-braces guard above this loop for the full
			// reasoning. A lapsed override must not abort an in-progress repair; only a comp granted
			// (or still live) since the candidate query ran does.
			if current.HasActiveManualOverride() {
				return false, nil
			}
			if repository.FormatOccurrenceTimestamp(current.PlaidCleanupPendingAt) != expectedOccurrence {
				return false, nil
			}
			return true, nil
		}

		eligible, err := isStillEligible(ctx)
		if err != nil {
			w.logger.Warn("failed to re-read user before Plaid-disconnect repair",
				"job_id", jobID, "user_id", userID, "error", err)
			errorCount++
			// Issue #1413: failure-visibility counter — see its doc comment
			// (observability/metrics.go) for why this, together with
			// PlaidCleanupOldestOutstandingSeconds and this log line's user_id field, is what makes
			// a user stuck in the retry loop observable at all. repairPlaidDisconnects's per-user
			// error isolation (this `continue`, not a `return`) is deliberate and unchanged by this
			// metric — one user's failure must never block the other steps this job run performs.
			observability.PlaidDisconnectRepairUserFailuresTotal.Inc()
			continue
		}
		if !eligible {
			w.logger.Info("Plaid-disconnect repair skipped — user no longer read-only-status, comped, or the marker has moved on to a different episode since the query ran",
				"job_id", jobID, "user_id", userID)
			skippedCount++
			continue
		}

		if disconnectErr := w.plaidCleanup.DisconnectAllUserItemsIfEligible(ctx, userID, isStillEligible); disconnectErr != nil {
			// Issue #1413 round-14 H1 (Codex review): ErrCleanupAborted means the per-item recheck
			// (isStillEligible, passed as DisconnectAllUserItemsIfEligible's recheck callback above)
			// correctly stopped mid-run because this user became ineligible partway through — e.g.
			// an admin granted a comp between items. This is neither success nor failure: some
			// items may already be disconnected (durably marked), but others remain live. It MUST
			// NOT fall through to the marker-clear below — this whole `if disconnectErr != nil`
			// branch (including this one) already `continue`s past it, so the marker is correctly
			// left set for a future run once the comp lapses. Logged at Info (not Warn) and
			// deliberately excluded from PlaidDisconnectRepairUserFailuresTotal, mirroring the
			// ErrSideEffectClaimBusy precedent immediately below — both are benign, expected
			// outcomes, not Plaid failures. See ErrCleanupAborted's doc comment
			// (plaid_item_cleanup_service.go) for the full contract this branch depends on.
			if errors.Is(disconnectErr, services.ErrCleanupAborted) {
				w.logger.Info("Plaid-disconnect repair aborted mid-run — user is no longer eligible (e.g. comped) partway through; some items may remain connected, plaid_cleanup_pending_at left set for a future run",
					"job_id", jobID, "user_id", userID)
				abortedCount++
				continue
			}
			// ErrSideEffectClaimBusy is benign, expected contention (another flow is already
			// mid-disconnect for this user) — logged at Info, not Warn, mirroring
			// past_due_service.go's processGraceExpiry precedent for the same distinction.
			if errors.Is(disconnectErr, services.ErrSideEffectClaimBusy) {
				w.logger.Info("Plaid-disconnect repair deferred — side-effect claim held by another flow",
					"job_id", jobID, "user_id", userID)
				// Deliberately NOT counted against PlaidDisconnectRepairUserFailuresTotal (unlike
				// errorCount just below, which still counts it for the existing log-summary
				// contract): this is benign, expected contention from another flow already
				// mid-disconnect for this same user, not a Plaid failure — it resolves on its own
				// once that flow completes and does not indicate a user stuck in the retry loop.
			} else {
				w.logger.Warn("failed to repair Plaid disconnect",
					"job_id", jobID, "user_id", userID, "error", disconnectErr)
				// Issue #1413: failure-visibility counter — see its doc comment
				// (observability/metrics.go). Per-user error isolation (this `continue`) is
				// deliberate and unchanged by this metric.
				observability.PlaidDisconnectRepairUserFailuresTotal.Inc()
			}
			errorCount++
			continue
		}

		// Issue #1413 round-11 Fix 3: clear the marker now that cleanup has actually completed, but
		// ONLY if it still equals expectedMarker — the exact episode isStillEligible's per-item
		// rechecks just verified through the last successful item. This must NOT be the
		// unconditional ClearPlaidCleanupPending: between the LAST successful per-item recheck
		// above and this call, a comp could still be granted and removed, starting a brand-new
		// episode with its own fresh marker — an unconditional clear here would erase that new
		// episode's grace window entirely, the exact Fix 3 defect one call site later.
		//
		// Issue #1413 Finding 4 (Codex review, PR #1443): a clearErr here is retried up to
		// plaidCleanupMarkerClearMaxAttempts times before being treated as a real failure — it is
		// NOT harmless, and the comment that used to sit here (claiming the user "will harmlessly
		// be re-considered next run") was false: DisconnectAllUserItemsIfEligible immediately above
		// already succeeded, so every one of this user's items is now disconnected, meaning
		// GetUsersNeedingPlaidDisconnectRepair's own live-item EXISTS clause will never match this
		// user again on any future run. A marker left set by a failed clear here is therefore stuck
		// non-NULL permanently, not "harmlessly re-considered." Retrying immediately (a same-process
		// retry of a simple conditional UPDATE, not a remote/rate-limited API call) closes the
		// common case — a transient DB blip — without needing a broader query change; if every
		// attempt still fails, it is counted like any other genuine per-user failure in this loop
		// (errorCount, PlaidDisconnectRepairUserFailuresTotal) so it is at least observable, rather
		// than silently swallowed.
		//
		// A mismatched marker (cleared == false, clearErr == nil) is deliberately NOT retried and
		// NOT counted as a failure — it means the marker legitimately moved on to a fresh episode
		// since this worker started, a normal, stable outcome to be evaluated on its own schedule,
		// not a transient error.
		cleared, clearErr := w.clearPlaidCleanupMarkerWithRetry(ctx, jobID, userID, expectedMarker)
		if clearErr != nil {
			w.logger.Error("Plaid disconnect repaired but failed to clear plaid_cleanup_pending_at after all retries — this user's Plaid items are already disconnected, so GetUsersNeedingPlaidDisconnectRepair's live-item requirement will never match them again; the marker may now be stuck non-NULL permanently unless cleared through another path",
				"job_id", jobID, "user_id", userID, "attempts", plaidCleanupMarkerClearMaxAttempts, "error", clearErr)
			errorCount++
			// Issue #1413 Finding 4: same failure-visibility counter used by every other per-user
			// failure in this loop — see its doc comment (observability/metrics.go), now updated to
			// document this as a third increment site. A persistent clear failure is the highest-risk
			// outcome this loop can produce (a permanently unreachable user, not just a retryable
			// one), so it must not be less visible than a disconnect failure.
			observability.PlaidDisconnectRepairUserFailuresTotal.Inc()
		} else if !cleared {
			w.logger.Info("Plaid disconnect repaired, but plaid_cleanup_pending_at had already moved on to a new episode since this worker started — leaving the new episode's marker untouched for its own future run",
				"job_id", jobID, "user_id", userID)
		}

		w.logger.Info("Plaid disconnect repaired", "job_id", jobID, "user_id", userID)
		repairedCount++
	}

	w.logger.Info("Plaid-disconnect repair processed",
		"job_id", jobID, "repaired", repairedCount, "skipped", skippedCount, "errors", errorCount, "aborted_mid_run", abortedCount)

	// Issue #1413 round 16: this ALWAYS returns nil here, deliberately, regardless of errorCount —
	// a decision made explicitly, not by default. The alternative (returning an error when
	// errorCount > 0) would make Work() propagate it, and River would retry the WHOLE job run —
	// re-running Step 0 (retention-clock repair), Step 1/2 (30d/7d notices), and Step 3 (auto-
	// deletion) alongside this step, every time even ONE user in this loop fails. That directly
	// conflicts with this loop's own per-user error isolation (every `continue` above): the whole
	// point of isolating one user's Plaid failure to a `continue` is that it must never block GDPR
	// deletion notices or auto-deletion for every OTHER user in the same run — propagating an error
	// from a per-user retry loop as a JOB-level failure would undo that isolation one level up,
	// even though this function's own control flow never lets it undo it internally.
	//
	// Does the self-healing backstop (StampMissingPlaidCleanupMarkers plus, as of Issue #1413 Finding
	// 1, its clearing counterpart ClearCompletedPlaidCleanupMarkers — both called at the top of this
	// function) lower the stakes of this decision? Yes, for BOTH directions now, and it is worth being
	// precise about how: Stamp heals a MISSING marker (NULL, while a live item exists); Clear heals a
	// STUCK marker — exactly the ClearPlaidCleanupPendingIfMatches exhaustion case just above, where a
	// disconnect already succeeded but every retry to null the marker afterward still failed. Through
	// round 17, this file's own comment here called a stuck marker in that state "an untidy stuck
	// timestamp, not a repeat billing/access risk" — that was WRONG, and is exactly Finding 1's
	// headline finding: GetUsersNeedingPlaidDisconnectRepair's live-item EXISTS clause does keep this
	// SAME episode from ever being reselected, but nothing stopped a LATER episode from inheriting the
	// stale timestamp — at the time this was found, an admin comp granted and removed over a stuck
	// marker resurrected it via removeUserTierOverrideUpdate's COALESCE (admin_user_repository.go)
	// instead of starting fresh, so a bank the user relinks in the meantime got disconnected
	// immediately, with zero grace, the next time this pass ran. ROUND 18 (admin_user_repository.go)
	// independently removed that COALESCE, closing the comp-involving path on the writer side too —
	// but a stale marker on a user no comp is EVER granted over gets no help from that fix. Clear
	// closes that population: it nulls a stuck marker on the very next pass (see its own doc comment,
	// profile_repository.go, for the full scenario and the #1453 exception it preserves), so this
	// backstop's second gap is bounded to one repair-pass interval too. This does not change the
	// return-nil decision itself —
	// per-user error isolation here is still preserved for the same reasons stated above, and a
	// persistent clear failure is already independently visible via its own Error log line and the
	// shared PlaidCleanupBackstopFailuresTotal counter (see this function's Clear call site) — but it
	// does mean the residual this paragraph used to accept as merely cosmetic is now actively healed,
	// not just tolerated.
	return nil
}

// monitorMalformedTierOverrides is Issue #1453's monitoring pass: reports the current count of
// users whose manual_tier_override does not name a currently-configured tier
// (config/subscription_tiers.yaml).
//
// Why this exists at all: users.manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint — Issue #1453 deliberately did NOT add one, because the tier set lives in
// config/subscription_tiers.yaml, not the database, and inlining it into schema would duplicate
// application config and drift the next time a tier is added or renamed. Migration 000127
// backfilled every row that was ALREADY malformed at the time it ran to NULL, and
// admin_user_service.go's UpdateUserTier validates every NEW write against tierConfig — but neither
// of those closes the class permanently the way a CHECK constraint would have: a legacy migration, a
// direct SQL write, or a write made while tierConfig was nil can all still reintroduce a malformed
// value, invisibly, with no write-time gate to catch it. This pass is the compensating control:
// unlike a one-time backfill, it runs on every scheduled pass, so a reintroduced malformed value is
// visible (MalformedManualTierOverrideCount gauge, plus an Error log line naming the affected user
// IDs) within one repair-pass interval, not left to be discovered whenever someone happens to query
// for it by hand.
//
// Deliberately non-fatal and does not fail the job, matching every other step in this function:
// this is an observability check, never a repair (it makes no write), so a failure here must not
// block the 30-day/7-day GDPR pre-deletion notices or deleteExpiredAccounts for every user.
//
// Deliberately a GAUGE (MalformedManualTierOverrideCount), not a counter: this reports how many
// rows are malformed RIGHT NOW, each pass — it is not a cumulative count of every malformed row ever
// observed across every pass, which would only ever grow and would misrepresent a single persistent
// malformed row as "many failures" the longer it goes unnoticed.
func (w *RetentionCleanupCheckWorker) monitorMalformedTierOverrides(ctx context.Context, jobID int64) error {
	malformed, err := w.profileRepo.GetUsersWithMalformedTierOverride(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for malformed manual_tier_override rows: %w", err)
	}
	if malformed == nil {
		// No validator wired (SetOverrideTierValidator never called on the injected repository) —
		// "unknown", not "zero found". Skip reporting entirely rather than publish a gauge value of
		// 0 that would misrepresent an unconfigured check as a clean bill of health. See
		// GetUsersWithMalformedTierOverride's own doc comment (profile_repository.go) for why this
		// distinction is load-bearing, not pedantic.
		w.logger.Warn("skipping malformed manual_tier_override check — no override tier validator configured on the injected repository",
			"job_id", jobID)
		return nil
	}

	observability.MalformedManualTierOverrideCount.Set(float64(len(malformed)))
	if len(malformed) == 0 {
		return nil
	}

	userIDs := make([]string, len(malformed))
	for i, u := range malformed {
		userIDs[i] = u.ID
	}
	w.logger.Error("found users with a manual_tier_override that does not name a configured tier — this should be impossible after migration 000127; some writer bypassed write-time validation",
		"job_id", jobID, "count", len(malformed), "user_ids", userIDs)
	return nil
}

// monitorOrphanedFKlessRows is Issue #1440 round-3 Finding 5's monitoring pass (Codex review,
// coordinator's #1440 follow-up brief): reports the current count, per table, of rows in
// dead_letter_jobs/idempotency_keys/mcp_audit_log — the three user-scoped tables with NO foreign
// key to users at all — whose user_id/actor_user_id no longer names any real user.
//
// Why this exists at all: DeleteUserCascade's own explicit erasure of these three tables runs
// inside its transaction, immediately before commit (see that method's own "round-3 reordering" doc
// comment, profile_repository.go), but none of them has any FK to users, so the FOR UPDATE lock
// that transaction holds on the users row never blocks a concurrent writer (an in-flight MCP audit
// call, an idempotency write, a River dead-letter insert) from landing in the narrow remaining
// window before commit. That residual cannot be closed by locking — this transaction cannot take a
// lock on a table that has no relationship to users at all — so this pass makes it OBSERVABLE
// instead, on every scheduled run, the same way monitorMalformedTierOverrides above already does for
// a different, structurally similar residual this codebase accepted it cannot fully prevent at
// write time.
//
// Deliberately non-fatal and does not fail the job, matching every other step in this function:
// this is an observability check, never a repair (it makes no write), so a failure here must not
// block the 30-day/7-day GDPR pre-deletion notices or deleteExpiredAccounts for every user.
//
// Deliberately GAUGES (OrphanedFKlessUserScopedRowsCount, labeled by table), not counters — mirrors
// MalformedManualTierOverrideCount's own reasoning: this reports how many rows are orphaned RIGHT
// NOW, each pass, not a cumulative total of every orphan ever observed across every pass.
//
// idempotency_keys is expected to read 0 within 24h of any transient non-zero reading regardless —
// IdempotencyCleanupWorker (jobs/idempotency_cleanup.go) sweeps every row past its own TTL daily,
// independent of this pass entirely — but dead_letter_jobs and mcp_audit_log have no such backstop,
// so a sustained non-zero reading for either requires an operator to act on what this pass surfaces.
func (w *RetentionCleanupCheckWorker) monitorOrphanedFKlessRows(ctx context.Context, jobID int64) error {
	counts, err := w.profileRepo.GetOrphanedFKlessUserScopedRowCounts(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for orphaned FK-less user-scoped rows: %w", err)
	}

	observability.OrphanedFKlessUserScopedRowsCount.WithLabelValues("dead_letter_jobs").Set(float64(counts.DeadLetterJobs))
	observability.OrphanedFKlessUserScopedRowsCount.WithLabelValues("idempotency_keys").Set(float64(counts.IdempotencyKeys))
	observability.OrphanedFKlessUserScopedRowsCount.WithLabelValues("mcp_audit_log").Set(float64(counts.MCPAuditLog))

	if counts.DeadLetterJobs == 0 && counts.IdempotencyKeys == 0 && counts.MCPAuditLog == 0 {
		return nil
	}

	w.logger.Warn("found rows in FK-less user-scoped tables whose user_id/actor_user_id no longer names any real user — residue of a concurrent write racing DeleteUserCascade's own erasure of these tables",
		"job_id", jobID,
		"dead_letter_jobs_orphaned", counts.DeadLetterJobs,
		"idempotency_keys_orphaned", counts.IdempotencyKeys,
		"mcp_audit_log_orphaned", counts.MCPAuditLog,
	)
	return nil
}

// monitorRetentionDeletionsWithheld is Issue #1569 T1's alerting pass (Step 2b) — THE HALF OF THAT
// FIX THAT IS NOT OPTIONAL.
//
// WHAT T1 CHANGED AND WHAT IT COST. apps/web/src/legal/PrivacyPolicy.tsx promises a cancelled user
// notice at 30 days and again at 7 days before their data is destroyed. Nothing enforced it: the
// hard-delete sweep never referenced retention_notice_30d_sent_at / _7d_sent_at, and no EmailService
// is passed to this worker in ANY environment (cmd/server/main.go), so canSendRetentionNotice
// returns false before the work-claim is taken and the markers stay NULL forever. Accounts were
// hard-deleted having been warned zero times. retentionNoticesDeliveredPredicate now gates the sweep
// and the claim — which is the safe direction to fail, and which converts a silent DESTRUCTION into
// a silent RETENTION. This pass is what stops the second one being silent.
//
// ⚠ WHY THE REASON MATTERS MORE THAN THE COUNT. A NULL marker means two unrelated things, and an
// alert on the undifferentiated NULL count would fire on every ordinary cancellation:
//   - the retention clock is still RUNNING, so GetUsersNeedingRetention30dNotice and its 7d sibling
//     can still select this user (both bound their population by retention_expires_at > NOW()). The
//     notice is simply not due yet. Healthy, and normally the largest number here.
//   - the clock has RUN OUT, so those two queries can never select the user again — their bound is
//     the opposite one. Nothing existing will ever send the missing notice, so the gate holds the
//     account indefinitely. That is the alert.
// The SQL half of that split is CountRetentionDeletionsWithheld's; the process half — whether an
// email service is wired at all — is not visible to SQL and is applied here.
//
// REASON PRECEDENCE, deliberate: a nil emailService is a process-wide cause that dominates every
// per-user one, so when it holds, the ENTIRE withheld population is reported under
// RetentionWithheldNoEmailService. Wiring the service is the single remedy for all of them, and
// splitting the number would suggest several. With a service wired, the same population reports as
// RetentionWithheldWindowPassed: the notice queries require retention_expires_at > NOW(), so a
// wired service still cannot reach these users.
//
// ⚠ A THIRD REASON EXISTS AND IS NOT REPORTED SEPARATELY *BY THIS GAUGE*: a withheld user with no
// address on file, which canSendRetentionNotice checks a few lines above on the DECRYPTED value. It
// is folded into whichever of the two labels applies. See CountRetentionDeletionsWithheld's own doc
// comment for why no SQL predicate can single it out (users.email is ciphertext and
// EncryptString("") is still "ENC$"+ct) and why the one column that could — email_blind_idx — is not
// worth a label: migration 000061's UNIQUE index caps that population at one row database-wide.
// Since Issue #1584 the state IS reported, per account rather than per label, by
// GetRetentionDeletionsWithheldPage's HasEmailOnFile — that read decrypts, so neither obstacle
// applies to it. The gauge is unchanged and should stay that way.
//
// ⚠ THE ACCOUNTS UNDER RetentionWithheldWindowPassed ARE A BACKLOG NOTHING DRAINS — but they are no
// longer a backlog nothing can SELECT (Issue #1584 closed that half, and an earlier version of this
// note, a TODO(#1569 T9 / email work), said otherwise). GetRetentionDeletionsWithheldPage
// enumerates them and GET /api/v1/admin/retention/withheld publishes the list beside this count.
// What is still missing is a DECISION, not a query: wiring an email service is necessary but not
// sufficient, because GetUsersNeedingRetention30dNotice and its 7d sibling both require
// retention_expires_at > NOW() and these users are past that, so whoever implements the notice
// email must also choose for this population between sending late against a re-armed clock,
// recording an explicit waiver and permitting deletion, and notifying out of band. That choice is
// parked until after go-live with beta. Until it is made they are retained indefinitely, counted
// here and enumerable there.
//
// ⚠ EVERY REASON LABEL MUST BE Set ON EVERY PASS. The enumeration below is by hand and the compiler
// will not say so — a GaugeVec label that is never Set does not exist in the scrape, so an omitted
// reason does not fail loudly: its accounts simply vanish and the gauge stops summing to the
// population. Same hazard, and the same warning, as monitorOrphanedPlaidItems's admin_state loop.
//
// NOTHING IS PUBLISHED ON A FAILED READ. Setting the gauges to 0 would tell an alert "no account is
// being withheld", which is a claim nothing checked — the same unknown-is-not-zero rule Steps 0c and
// 0e already apply. The gauges keep their last reading, and staleness/absent() plus the Error log
// the caller writes are what report the breakage.
//
// Deliberately GAUGES reset to 0 on a clean pass: an operator who wires the email service and lets
// the backlog drain must see the alert CLEAR. A counter, or a gauge only ever written when non-zero,
// would latch forever.
func (w *RetentionCleanupCheckWorker) monitorRetentionDeletionsWithheld(ctx context.Context, jobID int64) error {
	counts, err := w.profileRepo.CountRetentionDeletionsWithheld(ctx)
	if err != nil {
		return fmt.Errorf("failed to count retention hard-deletions withheld for want of a notice: %w", err)
	}

	noEmailService, windowPassed := 0, 0
	if w.emailService == nil {
		noEmailService = counts.WithheldNoticeMissing
	} else {
		windowPassed = counts.WithheldNoticeMissing
	}

	observability.RetentionDeletionsWithheld.
		WithLabelValues(observability.RetentionWithheldNoticePending).Set(float64(counts.NoticePending))
	observability.RetentionDeletionsWithheld.
		WithLabelValues(observability.RetentionWithheldNoEmailService).Set(float64(noEmailService))
	observability.RetentionDeletionsWithheld.
		WithLabelValues(observability.RetentionWithheldWindowPassed).Set(float64(windowPassed))

	if counts.WithheldNoticeMissing == 0 {
		return nil
	}

	// Logged as well as gauged, because a gauge nobody has wired an alert to is still invisible, and
	// this repo has no alert-rule file (see observability.RetentionDeletionsWithheld). No user id or
	// address is logged: the population is identifiable from the database by an operator who needs
	// it, and a log line is not an authenticated surface.
	w.logger.Warn("retention hard-deletions are being WITHHELD because the promised 30d/7d pre-deletion notices were never recorded — these accounts are retained past their stated window and no existing path can send the missing notice",
		"job_id", jobID,
		"withheld_total", counts.WithheldNoticeMissing,
		"withheld_no_email_service", noEmailService,
		"withheld_window_passed", windowPassed,
		"notice_pending_healthy", counts.NoticePending,
	)
	return nil
}

// monitorOrphanedPlaidItems is Issue #1497 item 4's monitoring pass — THE ITEM #1497'S OWN TITLE
// NAMES ("alerting on repeated re-attempts"), which two earlier passes over that ticket both missed.
//
// WHAT WAS MISSING. plaid_orphaned_items.retry_attempt_count is incremented by
// plaidOrphanedItemRepository.RecordRetryFailure on every failed automated removal, and until this
// pass it was read by NO METRIC ANYWHERE: jobs/plaid_orphan_retry.go emits only the generic
// JobsCompleted / JobDuration every River worker emits, both of which report a HEALTHY sweep for a
// run in which every single Item failed. So an erased user's Plaid Item could be re-attempted daily
// for a month, or sit needing a human indefinitely, with nothing anywhere reporting either fact.
//
// ⚠ WHY THIS PASS LIVES HERE AND NOT IN THE RETRY SWEEP, which is where the ticket's own wording
// points. It is forced, not chosen: the manual-intervention population is INVISIBLE to that sweep by
// construction. GetRetryablePage selects only rows holding a live, unexpired credential, which is
// exactly the COMPLEMENT of "nothing automated will finish this" — a gauge published from the sweep
// could never read anything but zero for the number that matters most. The whole-table read this
// needs is repository.List, and this worker is already where this codebase publishes DB-derived
// monitoring gauges (Step 0c monitorMalformedTierOverrides, Step 0d monitorOrphanedFKlessRows). Its
// cadence also matches the retry sweep's own daily cadence, so a gauge over attempt counts is
// refreshed exactly as often as those counts can change. This pass touches neither sweep, neither
// deadline, and makes no write.
//
// ⚠ THE DERIVATION IS SHARED, NOT DUPLICATED. domain.SummarisePlaidOrphanedItems is the same
// function SystemHealthService.getPlaidOrphanedItemMetrics calls for /admin/health's JSON. Two
// derivations of one state is how a dashboard and an alert come to disagree — and see that function
// for why the state cannot be a SQL COUNT at all (one of AdminState's checks depends on whether
// THIS PROCESS can decrypt the ciphertext, which no predicate over the table can see).
//
// ⚠ NOTHING IS PUBLISHED ON A FAILED OR TRUNCATED READ. Setting the gauges to 0 in either case would
// tell an alert "no erased user's Plaid Item needs a human", which is a claim nothing checked — the
// Prometheus-side twin of domain.PlaidOrphanedItemMetrics.Available, and the same "unknown != zero"
// rule Step 0c already applies to its own unconfigured case. The gauges keep their last reading, so
// staleness/absent() plus the Error log are what report the breakage. See
// observability.PlaidOrphanedItemsByAdminState for the intended alert rules, which live in prose
// because this repo has no alert-rule file to put them in.
//
// Deliberately GAUGES, not counters, and deliberately reset to 0 on a clean table: an operator who
// actually removes every outstanding Item must see the alert CLEAR. A counter, or a gauge only ever
// written when non-zero, would latch forever — this branch's signature defect (a mark that outlives
// the work) in alerting form.
func (w *RetentionCleanupCheckWorker) monitorOrphanedPlaidItems(ctx context.Context, jobID int64) error {
	if w.plaidOrphanedItemRepo == nil {
		// Unwired, not clean. Publishing zeroes here would be a green reading produced by the absence
		// of a check — exactly what Step 0c refuses for its own unconfigured case.
		w.logger.Warn("skipping the orphaned-Plaid-Item monitoring pass — no plaid_orphaned_items repository is wired, so the state and re-attempt gauges are left unpublished rather than reported as zero",
			"job_id", jobID)
		return nil
	}

	items, total, err := w.plaidOrphanedItemRepo.List(ctx, domain.PlaidOrphanedItemHealthScanCap, 0)
	if err != nil {
		return fmt.Errorf("failed to read plaid_orphaned_items for the state/re-attempt gauges: %w", err)
	}
	// Bounded by the SAME cap check the /admin/health derivation uses (see
	// domain.SummarisePlaidOrphanedItemsBounded). Past the cap nothing is published at all: a count
	// over the first 500 of N rows would UNDERSTATE the manual-intervention population, which is the
	// one direction this gauge must never err in.
	summary, err := domain.SummarisePlaidOrphanedItemsBounded(items, total, time.Now().UTC())
	if err != nil {
		return err
	}

	observability.PlaidOrphanedItemsByAdminState.
		WithLabelValues(string(domain.OrphanedItemAutoRetrying)).Set(float64(summary.AutoRetrying))
	observability.PlaidOrphanedItemsByAdminState.
		WithLabelValues(string(domain.OrphanedItemManualInterventionRequired)).Set(float64(summary.ManualInterventionRequired))
	observability.PlaidOrphanedItemsByAdminState.
		WithLabelValues(string(domain.OrphanedItemResolved)).Set(float64(summary.Resolved))
	// ⚠ EVERY domain.OrphanedItemAdminState MUST BE SET HERE, and the compiler will not say so — this
	// enumeration is by hand, and a GaugeVec label that is never Set simply does not exist in the
	// scrape. A state omitted here therefore does not fail loudly: its rows vanish, and these gauges
	// silently stop summing to the population. operator_attested (Issue #1497 item 2) is the fourth.
	observability.PlaidOrphanedItemsByAdminState.
		WithLabelValues(string(domain.OrphanedItemOperatorAttested)).Set(float64(summary.OperatorAttested))
	// .WithLabelValues() with no labels: an observability.UnmeasuredGauge, absent from the scrape
	// until this line runs, so a process that has not yet completed a pass reports nothing rather than
	// a fabricated 0. See its declaration for why this gauge in particular needed it.
	observability.PlaidOrphanedItemMaxRetryAttempts.WithLabelValues().Set(float64(summary.MaxUnresolvedRetryAttemptCount))

	if summary.ManualInterventionRequired == 0 {
		return nil
	}

	// Logged as well as gauged, because a gauge nobody has wired an alert to is still invisible —
	// and this repo has no alert-rule file yet (see observability.PlaidOrphanedItemsByAdminState).
	// plaid_item_id is deliberately NOT logged: it is the identifier an operator needs, and the admin
	// view at /api/v1/admin/plaid-orphaned-items is the authenticated surface that carries it. A log
	// line is not that surface.
	w.logger.Warn("Plaid Items stranded by a GDPR erasure need MANUAL removal in Plaid's dashboard — nothing automated will finish these, and each one may still be billing",
		"job_id", jobID,
		"manual_intervention_required", summary.ManualInterventionRequired,
		"auto_retrying", summary.AutoRetrying,
		"resolved", summary.Resolved,
		"operator_attested", summary.OperatorAttested,
		"max_unresolved_retry_attempts", summary.MaxUnresolvedRetryAttemptCount,
	)
	return nil
}

// clearPlaidCleanupMarkerWithRetry retries ClearPlaidCleanupPendingIfMatches up to
// plaidCleanupMarkerClearMaxAttempts times with exponential backoff (plaidCleanupMarkerClearBackoff)
// between attempts — see plaidCleanupMarkerClearMaxAttempts/plaidCleanupMarkerClearRetryDelay's doc
// comments for why a slower back-off (not the original Finding 4 fix's fixed 50ms) is warranted.
//
// Issue #1413 round 16 (Retry robustness, Codex review round on PR #1443): the wait between
// attempts is now CONTEXT-AWARE — a plain time.Sleep cannot be interrupted, so a worker shutdown
// (ctx canceled) during this loop used to block for the full remaining backoff regardless. Using
// time.NewTimer + select on ctx.Done() means a shutdown aborts the retry loop immediately instead of
// waiting it out; the LAST error observed is still returned (not ctx.Err()) so the caller's existing
// "all retries exhausted" logging and PlaidDisconnectRepairUserFailuresTotal accounting is unchanged
// regardless of whether the loop ran out of attempts or was interrupted by shutdown.
func (w *RetentionCleanupCheckWorker) clearPlaidCleanupMarkerWithRetry(ctx context.Context, jobID int64, userID string, expectedMarker time.Time) (bool, error) {
	var cleared bool
	var clearErr error
	for attempt := 1; attempt <= plaidCleanupMarkerClearMaxAttempts; attempt++ {
		cleared, clearErr = w.profileRepo.ClearPlaidCleanupPendingIfMatches(ctx, userID, expectedMarker)
		if clearErr == nil {
			return cleared, nil
		}
		if attempt == plaidCleanupMarkerClearMaxAttempts {
			break
		}
		delay := plaidCleanupMarkerClearBackoff(attempt)
		w.logger.Warn("failed to clear plaid_cleanup_pending_at — retrying",
			"job_id", jobID, "user_id", userID, "attempt", attempt, "delay_ms", delay.Milliseconds(), "error", clearErr)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			w.logger.Warn("plaid_cleanup_pending_at clear retry loop aborted — context done (shutdown)",
				"job_id", jobID, "user_id", userID, "attempt", attempt, "ctx_error", ctx.Err())
			return false, clearErr
		}
	}
	return cleared, clearErr
}

// deleteExpiredAccounts finds users with expired retention and executes deletion.
//
// Issue #1413 round-8 Blocker 2 (Codex review): RetentionCleanupCheckWorker is now constructed
// unconditionally in main.go (no longer gated on cfg.DeletionTokenSecret != ""), so w.deletionService
// can genuinely be nil here — DELETION_TOKEN_SECRET is a hard-deletion-specific secret (used only by
// DeletionService.RequestDeletion's cancellation-link JWT) that has nothing to do with Steps 0/0b/1/2
// above (retention-clock repair, Plaid-disconnect repair, 30d/7d notices), none of which touch
// w.deletionService at all. Calling a method on a nil interface panics rather than erroring, so this
// guard is required, not optional — it degrades this ONE step (auto-deletion) gracefully while
// leaving the load-bearing Plaid-disconnect repair pass completely unaffected.
func (w *RetentionCleanupCheckWorker) deleteExpiredAccounts(ctx context.Context, jobID int64) error {
	if w.deletionService == nil {
		w.logger.Warn("skipping expired-account auto-deletion — no deletion service configured (DELETION_TOKEN_SECRET unset); retention-clock repair and Plaid-disconnect repair still ran this pass",
			"job_id", jobID)
		return nil
	}

	// Issue #1477: resume where the last CAPPED run stopped, instead of restarting from the oldest
	// expired row every time. See sweep_resume.go and repository/sweep_cursor_repository.go for the
	// design, the two reset rules, and the starvation guarantee stated with its bound.
	resume := newSweepResume(ctx, w.sweepCursors, repository.SweepCursorExpiredRetention, w.logger, jobID)
	cursor := retentionCursorFrom(resume.start)
	var reached repository.SweepPosition
	cappedStop := false

	var totalUsers, deletedCount, errorCount int

	for {
		users, err := w.profileRepo.GetUsersWithExpiredRetentionPage(ctx, cursor, w.expiredRetentionPageSize)
		if err != nil {
			return fmt.Errorf("failed to fetch users with expired retention: %w", err)
		}

		if len(users) == 0 {
			// An empty page at a RESUMED position means the persisted cursor is past the end, not
			// that there is no work: clear it and re-query from the beginning within this same run.
			if resume.WrapNow(ctx, totalUsers) {
				cursor = nil
				continue
			}
			break
		}

		// fetchedCount is the page length AS RETURNED by the repository, before the cap truncation
		// below — the only reliable signal for whether the repository itself is exhausted
		// (fetchedCount < pageSize means it had nothing left to give, regardless of the cap).
		fetchedCount := len(users)

		// maxUsersPerRetentionSweep: truncate the page now if it would push totalUsers to or past
		// the cap, so the loop below never hard-deletes more than the cap allows — "at most
		// maxUsersPerRetentionSweep deletions", not "cap-or-more".
		if remaining := w.maxUsersPerRetentionSweep - totalUsers; fetchedCount >= remaining {
			// remaining <= 0 is unreachable given the break at the bottom of this loop, but
			// users[:remaining] would panic on a negative length, so it is guarded explicitly.
			if remaining <= 0 {
				break
			}
			users = users[:remaining]
		}

		totalUsers += len(users)
		w.logger.Info("found users with expired retention", "job_id", jobID, "user_count", len(users))

		lastUser := users[len(users)-1]
		w.deleteExpiredAccountPage(ctx, jobID, users, &deletedCount, &errorCount)

		// The cursor advances past every row this page yielded AFTER the cap truncation above — i.e.
		// past the last row actually processed, not necessarily the last row the repository returned.
		// Those are the same row except on a page the cap truncated, and on that page the loop always
		// breaks below, so the difference is never observable. What matters is that it advances past
		// rows the loop SKIPPED (manual override) or FAILED to delete: that is the whole point of a
		// keyset cursor here — a row that is eligible by the query's predicate but cannot actually be
		// deleted must not occupy the oldest slot of every subsequent page within this run.
		lastExpiresAt := derefRetentionExpiresAt(lastUser)
		cursor = &repository.RetentionSweepCursor{
			ExpiresAt: lastExpiresAt,
			UserID:    lastUser.ID,
		}
		// reached records only the TIMESTAMP half of that position — never lastUser.ID. That is the
		// whole reason sweep_cursors can outlive the GDPR erasure this loop body performs: see
		// repository/sweep_cursor_repository.go's header.
		reached = repository.SweepPosition{TS1: &lastExpiresAt}

		// exhausted is computed from fetchedCount (pre-truncation) so it reflects what the
		// REPOSITORY said, even on a page that also happens to trip the cap.
		exhausted := fetchedCount < w.expiredRetentionPageSize

		// Cap hit: stop THIS run's deletion step — but only WARN when the repository had not
		// already told us it was exhausted. A genuinely short final page that happens to land
		// exactly on the cap has no remainder to defer, and warning about one would be a false
		// alarm (the same boundary case pinned for the scheduled-deletion sweep).
		if totalUsers >= w.maxUsersPerRetentionSweep && !exhausted {
			w.logger.Warn("expired-retention deletion sweep hit its total-processed safety cap; deferring remainder to the next periodic run",
				"job_id", jobID, "processed", totalUsers, "cap", w.maxUsersPerRetentionSweep)
			cappedStop = true
			break
		}

		if exhausted {
			break
		}
	}

	// Persist the remainder position (cap hit) or clear it (end of set reached) — Issue #1477.
	resume.Finish(ctx, cappedStop, reached)

	if totalUsers == 0 {
		w.logger.Info("no users with expired retention", "job_id", jobID)
		return nil
	}

	w.logger.Info("retention deletions processed",
		"job_id", jobID, "deleted", deletedCount, "errors", errorCount)
	if errorCount > 0 {
		return fmt.Errorf("retention deletion: %d of %d deletions failed (see logs for details)", errorCount, totalUsers)
	}
	return nil
}

// deleteExpiredAccountPage runs the per-user hard-delete step for one page of expired-retention
// candidates. Split out of deleteExpiredAccounts (Issue #1478) purely so the pagination/cap
// bookkeeping and the per-user deletion logic stay separately readable; the body is unchanged.
func (w *RetentionCleanupCheckWorker) deleteExpiredAccountPage(ctx context.Context, jobID int64, users []*domain.User, deletedCount, errorCount *int) {
	for _, user := range users {
		// #1413 Critical finding C1 (third round of remediation): belt-and-braces hard-delete
		// guard — a user with ANY manual tier override, live or lapsed-but-uncleared, must never
		// be auto-deleted. This MUST use HasAnyManualOverride (not HasActiveManualOverride) to
		// agree with the retention queries (noManualOverrideAtAllPredicate) and with
		// retentionDeletionClaimPredicate — the retention-kind claim predicate ExecuteDeletion's
		// own claim enforces for the DeletionKindRetention call below — using the
		// live-only check here was exactly the bug this fix closes: a comp that lapsed moments
		// before the independently-scheduled expiry job clears the column would pass this guard
		// and reach ExecuteDeletion. The real enforcement is still ExecuteDeletion's claim; this
		// worker-level guard protects against a stale candidate list when a comp lands (or
		// lapses) between the query and this loop.
		if user.HasAnyManualOverride() {
			w.logger.Info("skipping retention deletion for user with a manual tier override (live or lapsed)",
				"job_id", jobID, "user_id", user.ID)
			continue
		}

		// repository.DeletionKindRetention, never DeletionKindUserRequested: these candidates come
		// from GetUsersWithExpiredRetention, which selects on retention_expires_at and explicitly
		// filters deletion_requested_at IS NULL — nobody asked for these deletions. The retention
		// claim predicate re-verifies exactly that, plus the no-override-at-all rule this
		// belt-and-braces guard above mirrors. Passing the user-requested kind here would refuse
		// every legitimate retention deletion (no request exists to satisfy it) AND, worse, would
		// be authorised for any user who happened to have an elapsed request — bypassing the
		// deliberate separation of the two authorisations.
		if deleteErr := w.deletionService.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention); deleteErr != nil {
			w.logger.Error("failed to execute retention deletion",
				"job_id", jobID, "user_id", user.ID, "error", deleteErr)
			*errorCount++
			continue
		}

		w.logger.Info("retention deletion executed", "job_id", jobID, "user_id", user.ID)
		*deletedCount++
	}
}

// derefRetentionExpiresAt safely reads user.RetentionExpiresAt for cursor advancement.
// GetUsersWithExpiredRetentionPage's WHERE clause guarantees every returned row has a non-nil
// RetentionExpiresAt, but this stays defensive rather than assuming that invariant holds forever
// (e.g. under a future test double that does not enforce it).
//
// What the zero-time fallback actually costs, stated fully: it never makes the sweep MISS a row (the
// cursor's ExpiresAt component only becomes more permissive), but it does stop the cursor ADVANCING —
// with a zero ExpiresAt, the tuple's first element decides every (retention_expires_at, id) > ($1, $2)
// comparison, so the query keeps returning the head page and this run spends its whole
// maxUsersPerRetentionSweep budget re-reading the same rows. That is bare-LIMIT starvation, which is
// the exact failure #1478 exists to remove, so the fallback is a safety net against a panic, NOT a
// tolerable steady state. TestRetentionCleanupCheckWorker_DeleteExpiredAccounts_PagesPastFirstBatch
// and _StopsAtTotalProcessedCap both assert the ExpiresAt component of the cursor this function
// feeds, precisely so a regression here cannot pass silently. Mirrors derefDeletionScheduledAt
// (scheduled_deletion_check.go), which has the same caveat.
func derefRetentionExpiresAt(user *domain.User) time.Time {
	if user.RetentionExpiresAt == nil {
		return time.Time{}
	}
	return *user.RetentionExpiresAt
}

// RetentionCleanupCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate deletion processing.
//
// Issue #1413 Task W (Codex review): ByState now matches River's own default
// (rivertype.UniqueOptsByStateDefault(), river@v0.43.0) exactly: Available, Completed, Pending,
// Retryable, Running, Scheduled. It previously omitted Completed, which — per
// river.UniqueOpts.ByState's doc comment (insert_opts.go) — meant a job that had ALREADY
// completed within the current ByPeriod bucket did not block a duplicate insert. Combined with
// RetentionCleanupCheckPeriodicOpts's RunOnStart: true (a catch-up insert attempted on every
// process restart/leader election), that let this daily job run on every boot on a
// multiple-deploys-per-day cadence instead of once per ByPeriod window.
//
// Deliberately NOT Cancelled or Discarded, even though River documents both as safe to *add*:
// river's canonical default (rivertype.UniqueOptsByStateDefault()) excludes both, and — more
// importantly for this specific job — Discarded means every retry was exhausted without the run
// ever completing its GDPR notice/deletion/repair work. Treating a discarded run as "already
// handled this period" would let that safety-critical work silently stall for up to river's
// DiscardedJobRetentionPeriodDefault (7 days) before another catch-up attempt could get through —
// exactly what RunOnStart exists to prevent. Cancelled is excluded for the same reasoning (and is
// currently unreachable in this codebase regardless: nothing cancels this job kind).
func RetentionCleanupCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 23 hours period — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			queue.JobStateCompleted,
		},
	}
}

// RetentionCleanupCheckInsertOpts returns the *river.InsertOpts this job's periodic registration
// in main.go must use — Queue: "low" plus the uniqueness options from
// RetentionCleanupCheckUniqueOpts(), converted from this package's queue-runner-agnostic
// queue.UniqueOpts to the river.UniqueOpts type river.InsertOpts.UniqueOpts actually requires (the
// same conversion internal/queue/river/client.go's toRiverUniqueOpts performs for the
// general-purpose enqueue path). Mirrors ManualTierOverrideExpiryCheckInsertOpts exactly.
//
// Issue #1413 Finding 2 (Codex review, PR #1443): main.go's periodic-job registration previously
// hand-wrote its own &river.InsertOpts{...} literal — never calling RetentionCleanupCheckUniqueOpts()
// at all — so the round that added Completed to that function's ByState was a no-op in production:
// a completed retention job still failed to block a RunOnStart insertion after every process
// restart or leader election, silently re-running the 30-day/7-day GDPR pre-deletion notices and
// deleteExpiredAccounts on every deploy of a repo that ships several times a day. This is the third
// "defined, tested, never wired" instance on this branch; ManualTierOverrideExpiryCheckInsertOpts
// (Issue #1413 G3) fixed the identical failure mode for the sibling job's own periodic
// registration one entry below this one in main.go. main.go now calls THIS function directly
// instead of hand-writing the InsertOpts literal, so main.go is no longer a second,
// independently-maintained expression of the same shape — a future change to
// RetentionCleanupCheckUniqueOpts() automatically propagates to what actually ships.
func RetentionCleanupCheckInsertOpts() *river.InsertOpts {
	unique := RetentionCleanupCheckUniqueOpts()

	states := make([]rivertype.JobState, len(unique.ByState))
	for i, s := range unique.ByState {
		states[i] = rivertype.JobState(s)
	}

	return &river.InsertOpts{
		Queue: "low",
		UniqueOpts: river.UniqueOpts{
			ByArgs:   unique.ByArgs,
			ByPeriod: unique.ByPeriod,
			ByQueue:  unique.ByQueue,
			ByState:  states,
		},
	}
}

// RetentionCleanupCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use.
//
// Issue #1413 P0: RunOnStart MUST be true. This worker is the only place that runs
// retention-clock repair, the 30-day and 7-day GDPR pre-deletion notices, deleteExpiredAccounts,
// AND the Plaid-disconnect repair pass — and none of it has a manual or admin trigger.
// river.NewPeriodicJob's own doc comment says the periodic scheduler keeps only in-memory
// state: "anytime a process quits or a new leader is elected, the whole process starts over
// without regard for the state of the last scheduler." Concretely (river v0.43.0
// internal/maintenance/periodic_job_enqueuer.go:409-414), nextRunAt is restored from persisted
// state only when PeriodicJobOpts.ID is set — at the time this comment was first written,
// this job (like all 14 in main.go) left ID unset, so nextRunAt was always recomputed as
// ScheduleFunc(now), i.e. now+24h. Issue #1448 fixed that for all 14 (see
// RetentionCleanupCheckPeriodicOpts below). :424-426 skips the catch-up insert entirely when
// RunOnStart is false. Since this repo deploys several times a day, every deploy or leader
// election used to push the target time forward another 24h before it was ever reached:
// RunOnStart: false let this daily job be enqueued zero times, ever.
//
// RunOnStart: true is only safe here because RetentionCleanupCheckUniqueOpts()'s ByPeriod/
// ByState dedup — carried into the InsertOpts this job's periodic registration in main.go
// builds — prevents a start-of-process catch-up run from double-firing alongside an
// already-scheduled run. The two are only safe together: do not flip this to true for a job
// whose InsertOpts lacks an equivalent UniqueOpts.
//
// Issue #1448: ID is now set (Kind()'s own "retention_cleanup_check" string) so River
// restores nextRunAt across restarts/leader elections instead of recomputing now+interval
// every time — this function's own doc history previously said this job "leaves ID unset"
// like all 14 in main.go; that is no longer true for this job, or for the other 13.
func RetentionCleanupCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: RetentionCleanupCheckArgs{}.Kind()}
}
