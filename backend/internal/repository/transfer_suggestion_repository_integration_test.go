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
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
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
			VALUES ($1, $2, 500.00, 'boost', 'pending', NOW(), NOW())
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

// TestReserveMatchedTransaction_CrossSyncDoubleCountGuard_Integration is the core GAP AH
// regression test. It proves the reserve/retain/release pattern prevents the same inflow
// transaction T from completing two different suggestions across sync runs:
//
//  1. Reserve T on suggestion A (fuzzy detect, first sync).
//  2. Retain T when confirming A — Complete(A, &T, ...) succeeds, T now locked to A.
//  3. Attempt to reserve T on suggestion B (second sync) → partial unique index blocks it (23505).
//  4. Attempt to Complete B with &T directly → 23505 again (belt-and-suspenders).
//  5. Complete B with nil (user-confirmed, no inflow) → succeeds (NULLs exempt from index).
//
// Before this fix, step 2 completed A with nil (fuzzy path) and step 3 also completed B
// with nil → two completed rows, double-count in the aggregate. Now step 3 errors at the
// reserve step, so the second sync cannot claim the same inflow.
func TestReserveMatchedTransaction_CrossSyncDoubleCountGuard_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user, linked_item, account, and an inflow transaction T.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "gapah-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-gapah-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-hysa', 'HYSA', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	txnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'plaid', -200.00, '2024-03-01', 'HYSA Deposit')
	`, txnID, userID, accountID, linkedItemID)
	require.NoError(t, err, "seed inflow transaction T")

	// Seed two pending suggestions (A and B) that both could match the same inflow.
	suggA := uuid.New().String()
	suggB := uuid.New().String()
	for _, id := range []string{suggA, suggB} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 200.00, 'boost', 'pending', NOW(), NOW())
		`, id, userID)
		require.NoError(t, err, "seed suggestion")
	}

	repo := NewTransferSuggestionRepository(tx)
	txnIDStr := txnID.String()
	now := time.Now()

	// Step 1: First sync — fuzzy match reserves T on suggestion A.
	err = repo.ReserveMatchedTransaction(ctx, suggA, txnIDStr)
	require.NoError(t, err, "step 1: reserve T on suggestion A must succeed")

	// Verify reservation persisted.
	suggAState, err := repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	require.NotNil(t, suggAState.MatchedTransactionID,
		"step 1: matched_transaction_id must be set after reservation")
	assert.Equal(t, txnIDStr, *suggAState.MatchedTransactionID)

	// Step 2: User confirms A — Complete retains the reserved T.
	err = repo.Complete(ctx, suggA, &txnIDStr, 200.00, "user_confirmed", now)
	require.NoError(t, err, "step 2: Complete with retained T must succeed")

	// Verify A is completed with T.
	suggAState, err = repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusCompleted, suggAState.Status)
	require.NotNil(t, suggAState.MatchedTransactionID,
		"step 2: completed suggestion must retain matched_transaction_id")
	assert.Equal(t, txnIDStr, *suggAState.MatchedTransactionID)

	// Step 3: Second sync — attempt to reserve T on suggestion B.
	// The partial unique index (WHERE matched_transaction_id IS NOT NULL) blocks this.
	_, err = tx.Exec(ctx, "SAVEPOINT sp_reserve_b")
	require.NoError(t, err)

	err = repo.ReserveMatchedTransaction(ctx, suggB, txnIDStr)
	require.Error(t, err, "step 3: reserving the same T on suggestion B must error")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "step 3: must be a Postgres error (index violation)")
	assert.Equal(t, "23505", pgErr.Code,
		"step 3: SQLSTATE 23505 — partial unique index prevented double-claim of inflow T")

	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_reserve_b")
	require.NoError(t, err)

	// Verify suggestion B is still pending and has no reservation.
	suggBState, err := repo.GetByID(ctx, suggB)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusPending, suggBState.Status,
		"step 3: B must remain pending after blocked reserve")
	assert.Nil(t, suggBState.MatchedTransactionID,
		"step 3: B must have no reservation after blocked reserve")

	// Step 4: Belt-and-suspenders — Complete B with &T directly also errors.
	_, err = tx.Exec(ctx, "SAVEPOINT sp_complete_b")
	require.NoError(t, err)

	err = repo.Complete(ctx, suggB, &txnIDStr, 200.00, "exact_match", now)
	require.Error(t, err, "step 4: Complete B with &T must error (partial unique index)")
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code)

	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_complete_b")
	require.NoError(t, err)

	// Step 5: Complete B with nil (user-confirmed, no specific transaction) — succeeds.
	// NULLs are exempt from the partial unique index.
	err = repo.Complete(ctx, suggB, nil, 150.00, "user_confirmed", now)
	require.NoError(t, err, "step 5: Complete B with nil matched_transaction_id must succeed")

	// Verify: exactly ONE completed row carries T (A), B's actual_amount is distinct from A.
	suggBState, err = repo.GetByID(ctx, suggB)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusCompleted, suggBState.Status)
	assert.Nil(t, suggBState.MatchedTransactionID,
		"step 5: B completed with nil matched_transaction_id — inflow T only counts once")
}

