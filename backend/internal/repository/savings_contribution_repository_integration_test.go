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
	"github.com/maxnussbaum/BoostedBudget/internal/database"
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
	// Issue #1461: route through database.NewPoolFromURL (not a bare pgxpool.New) so this pool
	// pins session TimeZone=UTC exactly like the production pool does.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "failed to connect to test database")
	t.Cleanup(func() { pool.Close() })
	return pool
}

// seedIntegrationUser inserts a minimal user row and returns the userID.
func seedIntegrationUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "clerk_"+userID, "test_"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

// seedIntegrationAccount inserts a minimal account row owned by userID.
func seedIntegrationAccount(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	linkedItemID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item_"+linkedItemID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM linked_items WHERE id = $1`, linkedItemID) //nolint:errcheck
	})

	accountID := uuid.New().String()
	_, err = pool.Exec(context.Background(), `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, current_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Test Account', 'depository', 'savings', 1000.0, now(), now())
	`, accountID, linkedItemID, userID, "plaid_"+accountID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID) //nolint:errcheck
	})
	return accountID
}

// seedIntegrationGoal inserts a savings goal owned by userID.
func seedIntegrationGoal(t *testing.T, pool *pgxpool.Pool, userID, accountID string) string {
	t.Helper()
	goalID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, current_amount, progress_basis, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1000.0, 0.0, 'contributions', now(), now())
	`, goalID, userID, accountID, "Goal "+goalID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM savings_goals WHERE id = $1`, goalID)
	})
	return goalID
}

// seedIntegrationTransaction inserts a transaction owned by userID with the given amount.
// Positive amount = outflow (Plaid convention), negative = inflow.
func seedIntegrationTransaction(t *testing.T, pool *pgxpool.Pool, userID, accountID string, amount float64) string {
	t.Helper()
	var linkedItemID string
	err := pool.QueryRow(context.Background(),
		`SELECT linked_item_id FROM accounts WHERE id = $1`, accountID,
	).Scan(&linkedItemID)
	require.NoError(t, err, "look up linked_item_id for account")

	txnID := uuid.New().String()
	_, err = pool.Exec(context.Background(), `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, name, date, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'plaid', $5, 'Test Transfer', now()::date, now(), now())
	`, txnID, userID, accountID, linkedItemID, amount)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM transactions WHERE id = $1`, txnID) //nolint:errcheck
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
		switch err {
		case nil:
			successCount++
		case domain.ErrContributionOverAllocated:
			overAllocCount++
		default:
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

	goalA := seedIntegrationGoal(t, pool, userA, accountA)               // owned by userA
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

// =============================================================================
// (d) userID-scoped sum reads — RG2 / M17
// =============================================================================

// TestSavingsContributionRepository_Integration_SumConfirmedByGoal_UserScoped verifies
// that SumConfirmedByGoal only sums contributions for a goal owned by the requesting user.
// A cross-user query must return 0 (not another user's contributions).
func TestSavingsContributionRepository_Integration_SumConfirmedByGoal_UserScoped(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userA := seedIntegrationUser(t, pool)
	userB := seedIntegrationUser(t, pool)
	accountA := seedIntegrationAccount(t, pool, userA)
	accountB := seedIntegrationAccount(t, pool, userB)
	goalA := seedIntegrationGoal(t, pool, userA, accountA)
	_ = seedIntegrationGoal(t, pool, userB, accountB)

	// Seed a confirmed contribution for goalA.
	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 500.0, 'manual', 'confirmed', false, true, CURRENT_DATE, now(), now())
	`, contribID, goalA)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE id = $1`, contribID)
	})

	repo := repository.NewSavingsContributionRepository(pool)

	t.Run("correct owner sees sum", func(t *testing.T) {
		total, err := repo.SumConfirmedByGoal(ctx, userA, goalA)
		require.NoError(t, err)
		assert.Equal(t, 500.0, total, "userA should see their own 500.0 contribution")
	})

	t.Run("wrong owner gets zero", func(t *testing.T) {
		// userB is asking for goalA which it does not own → should return 0, not 500.
		total, err := repo.SumConfirmedByGoal(ctx, userB, goalA)
		require.NoError(t, err)
		assert.Equal(t, 0.0, total, "userB must not see userA's contributions")
	})
}

// TestSavingsContributionRepository_Integration_SumConfirmedByGoalInPeriod_UserScoped verifies
// that SumConfirmedByGoalInPeriod only sums contributions for the requesting user.
func TestSavingsContributionRepository_Integration_SumConfirmedByGoalInPeriod_UserScoped(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userA := seedIntegrationUser(t, pool)
	userB := seedIntegrationUser(t, pool)
	accountA := seedIntegrationAccount(t, pool, userA)
	goalA := seedIntegrationGoal(t, pool, userA, accountA)

	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 300.0, 'manual', 'confirmed', false, true, CURRENT_DATE, now(), now())
	`, contribID, goalA)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE id = $1`, contribID)
	})

	repo := repository.NewSavingsContributionRepository(pool)
	start := time.Now().AddDate(0, -1, 0)
	end := time.Now().AddDate(0, 1, 0)

	t.Run("correct owner sees period sum", func(t *testing.T) {
		total, err := repo.SumConfirmedByGoalInPeriod(ctx, userA, goalA, start, end)
		require.NoError(t, err)
		assert.Equal(t, 300.0, total)
	})

	t.Run("wrong owner gets zero", func(t *testing.T) {
		total, err := repo.SumConfirmedByGoalInPeriod(ctx, userB, goalA, start, end)
		require.NoError(t, err)
		assert.Equal(t, 0.0, total, "userB must not see userA's period contributions")
	})
}

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
	_ = seedIntegrationTransaction(t, pool, userID, accountID, 75.0)              // outflow — excluded
	_ = inflowTxnID

	repo := repository.NewTransactionRepository(pool, repository.NewTestFieldEncryptor(t))

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

// =============================================================================
// (d) Migration 000099 — rejected status (R3-0 / R3-7)
// =============================================================================

// TestSavingsContributionRepository_Integration_RejectedStatus verifies that migration 000099
// extended the valid_contribution_status CHECK constraint to include 'rejected'.
//
// RED: fails before 000099 runs (constraint only allows 'proposed','confirmed').
// GREEN: passes after 000099 is applied.
func TestSavingsContributionRepository_Integration_RejectedStatus(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	// Insert a contribution with status='rejected' directly — this hits the CHECK constraint.
	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, amount, source, status, managed_by_reconciliation,
			 confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, 50.0, 'manual', 'rejected', false, true, CURRENT_DATE, now(), now())
	`, contribID, goalID)
	require.NoError(t, err, "status='rejected' must be allowed by the CHECK constraint after migration 000099")

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE id = $1`, contribID)
	})

	// Verify the row is readable and has the correct status.
	var status string
	err = pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, contribID,
	).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "rejected", status)
}

// TestSavingsContributionRepository_Integration_RejectedStatusConstraint verifies that
// the CHECK constraint still rejects invalid status values after migration 000099.
func TestSavingsContributionRepository_Integration_RejectedStatusConstraint(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	// 'bogus' should still be rejected.
	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, amount, source, status, managed_by_reconciliation,
			 confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, 50.0, 'manual', 'bogus', false, true, CURRENT_DATE, now(), now())
	`, contribID, goalID)
	require.Error(t, err, "status='bogus' must be rejected by the CHECK constraint")
	assert.Contains(t, err.Error(), "valid_contribution_status",
		"error should mention the constraint name")
}

