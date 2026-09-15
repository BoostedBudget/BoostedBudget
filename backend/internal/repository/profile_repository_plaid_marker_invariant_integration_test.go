//go:build integration

package repository

// Integration tests for Issue #1413 round 16: the plaid_cleanup_pending_at invariant.
//
// See domain.User.PlaidCleanupPendingAt's doc comment for the invariant every writer/clearer of
// this column must maintain, and its precise definition of "episode":
//
//	An ineligibility episode BEGINS the instant one of two things happens: the ORDINARY way,
//	subscription_status transitions into a raw status
//	domain.SubscriptionStatus(subscription_status).IsEligibleForPlaidDisconnectRepair() calls true
//	('inactive' or 'cancelled'); or the OTHER way, a user already in such a status with no episode
//	open (their previous episode's cleanup already completed) acquires a live linked item —
//	StampMissingPlaidCleanupMarkers is the writer for that second case. An episode ENDS the instant
//	Plaid cleanup for THAT episode completes (the marker is cleared) — NOT the instant the user
//	later leaves the ineligible status. A user can stay 'cancelled' indefinitely with no episode
//	open, once that episode's cleanup has run.
//
//	plaid_cleanup_pending_at is non-NULL EXACTLY WHEN an episode (per that definition) is
//	currently open. NULL covers two legal cases: the status isn't ineligible at all, OR the status
//	is still ineligible but the CURRENT episode's cleanup has already completed — the second is
//	exactly what assertPlaidMarkerInvariant below checks directly (both conjuncts, not just the
//	first), and is why a NULL marker on a still-'cancelled' user is legal, not a contradiction.
//
//	EVENTUALLY consistent, not instantaneously true for every writer at every instant: round 15
//	(Finding 3, Codex review round on PR #1443) found a real race (the override-removal path's
//	live-item check cannot serialize against a concurrent LinkAccounts INSERT) that can transiently
//	leave the marker NULL while a live item exists moments later. Round 16 does not try to close
//	that window by writing more carefully — it heals it: profileRepository.StampMissingPlaidCleanupMarkers
//	(profile_repository.go) runs at the start of every repairPlaidDisconnects pass and atomically
//	stamps any ineligible, live-item, NULL-marker row it finds, so this invariant is guaranteed true
//	again within at most one repair-pass interval, not necessarily true at every single instant.
//
//	Deliberately NOT part of this invariant: manual_tier_override. Whether a comp is currently
//	live is a wall-clock fact (HasActiveManualOverride() vs. time.Now()), not a write — round-11/
//	round-12 tied this column's SET-time value to "is there a live override right now", which
//	silently required some LATER writer to re-stamp the marker the instant the override lapsed;
//	if that writer never ran, the marker was wrong FOREVER with no writer at fault. Round-13's
//	fix: the marker tracks only the durable, override-independent "episode began" fact; CURRENT
//	eligibility (status AND live override) is evaluated separately, at query time, by
//	GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate.
//
//	One consequence of that split (Issue #1413 Finding 3, Codex review, PR #1443 follow-up): a
//	NULL marker next to a LIVE linked item is ALSO legal whenever an active override currently
//	exists — StampMissingPlaidCleanupMarkers deliberately excludes an actively-comped user from
//	stamping, mirroring the candidate query's own exclusion, so nothing stamps that row while the
//	comp stays live. See assertPlaidMarkerInvariant's own doc comment for the precise, single case
//	this widens.
//
// assertPlaidMarkerInvariant below is a direct, reusable encoding of that statement — every test
// in this file calls it after every state-changing step, rather than asserting only the single
// column value the specific fix under test cares about. This is deliberate: several prior
// remediation rounds on this branch each closed the previous reviewer's single finding while
// introducing a new one, precisely because each fix was checked only against its own narrow
// expectation. Round 11's brief calls the sweep test in this file "the test that would have caught
// all four [round-11] findings at once" — TestPlaidMarkerInvariant_Integration_Sweep below is that
// test; the other tests in this file are the individual RED-then-GREEN regressions per fix.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run 'TestClearRetentionDates_Integration|TestUpdateSubscriptionFromPaddle_Integration|TestClearPlaidCleanupPendingIfMatches_Integration|TestPlaidMarkerInvariant' -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// plaidMarkerRow is the subset of a users row assertPlaidMarkerInvariant needs.
type plaidMarkerRow struct {
	status  string
	comp    *string
	compEnd *time.Time
	marker  *time.Time
}