// TestClearStaleReservations_Integration verifies the ClearStaleReservations SQL:
//   - A pending suggestion with a past-expiry reservation has its reservation cleared (status stays pending).
//   - An accepted suggestion with a past-expiry reservation also has its reservation cleared (status stays accepted).
//   - A suggestion with a future-expiry reservation is NOT cleared.
//   - A completed suggestion (even with a past-expiry matched_transaction_id) is NOT cleared.
func TestClearStaleReservations_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "stale-res-"+userID.String()+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-stale-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-stale', 'Stale Test', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err)

	// Seed 4 inflow transactions (one per suggestion, to hold as reservations).
	txnIDs := make([]string, 4)
	for i := range txnIDs {
		id := uuid.New()
		txnIDs[i] = id.String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', -100.00, '2024-01-01', 'deposit')
		`, id, userID, accountID, linkedItemID)
		require.NoError(t, err)
	}

	pastExpiry := time.Now().Add(-time.Hour)
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)

	// Suggestion 1: pending, past expiry, reserved.
	stale1 := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'pending', $3, $4, NOW(), NOW())
	`, stale1, userID, txnIDs[0], pastExpiry)
	require.NoError(t, err, "seed stale1 (pending, past expiry, reserved)")

	// Suggestion 2: accepted, past expiry, reserved — status must NOT change.
	stale2 := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'accepted', $3, $4, NOW(), NOW())
	`, stale2, userID, txnIDs[1], pastExpiry)
	require.NoError(t, err, "seed stale2 (accepted, past expiry, reserved)")

	// Suggestion 3: pending, future expiry, reserved — should NOT be cleared.
	fresh := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'pending', $3, $4, NOW(), NOW())
	`, fresh, userID, txnIDs[2], futureExpiry)
	require.NoError(t, err, "seed fresh (pending, future expiry, reserved)")

	// Suggestion 4: completed, past expiry — completed rows must NOT be touched by ClearStaleReservations.
	completedWithTxn := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, completed_at, actual_amount, completion_source, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'completed', $3, $4, NOW(), 100.00, 'exact_match', NOW(), NOW())
	`, completedWithTxn, userID, txnIDs[3], pastExpiry)
	require.NoError(t, err, "seed completedWithTxn (completed, past expiry, matched)")

	repo := NewTransferSuggestionRepository(tx)

	cleared, err := repo.ClearStaleReservations(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cleared, int64(2),
		"ClearStaleReservations must clear stale1 (pending+past) and stale2 (accepted+past)")

	// stale1: reservation cleared, still pending.
	s1, err := repo.GetByID(ctx, stale1)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusPending, s1.Status,
		"stale1 must remain pending — ClearStaleReservations does not change status")
	assert.Nil(t, s1.MatchedTransactionID,
		"stale1 reservation must be cleared (inflow T freed for re-matching)")

	// stale2: reservation cleared, still accepted.
	s2, err := repo.GetByID(ctx, stale2)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusAccepted, s2.Status,
		"stale2 must remain accepted — ClearStaleReservations must not change accepted status")
	assert.Nil(t, s2.MatchedTransactionID,
		"stale2 reservation must be cleared (inflow T freed)")

	// fresh: reservation intact (future expiry).
	sf, err := repo.GetByID(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusPending, sf.Status)
	require.NotNil(t, sf.MatchedTransactionID,
		"fresh reservation must NOT be cleared (future expiry)")
	assert.Equal(t, txnIDs[2], *sf.MatchedTransactionID)

	// completedWithTxn: completed rows must not be touched.
	sc, err := repo.GetByID(ctx, completedWithTxn)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusCompleted, sc.Status)
	require.NotNil(t, sc.MatchedTransactionID,
		"completed row's matched_transaction_id must NOT be cleared by ClearStaleReservations")
	assert.Equal(t, txnIDs[3], *sc.MatchedTransactionID)
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
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
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
				VALUES ($1, $2, 100.00, 'boost', $3, $4, NOW(), NOW())
			`, id, userID, status, expiresAt)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
				VALUES ($1, $2, 100.00, 'boost', $3, NOW(), NOW())
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

	expiredPendingID := insertSugg("pending", &pastExpiry) // should expire
	keptPendingID := insertSugg("pending", &futureExpiry)  // future expiry → stays pending
	keptAcceptedID := insertSugg("accepted", &pastExpiry)  // accepted + past expiry → MUST stay accepted

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