// =============================================================================
// (e) Sticky reject tombstone — R3-7 / R4-T1
// =============================================================================

// TestSavingsContributionRepository_Integration_RejectTombstone_SurvivesResync is the
// canonical proof of the R3-7 sticky-tombstone guarantee.
//
// It verifies that after a user rejects a proposed contribution:
//   - Exactly one row exists with status='rejected' (the tombstone).
//   - A subsequent call to UpsertProposedFromTransaction for the same
//     (transaction_id, savings_goal_id) pair is a no-op — the tombstone is NOT
//     overwritten to status='proposed', and no duplicate row is created.
//
// This behavior is enforced by the partial unique index on (transaction_id, savings_goal_id)
// filtered WHERE transaction_id IS NOT NULL, combined with the ON CONFLICT … WHERE
// status='proposed' predicate in UpsertProposedFromTransaction: if the conflict row has
// status='rejected', the predicate is false and the DO UPDATE is skipped entirely.
//
// RED: fails before R3-7 (reject was a hard DELETE → re-upsert re-creates proposed row).
// GREEN: passes after R3-7 (reject sets status='rejected'; re-upsert is a no-op).
func TestSavingsContributionRepository_Integration_RejectTombstone_SurvivesResync(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	// Plaid inflow convention: negative amount; ABS is used in capacity checks.
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)

	repo := repository.NewSavingsContributionRepository(pool)

	// Step 1: Simulate sync detecting a proposed contribution.
	err := repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 100.0)
	require.NoError(t, err, "initial UpsertProposedFromTransaction must succeed")

	// Step 2: Fetch the contribution ID so we can call RejectContribution.
	var contribID string
	err = pool.QueryRow(ctx,
		`SELECT id FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID,
	).Scan(&contribID)
	require.NoError(t, err, "proposed contribution must exist after upsert")

	// Step 3: User rejects the proposed row.
	_, err = repo.RejectContribution(ctx, userID, goalID, contribID)
	require.NoError(t, err, "RejectContribution must succeed")

	// Verify tombstone is in place.
	var statusAfterReject string
	err = pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, contribID,
	).Scan(&statusAfterReject)
	require.NoError(t, err)
	assert.Equal(t, "rejected", statusAfterReject, "row must have status='rejected' after RejectContribution")

	// Step 4: Sync re-detects the same transfer-in — call UpsertProposedFromTransaction again.
	// The tombstone's ON CONFLICT predicate (WHERE status='proposed') makes this a no-op.
	err = repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 100.0)
	require.NoError(t, err, "re-upsert after reject must not error")

	// Step 5: Assert exactly one row remains and it is still 'rejected'.
	var rowCount int
	var statusAfterResync string
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(status), '')
		   FROM savings_goal_contributions
		  WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID,
	).Scan(&rowCount, &statusAfterResync)
	require.NoError(t, err)

	assert.Equal(t, 1, rowCount,
		"re-sync must not create a duplicate row; tombstone remains the only record")
	assert.Equal(t, "rejected", statusAfterResync,
		"tombstone status must remain 'rejected' after re-sync (R3-7 sticky guarantee)")
}

// TestSavingsContributionRepository_Integration_RejectTombstone_SurvivesArchiveThenResync closes
// the gap Task 8's brief identified: the canonical sticky test above (reject → immediate resync)
// never exercised ArchiveByGoal, and migration 000191's own test used an archived CONFIRMED row,
// not a rejected one.
//
// Before this fix, ArchiveByGoal archived every live row regardless of status. Once the rejected
// row got archived_at stamped, it left unique_contribution_per_goal_per_transaction (scoped to
// `archived_at IS NULL` by 000191) — so a later sync's INSERT no longer collided with the
// tombstone at all and inserted a brand-new status='proposed' row for the same transaction,
// resurrecting a transfer the user explicitly turned down.
//
// RED (pre-fix): rowCount == 2, one 'rejected' (archived) and one freshly re-proposed.
// GREEN (post-fix): the rejected row stays live (archived_at IS NULL) through the archive, so it
// keeps the uniqueness slot and the resync is still a no-op.
func TestSavingsContributionRepository_Integration_RejectTombstone_SurvivesArchiveThenResync(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)

	repo := repository.NewSavingsContributionRepository(pool)

	// Step 1: Simulate sync detecting a proposed contribution, then the user rejects it.
	err := repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 100.0)
	require.NoError(t, err, "initial UpsertProposedFromTransaction must succeed")

	var contribID string
	err = pool.QueryRow(ctx,
		`SELECT id FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID,
	).Scan(&contribID)
	require.NoError(t, err, "proposed contribution must exist after upsert")

	_, err = repo.RejectContribution(ctx, userID, goalID, contribID)
	require.NoError(t, err, "RejectContribution must succeed")

	// Step 2: The goal's basis leaves and re-enters "contributions", closing the period.
	require.NoError(t, repo.ArchiveByGoal(ctx, goalID))

	var archivedAt *time.Time
	var statusAfterArchive string
	err = pool.QueryRow(ctx,
		`SELECT status, archived_at FROM savings_goal_contributions WHERE id = $1`, contribID,
	).Scan(&statusAfterArchive, &archivedAt)
	require.NoError(t, err)
	assert.Equal(t, "rejected", statusAfterArchive, "archiving must not change the tombstone's status")
	assert.Nil(t, archivedAt,
		"a rejected row must NOT be archived — archiving it would release its uniqueness slot "+
			"and let a later sync re-propose the rejected transfer")

	// Step 3: A later Plaid sync re-detects the same transfer-in.
	err = repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 100.0)
	require.NoError(t, err, "re-upsert after archive must not error")

	// Step 4: Assert exactly one row remains and it is still 'rejected' — the transfer must NOT
	// be re-proposed.
	var rowCount int
	var statusAfterResync string
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(status), '')
		   FROM savings_goal_contributions
		  WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID,
	).Scan(&rowCount, &statusAfterResync)
	require.NoError(t, err)

	assert.Equal(t, 1, rowCount,
		"archiving the period and re-syncing must not create a duplicate row; the tombstone "+
			"remains the only record")
	assert.Equal(t, "rejected", statusAfterResync,
		"tombstone status must remain 'rejected' after archive-then-resync (R3-7 sticky guarantee "+
			"must survive a basis period boundary)")
}