func readPlaidMarkerRow(t *testing.T, pool *pgxpool.Pool, userID string) plaidMarkerRow {
	t.Helper()
	var row plaidMarkerRow
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT subscription_status, manual_tier_override, manual_tier_ends_at, plaid_cleanup_pending_at
		FROM users WHERE id = $1
	`, userID).Scan(&row.status, &row.comp, &row.compEnd, &row.marker))
	return row
}

// hasActiveOverride mirrors domain.User.hasActiveManualOverrideAt exactly (Issue #1413's existing
// parity convention between the Go predicate and its SQL/test twins). No longer consulted by
// assertPlaidMarkerInvariant (round-13 dropped override from the marker invariant entirely — see
// the file header comment), but kept as a helper for tests that need to independently assert on a
// row's override state directly (e.g. the positive control in
// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp).
func (r plaidMarkerRow) hasActiveOverride(now time.Time) bool {
	if r.comp == nil || *r.comp == "" {
		return false
	}
	return r.compEnd == nil || !r.compEnd.Before(now)
}

// assertPlaidMarkerInvariant is THE invariant check — the ONLY one; there used to be a second,
// narrower sibling (assertPlaidMarkerInvariantCleanupComplete), removed by the Issue #1413 Finding 2
// fix below. It re-derives, from the row's own committed state, whether an ineligibility episode is
// currently open (raw subscription_status alone — see the file header comment for why
// manual_tier_override is deliberately NOT part of this check as of round-13), and asserts
// plaid_cleanup_pending_at's nullness agrees with BOTH conjuncts of the invariant in BOTH
// directions — not just "is it set when it should be", but "is it correctly NULL when it should
// be" too. This is the single reusable assertion every test in this file drives every
// state-changing step through — see the file header comment for why that (not a fix-specific
// spot-check) is the point of this file.
//
// Task X (L2, Codex review round on PR #1443): the invariant has TWO conjuncts — non-NULL EXACTLY
// WHEN an episode is open AND cleanup for it has not completed — and this helper previously only
// checked the first, so a marker sitting on a user with no cleanup work left at all (e.g. every
// linked item already disconnected/replaced) silently passed. "Outstanding cleanup" here uses the
// SAME liveness definition GetUsersNeedingPlaidDisconnectRepair / GetOutstandingPlaidCleanupStats
// both use (profile_repository.go / job_health_repository.go): at least one linked_items row that
// isn't 'disconnected'/'replaced' (a NULL status counts as live).
//
// Round 14 -> 15 -> 16 on this second conjunct — read all three in order; the third is the one
// currently in force. Task X (above) tightened this helper to require outstanding cleanup whenever
// the marker is set. Round 15 then had to relax it again: Task Z's override-removal fix (Finding 3)
// had to make the marker stamp UNCONDITIONALLY at the time, because its liveness-guarded stamp lost
// a race with LinkAccounts (the CTE's EXISTS is a snapshot, and FOR UPDATE locks the users row, not
// the linked_items lifecycle — so a held lock merely delays the insert until after the admin's
// commit, which produces the leak rather than preventing it) — and asserting the strong second
// conjunct against that unconditional stamp would have made this helper fail on the ordinary
// remove-a-comp-after-cleanup-completed path, i.e. it would have asserted an invariant production
// deliberately did not maintain at the time. So round 15 relaxed this helper to match.
//
// Round 16 restores the strong second conjunct: profileRepository.StampMissingPlaidCleanupMarkers
// (profile_repository.go) now self-heals round 15's exact race on the very next repair pass instead
// of leaking forever (see domain.User.PlaidCleanupPendingAt's doc comment, "EVENTUALLY consistent"
// paragraph), which let round 14's liveLinkedItemExistsClause gate go back into
// removeUserTierOverrideSelectPrior (admin_user_repository.go) — so production no longer generates
// an unconditional phantom on that path, and this helper can go back to checking what it is
// actually FOR: auditing writers against the invariant they are supposed to hold, not an invariant
// weakened to match a bug. Both conjuncts are strict again:
//
//   - Non-NULL EXACTLY WHEN an episode is open (raw status ineligible) AND that episode's cleanup
//     has not completed — "outstanding cleanup" uses the SAME liveness definition
//     GetUsersNeedingPlaidDisconnectRepair / GetOutstandingPlaidCleanupStats both use
//     (profile_repository.go / job_health_repository.go): at least one linked_items row that isn't
//     'disconnected'/'replaced' (a NULL status counts as live).
//   - The ONE allowed exception is #1453, unchanged since Task X: a marker set with ZERO
//     linked_items rows at all (a user who never linked Plaid, or 000119/000120 migration residue —
//     indistinguishable at the row level, and both legitimate, reachable states).
//
// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up): before this fix, a NULL marker
// on an ineligible user was ALWAYS flagged as a violation here (an unconditional assert.NotNil),
// even though the invariant (domain.User.PlaidCleanupPendingAt's doc comment) explicitly permits a
// NULL marker on a still-ineligible user once that episode's cleanup has genuinely completed. A
// second helper, assertPlaidMarkerInvariantCleanupComplete, existed purely to cover that one legal
// case — but its doc comment was the ONLY thing preventing a caller from reaching for this one
// (documented as "THE invariant check") right after a legitimate clear and getting a false failure.
// That is a usability trap by construction, not merely a documentation gap: nothing about calling
// this function was wrong, so nothing here could have told a caller they picked the wrong helper.
//
// The fix makes this the ONLY helper by making it detect the completed-cleanup case itself, using
// the exact same live-item count this function already computes for the non-NULL branch: a NULL
// marker on an ineligible user is legal whenever the user has NO live linked item right now (either
// #1453's zero-rows population, or a genuinely completed cleanup with every item
// disconnected/replaced) — and is a real violation only if a LIVE item exists, which is exactly the
// case StampMissingPlaidCleanupMarkers exists to heal. There is no longer a second name to
// remember, so there is no longer a wrong one to pick. assertPlaidMarkerInvariantCleanupComplete is
// deleted; every former call site now calls this function instead — see
// TestUpdateSubscriptionFromPaddle_Integration_LaterTerminalEventDoesNotResurrectClearedMarker for
// both former call sites, now unified.
//
// Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up): the round-16 rule above — "a
// real violation only if a LIVE item exists" — was ITSELF one case too strict. StampMissingPlaidCleanupMarkers
// deliberately EXCLUDES an actively-comped user from stamping (noActiveManualOverridePredicate,
// profile_repository.go), matching GetUsersNeedingPlaidDisconnectRepair's own exclusion of the same
// population. So a NULL marker next to a live item is ALSO legal whenever an active override
// currently exists — that is exactly the reachable, intentional sequence
// TestPlaidMarkerInvariant_Integration_ActiveOverrideAndLiveItem_NullMarkerIsLegal below reproduces:
// a never-cleaned-up user with no marker and no items is comped, then links a bank (permitted — the
// comp makes their effective status active), and Stamp is never asked to touch that row while the
// comp stays live. The fix widens EXACTLY this one case (see the `if row.hasActiveOverride(...)`
// branch below) — every other check this function makes (the ineligible-false Nil check; the
// NULL-marker-with-live-items-and-no-override violation; the non-NULL-marker checks for both the
// #1453 zero-row exception and the "live item still outstanding" pass-through) is UNCHANGED. Round
// 15's relaxation of this same helper (above — dropping the second conjunct entirely rather than
// widening one specific, named case) was the far riskier shape of change, since it stopped this
// helper from catching an actual writer regression rather than merely accepting one additional
// legitimate state; this fix is deliberately scoped to widen nothing beyond the single case named
// above.
//
// This helper cannot itself distinguish "healed by the backstop on this exact call" from "would have
// been a genuine violation a moment ago" — both look identical once the backstop has run. That is
// fine: this file's own tests never race the backstop (it is not called from any shared setup
// helper — see StampMissingPlaidCleanupMarkers's own doc comment on why it must only ever be
// invoked from a fixture a test fully owns), so every assertion here observes production writers'
// steady-state behavior, not a mid-heal snapshot.
// plaidMarkerInvariantT is the minimal interface assertPlaidMarkerBranch1 needs from its first
// parameter: just enough to satisfy testify's assert.TestingT (Errorf) and to allow it to call
// Helper() for correct file:line attribution. *testing.T trivially satisfies this, so every
// existing call site below needs no change. Widening from the concrete *testing.T is what lets
// TestAssertPlaidMarkerBranch1_DetectsViolation (Issue #1455, below) substitute a recording
// double that CAPTURES a failure instead of propagating it to a live *testing.T — the only way to
// prove this branch can actually detect a violation, since calling assert.Nil against a real, live
// *testing.T with a non-nil marker fails the very test you are writing.
type plaidMarkerInvariantT interface {
	Errorf(format string, args ...interface{})
	Helper()
}

// assertPlaidMarkerBranch1 is invariant branch 1's own assertion (status NOT ineligible ⇒ marker
// must be NULL), extracted out of assertPlaidMarkerInvariant below so it can be driven directly —
// see plaidMarkerInvariantT's doc comment above, and TestAssertPlaidMarkerBranch1_DetectsViolation's
// doc comment (Issue #1455), for why: this exact assert.Nil call had never been driven
// against a genuinely violating row anywhere in this file before that test.
func assertPlaidMarkerBranch1(t plaidMarkerInvariantT, marker *time.Time, status string, context string) {
	assert.Nil(t, marker,
		"[%s] invariant violated: no ineligibility episode is open (status=%q) but plaid_cleanup_pending_at is %v",
		context, status, marker)
}

func assertPlaidMarkerInvariant(t *testing.T, pool *pgxpool.Pool, userID string, context string) {
	t.Helper()
	row := readPlaidMarkerRow(t, pool, userID)
	ineligible := domain.SubscriptionStatus(row.status).IsEligibleForPlaidDisconnectRepair()
	if !ineligible {
		assertPlaidMarkerBranch1(t, row.marker, row.status, context)
		return
	}

	total, live := plaidMarkerLinkedItemCounts(t, pool, userID)

	if row.marker == nil {
		// Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up): a NULL marker next to a
		// LIVE item is ALSO legal — widening this ONE case, and only this one — whenever the user
		// currently has an ACTIVE manual override. StampMissingPlaidCleanupMarkers
		// (profile_repository.go) deliberately excludes an actively-comped user from stamping
		// (noActiveManualOverridePredicate), mirroring GetUsersNeedingPlaidDisconnectRepair's own
		// exclusion of the same population — comping protects at query time, not by pre-emptively
		// stamping a marker the query would then have to explain away. So: a never-cleaned-up user
		// with no marker and no items, comped, who THEN links a bank (permitted — the comp makes
		// their effective status active) is a reachable, intentional state, not a defect — see
		// TestPlaidMarkerInvariant_Integration_ActiveOverrideAndLiveItem_NullMarkerIsLegal below.
		if row.hasActiveOverride(time.Now()) {
			return
		}
		// Legal NULL on an ineligible user with no active override: either #1453 (zero linked_items
		// rows at all) or a genuinely completed cleanup (at least one row, none of them live) —
		// either way, no LIVE item means no outstanding cleanup, so no episode is actually open right
		// now regardless of raw status. Illegal only if a live item exists AND no active override
		// excuses the gap: that is exactly the state StampMissingPlaidCleanupMarkers exists to heal,
		// so a NULL marker there is a real defect, not a legal quiescent state.
		assert.Equal(t, 0, live,
			"[%s] invariant violated: an ineligibility episode is open (status=%q, %d live linked item(s) outstanding, no active override) but plaid_cleanup_pending_at is NULL",
			context, row.status, live)
		return
	}

	if live > 0 {
		return
	}
	assert.Equal(t, 0, total,
		"[%s] invariant violated: status=%q is ineligible and plaid_cleanup_pending_at is set, but this user HAS %d linked_items row(s) and every one is already disconnected/replaced — cleanup for this episode already completed and the marker should have been cleared; the ONLY allowed exception is a user with ZERO linked_items rows at all (#1453 migration-residue / never-linked-Plaid shape)",
		context, row.status, total)
}

// plaidMarkerLinkedItemCounts returns the total number of linked_items rows for userID, and how
// many of those are "live" — the SAME liveness definition
// GetUsersNeedingPlaidDisconnectRepair/GetOutstandingPlaidCleanupStats use (a NULL status counts
// as live; only 'disconnected'/'replaced' don't). Used by assertPlaidMarkerInvariant's checks in
// BOTH the NULL-marker and non-NULL-marker branches above. Removed in round 15 (when the second
// conjunct itself was dropped) and reinstated in round 16 alongside it — see
// assertPlaidMarkerInvariant's own comment.
func plaidMarkerLinkedItemCounts(t *testing.T, pool *pgxpool.Pool, userID string) (total, live int) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status IS NULL OR status NOT IN ('disconnected', 'replaced'))
		FROM linked_items
		WHERE user_id = $1
	`, userID).Scan(&total, &live))
	return total, live
}

