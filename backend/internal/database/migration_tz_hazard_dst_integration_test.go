//go:build integration

// Issue #1461/#1464 follow-up (db-migration-guide/SKILL.md, "Migrations do not receive the
// application pool's UTC time zone pin"): proves that gap is a real, exploitable defect for two
// already-applied migrations, not just a documentation claim.
//
// Every real migration-application path in this repo connects WITHOUT pgx.go's
// RuntimeParams["TimeZone"]="UTC" pin: `make migrate-up` pipes files into `psql -f -`, CI's
// backend-integration-test job and docker-entrypoint.sh/scripts/test-migrations.sh all run the
// standalone `migrate` CLI. None of them go through database.NewPool/NewPoolFromURL. This test
// connects the same way — a bare pgx.Connect, no RuntimeParams override — and runs the *exact*
// arithmetic from two migrations under SET TIME ZONE 'America/New_York' vs SET TIME ZONE 'UTC',
// across the 2024-03-10 US DST spring-forward boundary, asserting the two zones disagree.
//
// Why a fixed instant is substituted for a literal NOW()/CURRENT_DATE call: now(),
// CURRENT_TIMESTAMP, and CURRENT_DATE are all frozen once, at transaction start, to the real
// wall-clock instant — plain SQL has no supported way to override that to an arbitrary historical
// instant (no superuser GUC for it). Substituting a controlled timestamptz literal for NOW()
// preserves the exact operator and interval literal from the migration and makes the DST-crossing
// deterministic and reproducible on whatever date this test happens to run, instead of only being
// exercisable on the ~1 day a year the real boundary falls on.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigrationTZHazard -tags integration -v -p 1
package database

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// connectRawUnpinned opens a bare pgx connection with no TimeZone RuntimeParam override —
// deliberately NOT database.NewPoolFromURL/NewPool, whose RuntimeParams["TimeZone"]="UTC" pin
// (Issue #1461) is exactly the thing no migration-application path in this repo goes through.
// Using the app's pinned pool here would test a connection type migrations never actually use.
func connectRawUnpinned(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping migration TZ-hazard integration test")
	}
	conn, err := pgx.Connect(context.Background(), dsn)
	require.NoError(t, err, "failed to open raw (unpinned) connection")
	t.Cleanup(func() { conn.Close(context.Background()) }) //nolint:errcheck
	return conn
}

func setSessionTimeZone(t *testing.T, conn *pgx.Conn, tz string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), "SET TIME ZONE '"+tz+"'")
	require.NoError(t, err, "SET TIME ZONE %q failed", tz)
}

// TestMigrationTZHazard_000105_NowMinusCalendarDays_DiffersAcrossDST reproduces
// backend/migrations/000105_add_past_due_started_at.up.sql:10's exact expression —
// `NOW() - INTERVAL '20 days'`, written into past_due_started_at, the anchor for the 21-day
// auto-cancel grace window — under an unpinned session, across the 2024-03-10
// America/New_York spring-forward transition.
func TestMigrationTZHazard_000105_NowMinusCalendarDays_DiffersAcrossDST(t *testing.T) {
	conn := connectRawUnpinned(t)
	ctx := context.Background()

	// Substituted for a real NOW() call: 2024-03-25T16:00:00Z is a fixed instant chosen so that
	// subtracting 20 CALENDAR days (000105's exact interval) in America/New_York local calendar
	// days crosses the 2024-03-10 02:00 local spring-forward transition (now - 20 local days =
	// local March 5, before the transition; "now" itself, March 25, is after it). In UTC there
	// is no DST, ever, so the same subtraction is always exactly 480 hours regardless of which
	// calendar dates it spans.
	const fixedNow = "'2024-03-25 16:00:00+00'::timestamptz"

	runExpr := func(tz string) string {
		setSessionTimeZone(t, conn, tz)
		var result string
		// This is 000105:10's exact expression, `NOW() - INTERVAL '20 days'`, with fixedNow
		// substituted for NOW() (see file header for why).
		err := conn.QueryRow(ctx, "SELECT ("+fixedNow+" - INTERVAL '20 days')::text").Scan(&result)
		require.NoError(t, err)
		return result
	}

	nyResult := runExpr("America/New_York")
	utcResult := runExpr("UTC")

	// Sanity-pin the exact wall-clock strings so a future change to Postgres's interval
	// arithmetic, or a typo in the fixed instant above, fails loudly here instead of just
	// silently changing the epoch delta assertion below.
	require.Equal(t, "2024-03-05 12:00:00-05", nyResult,
		"America/New_York result changed — re-verify the DST boundary assumption still holds")
	require.Equal(t, "2024-03-05 16:00:00+00", utcResult,
		"UTC result changed — re-verify the fixed instant/interval literals")

	epochOf := func(literal string) float64 {
		var epoch float64
		require.NoError(t, conn.QueryRow(ctx, "SELECT EXTRACT(EPOCH FROM '"+literal+"'::timestamptz)").Scan(&epoch))
		return epoch
	}
	nyEpoch := epochOf(nyResult)
	utcEpoch := epochOf(utcResult)

	// The actual defect assertion: the two session zones must compute DIFFERENT absolute
	// instants for the identical expression, differing by exactly one hour (the DST transition
	// 000105's calendar-day subtraction crossed).
	require.NotEqual(t, nyEpoch, utcEpoch,
		"BUG NOT REPRODUCED: NOW() - INTERVAL '20 days' produced the same absolute instant under "+
			"America/New_York and UTC sessions — the session-timezone hazard did not manifest")
	require.InDelta(t, 3600.0, math.Abs(nyEpoch-utcEpoch), 0.001,
		"expected exactly a 1-hour discrepancy from the single DST transition inside the 20-day window")
}

