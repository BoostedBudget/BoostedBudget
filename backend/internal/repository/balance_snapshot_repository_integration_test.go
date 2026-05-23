//go:build integration

package repository

// Integration tests for balance snapshot repository.
// These tests execute SQL against a real Postgres planner and catch type/aggregate
// errors that pgxmock cannot detect (e.g., max(uuid) — Issue #1173).
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetAggregatedHistory_Integration -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetAggregatedHistory_Integration proves GetAggregatedHistory executes valid SQL.
// The pgxmock unit tests cannot catch type errors (e.g., max(uuid)) because they never
// send SQL to a real planner — they match query strings and return canned rows.
func TestGetAggregatedHistory_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	// Wrap all seed data in a transaction so it's automatically rolled back.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Unique IDs scoped to this test run to avoid conflicts.
	userID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-test-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	depositoryID := uuid.New()
	creditID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-dep-test', 'Test Checking', 'depository')
	`, depositoryID, linkedItemID, userID)
	require.NoError(t, err, "seed depository account")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-cred-test', 'Test Credit Card', 'credit')
	`, creditID, linkedItemID, userID)
	require.NoError(t, err, "seed credit account")

	// Two UTC days, each with one snapshot per account type.
	day1 := time.Date(2024, 4, 28, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 4, 29, 12, 0, 0, 0, time.UTC)
	dep1, cred1 := 1000.0, -200.0
	dep2, cred2 := 1100.0, -220.0

	type seed struct {
		accountID uuid.UUID
		bal       float64
		at        time.Time
	}
	for _, s := range []seed{
		{depositoryID, dep1, day1},
		{creditID, cred1, day1},
		{depositoryID, dep2, day2},
		{creditID, cred2, day2},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
			VALUES ($1, $2, $3, 'USD', $4, 'manual')
		`, s.accountID, userID, s.bal, s.at)
		require.NoError(t, err, "seed balance_snapshot")
	}

	// Create the repository using the transaction (pgx.Tx satisfies the DB interface).
	repo := NewBalanceSnapshotRepository(tx)

	start := time.Date(2024, 4, 28, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 29, 23, 59, 59, 999, time.UTC)

	history, err := repo.GetAggregatedHistory(ctx, userID.String(), start, end, nil)
	require.NoError(t, err, "GetAggregatedHistory must not return an error")
	require.Len(t, history, 2, "expected one history point per UTC day")

	// Day 1: 1000 (depository) + -200 (credit) = 800 total
	assert.InDelta(t, 800.0, history[0].TotalBalance, 0.01, "day1 total")
	assert.InDelta(t, dep1, history[0].ByType["depository"], 0.01, "day1 depository")
	assert.InDelta(t, cred1, history[0].ByType["credit"], 0.01, "day1 credit")

	// Day 2: 1100 + -220 = 880 total
	assert.InDelta(t, 880.0, history[1].TotalBalance, 0.01, "day2 total")
	assert.InDelta(t, dep2, history[1].ByType["depository"], 0.01, "day2 depository")
	assert.InDelta(t, cred2, history[1].ByType["credit"], 0.01, "day2 credit")
}