// TestSessionDateCast_Integration_UsesUTCCalendarDay proves the Issue #1461 decision documented on
// UpsertFromTransaction's own doc comment: CURRENT_DATE (used there and by
// UpsertProposedFromTransaction for occurred_on) now resolves to the UTC calendar day, because
// NewPool pins every pooled connection's session TimeZone to UTC.
//
// ::date truncates a timestamptz to a calendar day in the SESSION time zone — the exact same
// session-dependence CURRENT_DATE has (Postgres defines CURRENT_DATE as the date part of the
// current transaction's start time, evaluated in the session TimeZone). This test picks a literal
// instant that falls on DIFFERENT calendar days in UTC vs. America/New_York (10:30pm EST on Jan 15
// is already Jan 16 in UTC) and asserts integrationDB's pool casts it to the UTC day, not the
// Eastern day.
//
// Falsified by removing RuntimeParams["TimeZone"] = "UTC" from database.buildPoolConfig
// (internal/database/pgx.go) and confirming this test goes RED on this host (whose own Postgres
// TimeZone defaults to America/New_York) — see Issue #1461's report for the verbatim failure
// output.
func TestSessionDateCast_Integration_UsesUTCCalendarDay(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	// 2026-01-15 22:30:00-05 (10:30pm EST) = 2026-01-16 03:30:00 UTC — already tomorrow in UTC.
	instant := time.Date(2026, 1, 16, 3, 30, 0, 0, time.UTC)

	var gotDate time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT $1::timestamptz::date`, instant).Scan(&gotDate))

	wantUTCDay := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	assert.True(t, wantUTCDay.Equal(gotDate.UTC()),
		"::date (the same session-timezone-dependent cast CURRENT_DATE uses) must resolve to the "+
			"UTC calendar day on integrationDB's pool — got %v, want %v", gotDate.UTC(), wantUTCDay)
}

// =============================================================================
// (d) Tombstone-aware ownership probes — Issue #1569 round 1, finding M-1
// =============================================================================

// Insert, InsertManualFromTransaction and ConfirmContribution each carry a
// `SELECT ... FROM transactions WHERE id = $1 AND user_id = $2 [AND deleted_at IS NULL] [FOR UPDATE]`
// ownership/capacity probe that #1569 edited. All three were executed by NO integration test — the
// round-1 report claimed otherwise and was wrong. The #1569 edits to this file were made by a
// scripted pass that produced three syntactically invalid queries elsewhere in the package, so
// "it compiles" was never evidence these parse.
//
// These tests do two things: execute the real SQL, and pin the tombstone behaviour that edit
// introduced — a transaction the provider removed can no longer back a savings contribution.
// ErrContributionNotFound is the right answer there: the money the contribution was derived from
// is no longer part of the ledger, and silently keeping the link would let a reversed transfer go
// on counting toward a goal.

func TestSavingsContributionRepository_Integration_Insert_RejectsTombstonedTransaction(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -50.0)

	repo := repository.NewSavingsContributionRepository(pool)

	live := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        10.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, live),
		"baseline: the probe must parse and accept a live transaction")

	tombstoneTransaction(t, pool, txnID)

	tombstoned := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        5.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	err := repo.Insert(ctx, userID, tombstoned)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"a provider-removed transaction must not be linkable as a contribution")
}

func TestSavingsContributionRepository_Integration_InsertManualFromTransaction_RejectsTombstoned(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -80.0)

	repo := repository.NewSavingsContributionRepository(pool)
	occurredOn := time.Now().Format("2006-01-02")

	created, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 20.0, occurredOn)
	require.NoError(t, err, "baseline: the FOR UPDATE capacity probe must parse against a live row")
	require.NotNil(t, created)

	tombstoneTransaction(t, pool, txnID)

	_, err = repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 20.0, occurredOn)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"remaining capacity on a removed transaction is not capacity")
}

func TestSavingsContributionRepository_Integration_ConfirmContribution_RejectsTombstoned(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -120.0)

	repo := repository.NewSavingsContributionRepository(pool)

	proposed := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        30.0,
		Source:        "confirmed_transfer",
		Status:        "proposed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, proposed))

	confirmed, err := repo.ConfirmContribution(ctx, userID, goalID, proposed.ID)
	require.NoError(t, err, "baseline: ConfirmContribution's FOR UPDATE probe must parse")
	require.NotNil(t, confirmed)
	assert.Equal(t, "confirmed", confirmed.Status)

	// A second proposed row on the same transaction — necessarily on a DIFFERENT goal, because
	// unique_contribution_per_goal_per_transaction allows only one per (goal, transaction) — then
	// tombstone the transaction under it.
	secondGoalID := seedIntegrationGoal(t, pool, userID, accountID)
	second := &domain.SavingsGoalContribution{
		SavingsGoalID: secondGoalID,
		TransactionID: &txnID,
		Amount:        10.0,
		Source:        "confirmed_transfer",
		Status:        "proposed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, second))

	tombstoneTransaction(t, pool, txnID)

	_, err = repo.ConfirmContribution(ctx, userID, goalID, second.ID)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"confirming a contribution whose transaction the provider removed must fail rather than "+
			"silently counting reversed money toward a goal")
}

// TestSavingsContributionRepository_Integration_ConfirmContribution_RefusesRejectedRow closes
// #1625 Task 9: RejectContribution sets a sticky tombstone (R3-7) so a later Plaid sync cannot
// re-propose a transfer the user already turned down. ConfirmContribution rejected only
// status=='confirmed' and then unconditionally flipped the row to 'confirmed' — so a direct
// confirm call on a rejected row silently undid the user's decision. This is a DIFFERENT scenario
// from TestSavingsContributionRepository_Integration_RejectTombstone_SurvivesResync above (which
// pins that a sync cannot resurrect the row): this one pins that the confirm API itself cannot.
func TestSavingsContributionRepository_Integration_ConfirmContribution_RefusesRejectedRow(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()
	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -50.0)

	repo := repository.NewSavingsContributionRepository(pool)

	proposed := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        20.0,
		Source:        "confirmed_transfer",
		Status:        "proposed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, proposed))

	rejected, err := repo.RejectContribution(ctx, userID, goalID, proposed.ID)
	require.NoError(t, err, "RejectContribution must succeed on a proposed row")
	require.Equal(t, "rejected", rejected.Status)

	_, err = repo.ConfirmContribution(ctx, userID, goalID, proposed.ID)
	require.Error(t, err, "confirming a rejected sticky tombstone must be refused, not silently succeed")
	assert.ErrorIs(t, err, domain.ErrContributionAlreadyRejected)

	var statusAfter string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, proposed.ID,
	).Scan(&statusAfter))
	assert.Equal(t, "rejected", statusAfter,
		"the row must remain 'rejected' — the refused confirm must not have mutated it")
}

// tombstoneTransaction applies the #1569 tombstone directly, so these tests do not depend on the
// Plaid sync path to reach the state they are about.
func tombstoneTransaction(t *testing.T, pool *pgxpool.Pool, txnID string) {
	t.Helper()
	ct, err := pool.Exec(context.Background(), `
		UPDATE transactions
		SET deleted_at = NOW(), deleted_reason = 'provider_removed_posted', deleted_by = 'plaid_sync'
		WHERE id = $1
	`, txnID)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "fixture must actually be tombstoned")
}

// =============================================================================
// Retention: ArchiveByGoal and the reconciler-ownership conflict guard (#1569)
// =============================================================================

// TestArchiveByGoal_Integration_LeavesUserEnteredRowsAlone pins the EFFECT of ArchiveByGoal on
// real rows, which the two pgxmock tests of it cannot: both match only the table-name prefix
// `UPDATE savings_goal_contributions`, never the SET or the WHERE, so they stay green under any
// mutation of either.
//
// Why the effect and not the SQL text: dropping the `AND managed_by_reconciliation = true` leg is
// nearly harmless (rows already false stay false; only updated_at is bumped). The mutation that
// matters is inverting the SET to `= true` — that stamps USER-ENTERED contributions as
// reconciler-managed, and DeleteManagedForTransaction TOMBSTONES rows carrying that flag. So this
// test seeds one user-entered row alongside one reconciler row and asserts where each lands.
//
// ⚠ CORRECTED (#1569 round 14). These three sentences said "HARD-DELETES" / "the hard delete",
// which stopped being true in round 12: migration 000180 turned that method into an
// `UPDATE … SET deleted_at, deleted_reason = 'provider_removed_txn'`. The stakes are LOWER than
// the old wording implied but not zero — a tombstoned contribution is retained and permanently
// invisible EVERYWHERE in the product (including the deleted-goal ledger), so wrongly stamping a
// user-entered row still makes the user's own money vanish from every surface they can reach.
//
// Mutants this goes RED on (both validated):
//   - SET managed_by_reconciliation = true  → the reconciler row is never archived.
//   - that SET plus dropping the WHERE leg  → the user's own row is marked reconciler-managed and
//     becomes eligible for the provider-triggered tombstone.
func TestArchiveByGoal_Integration_LeavesUserEnteredRowsAlone(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	repo := repository.NewSavingsContributionRepository(pool)

	// A contribution the user typed in themselves: manual, confirmed, never reconciler-owned.
	userRow := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        42.5,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, userRow))

	// A contribution the reconciler derived from a transfer, still under its management.
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	require.NoError(t, repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 100.0))
	var managedRowID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID).Scan(&managedRowID))
	require.True(t, managedFlag(t, pool, managedRowID), "fixture must start reconciler-managed")
	require.False(t, managedFlag(t, pool, userRow.ID), "fixture must start user-entered")

	// The goal switches progress basis away from "contributions".
	require.NoError(t, repo.ArchiveByGoal(ctx, goalID))

	// The reconciler's row is released: it is now a permanent historical record.
	assertManagedFlag(t, pool, managedRowID, false,
		"ArchiveByGoal must clear managed_by_reconciliation on the reconciler's own row")

	// The user's row is untouched — still theirs, still present, still the same money.
	assertManagedFlag(t, pool, userRow.ID, false,
		"ArchiveByGoal must never stamp a user-entered contribution as reconciler-managed: "+
			"DeleteManagedForTransaction tombstones rows carrying that flag")
	var amount float64
	var status, source string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT amount, status, source FROM savings_goal_contributions WHERE id = $1`,
		userRow.ID).Scan(&amount, &status, &source),
		"the user-entered contribution must still exist after a basis switch")
	assert.InDelta(t, 42.5, amount, 0.001, "the user's amount must survive the basis switch")
	assert.Equal(t, "confirmed", status, "the user's status must survive the basis switch")
	assert.Equal(t, "manual", source, "the user's source must survive the basis switch")
}