// recordingT is a minimal assert.TestingT double that RECORDS a failure instead of propagating it
// to a live *testing.T — see plaidMarkerInvariantT's doc comment above for why this is the only way
// to test that assertPlaidMarkerBranch1 can actually detect a violation (Issue #1455).
type recordingT struct {
	failed   bool
	messages []string
}

func (r *recordingT) Errorf(format string, args ...interface{}) {
	r.failed = true
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recordingT) Helper() {}

// TestAssertPlaidMarkerBranch1_DetectsViolation is Issue #1455: assertPlaidMarkerInvariant's
// branch 1 (status NOT ineligible ⇒ marker must be NULL) is pre-existing and unchanged on this
// branch, and no test anywhere in this file ever drove it against a row that actually violates it.
// The one subtest that constructs that exact raw state,
// branch1_backstop_heals_stale_marker_on_reactivation (below, in the sweep test), deliberately
// heals the row via ClearMarkersOnEligibleStatus BEFORE ever calling assertPlaidMarkerInvariant — by
// the time the helper runs there, the marker is already NULL and assert.Nil passes trivially. That
// subtest's own comment says this routing-around is deliberate, and it is correct for what THAT
// subtest is proving (the backstop heals the corrupted state) — but the net effect is that branch
// 1's own assert.Nil call had never actually been forced to fail anywhere in this suite. An
// assertion that cannot go red is not coverage.
//
// This test drives a genuine violation directly into the extracted assertPlaidMarkerBranch1, using
// recordingT (immediately above) to CAPTURE the resulting failure instead of propagating it to this
// test's own *testing.T — calling assert.Nil against a live *testing.T with a non-nil marker would
// fail the very test being written, so there is no way to observe "did this correctly fail" using a
// real *testing.T directly. No database access is needed: branch 1's predicate is a pure function of
// (marker, status), which is exactly why it could be extracted at all.
//
// Falsifiability: this test was run against a deliberately neutered assertPlaidMarkerBranch1 (its
// assert.Nil call replaced with a no-op) and confirmed to fail — rec.failed was false — before being
// restored to the real assertion. See the accompanying report for the RED transcript and
// confirmation the mutation landed in the assertion itself, not a comment.
func TestAssertPlaidMarkerBranch1_DetectsViolation(t *testing.T) {
	rec := &recordingT{}
	violatingMarker := time.Now().Add(-time.Hour)

	assertPlaidMarkerBranch1(rec, &violatingMarker, "active", "branch1 violation check")

	assert.True(t, rec.failed, "assertPlaidMarkerBranch1 must flag a non-NULL marker on a non-ineligible status as a violation")
	require.Len(t, rec.messages, 1)
	assert.Contains(t, rec.messages[0], "invariant violated", "the recorded failure message must be branch 1's own violation message")
}