// TestCompleteRetainingReservation_TOCTOUGuard_Integration proves GAP AR:
// CompleteRetainingReservation uses IS NOT DISTINCT FROM to guard the confirm
// read→write window. If the DB's matched_transaction_id changed between the
// service's GetByID and the repo's update (stale-clear + reclaim race), the
// update matches 0 rows and returns ErrTransferSuggestionAlreadyActioned.
//
// Cases verified:
//  1. Confirm with the correct observed reservation succeeds.
//  2. Confirm with nil observed reservation (no reservation) succeeds (normal user-confirmed path).
//  3. Confirm with a STALE observed reservation (DB was cleared between read and write) fails with AlreadyActioned.
//  4. Confirm on a terminal suggestion returns AlreadyActioned regardless of observed reservation.
func TestCompleteRetainingReservation_TOCTOUGuard_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user, linked_item, account, and two inflow transactions T1 and T2.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "gapar-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token-ar')
	`, linkedItemID, userID, "item-gapar-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-gapar', 'HYSA AR', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	t1ID := uuid.New()
	t2ID := uuid.New()
	for _, id := range []uuid.UUID{t1ID, t2ID} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', -100.00, '2024-03-01', 'Inflow')
		`, id, userID, accountID, linkedItemID)
		require.NoError(t, err, "seed transaction")
	}

	insertSugg := func() string {
		id := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 100.00, 'boost', 'pending', NOW(), NOW())
		`, id, userID)
		require.NoError(t, err, "seed suggestion")
		return id
	}

	repo := NewTransferSuggestionRepository(tx)
	now := time.Now()
	t1Str := t1ID.String()
	t2Str := t2ID.String()

	// Case 1: confirm with correct observed reservation succeeds.
	s1 := insertSugg()
	require.NoError(t, repo.ReserveMatchedTransaction(ctx, s1, t1Str))
	err = repo.CompleteRetainingReservation(ctx, s1, 100.00, "user_confirmed", now, &t1Str)
	require.NoError(t, err, "case 1: confirm with correct observed reservation must succeed")
	sugg, err := repo.GetByID(ctx, s1)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusCompleted, sugg.Status, "case 1: suggestion must be completed")

	// Case 2: confirm with nil observed reservation (normal user-confirmed, no fuzzy reservation).
	s2 := insertSugg()
	err = repo.CompleteRetainingReservation(ctx, s2, 100.00, "user_confirmed", now, nil)
	require.NoError(t, err, "case 2: confirm with nil observed reservation must succeed")
	sugg, err = repo.GetByID(ctx, s2)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusCompleted, sugg.Status, "case 2: suggestion must be completed")

	// Case 3: stale observed reservation — DB was cleared between read and write.
	// Use T2 (T1 is locked by completed s1). The service reads T2 as the reservation,
	// then ClearMatchedTransactionID runs (stale-clear path), then the confirm arrives
	// with the stale T2 value.
	// IS NOT DISTINCT FROM predicate: DB has NULL, expected is &T2 → no match → AlreadyActioned.
	s3 := insertSugg()
	require.NoError(t, repo.ReserveMatchedTransaction(ctx, s3, t2Str), "case 3: initial reserve")
	// Simulate stale-clear (reservation released, e.g. ClearStaleReservations ran).
	_, err = tx.Exec(ctx, `UPDATE transfer_suggestions SET matched_transaction_id = NULL WHERE id = $1`, s3)
	require.NoError(t, err, "case 3: simulate stale-clear")
	// Now confirm arrives with the stale (now-gone) reservation.
	err = repo.CompleteRetainingReservation(ctx, s3, 100.00, "user_confirmed", now, &t2Str)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"case 3: stale reservation mismatch must return AlreadyActioned (IS NOT DISTINCT FROM guard)")

	// Case 4: terminal suggestion → AlreadyActioned regardless of observed reservation.
	s4 := insertSugg()
	require.NoError(t, repo.UpdateStatus(ctx, s4, domain.SuggestionStatusRejected, nil))
	err = repo.CompleteRetainingReservation(ctx, s4, 100.00, "user_confirmed", now, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"case 4: terminal suggestion must return AlreadyActioned")
}

// TestClearMatchedTransactionID_ZeroRowAlreadyActioned_Integration proves GAP AQ:
// ClearMatchedTransactionID checks RowsAffected() and returns the appropriate
// error when the WHERE clause matches 0 rows (suggestion is terminal).
func TestClearMatchedTransactionID_ZeroRowAlreadyActioned_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "gapaq-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	insertSugg := func(status string) string {
		id := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 100.00, 'boost', $3, NOW(), NOW())
		`, id, userID, status)
		require.NoError(t, err, "seed suggestion")
		return id
	}

	repo := NewTransferSuggestionRepository(tx)

	// Terminal suggestions always return AlreadyActioned regardless of expectedTxnID —
	// the status guard fires before the IS NOT DISTINCT FROM check matters.
	rejectedID := insertSugg("rejected")
	err = repo.ClearMatchedTransactionID(ctx, rejectedID, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"clearing a rejected suggestion must return AlreadyActioned (GAP AQ: 0-row guard)")

	completedID := insertSugg("completed")
	err = repo.ClearMatchedTransactionID(ctx, completedID, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"clearing a completed suggestion must return AlreadyActioned (GAP AQ: 0-row guard)")

	expiredID := insertSugg("expired")
	err = repo.ClearMatchedTransactionID(ctx, expiredID, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"clearing an expired suggestion must return AlreadyActioned (terminal status)")

	// Clearing a non-existent suggestion returns NotFound.
	err = repo.ClearMatchedTransactionID(ctx, uuid.New().String(), nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionNotFound,
		"clearing a non-existent suggestion must return NotFound (classifyZeroRowUpdate)")

	// Clearing a pending suggestion with nil expectedTxnID (no reservation) → succeeds
	// because IS NOT DISTINCT FROM NULL = NULL is TRUE.
	pendingID := insertSugg("pending")
	err = repo.ClearMatchedTransactionID(ctx, pendingID, nil)
	require.NoError(t, err,
		"clearing a pending suggestion with nil expectedTxnID (happy path) must succeed")
}

