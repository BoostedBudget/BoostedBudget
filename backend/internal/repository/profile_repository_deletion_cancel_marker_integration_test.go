//go:build integration

package repository

// Integration test for Issue #1413's live-production defect (Task JA, PR #1443 round-7 gap
// analysis, highest severity): cancelling a GDPR deletion request must not leave a lapsed user's
// stale plaid_cleanup_pending_at marker in place, because that marker is the ONLY grace-window
// anchor GetUsersNeedingPlaidDisconnectRepair (profile_repository.go) uses to decide whether to
// sever a user's live bank connections.
//
// The defect: GetUsersNeedingPlaidDisconnectRepair, StampMissingPlaidCleanupMarkers, and
// ClearCompletedPlaidCleanupMarkers all filter `deletion_requested_at IS NULL` — so while a
// deletion request is pending, the marker is completely frozen: no backstop can touch it, and the
// repair candidate query cannot see the user at all. CancelDeletion then clears
// deletion_requested_at/deletion_scheduled_at and NOTHING else, making the user visible again to
// the repair candidate query with whatever marker value it froze at — which, for a user who has
// been in this state for weeks or months, is far past the 72-hour grace window. The very next
// repairPlaidDisconnects pass (which runs on every process boot, RunOnStart: true) then disconnects
// every one of that user's bank connections with ZERO grace, at the exact moment they signalled
// they wanted to keep the account.
//
// The fix: CancelDeletion's UPDATE also sets plaid_cleanup_pending_at = NULL. NULL (not NOW())
// delegates the decision to StampMissingPlaidCleanupMarkers, which only re-stamps a fresh NOW()
// if an episode is genuinely still open (ineligible status AND a live linked item AND no active
// comp) — see profile_repository.go's CancelDeletion doc comment for the full "two facts, two
// sources" rationale this mirrors.
//
// Falsifiability: reverting CancelDeletion to omit the plaid_cleanup_pending_at = NULL assignment
// makes this test's first assertion fail — the cancelling user WOULD be selected by
// GetUsersNeedingPlaidDisconnectRepair immediately after cancellation, which is the bug this test
// exists to catch.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestCancelDeletion_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelDeletion_Integration_ClearsStaleMarkerRestoresFullGraceWindow(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// A lapsed ("cancelled") user with a LIVE linked item — exactly the population the repair
	// query/backstops care about.
	user := createPlaidMarkerTestUser(t, pool, repo, "canceldeletionmarker", "cancelled")
	seedLinkedItem(t, pool, user.ID, "item-canceldeletionmarker-"+user.ID, "active")

	// Freeze the marker well beyond the 72h grace window, modeling "weeks or months pass while a
	// deletion request sits open" — no production writer can backdate a timestamp like this, so a
	// direct SQL helper is required here (the established convention throughout this package's
	// marker tests; see setPlaidCleanupPendingAt's own doc comment).
	staleMarker := time.Now().Add(-testGracePeriod - 200*time.Hour)
	setPlaidCleanupPendingAt(t, pool, user.ID, staleMarker)

	// File the deletion request through the REAL production writer (MarkForDeletion), not a raw
	// SQL UPDATE — this file's whole purpose is auditing CancelDeletion, the paired writer, against
	// the invariant, and a setup that bypasses the sibling writer under test cannot detect a
	// regression in either.
	scheduledAt := time.Now().Add(30 * 24 * time.Hour)
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))

	// Precondition: while the deletion request is pending, the user must be invisible to the
	// repair candidate query — this is the existing, correct half of the behavior; the defect is
	// entirely in what happens after cancellation.
	pendingCandidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range pendingCandidates {
		assert.NotEqual(t, user.ID, c.ID, "precondition: a user with a pending deletion request must be invisible to the repair candidate query")
	}

	// Act: the user changes their mind and cancels the deletion.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	// Discriminating assertion — this is the one that locks in NULL rather than NOW(), and without it
	// EVERY other assertion in this test is satisfied by the plausible WRONG fix. If CancelDeletion
	// wrote clock_timestamp() directly instead of NULL: the candidate query below would not select a
	// fresh marker either; StampMissingPlaidCleanupMarkers would simply SKIP this row (its WHERE
	// requires plaid_cleanup_pending_at IS NULL), leaving the value CancelDeletion had just written —
	// still inside the WithinDuration tolerance further down. So the whole test would pass while the
	// decision it exists to protect had been reversed.
	//
	// Writing NOW() directly is wrong because it stamps a marker unconditionally, including for a user
	// whose episode is NOT open (e.g. they resubscribed while the deletion request was pending) —
	// manufacturing exactly the phantom rows #1453 tracks. NULL delegates that judgement to the
	// backstop, which stamps only when an episode is genuinely open. Two facts, two sources.
	afterCancel := readPlaidMarkerRow(t, pool, user.ID)
	assert.Nil(t, afterCancel.marker,
		"CancelDeletion must NULL plaid_cleanup_pending_at and let StampMissingPlaidCleanupMarkers decide whether an episode is still open — writing NOW() directly would stamp a marker even for a user whose episode has already closed")

	// THE assertion that matters: immediately after cancellation, with no repair pass having run
	// yet, this user must NOT be selected for repair. Before the fix, they ARE selected — the
	// stale marker (frozen at staleMarker, now visible again) is already far past the grace
	// window, so the very next repairPlaidDisconnects pass would disconnect every bank connection
	// with zero grace.
	candidatesAfterCancel, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	found := false
	for _, c := range candidatesAfterCancel {
		if c.ID == user.ID {
			found = true
		}
	}
	assert.False(t, found, "cancelling a deletion request must not leave a stale plaid_cleanup_pending_at marker that gets a user's banks disconnected with zero grace — CancelDeletion must null the marker")

	// Follow-through: the self-healing backstop restamps a FRESH marker for this still-open
	// episode (ineligible status + live item + no comp) within the same pass a real
	// repairPlaidDisconnects run would perform it in (Stamp runs before the candidate query) — so
	// the user gets a genuine, full 72-hour grace window, not zero.
	_, err = repo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)

	row := readPlaidMarkerRow(t, pool, user.ID)
	require.NotNil(t, row.marker, "the backstop must re-arm the marker for this still-open episode after cancellation clears the stale value")
	assert.WithinDuration(t, time.Now(), *row.marker, 5*time.Second, "the restamped marker must be FRESH (now), not resurrect the stale pre-deletion-request value — grace must restart cleanly")
	assertPlaidMarkerInvariant(t, pool, user.ID, "after cancel deletion + stamp backstop")

	// And the restamped marker is fresh, so the candidate query must NOT select the user again
	// immediately — proving the full 72h grace window is genuinely restored, not skipped.
	candidatesAfterStamp, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	for _, c := range candidatesAfterStamp {
		assert.NotEqual(t, user.ID, c.ID, "a freshly-restamped marker must not be immediately past the grace window")
	}
}
