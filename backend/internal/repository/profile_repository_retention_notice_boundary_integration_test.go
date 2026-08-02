//go:build integration

package repository

// Integration test for the four GDPR retention-notice boundary predicates in
// profile_repository.go — IsRetentionNotice30dEligibleForOccurrence (:2051),
// IsRetentionNotice7dEligibleForOccurrence (:2075), GetUsersNeedingRetention30dNotice (:2206),
// and GetUsersNeedingRetention7dNotice (:2230) — all four of which gate on
// `retention_expires_at - INTERVAL '<n> days' <= NOW()`.
//
// Issue #1461 pinned database.NewPool's session TimeZone to UTC because
// `clock_timestamp() + make_interval(months => N)` — used to WRITE retention_expires_at — is
// calendar arithmetic and PostgreSQL evaluates it in the SESSION time zone. The #1461 sweep's
// commit message claimed only two lines (the CURRENT_DATE writes in
// savings_contribution_repository.go) were genuinely session-TZ-dependent; that undercounts by
// four. `INTERVAL '<n> days'` subtracted from a timestamptz is the exact same hazard class as
// make_interval(months => N): PostgreSQL evaluates an interval's CALENDAR components
// (years/months/weeks/days) in the session zone, preserving local wall-clock time across the
// subtraction rather than moving the absolute instant by an exact duration, while its
// FIXED-DURATION components (hours/mins/secs) are exact and session-independent. `INTERVAL '30
// days'` has a `days` component, so it is affected; it is NOT equivalent to `INTERVAL '720 hours'`
// once a DST boundary sits between the anchor and the result, even though both "look like" 30
// days.
//
// This is currently latent, not live: database.NewPool's UTC pin (pgx.go) makes all four sites
// deterministic in practice, the same way it does for make_interval(months). This test exists so
// that fact is asserted rather than assumed, as an untested sibling of the make_interval guard
// (TestDbAddMonths_Integration_IndependentOfSessionTimeZone,
// admin_user_repository_remove_tier_override_integration_test.go) already covers.
//
// Shape chosen: session-zone contrast on the raw expression, not a behavioural test through the
// eligibility methods themselves. Those methods compare against NOW() at the moment they run
// (real wall-clock "today"), which cannot be pinned to a specific historical DST boundary in a
// deterministic CI run — a behavioural test would only exercise the hazard on the handful of days
// a year a DST transition actually falls within the relevant window. Evaluating the identical
// `<timestamptz> - INTERVAL '<n> days'` fragment directly, against a fixed anchor, lets the test
// run any day of the year while still crossing a real transition.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionNoticeBoundary_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retentionNotice30dBoundaryQuery and retentionNotice7dBoundaryQuery are verbatim copies of the
// `retention_expires_at - INTERVAL '<n> days'` fragment from
// IsRetentionNotice30dEligibleForOccurrence/GetUsersNeedingRetention30dNotice and their 7-day
// siblings (profile_repository.go). Deliberately NOT pinned with `AT TIME ZONE 'UTC'` here, unlike
// dbAddMonthsQuery above in this package — production does not pin these either; the point of this
// test is to show the expression's result depends on the session, exactly as it does today.
const (
	retentionNotice30dBoundaryQuery = `SELECT $1::timestamptz - INTERVAL '30 days'`
	retentionNotice7dBoundaryQuery  = `SELECT $1::timestamptz - INTERVAL '7 days'`
)

// TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary is the regression guard for the four
// `retention_expires_at - INTERVAL '<n> days'` sites Issue #1461's sweep missed.
//
// anchor = 2026-03-10 12:00:00 UTC, two days after the 2026-03-08 America/New_York spring-forward
// transition (01:00 EST -> 03:00 EDT, i.e. 07:00 UTC — confirmed against a live Postgres 17
// instance before writing this test). Subtracting either 7 or 30 calendar days from the anchor
// crosses back over that same transition into EST.
//
// Expected arithmetic (verified against a live Postgres 17 instance before writing these
// assertions, not assumed from documentation alone):
//   - Evaluated with the session pinned to UTC (matching database.NewPool's Issue #1461 pin —
//     the connectTestDB pool below routes through it), UTC has no DST, so calendar-day
//     subtraction is an exact 7-or-30-day move: 2026-03-03 12:00:00 UTC / 2026-02-08 12:00:00 UTC.
//   - Evaluated on a connection forced to America/New_York (simulating the pin lost or bypassed —
//     Issue #1461's confirmed unpinned local default), the SAME expression instead preserves the
//     anchor's LOCAL wall-clock reading (08:00 — March 10 is already EDT) across the subtraction,
//     landing on a day that is back in EST (UTC-5, not EDT's UTC-4) — one hour LATER in absolute
//     terms: 2026-03-03 13:00:00 UTC / 2026-02-08 13:00:00 UTC.
//
// Falsified by changing retentionNotice30dBoundaryQuery/7dBoundaryQuery's `INTERVAL '30
// days'`/`'7 days'` to the equivalent-LOOKING but FIXED-DURATION `INTERVAL '720 hours'`/`'168
// hours'`: hours are exact and session-independent, so both sessions then agree and the "differs
// by exactly one hour" assertion below goes RED. Confirmed RED with that mutation applied to the
// query constants above (not a comment), then restored — see the PR/task report for the verbatim
// failure output.
func TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	anchor := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	// Force ONE connection's session TimeZone to America/New_York, simulating Issue #1461's pin
	// having been lost or bypassed. Acquired (not just SET on the pool) so the pool's other
	// connections remain UTC-pinned for the pool.QueryRow calls below to use.
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "SET TIME ZONE 'America/New_York'")
	require.NoError(t, err)

	cases := []struct {
		name    string
		query   string
		wantUTC time.Time
		wantNY  time.Time
	}{
		{
			name:    "30d",
			query:   retentionNotice30dBoundaryQuery,
			wantUTC: time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC),
			wantNY:  time.Date(2026, 2, 8, 13, 0, 0, 0, time.UTC),
		},
		{
			name:    "7d",
			query:   retentionNotice7dBoundaryQuery,
			wantUTC: time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC),
			wantNY:  time.Date(2026, 3, 3, 13, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var utcResult time.Time
			require.NoError(t, pool.QueryRow(ctx, tc.query, anchor).Scan(&utcResult))
			assert.True(t, tc.wantUTC.Equal(utcResult.UTC()),
				"UTC-session result: got %v, want %v", utcResult.UTC(), tc.wantUTC)

			var nyResult time.Time
			require.NoError(t, conn.QueryRow(ctx, tc.query, anchor).Scan(&nyResult))
			assert.True(t, tc.wantNY.Equal(nyResult.UTC()),
				"America/New_York-session result: got %v, want %v", nyResult.UTC(), tc.wantNY)

			assert.False(t, utcResult.UTC().Equal(nyResult.UTC()),
				"expected the session TimeZone to change the absolute instant across this DST "+
					"boundary — got the same result (%v) in both sessions, which would mean the "+
					"UTC pool pin (pgx.go) is not actually load-bearing for this expression",
				utcResult.UTC())
			assert.Equal(t, time.Hour, nyResult.UTC().Sub(utcResult.UTC()),
				"expected exactly a 1-hour drift (one DST transition's worth) between sessions")
		})
	}
}