// TestClearMatchedTransactionID_WrongExpected_DoesNotRelease proves GAP AV:
// ClearMatchedTransactionID with a stale expectedTxnID must not release a newer
// reservation T2 on the same suggestion.
//
// Scenario:
//  1. Reserve inflow T on suggestion A.
//  2. Stale-clear (simulate ClearStaleReservations: set matched_transaction_id = NULL).
//  3. Reserve inflow T2 on suggestion A (new sync claimed T2).
//  4. Call ClearMatchedTransactionID(A, expectedTxnID=&T) — old decline arrives.
//  5. Assert returns ErrTransferSuggestionAlreadyActioned and T2 is still reserved on A.
func TestClearMatchedTransactionID_WrongExpected_DoesNotRelease(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user and linked_item/account/transactions.
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "gapav-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-gapav-token')
	`, linkedItemID, userID, "item-gapav-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-gapav', 'GAP AV Test', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// Seed two inflow transactions T and T2.
	tID := uuid.New()
	t2ID := uuid.New()
	for _, id := range []uuid.UUID{tID, t2ID} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', -100.00, '2024-03-01', 'Deposit')
		`, id, userID, accountID, linkedItemID)
		require.NoError(t, err, "seed transaction")
	}

	// Seed suggestion A.
	suggA := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'pending', NOW(), NOW())
	`, suggA, userID)
	require.NoError(t, err, "seed suggestion A")

	repo := NewTransferSuggestionRepository(tx)
	tStr := tID.String()
	t2Str := t2ID.String()

	// Step 1: Reserve T on suggestion A.
	require.NoError(t, repo.ReserveMatchedTransaction(ctx, suggA, tStr), "step 1: reserve T")

	// Step 2: Simulate stale-clear (ClearStaleReservations released T).
	_, err = tx.Exec(ctx, `UPDATE transfer_suggestions SET matched_transaction_id = NULL WHERE id = $1`, suggA)
	require.NoError(t, err, "step 2: stale-clear")

	// Step 3: New sync reserves T2 on suggestion A.
	require.NoError(t, repo.ReserveMatchedTransaction(ctx, suggA, t2Str), "step 3: reserve T2")

	// Verify T2 is now the reservation.
	state, err := repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	require.NotNil(t, state.MatchedTransactionID)
	assert.Equal(t, t2Str, *state.MatchedTransactionID, "step 3: T2 must be the current reservation")

	// Step 4: Old decline arrives carrying stale T — must NOT release T2.
	// GAP AV: IS NOT DISTINCT FROM $3 compares T vs T2 → no match → 0 rows → AlreadyActioned.
	err = repo.ClearMatchedTransactionID(ctx, suggA, &tStr)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"step 4: stale decline with wrong expectedTxnID must return AlreadyActioned (GAP AV guard)")

	// Step 5: T2 must still be reserved on A.
	state, err = repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	require.NotNil(t, state.MatchedTransactionID, "step 5: T2 must still be reserved after stale decline")
	assert.Equal(t, t2Str, *state.MatchedTransactionID, "step 5: reservation must remain T2, not be cleared")
}

// TestDeclineReleasesReservationForReClaim_Integration proves GAP AI:
// ClearMatchedTransactionID releases the reservation so that a second suggestion
// can claim the same inflow transaction.
//
//  1. Seed user + linked_item + account + transaction T.
//  2. Seed suggestion A and suggestion B (both pending).
//  3. Reserve T on A → matched_transaction_id = T.
//  4. Decline A → ClearMatchedTransactionID → matched_transaction_id = NULL.
//  5. Reserve T on B → succeeds (T is free again).
func TestDeclineReleasesReservationForReClaim_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "decline-ai-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-decline-token')
	`, linkedItemID, userID, "item-decline-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-decline', 'Decline Test', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	txnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'plaid', -300.00, '2024-03-01', 'Savings Transfer')
	`, txnID, userID, accountID, linkedItemID)
	require.NoError(t, err, "seed transaction T")

	suggA := uuid.New().String()
	suggB := uuid.New().String()
	for _, id := range []string{suggA, suggB} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 300.00, 'boost', 'pending', NOW(), NOW())
		`, id, userID)
		require.NoError(t, err, "seed suggestion "+id)
	}

	repo := NewTransferSuggestionRepository(tx)
	txnIDStr := txnID.String()

	// Step 3: Reserve T on A.
	err = repo.ReserveMatchedTransaction(ctx, suggA, txnIDStr)
	require.NoError(t, err, "step 3: reserve T on suggestion A must succeed")

	suggAState, err := repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	require.NotNil(t, suggAState.MatchedTransactionID)
	assert.Equal(t, txnIDStr, *suggAState.MatchedTransactionID, "step 3: A must hold reservation on T")

	// Step 4: Decline A — clear the reservation.
	// GAP AV: pass the observed reservation (txnIDStr) as expectedTxnID.
	err = repo.ClearMatchedTransactionID(ctx, suggA, &txnIDStr)
	require.NoError(t, err, "step 4: ClearMatchedTransactionID must succeed")

	suggAState, err = repo.GetByID(ctx, suggA)
	require.NoError(t, err)
	assert.Nil(t, suggAState.MatchedTransactionID,
		"step 4: A.matched_transaction_id must be NULL after decline")

	// Step 5: Reserve T on B — must now succeed since T was freed.
	err = repo.ReserveMatchedTransaction(ctx, suggB, txnIDStr)
	require.NoError(t, err, "step 5: reserving T on B must succeed after A declined")

	suggBState, err := repo.GetByID(ctx, suggB)
	require.NoError(t, err)
	require.NotNil(t, suggBState.MatchedTransactionID)
	assert.Equal(t, txnIDStr, *suggBState.MatchedTransactionID,
		"step 5: B must now hold the reservation on T")
}

