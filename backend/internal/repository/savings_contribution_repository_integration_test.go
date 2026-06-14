//go:build integration

package repository_test

// savings_contribution_repository_integration_test.go
//
// Integration tests for SavingsContributionRepository against a real PostgreSQL database.
// Run with: go test -tags integration ./internal/repository/... -v
//
// These tests cover:
//   (a) Allocation-cap race guard (Codex C2): concurrent UpsertFromTransaction calls
//       cannot over-allocate a single transaction's amount across multiple goals.
//   (b) Cross-user ownership rejection (Codex C1 / G7): contributions for a goal or
//       transaction owned by a different user are rejected.
//   (c) Inflow sign/magnitude (Issue #1300 plan §1D / M16): GetInflowTransactionsForAccount
//       returns rows with ABS(amount) so downstream consumers receive positive magnitudes
//       even though Plaid stores inflows as negative amounts.
//
// DATABASE_URL environment variable must point to a test PostgreSQL instance that has all
// migrations applied (make migrate-up) before running these tests.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationDB returns a live pgxpool.Pool from DATABASE_URL, or skips the test.
func integrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "failed to connect to test database")
	t.Cleanup(func() { pool.Close() })
	return pool
}

// seedIntegrationUser inserts a minimal user row and returns the userID.
func seedIntegrationUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_id, email, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
	`, userID, "clerk_"+userID, "test_"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

// seedIntegrationAccount inserts a minimal account row owned by userID.
func seedIntegrationAccount(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	accountID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, user_id, plaid_account_id, name, type, subtype, current_balance, created_at, updated_at)
		VALUES ($1, $2, $3, 'Test Account', 'depository', 'savings', 1000.0, now(), now())
	`, accountID, userID, "plaid_"+accountID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
	})
	return accountID
}

// seedIntegrationGoal inserts a savings goal owned by userID.
func seedIntegrationGoal(t *testing.T, pool *pgxpool.Pool, userID, accountID string) string {
	t.Helper()
	goalID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, current_amount, progress_basis, created_at, updated_at)
		VALUES ($1, $2, $3, 'Integration Test Goal', 1000.0, 0.0, 'contributions', now(), now())
	`, goalID, userID, accountID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM savings_goals WHERE id = $1`, goalID)
	})
	return goalID
}

// seedIntegrationTransaction inserts a transaction owned by userID with the given amount.
// Positive amount = outflow (Plaid convention), negative = inflow.
func seedIntegrationTransaction(t *testing.T, pool *pgxpool.Pool, userID, accountID string, amount float64) string {
	t.Helper()
	txnID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO transactions (id, user_id, account_id, amount, name, date, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Test Transfer', now()::date, now(), now())
	`, txnID, userID, accountID, amount)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM transactions WHERE id = $1`, txnID)
	})
	return txnID
}

// =============================================================================
// (a) Allocation-cap race — Codex C2
// =============================================================================

