package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1497 item 5: the /admin/health orphan count, and item 4: the repeated-re-attempt gauge.
// Both read the SAME derivation (SummarisePlaidOrphanedItems) so the JSON an operator reads on
// /admin/health and the Prometheus gauge an alert fires on can never disagree about how many rows
// are in which state.
//
// ⚠ WHY THESE TESTS SEED A REAL POPULATION rather than asserting field presence. A test that only
// checks the summary struct has a ManualInterventionRequired field passes against a hardcoded zero,
// which is precisely the class of check this branch keeps having to remove. Every assertion below
// names a NUMBER derived from rows whose state was constructed deliberately, and the two operator
// states are asserted to differ from each other — a summariser that conflated them (counting
// "unresolved" once, which is the tempting shortcut) fails.

// orphanRowAutoRetrying builds a row AdminState reports as auto_retrying: a live, decryptable
// credential with a retry observed inside OrphanedItemRetryStaleAfter.
func orphanRowAutoRetrying(now time.Time, attempts int) PlaidOrphanedItem {
	lastRetry := now.Add(-1 * time.Hour)
	return PlaidOrphanedItem{
		ID:                "auto",
		AccessToken:       "access-live",
		TokenExpiresAt:    now.Add(10 * 24 * time.Hour),
		OrphanedAt:        now.Add(-5 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanRowManualExpiredToken builds a row AdminState reports as manual_intervention_required via
// check 2 (a dead token) — the check migration 000135's deadline makes inevitable.
func orphanRowManualExpiredToken(now time.Time, attempts int) PlaidOrphanedItem {
	lastRetry := now.Add(-1 * time.Hour)
	return PlaidOrphanedItem{
		ID:                "manual-expired",
		AccessToken:       "access-spent",
		TokenExpiresAt:    now.Add(-1 * time.Minute),
		OrphanedAt:        now.Add(-40 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanRowManualStale builds a row AdminState reports as manual_intervention_required via check 4
// (a live token nothing has retried for OrphanedItemRetryStaleAfter).
func orphanRowManualStale(now time.Time, attempts int) PlaidOrphanedItem {
	lastRetry := now.Add(-OrphanedItemRetryStaleAfter - time.Hour)
	return PlaidOrphanedItem{
		ID:                "manual-stale",
		AccessToken:       "access-live",
		TokenExpiresAt:    now.Add(10 * 24 * time.Hour),
		OrphanedAt:        now.Add(-20 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanRowResolved builds a proved-removed row: history, nobody's work.
func orphanRowResolved(now time.Time, attempts int) PlaidOrphanedItem {
	resolved := now.Add(-3 * time.Hour)
	return PlaidOrphanedItem{
		ID:                "resolved",
		TokenExpiresAt:    now.Add(-1 * time.Hour),
		OrphanedAt:        now.Add(-31 * 24 * time.Hour),
		ResolvedAt:        &resolved,
		RetryAttemptCount: attempts,
	}
}

func TestSummarisePlaidOrphanedItems_CountsEachStateSeparately(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	items := []PlaidOrphanedItem{
		orphanRowAutoRetrying(now, 2),
		orphanRowAutoRetrying(now, 3),
		orphanRowManualExpiredToken(now, 9),
		orphanRowManualStale(now, 1),
		orphanRowManualStale(now, 1),
		orphanRowManualStale(now, 1),
		orphanRowResolved(now, 4),
	}

	got := SummarisePlaidOrphanedItems(items, now)

	assert.Equal(t, int64(2), got.AutoRetrying, "two rows hold a live credential with a recent observed retry")
	assert.Equal(t, int64(4), got.ManualInterventionRequired, "one expired-token row plus three stale-retry rows")
	assert.Equal(t, int64(1), got.Resolved, "one proved removal, kept as history")

	// THE SIBLING THAT MAKES THE COUNTS FALSIFIABLE: the two operator states must be DIFFERENT
	// numbers here, so a summariser that reported one "unresolved" total (2+4 = 6) for both — the
	// shortcut this feature exists to prevent, because auto-retrying and manual-intervention call
	// for opposite human action — cannot pass.
	assert.NotEqual(t, got.AutoRetrying, got.ManualInterventionRequired,
		"the fixture is deliberately asymmetric so the two states cannot be satisfied by one shared total")
	assert.Equal(t, int64(len(items)), got.AutoRetrying+got.ManualInterventionRequired+got.Resolved,
		"AdminState is total over these rows, so the three counts must partition the population exactly")
}

// Item 4's gauge. RetryAttemptCount is written by RecordRetryFailure and, before this, read by no
// metric at all — the omission named in #1497's own title.
func TestSummarisePlaidOrphanedItems_ReportsTheHighestUnresolvedAttemptCount(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	items := []PlaidOrphanedItem{
		orphanRowAutoRetrying(now, 4),
		orphanRowManualStale(now, 17),
		orphanRowAutoRetrying(now, 2),
	}

	got := SummarisePlaidOrphanedItems(items, now)

	assert.Equal(t, int64(17), got.MaxUnresolvedRetryAttemptCount,
		"the point of this gauge is the WORST row: an alert on 'something is being re-attempted over and over' cannot be expressed by a mean or by the last row scanned")
}

// ⚠ THE SIBLING THAT PINS "UNRESOLVED". A resolved row's attempt history is finished — it is
// history, not a live re-attempt loop — so letting it drive this gauge would keep an alert latched
// forever on work that succeeded, which is this branch's signature defect (a mark that outlives the
// work) wearing an alerting hat.
func TestSummarisePlaidOrphanedItems_ResolvedRowsDoNotDriveTheAttemptGauge(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	items := []PlaidOrphanedItem{
		orphanRowResolved(now, 99),
		orphanRowAutoRetrying(now, 3),
	}

	got := SummarisePlaidOrphanedItems(items, now)

	require.Equal(t, int64(1), got.Resolved, "the 99-attempt row must genuinely be in the resolved state, or this test proves nothing")
	assert.Equal(t, int64(3), got.MaxUnresolvedRetryAttemptCount,
		"the resolved row's 99 attempts are finished history and must not latch the gauge")
}

// asOf is honoured rather than the clock being read internally — #1466's rule. The SAME rows
// summarised at two instants must produce different states, because check 2 and check 4 are both
// time-dependent. Without this, the derivation could freeze a stale verdict.
func TestSummarisePlaidOrphanedItems_HonoursAsOf(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	items := []PlaidOrphanedItem{orphanRowAutoRetrying(now, 1)}

	atNow := SummarisePlaidOrphanedItems(items, now)
	require.Equal(t, int64(1), atNow.AutoRetrying)
	require.Equal(t, int64(0), atNow.ManualInterventionRequired)

	// Past the row's token deadline (10 days) the same row is manual work, by check 2.
	later := now.Add(11 * 24 * time.Hour)
	atLater := SummarisePlaidOrphanedItems(items, later)
	assert.Equal(t, int64(0), atLater.AutoRetrying)
	assert.Equal(t, int64(1), atLater.ManualInterventionRequired)
}

// An empty table is a REAL zero, and it must be reported as one — distinct from the unavailable
// case, which is the CALLER's job to mark (see PlaidOrphanedItemMetrics.Available). This test
// deliberately does not assert Available: a domain summariser asserting its own struct's Go zero
// value would be the tautology this exercise has already shipped once.
func TestSummarisePlaidOrphanedItems_EmptyPopulationIsAllZeroes(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	got := SummarisePlaidOrphanedItems(nil, now)

	assert.Equal(t, int64(0), got.AutoRetrying)
	assert.Equal(t, int64(0), got.ManualInterventionRequired)
	assert.Equal(t, int64(0), got.Resolved)
	assert.Equal(t, int64(0), got.MaxUnresolvedRetryAttemptCount)
}

// The cap is the honest answer to migrations 000134/000135's "single digits of rows" argument: that
// argument is about the RATE rows arrive at, and nothing prunes this table (no production DELETE
// exists, and MarkResolved only stamps resolved_at), so it grows monotonically. The cap turns an
// unbounded read into a bounded one whose overflow is reported rather than silently truncated.
func TestPlaidOrphanedItemHealthScanCap_IsFarAboveTheArguedTableSizeAndStillBounded(t *testing.T) {
	assert.Greater(t, PlaidOrphanedItemHealthScanCap, 100,
		"the cap must leave enormous headroom over the single-digit population migrations 000134/000135 argue for, so it is a tripwire and not a routine limit")
	assert.LessOrEqual(t, PlaidOrphanedItemHealthScanCap, 5000,
		"and it must stay small enough that loading and decrypting that many rows on an admin request is bounded work")
}