// managedFlag reads the stored managed_by_reconciliation of one contribution row, failing the
// test outright if the row is gone.
func managedFlag(t *testing.T, pool *pgxpool.Pool, contribID string) bool {
	t.Helper()
	var got bool
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT managed_by_reconciliation FROM savings_goal_contributions WHERE id = $1`,
		contribID).Scan(&got), "contribution row must still exist")
	return got
}

// assertManagedFlag is non-fatal on purpose: a mutation that mis-flags BOTH the reconciler's row
// and the user's row must be reported against both, not just the first one reached.
func assertManagedFlag(t *testing.T, pool *pgxpool.Pool, contribID string, want bool, msg string) {
	t.Helper()
	assert.Equal(t, want, managedFlag(t, pool, contribID), msg)
}

// TestUpsertProposedFromTransaction_Integration_LeavesNonManagedProposedRowAlone proves the C3
// guard against real rows. Its only other test asserts the guard by SUBSTRING-matching
// "managed_by_reconciliation = true" against the query text — which the INSERT's own column list
// value would satisfy on its own — and no fixture anywhere creates the row the guard is about:
// a status='proposed' contribution the reconciler does NOT own.
//
// RED on removing `AND savings_goal_contributions.managed_by_reconciliation = true` from the
// ON CONFLICT DO UPDATE WHERE clause: the reconciler then overwrites the user's amount.
func TestUpsertProposedFromTransaction_Integration_LeavesNonManagedProposedRowAlone(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -500.0)
	repo := repository.NewSavingsContributionRepository(pool)

	// A proposed row occupying the (transaction_id, savings_goal_id) conflict slot that the
	// reconciler does not own — e.g. one the user entered against a transaction themselves.
	manual := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        50.0,
		Source:        "manual",
		Status:        "proposed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, manual))
	require.False(t, managedFlag(t, pool, manual.ID), "fixture must be a NON-managed proposed row")

	// Sync sees the same transfer and proposes a different amount.
	require.NoError(t, repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 500.0))

	var rowCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "the conflict slot must still hold exactly one row")

	var amount float64
	var source string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT amount, source FROM savings_goal_contributions WHERE id = $1`,
		manual.ID).Scan(&amount, &source))
	assert.InDelta(t, 50.0, amount, 0.001,
		"the reconciler must not overwrite the amount on a proposed row it does not own")
	assert.Equal(t, "manual", source, "the reconciler must not take over a user's row")
	assertManagedFlag(t, pool, manual.ID, false,
		"the reconciler must not claim ownership of a row it did not create")
}

