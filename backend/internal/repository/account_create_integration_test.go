//go:build integration

package repository

// Integration tests for account Create/CreateBatch correctness (#1306 P1 fix).
// Verifies that accounts.user_account_id is persisted at creation time.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_CreateBatch_PersistsUserAccountID_Integration is the RED gate
// for issue #1306 P1: CreateBatch must persist accounts.user_account_id, not silently
// drop it. Before the fix this test fails because the INSERT omits the column.
func TestAccountRepository_CreateBatch_PersistsUserAccountID_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p1306-"+userID.String(), "p1306-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Seed linked_item
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "p1306-item-"+linkedItemID.String())
	require.NoError(t, err)

	// Seed user_account (FK target must exist before the account row is created)
	uaID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-p1306')
	`, uaID, userID)
	require.NoError(t, err)
	uaStr := uaID.String()

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	acct := domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		UserAccountID:  &uaStr,
		PlaidAccountID: "p1306-acc-" + uuid.New().String(),
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	require.NoError(t, repo.CreateBatch(ctx, []domain.Account{acct}))

	var got *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM accounts WHERE id = $1`, acct.ID).Scan(&got))
	require.NotNil(t, got, "user_account_id must be persisted by CreateBatch, not silently dropped")
	assert.Equal(t, uaStr, *got)
}