// TestMigrationTZHazard_000016_CurrentDateMechanism_DiffersAcrossZones reproduces the mechanism
// behind backend/migrations/000016_create_balance_snapshots_table.up.sql:48's
// `CURRENT_DATE AS snapshot_date`, written into a DATE NOT NULL column.
//
// CURRENT_DATE is documented as the date part of the current transaction's start instant,
// converted to the session time zone (Postgres source: GetSQLCurrentDate in
// src/backend/utils/adt/timestamp.c does exactly this: timestamptz2date(transaction start)).
// Plain SQL cannot freeze a transaction's start instant to an arbitrary historical value, so the
// literal CURRENT_DATE token can't be tested deterministically against a chosen instant. This
// test instead verifies the exact conversion CURRENT_DATE relies on, using a fixed instant.
//
// Note this mismatch is NOT a DST-specific phenomenon the way 000105's calendar-interval
// arithmetic is — America/New_York is at least 4 (EDT) or 5 (EST) hours behind UTC year-round,
// so any instant in the early-UTC-morning window shows the same calendar-date disagreement on
// almost any day of the year, DST transition or not. The instant below happens to fall on the
// same 2024-03-10 date as the other test for narrative consistency, but the reason it disagrees
// here is the plain UTC-vs-local offset, not the transition itself.
func TestMigrationTZHazard_000016_CurrentDateMechanism_DiffersAcrossZones(t *testing.T) {
	conn := connectRawUnpinned(t)
	ctx := context.Background()

	const fixedInstant = "'2024-03-10T03:00:00Z'::timestamptz"

	dateAt := func(tz string) string {
		setSessionTimeZone(t, conn, tz)
		var d string
		// (<instant> AT TIME ZONE tz)::date is the documented equivalent of what CURRENT_DATE
		// computes from the (unfreezable) transaction-start instant; see the doc comment above.
		err := conn.QueryRow(ctx, "SELECT ("+fixedInstant+" AT TIME ZONE '"+tz+"')::date::text").Scan(&d)
		require.NoError(t, err)
		return d
	}

	utcDate := dateAt("UTC")
	nyDate := dateAt("America/New_York")

	require.Equal(t, "2024-03-10", utcDate, "UTC calendar date changed — re-verify the fixed instant")
	require.Equal(t, "2024-03-09", nyDate, "America/New_York calendar date changed — re-verify the fixed instant")
	require.NotEqual(t, utcDate, nyDate,
		"BUG NOT REPRODUCED: the CURRENT_DATE-equivalent conversion produced the same calendar "+
			"date under UTC and America/New_York for an instant chosen to straddle midnight UTC "+
			"while still being afternoon/evening local time the day before")

	// Supplementary and deliberately NOT asserted: literal CURRENT_DATE compared under both
	// zones within one transaction (so the frozen "now" is identical for both SELECTs — only
	// TimeZone changes between them). Whether these two differ depends on the real wall-clock
	// moment this test happens to run (only ~4-5 of each UTC day's 24 hours fall in the
	// mismatch window), so asserting on it would make the test flaky. Logged for visibility only.
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, "SET TIME ZONE 'UTC'")
	require.NoError(t, err)
	var liveUTC string
	require.NoError(t, tx.QueryRow(ctx, "SELECT CURRENT_DATE::text").Scan(&liveUTC))
	_, err = tx.Exec(ctx, "SET TIME ZONE 'America/New_York'")
	require.NoError(t, err)
	var liveNY string
	require.NoError(t, tx.QueryRow(ctx, "SELECT CURRENT_DATE::text").Scan(&liveNY))
	t.Logf("live CURRENT_DATE at test run time (informational only, not asserted): UTC=%s America/New_York=%s", liveUTC, liveNY)
}