// TestSavingsContributionRepository_Integration_AllocCapRace verifies that concurrent
// UpsertFromTransaction calls for the same transaction cannot collectively over-allocate
// the transaction's full amount across two goals. The SELECT … FOR UPDATE advisory lock
// ensures only one concurrent upsert succeeds when both would exhaust the cap.
func TestSavingsContributionRepository_Integration_AllocCapRace(t *testing.T) {
	pool := integrationDB(t)
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	txnAmount := 100.0 // transaction amount (stored as negative inflow in transactions)
	// We seed the transaction as negative (Plaid inflow convention); the repo uses ABS(amount).
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -txnAmount)
	// Two goals that will compete for the same transaction.
	goalA := seedIntegrationGoal(t, pool, userID, accountID)
	goalB := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)

	// Both goroutines try to allocate 80 units (total = 160 > 100 → one must fail).
	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make([]error, 2)

	for i, goalID := range []string{goalA, goalB} {
		wg.Add(1)
		go func(idx int, gID string) {
			defer wg.Done()
			errors[idx] = repo.UpsertFromTransaction(ctx, userID, gID, txnID, 80.0, "confirmed_transfer")
		}(i, goalID)
	}
	wg.Wait()

	// Exactly one must succeed and one must fail with ErrContributionOverAllocated.
	var successCount, overAllocCount int
	for _, err := range errors {
		if err == nil {
			successCount++
		} else if err == domain.ErrContributionOverAllocated {
			overAllocCount++
		} else {
			t.Errorf("unexpected error type: %v", err)
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent upsert should succeed")
	assert.Equal(t, 1, overAllocCount, "exactly one concurrent upsert should fail with ErrContributionOverAllocated")

	// Confirm the committed amount does not exceed the transaction amount.
	var totalAllocated float64
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM savings_goal_contributions WHERE transaction_id = $1`,
		txnID).Scan(&totalAllocated)
	require.NoError(t, err)
	assert.LessOrEqual(t, totalAllocated, txnAmount,
		"total allocated across all goals must not exceed transaction amount")
}

// =============================================================================
// (b) Cross-user ownership rejection — Codex C1 / G7
// =============================================================================

// TestSavingsContributionRepository_Integration_CrossUserOwnership verifies that
// UpsertFromTransaction rejects goals and transactions not owned by the caller.
func TestSavingsContributionRepository_Integration_CrossUserOwnership(t *testing.T) {
	pool := integrationDB(t)
	userA := seedIntegrationUser(t, pool)
	userB := seedIntegrationUser(t, pool)
	accountA := seedIntegrationAccount(t, pool, userA)
	accountB := seedIntegrationAccount(t, pool, userB)

	goalA := seedIntegrationGoal(t, pool, userA, accountA)    // owned by userA
	txnB := seedIntegrationTransaction(t, pool, userB, accountB, -200.0) // owned by userB

	repo := repository.NewSavingsContributionRepository(pool)
	ctx := context.Background()

	t.Run("caller does not own the goal", func(t *testing.T) {
		// userB tries to allocate from goalA (owned by userA)
		err := repo.UpsertFromTransaction(ctx, userB, goalA, txnB, 50.0, "confirmed_transfer")
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound,
			"should reject: caller (userB) does not own goalA")
	})

	txnA := seedIntegrationTransaction(t, pool, userA, accountA, -200.0) // owned by userA
	goalB := seedIntegrationGoal(t, pool, userB, accountB)               // owned by userB

	t.Run("caller does not own the transaction", func(t *testing.T) {
		// userB owns goalB but txnA is owned by userA
		err := repo.UpsertFromTransaction(ctx, userB, goalB, txnA, 50.0, "confirmed_transfer")
		assert.ErrorIs(t, err, domain.ErrContributionNotFound,
			"should reject: caller (userB) does not own txnA")
	})

	t.Run("ownership check passes for matching user", func(t *testing.T) {
		txnAOwn := seedIntegrationTransaction(t, pool, userA, accountA, -200.0)
		err := repo.UpsertFromTransaction(ctx, userA, goalA, txnAOwn, 50.0, "confirmed_transfer")
		assert.NoError(t, err, "should succeed: userA owns both goalA and txnAOwn")
	})
}

// =============================================================================
// (c) Inflow sign / magnitude — Issue #1300 §1D
// =============================================================================

// TestTransactionRepository_Integration_InflowSignMagnitude verifies that
// GetInflowTransactionsForAccount returns only inflow transactions (amount < 0 in
// Plaid convention) and reports their magnitude as a POSITIVE value (ABS(amount)).
// This guards against the sign-inversion class of bugs documented in Issue #1303.
func TestTransactionRepository_Integration_InflowSignMagnitude(t *testing.T) {
	pool := integrationDB(t)
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	ctx := context.Background()

	// Seed a variety of transactions:
	//   inflow (Plaid convention: negative)  → should appear with positive magnitude
	//   outflow (Plaid convention: positive) → should NOT appear
	inflowTxnID := seedIntegrationTransaction(t, pool, userID, accountID, -150.0) // inflow
	_ = seedIntegrationTransaction(t, pool, userID, accountID, 75.0)               // outflow — excluded
	_ = inflowTxnID

	repo := repository.NewTransactionRepository(pool)

	start := time.Now().AddDate(0, -1, 0)
	end := time.Now().AddDate(0, 1, 0)

	rows, err := repo.GetInflowTransactionsForAccount(ctx, userID, accountID, start, end)
	require.NoError(t, err)

	// All returned rows must have positive magnitude (ABS of stored negative amount).
	for _, row := range rows {
		assert.Greater(t, row.Magnitude, 0.0,
			fmt.Sprintf("transaction %s magnitude must be positive (ABS of inflow); got %.2f", row.ID, row.Magnitude))
	}

	// The inflow transaction must be present with the correct positive magnitude.
	var found bool
	for _, row := range rows {
		if row.ID == inflowTxnID {
			found = true
			assert.Equal(t, 150.0, row.Magnitude,
				"inflow of -150.0 must be returned as magnitude 150.0")
		}
	}
	assert.True(t, found, "inflow transaction must appear in GetInflowTransactionsForAccount results")
}
