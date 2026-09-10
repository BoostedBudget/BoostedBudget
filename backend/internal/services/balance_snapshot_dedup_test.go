package services

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1569 Task 20 (S8). createBalanceSnapshots' deduplication was purely time-based: if ANY
// snapshot existed for the account within five minutes, the incoming one was dropped, whatever it
// said.
//
// So two genuinely different balances arriving less than five minutes apart — a paycheck landing
// between a webhook and a manual refresh, two webhooks for one settlement burst — silently lost the
// later, truer figure. Note the direction, which the census got backwards: it is the INCOMING write
// that is skipped, so the row that survives is the OLDER one and the balance history goes on
// recording a number that has already stopped being true. Nothing else records it; this table is
// the only place a balance at an instant is ever written down.
//
// The window's stated purpose was to "REDUCE excessive snapshots" from high-frequency syncs — i.e.
// to stop the SAME number being written repeatedly. Adding the value test keeps that whole purpose
// and stops it discarding information.

func f(v float64) *float64 { return &v }

func TestIsRedundantSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	const window = 5 * time.Minute
	recent := now.Add(-1 * time.Minute)
	old := now.Add(-6 * time.Minute)

	cases := []struct {
		name   string
		latest *domain.BalanceSnapshot
		next   *domain.BalanceSnapshot
		want   bool
		why    string
	}{
		{
			name:   "identical figures inside the window are redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100), AvailableBalance: f(90)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100), AvailableBalance: f(90)},
			want:   true,
			why:    "this is the whole point of the window — a high-frequency sync re-reporting the same number",
		},
		{
			name:   "a CHANGED current balance inside the window is NOT redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100), AvailableBalance: f(90)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(2100), AvailableBalance: f(90)},
			want:   false,
			why:    "the defect: a real balance change four minutes after the last sync was thrown away",
		},
		{
			name:   "a CHANGED available balance alone is NOT redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100), AvailableBalance: f(90)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100), AvailableBalance: f(40)},
			want:   false,
			why:    "a hold clearing moves available without moving current; comparing only current would drop it",
		},
		{
			name:   "known -> unknown available balance is a change",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100), AvailableBalance: f(90)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100)},
			want:   false,
			why:    "NULL is a value here, not a wildcard — the provider stopped reporting it, and that is news",
		},
		{
			name:   "unknown -> known available balance is a change",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100), AvailableBalance: f(90)},
			want:   false,
			why:    "the mirror of the case above; a one-sided nil check would get one of the two wrong",
		},
		{
			name:   "both unknown, same current, inside the window IS redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: recent, CurrentBalance: f(100)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100)},
			want:   true,
			why:    "two NULLs are the same statement, so this must still dedup",
		},
		{
			name:   "identical figures OUTSIDE the window are not redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: old, CurrentBalance: f(100), AvailableBalance: f(90)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100), AvailableBalance: f(90)},
			want:   false,
			why:    "the periodic heartbeat that lets a chart show a flat line rather than a gap",
		},
		{
			name:   "exactly at the window boundary is not redundant",
			latest: &domain.BalanceSnapshot{SnapshotAt: now.Add(-window), CurrentBalance: f(100)},
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100)},
			want:   false,
			why:    "the original predicate was `< window`; the boundary must not change behaviour",
		},
		{
			name:   "no previous snapshot is never redundant",
			latest: nil,
			next:   &domain.BalanceSnapshot{SnapshotAt: now, CurrentBalance: f(100)},
			want:   false,
			why:    "the first snapshot for an account must always be written",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isRedundantSnapshot(tc.latest, tc.next, now, window), tc.why)
		})
	}
}