// TestInsertManualFromTransaction_Integration_RefusesToOverwriteAConfirmedRow drives a real
// confirmed row into the (transaction_id, savings_goal_id) conflict slot. Until now
// ErrContributionAlreadyConfirmed was only ever seen at the Huma layer from a MOCKED repo
// returning it — nothing proved the repository produces it, nor that the already-confirmed row is
// left as the user logged it rather than silently re-written with the second call's amount and
// date.
func TestInsertManualFromTransaction_Integration_RefusesToOverwriteAConfirmedRow(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	first, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 40.0, "2026-01-05")
	require.NoError(t, err, "the first manual log takes the conflict slot as status='confirmed'")
	require.Equal(t, "confirmed", first.Status)

	second, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 60.0, "2026-02-09")
	assert.ErrorIs(t, err, domain.ErrContributionAlreadyConfirmed,
		"a second manual log against an already-confirmed slot must be refused by the repository "+
			"itself, not only by a mock standing in for it")
	assert.Nil(t, second)

	var amount float64
	var occurredOn, status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT amount, occurred_on::text, status FROM savings_goal_contributions WHERE id = $1`,
		first.ID).Scan(&amount, &occurredOn, &status))
	assert.InDelta(t, 40.0, amount, 0.001, "the confirmed amount the user logged must stand")
	assert.Equal(t, "2026-01-05", occurredOn, "and so must the date they logged it on")
	assert.Equal(t, "confirmed", status)
}

// TestUpsertFromTransaction_Integration_RefusesToOverwriteAUserConfirmedRow is the regression test
// for #1569 round 12 task 3: an independent Codex audit found that UpsertFromTransaction (the
// AUTOMATIC writer reached from transferCorrelationService.createBoostMatchContribution) had NO
// predicate on its ON CONFLICT DO UPDATE, unlike InsertManualFromTransaction (the manual writer,
// WHERE status = 'proposed') and UpsertProposedFromTransaction (WHERE status = 'proposed' AND
// managed_by_reconciliation = true). A user confirms a contribution with an explicit amount, then a
// later automatic correlation/boost-match pass silently overwrote it — no audit, no user action.
//
// The discriminator is confirmed_by_user, not status: a machine-confirmed row (this same method's
// own INSERT sets status='confirmed', confirmed_by_user=false) must remain updatable by a later
// automatic pass — that is legitimate machine-to-machine correction, not the defect. Only a row
// with confirmed_by_user=true (on a transaction-linked row, set only by InsertManualFromTransaction and
// ConfirmContribution — both explicit user actions) may never be overwritten by this path.
func TestUpsertFromTransaction_Integration_RefusesToOverwriteAUserConfirmedRow(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	// The user manually confirms a $40 contribution against this transaction/goal pair.
	confirmed, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 40.0, "2026-01-05")
	require.NoError(t, err)
	require.Equal(t, "confirmed", confirmed.Status)
	require.True(t, confirmed.ConfirmedByUser, "fixture must be user-confirmed for this test to be meaningful")

	// Automatic transfer correlation later "sees" a different amount ($80) for the same
	// (transaction, goal) pair and tries to upsert it — this must NOT be treated as an error
	// (it's an expected race between the user and the reconciler), and must NOT overwrite the
	// user's ledger entry.
	err = repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 80.0, "boost_match")
	require.NoError(t, err, "the automatic writer must not surface an error merely because a "+
		"user-confirmed row already occupies the slot")

	var amount float64
	var source, status string
	var confirmedByUser bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT amount, source, status, confirmed_by_user FROM savings_goal_contributions WHERE id = $1`,
		confirmed.ID).Scan(&amount, &source, &status, &confirmedByUser))
	assert.InDelta(t, 40.0, amount, 0.001, "the user-confirmed amount must survive the automatic upsert")
	assert.Equal(t, "manual", source, "the automatic writer must not steal the row's source")
	assert.Equal(t, "confirmed", status)
	assert.True(t, confirmedByUser, "confirmed_by_user must remain true")

	var rowCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "the automatic writer must not have inserted a second row either")
}

