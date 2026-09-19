//go:build integration

package repository

// Integration tests for ProfileRepository.SetRetentionDates (Task NA, Issue #1413 follow-up on
// PR #1443).
//
// SetRetentionDates used to accept Go-computed startedAt/expiresAt time.Time values from its
// callers (subscription_webhook.go's Paddle-cancellation path, trial_service.go's trial-expiry
// path), each built via `now.AddDate(0, retentionMonths, 0)`. AdminUserRepository.
// RemoveUserTierOverride's own SQL, meanwhile, has always computed the same column via
// `clock_timestamp() + make_interval(months => $N)`. The two disagree whenever the anchor's
// day-of-month does not exist N months later: Go's time.Time.AddDate normalizes the overflow
// FORWARD (31 August + 6 months -> 3 March), while Postgres's make_interval CLAMPS to the target
// month's last day (-> 28 February) instead. Since retention_expires_at is a GDPR deletion
// deadline, the Go paths silently OVER-RETAINED personal data relative to the admin-override path
// — the wrong direction for a deletion clock. Task NA ratified aligning every writer on Postgres's
// clamping semantics: SetRetentionDates now takes retentionMonths int and computes both
// retention_started_at and retention_expires_at entirely in Postgres via clock_timestamp() +
// make_interval, matching RemoveUserTierOverride's SQL exactly.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestSetRetentionDates_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestSetRetentionDates_Integration_ArmsClockWithPostgresArithmetic is the end-to-end proof that
// the real SetRetentionDates call writes a non-NULL retention_started_at ~now and a non-NULL
// retention_expires_at ~now+retentionMonths — fenced with dbNow/dbAddMonths (not time.Now()/AddDate)
// so the assertion is immune to both Go-vs-Postgres clock drift and Go-vs-Postgres month-arithmetic
// divergence, exactly like the Task NA Part 1 rewrites in
// admin_user_repository_remove_tier_override_integration_test.go.
func TestSetRetentionDates_Integration_ArmsClockWithPostgresArithmetic(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_setretdates_" + suffix,
		Email:              "setretdates-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	const retentionMonths = testRetentionMonths // 9 — deliberately not the config default of 6

	before := dbNow(t, pool)
	// The fixture user above is 'cancelled', so this drives the guarded production shape (Issue
	// #1500 residual): the write's own authorising condition, satisfied.
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), retentionMonths))
	after := dbNow(t, pool)

	var retStarted, retExpires *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retention_started_at, retention_expires_at FROM users WHERE id = $1`, user.ID,
	).Scan(&retStarted, &retExpires))
	require.NotNil(t, retStarted, "retention_started_at must be set")
	require.NotNil(t, retExpires, "retention_expires_at must be set")

	assertBetweenFence(t, before, after, *retStarted, "retention_started_at must be ~now")
	assertBetweenFence(t, dbAddMonths(t, pool, before, retentionMonths), dbAddMonths(t, pool, after, retentionMonths), *retExpires,
		"retention_expires_at must be ~now+retentionMonths, computed via Postgres's make_interval")
}

// TestSetRetentionDates_Integration_MakeIntervalClampsAtMonthEnd pins Task NA's ratified fix
// explicitly, at a FIXED calendar boundary rather than at whatever day the test suite happens to
// run on: "six months after 31 August" must land on 28 February (2027 is not a leap year), never 3
// March.
//
// Why this is anchored to a literal timestamp instead of driving it through SetRetentionDates end
// to end: SetRetentionDates deliberately no longer accepts a caller-supplied anchor — both
// retention_started_at and retention_expires_at are computed from Postgres's OWN clock_timestamp()
// (see SetRetentionDates's doc comment). That is the whole point of Task NA's fix, but it also
// means nothing in this codebase can force "now" to fall on a specific day of the month inside a
// live integration test — Postgres's wall clock cannot be faked without a superuser session
// extension. The expression exercised below, `<anchor> + make_interval(months => $N)`, is
// textually the SAME arithmetic SetRetentionDates's SQL and
// AdminUserRepository.RemoveUserTierOverride's removeUserTierOverrideUpdate both use for
// retention_expires_at — it just substitutes a literal anchor for clock_timestamp() so the
// month-end scenario is reproducible on every run, not just on the 31st.
//
// This is the test that stops someone reintroducing Go-side AddDate arithmetic into that
// expression — falsified by temporarily pointing the assertion below at Go's overflowed answer
// (2027-03-03) instead of Postgres's clamped one (2027-02-28) and confirming the test goes RED;
// see Task NA's report for the verbatim failure output.
func TestSetRetentionDates_Integration_MakeIntervalClampsAtMonthEnd(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	anchor := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	// AT TIME ZONE 'UTC' converts the timestamptz into a naive UTC wall-clock timestamp BEFORE the
	// make_interval addition, so the calendar arithmetic happens without any DST reinterpretation
	// (August 31 sits in EDT, February 28 in EST on a server running America/New_York — doing the
	// addition directly on a timestamptz would let that DST offset change shift the result by an
	// hour, a confound unrelated to the month-clamping behavior this test exists to isolate).
	var expires time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT ($1::timestamptz AT TIME ZONE 'UTC') + make_interval(months => $2::int)`, anchor, 6,
	).Scan(&expires))
	expires = expires.UTC()

	wantClamped := time.Date(2027, 2, 28, 12, 0, 0, 0, time.UTC)
	assert.True(t, wantClamped.Equal(expires),
		"six months after August 31 must clamp to February 28 (Postgres make_interval semantics — the semantics SetRetentionDates and RemoveUserTierOverride both use) — got %v, want %v", expires, wantClamped)

	// Document, not just assert, what the WRONG answer would have been if this expression were ever
	// replaced by Go's time.Time.AddDate: Go normalizes the overflow FORWARD into March instead of
	// clamping to February's last day. Explicitly proving the two disagree here is what makes this
	// test meaningful — if Postgres's own arithmetic ever behaved like Go's (or someone piped a
	// Go-computed value back into this column), this fixture's expected/actual values would
	// coincide and the assertion above would go undetected-wrong instead of catching the
	// regression.
	goOverflowed := anchor.AddDate(0, 6, 0)
	require.True(t, time.Date(2027, 3, 3, 12, 0, 0, 0, time.UTC).Equal(goOverflowed),
		"sanity check on Go's own semantics: AddDate(0,6,0) from Aug 31 must normalize forward to March 3")
	assert.False(t, expires.Equal(goOverflowed),
		"the Postgres-clamped result must NOT match Go's AddDate overflow result — got the same value %v for both, which would mean make_interval stopped clamping", expires)
}

