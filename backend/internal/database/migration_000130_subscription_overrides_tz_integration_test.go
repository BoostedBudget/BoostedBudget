//go:build integration

// Regression test for migration 000130 (convert user_subscription_overrides' zone-naked
// TIMESTAMP columns to TIMESTAMPTZ) — Issue #1461/#1464 follow-up.
//
// Unlike the sibling hazards documented in migration_tz_hazard_dst_integration_test.go (000105,
// 000016), which are already-applied and permanently unfixable, THIS defect is fixed by migration
// 000130 in this same batch. So this test is written to run against whichever schema state the
// connected database is currently in, and its assertion is expected to FLIP:
//   - Before 000130 is applied: expires_at is a naked TIMESTAMP. Comparing it against a
//     timestamptz threshold (postgres_subscription_override_repository.go:44's
//     `expires_at > NOW()`) makes Postgres interpret the naked value in the SESSION time zone, so
//     the predicate disagrees between an unpinned America/New_York session and a UTC session. The
//     assertion below FAILS (RED).
//   - After 000130 is applied: expires_at is TIMESTAMPTZ, an absolute instant. Comparisons against
//     it are session-zone-independent, so both sessions agree. The assertion below PASSES (GREEN).
//
// Uses connectRawUnpinned/setSessionTimeZone from migration_tz_hazard_dst_integration_test.go
// (same package, same connection style) — deliberately NOT database.NewPool/NewPoolFromURL, whose
// RuntimeParams["TimeZone"]="UTC" pin is exactly the thing no migration-application path in this
// repo goes through (see that file's header comment for the full inventory of unpinned paths).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigrationTZHazard_UserSubscriptionOverrides -tags integration -v -p 1
package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// seedTZHazardOverrideUser inserts a minimal users row on a bare (unpinned) connection and
// registers cleanup. Mirrors seedMigTestUser (migration_000100_integration_test.go) but adapted to
// a *pgx.Conn instead of a *pgxpool.Pool, since this test's whole point is to use the SAME
// unpinned connection type as connectRawUnpinned rather than a pinned pool.
func seedTZHazardOverrideUser(t *testing.T, conn *pgx.Conn, suffix string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	_, err := conn.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|mig130-"+suffix+"-"+userID, "mig130-"+suffix+"-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() {
		// ON DELETE CASCADE on fk_user_subscription_overrides_user (000004:16) removes the
		// override row seeded below along with the user.
		conn.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID) //nolint:errcheck
	})
	return userID
}

func TestMigrationTZHazard_UserSubscriptionOverrides_ExpiresAtComparison_ConsistentAcrossZones(t *testing.T) {
	conn := connectRawUnpinned(t)
	ctx := context.Background()

	userID := seedTZHazardOverrideUser(t, conn, "expcmp")
	const resourceType = "tz_hazard_regression_test_resource"

	// Anchor the session to UTC for the INSERT itself so the seeded value is deterministic
	// regardless of which schema state (naked TIMESTAMP or TIMESTAMPTZ) is currently live:
	//   - Pre-000130 (naked TIMESTAMP column): a text-to-timestamp cast never consults the
	//     session zone, so this literal is stored as the wall-clock fields verbatim no matter what
	//     the session zone is at INSERT time. Anchoring to UTC here is a no-op for this branch,
	//     but doesn't hurt it.
	//   - Post-000130 (TIMESTAMPTZ column): the ::timestamp value gets implicitly assignment-cast
	//     to timestamptz on INSERT, and THAT cast DOES consult the session zone. Anchoring to UTC
	//     here is what makes the stored absolute instant deterministic (2024-01-15T20:00:00Z)
	//     instead of depending on whatever zone the test happened to connect under.
	setSessionTimeZone(t, conn, "UTC")

	// The boundary value, and why it discriminates:
	//
	// expiresAtLiteral ("2024-01-15 20:00:00") is a plain calendar date in January — outside any
	// DST transition window, so America/New_York is unambiguously EST (UTC-5) here. Interpreted as
	// a wall clock in two different zones, it names two different absolute instants exactly 5 hours
	// apart:
	//   - as UTC:              2024-01-15T20:00:00Z
	//   - as America/New_York: 2024-01-15T20:00:00-05:00 = 2024-01-16T01:00:00Z
	//
	// comparisonInstant (2024-01-15T22:00:00Z) is chosen to fall strictly BETWEEN those two
	// absolute instants (2 hours after the UTC reading, 3 hours before the America/New_York
	// reading) — deliberately close to both (a "few hours" on either side, per the task brief),
	// so the zone offset alone flips which side of the threshold the row lands on:
	//   - Under a UTC session, the naked value reads as 20:00Z, which is BEFORE the threshold ->
	//     `expires_at > comparisonInstant` is FALSE (override already expired).
	//   - Under an America/New_York session, the SAME stored bytes read as 01:00Z the next day,
	//     which is AFTER the threshold -> `expires_at > comparisonInstant` is TRUE (override still
	//     active).
	// This is exactly the ambiguity postgres_subscription_override_repository.go:44's
	// `WHERE ... (expires_at IS NULL OR expires_at > NOW())` predicate has today: whether a given
	// override reads as expired or active depends on the connection's session zone, not just on
	// the stored value.
	const expiresAtLiteral = "2024-01-15 20:00:00"
	const comparisonInstant = "'2024-01-15 22:00:00+00'::timestamptz"

	_, err := conn.Exec(ctx, `
		INSERT INTO user_subscription_overrides (user_id, resource_type, limit_value, reason, expires_at)
		VALUES ($1, $2, $3, $4, $5::timestamp)
	`, userID, resourceType, -1, "tz-hazard-regression-test", expiresAtLiteral)
	require.NoError(t, err)

	checkActive := func(tz string) bool {
		setSessionTimeZone(t, conn, tz)
		var active bool
		err := conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM user_subscription_overrides
				WHERE user_id = $1 AND resource_type = $2 AND expires_at > `+comparisonInstant+`
			)
		`, userID, resourceType).Scan(&active)
		require.NoError(t, err)
		return active
	}

	utcActive := checkActive("UTC")
	nyActive := checkActive("America/New_York")

	t.Logf("expires_at > %s : UTC session => active=%v, America/New_York session => active=%v",
		comparisonInstant, utcActive, nyActive)

	// The actual regression assertion. Before migration 000130, this fails: utcActive=false,
	// nyActive=true — the same override row is simultaneously "expired" and "still active"
	// depending only on which zone the reading connection happens to be in. After 000130 converts
	// expires_at to TIMESTAMPTZ, the stored value becomes the fixed absolute instant 2024-01-15
	// 20:00:00 UTC regardless of session zone at read time, so both sessions agree (both false),
	// and this assertion passes.
	require.Equal(t, utcActive, nyActive,
		"BUG REPRODUCED: user_subscription_overrides.expires_at comparison result depends on the "+
			"reading session's time zone (UTC active=%v vs America/New_York active=%v) — expires_at "+
			"is a zone-naked TIMESTAMP; see migration 000130", utcActive, nyActive)
}
