package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asOf is the fixed clock every case below is expressed relative to. Issue #1466's lesson applied
// here from the start: a state derivation that reads time.Now() internally cannot be tested for the
// boundary it exists to enforce, so AdminState takes asOf and these tests are the reason.
var orphanStateAsOf = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

// newOrphan builds the ONE state this table's rows are in the moment DeleteUserCascade writes them:
// a live retained token, no retry yet, unresolved. Every case below mutates exactly one field away
// from it, so each assertion names a single cause.
func newOrphan() PlaidOrphanedItem {
	return PlaidOrphanedItem{
		ID:             "11111111-1111-1111-1111-111111111111",
		LinkedItemID:   "22222222-2222-2222-2222-222222222222",
		PlaidItemID:    "item-abc",
		OrphanedAt:     orphanStateAsOf.Add(-1 * time.Hour),
		AccessToken:    "access-sandbox-live",
		TokenExpiresAt: orphanStateAsOf.Add(OrphanedItemTokenRetention),
	}
}

func TestPlaidOrphanedItem_AdminState_FreshlyOrphanedRowIsAutoRetrying(t *testing.T) {
	assert.Equal(t, OrphanedItemAutoRetrying, newOrphan().AdminState(orphanStateAsOf))
}

func TestPlaidOrphanedItem_AdminState_ResolvedBeatsEverythingElse(t *testing.T) {
	o := newOrphan()
	resolved := orphanStateAsOf.Add(-30 * time.Minute)
	o.ResolvedAt = &resolved
	assert.Equal(t, OrphanedItemResolved, o.AdminState(orphanStateAsOf),
		"a resolved row needs no human action at all and must never be presented as work")
}

// The state this whole table exists to surface: token gone, Item possibly still live and billing,
// nothing automated left to try.
func TestPlaidOrphanedItem_AdminState_PurgedTokenRequiresManualIntervention(t *testing.T) {
	o := newOrphan()
	o.AccessToken = ""
	purged := orphanStateAsOf.Add(-10 * time.Minute)
	o.TokenPurgedAt = &purged
	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf))
}

// Rows written before migration 000135 never held a credential. They are indistinguishable from
// purged rows in terms of what an operator must now do, and must NOT read as auto-retrying.
func TestPlaidOrphanedItem_AdminState_NeverRetainedTokenRequiresManualIntervention(t *testing.T) {
	o := newOrphan()
	o.AccessToken = ""
	require.Nil(t, o.TokenPurgedAt, "the pre-000135 shape: no token, and no purge ever recorded")
	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf))
}

// The expiry boundary, asserted on both sides of the same instant. An expired token is not a
// credential this system may use, whether or not the purge sweep has caught up with it yet — so the
// state flips on the deadline, not on the purge.
func TestPlaidOrphanedItem_AdminState_ExpiryBoundaryIsExclusive(t *testing.T) {
	o := newOrphan()
	o.TokenExpiresAt = orphanStateAsOf.Add(1 * time.Nanosecond)
	assert.Equal(t, OrphanedItemAutoRetrying, o.AdminState(orphanStateAsOf),
		"one nanosecond before the deadline the token is still usable")

	o.TokenExpiresAt = orphanStateAsOf
	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf),
		"at the deadline the token is spent, even though the purge sweep has not run yet")
}

// A rotated encryption key makes the retained ciphertext permanently useless, and no retry can
// recover from it while the key is missing. The row must stop claiming to be auto-retrying even
// though its token is present and unexpired.
func TestPlaidOrphanedItem_AdminState_UndecryptableTokenRequiresManualIntervention(t *testing.T) {
	o := newOrphan()
	o.LastRetryErrorCode = OrphanTokenUndecryptableCode
	last := orphanStateAsOf.Add(-1 * time.Minute)
	o.LastRetryAt = &last
	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf))
}