// createPlaidMarkerTestUser creates a minimal user with the given initial status for these tests.
func createPlaidMarkerTestUser(t *testing.T, pool *pgxpool.Pool, repo *profileRepository, namePrefix, status string) *domain.User {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + namePrefix + "_" + suffix,
		Email:              namePrefix + "-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck
	return user
}

// ─── Fix 1 (Blocker): ClearRetentionDates must not clobber a fresher cancellation's marker ──

// TestClearRetentionDates_Integration_StaleActiveClearDoesNotClobberFresherCancellationMarker is
// the round-11 Blocker RED test. Sequence modeled: an "active" webhook is about to call
// ClearRetentionDates, but pauses; a NEWER cancellation webhook lands first, writing
// subscription_status='cancelled' and a FRESH plaid_cleanup_pending_at; the stale "active"
// webhook then resumes and finally calls ClearRetentionDates. Before the round-11 fix,
// ClearRetentionDates unconditionally nulled the marker — losing the fresh cancellation's marker
// forever (the repair query requires a non-NULL marker, so cleanup would never run; the user stays
// cancelled with a live Plaid item indefinitely).
//
// Falsifiability: reverting ClearRetentionDates to unconditionally NULL plaid_cleanup_pending_at
// makes this test fail — the marker would be NULL after the stale clear call.
func TestClearRetentionDates_Integration_StaleActiveClearDoesNotClobberFresherCancellationMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "staleactiveclear", "active")

	// The newer cancellation webhook already committed: status='cancelled' plus a fresh marker —
	// via the REAL production writer for this joint transition, UpdateSubscriptionFromPaddle, not
	// a raw SQL UPDATE that could silently drift from what that writer actually does (Issue #1413
	// Task W, Codex review: this file's entire purpose is auditing writers against the marker
	// invariant — a setup that bypasses the writer under test cannot detect a writer that violates
	// it). The row's OLD status is 'active' (not already repair-eligible), so this is a genuine
	// transition and the writer's own COALESCE(plaid_cleanup_pending_at, now) freshly stamps the
	// marker, exactly modeling "the newer cancellation webhook already committed."
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	freshMarkerRow := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, freshMarkerRow.marker, "precondition: the cancellation webhook must have started the marker")
	freshMarker := *freshMarkerRow.marker
	assertPlaidMarkerInvariant(t, pool, user.ID, "after newer cancellation webhook lands")

	// The STALE "active" webhook, from before the cancellation, now finally calls
	// ClearRetentionDates — this must NOT clobber the fresher marker, because the row's CURRENT
	// (live) status is 'cancelled', not 'active'.
	require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "the fresh cancellation marker must survive a stale active-webhook's ClearRetentionDates call")
	assert.WithinDuration(t, freshMarker, *row.marker, time.Second, "the surviving marker must still be the fresh cancellation's own value, not resurrected from elsewhere")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after stale ClearRetentionDates call")

	// The repair query must still find this user — this is the blocker's real-world impact: without
	// the fix, this user's Plaid access would persist indefinitely because the repair pass would
	// never see them as a candidate.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-testGracePeriod-time.Hour))
	seedLinkedItem(t, pool, user.ID, "item-staleactiveclear", "active")
	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidates {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.True(t, found, "the user must still be selected by the repair query — the blocker's impact was that they never would be")
}

// TestClearRetentionDates_Integration_GenuinelyActiveClearsMarker is the positive control for Fix
// 1: when the row's LIVE status genuinely is not Plaid-repair-eligible (the normal resubscribe
// case, with no stale-webhook race), ClearRetentionDates must still clear the marker — proving the
// conditional clear isn't just a no-op that never fires.
func TestClearRetentionDates_Integration_GenuinelyActiveClearsMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "genuineactiveclear", "cancelled")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))

	// Genuine resubscribe: status flips to active FIRST (mirroring
	// UpdateSubscriptionFromPaddle running before ClearRetentionDates in every real caller), THEN
	// ClearRetentionDates runs as the belt-and-braces second write.
	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)

	require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, row.marker, "a genuinely active user's marker must be cleared by ClearRetentionDates")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine resubscribe clear")
}

// ─── Fix 3 (High): the repair recheck must validate the marker's identity, not merely presence ──

// TestClearPlaidCleanupPendingIfMatches_Integration_ClearsWhenMatches proves the CAS-guarded clear
// (Issue #1413 round-11 Fix 3, mirroring ClearPastDueStartedAtIfMatches's established pattern)
// clears the marker when it still equals the expected value.
func TestClearPlaidCleanupPendingIfMatches_Integration_ClearsWhenMatches(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "casmatch", "cancelled")
	marker := time.Now().Add(-100 * time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, marker)

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, marker)
	require.NoError(t, err)
	assert.True(t, cleared, "the clear must succeed when the marker still equals the expected value")

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, row.marker)
}

// TestClearPlaidCleanupPendingIfMatches_Integration_SkipsWhenMismatched proves the CAS clear
// leaves a REPLACED marker (a newer episode) untouched — the core of Fix 3: a stale worker must
// not erase a fresher episode's marker just because a marker is present.
func TestClearPlaidCleanupPendingIfMatches_Integration_SkipsWhenMismatched(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "casmismatch", "cancelled")
	staleExpected := time.Now().Add(-200 * time.Hour)
	freshActual := time.Now().Add(-time.Minute)
	setPlaidCleanupPendingAt(t, pool, user.ID, freshActual)

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, staleExpected)
	require.NoError(t, err)
	assert.False(t, cleared, "the clear must be a no-op when the current marker no longer matches the expected (stale) value")

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker)
	assert.WithinDuration(t, freshActual, *row.marker, time.Second, "the newer episode's marker must survive untouched")
}

// TestClearPlaidCleanupPendingIfMatches_Integration_UserNotFound proves the not-found case is
// distinguishable from a CAS mismatch, mirroring ClearPastDueStartedAtIfMatches's contract.
func TestClearPlaidCleanupPendingIfMatches_Integration_UserNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, "00000000-0000-0000-0000-000000000000", time.Now())
	assert.False(t, cleared)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// ─── Round-13: cancellation while comped now SETS the marker; a live override protects the user
// only at query time, not by suppressing the marker ──

// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp
// is the Issue #1413 round-13 test, replacing the round-11 Fix 4 test of the same scenario with
// the OPPOSITE marker assertion: writing subscription_status='cancelled' for a user with a LIVE
// manual tier override now DOES set plaid_cleanup_pending_at — the marker tracks only the raw
// subscription_status episode, independent of override (see the file header comment).
//
// Round-11 Fix 4's gate (null the marker while a live override exists) was itself an instance of
// the round-13 P0's root cause: hasActiveManualOverrideSQLExpr checked "is there a live override
// RIGHT NOW", a wall-clock fact, live in the write — correct at that instant, but if the comp later
// lapsed with the daily expiry job degraded or never run, NOTHING would ever re-stamp the marker,
// permanently hiding this user from the repair pass. Per the restated invariant, CURRENT
// eligibility is instead entirely GetUsersNeedingPlaidDisconnectRepair's job, via its own
// noActiveManualOverridePredicate, re-evaluated fresh on every run — which is what this test's
// second half proves: the marker being set does NOT mean the user gets disconnected while the comp
// is still live.
//
// Falsifiability: reverting UpdateSubscriptionFromPaddle's eligible-branch SQL to reintroduce the
// hasActiveManualOverrideSQLExpr CASE (nulling the marker while a live override exists) makes the
// first half of this test fail — the marker would be NULL instead of set.
func TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "cancelwhilecomped", "active")
	compEnds := time.Now().Add(24 * time.Hour) // live
	// Grant the live comp through the REAL production writer (AdminUserRepository.UpdateUserTier),
	// not a raw SQL UPDATE — Issue #1413 Task W (Codex review): this file's entire purpose is
	// auditing writers against the marker invariant, and TestPlaidMarkerInvariant_Integration_Sweep
	// (this same file, comp_grant_to_remove/comp_grant_then_lapse_never_removed) already grants
	// comps this way — a raw SQL grant here was inconsistent with that standard, and (per
	// updateUserTierQuery's own doc comment) a setup that bypasses the writer under test cannot
	// detect a writer that violates it. UpdateUserTier deliberately leaves plaid_cleanup_pending_at
	// untouched, so this substitution changes nothing this test observes.
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant live comp", &compEnds)
	require.NoError(t, err)
	assertPlaidMarkerInvariant(t, pool, user.ID, "before cancellation, while comped")

	// Mirrors subscription_webhook.go's processSubscriptionCanceled: write 'cancelled' as billing
	// truth, unconditionally, regardless of the comp.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	beforeRemoval := readPlaidMarkerRow(t, pool, user.ID)
	assert.Equal(t, "cancelled", beforeRemoval.status, "billing truth (raw subscription_status) must still flip to cancelled regardless of the comp")
	require.NotNil(t, beforeRemoval.marker, "the marker must be SET — a genuine raw-status transition into ineligibility began an episode, independent of the live override")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after cancellation while comped")

	// The live comp is what actually protects this user from repair — proven at the query layer,
	// not by suppressing the marker.
	seedLinkedItem(t, pool, user.ID, "item-cancelwhilecomped-"+user.ID, "active")
	candidatesWhileLive, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
	require.NoError(t, err)
	for _, c := range candidatesWhileLive {
		assert.NotEqual(t, user.ID, c.ID, "a user with a CURRENTLY-LIVE comp must never be selected for repair, even though the marker is now set")
	}

	// Follow-through: once the comp is later removed, the marker must now be FRESH — Issue #1413
	// Round 18 (Codex review, PR #1443 follow-up, High finding) superseded round-13's "PRESERVE the
	// original cancellation-time value" behavior asserted here through that round: preserving the
	// original value via COALESCE(existing, NOW()) is exactly the mechanism that resurrects a STALE
	// marker from an already-completed episode when the sequence is comp-grant -> stale marker ->
	// relink -> comp-removal (see removeUserTierOverrideSelectPrior's own doc comment,
	// admin_user_repository.go, "ROUND 18", for the full Path A/Path B hazard and the reasoning for
	// why restarting the grace window here is correct, not a regression, even for THIS test's shape
	// where the episode was genuinely still open at removal time). should_mark_plaid_cleanup_pending
	// now writes NOW() unconditionally whenever it fires — which it does here: a prior override
	// existed, the reverted status ('cancelled') is repair-eligible, and a live linked item was
	// seeded above.
	before := dbNow(t, pool)
	_, err = adminRepo.RemoveUserTierOverride(ctx, "", user.ID, "test: comp removed", 6)
	require.NoError(t, err)
	after := dbNow(t, pool)

	afterRemoval := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, afterRemoval.marker)
	assertBetweenFence(t, before, after, *afterRemoval.marker,
		"removing the comp must now RESTART the grace window with a fresh NOW(), not preserve the original cancellation-time value — the user held a comp for the interim, so the window to react to actually losing access runs from losing it, not from whenever the underlying episode first began")
	assert.False(t, afterRemoval.marker.Equal(*beforeRemoval.marker),
		"the marker after removal must NOT be the original cancellation-time value anymore")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after comp removal")
}

// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileOverrideLapsed_MarkerIsSet is the
// positive control proving a LAPSED (expired) override never suppressed the marker either, before
// or after round-13 — matching HasActiveManualOverride()'s boundary semantics exactly.
func TestUpdateSubscriptionFromPaddle_Integration_CancelWhileOverrideLapsed_MarkerIsSet(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "cancelwhilelapsed", "active")
	lapsedEnds := time.Now().Add(-24 * time.Hour) // already expired
	// Grant the (already-lapsed) comp through the REAL production writer
	// (AdminUserRepository.UpdateUserTier), not a raw SQL UPDATE — same rationale as the sibling
	// test above, and matching TestPlaidMarkerInvariant_Integration_Sweep's
	// comp_grant_then_lapse_never_removed subtest (this same file), which grants an
	// already-past-expiry comp through this exact writer.
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant already-lapsed comp", &lapsedEnds)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "a LAPSED override must not suppress the marker — only a currently-active one does")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after cancellation with a lapsed override")
}

// ─── Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up): a NULL marker next to a
// LIVE item is ALSO legal while an active override exists ──────────────────────────────────────────