// TestUpsertFromTransaction_Integration_DoesNotResurrectARejectedRow proves the fix does not
// merely swap one hole for another: confirmed_by_user is false on a REJECTED row too (nothing sets
// it true on rejection), so a naive `WHERE confirmed_by_user = false` predicate would let the
// automatic writer silently resurrect a row the user explicitly rejected back to 'confirmed' — the
// same class of defect, laundered through a different status value. The fix must also exclude
// status = 'rejected', matching the sibling automatic writer UpsertProposedFromTransaction's
// established rule (R3-7) that rejected tombstones are never touched by automatic paths.
func TestUpsertFromTransaction_Integration_DoesNotResurrectARejectedRow(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	// Sync proposes a contribution, then the user explicitly rejects it.
	require.NoError(t, repo.UpsertProposedFromTransaction(ctx, userID, goalID, txnID, 30.0))
	var proposedID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID).Scan(&proposedID))
	rejected, err := repo.RejectContribution(ctx, userID, goalID, proposedID)
	require.NoError(t, err)
	require.Equal(t, "rejected", rejected.Status)
	require.False(t, rejected.ConfirmedByUser, "rejection does not flip confirmed_by_user — this is exactly why status alone must also be checked")

	// A later automatic pass tries to write the same slot.
	err = repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 30.0, "boost_match")
	require.NoError(t, err)

	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, proposedID).Scan(&status))
	assert.Equal(t, "rejected", status, "the automatic writer must not resurrect an explicit user rejection")
}