// TestIsRedundantSnapshot_NeverDedupsAcrossAUTCDayBoundary is #1626 gap G7.
//
// THE DEFECT. The window is five minutes of ELAPSED TIME and knows nothing about the UTC day, but
// the gap-filler's entire unit of work IS the UTC day: balanceCaptureService.CaptureItemBalances
// asks AccountsMissingDailySnapshot whether TODAY's cohort is complete, and migration 000194's
// partial unique index is keyed on the UTC date. So a capture landing in the 00:00–00:05 UTC
// window — a RunOnStart deploy just after midnight, or River retrying a 23:5x attempt that failed —
// saw the account as missing today's row, deduped against YESTERDAY's 23:58 identical row, wrote
// nothing, and returned success. The day it was asked to fill stayed empty, permanently, with no
// backfill and nothing in the outcome counters but a Deduped++.
//
// ⚠ WHY THE EXISTING TABLE ABOVE COULD NOT CATCH IT: every one of its fixtures sits at 12:00 UTC
// with offsets of minutes, so `now` and `latest.SnapshotAt` are always the same calendar day. The
// defect is unreachable without a fixture that straddles midnight, which is exactly why it survived.
//
// ⚠ AND THE FIXTURES BELOW ARE DELIBERATELY NOT ON THE BOUNDARY THE CODE WOULD GENERATE ANYWAY. A
// pair at 23:59:59 / 00:00:00 is one second apart AND on different days, so it would be killed by
// the day gate and by nothing else — but it would also pass if the implementation used any
// second-resolution proxy for "different day". The pair used here is 23:58 / 00:02, four minutes
// apart: comfortably INSIDE the five-minute window (so the elapsed-time test says "redundant") and
// on different UTC days (so the day gate must override it). Only a real day comparison satisfies it.
func TestIsRedundantSnapshot_NeverDedupsAcrossAUTCDayBoundary(t *testing.T) {
	const window = 5 * time.Minute

	// 00:02 UTC on the 25th, against an identical row written at 23:58 UTC on the 24th.
	justAfterMidnight := time.Date(2026, 8, 25, 0, 2, 0, 0, time.UTC)
	lateYesterday := time.Date(2026, 8, 24, 23, 58, 0, 0, time.UTC)
	require.Less(t, justAfterMidnight.Sub(lateYesterday), window,
		"fixture guard: the two instants must be INSIDE the dedup window, or this test would pass "+
			"on the elapsed-time test alone and prove nothing about the day gate")

	latest := &domain.BalanceSnapshot{SnapshotAt: lateYesterday, CurrentBalance: f(100), AvailableBalance: f(90)}
	next := &domain.BalanceSnapshot{SnapshotAt: justAfterMidnight, CurrentBalance: f(100), AvailableBalance: f(90)}

	assert.False(t, isRedundantSnapshot(latest, next, justAfterMidnight, window),
		"an identical snapshot from YESTERDAY must never suppress today's row. The gap-filler's unit "+
			"of work is the UTC day (AccountsMissingDailySnapshot, migration 000194's date-keyed "+
			"index), so suppressing here writes nothing for a day the cohort check just reported as "+
			"missing — and returns success while doing it")

	// The control that stops the fix from being "never dedup near midnight": the SAME clock times,
	// both on the new day, must still dedup. Without this, a mutation that simply disabled the
	// window whenever the hour was small would pass the assertion above.
	sameDayEarly := time.Date(2026, 8, 25, 0, 1, 0, 0, time.UTC)
	sameDayLater := time.Date(2026, 8, 25, 0, 4, 0, 0, time.UTC)
	assert.True(t, isRedundantSnapshot(
		&domain.BalanceSnapshot{SnapshotAt: sameDayEarly, CurrentBalance: f(100), AvailableBalance: f(90)},
		&domain.BalanceSnapshot{SnapshotAt: sameDayLater, CurrentBalance: f(100), AvailableBalance: f(90)},
		sameDayLater, window),
		"two identical snapshots minutes apart on the SAME UTC day must still dedup — the day gate "+
			"must be a day comparison, not a 'near midnight' exemption")

	// And the day gate must not resurrect a dedup the VALUE test already refused: a changed balance
	// on the same day stays non-redundant. This pins that the two conditions compose as AND.
	assert.False(t, isRedundantSnapshot(
		&domain.BalanceSnapshot{SnapshotAt: sameDayEarly, CurrentBalance: f(100)},
		&domain.BalanceSnapshot{SnapshotAt: sameDayLater, CurrentBalance: f(2100)},
		sameDayLater, window),
		"a changed balance on the same day is still not redundant")

	// The day is UTC, not the wall-clock day of whatever zone the instants happen to carry.
	//
	// ⚠ BOTH operands are re-expressed in UTC+13, and that is the only shape that falsifies the
	// choice. In UTC+13 these two instants read 12:58 and 13:02 on the SAME local date (the 25th),
	// while their UTC dates are the 24th and the 25th. So a comparison that dropped the .UTC()
	// conversion would call them the same day and dedup — losing the row for UTC's 25th, which is
	// the day AccountsMissingDailySnapshot asked about and the day migration 000194's index is keyed
	// on. Converting only ONE operand does NOT falsify it: a UTC `latest` against a UTC+13 `now`
	// still differs by local date, so that fixture passes with or without the conversion. It was
	// written that way first and a mutation dropping both .UTC() calls survived it.
	plusThirteen := time.FixedZone("UTC+13", 13*3600)
	localLatest := &domain.BalanceSnapshot{
		SnapshotAt: lateYesterday.In(plusThirteen), CurrentBalance: f(100), AvailableBalance: f(90),
	}
	require.Equal(t, justAfterMidnight.In(plusThirteen).Day(), localLatest.SnapshotAt.Day(),
		"fixture guard: in this zone both instants must fall on the SAME local date, or the test "+
			"would pass on the local comparison too and prove nothing about UTC")
	assert.False(t, isRedundantSnapshot(localLatest, next, justAfterMidnight.In(plusThirteen), window),
		"the day comparison must be in UTC. These two instants share a LOCAL date in UTC+13 but "+
			"straddle UTC midnight, so a local-zone comparison would dedup and lose the row for "+
			"UTC's 25th — the day the cohort check asked about")
}

func TestSameBalance_NilIsAValueNotAWildcard(t *testing.T) {
	assert.True(t, sameBalance(nil, nil))
	assert.False(t, sameBalance(nil, f(0)), "NULL and 0.00 are different statements about an account")
	assert.False(t, sameBalance(f(0), nil))
	assert.True(t, sameBalance(f(-12.34), f(-12.34)))
	assert.False(t, sameBalance(f(-12.34), f(-12.35)))
}
