//go:build integration

package repository

// Issue #1569 Task 20 (N8) — the census claim, RE-DERIVED AND NOT REPRODUCED, then pinned.
//
// THE CLAIM: "ClearStaleReservations / ClearMatchedTransactionID null a user-confirmed match."
//
// IT DOES NOT REPRODUCE. A user confirming a match does not leave the row in a state either method
// can reach. transferSuggestionService.ConfirmTransferSuggestionMatch calls
// CompleteRetainingReservation, which moves the row to status = 'completed' and RETAINS
// matched_transaction_id (its name says so). Every statement in this repository that writes
// `matched_transaction_id = NULL` is scoped to non-terminal statuses — enumerated by
// `/usr/bin/grep -rn "matched_transaction_id = NULL" backend/`, four production sites, all four
// checked, none of them reachable for a 'completed' row:
//
//   ClearMatchedTransactionID   WHERE ... status IN ('pending', 'accepted')
//   ClearStaleReservations      WHERE ... status IN ('pending', 'accepted')
//   UpdateStatus (reject leg)   WHERE id = $5 AND status = 'pending'
//   CancelAcceptedSuggestion    WHERE id = $4 AND status IN ('pending', 'accepted')
//
// What the two named methods DO clear is a RESERVATION on a pending or accepted row — a machine's
// claim on an inflow, made by correlation, awaiting the user's confirmation. Releasing an expired
// one is the designed behaviour and is paired with closing the open notification so the user can be
// re-prompted (see ClearStaleReservations' GAP BA note); NOT releasing it would strand the
// suggestion permanently. 'accepted' is the user accepting the TRANSFER, not confirming the MATCH.
//
// So the item is deleted rather than fixed. This test is what makes that deletion checkable: it is
// the assertion the claim would have needed, run against the real schema. If a future change makes
// either method reach a completed row, this goes red and the claim becomes true.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferSuggestion_ConfirmedMatchSurvivesBothClears_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back on the shared integration DB

	userID, itemID, accountID := seedDownProbeFixture(t, ctx, tx, "n8-confirmed")
	_ = itemID

	txnID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, source, amount, date, name)
		VALUES ($1, $2, $3, 'plaid', -250.00, CURRENT_DATE, 'Transfer In')`,
		txnID, userID, accountID)
	require.NoError(t, err)

	// The row a user has CONFIRMED: completed, reservation retained, expires_at long past so
	// ClearStaleReservations' own date predicate is satisfied and only the status predicate can
	// save it. Without the past expires_at this test would pass for the wrong reason.
	confirmedID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, expires_at, matched_transaction_id, completed_at, completion_source, actual_amount)
		VALUES ($1, $2, 250.00, 'manual', 'completed', NOW() - INTERVAL '30 days', $3, NOW(), 'user_confirmed', 250.00)`,
		confirmedID, userID, txnID)
	require.NoError(t, err)

	// A control row in the state the two methods ARE for: accepted, reserved, expired. It must be
	// cleared, or this test proves only that the methods do nothing at all.
	//
	// It needs its OWN inflow: idx_transfer_sugg_matched_txn_id is a UNIQUE index, so one
	// transaction can be reserved by at most one suggestion. (Learned from the schema rejecting the
	// first draft of this fixture, not assumed.)
	controlTxnID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, source, amount, date, name)
		VALUES ($1, $2, $3, 'plaid', -250.00, CURRENT_DATE, 'Transfer In Control')`,
		controlTxnID, userID, accountID)
	require.NoError(t, err)

	acceptedID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfer_suggestions
			(id, user_id, amount, source, status, expires_at, matched_transaction_id)
		VALUES ($1, $2, 250.00, 'manual', 'accepted', NOW() - INTERVAL '30 days', $3)`,
		acceptedID, userID, controlTxnID)
	require.NoError(t, err)

	repo := NewTransferSuggestionRepository(tx)

	// ⚠ ClearStaleReservations is unscoped by user (it is a daily sweep), so it will also clear
	// stale reservations belonging to other tests' fixtures on this shared database. That is why
	// the whole test runs inside a rolled-back transaction, and why the assertions below are
	// per-row rather than on the returned count.
	_, err = repo.ClearStaleReservations(ctx)
	require.NoError(t, err)

	var afterSweep *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT matched_transaction_id::text FROM transfer_suggestions WHERE id = $1`, confirmedID).Scan(&afterSweep))
	require.NotNil(t, afterSweep,
		"ClearStaleReservations must not touch a completed suggestion — its status predicate is "+
			"IN ('pending','accepted'), and the confirmed match is the record of what the user said")
	assert.Equal(t, txnID, *afterSweep)

	var controlAfterSweep *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT matched_transaction_id::text FROM transfer_suggestions WHERE id = $1`, acceptedID).Scan(&controlAfterSweep))
	assert.Nil(t, controlAfterSweep,
		"control: an expired reservation on an ACCEPTED row must still be released, or this test "+
			"would pass against a sweep that does nothing")

	// The targeted clear must refuse the completed row too, and refuse it as an ERROR rather than
	// as a silent no-op — a caller told "done" about a write that did not happen is the silent-
	// success class this whole issue exists to remove.
	err = repo.ClearMatchedTransactionID(ctx, confirmedID, &txnID)
	require.Error(t, err, "ClearMatchedTransactionID must refuse a completed suggestion")

	var afterTargeted *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT matched_transaction_id::text FROM transfer_suggestions WHERE id = $1`, confirmedID).Scan(&afterTargeted))
	require.NotNil(t, afterTargeted)
	assert.Equal(t, txnID, *afterTargeted)

	// Completion metadata is likewise untouched: the reservation surviving would be worth little if
	// the answer it records did not.
	var completionSource *string
	var completedAt *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT completion_source, completed_at FROM transfer_suggestions WHERE id = $1`,
		confirmedID).Scan(&completionSource, &completedAt))
	require.NotNil(t, completionSource)
	assert.Equal(t, "user_confirmed", *completionSource)
	require.NotNil(t, completedAt)
}
