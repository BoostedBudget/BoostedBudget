//go:build integration

package repository

// Integration test for TransferSuggestionRepository.
// Verifies that the partial unique index on matched_transaction_id
// (WHERE matched_transaction_id IS NOT NULL) prevents one inflow transaction
// from completing two suggestions, while NULL completions remain unrestricted.
//
// pgxmock cannot verify partial-index uniqueness because it never sends SQL
// to a real planner — it matches query strings and returns canned rows.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestCompletePartialUnique -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletePartialUniqueMatchedTxn_Integration proves that:
//  1. Completing suggestion A with matched_transaction_id=T succeeds.
//  2. Completing suggestion B with the same T errors (partial unique index violation).
//  3. Completing suggestions C and D with matched_transaction_id=nil both succeed
//     (NULL values are exempt from the partial unique index).
//
// After the test, suggestion B must remain pending (the violation was rolled back
// via SAVEPOINT so the transaction stays usable for subsequent steps).
func TestCompletePartialUniqueMatchedTxn_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed the data hierarchy needed for the matched_transaction_id FK reference:
	// users → linked_items → accounts → transactions.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "int-sugg-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-test-savings', 'Test Savings', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// Seed the inflow transaction that will be the target of matched_transaction_id.
	txnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'plaid', -500.00, '2024-01-15', 'Transfer to Savings')
	`, txnID, userID, accountID, linkedItemID)
	require.NoError(t, err, "seed transaction")

	// Seed 4 pending transfer suggestions.
	suggIDs := make([]string, 4)
	for i := range suggIDs {
		suggIDs[i] = uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 500.00, 'rule', 'pending', NOW(), NOW())
		`, suggIDs[i], userID)
		require.NoError(t, err, "seed transfer_suggestion")
	}

	repo := NewTransferSuggestionRepository(tx)
	txnIDStr := txnID.String()
	now := time.Now()

	// Step 1: Complete suggestion A with matched_transaction_id=T → must succeed.
	err = repo.Complete(ctx, suggIDs[0], &txnIDStr, 500.00, "exact_match", now)
	require.NoError(t, err)

	// Step 2: Complete suggestion B with the SAME matched_transaction_id=T → must error
	// with a unique violation (SQLSTATE 23505) from the partial unique index.
	// Use a SAVEPOINT so the transaction remains usable after the expected error.
	_, err = tx.Exec(ctx, "SAVEPOINT sp_dup_check")
	require.NoError(t, err)

	err = repo.Complete(ctx, suggIDs[1], &txnIDStr, 500.00, "exact_match", now)
	require.Error(t, err, "completing suggestion B with the same matched_transaction_id must error")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr,
		"error must be a PostgreSQL error, not a timeout or network issue")
	assert.Equal(t, "23505", pgErr.Code,
		"SQLSTATE must be 23505 (unique_violation) — the partial unique index idx_transfer_sugg_matched_txn_id fired")

	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_dup_check")
	require.NoError(t, err)

	// Step 3: Complete suggestion C with nil (user-confirmed, no specific transaction) → must succeed.
	err = repo.Complete(ctx, suggIDs[2], nil, 500.00, "user_confirmed", now)
	require.NoError(t, err)

	// Step 4: Complete suggestion D with nil → must also succeed, proving NULLs don't conflict.
	err = repo.Complete(ctx, suggIDs[3], nil, 500.00, "user_confirmed", now)
	require.NoError(t, err)

	// Verify suggestion B is still pending — the savepoint rollback worked correctly.
	suggB, err := repo.GetByID(ctx, suggIDs[1])
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusPending, suggB.Status)
}