// TestClearStaleReservations_ClosesMatchNotifications_Integration verifies GAP BA:
// when ClearStaleReservations frees a stale reservation it must also mark the
// associated open transfer_suggestion_match notification as read so that:
//
//	(a) The stale prompt is visually closed for the user.
//	(b) The type-scoped open-notification guard in CorrelateForUser no longer
//	    suppresses re-prompting on the stranded accepted suggestion.
//
// pgxmock cannot verify this because the two table updates are only linked by
// the SQL CTE — a unit mock would not execute the JOIN.
func TestClearStaleReservations_ClosesMatchNotifications_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user + account hierarchy (needed for FK constraints).
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|ba-"+userID.String(), "notif-close-"+userID.String()+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-ba-token')
	`, linkedItemID, userID, "item-ba-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-ba', 'BA Test', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err)

	// Seed two inflow transactions (one per suggestion).
	staleTxnID := uuid.New()
	freshTxnID := uuid.New()
	for _, txnID := range []uuid.UUID{staleTxnID, freshTxnID} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', -200.00, '2024-02-01', 'inflow')
		`, txnID, userID, accountID, linkedItemID)
		require.NoError(t, err)
	}

	pastExpiry := time.Now().Add(-1 * time.Hour)
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)

	// Stale suggestion: accepted, past expiry, has reserved staleTxnID.
	staleID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, created_at, updated_at)
		VALUES ($1, $2, 200.00, 'boost', 'accepted', $3, $4, NOW(), NOW())
	`, staleID, userID, staleTxnID.String(), pastExpiry)
	require.NoError(t, err)

	// Fresh suggestion: pending, future expiry, has reserved freshTxnID.
	freshID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, expires_at, created_at, updated_at)
		VALUES ($1, $2, 200.00, 'boost', 'pending', $3, $4, NOW(), NOW())
	`, freshID, userID, freshTxnID.String(), futureExpiry)
	require.NoError(t, err)

	// Open transfer_suggestion_match notification for the stale suggestion.
	staleNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, false, false, 'accept_reject', $6, NOW(), NOW())
	`, staleNotifID, userID, domain.NotificationTypeTransferSuggestionMatch,
		"Did you transfer a different amount?", "We suggested $200.00 but detected $190.00", staleID)
	require.NoError(t, err, "seed open match notification for stale suggestion")

	// Open transfer_suggestion_match notification for the fresh suggestion.
	freshNotifID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications
			(id, user_id, type, title, message, read, sent_email, interaction_pattern, transfer_suggestion_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, false, false, 'accept_reject', $6, NOW(), NOW())
	`, freshNotifID, userID, domain.NotificationTypeTransferSuggestionMatch,
		"Did you transfer a different amount?", "We suggested $200.00 but detected $190.00", freshID)
	require.NoError(t, err, "seed open match notification for fresh suggestion")

	repo := NewTransferSuggestionRepository(tx)

	cleared, err := repo.ClearStaleReservations(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cleared, int64(1), "at least the stale suggestion reservation must be cleared")

	// GAP BA: stale suggestion's match notification must be marked read.
	var staleRead bool
	err = tx.QueryRow(ctx, `SELECT read FROM notifications WHERE id = $1`, staleNotifID).Scan(&staleRead)
	require.NoError(t, err)
	assert.True(t, staleRead,
		"GAP BA: stale suggestion's open transfer_suggestion_match notification must be closed "+
			"(read=true) when ClearStaleReservations frees the reservation — otherwise the "+
			"correlation guard suppresses future re-prompting and strands the accepted suggestion")

	// Fresh suggestion's match notification must remain unread (expiry not reached).
	var freshRead bool
	err = tx.QueryRow(ctx, `SELECT read FROM notifications WHERE id = $1`, freshNotifID).Scan(&freshRead)
	require.NoError(t, err)
	assert.False(t, freshRead,
		"fresh suggestion's notification must NOT be closed (expiry not yet reached)")
}

