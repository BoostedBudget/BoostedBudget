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
// reconciler-managed, and DeleteManagedForTransaction HARD-DELETES rows carrying that flag. So
// this test seeds one user-entered row alongside one reconciler row and asserts where each lands.
//
// Mutants this goes RED on (both validated):
//   - SET managed_by_reconciliation = true  → the reconciler row is never archived.
//   - that SET plus dropping the WHERE leg  → the user's own row is marked reconciler-managed and
//     becomes eligible for the hard delete.
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
			"DeleteManagedForTransaction hard-deletes rows carrying that flag")
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
// with confirmed_by_user=true (set exclusively by InsertManualFromTransaction and
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
	rejected, err := repo.RejectContribution(ctx, userID, proposedID)
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
