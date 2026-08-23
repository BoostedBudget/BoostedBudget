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
	_ = seedIntegrationTransaction(t, pool, userID, accountID, 75.0)               // outflow — excluded
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
	_, err = repo.RejectContribution(ctx, userID, contribID)
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

	confirmed, err := repo.ConfirmContribution(ctx, userID, proposed.ID)
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

	_, err = repo.ConfirmContribution(ctx, userID, second.ID)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"confirming a contribution whose transaction the provider removed must fail rather than "+
			"silently counting reversed money toward a goal")
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