// TestUpsertFromTransaction_Integration_StillOverwritesAMachineConfirmedRow pins that the fix does
// not overcorrect: two automatic writers (e.g. a proposed row promoted by one correlation pass,
// then re-touched by another) must still be able to update each other's rows. Only a USER's
// confirmation is protected, not every confirmed row.
func TestUpsertFromTransaction_Integration_StillOverwritesAMachineConfirmedRow(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	require.NoError(t, repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 25.0, "confirmed_transfer"))
	require.NoError(t, repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 35.0, "boost_match"))

	var amount float64
	var source string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT amount, source FROM savings_goal_contributions WHERE transaction_id = $1 AND savings_goal_id = $2`,
		txnID, goalID).Scan(&amount, &source))
	assert.InDelta(t, 35.0, amount, 0.001, "a machine-confirmed row must remain updatable by a later automatic pass")
	assert.Equal(t, "boost_match", source)
}

// TestSavingsContributionRepository_Integration_BasisRoundTripDoesNotDoubleCount is the #1625
// Task 12 regression test, written against real SQL because pgxmock cannot catch an aggregate
// error — the defect here IS the aggregate.
//
// The sequence that was broken:
//
//	ledger totals 100 → switch to manual → switch back to contributions
//	  → handleBasisSwitch seeds a synthetic 100
//	  → SumConfirmedByGoal returns the ORIGINAL 100 plus the synthetic 100 = 200
//
// ArchiveByGoal only cleared managed_by_reconciliation, which left the rows live and confirmed
// and — because user-entered rows already carry false — destroyed the only signal that could
// have distinguished them afterwards. Both sums now exclude archived rows.
func TestSavingsContributionRepository_Integration_BasisRoundTripDoesNotDoubleCount(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	repo := repository.NewSavingsContributionRepository(pool)

	insert := func(amount float64, managed bool) string {
		id := uuid.New().String()
		_, err := pool.Exec(ctx, `
			INSERT INTO savings_goal_contributions
				(id, savings_goal_id, transaction_id, amount, source, status,
				 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
			VALUES ($1, $2, NULL, $3, 'manual', 'confirmed', $4, true, CURRENT_DATE, now(), now())
		`, id, goalID, amount, managed)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE id = $1`, id)
		})
		return id
	}

	// A closed contributions period: one reconciler row and one the user entered by hand. The
	// user's row is the one the old flag-clearing approach could never account for.
	insert(60, true)
	insert(40, false)

	total, err := repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	require.Equal(t, 100.0, total, "precondition: the live ledger totals 100")

	// Leaving the contributions basis closes the period.
	require.NoError(t, repo.ArchiveByGoal(ctx, goalID))

	total, err = repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	assert.Equal(t, 0.0, total,
		"an archived period must not count — including the user's own manual row, which the "+
			"managed_by_reconciliation flag never covered")

	// Returning to the contributions basis seeds the carried-over balance.
	insert(100, false)

	total, err = repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	assert.Equal(t, 100.0, total,
		"the round-trip reported 200 before #1625: the archived period summed alongside its own seed")

	// Both sums must agree, or the lifetime total and the period total describe one ledger
	// differently.
	periodTotal, err := repo.SumConfirmedByGoalInPeriod(ctx, userID, goalID,
		time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Equal(t, 100.0, periodTotal, "SumConfirmedByGoalInPeriod must exclude archived rows too")

	// Archived rows are RETAINED, not destroyed — this is a retention system.
	var retained int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions
		  WHERE savings_goal_id = $1 AND archived_at IS NOT NULL AND deleted_at IS NULL`,
		goalID).Scan(&retained))
	assert.Equal(t, 2, retained, "the closed period must still be readable, just not counted")

	// Idempotent: archiving again must not disturb the live seed.
	require.NoError(t, repo.ArchiveByGoal(ctx, goalID))
	total, err = repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	assert.Equal(t, 0.0, total, "a second archive closes the new period too, and does not error")
}

// TestSavingsContributionRepository_Integration_ArchivedRowReleasesTheUniquenessSlot is the
// regression test for the defect migration 000190 CREATED and 000191 fixes (found by adversarial
// review, not by the original change).
//
// 000190 made archived_at a period boundary but left
// unique_contribution_per_goal_per_transaction conditional only on transaction_id/deleted_at, so
// an archived row still owned its (transaction, goal) slot. A goal returning to the contributions
// basis then could not re-record a transaction from a previous period: the upsert collided with
// the archived row and its DO UPDATE predicate (status='proposed') failed against an archived
// CONFIRMED row, so the caller was told the contribution already existed and the new period's
// contribution was never recorded. That is the opposite of what 000190 set out to fix.
func TestSavingsContributionRepository_Integration_ArchivedRowReleasesTheUniquenessSlot(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -250.0)
	repo := repository.NewSavingsContributionRepository(pool)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE savings_goal_id = $1`, goalID)
	})

	// Period 1: the transaction is recorded against the goal.
	first, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 250.0, "2026-01-15")
	require.NoError(t, err)
	require.NotNil(t, first)

	total, err := repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	require.Equal(t, 250.0, total)

	// The goal leaves the contributions basis: period 1 closes.
	require.NoError(t, repo.ArchiveByGoal(ctx, goalID))

	// Period 2: the SAME transaction must be recordable again. Before 000191 this returned
	// ErrContributionAlreadyConfirmed, because the archived row still held the unique slot.
	second, err := repo.InsertManualFromTransaction(ctx, userID, goalID, txnID, 250.0, "2026-06-15")
	require.NoError(t, err,
		"the archived row still owns the (transaction, goal) uniqueness slot, so the new period "+
			"cannot record this transaction at all")
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID, "a new period must get its OWN row, not reuse the archived one")

	// Only the live period counts.
	total, err = repo.SumConfirmedByGoal(ctx, userID, goalID)
	require.NoError(t, err)
	assert.Equal(t, 250.0, total, "the archived period must not be added to the live one")

	// And both rows are retained — the closed period is still history.
	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions
		  WHERE savings_goal_id = $1 AND deleted_at IS NULL`, goalID).Scan(&rows))
	assert.Equal(t, 2, rows, "both periods' rows must be retained")
}

// TestSavingsContributionRepository_Integration_ConfirmRejectAreBoundToTheirGoal closes the
// bypass an adversarial review found in the Task 10 basis gate.
//
// The handler validates the goal in the URL, but the repository looked the contribution up by its
// OWN id and the user id alone. A same-user request of the shape
//
//	/goals/{a-contributions-goal}/contributions/{a-manual-goal's-contribution}/confirm
//
// therefore passed goal A's basis gate and then mutated goal B's contribution — defeating exactly
// the protection the gate was added for, and leaving B's derived balance stale because the handler
// recomputes the goal from the URL.
//
// Bound in the SQL rather than in the handler: a handler-side check would be a TOCTOU race against
// the same FOR UPDATE this query already takes.
func TestSavingsContributionRepository_Integration_ConfirmRejectAreBoundToTheirGoal(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalA := seedIntegrationGoal(t, pool, userID, accountID)
	goalB := seedIntegrationGoal(t, pool, userID, accountID)
	repo := repository.NewSavingsContributionRepository(pool)

	// A proposed contribution belonging to goal B.
	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 75.0, 'manual', 'proposed', true, false, CURRENT_DATE, now(), now())
	`, contribID, goalB)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE id = $1`, contribID)
	})

	// Same user, but the contribution is not goal A's. Both must refuse.
	_, err = repo.ConfirmContribution(ctx, userID, goalA, contribID)
	require.ErrorIs(t, err, domain.ErrContributionNotFound,
		"confirming another goal's contribution under goal A's id must not succeed — that is the "+
			"basis-gate bypass")

	_, err = repo.RejectContribution(ctx, userID, goalA, contribID)
	require.ErrorIs(t, err, domain.ErrContributionNotFound,
		"rejecting another goal's contribution under goal A's id must not succeed")

	// The row is untouched.
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, contribID).Scan(&status))
	assert.Equal(t, "proposed", status, "the mismatched call mutated the row anyway")

	// The correct goal still works.
	confirmed, err := repo.ConfirmContribution(ctx, userID, goalB, contribID)
	require.NoError(t, err, "binding the goal must not break the legitimate call")
	assert.Equal(t, "confirmed", confirmed.Status)
}

// TestDeleteContribution_Integration_IsBoundToItsGoal is Task 1 of #1625: the same cross-goal
// bypass ConfirmContribution/RejectContribution had, but on the ONE hard delete of financial
// history on this branch. A prior round bound goalID into Confirm and Reject and missed Delete —
// the destructive path was the one left open.
//
// Before the fix, DeleteContribution looked the row up by c.id and g.user_id alone
// (savings_contribution_repository.go, pre-#1625), so a same-user
// DELETE /goals/{goalA}/contributions/{goalB's-contribution} permanently destroyed goal B's row —
// not a stale recompute like the Confirm/Reject bypass, but an irreversible loss of financial
// history with no restore path anywhere on this branch.
func TestDeleteContribution_Integration_IsBoundToItsGoal(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalA := seedIntegrationGoal(t, pool, userID, accountID)
	goalB := seedIntegrationGoal(t, pool, userID, accountID)
	repo := repository.NewSavingsContributionRepository(pool)

	// A confirmed contribution belonging to goal B — live, visible, destroyable data if named
	// through ITS OWN goal.
	contribID := uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO savings_goal_contributions
			(id, savings_goal_id, transaction_id, amount, source, status,
			 managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at)
		VALUES ($1, $2, NULL, 55.0, 'manual', 'confirmed', false, true, CURRENT_DATE, now(), now())
	`, contribID, goalB)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM savings_goal_contributions WHERE id = $1`, contribID)
	})

	// Same user, but the contribution is not goal A's. Must refuse — and MUST NOT destroy the row.
	err = repo.DeleteContribution(ctx, userID, goalA, contribID, nil)
	require.ErrorIs(t, err, domain.ErrContributionNotFound,
		"deleting another goal's contribution under goal A's id must not succeed — that is the "+
			"cross-goal DELETE bypass (#1625)")

	// The row must still exist — this is the one hard delete of financial history on this branch,
	// so a bypass here is irreversible, unlike the Confirm/Reject bypass's stale recompute.
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM savings_goal_contributions WHERE id = $1`, contribID).Scan(&status),
		"the row must still exist after the refused cross-goal delete")
	assert.Equal(t, "confirmed", status, "the mismatched call destroyed or mutated the row anyway")

	// The correct goal still works: binding goalID must not break the legitimate delete.
	require.NoError(t, repo.DeleteContribution(ctx, userID, goalB, contribID, nil),
		"binding the goal must not break the legitimate call")

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, contribID).Scan(&stillThere))
	assert.Equal(t, 0, stillThere, "the correct goal's delete must still destroy the row")
}

