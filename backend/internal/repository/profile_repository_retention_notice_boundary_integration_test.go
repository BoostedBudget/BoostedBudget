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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
// transition (02:00 EST -> 03:00 EDT, i.e. 07:00 UTC — confirmed against a live Postgres 17
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

// TestRetentionNoticeBoundary_Integration_ProductionAppliesConfiguredWindow is the companion to
// TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary above (Codex review finding: that
// test runs COPIED SQL constants — retentionNotice30dBoundaryQuery/7dBoundaryQuery — as a bare
// SELECT, never the real repository methods, so it proves PostgreSQL's interval arithmetic behaves
// a certain way in the abstract but would stay green even if
// IsRetentionNotice30dEligibleForOccurrence/7d or GetUsersNeedingRetention30dNotice/7dNotice
// (profile_repository.go ~:2056, :2084, :2224, :2252) stopped using that arithmetic at all).
//
// This test closes that gap by driving the REAL production methods — both the per-occurrence
// re-check pair and the set-based candidate-query pair the retention_cleanup_check job actually
// calls — against seeded retention_expires_at values straddling each method's own eligibility
// boundary (retention_expires_at - window <= NOW()), proving production genuinely applies a
// ~30-day / ~7-day window, in the right direction, through the real code path.
//
// Residual — be honest about what this pair (this test + the DST test above) does and does not
// cover, together:
//   - The DST test proves the arithmetic is session-time-zone-sensitive, and that
//     database.NewPool's UTC pin (pgx.go) makes it deterministic.
//   - THIS test proves production genuinely applies a 30d/7d window at all, in the right
//     direction, via the real repository methods.
//   - NEITHER, alone or together, proves production is immune to someone silently swapping
//     `INTERVAL '30 days'` for the equivalent-LOOKING but FIXED-DURATION `INTERVAL '720 hours'`
//     (or `'7 days'` for `'168 hours'`): hours are exact and session-independent, so away from an
//     actual DST transition the two forms produce the identical instant, and this test's
//     five-minute boundary margin — run on whatever day CI happens to execute it — cannot
//     distinguish "calendar days, UTC-pinned" from "exact hours" unless a real transition happens
//     to fall inside the probed window (which, on almost every day of the year, it does not).
//     Only TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary (which deliberately anchors
//     ON a real 2026 transition) can catch that specific substitution, and only for session
//     configurations where the substitution would actually change the result. Both tests are
//     needed; neither is redundant with the other.
func TestRetentionNoticeBoundary_Integration_ProductionAppliesConfiguredWindow(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// Comfortably larger than any plausible test/DB round-trip latency, small relative to the
	// 7-day/30-day windows being probed — "just past"/"just short of" the boundary, not merely
	// "somewhere inside/outside" like the coarser (25d/45d/10d) fixtures elsewhere in this package.
	const margin = 5 * time.Minute

	tests := []struct {
		name       string
		window     time.Duration
		eligibleFn func(ctx context.Context, userID string, expected time.Time) (bool, error)
		listFn     func(ctx context.Context) ([]*domain.User, error)
	}{
		{"30d", 30 * 24 * time.Hour, repo.IsRetentionNotice30dEligibleForOccurrence, repo.GetUsersNeedingRetention30dNotice},
		{"7d", 7 * 24 * time.Hour, repo.IsRetentionNotice7dEligibleForOccurrence, repo.GetUsersNeedingRetention7dNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just PAST the boundary: retention_expires_at - window is slightly BEFORE now, so the
			// window has already opened -> eligible.
			eligibleExpiresAt := time.Now().Add(tt.window - margin).Truncate(time.Microsecond)
			eligibleUser := createRetentionNoticeFixtureUser(t, ctx, pool, repo,
				fmt.Sprintf("boundeligible_%s_%d", tt.name, time.Now().UnixNano()),
				retentionNoticeFixtureOpts{retentionExpiresAt: &eligibleExpiresAt})

			// Just SHORT of the boundary: retention_expires_at - window is slightly AFTER now, so
			// the window has not opened yet -> not eligible.
			notYetExpiresAt := time.Now().Add(tt.window + margin).Truncate(time.Microsecond)
			notYetUser := createRetentionNoticeFixtureUser(t, ctx, pool, repo,
				fmt.Sprintf("boundnotyet_%s_%d", tt.name, time.Now().UnixNano()),
				retentionNoticeFixtureOpts{retentionExpiresAt: &notYetExpiresAt})

			eligible, err := tt.eligibleFn(ctx, eligibleUser.ID, eligibleExpiresAt)
			require.NoError(t, err)
			assert.True(t, eligible, "%s: retention_expires_at - window just PAST NOW() must already be eligible", tt.name)

			notYetEligible, err := tt.eligibleFn(ctx, notYetUser.ID, notYetExpiresAt)
			require.NoError(t, err)
			assert.False(t, notYetEligible, "%s: retention_expires_at - window just SHORT of NOW() must not be eligible yet", tt.name)

			// Same proof through the set-based candidate query retention_cleanup_check.go actually
			// calls — not just the per-occurrence re-check above.
			listed, err := tt.listFn(ctx)
			require.NoError(t, err)
			assert.True(t, containsUserID(listed, eligibleUser.ID),
				"%s: the set-based candidate query must include a user whose window has just opened", tt.name)
			assert.False(t, containsUserID(listed, notYetUser.ID),
				"%s: the set-based candidate query must NOT include a user whose window has not opened yet", tt.name)
		})
	}
}

// containsUserID reports whether users contains a user with the given ID.
func containsUserID(users []*domain.User, id string) bool {
	for _, u := range users {
		if u.ID == id {
			return true
		}
	}
	return false
}
