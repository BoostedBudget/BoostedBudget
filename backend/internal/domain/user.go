package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user account in the system with GDPR-compliant data residency
type User struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Clerk user ID (from JWT sub claim)
	ClerkUserID string `json:"clerk_user_id" db:"clerk_user_id"`

	// Basic user information
	Email         string `json:"email" db:"email"`
	EmailVerified bool   `json:"email_verified" db:"email_verified"`

	// Subscription information
	SubscriptionTier      string     `json:"subscription_tier" db:"subscription_tier"`     // premium, household_2, household_3, household_4, household_5
	SubscriptionStatus    string     `json:"subscription_status" db:"subscription_status"` // active, inactive, trial, cancelled, past_due
	SubscriptionStartedAt *time.Time `json:"subscription_started_at,omitempty" db:"subscription_started_at"`
	SubscriptionEndsAt    *time.Time `json:"subscription_ends_at,omitempty" db:"subscription_ends_at"`

	// Manual tier overrides (Issue #641)
	ManualTierOverride *string    `json:"manual_tier_override,omitempty" db:"manual_tier_override"`
	ManualTierEndsAt   *time.Time `json:"manual_tier_ends_at,omitempty" db:"manual_tier_ends_at"`
	EffectiveTier      string     `json:"effective_tier,omitempty"` // Computed field, not stored in DB

	// Effective status/comp fields (Issue #1413) — computed, not stored in DB.
	// Populated by a later task (see EffectiveTier precedent above).
	EffectiveStatus string     `json:"effective_status,omitempty"`
	AccessResolved  *bool      `json:"access_resolved,omitempty"` // nil only for legacy/unconfigured readers
	AccessSource    string     `json:"access_source,omitempty"`
	IsComped        bool       `json:"is_comped"` // No omitempty: frontend must always see this key, never distinguish false from absent.
	CompTier        string     `json:"comp_tier,omitempty"`
	CompEndsAt      *time.Time `json:"comp_ends_at,omitempty"`

	// Trial information (Issue #679)
	TrialStartedAt     *time.Time `json:"trial_started_at,omitempty" db:"trial_started_at"`
	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty" db:"trial_ends_at"`
	HadTrial           bool       `json:"had_trial" db:"had_trial"`
	TrialDaysRemaining *int       `json:"trial_days_remaining,omitempty"` // Computed field, not stored in DB

	// Paddle customer ID (for payments)
	PaddleCustomerID *string `json:"paddle_customer_id,omitempty" db:"paddle_customer_id"`

	// Paddle subscription ID (for subscription management)
	PaddleSubscriptionID *string `json:"paddle_subscription_id,omitempty" db:"paddle_subscription_id"`

	// GDPR: Primary region for data residency (Fly.io IATA codes)
	PrimaryRegion string `json:"primary_region" db:"primary_region"` // see config/regions.yaml

	// Account status
	IsActive  bool `json:"is_active" db:"is_active"`
	IsDeleted bool `json:"is_deleted" db:"is_deleted"`

	// GDPR: Account deletion (30-day grace period)
	DeletionRequestedAt *time.Time `json:"deletion_requested_at,omitempty" db:"deletion_requested_at"`
	DeletionScheduledAt *time.Time `json:"deletion_scheduled_at,omitempty" db:"deletion_scheduled_at"`

	// GDPR: Retention tracking - 6 months read-only, then auto-delete (Issue #930)
	RetentionStartedAt       *time.Time `json:"retention_started_at,omitempty" db:"retention_started_at"`
	RetentionExpiresAt       *time.Time `json:"retention_expires_at,omitempty" db:"retention_expires_at"`
	RetentionNotice30dSentAt *time.Time `json:"retention_notice_30d_sent_at,omitempty" db:"retention_notice_30d_sent_at"`
	RetentionNotice7dSentAt  *time.Time `json:"retention_notice_7d_sent_at,omitempty" db:"retention_notice_7d_sent_at"`

	// Dunning: tracks when a user entered past_due state (Issue #898)
	PastDueStartedAt *time.Time `json:"past_due_started_at,omitempty" db:"past_due_started_at"`

	// PlaidCleanupPendingAt is the Issue #1413 round-10 explicit marker for
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's grace-window anchor — see
	// GetUsersNeedingPlaidDisconnectRepair's doc comment (profile_repository.go) for why this
	// REPLACES RetentionStartedAt (the derived anchor rounds 8/9 showed could not be made sound).
	//
	// THE INVARIANT (Issue #1413 round 16 — read this before touching ANY writer or clearer of
	// this column):
	//
	//   Define EPISODE precisely, because the rest of this invariant only makes sense against this
	//   definition: an ineligibility episode BEGINS the instant one of two things happens. The
	//   ORDINARY way: subscription_status transitions into a raw status
	//   domain.SubscriptionStatus(subscription_status).IsEligibleForPlaidDisconnectRepair() calls true
	//   ('inactive' or 'cancelled') — every SET site below except one begins an episode this way. The
	//   OTHER way: a user who is ALREADY in such a status, with no episode currently open (their
	//   previous episode's cleanup already completed, so the marker is legitimately NULL), acquires a
	//   live linked item — there is outstanding cleanup again, with no status transition anywhere;
	//   profileRepository.StampMissingPlaidCleanupMarkers (profile_repository.go) is the writer for
	//   this second case, and stamping it is correct, not a stray write. An episode ENDS the instant
	//   Plaid cleanup for THAT SPECIFIC episode completes — i.e. the moment the marker is cleared for
	//   it (ClearPlaidCleanupPending/ClearPlaidCleanupPendingIfMatches). Ending is NOT the same as the
	//   user later leaving the ineligible status: a user can stay 'cancelled' indefinitely after their
	//   episode's cleanup has already finished (no resubscribe required), with no episode open at all
	//   during that time.
	//
	//   Given that definition: plaid_cleanup_pending_at is non-NULL EXACTLY WHEN an episode (as
	//   defined above) is currently OPEN — begun, and not yet ended. It is NULL whenever no episode
	//   is open, which covers TWO distinct, both-legal cases: (a) the raw status is not ineligible at
	//   all (no episode could be open), and (b) the raw status IS still ineligible, but the CURRENT
	//   episode's cleanup has already completed (the episode ended while the status did not change).
	//   Case (b) is exactly what assertPlaidMarkerInvariant
	//   (profile_repository_plaid_marker_invariant_integration_test.go) itself checks directly — a
	//   NULL marker on a still-'cancelled' user with no live linked item is legal, NOT a contradiction
	//   of "non-NULL while an episode is open" — there is no episode open there once cleanup for it
	//   has run.
	//
	//   Its value, whenever non-NULL, records the instant the CURRENT episode became OBSERVABLE to
	//   whichever writer recorded it — never an earlier episode's (a stale write must not resurrect a
	//   value a newer episode already superseded) and never a later one's (a fresh episode must get
	//   its OWN fresh value, not the survivor of a stale COALESCE). For an ordinary status-transition
	//   writer, "observable" and "began" coincide exactly: the transition instant. For a
	//   StampMissingPlaidCleanupMarkers heal they can diverge: the heal's own NOW() may be later than
	//   the moment the racing INSERT actually landed — the backstop cannot know that, only that
	//   outstanding cleanup exists now. The practical consequence, stated at
	//   StampMissingPlaidCleanupMarkers's own doc comment, is deliberate: a healed user's grace window
	//   runs from the heal, so a disconnect can be delayed by up to one extra grace period. Delayed is
	//   recoverable; never is not.
	//
	//   EVENTUALLY consistent, not instantaneously consistent for every writer, every time — say so
	//   plainly rather than leaving it implicit. Round 15 (Finding 3, High, a Codex review round on
	//   PR #1443) found a real race: the override-removal path's live-item check
	//   (liveLinkedItemExistsClause, restored in round 16 — see removeUserTierOverrideSelectPrior's
	//   doc comment, admin_user_repository.go, for the full history) is a snapshot at SELECT time,
	//   and its FOR UPDATE locks the users row, not the linked_items lifecycle — it cannot serialize
	//   against AccountService.LinkAccounts' independent, autocommit INSERT into linked_items. A
	//   linked item that lands moments after that snapshot can leave the marker NULL while a live
	//   item genuinely exists — a real, if narrow, violation of the invariant above for as long as it
	//   persists. Round 16's answer is not to try to close that window by writing more carefully (five
	//   rounds already showed that direction keeps trading one leak for another) but to SELF-HEAL it:
	//   profileRepository.StampMissingPlaidCleanupMarkers (profile_repository.go) runs once at the
	//   start of every repairPlaidDisconnects pass (retention_cleanup_check.go) and atomically stamps
	//   any ineligible, live-item, NULL-marker row it finds. So: this invariant is guaranteed true
	//   again within at most one repair-pass interval of any writer failing to maintain it, not
	//   necessarily true at every single instant for every writer. What must never happen, and what
	//   every writer must still hold on its own, is the harmful direction of case (b) above reversed:
	//   never a NULL marker while a LIVE item exists and no other writer is about to close the gap —
	//   that is precisely the leak the backstop exists to bound, not to license.
	//
	//   A marker with zero live items but a non-empty linked_items history (every item already
	//   disconnected/replaced) is a genuine violation, not a phantom to wave through — cleanup for
	//   that episode has finished, and the marker should have been cleared (see
	//   assertPlaidMarkerInvariant, profile_repository_plaid_marker_invariant_integration_test.go).
	//   The ONE allowed exception is #1453: a user with
	//   ZERO linked_items rows at all (never linked Plaid, or 000119/000120 migration residue) — an
	//   ordinary Plaid-less cancellation and true migration residue are indistinguishable at the row
	//   level, and both are legitimate, reachable states.
	//
	//   Issue #1413 Finding 1 (Codex review, PR #1443 follow-up): until this fix, this genuine
	//   violation was only ever CHECKED (assertPlaidMarkerInvariant), never HEALED — nothing cleared a
	//   marker left set after its episode's cleanup completed, and several comments across this branch
	//   incorrectly described that state as harmless "inert" or "untidy" residue the candidate query's
	//   own live-item EXISTS clause would filter out forever. It is not harmless: a stale marker left
	//   set through a comp grant, a relink, and a comp removal USED TO survive
	//   removeUserTierOverrideUpdate's COALESCE(existing, NOW()) unchanged (admin_user_repository.go)
	//   — that COALESCE was removed by this same fix (ROUND 18 in removeUserTierOverrideSelectPrior's
	//   doc comment, admin_user_repository.go; since further refined by ROUND 19/20 to anchor on the
	//   lapse instant and on wall-clock time respectively — see that comment for the CURRENT write) —
	//   so the NEXT repair pass would see a genuinely fresh live item paired with an already-expired
	//   grace-window timestamp and disconnect it immediately — the exact protection this whole grace
	//   window exists to provide, silently skipped. ClearCompletedPlaidCleanupMarkers
	//   (profile_repository.go) is this violation's healer, symmetric with StampMissingPlaidCleanupMarkers
	//   for the OTHER direction: both run on every repairPlaidDisconnects pass, so this invariant's
	//   second conjunct is now, like the first, guaranteed true again within at most one repair-pass
	//   interval of any writer failing to maintain it — never true permanently, by design, for the
	//   same reason stated above.
	//
	//   Deliberately NOT part of this invariant: manual_tier_override. Whether the user carries a
	//   currently-active admin comp is a function of WALL-CLOCK TIME (HasActiveManualOverride()
	//   compares manual_tier_ends_at against time.Now() on every read), not of any write — an
	//   override can transition from live to lapsed with NO writer ever running. Round-11 through
	//   round-12 tied the marker's SET-time value to "is there a live override right now", which
	//   silently required some LATER writer (the comp-expiry job, or an admin clicking Remove
	//   Override) to re-stamp the marker the instant the override lapsed — and if that later
	//   writer never runs (a degraded/dead-lettered expiry job, or an admin who never clicks),
	//   the marker stays wrong FOREVER, with no writer at fault: the invariant's own truth value
	//   changed on the clock, not on a write, so no write-time trigger could have kept it correct.
	//   That is the round-13 root-cause finding: comping an already inactive/cancelled user (a
	//   plausible real action — comping a churned account) nulled the marker on grant, the comp
	//   later lapsed with no write, and the user's live Plaid connections became permanently
	//   invisible to every query in the codebase.
	//
	//   The fix is to keep the time-varying term OUT of this column entirely: the marker records
	//   only the durable fact "an ineligibility episode began, cleanup outstanding" — a function of
	//   raw subscription_status and of whether live linked items exist, and of nothing else. (Before
	//   round 17 this said "a pure function of raw subscription_status transitions", which was the
	//   same inaccuracy corrected above: the linked-item lifecycle can open an episode too. What
	//   matters, and what has been true since round 13, is that the override is not a term.)
	//   A comp grant does not touch raw
	//   subscription_status, so it must not touch this column either; a comp lapsing is a
	//   read-time fact with nothing to write. CURRENT eligibility (raw status AND whether a
	//   manual override is presently live) is evaluated separately, entirely at query time, by
	//   GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate — never baked
	//   into this column's value.
	//
	// Every writer must leave the column in a state satisfying this invariant when it returns —
	// not merely "correct for the one transition this writer handles in isolation". That
	// isolated-reasoning failure mode is exactly how several prior remediation rounds on this
	// branch each closed the previous reviewer's finding while introducing a new one.
	//
	// Round 17 (Codex review): reconcile that obligation with the "EVENTUALLY consistent" statement
	// above, which read as a contradiction. Both hold, over different scopes, and the distinction is
	// the whole design:
	//
	//   - Every writer is FULLY obliged for the state it can observe and serialize against. "The
	//     backstop will catch it" is NOT a licence to write carelessly — the backstop bounds the leak,
	//     it does not sanction one. A writer that leaves a NULL marker on a user it could see had live
	//     items is a defect, and assertPlaidMarkerInvariant is there to fail on exactly that.
	//   - The system as a whole is eventually consistent for the ONE gap no writer can close: a
	//     concurrent linked_items INSERT that lands after a writer's snapshot. Five rounds proved that
	//     window cannot be closed from inside these writers, so the backstop closes it from outside,
	//     within one repair-pass interval.
	//
	// In short: writers owe correctness; the backstop owes recovery. Neither excuses the other. The
	// known SET sites are: UpdateSubscriptionFromPaddle, CancelIfPastDueOccurrenceWithRetention,
	// ExpireTrialIfNoOverride, UpdateTrialStatus (all profile_repository.go — this last one's own
	// eligible-status branch COALESCEs a fresh value in exactly like its siblings; see its doc
	// comment for the "granting a trial to a cancelled user with an outstanding marker" case this
	// closes), AdminUserRepository's
	// removeUserTierOverrideUpdate (admin_user_repository.go, shared by RemoveUserTierOverride/
	// RemoveUserTierOverrideIfMatches), profileRepository.StampMissingPlaidCleanupMarkers
	// (profile_repository.go) — the one exception to "begins on a status transition" in the episode
	// definition above, since it begins an episode via a live-item insertion instead — and the three
	// one-time migration backfills that predate any of the above ever having run over their target
	// rows: 000119_add_plaid_cleanup_pending_at, 000120_fix_plaid_cleanup_pending_at_backfill, and
	// 000121_backfill_plaid_cleanup_pending_at_excluded_overrides (migrations/). See each migration's
	// own doc comment for why it exists and what population it targets; none of the three runs again
	// after deploy, so each is a SET site only for the historical rows it backfilled at deploy time.
	// Round 14 -> 15 -> 16 on this last site's should_mark_plaid_cleanup_pending CASE — read all
	// three in order; the third is the one currently in force (see removeUserTierOverrideSelectPrior's
	// own doc comment, admin_user_repository.go, for the full history this summarises): round 14
	// gated the CASE on liveLinkedItemExistsClause, because firing for ANY prior override on an
	// inactive/cancelled user re-armed a phantom marker for an already-ended episode (cancel ->
	// repair completes and clears -> comp granted over the cleaned-up user -> comp removed while raw
	// status is still ineligible). Round 15 found that gate itself raced a concurrent LinkAccounts
	// INSERT into a worse bug at the time (a permanent leak, not a phantom) and removed the gate
	// again, restoring the unconditional stamp — but that traded round 14's race for round 14's own
	// phantom-marker bug, on purpose, and forced this invariant's own wording to be weakened to
	// match. Round 16 restored round 14's gate (it is back in should_mark_plaid_cleanup_pending)
	// because profileRepository.StampMissingPlaidCleanupMarkers (profile_repository.go) now
	// self-heals round 15's exact race on the very next repair pass instead of leaking forever — see
	// the "EVENTUALLY consistent" paragraph above. The stamp still serves its original purpose (a
	// user whose raw status was already ineligible before ever gaining a marker, e.g. an
	// imported/backfilled row) and, at round 16, was a no-op whenever the marker was already set
	// (its own WHERE clause requires plaid_cleanup_pending_at IS NULL, so it never overwrites an
	// existing value — this was never a COALESCE in this statement, unlike removeUserTierOverrideUpdate's
	// CASE discussed above). The two round-14 M1 regression tests are back to their original names
	// and Nil assertions (admin_user_repository_remove_tier_override_integration_test.go).
	// assertPlaidMarkerInvariant was restored to a strict form at round 16
	// (profile_repository_plaid_marker_invariant_integration_test.go) — see each one's own comment —
	// but is NOT strict in every respect today: Issue #1413 Finding 3 (Codex review, PR #1443
	// follow-up) widened it once more, to also treat a NULL marker as legal while the user currently
	// has an ACTIVE manual override — StampMissingPlaidCleanupMarkers deliberately excludes an
	// actively-comped user from stamping (noActiveManualOverridePredicate, profile_repository.go), so
	// a NULL marker next to a live linked item is a real, reachable, and intentional state whenever a
	// live comp is the reason nothing has stamped it yet, not a defect. Every other strict check the
	// helper made at round 16 is unchanged; see the helper's own doc comment for the precise, single
	// case widened.
	//
	// UpdateSubscriptionStatus was REMOVED (Issue #1413 round-5 gap G6, PR #1443 gap analysis;
	// formerly documented here as round-14 finding L1's "not a SET/CLEAR site"). It wrote
	// subscription_status directly without ever touching this column, and `rg` confirmed zero
	// production callers, so it was deleted rather than left as a live trap. The durable reason
	// this matters is NOT the method's name, it's the invariant gap it exposed: until Issue #1455,
	// both self-healing backstops (StampMissingPlaidCleanupMarkers, ClearCompletedPlaidCleanupMarkers)
	// were gated on an ineligible-status predicate, so this invariant's FIRST branch — status not
	// ineligible ⇒ marker must be NULL — had NO backstop at all. Issue #1455's
	// ClearMarkersOnEligibleStatus (profile_repository.go) closes that gap: a THIRD self-healing
	// backstop, called from repairPlaidDisconnects (retention_cleanup_check.go, internal/queue/jobs)
	// alongside the other two, that clears any non-NULL marker it finds on a row whose raw status is
	// not inactive/cancelled — see that method's own doc comment for the full reasoning. Any future
	// status-only writer (one that sets subscription_status without also transitioning this column,
	// the same shape UpdateSubscriptionStatus had) still MUST maintain plaid_cleanup_pending_at
	// itself — round 17's framing above applies here exactly as it does to the other two backstops:
	// "the backstop will catch it eventually" bounds how long a violation can persist, it does not
	// excuse a writer from getting this right in the first place. The known CLEAR sites are:
	// UpdateSubscriptionFromPaddle, ClearRetentionDates,
	// ClearPlaidCleanupPending/ClearPlaidCleanupPendingIfMatches, UpdateTrialStatus,
	// ClearCompletedPlaidCleanupMarkers, and ClearMarkersOnEligibleStatus (all profile_repository.go).
	// AdminUserRepository's
	// updateUserTierQuery (comp grant/extend)
	// deliberately does NOT touch this column at all as of round-13 — see its own doc comment.
	// See each site's own doc comment for its justification against this invariant, and
	// GetUsersNeedingPlaidDisconnectRepair's doc comment for the query whose correctness depends
	// entirely on it holding.
	PlaidCleanupPendingAt *time.Time `json:"plaid_cleanup_pending_at,omitempty" db:"plaid_cleanup_pending_at"`

	// Timezone settings (Issue #972)
	// Timezone stores the user's IANA timezone string (default "UTC").
	// TimezoneSource tracks how it was set: "default", "auto" (browser-detected), or "user" (explicit choice).
	Timezone       string `json:"timezone" db:"timezone"`
	TimezoneSource string `json:"timezone_source" db:"timezone_source"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// UpdateProfileRequest represents the request body for updating a user profile
// Only allows updating specific fields (email, primary_region, timezone, timezone_source)
// Subscription fields are managed separately through payment webhooks
type UpdateProfileRequest struct {
	Email          *string `json:"email,omitempty"`
	PrimaryRegion  *string `json:"primary_region,omitempty"`
	Timezone       *string `json:"timezone,omitempty"`
	TimezoneSource *string `json:"timezone_source,omitempty"`
}

// ComputeTrialDaysRemaining calculates and sets the TrialDaysRemaining field
// Returns the number of days remaining in trial, or nil if not on trial
func (u *User) ComputeTrialDaysRemaining() {
	if u.SubscriptionStatus != "trial" || u.TrialEndsAt == nil {
		u.TrialDaysRemaining = nil
		return
	}

	daysRemaining := int(time.Until(*u.TrialEndsAt).Hours() / 24)
	if daysRemaining < 0 {
		daysRemaining = 0
	}
	u.TrialDaysRemaining = &daysRemaining
}

// hasActiveManualOverrideAt reports whether a manual tier override is set and has not expired
// as of the given timestamp. Extracted, character-for-character, from the original
// ComputeEffectiveTier condition, so that ComputeEffectiveTier and ComputeEffectiveStatus share
// a single predicate and can never disagree about whether an override is currently active.
func (u *User) hasActiveManualOverrideAt(now time.Time) bool {
	if u.ManualTierOverride != nil && *u.ManualTierOverride != "" {
		if u.ManualTierEndsAt == nil || !u.ManualTierEndsAt.Before(now) {
			return true
		}
	}
	return false
}

// HasActiveManualOverride reports whether the user currently has a non-expired manual tier
// override in effect, evaluated against the current time.
func (u *User) HasActiveManualOverride() bool {
	return u.hasActiveManualOverrideAt(time.Now())
}

// HasAnyManualOverride reports whether the user carries a manual tier override AT ALL — live or
// already lapsed. Unlike HasActiveManualOverride, expiry is irrelevant here: this is the Go twin
// of the SQL noManualOverrideAtAllPredicate (profile_repository.go), used by belt-and-braces
// guards that must agree with the retention queries' "exclude on any override, live or lapsed"
// rule (Issue #1413 Critical finding C1). A comp that lapsed but has not yet been cleared by the
// daily manual_tier_override_expiry_check job must still block auto-deletion, not just a
// currently-active one — using HasActiveManualOverride for that guard was the C1 bug.
func (u *User) HasAnyManualOverride() bool {
	return u.ManualTierOverride != nil && *u.ManualTierOverride != ""
}

// computeEffectiveTierAt returns the effective tier for the user as of the given timestamp.
// If an active manual override is in effect at that instant, its tier is returned; otherwise
// the base SubscriptionTier is returned.
func (u *User) computeEffectiveTierAt(now time.Time) string {
	if u.hasActiveManualOverrideAt(now) {
		return *u.ManualTierOverride
	}
	return u.SubscriptionTier
}

// ComputeEffectiveTier returns the effective tier for the user.
// If a non-expired manual override is set, that tier is returned.
// Otherwise the base SubscriptionTier is returned.
func (u *User) ComputeEffectiveTier() string {
	return u.computeEffectiveTierAt(time.Now())
}

// computeEffectiveStatusAt returns the effective subscription status for the user as of the
// given timestamp. An active manual override forces the status to active — an admin "comp"
// must actually unlock access, not just relabel the tier while every access gate keeps reading
// the stale, untouched SubscriptionStatus (Issue #1413).
func (u *User) computeEffectiveStatusAt(now time.Time) string {
	if u.hasActiveManualOverrideAt(now) {
		return string(SubscriptionStatusActive)
	}
	return u.SubscriptionStatus
}

// ComputeEffectiveStatus returns the effective subscription status for the user.
// If an active manual tier override is set, the status is forced to active (Issue #1413).
// Otherwise the base SubscriptionStatus is returned unchanged.
func (u *User) ComputeEffectiveStatus() string {
	return u.computeEffectiveStatusAt(time.Now())
}

// EffectiveStatusWithActiveOverride returns the same value ComputeEffectiveStatus() would,
// plus the raw manual_tier_override string when (and only when) a currently-active override is
// what produced that status.
//
// Added for Issue #1413 G2: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so its value can be garbage (a legacy row, a direct SQL write, a write made while
// the tier config was nil). A caller on the access path — e.g.
// GetEffectiveSubscriptionStatusByUserID (backend/internal/repository/profile_repository.go) —
// needs to validate that the override names a real configured tier before trusting "active",
// which domain cannot do itself without importing config (forbidden: domain must stay
// config-agnostic; see CLAUDE.md).
//
// Both values are computed from a single time.Now() read, exactly like ClerkPublicMetadata does
// above, rather than having the caller separately call ComputeEffectiveStatus() and
// HasActiveManualOverride() (each reading time.Now() independently) — that would risk the two
// disagreeing about whether the override was active in the nanoseconds around the expiry
// boundary. See TestEffectiveTierAndStatusAt_AgreeAtSharedBoundary for why that boundary matters.
//
// hasOverride is false whenever there is no CURRENTLY ACTIVE override (none set, or expired); in
// that case status is simply the raw SubscriptionStatus and overrideValue is "".
//
// Delegates to EffectiveStatusWithActiveOverrideAt(time.Now()) — see that method's doc comment
// for why a caller assembling a response around a single already-captured snapshot instant (e.g.
// an `as_of` field) MUST call the -At variant with that instant instead of this method, which
// reads its own fresh clock.
func (u *User) EffectiveStatusWithActiveOverride() (status string, overrideValue string, hasOverride bool) {
	return u.EffectiveStatusWithActiveOverrideAt(time.Now())
}

// EffectiveStatusWithActiveOverrideAt is the -At twin of EffectiveStatusWithActiveOverride,
// evaluated against the given instant instead of an internal time.Now() read.
//
// Added for Codex round-N review of Issue #1462/#1413: a caller that has already captured a
// snapshot instant (e.g. an admin handler's `asOf := time.Now()`, echoed back to the client as
// `as_of`) must derive EVERY value in that same response — including EffectiveStatus — from that
// identical instant. Calling the no-arg EffectiveStatusWithActiveOverride() instead would read a
// SECOND, independent time.Now(), so a request whose processing crosses the override's expiry
// boundary between the two reads would return an `as_of` and an `effective_status` describing two
// different moments — the exact "two clocks describing one user" defect #1462 was meant to fix,
// reintroduced one layer up. See TestAdminUserService_GetUserDetail_EffectiveStatus_DerivedFromPassedInstant_NotLaterClockRead
// (admin_user_service_test.go) for the regression test.
func (u *User) EffectiveStatusWithActiveOverrideAt(now time.Time) (status string, overrideValue string, hasOverride bool) {
	if u.hasActiveManualOverrideAt(now) {
		return string(SubscriptionStatusActive), *u.ManualTierOverride, true
	}
	return u.SubscriptionStatus, "", false
}

// EffectiveTierWithActiveOverride returns the same value ComputeEffectiveTier() would, plus the
// raw manual_tier_override string when (and only when) a currently-active override is what
// produced that tier. This is the TIER-axis twin of EffectiveStatusWithActiveOverride, added for
// Issue #1413 (this task): manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so its value can be garbage (a legacy row, a direct SQL write, a write made while
// the tier config was nil). A caller that can look up configured tiers (e.g. via a
// *config.TierConfig it already holds, or an injected validator func) must validate overrideValue
// before trusting the returned tier, falling back to the raw SubscriptionTier on rejection —
// exactly like every tier-config-aware caller already falls back to the raw SubscriptionStatus on
// the status axis. domain cannot do this validation itself without importing config (forbidden;
// see CLAUDE.md), so it only surfaces the raw pieces for the caller to check.
//
// Both status and tier ultimately derive from the identical hasActiveManualOverrideAt check, so
// a caller that already computed EffectiveStatusWithActiveOverride() for the same user does not
// need to call this too — its overrideValue/hasOverride are exactly what this method would also
// return; reuse the status call's result to avoid a redundant time.Now() read.
//
// hasOverride is false whenever there is no CURRENTLY ACTIVE override (none set, or expired); in
// that case tier is simply the raw SubscriptionTier and overrideValue is "".
//
// Delegates to EffectiveTierWithActiveOverrideAt(time.Now()) — see that method's doc comment for
// why a caller assembling a response around a single already-captured snapshot instant (e.g. an
// `as_of` field) MUST call the -At variant with that instant instead of this method, which reads
// its own fresh clock.
func (u *User) EffectiveTierWithActiveOverride() (tier string, overrideValue string, hasOverride bool) {
	return u.EffectiveTierWithActiveOverrideAt(time.Now())
}

// EffectiveTierWithActiveOverrideAt is the -At twin of EffectiveTierWithActiveOverride,
// evaluated against the given instant instead of an internal time.Now() read.
//
// Added for Issue #1466 (a #1462 residual, found by Codex adversarial review of PR #1465's
// gap-fix commits): PR #1465 threaded a caller-captured instant through the STATUS axis
// (EffectiveStatusWithActiveOverrideAt) but left the TIER axis reading its own time.Now() — so a
// caller assembling a response around a single snapshot instant (e.g. an admin handler's
// `asOf := time.Now()`, echoed back to the client as `as_of`) could still return an
// `effective_tier` computed at a LATER instant than the one `as_of`/`effective_status` describe,
// if the override's expiry boundary fell between the handler's asOf capture and this call — the
// exact "two clocks describing one user" defect #1462/#1465 fixed on the status axis,
// reintroduced on the tier axis. See
// TestGetUserSubscription_EffectiveTier_DerivedFromAsOf_NotLaterClockRead
// (admin_subscriptions_test.go) for the regression test.
func (u *User) EffectiveTierWithActiveOverrideAt(now time.Time) (tier string, overrideValue string, hasOverride bool) {
	if u.hasActiveManualOverrideAt(now) {
		return *u.ManualTierOverride, *u.ManualTierOverride, true
	}
	return u.SubscriptionTier, "", false
}

// ClerkPublicMetadata returns the canonical Clerk publicMetadata payload for this user.
//
// The "tier" key is deliberately included with an explicit nil value rather than being
// omitted. Clerk's PATCH /v1/users/{id}/metadata endpoint performs a shallow merge of the
// supplied map into existing publicMetadata; passing an explicit null for a key deletes that
// key server-side, whereas omitting the key entirely would leave any existing value untouched.
// A pre-existing bug wrote an orphaned "tier" key into Clerk metadata, and this payload
// retroactively cleans it up on every sync.
//
// Note: no backend access gate reads Clerk metadata today — auth.ExtractSubscriptionTier's
// only callers are tests. Staleness of this payload (e.g. before a later task wires up sync)
// is therefore a UI/analytics concern, not a security one; the backend's authoritative source
// is ComputeEffectiveTier/ComputeEffectiveStatus evaluated directly against the database row.
//
// tier and status are computed from a single shared clock read rather than by calling the
// public ComputeEffectiveTier/ComputeEffectiveStatus methods (which would each read time.Now()
// independently), so the two fields can never disagree about whether an override is live.
func (u *User) ClerkPublicMetadata() map[string]interface{} {
	now := time.Now()
	return map[string]interface{}{
		"subscription_tier":   u.computeEffectiveTierAt(now),
		"subscription_status": u.computeEffectiveStatusAt(now),
		"tier":                nil,
	}
}

// NewUser creates a new user with default values (premium trial, default region)
// Used when a user logs in for the first time via Clerk
func NewUser(clerkUserID, email string, emailVerified bool) *User {
	now := time.Now()
	return &User{
		ID:                 uuid.New().String(),
		ClerkUserID:        clerkUserID,
		Email:              email,
		EmailVerified:      emailVerified,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      DefaultRegionCode(),
		IsActive:           true,
		IsDeleted:          false,
		Timezone:           "UTC",
		TimezoneSource:     "default",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
