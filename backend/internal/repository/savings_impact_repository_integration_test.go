//go:build integration

package repository

// Integration tests for savings impact repository.
// These tests execute SQL against a real Postgres planner and catch type/aggregate
// errors that pgxmock cannot detect (e.g., ON CONFLICT expression-index mismatches,
// aggregate JOIN + filter correctness — Issue #972).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestWriteSnapshot -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteSnapshot_OnConflictUpsert_Integration proves the ON CONFLICT
// ((DATE(computed_at AT TIME ZONE 'UTC'))) expression exactly matches the unique
// index created by migration 000092. pgxmock cannot catch expression-index
// target mismatches because it never sends SQL to a real planner.
// This is the regression guard for the Codex HIGH bug (wrong expression target).
func TestWriteSnapshot_OnConflictUpsert_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepository(tx)

	// First write: snapshot at 09:00 UTC on the test day.
	day1 := time.Date(2024, 3, 15, 9, 0, 0, 0, time.UTC)
	snap1 := &domain.SavingsImpactSnapshot{
		ComputedAt:     day1,
		TotalAmount:    1000.00,
		UserCount:      5,
		CompletedCount: 10,
	}
	err = repo.WriteSnapshot(ctx, snap1)
	require.NoError(t, err, "first WriteSnapshot must succeed")

	// Second write: same UTC calendar day (different wall-clock time), different totals.
	// The ON CONFLICT clause must upsert, not insert a duplicate.
	day2 := time.Date(2024, 3, 15, 18, 30, 0, 0, time.UTC)
	snap2 := &domain.SavingsImpactSnapshot{
		ComputedAt:     day2,
		TotalAmount:    1100.00,
		UserCount:      6,
		CompletedCount: 11,
	}
	err = repo.WriteSnapshot(ctx, snap2)
	require.NoError(t, err, "second WriteSnapshot on same UTC date must not error (ON CONFLICT upsert)")

	// Verify exactly one row exists for this UTC date.
	var rowCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM savings_impact_snapshots
		 WHERE DATE(computed_at AT TIME ZONE 'UTC') = DATE($1 AT TIME ZONE 'UTC')`,
		day1,
	).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount,
		"ON CONFLICT upsert must produce exactly one row per UTC date (not two)")

	// Verify the upserted row holds the second write's values.
	// Query by the specific UTC date rather than GetLatestSnapshot, which could return
	// a row from a different date if the test DB already has more-recent committed snapshots.
	var gotTotal float64
	var gotUsers, gotCompleted int
	err = tx.QueryRow(ctx, `
		SELECT total_amount, user_count, completed_count
		FROM savings_impact_snapshots
		WHERE DATE(computed_at AT TIME ZONE 'UTC') = DATE($1 AT TIME ZONE 'UTC')
	`, day1).Scan(&gotTotal, &gotUsers, &gotCompleted)
	require.NoError(t, err)
	assert.InDelta(t, 1100.00, gotTotal, 0.01,
		"upserted snapshot must reflect second write's total_amount")
	assert.Equal(t, 6, gotUsers,
		"upserted snapshot must reflect second write's user_count")
	assert.Equal(t, 11, gotCompleted,
		"upserted snapshot must reflect second write's completed_count")
}

// TestGetAggregateSavingsStats_Integration proves the aggregate query correctly
// applies the include_in_aggregate_stats privacy JOIN filter and counts only
// completed suggestions with non-NULL actual_amount. Uses a delta approach
// relative to the pre-seed baseline so the test is safe against non-empty DBs.
func TestGetAggregateSavingsStats_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewSavingsImpactRepository(tx)

	// Capture baseline before seeding so the test is safe against non-empty DBs.
	baseTotal, baseUsers, baseCompleted, err := repo.GetAggregateSavingsStats(ctx)
	require.NoError(t, err, "baseline aggregate query must not error")

	// Seed two users: one with consent, one without.
	userConsentedID := uuid.New()
	userNoConsentID := uuid.New()
	for _, row := range []struct {
		id      uuid.UUID
		consent bool
	}{
		{userConsentedID, true},
		{userNoConsentID, false},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
		`, row.id, "test|"+row.id.String(), "int-agg-"+row.id.String()+"@example.com")
		require.NoError(t, err, "seed user")

		_, err = tx.Exec(ctx, `
			INSERT INTO privacy_settings (user_id, include_in_aggregate_stats)
			VALUES ($1, $2)
		`, row.id, row.consent)
		require.NoError(t, err, "seed privacy_settings")
	}

	// Seed 2 completed suggestions for the consented user (100 + 200 = 300 total).
	now := time.Now().UTC()
	for _, amount := range []float64{100.00, 200.00} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, completed_at, actual_amount, completion_source, created_at, updated_at)
			VALUES ($1, $2, $3, 'rule', 'completed', $4, $3, 'exact_match', $4, $4)
		`, uuid.New(), userConsentedID, amount, now)
		require.NoError(t, err, "seed completed suggestion for consented user")
	}

	// Seed 1 pending suggestion for the consented user (must NOT count toward the aggregate).
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 50.00, 'rule', 'pending', $3, $3)
	`, uuid.New(), userConsentedID, now)
	require.NoError(t, err, "seed pending suggestion for consented user")

	// Seed 1 completed suggestion for the non-consented user (must NOT count toward the aggregate).
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, completed_at, actual_amount, completion_source, created_at, updated_at)
		VALUES ($1, $2, 50.00, 'rule', 'completed', $3, 50.00, 'exact_match', $3, $3)
	`, uuid.New(), userNoConsentID, now)
	require.NoError(t, err, "seed completed suggestion for non-consented user")

	// Run the aggregate — delta vs. baseline proves the JOIN + filter logic.
	total, users, completed, err := repo.GetAggregateSavingsStats(ctx)
	require.NoError(t, err, "GetAggregateSavingsStats must not error after seeding")

	assert.InDelta(t, 300.00, total-baseTotal, 0.01,
		"aggregate must include only consented, completed suggestions (100+200=300)")
	assert.Equal(t, 1, users-baseUsers,
		"aggregate must count only one consented user")
	assert.Equal(t, 2, completed-baseCompleted,
		"aggregate must count exactly two completed+consented suggestions")
}
