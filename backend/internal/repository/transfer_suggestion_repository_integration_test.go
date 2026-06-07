//go:build integration

package repository

// Integration tests for TransferSuggestionRepository.
//
// These tests verify SQL-layer guarantees that pgxmock cannot check:
// partial unique index on matched_transaction_id, status-guard WHERE clauses,
// and ExpirePending transition logic (pending → expired; accepted stays accepted).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration -v

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

// TestUpdateStatusAndComplete_StatusGuards_Integration verifies the SQL status-guard
// WHERE clauses in UpdateStatus and Complete:
//   - UpdateStatus only applies to pending rows (accepted/rejected/expired/completed → AlreadyActioned)
//   - Complete applies to pending AND accepted rows
//   - Rejected/expired/completed rows → AlreadyActioned from Complete
//   - ExpirePending only transitions pending rows (accepted/rejected/completed rows unchanged)
//
// pgxmock returns canned rows without running WHERE clauses; these guards require real SQL.
func TestUpdateStatusAndComplete_StatusGuards_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed a user and linked item (required by FK constraints on transfer_suggestions).
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID.String(), "status-guard-"+userID.String()+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-status-"+linkedItemID.String())
	require.NoError(t, err)

	// Helper to insert a suggestion with a specific status and optional expires_at.
	insertSugg := func(status string, expiresAt *time.Time) string {
		id := uuid.New().String()
		if expiresAt != nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO transfer_suggestions (id, user_id, amount, source, status, expires_at, created_at, updated_at)
				VALUES ($1, $2, 100.00, 'rule', $3, $4, NOW(), NOW())
			`, id, userID, status, expiresAt)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
				VALUES ($1, $2, 100.00, 'rule', $3, NOW(), NOW())
			`, id, userID, status)
		}
		require.NoError(t, err)
		return id
	}

	repo := NewTransferSuggestionRepository(tx)
	now := time.Now()

	// === UpdateStatus guards: only pending → accepted/rejected ===
	pendingID := insertSugg("pending", nil)
	acceptedID := insertSugg("accepted", nil)
	rejectedID := insertSugg("rejected", nil)

	// UpdateStatus on pending → succeeds.
	err = repo.UpdateStatus(ctx, pendingID, domain.SuggestionStatusAccepted, nil)
	require.NoError(t, err, "UpdateStatus pending→accepted must succeed")

	// UpdateStatus on already-accepted → AlreadyActioned.
	err = repo.UpdateStatus(ctx, acceptedID, domain.SuggestionStatusRejected, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"UpdateStatus accepted→rejected must return AlreadyActioned (WHERE status='pending' fails)")

	// UpdateStatus on rejected → AlreadyActioned.
	err = repo.UpdateStatus(ctx, rejectedID, domain.SuggestionStatusAccepted, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"UpdateStatus rejected→accepted must return AlreadyActioned")

	// === Complete guards: pending OR accepted → completed; terminal → AlreadyActioned ===
	pendingID2 := insertSugg("pending", nil)
	acceptedID2 := insertSugg("accepted", nil)
	rejectedID2 := insertSugg("rejected", nil)
	completedID := insertSugg("completed", nil)

	// Complete on pending → succeeds.
	err = repo.Complete(ctx, pendingID2, nil, 100.00, "user_confirmed", now)
	require.NoError(t, err, "Complete pending→completed must succeed")

	// Complete on accepted → succeeds.
	err = repo.Complete(ctx, acceptedID2, nil, 100.00, "exact_match", now)
	require.NoError(t, err, "Complete accepted→completed must succeed")

	// Complete on rejected → AlreadyActioned.
	err = repo.Complete(ctx, rejectedID2, nil, 100.00, "exact_match", now)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"Complete on rejected must return AlreadyActioned")

	// Complete on already-completed → AlreadyActioned.
	err = repo.Complete(ctx, completedID, nil, 100.00, "exact_match", now)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"Complete on completed must return AlreadyActioned")

	// === ExpirePending: only pending with past expires_at transitions; accepted stays accepted ===
	pastExpiry := now.Add(-time.Hour)
	futureExpiry := now.Add(30 * 24 * time.Hour)

	expiredPendingID := insertSugg("pending", &pastExpiry)   // should expire
	keptPendingID := insertSugg("pending", &futureExpiry)    // future expiry → stays pending
	keptAcceptedID := insertSugg("accepted", &pastExpiry)    // accepted + past expiry → MUST stay accepted

	count, err := repo.ExpirePending(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1), "at least one pending suggestion should have been expired")

	// Pending with past expiry → expired.
	sugg, err := repo.GetByID(ctx, expiredPendingID)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusExpired, sugg.Status,
		"pending suggestion with past expires_at must be transitioned to expired")

	// Pending with future expiry → still pending.
	sugg, err = repo.GetByID(ctx, keptPendingID)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusPending, sugg.Status,
		"pending suggestion with future expires_at must remain pending")

	// Accepted with past expiry → MUST remain accepted (user intent must not be auto-expired).
	sugg, err = repo.GetByID(ctx, keptAcceptedID)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusAccepted, sugg.Status,
		"accepted suggestion must never be auto-expired by ExpirePending (user explicitly accepted)")
}
