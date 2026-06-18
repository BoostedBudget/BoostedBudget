//go:build integration

package repository

// Integration tests for account Create/CreateBatch correctness (#1306 P1 fix).
// Verifies that accounts.user_account_id is persisted at creation time.
//
// Fixture note: institution_id and access_token_encrypted use plain-text sentinel
// values in all integration fixtures (consistent with seedInvestmentFixture). The
// user_accounts seed row only needs to satisfy the FK constraint — its columns are
// not read back by these tests.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_CreateBatch_PersistsUserAccountID_Integration verifies that
// CreateBatch persists user_account_id to the database.
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

// TestAccountRepository_Create_PersistsUserAccountID_Integration verifies that the
// single-row Create method also persists user_account_id. Create is not called from
// the production service path today (account_service.go uses CreateBatch), but it is
// a public interface method and the fix touched its INSERT too.
func TestAccountRepository_Create_PersistsUserAccountID_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p1306-cr-"+userID.String(), "p1306-cr-"+userID.String()+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "p1306-cr-item-"+linkedItemID.String())
	require.NoError(t, err)

	uaID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-p1306-cr')
	`, uaID, userID)
	require.NoError(t, err)
	uaStr := uaID.String()

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	acct := &domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		UserAccountID:  &uaStr,
		PlaidAccountID: "p1306-cr-acc-" + uuid.New().String(),
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	require.NoError(t, repo.Create(ctx, acct))

	var got *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM accounts WHERE id = $1`, acct.ID).Scan(&got))
	require.NotNil(t, got, "user_account_id must be persisted by Create, not silently dropped")
	assert.Equal(t, uaStr, *got)
}