// TestGetByUserIDStatuses_ReturnsFilteredByStatus_Integration proves that
// GetByUserIDStatuses returns only the rows matching the requested status slice
// and orders them most-recent-first.
//
// pgxmock cannot verify the ANY($2) array binding or ORDER BY — this requires
// a real Postgres connection.
func TestGetByUserIDStatuses_ReturnsFilteredByStatus_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|filter-"+userID.String(), "status-filter-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed one suggestion of each status, with distinct created_at times so ordering is deterministic.
	type statusEntry struct {
		id     string
		status domain.SuggestionStatus
	}
	statuses := []statusEntry{
		{uuid.New().String(), domain.SuggestionStatusPending},
		{uuid.New().String(), domain.SuggestionStatusAccepted},
		{uuid.New().String(), domain.SuggestionStatusRejected},
		{uuid.New().String(), domain.SuggestionStatusExpired},
		{uuid.New().String(), domain.SuggestionStatusCompleted},
	}
	base := time.Now().Add(-5 * time.Minute)
	for i, entry := range statuses {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 100.00, 'boost', $3, $4, $4)
		`, entry.id, userID, string(entry.status), createdAt)
		require.NoError(t, err, "seed suggestion status=%s", entry.status)
	}

	repo := NewTransferSuggestionRepository(tx)

	// Query accepted only — expect exactly the accepted row.
	items, total, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusAccepted}, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total, "total must equal the count of accepted rows")
	require.Len(t, items, 1, "items must contain exactly the accepted suggestion")
	assert.Equal(t, string(domain.SuggestionStatusAccepted), string(items[0].Status))

	// Query completed+rejected+expired — expect 3 rows, most-recent-first.
	items, total, err = repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{
			domain.SuggestionStatusCompleted,
			domain.SuggestionStatusRejected,
			domain.SuggestionStatusExpired,
		}, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total, "total must equal count of completed+rejected+expired rows")
	require.Len(t, items, 3, "items must contain completed, rejected, expired")

	// Verify most-recent-first ordering by created_at.
	for i := 0; i < len(items)-1; i++ {
		assert.True(t, !items[i].CreatedAt.Before(items[i+1].CreatedAt),
			"items must be ordered most-recent-first (index %d vs %d)", i, i+1)
	}
}

// TestGetByUserIDStatuses_EmptySlice_ReturnsPendingOnly_Integration proves that
// passing a nil status slice preserves the original pending-only semantics,
// matching the existing GetPendingByUserID contract.
func TestGetByUserIDStatuses_EmptySlice_ReturnsPendingOnly_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|empty-"+userID.String(), "empty-slice-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Seed one pending and one completed suggestion.
	pendingID := uuid.New().String()
	completedID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
		VALUES ($1, $2, 200.00, 'boost', 'pending', NOW(), NOW()),
		       ($3, $2, 200.00, 'boost', 'completed', NOW() - interval '1 minute', NOW())
	`, pendingID, userID, completedID)
	require.NoError(t, err)

	repo := NewTransferSuggestionRepository(tx)

	// nil/empty slice → pending only.
	items, total, err := repo.GetByUserIDStatuses(ctx, userID.String(), nil, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total, "empty-slice call must return exactly the pending row (total=1)")
	require.Len(t, items, 1)
	assert.Equal(t, string(domain.SuggestionStatusPending), string(items[0].Status))
	assert.Equal(t, pendingID, items[0].ID)
}