// TestPlaidMarkerInvariant_Integration_ActiveOverrideAndLiveItem_NullMarkerIsLegal reproduces the
// exact reachable sequence assertPlaidMarkerInvariant's round-16 rule ("NULL is legal only when zero
// LIVE items exist") wrongly rejected:
//
//  1. A cancelled user has no marker and no linked items at all (an ordinary Plaid-less churn, or a
//     fully-healed prior episode).
//  2. An admin grants an ACTIVE comp (adminRepo.UpdateUserTier).
//  3. The user links a live bank — permitted, because the comp makes their EFFECTIVE status active
//     (internal/huma/middleware/read_only.go).
//  4. StampMissingPlaidCleanupMarkers deliberately EXCLUDES an actively-comped user
//     (noActiveManualOverridePredicate) — matching GetUsersNeedingPlaidDisconnectRepair's own
//     exclusion of the same population — so it does not stamp this row.
//  5. The marker stays NULL while a live item genuinely exists.
//
// That is production's own intended shape, not a leak: GetUsersNeedingPlaidDisconnectRepair itself
// never selects this user while the comp stays live (see
// TestUpdateSubscriptionFromPaddle_Integration_CancelWhileComped_MarkerSetButRepairExcludesLiveComp's
// sibling proof, above in this file, for the query-layer half of the same guarantee), so nothing is
// actually at risk of disconnect. Only the invariant HELPER was too strict.
//
// Falsifiability: reverting assertPlaidMarkerInvariant's `if row.hasActiveOverride(...)` branch makes
// this test fail at its final call — a NULL marker with a live item present would be flagged as a
// violation even though no writer is at fault and the state is fully intentional.
func TestPlaidMarkerInvariant_Integration_ActiveOverrideAndLiveItem_NullMarkerIsLegal(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "activeoverridelive", "cancelled")
	// Precondition: an ordinary Plaid-less cancellation — no marker, no linked items at all.
	assertPlaidMarkerInvariant(t, pool, user.ID, "before comp grant: cancelled, no marker, no items")

	compEnds := time.Now().Add(24 * time.Hour) // live
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant live comp over never-linked user", &compEnds)
	require.NoError(t, err)

	seedLinkedItem(t, pool, user.ID, "item-activeoverridelive-"+user.ID, "active")

	// Run the self-healing backstop — it must NOT stamp this row while the comp stays live.
	healedCount, err := repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, healedCount, "precondition: Stamp must not touch an actively-comped user")

	preRow := readPlaidMarkerRow(t, pool, user.ID)
	require.Nil(t, preRow.marker, "precondition: the marker must still be NULL — Stamp deliberately excludes an actively-comped user")
	require.True(t, preRow.hasActiveOverride(time.Now()), "precondition: the comp must still be active")
	total, live := plaidMarkerLinkedItemCounts(t, pool, user.ID)
	require.Equal(t, 1, total)
	require.Equal(t, 1, live, "precondition: a genuinely LIVE item now exists")

	// The core assertion: this exact state must be recognised as LEGAL, not flagged as a violation.
	assertPlaidMarkerInvariant(t, pool, user.ID, "active comp, live item, NULL marker: must be legal")
}

// ─── Round-12 Fix 4 (Medium): a later terminal webhook must not resurrect a completed marker ──

// TestUpdateSubscriptionFromPaddle_Integration_LaterTerminalEventDoesNotResurrectClearedMarker is
// the Issue #1413 round-12 Fix 4 RED test. Sequence modeled: a user cancels (marker set); the
// Plaid-disconnect repair pass completes and clears the marker
// (ClearPlaidCleanupPendingIfMatches) — cleanup for THIS episode is done; a DISTINCT, LATER
// terminal webhook for the SAME still-cancelled subscription then arrives (e.g. a redelivered or
// duplicate subscription.updated/canceled event that is not a genuine status transition) and calls
// UpdateSubscriptionFromPaddle with status='cancelled' again. Before the fix, the eligible branch's
// bare `COALESCE(plaid_cleanup_pending_at, $N)` saw a NULL marker and manufactured a BRAND NEW one
// — for cleanup that had already completed, timestamped AFTER the episode actually began.
//
// Falsifiability: reverting the eligible-branch SQL to `COALESCE(plaid_cleanup_pending_at, $N)`
// (dropping the "genuine transition" WHEN clause) makes this test fail — the marker would be
// resurrected non-NULL.
func TestUpdateSubscriptionFromPaddle_Integration_LaterTerminalEventDoesNotResurrectClearedMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "laterterminalnoresurrect", "active")

	// The FIRST cancellation: genuine transition into ineligibility — marker starts.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	afterFirstCancel := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, afterFirstCancel.marker, "the first cancellation must start the marker")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after first cancellation")

	// The repair pass completes cleanup and clears the marker for this episode.
	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, *afterFirstCancel.marker)
	require.NoError(t, err)
	require.True(t, cleared, "precondition: the repair pass must successfully clear the marker")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after repair pass clears the marker")

	// A DISTINCT, LATER terminal webhook for the SAME still-cancelled subscription arrives — NOT a
	// genuine transition (the row was already 'cancelled' before this call).
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	assert.Equal(t, "cancelled", row.status)
	assert.Nil(t, row.marker, "a later terminal webhook for a user ALREADY cancelled must NOT resurrect a new marker — cleanup for this episode already completed")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after later distinct terminal event for the same episode")
}

// TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker is the positive
// control for round-12 Fix 4: a REAL new ineligibility episode (resubscribe, then cancel again)
// must still get its OWN fresh marker — proving the "genuine transition" guard isn't a blanket
// suppression that breaks the ordinary repeat-cancellation case.
func TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := createPlaidMarkerTestUser(t, pool, repo, "genuinenewepisode", "active")

	// Episode 1: cancel, then repair completes and clears.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
	episode1 := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, episode1.marker)
	cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, *episode1.marker)
	require.NoError(t, err)
	require.True(t, cleared)

	// Genuine resubscribe: status flips back to active — a real transition OUT of ineligibility.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine resubscribe")

	// Fence from the DATABASE clock (never compare Go time.Now() against a Postgres-written
	// timestamp — CI's Postgres container clock can lag the runner's).
	var before time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT NOW()").Scan(&before))

	// Episode 2: a GENUINE new cancellation — the row's old status is 'active', not already
	// eligible, so this must get its OWN fresh marker, not be suppressed.
	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "a genuine NEW cancellation episode must get its own fresh marker")
	assert.False(t, row.marker.Before(before), "the new episode's marker must be fresh (>= the fence), not suppressed or resurrected from episode 1")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after genuine second cancellation")
}

// ─── The invariant sweep — the single most valuable test in this task (per the round-11 brief):
// walks the realistic transition sequences and asserts the invariant holds after EVERY one. This
// is the test that would have caught all four round-11 findings at once. ──