// An ordinary Plaid failure is NOT terminal: the row keeps claiming auto-retrying, because it is.
func TestPlaidOrphanedItem_AdminState_OrdinaryRetryFailureStaysAutoRetrying(t *testing.T) {
	o := newOrphan()
	o.LastRetryErrorCode = "INTERNAL_SERVER_ERROR"
	last := orphanStateAsOf.Add(-1 * time.Minute)
	o.LastRetryAt = &last
	o.RetryAttemptCount = 9
	assert.Equal(t, OrphanedItemAutoRetrying, o.AdminState(orphanStateAsOf),
		"nothing in this system gives up on an attempt count; only the expiry ends the retrying")
}

// THE SIXTH-INSTANCE GUARD, and the reason AdminState is not simply "token live => auto-retrying".
// "Auto-retrying" is a claim about a JOB, and this row cannot see whether that job is registered,
// wired, or starved. So the claim DECAYS: if no retry has been observed within
// OrphanedItemRetryStaleAfter, the row stops asserting that something is working on it — whatever
// the cause — and asks for a human instead. Over-reporting manual work is recoverable; a row that
// says "auto-retrying" for 30 days while nothing retries is the failure this branch keeps finding.
func TestPlaidOrphanedItem_AdminState_LiveTokenWithNoRetryForTooLongStopsClaimingAutoRetry(t *testing.T) {
	o := newOrphan()
	o.OrphanedAt = orphanStateAsOf.Add(-OrphanedItemRetryStaleAfter - time.Minute)
	require.Nil(t, o.LastRetryAt, "no retry has ever run for this record")
	require.True(t, o.TokenExpiresAt.After(orphanStateAsOf), "and its token is still perfectly live")

	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf),
		"the retry sweep is demonstrably not running for this row, so the row must not claim it is")
}

func TestPlaidOrphanedItem_AdminState_StaleLastRetryStopsClaimingAutoRetry(t *testing.T) {
	o := newOrphan()
	o.OrphanedAt = orphanStateAsOf.Add(-20 * 24 * time.Hour)
	last := orphanStateAsOf.Add(-OrphanedItemRetryStaleAfter - time.Minute)
	o.LastRetryAt = &last
	o.RetryAttemptCount = 3

	assert.Equal(t, OrphanedItemManualInterventionRequired, o.AdminState(orphanStateAsOf),
		"retries used to happen for this row and have stopped; that is the same operator story")
}

// The grace half of the same rule: a row younger than the staleness horizon has not yet had time to
// be retried, and must not be reported as needing a human on its first day.
func TestPlaidOrphanedItem_AdminState_RecentlyOrphanedRowIsNotYetStale(t *testing.T) {
	o := newOrphan()
	o.OrphanedAt = orphanStateAsOf.Add(-OrphanedItemRetryStaleAfter + time.Minute)
	require.Nil(t, o.LastRetryAt)
	assert.Equal(t, OrphanedItemAutoRetrying, o.AdminState(orphanStateAsOf))
}

// TestOrphanedItemTokenRetention_IsThirtyDays pins the retention period itself, because it is the
// GDPR commitment this feature is defensible on and a silent change to it is a silent change to that
// commitment. It is deliberately NOT asserted against config.DeletionGracePeriodDays — see the
// constant's doc comment for why the two are equal by argument rather than by wiring.
func TestOrphanedItemTokenRetention_IsThirtyDays(t *testing.T) {
	assert.Equal(t, 30*24*time.Hour, OrphanedItemTokenRetention)
}

// The two clocks must not be conflated (the brief for #1497 says so explicitly, and this branch has
// unpicked that coupling before). Nothing in code can stop someone writing
// OrphanedItemTokenRetention = uncleanedLinkedItemFailedRemovalGrace; this can.
func TestOrphanedItemTokenRetention_IsNotTheC2CoolingOffWindow(t *testing.T) {
	assert.NotEqual(t, 7*24*time.Hour, OrphanedItemTokenRetention,
		"the C2 window bounds how long an erasure waits; this bounds how long a credential outlives one")
}