// TestSavingsContributionRepository_Integration_ArchivedRowReleasesCapacity_CrossGoal is Task 7.
//
// Migration 000191 narrowed unique_contribution_per_goal_per_transaction's uniqueness slot to
// `archived_at IS NULL`, but all three capacity checks (UpsertFromTransaction,
// InsertManualFromTransaction, ConfirmContribution) still summed
// `WHERE transaction_id = $1 AND status = 'confirmed' AND savings_goal_id != $2 AND deleted_at IS
// NULL` with no `archived_at IS NULL` leg — so an archived row kept reserving capacity forever.
//
// The obvious same-goal scenario ("a goal re-records its own transaction after archiving") proves
// nothing: every capacity query already carries `savings_goal_id != $2`, which excludes the goal
// being written to regardless of archived_at. The real exposure is CROSS-GOAL: goal A's archived
// row must not reserve capacity from goal B's fresh allocation of the SAME transaction.
func TestSavingsContributionRepository_Integration_ArchivedRowReleasesCapacity_CrossGoal(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalA := seedIntegrationGoal(t, pool, userID, accountID)
	goalB := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE savings_goal_id IN ($1, $2)`, goalA, goalB)
	})

	// Goal A allocates the full transaction amount.
	_, err := repo.InsertManualFromTransaction(ctx, userID, goalA, txnID, 100.0, "2026-01-15")
	require.NoError(t, err)

	// Goal A's period closes — its row is archived, not deleted.
	require.NoError(t, repo.ArchiveByGoal(ctx, goalA))

	// Goal B now allocates the SAME transaction for the FULL amount. Goal A's archived row must
	// not reserve any of that capacity — it is a closed period's history, not a live allocation.
	second, err := repo.InsertManualFromTransaction(ctx, userID, goalB, txnID, 100.0, "2026-06-15")
	require.NoError(t, err,
		"goal A's ARCHIVED row still reserved capacity from goal B — the capacity slot was not "+
			"released alongside the uniqueness slot 000191 freed (#1625 Task 7)")
	require.NotNil(t, second)
}

// TestSavingsContributionRepository_Integration_LiveRowStillReservesCapacity_CrossGoal guards the
// other direction of Task 7: adding `archived_at IS NULL` to the capacity queries must NOT disable
// the over-allocation guard for the case it exists for. A LIVE (non-archived) confirmed row on
// goal A must still block goal B from over-allocating the same transaction.
func TestSavingsContributionRepository_Integration_LiveRowStillReservesCapacity_CrossGoal(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalA := seedIntegrationGoal(t, pool, userID, accountID)
	goalB := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)
	repo := repository.NewSavingsContributionRepository(pool)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM savings_goal_contributions WHERE savings_goal_id IN ($1, $2)`, goalA, goalB)
	})

	// Goal A allocates the full transaction amount and stays LIVE (never archived).
	_, err := repo.InsertManualFromTransaction(ctx, userID, goalA, txnID, 100.0, "2026-01-15")
	require.NoError(t, err)

	// Goal B tries to allocate the same transaction for the full amount — must still be refused,
	// because goal A's row is live, not archived.
	_, err = repo.InsertManualFromTransaction(ctx, userID, goalB, txnID, 100.0, "2026-06-15")
	require.ErrorIs(t, err, domain.ErrContributionOverAllocated,
		"a LIVE cross-goal allocation must still be refused — the archived_at filter must not "+
			"weaken the guard for the case it exists for")
}