// TestGetByUserIDStatuses_LimitOffset_Integration proves that limit and offset
// are respected and that total reflects the full filtered count (not just the
// page size), enabling UI pagination.
func TestGetByUserIDStatuses_LimitOffset_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|page-"+userID.String(), "page-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Seed 5 completed suggestions with distinct created_at times.
	base := time.Now().Add(-10 * time.Minute)
	var suggIDs [5]string
	for i := range suggIDs {
		suggIDs[i] = uuid.New().String()
		createdAt := base.Add(time.Duration(i) * time.Minute)
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_suggestions
				(id, user_id, amount, source, status, created_at, updated_at)
			VALUES ($1, $2, 50.00, 'boost', 'completed', $3, $3)
		`, suggIDs[i], userID, createdAt)
		require.NoError(t, err)
	}

	repo := NewTransferSuggestionRepository(tx)

	// Page 1: limit=2, offset=0 → 2 items, total=5.
	page1, total, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusCompleted}, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total, "total must reflect all 5 completed rows, not just page size")
	assert.Len(t, page1, 2, "page 1 must return exactly 2 items")

	// Page 2: limit=2, offset=2 → 2 items, total=5.
	page2, total2, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusCompleted}, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total2, "total must be consistent across pages")
	assert.Len(t, page2, 2, "page 2 must return exactly 2 items")

	// Pages must not overlap.
	page1IDs := map[string]bool{page1[0].ID: true, page1[1].ID: true}
	for _, item := range page2 {
		assert.False(t, page1IDs[item.ID], "page 2 must not repeat items from page 1")
	}

	// Last page: limit=2, offset=4 → 1 item, total=5.
	page3, total3, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusCompleted}, 2, 4)
	require.NoError(t, err)
	assert.Equal(t, 5, total3, "total must be consistent on last page")
	assert.Len(t, page3, 1, "last page must return only the remaining item")
}

// TestGetByUserIDStatuses_Enrichment_Integration proves that GetByUserIDStatuses
// populates the derived enrichment fields (ToAccountName, FromAccountName, BoostName,
// MatchedTransactionAt) when the underlying rows have those relationships.
// These fields come from JOINs and cannot be tested with pgxmock.
func TestGetByUserIDStatuses_Enrichment_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|enr-"+userID.String(), "enr-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// Seed linked_item → from-account (Plaid) and to-account (Plaid) with distinct names.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-enr-token')
	`, linkedItemID, userID, "item-enr-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	fromAccID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-from-checking', 'Checking Account', 'depository')
	`, fromAccID, linkedItemID, userID)
	require.NoError(t, err, "seed from-account")

	toAccID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, nickname, type)
		VALUES ($1, $2, $3, 'plaid-to-savings', 'Savings', 'Emergency Fund', 'depository')
	`, toAccID, linkedItemID, userID)
	require.NoError(t, err, "seed to-account with nickname")

	// Seed a boost so we have a boost name to join.
	ruleID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO boosts (id, user_id, name, enabled, priority, boost_type, trigger_config, conditions, condition_logic, actions, created_at, updated_at)
		VALUES ($1, $2, 'Save Extra Cash', true, 0, 'category_rollover',
		        '{"type":"scheduled","cron":"0 0 1 * *"}'::jsonb,
		        '[]'::jsonb, 'AND', '[]'::jsonb, NOW(), NOW())
	`, ruleID, userID)
	require.NoError(t, err, "seed boost")

	// Seed a transaction (for the matched_transaction_at enrichment).
	txnID := uuid.New()
	txnCreatedAt := time.Now().Add(-2 * time.Hour)
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name, created_at)
		VALUES ($1, $2, $3, $4, 'plaid', -300.00, '2024-03-01', 'Transfer', $5)
	`, txnID, userID, toAccID, linkedItemID, txnCreatedAt)
	require.NoError(t, err, "seed transaction")

	// Seed a completed suggestion linked to the boost, from-account, to-account, and matched transaction.
	suggID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, boost_id, from_account_id, to_account_id,
			 amount, source, status, matched_transaction_id, actual_amount, completion_source,
			 completed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5,
		        300.00, 'boost', 'completed', $6, 300.00, 'exact_match',
		        NOW(), NOW(), NOW())
	`, suggID, userID, ruleID, fromAccID, toAccID, txnID)
	require.NoError(t, err, "seed completed suggestion")

	repo := NewTransferSuggestionRepository(tx)

	items, total, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusCompleted}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)

	got := items[0]
	assert.Equal(t, suggID, got.ID)

	// to_account_name: nickname wins over name when set.
	require.NotNil(t, got.ToAccountName, "ToAccountName must be populated from to-account JOIN")
	assert.Equal(t, "Emergency Fund", *got.ToAccountName)

	// from_account_name: no nickname → name used.
	require.NotNil(t, got.FromAccountName, "FromAccountName must be populated from from-account JOIN")
	assert.Equal(t, "Checking Account", *got.FromAccountName)

	// boost name from boosts JOIN.
	require.NotNil(t, got.BoostName, "BoostName must be populated from boosts JOIN")
	assert.Equal(t, "Save Extra Cash", *got.BoostName)

	// matched_transaction_at from transactions JOIN.
	require.NotNil(t, got.MatchedTransactionAt, "MatchedTransactionAt must be populated from transactions JOIN")
	assert.WithinDuration(t, txnCreatedAt, *got.MatchedTransactionAt, time.Second)
}

// TestGetByUserIDStatuses_EventTimeOrdering_Integration proves that results are ordered
// by event-time (COALESCE(completed_at, user_action_at, updated_at, created_at) DESC)
// rather than raw created_at DESC. A suggestion created long ago but completed recently
// must sort above a suggestion created recently but never actioned.
func TestGetByUserIDStatuses_EventTimeOrdering_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|ord-"+userID.String(), "ord-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	// "old-completed": created 10 minutes ago, completed 1 minute ago (high event time).
	oldCreated := time.Now().Add(-10 * time.Minute)
	recentCompleted := time.Now().Add(-1 * time.Minute)
	oldCompletedID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, completed_at, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'completed', $3, $4, $3)
	`, oldCompletedID, userID, recentCompleted, oldCreated)
	require.NoError(t, err, "seed old-created/recent-completed suggestion")

	// "new-rejected": created 5 minutes ago, rejected 8 minutes ago (lower event time).
	newCreated := time.Now().Add(-5 * time.Minute)
	olderRejected := time.Now().Add(-8 * time.Minute)
	newRejectedID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, user_action_at, created_at, updated_at)
		VALUES ($1, $2, 100.00, 'boost', 'rejected', $3, $4, $3)
	`, newRejectedID, userID, olderRejected, newCreated)
	require.NoError(t, err, "seed newer-created/older-event suggestion")

	repo := NewTransferSuggestionRepository(tx)

	items, total, err := repo.GetByUserIDStatuses(ctx, userID.String(),
		[]domain.SuggestionStatus{domain.SuggestionStatusCompleted, domain.SuggestionStatusRejected},
		10, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, items, 2)

	// old-completed (event time = completed_at = 1 min ago) must be first.
	assert.Equal(t, oldCompletedID, items[0].ID,
		"suggestion completed more recently must sort first, even if created earlier")
	assert.Equal(t, newRejectedID, items[1].ID,
		"suggestion with older event time must sort second, even if created more recently")
}

// TestCancelAcceptedSuggestion_Integration proves that CancelAcceptedSuggestion:
//   - Transitions pending → rejected (succeeds).
//   - Transitions accepted → rejected (the fix for the bug where UpdateStatus silently
//     no-ops on accepted rows due to WHERE status='pending').
//   - Clears matched_transaction_id in the same UPDATE.
//   - Returns ErrTransferSuggestionAlreadyActioned for completed/rejected/expired rows.
func TestCancelAcceptedSuggestion_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID.String(), "cancel-accepted-"+userID.String()+"@example.com")
	require.NoError(t, err)

	insert := func(status string, matchedTxnID *string) string {
		id := uuid.New().String()
		if matchedTxnID != nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO transfer_suggestions (id, user_id, amount, source, status, matched_transaction_id, created_at, updated_at)
				VALUES ($1, $2, 100.00, 'boost', $3, $4, NOW(), NOW())
			`, id, userID, status, *matchedTxnID)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO transfer_suggestions (id, user_id, amount, source, status, created_at, updated_at)
				VALUES ($1, $2, 100.00, 'boost', $3, NOW(), NOW())
			`, id, userID, status)
		}
		require.NoError(t, err)
		return id
	}

	// Seed linked_item → account → transaction so matched_transaction_id FK reference is valid.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-cancel-token')
	`, linkedItemID, userID, "item-cancel-"+linkedItemID.String())
	require.NoError(t, err)

	accountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Cancel Test Account', 'depository')
	`, accountID, linkedItemID, userID, "plaid-cancel-"+accountID.String())
	require.NoError(t, err)

	txnUUID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'plaid', -100.00, '2024-01-01', 'Cancel Test Txn')
	`, txnUUID, userID, accountID, linkedItemID)
	require.NoError(t, err)

	repo := NewTransferSuggestionRepository(tx)

	// pending → rejected.
	pendingID := insert("pending", nil)
	err = repo.CancelAcceptedSuggestion(ctx, pendingID, nil)
	require.NoError(t, err, "CancelAcceptedSuggestion on pending must succeed")
	s, err := repo.GetByID(ctx, pendingID)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusRejected, s.Status)

	// accepted → rejected (this is the bug fix — UpdateStatus would silently no-op here).
	txnID := txnUUID.String()
	acceptedID := insert("accepted", &txnID)
	err = repo.CancelAcceptedSuggestion(ctx, acceptedID, nil)
	require.NoError(t, err, "CancelAcceptedSuggestion on accepted must succeed")
	s, err = repo.GetByID(ctx, acceptedID)
	require.NoError(t, err)
	assert.Equal(t, domain.SuggestionStatusRejected, s.Status)
	assert.Nil(t, s.MatchedTransactionID, "matched_transaction_id must be cleared on cancel")

	// completed → AlreadyActioned.
	completedID := insert("completed", nil)
	err = repo.CancelAcceptedSuggestion(ctx, completedID, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"CancelAcceptedSuggestion on completed must return AlreadyActioned")

	// rejected → AlreadyActioned.
	rejectedID := insert("rejected", nil)
	err = repo.CancelAcceptedSuggestion(ctx, rejectedID, nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned,
		"CancelAcceptedSuggestion on rejected must return AlreadyActioned")

	// not found → ErrTransferSuggestionNotFound.
	err = repo.CancelAcceptedSuggestion(ctx, uuid.New().String(), nil)
	require.ErrorIs(t, err, domain.ErrTransferSuggestionNotFound,
		"CancelAcceptedSuggestion on non-existent ID must return NotFound")
}