// TestSetRetentionDates_Integration_PreservesAbsoluteDeadlineAcrossDSTBoundary is Issue #1461's
// headline test: it proves retention_expires_at's absolute GDPR deletion deadline survives a real
// spring-forward DST boundary, on the exact pool (connectTestDB) production code runs on.
//
// retention_expires_at is written as `clock_timestamp() + make_interval(months => N)` — a
// timestamptz plus a calendar interval, with NO inline AT TIME ZONE of its own (see
// SetRetentionDates/RemoveUserTierOverride's own SQL). Postgres performs that addition in the
// SESSION time zone: it converts the timestamptz to session-local wall-clock time, adds N months
// to the wall clock, then reinterprets the result in whatever UTC offset applies THEN — which can
// differ from the offset that applied at the anchor if a DST transition falls in between. Pinning
// every pooled connection's session TimeZone to UTC (NewPool, Issue #1461) removes that
// session-dependence: this test exercises the SAME unqualified expression shape production uses,
// through connectTestDB's pool (which now routes through database.NewPoolFromURL — see that
// helper's own comment), and asserts it lands exactly on the UTC-computed reference regardless.
//
// Falsified by removing RuntimeParams["TimeZone"] = "UTC" from database.buildPoolConfig
// (internal/database/pgx.go) and confirming this test goes RED (on this host, whose own Postgres
// TimeZone defaults to America/New_York) — see Issue #1461's report for the verbatim failure
// output.
func TestSetRetentionDates_Integration_PreservesAbsoluteDeadlineAcrossDSTBoundary(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// 2026-01-15 12:00:00-05 (EST) — expressed as its UTC instant so the literal is unambiguous.
	anchor := time.Date(2026, 1, 15, 17, 0, 0, 0, time.UTC)
	const retentionMonths = 6

	// The EXACT expression shape SetRetentionDates/RemoveUserTierOverride use in production:
	// <timestamptz> + make_interval(months => N), relying entirely on whatever session TimeZone
	// this pool's connections carry — no inline AT TIME ZONE here. If NewPool's UTC pin is in
	// place, this is computed as if in UTC; if the pin is ever removed, this instead reflects the
	// host's own ambient Postgres TimeZone default.
	var actual time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT $1::timestamptz + make_interval(months => $2::int)`, anchor, retentionMonths,
	).Scan(&actual))
	actual = actual.UTC()

	// The intended absolute deadline: 6 calendar months added in UTC, with no DST reinterpretation
	// at all — January (EST, -05) and July (EDT, -04) must not matter.
	want := time.Date(2026, 7, 15, 17, 0, 0, 0, time.UTC)
	assert.True(t, want.Equal(actual),
		"retention_expires_at-shaped arithmetic (clock_timestamp() + make_interval(months => N)) "+
			"on connectTestDB's pool must preserve the absolute deadline across the January(EST)->"+
			"July(EDT) DST boundary — got %v, want %v; a divergence here means NewPool's session "+
			"TimeZone pin is missing or not reaching this pool", actual, want)
}

// TestSetRetentionDates_Integration_PreservesAbsoluteDeadlineAcrossFallBackDSTBoundary is the
// fall-back (EDT->EST) counterpart to
// TestSetRetentionDates_Integration_PreservesAbsoluteDeadlineAcrossDSTBoundary above. Issue
// #1461 suggestion 3 asked for DST coverage "across a spring-forward/fall-back boundary", but
// both tests that existed before this one — the spring-forward test above (January EST -> July
// EDT) and TestRetentionNoticeBoundary_Integration_CrossesDSTBoundary
// (profile_repository_retention_notice_boundary_integration_test.go, anchored on the 2026-03-08
// spring-forward transition) — only exercise the EST->EDT direction; a search of this codebase
// for any EDT->EST anchor found none.
//
// America/New_York falls back from EDT to EST at 2026-11-01 02:00:00 EDT (clocks set back to
// 01:00:00 EST), i.e. 2026-11-01 06:00:00 UTC — confirmed against a live Postgres 17 instance
// before writing this test (with the session TimeZone set to America/New_York,
// `SELECT '2026-11-01 05:59:00+00'::timestamptz` renders as 2026-11-01 01:59:00-04 (EDT), and
// `SELECT '2026-11-01 06:00:00+00'::timestamptz` renders as 2026-11-01 01:00:00-05 (EST)), not
// assumed from documentation alone. The anchor below (May,
// EDT) plus 6 calendar months lands in November (EST), crossing that boundary in the OPPOSITE
// direction from the existing spring-forward test — and, unlike that test, this direction is one
// where an unpinned America/New_York session's wall-clock-preserving arithmetic lands an hour
// LATER than the UTC-computed instant (verified directly against this host's Postgres: with the
// session's own ambient TimeZone, `'2026-05-15 16:00:00+00'::timestamptz + make_interval(months
// => 6)` returns 2026-11-15 17:00:00 UTC, not the 16:00:00 UTC this test asserts below).
//
// Falsified the same way as the spring-forward test above: removing
// RuntimeParams["TimeZone"] = "UTC" from database.buildPoolConfig (internal/database/pgx.go) and
// confirming this test goes RED.
func TestSetRetentionDates_Integration_PreservesAbsoluteDeadlineAcrossFallBackDSTBoundary(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// 2026-05-15 12:00:00-04 (EDT) — expressed as its UTC instant so the literal is unambiguous.
	anchor := time.Date(2026, 5, 15, 16, 0, 0, 0, time.UTC)
	const retentionMonths = 6

	// The EXACT expression shape SetRetentionDates/RemoveUserTierOverride use in production:
	// <timestamptz> + make_interval(months => N), relying entirely on whatever session TimeZone
	// this pool's connections carry — no inline AT TIME ZONE here.
	var actual time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT $1::timestamptz + make_interval(months => $2::int)`, anchor, retentionMonths,
	).Scan(&actual))
	actual = actual.UTC()

	// The intended absolute deadline: 6 calendar months added in UTC, with no DST reinterpretation
	// at all — May (EDT, -04) and November (EST, -05) must not matter.
	want := time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)
	assert.True(t, want.Equal(actual),
		"retention_expires_at-shaped arithmetic (clock_timestamp() + make_interval(months => N)) "+
			"on connectTestDB's pool must preserve the absolute deadline across the May(EDT)->"+
			"November(EST) fall-back DST boundary — got %v, want %v; a divergence here means "+
			"NewPool's session TimeZone pin is missing or not reaching this pool", actual, want)
}