// TestPlaidMarkerInvariant_Integration_Sweep walks: trial→expire, active→cancel,
// cancel→resubscribe, comp grant→remove, past-due→grace — asserting
// assertPlaidMarkerInvariant after every single transition, on independent users so one
// sequence's fixture never leaks into another's assertions.
//
// PR #1443 gap-analysis finding (requirements-gap-analysis follow-up): every user this test built
// came from createPlaidMarkerTestUser, which never seeds a linked_items row — so four of these six
// subtests (trial_to_expire — deleted in #1652 —, active_to_cancel, comp_grant_to_remove,
// past_due_to_grace) asserted
// NOTHING about the marker. With zero linked_items rows, assertPlaidMarkerInvariant's non-NULL
// branch always lands in the #1453 zero-rows exception (assert.Equal(t, 0, total) trivially true)
// and its NULL branch always lands in assert.Equal(t, 0, live) (also trivially true, 0 == 0) —
// BOTH branches pass no matter what the marker actually held. cancel_to_resubscribe was the one
// exception (its post-resubscribe check lands in the item-independent `!ineligible` branch, a real
// assert.Nil), and comp_grant_then_lapse_never_removed asserts its own repair-query membership
// directly rather than going through the marker invariant a second time. The fix: each of them
// now seeds one live linked item (seedLinkedItem) before its first assertion, so every
// assertPlaidMarkerInvariant call in this test actually exercises the invariant's real branches
// instead of the zero-rows escape hatch. Seeded per-subtest, not inside createPlaidMarkerTestUser
// itself: TestPlaidMarkerInvariant_Integration_ActiveOverrideAndLiveItem_NullMarkerIsLegal (above)
// depends on that helper producing a user with NO linked_items rows at all for its own "no items"
// precondition — changing the shared fixture would break that test's setup.
func TestPlaidMarkerInvariant_Integration_Sweep(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()


	t.Run("active_to_cancel", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepactive", "active")
		// See this test's doc comment above for why this seed is required (not optional) for the
		// invariant check after cancellation to be anything other than a tautology.
		seedLinkedItem(t, pool, user.ID, "item-"+user.ID, "active")
		assertPlaidMarkerInvariant(t, pool, user.ID, "active: initial")

		require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
		assertPlaidMarkerInvariant(t, pool, user.ID, "active->cancel: after cancellation webhook")
	})

	t.Run("cancel_to_resubscribe", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepresub", "cancelled")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: cancelled with marker")

		// Mirrors subscription_webhook.go's resubscribe handling: UpdateSubscriptionFromPaddle
		// first, then the belt-and-braces ClearRetentionDates.
		require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: after status write")
		require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cancel->resubscribe: after ClearRetentionDates")
	})

	t.Run("comp_grant_to_remove", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepcomp", "cancelled")
		// See this test's doc comment above: without a live linked item, every check below is a
		// tautology — including, worst of all, the one after RemoveUserTierOverride, which
		// exercises removeUserTierOverrideUpdate (admin_user_repository.go), the single
		// most-revised writer on this branch. should_mark_plaid_cleanup_pending itself is gated on
		// liveLinkedItemExistsClause, so without this seed the removal doesn't even reach the
		// writer's marker-stamping branch — it silently falls through the ELSE (leave the marker
		// untouched), which the zero-rows exception then hides either way.
		seedLinkedItem(t, pool, user.ID, "item-"+user.ID, "active")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: cancelled with marker, before grant")

		expiresAt := time.Now().Add(48 * time.Hour)
		_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: grant", &expiresAt)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: after grant")

		_, err = adminRepo.RemoveUserTierOverride(ctx, "", user.ID, "test: remove", 6)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp: after remove")
	})

	// t.Run("comp_grant_then_lapse_never_removed", ...) is the Issue #1413 round-13 P0 path:
	// unlike comp_grant_to_remove above (which always calls the unconditional admin removal
	// immediately and so never models a stalled expiry job), this subtest grants a comp with an
	// expiresAt ALREADY in the past — modeling a comp that has lapsed by the time anyone checks —
	// and deliberately NEVER calls RemoveUserTierOverride/RemoveUserTierOverrideIfMatches, modeling
	// the daily comp-expiry job degrading or never running at all. The invariant must still hold
	// throughout (the marker survives the grant untouched), and the repair query must still find
	// this user despite the override column still being populated (lapsed, never cleared) — proving
	// the fix does not depend on any writer ever re-visiting this row again.
	t.Run("comp_grant_then_lapse_never_removed", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepcomplapse", "cancelled")
		// Seeded HERE, before the invariant assertions — not just before the repair query below.
		// With zero linked_items rows assertPlaidMarkerInvariant's NULL-marker branch degenerates to
		// assert.Equal(t, 0, live) against live == 0, which passes whatever the marker holds. So a
		// comp grant that wrongly CLEARED the marker — the exact round-13 P0 regression this subtest
		// exists to catch, and which the "after grant" assertion below is positioned to detect —
		// would go completely unnoticed. One live item is what makes that branch bite.
		// (Issue #1413 round-5 gap analysis; independently flagged as a High finding by Codex.)
		seedLinkedItem(t, pool, user.ID, "item-"+user.ID, "active")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp-lapse: cancelled with marker, before grant")

		lapsedExpiry := time.Now().Add(-1 * time.Hour)
		_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: comp an already-cancelled user", &lapsedExpiry)
		require.NoError(t, err)
		assertPlaidMarkerInvariant(t, pool, user.ID, "comp-lapse: after grant with already-past expiry")

		// Deliberately NOT calling RemoveUserTierOverride / RemoveUserTierOverrideIfMatches — this
		// is the whole point of this subtest: the expiry job never runs.
		candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, time.Millisecond)
		require.NoError(t, err)
		found := false
		for _, c := range candidates {
			if c.ID == user.ID {
				found = true
			}
		}
		assert.True(t, found, "a comp granted over an already-cancelled user, lapsed and never removed, must still be found by the repair pass")
	})

	// t.Run("branch1_backstop_heals_stale_marker_on_reactivation", ...) is Issue #1455's sweep
	// subtest: the invariant's FIRST branch (status NOT ineligible ⇒ marker must be NULL) had no
	// runtime backstop at all until ClearMarkersOnEligibleStatus (profile_repository.go) — both
	// StampMissingPlaidCleanupMarkers and ClearCompletedPlaidCleanupMarkers are gated on
	// plaidDisconnectRepairEligibleStatusPredicate, so neither ever looks at a row whose status is
	// NOT inactive/cancelled. No production writer can reach this state today (PR #1443 deleted the
	// only one that could — see Issue #1455's "Current state" section) — modeled directly via
	// setPlaidCleanupPendingAt, the same raw-SQL convention comp_grant_to_remove and
	// cancel_to_resubscribe already use for states no writer currently produces.
	//
	// Deliberately does NOT call assertPlaidMarkerInvariant on the corrupted state itself — that
	// helper's whole point is to fail loudly on a genuine violation, and asserting a state we already
	// know violates the invariant would just be a scripted, uninformative failure. Instead this
	// subtest reads the column directly to confirm the violation exists, THEN calls the new backstop,
	// THEN uses assertPlaidMarkerInvariant to prove the invariant holds again afterward — the
	// "proves your backstop repairs it" case Issue #1455 asks for.
	t.Run("branch1_backstop_heals_stale_marker_on_reactivation", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepbranch1", "active")
		setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-time.Hour))

		var preRepair *time.Time
		require.NoError(t, pool.QueryRow(ctx, "SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", user.ID).Scan(&preRepair))
		require.NotNil(t, preRepair, "setup sanity check: the corrupted state must actually have a non-NULL marker before the backstop runs")

		clearedCount, err := repo.ClearMarkersOnEligibleStatus(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, clearedCount, 1, "at least this fixture's row must be healed")

		assertPlaidMarkerInvariant(t, pool, user.ID, "branch1: after ClearMarkersOnEligibleStatus")
	})

	// t.Run("cancel_to_disconnect_to_cas_clear", ...) is Issue #1457 Gap 1: no existing real-DB
	// test paired a genuine Plaid disconnect (LinkedItemRepository.MarkItemsDisconnected) with the
	// CAS-guarded clear (ProfileRepository.ClearPlaidCleanupPendingIfMatches) — the exact pair
	// repairPlaidDisconnects (retention_cleanup_check.go) calls in sequence once a disconnect
	// succeeds. What existed before this subtest: mock-based unit tests of the mechanics elsewhere
	// (subscription_webhook_test.go, retention_cleanup_check_test.go, past_due_service_test.go);
	// one integration test pairing a real MarkItemsDisconnected with the UNCONDITIONAL
	// ClearPlaidCleanupPending, not the CAS variant
	// (TestRemoveUserTierOverride_Integration_M1_NoPhantomMarker_WhenCleanupAlreadyComplete,
	// admin_user_repository_remove_tier_override_integration_test.go); and CAS-clear integration
	// tests, above in this file, that never disconnect anything at all
	// (TestClearPlaidCleanupPendingIfMatches_Integration_ClearsWhenMatches/_SkipsWhenMismatched).
	// None of those exercises the actual intermediate window between a real disconnect and its
	// CAS-guarded clear — which is the entire point of this subtest.
	t.Run("cancel_to_disconnect_to_cas_clear", func(t *testing.T) {
		user := createPlaidMarkerTestUser(t, pool, repo, "sweepcasclear", "active")
		linkedItemRepo := NewLinkedItemRepository(pool, enc)

		// Seed a LIVE linked_items row BEFORE the cancellation — non-negotiable (see
		// this test's doc comment above). With zero linked_items rows,
		// assertPlaidMarkerInvariant's non-NULL branch always lands in the #1453 zero-rows
		// exception (assert.Equal(t, 0, total) trivially true) and its NULL branch always lands in
		// assert.Equal(t, 0, live) (also trivially true) — BOTH branches would pass no matter what
		// this subtest does, exactly the vacuity found in five subtests of this sweep in an
		// earlier round.
		var itemID string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'active')
			RETURNING id
		`, user.ID, "item-"+user.ID).Scan(&itemID))
		assertPlaidMarkerInvariant(t, pool, user.ID, "cas-clear: initial, before cancellation")

		// Step 1: the real production writer for a joint status+marker transition into
		// ineligibility — UpdateSubscriptionFromPaddle('cancelled') — starts the episode.
		require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "cancelled", nil, nil, nil))
		afterCancel := readPlaidMarkerRow(t, pool, user.ID)
		require.NotNil(t, afterCancel.marker, "cancellation must start the episode's marker")
		marker := *afterCancel.marker
		assertPlaidMarkerInvariant(t, pool, user.ID, "cas-clear: after cancellation")

		// Step 2: the real disconnect writer — the "disconnect" half of Issue #1457 Gap 1.
		require.NoError(t, linkedItemRepo.MarkItemsDisconnected(ctx, []string{itemID}))
		var itemStatus *string
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM linked_items WHERE id = $1`, itemID).Scan(&itemStatus))
		require.NotNil(t, itemStatus)
		require.Equal(t, "disconnected", *itemStatus, "precondition: MarkItemsDisconnected must actually have transitioned the item's status, not just returned nil")

		// The intermediate window — the entire point of this subtest. The marker is STILL
		// non-NULL (step 1's value, untouched: disconnecting an item never itself touches this
		// column), but the only linked item is now 'disconnected' (live == 0): cleanup has, in
		// reality, already completed, and the CAS clear (step 3) simply hasn't run yet.
		//
		// assertPlaidMarkerInvariant is deliberately NOT called here, unlike every other step in
		// this subtest. Its non-NULL branch treats "live == 0 but total > 0" as a genuine
		// violation ("cleanup already completed and the marker should have been cleared") — the
		// correct STEADY-STATE reading, but this instant is not steady state: production's own
		// repairPlaidDisconnects pass calls MarkItemsDisconnected and
		// ClearPlaidCleanupPendingIfMatches as two SEPARATE sequential writes
		// (retention_cleanup_check.go), never atomically together, so this exact
		// non-NULL-marker/zero-live-items window is a real, expected, and temporary state in the
		// DOCUMENTED direction — domain.User.PlaidCleanupPendingAt's own doc comment: "An episode
		// ENDS the instant Plaid cleanup for THAT SPECIFIC episode completes — i.e. the moment
		// the marker is cleared for it", NOT the instant the linked item is actually disconnected.
		// Calling the helper here would assert a state it was never designed to tolerate, purely
		// because this subtest compresses two sequential production writes into one test body.
		// This mirrors branch1_backstop_heals_stale_marker_on_reactivation's own precedent
		// (above): assert the raw column state directly when the helper cannot express the exact
		// in-between state under test, rather than contorting the helper to special-case it.
		midState := readPlaidMarkerRow(t, pool, user.ID)
		require.NotNil(t, midState.marker, "the marker must still be set immediately after the disconnect — only the CAS clear (next step) ends the episode")
		assert.True(t, marker.Equal(*midState.marker), "the marker must still be the SAME value cancellation set — disconnecting an item must not itself touch it")
		_, liveAfterDisconnect := plaidMarkerLinkedItemCounts(t, pool, user.ID)
		assert.Equal(t, 0, liveAfterDisconnect, "the only linked item must now be disconnected (not live) — this is the state the CAS clear below is about to observe")

		// Step 3: the CAS-guarded clear, keyed to the EXACT value read at candidate-selection
		// time — mirroring repairPlaidDisconnects's real call to ClearPlaidCleanupPendingIfMatches.
		// This is the "... CAS-clear" half of Issue #1457 Gap 1, and the instant the episode
		// actually ends per the domain doc's own definition.
		cleared, err := repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, marker)
		require.NoError(t, err)
		require.True(t, cleared, "the CAS clear must succeed — the marker has not moved since cancellation set it")
		assertPlaidMarkerInvariant(t, pool, user.ID, "cas-clear: after the CAS-guarded clear")

		// A CAS mismatch (clearing with a STALE timestamp) leaving the marker in place is already
		// covered generically, elsewhere in this file —
		// TestClearPlaidCleanupPendingIfMatches_Integration_SkipsWhenMismatched, above — so it is
		// deliberately not duplicated here.
	})
}
