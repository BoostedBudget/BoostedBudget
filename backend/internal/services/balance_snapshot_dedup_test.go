package services

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
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

func TestSameBalance_NilIsAValueNotAWildcard(t *testing.T) {
	assert.True(t, sameBalance(nil, nil))
	assert.False(t, sameBalance(nil, f(0)), "NULL and 0.00 are different statements about an account")
	assert.False(t, sameBalance(f(0), nil))
	assert.True(t, sameBalance(f(-12.34), f(-12.34)))
	assert.False(t, sameBalance(f(-12.34), f(-12.35)))
}
