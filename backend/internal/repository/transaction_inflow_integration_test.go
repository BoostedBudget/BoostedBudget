//go:build integration

package repository

// TestGetRecentInflowsByUserID_FiltersPostedInflowsSinceCutoff_Integration validates
// that the SQL in GetRecentInflowsByUserID correctly applies all four filter conditions:
//   - amount < 0 (inflow only; outflows excluded)
//   - pending = false (posted only; pending transactions excluded)
//   - date >= since (cutoff; older transactions excluded)
//   - user_id = $1 (user-scoped; other users' transactions excluded)
//   - ORDER BY date ASC
//
// pgxmock unit tests return canned rows without running WHERE clauses; these
// four exclusion conditions require real SQL on real data to verify.
//
// Issue #972: Savings Transfer Automation — correlation source SQL (C4 test rigor).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration \
//	  -run TestGetRecentInflowsByUserID_FiltersPostedInflowsSinceCutoff -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRecentInflowsByUserID_FiltersPostedInflowsSinceCutoff_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user and their linked item + account.
	userID := uuid.New().String()
	otherUserID := uuid.New().String()

	for _, uid := range []string{userID, otherUserID} {
		_, err = tx.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, uid, "test|"+uid, "inflow-inttest-"+uid+"@example.com")
		require.NoError(t, err)
	}

	linkedItemID := uuid.New().String()
	otherLinkedItemID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-inflow-"+linkedItemID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, otherLinkedItemID, otherUserID, "item-other-"+otherLinkedItemID)
	require.NoError(t, err)

	accountID := uuid.New().String()
	otherAccountID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-inflow-test', 'Savings Account', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-other-inflow', 'Other Savings', 'depository')
	`, otherAccountID, otherLinkedItemID, otherUserID)
	require.NoError(t, err)

	// Dates for controlling the "since" cutoff.
	since := time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC)
	afterSince := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	beforeSince := time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC)

	insertTxn := func(uid, acctID, liID string, amount float64, date time.Time, pending bool) string {
		id := uuid.New().String()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions
				(id, user_id, account_id, linked_item_id, source, amount, date, name, pending)
			VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Test Transaction', $7)
		`, id, uid, acctID, liID, amount, date, pending)
		require.NoError(t, err)
		return id
	}

	// ✅ SHOULD be returned: posted inflow (amount<0) on or after since date.
	wantID1 := insertTxn(userID, accountID, linkedItemID, -500.0, afterSince, false)

	// ✅ SHOULD be returned: inflow on exactly the since date (inclusive boundary).
	wantID2 := insertTxn(userID, accountID, linkedItemID, -250.0, since, false)

	// ❌ Outflow (amount > 0): must be excluded.
	insertTxn(userID, accountID, linkedItemID, +200.0, afterSince, false)

	// ❌ Pending inflow: must be excluded (only posted = non-pending).
	insertTxn(userID, accountID, linkedItemID, -300.0, afterSince, true)

	// ❌ Old inflow (before since cutoff): must be excluded.
	insertTxn(userID, accountID, linkedItemID, -100.0, beforeSince, false)

	// ❌ Other user's inflow: must be excluded.
	insertTxn(otherUserID, otherAccountID, otherLinkedItemID, -400.0, afterSince, false)

	repo := NewTransactionRepository(tx, enc)
	results, err := repo.GetRecentInflowsByUserID(ctx, userID, since)
	require.NoError(t, err)

	// Exactly two rows should be returned (the two "want" rows above).
	require.Len(t, results, 2,
		"only two posted inflow transactions on or after %s should be returned; got %d",
		since.Format("2006-01-02"), len(results))

	// Verify the IDs match the expected rows.
	gotIDs := map[string]bool{results[0].ID: true, results[1].ID: true}
	assert.True(t, gotIDs[wantID1], "posted inflow after since must be in results")
	assert.True(t, gotIDs[wantID2], "posted inflow on since boundary must be in results")

	// Verify both are inflows (amount < 0) and not pending.
	for _, r := range results {
		assert.Less(t, r.Amount, 0.0,
			"result %s must be an inflow (amount < 0), got %f", r.ID, r.Amount)
		assert.Equal(t, domain.Transaction{}.Pending, r.Pending,
			"result %s must not be pending", r.ID) // Pending is bool zero-value = false
		assert.False(t, r.Pending, "result must be posted (non-pending)")
	}

	// Verify ORDER BY date ASC: first result should be the earlier date (since boundary).
	assert.Equal(t, wantID2, results[0].ID,
		"results must be ordered by date ASC: since-date row must come first")
	assert.Equal(t, wantID1, results[1].ID,
		"results must be ordered by date ASC: after-since row must come second")
}
